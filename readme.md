# Blog REST API

A small blog backend built as a take-home exercise: users, posts, and comments
with JWT authentication, backed by MySQL. The original assignment text lives in
[TASK.md](TASK.md).

## Overview

- Register and log in users; email/password authentication issues a JWT.
- CRUD blog posts; creating, updating, and deleting require authentication, and
  update/delete are restricted to the post's author.
- Comment on posts; the comment author is taken from the token and the display
  name is stored as a creation-time snapshot.
- Reads (posts, comments, health) are public.
- One binary serves the HTTP API; a second binary applies SQL migrations.
- `docker-compose up` starts MySQL, applies migrations, and serves the API on
  port 8080.

## Tech Stack

| Component        | Choice                                                        |
| ---------------- | ------------------------------------------------------------- |
| Language         | Go 1.27 (see the toolchain decision below)                     |
| HTTP framework   | Gin v1.12                                                      |
| Database         | MySQL 8.0 (InnoDB, `utf8mb4`)                                  |
| DB access        | `database/sql` + `github.com/go-sql-driver/mysql` — explicit SQL, no ORM |
| Auth             | `github.com/golang-jwt/jwt/v5` (HS256) + `golang.org/x/crypto/bcrypt` |
| Migrations       | `cmd/migrate`, a small up-only runner tracked in `schema_migrations` |
| Containerization | Multi-stage Dockerfile, Docker Compose                         |
| Dev convenience  | `github.com/joho/godotenv` (loads a local `.env`; production sets real env vars) |

## Architecture & Dependency Direction

Modular monolith with pragmatic hexagonal structure:

```
HTTP Handler → Application Service → Repository Port → MySQL Adapter
```

- Each module keeps its HTTP and MySQL adapters behind small interfaces
  (ports). Services depend only on those ports, `context.Context`, and plain
  Go models — never on Gin or `database/sql` types.
- Dependencies are wired by hand in `cmd/api` (no DI framework); the wiring is
  explicit and linear.
- Dependency direction: adapters and middleware may import the shared
  transport helpers; domain packages never import the transport layer.
  `response.FromError` is the only place that knows every module's error
  sentinels.
- Cross-module needs are expressed as consumer-side ports: the comments module
  defines `PostChecker` (satisfied by `posts.Service.Exists`) and
  `UserNameProvider` (satisfied by `identity.Service.UserName`), so it never
  imports the sibling services.

Why a monolith and not microservices: the domain is three tightly related
entities with one deployment, one database, and one team-sized scope.
Splitting it would add network failure modes, distributed transactions, and
infrastructure with no benefit at this scale.

## Modules

| Module     | Responsibility                                                        |
| ---------- | --------------------------------------------------------------------- |
| `identity` | Users, registration validation, bcrypt hashing, login, JWT issue/verify |
| `posts`    | Post CRUD, ownership rules, pagination, existence checks for comments |
| `comments` | Comment creation (author from token, name snapshot) and per-post listing |
| `httpapi`  | Router composition, auth middleware, request parsing, response envelope |
| `config`   | Environment-based configuration with validation                      |
| `platform/database` | Bounded MySQL connection pool shared by the adapters        |

## Folder Structure

```
.
├── app/
│   ├── cmd/
│   │   ├── api/main.go          # HTTP server entrypoint (wiring + lifecycle)
│   │   └── migrate/main.go      # up-only migration runner
│   ├── migrations/              # 00N_*.up.sql / 00N_*.down.sql
│   ├── tests/integration/       # env-gated MySQL integration tests
│   └── internal/
│       ├── config/              # env loading + validation
│       ├── platform/database/   # pooled *sql.DB
│       ├── httpapi/
│       │   ├── router.go        # route table, healthz, JSON 404
│       │   ├── middleware/      # Bearer auth (aborts the chain on failure)
│       │   ├── request/         # strict pagination + path-ID parsing
│       │   └── response/        # envelopes + central error mapping
│       ├── identity/            # model, errors, ports, service, jwt
│       │   └── adapter/{http,mysql}/
│       ├── posts/
│       │   └── adapter/{http,mysql}/
│       └── comments/
│           └── adapter/{http,mysql}/
├── Dockerfile                   # multi-stage build (api + migrate binaries)
├── docker-compose.yml           # db + migrate + app
├── Makefile                     # build/up/test/migrate helpers
├── .env.example                 # configuration template
├── blog-api.postman_collection.json
├── plan.md                      # implementation plan used during development
└── TASK.md                      # original assignment brief
```

## Database Schema & Relationships

```
users 1 ──── * posts 1 ──── * comments
users 1 ──── * comments
```

| Table               | Columns | Notes |
| ------------------- | ------- | ----- |
| `users`             | `id` PK, `name`, `email` (unique `uq_users_email`), `password_hash`, `created_at`, `updated_at` | Application-level limits: name and email ≤ 50 characters (columns keep wider slack). |
| `posts`             | `id` PK, `title`, `content` TEXT, `author_id` FK → `users` (RESTRICT), `created_at`, `updated_at` | `idx_posts_author (author_id)`, `idx_posts_created_id (created_at, id)` |
| `comments`          | `id` PK, `post_id` FK → `posts` (**CASCADE**), `author_id` FK → `users` (RESTRICT), `author_name`, `content` TEXT, `created_at` | `idx_comments_post_created_id (post_id, created_at, id)` |
| `schema_migrations` | `version` PK, `applied_at` | Applied migration versions |

- Deleting a post cascade-deletes its comments at the database level.
- `author_name` on comments is a **snapshot** taken from Identity at creation
  time; renaming a user never rewrites comment history.
- `comments.author_id` is a deliberate extension of the original assignment
  (which only listed `author_name`), needed because creating a comment
  requires authentication.

## API Endpoints

All paths are exact; `{id}` is a positive integer (anything else is a 400).

| Method | Path                      | Auth              | Success | Notes |
| ------ | ------------------------- | ----------------- | ------- | ----- |
| POST   | `/register`               | Public            | 201     | 409 on duplicate email |
| POST   | `/login`                  | Public            | 200     | Returns the JWT |
| GET    | `/healthz`                | Public            | 200     | 503 when the DB ping fails |
| GET    | `/posts`                  | Public            | 200     | Paginated list |
| GET    | `/posts/{id}`             | Public            | 200     | 404 when missing |
| POST   | `/posts`                  | Bearer            | 201     | Author from the token |
| PUT    | `/posts/{id}`             | Bearer (author)   | 200     | 403 for another user's post |
| DELETE | `/posts/{id}`             | Bearer (author)   | 204     | 403 for another user's post |
| GET    | `/posts/{id}/comments`    | Public            | 200     | Paginated; 404 when the post is gone |
| POST   | `/posts/{id}/comments`    | Bearer            | 201     | 404 when the post is gone |

## Authentication & Authorization

- `POST /login` returns a JWT (HS256). Send it as
  `Authorization: Bearer <token>` on protected routes.
- The token carries `sub` = user ID and an expiry (`JWT_TTL`, default 24h).
  Verification requires a valid HS256 signature, an `exp` claim, and a
  positive integer `sub`; every failure is a 401 and the middleware
  **aborts** the chain (`c.Abort()`), so protected handlers never run.
- Authorization policy: reads are public; writes require a token. Update and
  delete additionally check ownership in the service **and** in SQL
  (`WHERE id = ? AND author_id = ?`). A missing post is 404; a post owned by
  someone else is 403.
- Passwords are hashed with bcrypt; tokens and passwords are never logged.
- The comment author is always the authenticated user: `author_id` and
  `author_name` cannot be set from the request body.

### Pagination

`GET /posts` and `GET /posts/{id}/comments` accept `?page` and `?limit`:

- Absent parameters default to `page=1`, `limit=10`.
- A **present** parameter must be an integer in range (`page ≥ 1`,
  `1 ≤ limit ≤ 100`). `?page=0`, `?limit=0`, `?limit=101`, and non-numeric
  values are rejected with 400 `VALIDATION_ERROR` — never silently defaulted.
  A page so large that its row offset overflows is rejected as well
  (`Page is too large.`).

## Request & Response Examples

Envelope shapes:

- Single resource: `{"data": {...}}`
- List: `{"data": [...], "pagination": {"page": 1, "limit": 10, "total": 0}}`
- Error: `{"error": {"code": "VALIDATION_ERROR", "message": "..."}}`

Register:

```bash
curl -X POST localhost:8080/register -H 'Content-Type: application/json' \
  -d '{"name":"Budi","email":"budi@example.com","password":"secret123"}'
```

```json
{"data":{"id":1,"name":"Budi","email":"budi@example.com","created_at":"2026-10-11T00:00:00Z"}}
```

Login and use the token:

```bash
TOKEN=$(curl -s -X POST localhost:8080/login -H 'Content-Type: application/json' \
  -d '{"email":"budi@example.com","password":"secret123"}' | jq -r .data.token)

curl -X POST localhost:8080/posts -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"title":"Hello","content":"World"}'
```

```json
{"data":{"id":1,"title":"Hello","content":"World","author_id":1,"created_at":"...","updated_at":"..."}}
```

List posts:

```json
{"data":[{"id":1,"title":"Hello","content":"World","author_id":1,"created_at":"...","updated_at":"..."}],
 "pagination":{"page":1,"limit":10,"total":1}}
```

Create a comment (author comes from the token):

```bash
curl -X POST localhost:8080/posts/1/comments -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"content":"Nice post!"}'
```

```json
{"data":{"id":1,"post_id":1,"author_id":1,"author_name":"Budi","content":"Nice post!","created_at":"..."}}
```

Error example:

```json
{"error":{"code":"NOT_FOUND","message":"The requested resource was not found."}}
```

## Error Codes

| Code               | HTTP status | Meaning |
| ------------------ | ----------- | ------- |
| `VALIDATION_ERROR` | 400         | Invalid body, path ID, or pagination; carries a specific user-safe message |
| `UNAUTHORIZED`     | 401         | Missing/invalid/expired token, wrong credentials |
| `FORBIDDEN`        | 403         | Authenticated but not the resource owner |
| `NOT_FOUND`        | 404         | No such resource (also unknown routes) |
| `CONFLICT`         | 409         | Duplicate email registration |
| `INTERNAL_ERROR`   | 500         | Unhandled failure — generic message, cause logged server-side only |

Validation messages are specific and unit-free, for example:
`Password must be at least 8 characters.`,
`Password must be at most 15 characters.`,
`Password can only contain letters and numbers.`,
`Page must be at least 1.`,
`Page is too large.`,
`Limit must be between 1 and 100.`

Responses never contain SQL, DSNs, hashes, or stack traces.

## Configuration

All configuration comes from environment variables; real deployments set them
directly. For local development, `cmd/api` also loads the nearest `.env`
walking up from the working directory (variables already present in the
environment always win; the file is git-ignored).

| Variable          | Required | Default      | Notes |
| ----------------- | -------- | ------------ | ----- |
| `DB_DSN`          | yes      | —            | Must include `parseTime=true` so `TIMESTAMP` scans into `time.Time` |
| `JWT_SECRET`      | yes      | —            | At least 32 bytes; generate with `openssl rand -hex 32` |
| `APP_PORT`        | no       | `8080`       | HTTP listen port |
| `JWT_TTL`         | no       | `24h`        | Go duration syntax |
| `MIGRATIONS_DIR`  | no       | `migrations` | Used by `cmd/migrate`; relative to its working directory |

Example `.env` (see `.env.example`):

```
APP_PORT=8080
DB_DSN=root:abc123@tcp(127.0.0.1:3333)/appdb?parseTime=true&charset=utf8mb4
JWT_SECRET=replace-with-openssl-rand-hex-32-output
JWT_TTL=24h
```

## Running with Docker

```bash
docker-compose build
docker-compose up
```

The server is at http://localhost:8080; check it with
`curl localhost:8080/healthz`.

Compose runs three services:

- `db` — MySQL 8.0, published on `localhost:3333` (so host-side tools can
  reach the same data), health-checked, data persisted in the named volume
  `db_data`.
- `migrate` — one-shot job using the same image; waits for `db` to be healthy,
  applies migrations, exits 0.
- `app` — the API; waits until `migrate` completed successfully. It stops
  gracefully (15s drain, Compose allows 20s before SIGKILL).

Useful commands: `docker-compose ps`, `docker-compose logs -f app`,
`docker-compose down` (keeps data), `docker-compose down -v` (deletes data).

## Manual Setup

Prerequisites: Go 1.27, MySQL 8.0.

```bash
cd app
DB_DSN='root:abc123@tcp(127.0.0.1:3333)/appdb?parseTime=true&charset=utf8mb4' go run ./cmd/migrate
DB_DSN='...' JWT_SECRET='...' go run ./cmd/api
```

Or, with a root `.env` in place, `make migrate && make start` (see below). The
MySQL database (`appdb` by default) must exist first; migrations do not create
it.

## Makefile Targets

| Target             | What it does |
| ------------------ | ------------ |
| `make all`         | `build` + `up` |
| `make build`       | `docker-compose build` |
| `make up`          | `docker-compose up -d` |
| `make start`       | Runs the API on the host (`cd app && go run ./cmd/api`) |
| `make down`        | `docker-compose down` (keeps the volume) |
| `make restart`     | `down` + `up` |
| `make logs`        | Follows the app logs |
| `make test`        | `go test ./...` (integration tests skip without `TEST_MYSQL_DSN`) |
| `make test-integration` | Runs the env-gated integration suite against `localhost:3333` |
| `make clean`       | Removes this project's containers, volume, and image — no machine-wide prune |
| `make migrate`     | Applies migrations on the host, reading `DB_DSN` from the root `.env` |

## Database Migrations

- SQL files live in `app/migrations`, named `NNN_description.up.sql` with a
  matching `.down.sql` for manual rollback.
- `cmd/migrate` is up-only and idempotent: it records applied versions in
  `schema_migrations` and skips them on later runs. One statement per file.
- In Docker, migrations run automatically before the API starts.
- Locally: `make migrate` (or the explicit `go run ./cmd/migrate` above).

## Testing

```bash
cd app
go test ./...                      # unit suites — no database needed
go test -race ./...               # same, with the race detector
```

- Service, handler, middleware, JWT, and error-mapping logic is covered with
  in-memory fakes; adapter tests pin the MySQL error mapping (1062 → duplicate
  email, `ErrNoRows` → not found).
- MySQL-backed integration tests are gated by `TEST_MYSQL_DSN` and skip when it
  is unset, so `go test ./...` stays green without a database:

```bash
# with the compose database running (published on localhost:3333)
cd app && TEST_MYSQL_DSN='root:abc123@tcp(127.0.0.1:3333)/appdb?parseTime=true&charset=utf8mb4' \
  go test -count=1 ./tests/integration/... -v
```

They cover: migration idempotency against a real MySQL, the real unique-index
race (1062 → `CONFLICT`), the no-op update returning the row, and a delete
matching zero rows returning `NOT_FOUND` rather than a silent success.

- A ready-to-import Postman collection is included:
  `blog-api.postman_collection.json` (login saves the token automatically).

## Design Decisions & Trade-offs

- **Comment author relationship.** `author_id` was added because comment
  creation requires authentication; the token guarantees the author. The
  original `author_name` remains as a creation-time snapshot so comment
  history is stable even if the user's name changes later.
- **Authentication policy.** Reads are public, writes are authenticated. The
  assignment leaves comment authentication open; requiring a token is the
  deliberate choice and keeps "who wrote this" unambiguous.
- **Ownership enforcement.** The service checks ownership first (403 vs 404),
  and the SQL statements repeat the `author_id` guard as the integrity
  backstop — a race cannot update or delete another user's post.
- **Strict pagination.** Only *absent* parameters default. A present but
  invalid value (including `0`) is a 400: silently clamping would hide client
  bugs. The `(created_at, id)` indexes match the `ORDER BY created_at DESC,
  id DESC` listing queries.
- **Central error mapping.** `response.FromError` is the single transport-layer
  place that knows every module's sentinels; domain packages never import the
  response package. Cost: one case-group per module in one file. If module
  count or team boundaries grow, per-module translation in each adapter is the
  natural evolution.
- **Interfaces reduce coupling, not storage portability.** Swapping MySQL for
  another store is not automatic: schema, queries, and integrity behavior
  (unique index, FK cascade) all move with it. The ports keep the service
  layer testable and stable, not the storage choice free.
- **Go toolchain.** Upgraded from the assignment's 1.21 to 1.27 (1.21 is EOL).
  The upgrade is a direction decision, not a dependency requirement.
- **Error wording.** End-user messages are specific where actionable but never
  mention units or internals ("Content is too long.", never "bytes"). Unhandled
  errors return a generic 500 and are logged server-side.

## Scope & Future Improvements

Deliberately out of scope: frontend, Redis/caching, message brokers,
microservices, CQRS/event sourcing, likes/tags/categories, social login,
email verification, password reset, profile update endpoints, soft deletes,
admin tooling, and DI frameworks.

Natural next steps if this grew: refresh tokens with revocation, rate limiting
on auth endpoints, an OpenAPI spec generated from the routes, a CI pipeline
running lint + unit + integration suites, cursor-based pagination for large
tables, and structured request logging/tracing.
