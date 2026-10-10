// Package identitymysql is the MySQL adapter for the identity module's
// UserRepository port. It is the only place in the module that knows about SQL
// or specific driver error codes.
package identitymysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"app/internal/identity"

	"github.com/go-sql-driver/mysql"
)

// mysqlDuplicateEntry is the MySQL error number for a unique-key violation.
const mysqlDuplicateEntry = 1062

// Repository stores users in MySQL. It implements identity.UserRepository.
type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

const userColumns = "id, name, email, password_hash, created_at, updated_at"

// Create inserts a user and returns the persisted row (with server-generated
// id/timestamps). A duplicate email is mapped to identity.ErrEmailTaken.
func (r *Repository) Create(ctx context.Context, name, email, passwordHash string) (identity.User, error) {
	res, err := r.db.ExecContext(ctx,
		"INSERT INTO users (name, email, password_hash) VALUES (?, ?, ?)",
		name, email, passwordHash,
	)
	if err != nil {
		return identity.User{}, mapError(err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return identity.User{}, fmt.Errorf("identity: last insert id: %w", err)
	}

	return r.GetByID(ctx, id)
}

func (r *Repository) GetByEmail(ctx context.Context, email string) (identity.User, error) {
	return scanUser(r.db.QueryRowContext(ctx,
		"SELECT "+userColumns+" FROM users WHERE email = ?", email,
	))
}

func (r *Repository) GetByID(ctx context.Context, id int64) (identity.User, error) {
	return scanUser(r.db.QueryRowContext(ctx,
		"SELECT "+userColumns+" FROM users WHERE id = ?", id,
	))
}

func scanUser(row *sql.Row) (identity.User, error) {
	var u identity.User
	if err := row.Scan(&u.ID, &u.Name, &u.Email, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt); err != nil {
		return identity.User{}, mapError(err)
	}
	return u, nil
}

// mapError translates driver-level failures into Identity domain errors.
//
// The database unique constraint is the final guarantee against duplicate
// emails, including concurrent registrations. MySQL error 1062 is mapped to
// identity.ErrEmailTaken so callers can return a conflict response.
//
// Unrecognized errors are preserved for higher layers to handle.
func mapError(err error) error {
	if err == nil {
		return nil
	}

	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == mysqlDuplicateEntry {
		return identity.ErrEmailTaken
	}
	if errors.Is(err, sql.ErrNoRows) {
		return identity.ErrUserNotFound
	}
	return err
}
