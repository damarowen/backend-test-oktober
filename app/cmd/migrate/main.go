// Command migrate applies the versioned SQL migrations in app/migrations to the
// configured MySQL database. It is up-only and idempotent: applied versions are
// recorded in schema_migrations and skipped on subsequent runs. The .down.sql
// files are provided for manual rollback.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"app/internal/platform/database"
)

const (
	migrationsDirEnv     = "MIGRATIONS_DIR"
	defaultMigrationsDir = "migrations"
	upSuffix             = ".up.sql"
	migrateTimeout       = 60 * time.Second
)

const createSchemaMigrations = `CREATE TABLE IF NOT EXISTS schema_migrations (
    version    VARCHAR(32) NOT NULL,
    applied_at TIMESTAMP   NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (version)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "migrate: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("migrate: all migrations applied")
}

func run() error {
	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		return fmt.Errorf("DB_DSN is required")
	}

	dir := os.Getenv(migrationsDirEnv)
	if dir == "" {
		dir = defaultMigrationsDir
	}

	files, err := filepath.Glob(filepath.Join(dir, "*"+upSuffix))
	if err != nil {
		return fmt.Errorf("scan migrations dir %q: %w", dir, err)
	}
	if len(files) == 0 {
		return fmt.Errorf("no %s files found in %q", upSuffix, dir)
	}
	sort.Strings(files)

	ctx, cancel := context.WithTimeout(context.Background(), migrateTimeout)
	defer cancel()

	db, err := database.Open(ctx, dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	if _, err := db.ExecContext(ctx, createSchemaMigrations); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	applied, err := appliedVersions(ctx, db)
	if err != nil {
		return err
	}

	for _, file := range files {
		version := strings.TrimSuffix(filepath.Base(file), upSuffix)
		if applied[version] {
			fmt.Printf("migrate: %s already applied, skipping\n", version)
			continue
		}

		stmt, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("read %s: %w", file, err)
		}
		if _, err := db.ExecContext(ctx, string(stmt)); err != nil {
			return fmt.Errorf("apply %s: %w", version, err)
		}
		if _, err := db.ExecContext(ctx, "INSERT INTO schema_migrations (version) VALUES (?)", version); err != nil {
			return fmt.Errorf("record %s: %w", version, err)
		}
		fmt.Printf("migrate: applied %s\n", version)
	}

	return nil
}

func appliedVersions(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("read schema_migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[string]bool)
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("scan schema_migrations: %w", err)
		}
		applied[version] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schema_migrations: %w", err)
	}
	return applied, nil
}
