// Package commentsmysql is the MySQL adapter for the comments module's
// Repository port. It is the only place in the module that knows about SQL or
// driver error codes.
package commentsmysql

import (
	"context"
	"database/sql"
	"errors"

	"app/internal/comments"

	"github.com/go-sql-driver/mysql"
)

// mysqlForeignKeyViolation is the MySQL error number for a failed foreign-key
// constraint. For this table the only possible violation is fk_comments_post
// (the post vanished between the service's check and the insert), because there
// is no endpoint that deletes users.
const mysqlForeignKeyViolation = 1452

// Repository stores comments in MySQL. It implements comments.Repository.
type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

const commentColumns = "id, post_id, author_id, author_name, content, created_at"

// Create inserts a comment and returns the persisted row, with the
// server-generated id and creation time.
func (r *Repository) Create(ctx context.Context, c comments.Comment) (comments.Comment, error) {
	res, err := r.db.ExecContext(ctx,
		"INSERT INTO comments (post_id, author_id, author_name, content) VALUES (?, ?, ?, ?)",
		c.PostID, c.AuthorID, c.AuthorName, c.Content,
	)
	if err != nil {
		return comments.Comment{}, mapError(err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return comments.Comment{}, err
	}
	return scanComment(r.db.QueryRowContext(ctx,
		"SELECT "+commentColumns+" FROM comments WHERE id = ?", id,
	))
}

// ListByPost returns one page of a post's comments, newest first, plus the
// total count for that post.
func (r *Repository) ListByPost(ctx context.Context, postID int64, page, limit int) ([]comments.Comment, int64, error) {
	var total int64
	if err := r.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM comments WHERE post_id = ?", postID,
	).Scan(&total); err != nil {
		return nil, 0, mapError(err)
	}

	rows, err := r.db.QueryContext(ctx,
		"SELECT "+commentColumns+" FROM comments WHERE post_id = ? ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?",
		postID, limit, (page-1)*limit,
	)
	if err != nil {
		return nil, 0, mapError(err)
	}
	defer rows.Close()

	list := make([]comments.Comment, 0, limit)
	for rows.Next() {
		var c comments.Comment
		if err := rows.Scan(&c.ID, &c.PostID, &c.AuthorID, &c.AuthorName, &c.Content, &c.CreatedAt); err != nil {
			return nil, 0, mapError(err)
		}
		list = append(list, c)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, mapError(err)
	}
	return list, total, nil
}

func scanComment(row *sql.Row) (comments.Comment, error) {
	var c comments.Comment
	if err := row.Scan(&c.ID, &c.PostID, &c.AuthorID, &c.AuthorName, &c.Content, &c.CreatedAt); err != nil {
		return comments.Comment{}, mapError(err)
	}
	return c, nil
}

// mapError translates driver-level failures into domain errors. The post
// foreign-key violation becomes ErrPostNotFound; every other error keeps its
// identity, so an outage surfaces as 500 and is never misreported as a
// business condition.
func mapError(err error) error {
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == mysqlForeignKeyViolation {
		return comments.ErrPostNotFound
	}
	return err
}
