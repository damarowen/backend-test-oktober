package commentsmysql

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"app/internal/comments"

	"github.com/go-sql-driver/mysql"
)

// TestRepository_MapError pins the narrow translation contract: only the post
// foreign-key violation (1452) — which a lost race between the service's
// existence check and the insert can trigger — becomes a business condition.
// Everything else keeps its identity, so an outage surfaces as 500 and is never
// misreported as a missing post.
func TestRepository_MapError(t *testing.T) {
	fkErr := &mysql.MySQLError{Number: 1452, Message: "Cannot add or update a child row: a foreign key constraint fails (`appdb`.`comments`, CONSTRAINT `fk_comments_post` FOREIGN KEY (`post_id`) REFERENCES `posts` (`id`))"}
	otherMySQL := &mysql.MySQLError{Number: 1045, Message: "Access denied for user 'app'@'%'"}
	networkErr := errors.New("dial tcp 127.0.0.1:3306: connect: connection refused")

	tests := []struct {
		name   string
		in     error
		wantIs error // business sentinel the result must match, when set
		want   error // exact error the result must be, when set
	}{
		{"post FK violation maps to ErrPostNotFound", fkErr, comments.ErrPostNotFound, nil},
		{"wrapped FK violation maps to ErrPostNotFound", fmt.Errorf("comments: insert: %w", fkErr), comments.ErrPostNotFound, nil},
		{"other mysql error passes through", otherMySQL, nil, otherMySQL},
		{"network error passes through", networkErr, nil, networkErr},
		{"no rows passes through", sql.ErrNoRows, nil, sql.ErrNoRows},
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
