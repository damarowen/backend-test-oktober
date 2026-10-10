// Package database provides the shared MySQL connection pool used by the
// application's repository adapters.
package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	// Registers the "mysql" driver with database/sql.
	_ "github.com/go-sql-driver/mysql"
)

const (
	maxOpenConns    = 25
	maxIdleConns    = 10
	connMaxLifetime = 5 * time.Minute
)

// Open creates a MySQL connection pool, applies bounded pool settings, and
// verifies connectivity before returning. The caller owns the returned *sql.DB
// and must Close it on shutdown.
func Open(ctx context.Context, dsn string) (*sql.DB, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("database: open: %w", err)
	}

	db.SetMaxOpenConns(maxOpenConns)
	db.SetMaxIdleConns(maxIdleConns)
	db.SetConnMaxLifetime(connMaxLifetime)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("database: ping: %w", err)
	}

	return db, nil
}
