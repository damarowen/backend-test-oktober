# Blog REST API Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a production-minded Blog REST API (users, posts, comments) in Go with MySQL, JWT auth, tested business logic, and one-command Docker startup.

**Architecture:** Modular Monolith with pragmatic Hexagonal Architecture. Three business modules (identity, posts, comments) inside one deployable binary; dependency flow is `HTTP Handler → Application Service → Repository Port → MySQL Adapter`, wired manually in `cmd/api`. Services depend on `context.Context` and small interfaces only — never on Gin or `database/sql` types.

**Tech Stack:** Go 1.27 (upgraded from 1.21 at requester's direction; 1.21 is EOL — see Task 10 README note), Gin, MySQL 8.0 (InnoDB, utf8mb4), `database/sql` + `github.com/go-sql-driver/mysql`, `golang.org/x/crypto/bcrypt`, `github.com/golang-jwt/jwt/v5`, SQL migrations via `cmd/migrate`, Go `testing` + `net/http/httptest`, Docker Compose.

**Spec:** `readme.md` (original take-home test) plus the Implementation Prompt supplied in session on 2026-10-10. Binding values from both are copied into Global Constraints below; when the two conflict with the prompt, the take-home requirements win and the conflict is documented in the README.

## Global Constraints

- Go toolchain: **1.27**. MySQL **8.0**. Keep the module at `app/` (module name `app`), Docker files and README at repo root — matches the existing repo layout.
- Public API paths exactly: `POST /register`, `POST /login`, `POST /posts`, `GET /posts`, `GET /posts/{id}`, `PUT /posts/{id}`, `DELETE /posts/{id}`, `POST /posts/{id}/comments`, `GET /posts/{id}/comments`, `GET /healthz`. Gin uses `:id` internally; public paths unchanged.
- Response envelopes — single: `{"data": {...}}`; list: `{"data": [...], "pagination": {"page": 1, "limit": 10, "total": 0}}`; error: `{"error": {"code": "VALIDATION_ERROR", "message": "The request is invalid."}}`.
- Error codes → status: `VALIDATION_ERROR`→400, `UNAUTHORIZED`→401, `FORBIDDEN`→403, `NOT_FOUND`→404, `CONFLICT`→409, `INTERNAL_ERROR`→500. Never leak SQL, DSNs, hashes, or stack traces.
- Validation limits: name trimmed non-empty ≤ 50 chars; email valid ≤ 50 chars; password **8–15 characters, letters and digits only (`[A-Za-z0-9]`)**; title trimmed non-empty ≤ 255 chars; post/comment content non-empty ≤ 65,535 bytes (fits `TEXT`); IDs positive int64; pagination: missing → `page=1, limit=10`, explicitly out of range (page < 1, limit < 1 or > 100) → 400.
- Error wording: end-user messages never use technical units ("bytes") or internal detail. Password messages are `Password must be at least 8 characters.`, `Password must be at most 15 characters.`, `Password can only contain letters and numbers.`
- Email normalization: `strings.TrimSpace` + `strings.ToLower`, applied identically at register and login. DB unique index is the real guarantee.
- JWT: HS256 only (reject other algs), `sub` = user ID, expiry `JWT_TTL` (default 24h), secret from env only (required, ≥ 32 chars). Tokens and passwords never logged.
- Auth policy: public = register, login, get/list posts, list comments. Auth required = create post, update/delete post, create comment. Update/delete enforce ownership in service **and** SQL (`WHERE id = ? AND author_id = ?`): 404 if missing, 403 if owned by another user.
- `comments.author_id` is a **deliberate entity extension** (documented); `author_name` is a server-side snapshot from Identity at creation time — never taken from the request body.
- SQL: parameterized statements only. Explicit SQL, no ORM. Migrations applied by `cmd/migrate` (runs before the API in Docker Compose), tracked in `schema_migrations`.
- Scope exclusions (do NOT implement): frontend, Redis, Kafka/RabbitMQ, microservices, CQRS, event sourcing, likes/tags/categories, social login, email verification, password reset, admin dashboard, profile update endpoints, soft delete, generic repositories, DI frameworks.
- **Commits:** the requester manages git history. Do NOT commit per task — leave changes in the working tree and report them. This rule supersedes the per-task `Commit` steps below: do not run them; end each task with a review instead.

## Review Focus

1. **Password outside 8–15 characters, or containing anything other than letters and digits** → 400 `VALIDATION_ERROR` with a specific, byte-free message. Pinned by `TestRegister_PasswordBoundaries` + `TestRegister_PasswordMessages` (Task 2).
2. **PUT update with identical values** (no-op) → 200 with the resource, not 404/500. Pinned by `TestRepository_Update_NoChange_ReturnsPost` (Task 6).
3. **Post deleted between comment existence check and insert** → 404 `NOT_FOUND`, no MySQL error text leaked. Pinned by `TestService_Create_FKRace_MapsToPostNotFound` (Task 7).
4. **JWT tampering and missing claims** — `alg: none`/RS256 → 401 (the algorithm is never taken from the token header); a correctly signed token **without `exp`** → 401 (`jwt.WithExpirationRequired()`); a correctly signed token with garbage/zero/negative `sub` → 401. Pinned by `TestJWT_WrongAlgorithmRejected`, `TestJWT_MissingExpiryRejected`, `TestJWT_InvalidSubjectRejected` (Task 3).
5. **Concurrent duplicate registration** → 409 `CONFLICT`, not 500. Pinned by `TestRepository_MapError_DuplicateEmail` (Task 4: unit mapping of MySQL 1062 → `ErrEmailTaken`) and `TestIntegration_UserEmailUniqueness` (Task 9: real unique index, real race).
6. **401 must abort the Gin chain** — an unauthenticated request must never reach a protected handler; writing 401 without `c.Abort()` is not enough. Pinned by `TestAuth_Failure_AbortsChain` (Task 3).

---

### Task 1: Foundation — dependencies, config, database platform, migrations

**Files:**
- Modify: `app/go.mod`
- Create: `app/internal/config/config.go`, `app/internal/config/config_test.go`
- Create: `app/internal/platform/database/mysql.go`
- Create: `app/migrations/001_create_users.up.sql`, `app/migrations/001_create_users.down.sql`, `app/migrations/002_create_posts.up.sql`, `app/migrations/002_create_posts.down.sql`, `app/migrations/003_create_comments.up.sql`, `app/migrations/003_create_comments.down.sql`
- Create: `app/cmd/migrate/main.go`
- Create: `.env.example`

**Interfaces:**
- Consumes: nothing (first task).
- Produces:
  - `config.Config{Port string; DBDSN string; JWTSecret string; JWTTTL time.Duration}`
  - `config.Load() (Config, error)` — reads `APP_PORT` (default `8080`), `DB_DSN` (required), `JWT_SECRET` (required, ≥ 32 chars), `JWT_TTL` (default `24h`); returns descriptive errors for missing/invalid values.
  - `database.Open(ctx context.Context, dsn string) (*sql.DB, error)` — pool with `SetMaxOpenConns(25)`, `SetMaxIdleConns(10)`, `SetConnMaxLifetime(5 * time.Minute)`, `PingContext` on open.
  - Migrations DDL as specified in step 5; `cmd/migrate` applies `app/migrations/*.up.sql` in filename order, recording versions in `schema_migrations(version VARCHAR(32) PRIMARY KEY, applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP)`, idempotent on re-run.

- [ ] **Step 1: Write failing config tests** in `app/internal/config/config_test.go`:
  - `TestLoad_Defaults` — set only `DB_DSN` + `JWT_SECRET`; expect `Port == "8080"`, `JWTTTL == 24*time.Hour`.
  - `TestLoad_MissingJWTSecret_ReturnsError` — clear `JWT_SECRET`; expect error mentioning `JWT_SECRET`.
  - `TestLoad_ShortJWTSecret_ReturnsError` — `JWT_SECRET` of 8 chars; expect error.
  - `TestLoad_MissingDBDSN_ReturnsError`.
- [ ] **Step 2: Run tests to verify they fail.** Run: `cd app && go test ./internal/config/...` — Expected: FAIL, package does not exist.
- [ ] **Step 3: Add dependencies.** Run: `cd app && go get github.com/gin-gonic/gin@latest github.com/go-sql-driver/mysql@latest golang.org/x/crypto@latest github.com/golang-jwt/jwt/v5@latest` and set `go 1.27` in `go.mod`.
- [ ] **Step 4: Implement `config.go` and `database/mysql.go`** to the Produces signatures.
- [ ] **Step 5: Write the migrations** (utf8mb4, InnoDB, `BIGINT` signed PKs):
  - `users(id BIGINT AUTO_INCREMENT PK, name VARCHAR(100) NOT NULL, email VARCHAR(255) NOT NULL, password_hash VARCHAR(255) NOT NULL, created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP, UNIQUE KEY uq_users_email (email))`
    - Requester decision: application-level validation caps name and email at **50 characters** (`maxNameLength`/`maxEmailLength`), stricter than the column widths above. The columns keep their wider capacity as harmless slack; the app is the enforcing gate. Tightening the columns to `VARCHAR(50)` is optional and would need a fresh database or an `ALTER`.
  - `posts(id BIGINT PK AI, title VARCHAR(255) NOT NULL, content TEXT NOT NULL, author_id BIGINT NOT NULL, created_at/updated_at as above, CONSTRAINT fk_posts_author FOREIGN KEY (author_id) REFERENCES users(id) ON DELETE RESTRICT, KEY idx_posts_author (author_id), KEY idx_posts_created_id (created_at, id))`
  - `comments(id BIGINT PK AI, post_id BIGINT NOT NULL, author_id BIGINT NOT NULL, author_name VARCHAR(100) NOT NULL, content TEXT NOT NULL, created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP, CONSTRAINT fk_comments_post FOREIGN KEY (post_id) REFERENCES posts(id) ON DELETE CASCADE, CONSTRAINT fk_comments_author FOREIGN KEY (author_id) REFERENCES users(id) ON DELETE RESTRICT, KEY idx_comments_post_created_id (post_id, created_at, id))`
  - `.down.sql` files drop in reverse order.
- [ ] **Step 6: Implement `cmd/migrate/main.go`** (up-only runner described in Produces; clear startup errors; `os.Exit(1)` on failure).
- [ ] **Step 7: Verify.** Run: `cd app && gofmt -w . && go vet ./... && go test ./... && go build ./...` — Expected: all PASS.
- [ ] **Step 8: Review** — no commit (the requester owns git history).

### Task 2: Identity service — registration and login logic

**Files:**
- Create: `app/internal/identity/model.go`, `errors.go`, `ports.go`, `service.go`
- Test: `app/internal/identity/service_test.go`, `app/internal/identity/fake_test.go`

**Interfaces:**
- Consumes: nothing external.
- Produces:
  - `identity.User{ID int64; Name, Email, PasswordHash string; CreatedAt, UpdatedAt time.Time}`
  - Sentinels: `ErrEmailTaken`, `ErrInvalidCredentials`, `ErrUserNotFound`, `ErrInvalidInput` (all `errors.New` with `identity:` prefix).
  - `UserRepository interface { Create(ctx, name, email, passwordHash string) (User, error); GetByEmail(ctx, email string) (User, error); GetByID(ctx, id int64) (User, error) }`
  - `TokenService interface { Issue(userID int64) (string, error); Verify(token string) (int64, error) }`
  - `NewService(repo UserRepository, tokens TokenService) *Service`
  - `(s *Service) Register(ctx, name, email, password string) (User, error)` — validate, normalize email, bcrypt hash (`bcrypt.GenerateFromPassword`, cost default), delegate duplicate handling to repo error mapping.
  - `(s *Service) Login(ctx, email, password string) (string, error)` — returns signed token; unknown email and wrong password both return `ErrInvalidCredentials`.
  - `(s *Service) UserName(ctx, userID int64) (string, error)` — `ErrUserNotFound` when missing (used by comments module later).

- [ ] **Step 1: Write failing tests** (`fake_test.go` holds an in-memory `UserRepository` fake and a stub `TokenService`):
  - `TestRegister_Success` — returned user's `Email` is lowercased/trimmed ("  Budi@Example.COM " → "budi@example.com"), `PasswordHash != password`, `bcrypt.CompareHashAndPassword` succeeds.
  - `TestRegister_InvalidInput` (table-driven) — empty name, name > 50 chars, invalid email, email > 50 chars, empty/short password, 16-char password, password with symbol/space/non-ASCII → `ErrInvalidInput`.
  - `TestRegister_PasswordBoundaries` — 7 chars fail, 8 pass, 15 pass, 16 fail.
  - `TestRegister_PasswordMessages` — exact user-facing strings, and asserts no message contains "byte".
  - `TestRegister_DuplicateEmail` — fake returns `ErrEmailTaken` → surfaces `ErrEmailTaken`.
  - `TestLogin_Success` — returns the stub token.
  - `TestLogin_WrongPassword` and `TestLogin_UnknownEmail` — both `ErrInvalidCredentials` (identical error, no user enumeration).
  - `TestUserName_Found` / `TestUserName_NotFound`.
- [ ] **Step 2: Run tests to verify they fail.** Run: `cd app && go test ./internal/identity/...` — Expected: FAIL, undefined symbols.
- [ ] **Step 3: Implement `model.go`, `errors.go`, `ports.go`, `service.go`** to the Produces signatures. Email normalization in one private helper used by both Register and Login.
- [ ] **Step 4: Run tests to verify they pass.** Run: `cd app && go test ./internal/identity/... -v` — Expected: PASS.
- [ ] **Step 5: Review** — no commit (the requester owns git history).

### Task 3: JWT, response envelope, auth middleware

**Files:**
- Create: `app/internal/identity/jwt.go`, `app/internal/identity/jwt_test.go`
- Create: `app/internal/httpapi/response/response.go`, `app/internal/httpapi/response/response_test.go`
- Create: `app/internal/httpapi/middleware/auth.go`, `app/internal/httpapi/middleware/auth_test.go`

**Interfaces:**
- Consumes: `identity.TokenService` (Task 2).
- Produces:
  - `identity.NewJWT(secret string, ttl time.Duration) *JWT` implementing `TokenService`. Claims: `jwt.RegisteredClaims` with `Subject` = decimal user ID, `ExpiresAt` = now+ttl, `IssuedAt`; HS256. **Verification is strict:** parse with `jwt.WithValidMethods([]string{"HS256"})` **and** `jwt.WithExpirationRequired()` — a signed token with no `exp` must be rejected. After parsing, validate `Subject`: `strconv.ParseInt` must succeed and the value must be **> 0**; missing/garbage/zero/negative → error. No logging of tokens.
  - `response.Data(c *gin.Context, status int, data any)` → `{"data": ...}`
  - `response.Paginated(c *gin.Context, data any, page, limit int, total int64)` → `{"data": ..., "pagination": {...}}`
  - `response.APIError(c *gin.Context, status int, code, message string)` → `{"error": {...}}`
  - `response.FromError(c *gin.Context, err error) bool` — maps module sentinels (`ErrInvalidInput`→400, `ErrInvalidCredentials`/auth errors→401, `ErrForbidden`→403, `ErrNotFound`/`ErrUserNotFound`/`ErrPostNotFound`→404, `ErrEmailTaken`→409, else→500 `INTERNAL_ERROR` with generic message). Returns false for unhandled errors. For `ErrInvalidInput`, the error message is surfaced to the client: when `errors.As` yields an `identity.ValidationError`, its `Message` is used verbatim; otherwise the generic `The request is invalid.` is used. No other error ever exposes its raw text.
  - `middleware.Auth(tokens identity.TokenService) gin.HandlerFunc` — reads `Authorization: Bearer <token>`, verifies, stores user ID under typed unexported key. **Every failure path must abort the Gin chain** (`c.AbortWithStatusJSON(401, …)` or `c.Abort()` then `return`) so the protected handler never runs: missing header, wrong scheme, malformed/invalid/expired token, or a non-positive user ID. Only the success path calls `c.Next()`.
  - `middleware.UserID(c *gin.Context) (int64, bool)` — the only way to read the authenticated ID.

- [ ] **Step 1: Write failing tests:**
  - `TestJWT_IssueAndVerify_RoundTrip` — `Verify(Issue(42)) == 42`.
  - `TestJWT_ExpiredTokenRejected` — `NewJWT(secret, -1*time.Minute)`; Verify → error.
  - `TestJWT_MissingExpiryRejected` — hand-craft a token signed with the correct secret but **no `exp`**; Verify → error (Review Focus #4).
  - `TestJWT_InvalidSubjectRejected` (table) — `Subject` = `""`, `"abc"`, `"0"`, `"-1"` on correctly signed tokens; Verify → error.
  - `TestJWT_TamperedSignatureRejected`.
  - `TestJWT_WrongAlgorithmRejected` — hand-craft an `alg: none` and an RS256 token; Verify → error (Review Focus #4).
  - `response_test.go`: `TestFromError_MapsSentinels` (table-driven over every sentinel → expected status/code), `TestFromError_ValidationMessage_Surfaced` (a `*identity.ValidationError` message reaches the body verbatim), `TestFromError_Unknown_Returns500Generic` (message must not contain the raw error text).
  - `TestAuth_MissingHeader_Returns401`, `TestAuth_MalformedHeader_Returns401`, `TestAuth_InvalidToken_Returns401`, `TestAuth_ValidToken_SetsUserID` (uses `middleware.UserID`).
  - `TestAuth_Failure_AbortsChain` (table: header missing, wrong scheme, invalid token, valid signature with non-positive `sub`) — asserts status 401 **and** that the protected handler was never executed (flag set inside the handler must stay false) (Review Focus #6).
- [ ] **Step 2: Run to verify failure.** Run: `cd app && go test ./internal/identity/... ./internal/httpapi/...` — Expected: FAIL, undefined symbols.
- [ ] **Step 3: Implement `jwt.go`, `response/response.go`, `middleware/auth.go`.**
- [ ] **Step 4: Run to verify pass.** Run: `cd app && go test ./internal/identity/... ./internal/httpapi/... -v` — Expected: PASS.
- [ ] **Step 5: Review** — no commit (the requester owns git history).

### Task 4: Identity MySQL adapter and HTTP handlers

**Files:**
- Create: `app/internal/identity/adapter/mysql/repository.go`, `app/internal/identity/adapter/mysql/repository_test.go`
- Create: `app/internal/identity/adapter/http/handler.go`, `app/internal/identity/adapter/http/handler_test.go`

**Interfaces:**
- Consumes: `identity.UserRepository` port + `identity.Service` (Task 2), `response` (Task 3), `*sql.DB` from `database.Open` (Task 1).
- Produces:
  - `identitymysql.NewRepository(db *sql.DB) *Repository` implementing `identity.UserRepository`:
    - `Create` → `INSERT INTO users (name, email, password_hash) VALUES (?,?,?)`, then `LastInsertId`, then re-select by id for `created_at`/`updated_at`.
    - `GetByEmail` / `GetByID` → `SELECT id, name, email, password_hash, created_at, updated_at FROM users WHERE email = ?` / `WHERE id = ?`.
    - `mapError(err error) error` (unexported, unit-tested without a DB): `*mysql.MySQLError` with `Number == 1062` → `identity.ErrEmailTaken`; `sql.ErrNoRows` → `identity.ErrUserNotFound`; anything else passes through unchanged so a DB outage stays a 500.
    - **Why this mapping is the real guard:** the service's duplicate pre-check has a TOCTOU race, so under concurrent registration only the unique index + 1062 mapping turns the collision into 409 instead of 500.
  - `identityhttp.NewHandler(svc *identity.Service) *Handler`
  - `(h *Handler) RegisterAuth(r *gin.RouterGroup)` — registers `POST /register` and `POST /login` (exact paths; pass `r := engine.Group("/")` later in Task 8).
  - Request DTOs decoded with `ShouldBindJSON`; unknown/extra ownership fields are ignored (decode into explicit DTO structs with only expected fields).
  - Responses: register → 201 `{"data": {"id", "name", "email", "created_at"}}` (never `password_hash`); login → 200 `{"data": {"token", "token_type": "Bearer", "expires_in": <seconds>}}`.

- [ ] **Step 1: Write failing tests** — repository error mapping first, then httptest handlers (build a minimal `gin.New()` engine in the test):
  - `TestRepository_MapError_DuplicateEmail` — `mapError(&mysql.MySQLError{Number: 1062, Message: "Duplicate entry ... for key 'uq_users_email'"})` → `identity.ErrEmailTaken`.
  - `TestRepository_MapError_NoRows` — `mapError(sql.ErrNoRows)` → `identity.ErrUserNotFound`.
  - `TestRepository_MapError_Other` — an unrelated error is returned unchanged (not wrapped into a business error).
  - `TestRegister_Created_201` — body matches envelope, JSON must not contain "password_hash", status 201.
  - `TestRegister_MalformedJSON_Returns400`.
  - `TestRegister_InvalidFields_Returns400_ValidationError`.
  - `TestRegister_DuplicateEmail_Returns409_Conflict`.
  - `TestLogin_OK_ReturnsToken` — 200, `token_type == "Bearer"`, `expires_in == 86400`.
  - `TestLogin_BadPassword_Returns401_GenericMessage` — message identical to unknown-email case.
- [ ] **Step 2: Run to verify failure.** Run: `cd app && go test ./internal/identity/...` — Expected: FAIL (undefined symbols in the new packages).
- [ ] **Step 3: Implement `repository.go`** (parameterized SQL only; `Create`/`GetByEmail`/`GetByID` route every error through `mapError`) **and `handler.go`** mapping service errors through `response.FromError`.
- [ ] **Step 4: Run to verify pass.** Run: `cd app && go test ./internal/identity/... -v` — Expected: PASS.
- [ ] **Step 5: Review** — no commit (see Global Constraints: the requester owns git history).

### Task 5: Posts service — CRUD, ownership, pagination

**Files:**
- Create: `app/internal/posts/model.go`, `errors.go`, `ports.go`, `service.go`
- Test: `app/internal/posts/service_test.go`, `app/internal/posts/fake_test.go`

**Interfaces:**
- Consumes: nothing external.
- Produces:
  - `posts.Post{ID, AuthorID int64; Title, Content string; CreatedAt, UpdatedAt time.Time}`
  - Sentinels: `ErrNotFound`, `ErrForbidden`, `ErrInvalidInput`.
  - `Repository interface { Create(ctx, authorID int64, title, content string) (Post, error); GetByID(ctx, id int64) (Post, error); List(ctx, page, limit int) ([]Post, int64, error); Update(ctx, id, authorID int64, title, content string) (Post, error); Delete(ctx, id, authorID int64) error }`
  - `NewService(repo Repository) *Service`
  - `(s *Service) Create(ctx, userID int64, title, content string) (Post, error)`
  - `(s *Service) Get(ctx, id int64) (Post, error)`
  - `(s *Service) List(ctx, page, limit int) ([]Post, int64, error)` — `normalizePagination(page, limit) (int, int, error)`: 0 → defaults 1/10, out of range → `ErrInvalidInput`.
  - `(s *Service) Update(ctx, userID, id int64, title, content string) (Post, error)` — full update of title+content; ownership check (missing → `ErrNotFound`, other author → `ErrForbidden`); author never changes.
  - `(s *Service) Delete(ctx, userID, id int64) error` — same ownership rules.
  - `(s *Service) Exists(ctx, id int64) (bool, error)` — satisfies comments' `PostChecker` port (Task 7).

- [ ] **Step 1: Write failing tests** (fake repository in `fake_test.go`):
  - `TestCreate_Success` — repo receives the authenticated `userID`, ignores nothing else (signature has no author field).
  - `TestCreate_InvalidInput` (table) — empty/whitespace title, title > 255, empty content, content > 65,535 bytes → `ErrInvalidInput`.
  - `TestGet_NotFound` — `ErrNotFound`.
  - `TestList_Defaults` — `List(ctx, 0, 0)` calls repo with page=1, limit=10.
  - `TestList_InvalidPagination` (table) — `(-1, 10)`, `(1, 0-)`→default ok, `(1, 101)`, `(1, -5)` → `ErrInvalidInput` for out-of-range only (Review Focus check for limits).
  - `TestUpdate_ByOwner_Succeeds` — author ID unchanged.
  - `TestUpdate_ByAnotherUser_Forbidden` — `ErrForbidden` (Review: 403 not 404).
  - `TestUpdate_NotFound`.
  - `TestDelete_ByOwner_Succeeds`, `TestDelete_ByAnotherUser_Forbidden`, `TestDelete_NotFound`.
  - `TestExists_True/False`.
- [ ] **Step 2: Run to verify failure.** Run: `cd app && go test ./internal/posts/...` — Expected: FAIL.
- [ ] **Step 3: Implement the posts domain files** to the Produces signatures.
- [ ] **Step 4: Run to verify pass.** Run: `cd app && go test ./internal/posts/... -v` — Expected: PASS.
- [ ] **Step 5: Review** — no commit (the requester owns git history).

### Task 6: Posts MySQL adapter and HTTP handler

**Files:**
- Create: `app/internal/posts/adapter/mysql/repository.go`, `app/internal/posts/adapter/mysql/repository_test.go` (error-mapping unit tests; DB-backed tests live in Task 9)
- Create: `app/internal/posts/adapter/http/handler.go`, `app/internal/posts/adapter/http/handler_test.go`

**Interfaces:**
- Consumes: `posts.Repository` port (Task 5), `response`, `middleware` (Task 3).
- Produces:
  - `postsmysql.NewRepository(db *sql.DB) *Repository` implementing `posts.Repository`. Queries: `Create` `INSERT INTO posts (title, content, author_id) VALUES (?,?,?)` + re-select; `GetByID`; `List` with `SELECT ... ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?` + `SELECT COUNT(*) FROM posts`; `Update` `UPDATE posts SET title=?, content=? WHERE id=? AND author_id=?`; `Delete` `DELETE FROM posts WHERE id=? AND author_id=?`.
  - **No-op update decision:** when `Update` reports 0 rows affected, re-`SELECT` by id: row found → return it (update changed nothing); not found → `ErrNotFound`. Do not require `clientFoundRows`.
  - `postshttp.NewHandler(svc *posts.Service) *Handler`
  - `(h *Handler) Register(r *gin.RouterGroup, auth gin.HandlerFunc)` — `POST /posts` and `PUT/DELETE /posts/:id` behind `auth`; `GET /posts`, `GET /posts/:id` public. Query `?page=&limit=` parsed with `strconv`.
  - Responses: create → 201; get → 200; list → 200 paginated; update → 200 with updated resource; delete → 204 (no body).

- [ ] **Step 1: Write failing tests:**
  - `TestRepository_Update_NoChange_ReturnsPost` — fake `sql` driver or stubbed affected-rows=0 path returns the row (Review Focus #2). If driving this through `database/sql` stubs is disproportionate, pin the decision with a driver-level test against the integration harness in Task 9 instead — but the behavior must be pinned by a named test somewhere.
  - `TestRepository_MapError` (table): unrelated MySQL errors pass through unchanged, so an insert with a bad `author_id` surfaces as an error rather than being misreported as a business condition. (Identity's 1062 mapping — Review Focus #5 — is pinned in Task 4; comments' 1452 → `ErrPostNotFound` mapping is pinned in Task 7.)
  - HTTP (httptest, real `posts.Service` over a fake repo):
    - `TestCreatePost_NoAuth_Returns401`
    - `TestCreatePost_Authenticated_Returns201_AuthorFromToken` — request body containing `author_id: 999` is ignored; response `author_id` == token user.
    - `TestCreatePost_InvalidBody_Returns400`
    - `TestGetPost_Found_Returns200` / `TestGetPost_Missing_Returns404` / `TestGetPost_InvalidID_Returns400`
    - `TestListPosts_DefaultPagination` — response `pagination == {page:1, limit:10, total:<n>}`.
    - `TestListPosts_LimitOver100_Returns400`
    - `TestUpdatePost_ByOtherUser_Returns403`, `TestUpdatePost_Missing_Returns404`, `TestUpdatePost_ByOwner_Returns200`
    - `TestDeletePost_ByOwner_Returns204`, `TestDeletePost_ByOtherUser_Returns403`
- [ ] **Step 2: Run to verify failure.** Run: `cd app && go test ./internal/posts/adapter/...` — Expected: FAIL.
- [ ] **Step 3: Implement `repository.go` and `handler.go`.**
- [ ] **Step 4: Run to verify pass.** Run: `cd app && go test ./internal/posts/... -v` — Expected: PASS.
- [ ] **Step 5: Review** — no commit (the requester owns git history).

### Task 7: Comments module — service, ports, MySQL adapter, handler

**Files:**
- Create: `app/internal/comments/model.go`, `errors.go`, `ports.go`, `service.go`
- Create: `app/internal/comments/adapter/mysql/repository.go`
- Create: `app/internal/comments/adapter/http/handler.go`
- Test: `app/internal/comments/service_test.go`, `fake_test.go`, `app/internal/comments/adapter/http/handler_test.go`

**Interfaces:**
- Consumes: `posts.PostChecker` target satisfied by `posts.Service.Exists` (Task 5); `identity.Service.UserName` (Task 2).
- Produces:
  - `comments.Comment{ID, PostID, AuthorID int64; AuthorName, Content string; CreatedAt time.Time}`
  - Sentinels: `ErrPostNotFound`, `ErrInvalidInput`, `ErrAuthorNotFound`, plus `ErrInvalidInput` reused semantics for empty content.
  - Small ports (defined here, implemented by existing services — no import cycle):
    - `PostChecker interface { Exists(ctx context.Context, postID int64) (bool, error) }`
    - `UserNameProvider interface { UserName(ctx context.Context, userID int64) (string, error) }`
  - `Repository interface { Create(ctx, c Comment) (Comment, error); ListByPost(ctx, postID int64, page, limit int) ([]Comment, int64, error) }`
  - `NewService(repo Repository, posts PostChecker, users UserNameProvider) *Service`
  - `(s *Service) Create(ctx, postID, authorID int64, content string) (Comment, error)` — validate content; `posts.Exists` → `ErrPostNotFound`; `users.UserName` → `ErrAuthorNotFound`; store snapshot `AuthorName`. MySQL 1452 from repo (post deleted in the race window) → `ErrPostNotFound` (Review Focus #3).
  - `(s *Service) ListByPost(ctx, postID int64, page, limit int) ([]Comment, int64, error)` — post missing → `ErrPostNotFound`; empty slice (not nil in JSON — must marshal `[]`) when no comments; same pagination rules as posts; order `created_at DESC, id DESC` in the adapter.

- [ ] **Step 1: Write failing tests:**
  - `TestCreate_Success` — stored comment has `AuthorID` from args, `AuthorName` from `UserNameProvider` (request-supplied names impossible by construction — handler DTO has only `content`).
  - `TestCreate_InvalidContent` (table) — empty, whitespace-only, > 65,535 bytes → `ErrInvalidInput`.
  - `TestCreate_PostNotFound` — `PostChecker` returns false → `ErrPostNotFound`.
  - `TestCreate_FKRace_MapsToPostNotFound` — repo returns a FK-violation error → `ErrPostNotFound`, and the error message surfaced to clients never contains "Error 1452" (Review Focus #3).
  - `TestListByPost_PostNotFound`, `TestListByPost_Empty` (returns empty slice, total 0), `TestListByPost_Defaults`.
  - HTTP: `TestCreateComment_NoAuth_Returns401`, `TestCreateComment_InvalidBody_IgnoresAuthorFields_Returns201WithTokenAuthor`, `TestCreateComment_MissingPost_Returns404`, `TestListComments_OK_ReturnsAuthorIDAndName`, `TestListComments_MissingPost_Returns404`.
- [ ] **Step 2: Run to verify failure.** Run: `cd app && go test ./internal/comments/...` — Expected: FAIL.
- [ ] **Step 3: Implement comments domain, MySQL adapter (`Create` inserts `post_id, author_id, author_name, content`; `ListByPost` selects with `WHERE post_id = ? ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?` + count), and handler (`POST /posts/:id/comments`, `GET /posts/:id/comments`; create behind `auth`, list public).**
- [ ] **Step 4: Run to verify pass.** Run: `cd app && go test ./internal/comments/... -v` — Expected: PASS.
- [ ] **Step 5: Review** — no commit (the requester owns git history).

### Task 8: Composition — router, main, health, graceful shutdown

**Files:**
- Create: `app/internal/httpapi/router.go`, `app/internal/httpapi/router_test.go`
- Rewrite: `app/cmd/api/main.go` (replaces the Hello World starter; preserve nothing but the `:8080` port)
- Delete: `app/main.go` (superseded by `cmd/api`)

**Interfaces:**
- Consumes: everything from Tasks 2–7 plus `config`, `database`.
- Produces:
  - `httpapi.NewRouter(h Handlers, auth gin.HandlerFunc) *gin.Engine` with `type Handlers struct { Identity *identityhttp.Handler; Posts *postshttp.Handler; Comments *commentshttp.Handler }` — wires exact public paths, `GET /healthz` (200 `{"data":{"status":"ok"}}`; 503 when `db.PingContext` fails), JSON 404 handler returning the error envelope, and `gin.Logger`/`gin.Recovery`.
  - `cmd/api/main.go` lifecycle: `config.Load` → `database.Open` → build services/adapters (manual DI, explicit and linear) → `httpapi.NewRouter` → `http.Server` with `ReadHeaderTimeout: 5s` → `signal.NotifyContext` for SIGINT/SIGTERM → graceful `srv.Shutdown(ctx)` with 15s drain → `db.Close()`. Startup failures exit non-zero with clear messages.

- [ ] **Step 1: Write failing httptest tests** (`router_test.go` builds the full router with real services over in-test fakes):
  - `TestRouter_RouteRegistration` (table): every required method+path returns non-404 for a well-formed request (or 401 where protected).
  - `TestHealthz_OK`.
  - `TestUnknownRoute_ReturnsErrorEnvelope` — JSON contains `error.code`.
  - `TestProtectedRoutes_MissingToken_Returns401` (table over POST /posts, PUT/DELETE /posts/1, POST /posts/1/comments).
- [ ] **Step 2: Run to verify failure.** Run: `cd app && go test ./internal/httpapi/...` — Expected: FAIL.
- [ ] **Step 3: Implement `router.go` and rewrite `cmd/api/main.go`; delete `app/main.go`.**
- [ ] **Step 4: Run to verify pass.** Run: `cd app && go test ./... -v` — Expected: PASS, full suite green.
- [ ] **Step 5: Review** — no commit (the requester owns git history).

### Task 9: Integration tests, Docker, environment

**Files:**
- Create: `app/tests/integration/integration_test.go` (env-gated: `TEST_MYSQL_DSN`, `t.Skip` when empty — no build tags)
- Rewrite: `Dockerfile` (multi-stage), `docker-compose.yml`
- Create: `.env.example` if not already complete (Task 1 creates it; here verify keys)
- Modify: `.gitignore` (add `.env`)

**Interfaces:**
- Consumes: all repos and `cmd/migrate`.
- Produces:
  - Integration tests: `TestIntegration_MigrateUp_Idempotent`, `TestIntegration_UserEmailUniqueness` (duplicate → `ErrEmailTaken`, Review Focus #5 DB path), `TestIntegration_PostFK_Enforced` (insert with unknown author → FK error), `TestIntegration_CommentCascadeDelete` (delete post → comments gone), `TestIntegration_PostPagination_Order` (seed 25 posts; page 1 returns 10 rows ordered `created_at DESC, id DESC`; total 25), `TestIntegration_UpdateNoChange_ReturnsPost` (Review Focus #2 DB path).
  - Dockerfile: stage 1 `golang:1.27-alpine` — build `./cmd/api` and `./cmd/migrate` to `/out/`; stage 2 slim runtime (e.g. `alpine:3.21`) copying binaries; `EXPOSE 8080`; default `CMD ["/out/api"]`.
  - docker-compose: `db` (mysql:8.0, `MYSQL_DATABASE=appdb`, credentials from env with dev defaults, healthcheck `mysqladmin ping --silent`, named volume `db_data`), `migrate` (same image, `command: ["/out/migrate"]`, `depends_on: db: condition: service_healthy`), `app` (`command: ["/out/api"]`, `depends_on: migrate: condition: service_completed_successfully`, ports `8080:8080`, env `DB_DSN`, `JWT_SECRET`, `JWT_TTL`, `APP_PORT`). `docker-compose build && docker-compose up` remains the documented workflow.
  - Note: the previous Air-based live-reload Dockerfile is replaced by the multi-stage build; manual dev uses `go run ./cmd/api` (documented in README).

- [ ] **Step 1: Write failing integration tests** (skip without `TEST_MYSQL_DSN`).
- [ ] **Step 2: Run them against a MySQL instance.** Run: `cd app && TEST_MYSQL_DSN='...' go test ./tests/integration/... -v` (DSN points at the compose MySQL on `localhost:3333`, or a locally started container). Expected: FAIL first where behavior is unimplemented, then PASS after fixes.
- [ ] **Step 3: Write Dockerfile + docker-compose.yml; verify `.env.example` keys** (`APP_PORT`, `DB_DSN`, `JWT_SECRET`, `JWT_TTL` — placeholders only).
- [ ] **Step 4: Build and boot the stack.** Run: `docker-compose build && docker-compose up -d` then `curl localhost:8080/healthz`. Expected: `{"data":{"status":"ok"}}`, `docker-compose ps` shows all three services healthy/completed.
- [ ] **Step 5: Review** — no commit (the requester owns git history).

### Task 10: README, final verification, end-to-end checklist

**Files:**
- Rewrite: `readme.md` (keep as README of record; include all 17 required sections: overview, architecture + dependency direction, modules, folder structure, stack, schema + relationships, endpoints, auth per endpoint, request/response examples, error codes, env setup, Docker instructions, manual setup, migration instructions, test commands, design decisions, scope limitations/future improvements)

**Interfaces:**
- Consumes: everything.
- Produces: README sections covering the required design decisions:
  - **Comment author relationship** — `author_id` added deliberately because comment creation requires authentication; `author_name` retained as a creation-time snapshot; later name changes do not rewrite history (accepted trade-off).
  - **Authentication policy** — reads public, writes authenticated (the original spec leaves comment auth open; documented decision).
  - **Ownership enforcement** — service check + `WHERE id=? AND author_id=?`; 403 vs 404 semantics.
  - **Pagination and indexes** — query patterns served by `idx_posts_created_id (created_at, id)` and `idx_comments_post_created_id (post_id, created_at, id)`.
  - **Architecture** — modular monolith + pragmatic hexagonal; why not microservices.
  - **Trade-offs** — interfaces reduce coupling but switching MySQL→MongoDB is not automatic (schema, queries, integrity testing all change).
  - **Go toolchain decision** — upgraded 1.21 → 1.27 (latest stable, supported) at requester's direction; upgrade is not dependency-driven; 1.21 is EOL.

- [ ] **Step 1: Write the README** per Produces.
- [ ] **Step 2: Static checks.** Run: `cd app && gofmt -w . && go vet ./... && go test ./... && go build ./...` — inspect the gofmt diff; Expected: all PASS.
- [ ] **Step 3: Docker end-to-end checklist** against `docker-compose up` (curl each; report actual results):
  1. Register → 201, no `password_hash` in response
  2. Login → 200, usable JWT
  3. POST /posts without token → 401
  4. POST /posts with token → 201
  5. GET /posts and GET /posts/{id} public → 200
  6. User B cannot PUT/DELETE user A's post → 403
  7. POST /posts/{id}/comments with token → 201 with `author_id` + `author_name` snapshot
  8. Comment without token → 401
  9. GET /posts/{id}/comments public → 200, `[]` when empty
  10. DELETE post → comments cascaded (GET comments after → 404 or empty per spec'd behavior)
  11. Duplicate register → 409
  12. No `password_hash`, SQL, or DSN text in any response
- [ ] **Step 4: Review** — no commit (the requester owns git history).

---

## Self-review notes

- **Spec coverage:** every section of the Implementation Prompt maps to a task (foundation §2/§6/§7/§13 → T1; identity §8 → T2–T4; posts §9 → T5–T6; comments §10 → T7; responses/errors §11 → T3; validation §12 → T2/T5/T7; testing §14 → T1–T9; Docker §15 → T9; config §16 → T1/T9; README §17 → T10; verification §19–§21 → T10 + final report). The one deliberate decision the spec leaves open — pagination out-of-range → 400 (vs clamping) — is fixed in Global Constraints.
- **Type consistency:** `TokenService`, `PostChecker`, `UserNameProvider`, `Repository` signatures above are the names later tasks reference; handler registration method names (`Register`, `RegisterAuth`) are consistent across T4/T6/T7/T8.
- **Review Focus:** all five failure modes are pinned by a named test in the owning task.
