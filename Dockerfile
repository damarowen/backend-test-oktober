# syntax=docker/dockerfile:1

# --- build stage -------------------------------------------------------------
FROM golang:1.27-alpine AS build

WORKDIR /src

# Module dependencies in their own layer, so source edits do not refetch them.
COPY app/go.mod app/go.sum ./
RUN go mod download

COPY app/ ./

# Create 2  binaries: the runtime image is a bare Alpine with no libc toolchain.
RUN CGO_ENABLED=0 go build -o /out/api ./cmd/api \
 && CGO_ENABLED=0 go build -o /out/migrate ./cmd/migrate

# --- runtime stage -----------------------------------------------------------
FROM alpine:3.21

# Gin's warning-free production mode; cmd/migrate reads the SQL files from
# MIGRATIONS_DIR relative to WORKDIR.
ENV GIN_MODE=release \
    MIGRATIONS_DIR=/app/migrations

WORKDIR /app

COPY --from=build /out/api /out/api
COPY --from=build /out/migrate /out/migrate
COPY --from=build /src/migrations /app/migrations

EXPOSE 8080

CMD ["/out/api"]
