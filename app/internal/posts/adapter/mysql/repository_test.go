package postsmysql

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"app/internal/posts"

	"github.com/go-sql-driver/mysql"
)

// TestRepository_MapError pins how driver-level failures are translated: only
// "no rows" becomes a business condition. Everything else keeps its identity,
// so an outage surfaces as 500 and a foreign-key violation (an insert with a
// bad author_id) is never misreported as a missing post.
func TestRepository_MapError(t *testing.T) {
	fkErr := &mysql.MySQLError{Number: 1452, Message: "Cannot add or update a child row: a foreign key constraint fails"}
	networkErr := errors.New("dial tcp 127.0.0.1:3306: connect: connection refused")

	tests := []struct {
		name   string
		in     error
		wantIs error // business sentinel the result must match, when set
		want   error // exact error the result must be, when set
	}{
		{"no rows maps to ErrNotFound", sql.ErrNoRows, posts.ErrNotFound, nil},
		{"wrapped no rows maps to ErrNotFound", fmt.Errorf("posts: scan: %w", sql.ErrNoRows), posts.ErrNotFound, nil},
		{"foreign key error passes through", fkErr, nil, fkErr},
		{"network error passes through", networkErr, nil, networkErr},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapError(tt.in)

			if tt.want != nil {
				if got != tt.want {
					t.Errorf("mapError() = %v, want the same error unchanged", got)
				}
				return
			}
			if !errors.Is(got, tt.wantIs) {
				t.Errorf("mapError() = %v, want %v", got, tt.wantIs)
			}
		})
	}
}
