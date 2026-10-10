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
	cd app && air main.go

# Stop the application
down:
	docker-compose down

# Restart the application
restart: down up

# View application logs
logs:
	docker-compose logs -f

# Run Go tests
test:
	cd app && go test -v ./...

# Clean build artifacts and volumes
clean:
	docker-compose down -v
	docker system prune -f

# Run database migrations
migrate:
	@echo "Run migrations here (e.g., mysql -h 127.0.0.1 -P 3333 -u root -pabc123 appdb < migrations/init.sql)"
