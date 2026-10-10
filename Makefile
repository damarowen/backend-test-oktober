.PHONY: build up down restart logs test clean migrate

# Default target
all: build up

# Build the Docker containers
build:
	docker-compose build

# Start the application
up:
	docker-compose up -d

# Start the application with Air live-reload
start:
	cd app/cmd/api && go run main.go

# Stop the application
down:
	docker-compose down

# Restart the application
restart: down up

# Run Go tests
test:
	cd app && go test -v ./...

# Clean build artifacts and volumes
clean:
	docker-compose down -v
	docker system prune -f

# Run database migrations
migrate:
	cd app && go run ./cmd/migrate