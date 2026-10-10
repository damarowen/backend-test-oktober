package identitymysql

import (
	"database/sql"
	"errors"
	"testing"

	"app/internal/identity"

	"github.com/go-sql-driver/mysql"
)

func TestRepository_MapError_DuplicateEmail(t *testing.T) {
	err := mapError(&mysql.MySQLError{
		Number:  1062,
		Message: "Duplicate entry 'ada@example.com' for key 'uq_users_email'",
	})

	if !errors.Is(err, identity.ErrEmailTaken) {
		t.Errorf("mapError(1062) = %v, want ErrEmailTaken", err)
	}
}

func TestRepository_MapError_NoRows(t *testing.T) {
	err := mapError(sql.ErrNoRows)

	if !errors.Is(err, identity.ErrUserNotFound) {
		t.Errorf("mapError(sql.ErrNoRows) = %v, want ErrUserNotFound", err)
	}
}

func TestRepository_MapError_Other(t *testing.T) {
	// A foreign-key failure (1452) must surface as an error, not be mistaken
	// for a business condition.
	fk := &mysql.MySQLError{Number: 1452, Message: "Cannot add or update a child row"}
	if got := mapError(fk); got != error(fk) {
		t.Errorf("mapError(1452) = %v, want it returned unchanged", got)
	}

	// An infrastructure failure must stay a 500, not become a 404/409.
	outage := errors.New("dial tcp 127.0.0.1:3306: connect: connection refused")
	if got := mapError(outage); got != error(outage) {
		t.Errorf("mapError(outage) = %v, want it returned unchanged", got)
	}
}
