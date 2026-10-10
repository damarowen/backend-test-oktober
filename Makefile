.PHONY: all build up start down restart logs test test-integration clean migrate

# Default target
all: build up

# Host-side commands read the shared development .env. Values are read
# directly with surrounding quotes stripped: a DSN containing '&' is never
# shell-split, and quoted values (common in .env files) still work.
DB_DSN ?= $(shell sed -n 's/^DB_DSN=//p' .env 2>/dev/null | head -n1 | tr -d '"' | tr -d "'")
MIGRATIONS_DIR ?= $(shell sed -n 's/^MIGRATIONS_DIR=//p' .env 2>/dev/null | head -n1 | tr -d '"' | tr -d "'")
export DB_DSN MIGRATIONS_DIR

# Build the Docker containers
build:
	docker-compose build

# Start the application
up:
	docker-compose up -d

# Run the API on the host (loads the root .env via godotenv)
start:
	cd app && go run ./cmd/api

# Stop the application
down:
	docker-compose down

# Restart the application
restart: down up

# Follow the application logs
logs:
	docker-compose logs -f app

# Run Go tests (integration tests skip without TEST_MYSQL_DSN)
test:
	cd app && go test -v ./...

# Run the MySQL-backed integration tests against the compose database
test-integration:
	cd app && TEST_MYSQL_DSN='root:abc123@tcp(127.0.0.1:3333)/appdb?parseTime=true&charset=utf8mb4' go test -count=1 ./tests/integration/... -v

# Clean build artifacts and volumes (this project only)
clean:
	docker-compose down -v
	docker image rm blog-api:latest 2>/dev/null || true

# Run database migrations (DB_DSN comes from the root .env)
migrate:
	cd app && go run ./cmd/migrate
