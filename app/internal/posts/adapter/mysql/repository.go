// Package postsmysql is the MySQL adapter for the posts module's Repository
// port. It is the only place in the module that knows about SQL.
package postsmysql

import (
	"context"
	"database/sql"
	"errors"

	"app/internal/posts"
)

// Repository stores posts in MySQL. It implements posts.Repository.
type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

const postColumns = "id, title, content, author_id, created_at, updated_at"

// Create inserts a post for authorID and returns the persisted row, with the
// server-generated id and timestamps.
func (r *Repository) Create(ctx context.Context, authorID int64, title, content string) (posts.Post, error) {
	res, err := r.db.ExecContext(ctx,
		"INSERT INTO posts (title, content, author_id) VALUES (?, ?, ?)",
		title, content, authorID,
	)
	if err != nil {
		return posts.Post{}, mapError(err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return posts.Post{}, err
	}
	return r.GetByID(ctx, id)
}

func (r *Repository) GetByID(ctx context.Context, id int64) (posts.Post, error) {
	return scanPost(r.db.QueryRowContext(ctx,
		"SELECT "+postColumns+" FROM posts WHERE id = ?", id,
	))
}

// getByIDAndAuthor selects a post only when it belongs to authorID. It backs
// the update read-back, so a caller can never observe another author's row.
func (r *Repository) getByIDAndAuthor(ctx context.Context, id, authorID int64) (posts.Post, error) {
	return scanPost(r.db.QueryRowContext(ctx,
		"SELECT "+postColumns+" FROM posts WHERE id = ? AND author_id = ?", id, authorID,
	))
}

// List returns one page of posts, newest first, plus the total row count.
func (r *Repository) List(ctx context.Context, page, limit int) ([]posts.Post, int64, error) {
	var total int64
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM posts").Scan(&total); err != nil {
		return nil, 0, mapError(err)
	}

	rows, err := r.db.QueryContext(ctx,
		"SELECT "+postColumns+" FROM posts ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?",
		limit, (page-1)*limit,
	)
	if err != nil {
		return nil, 0, mapError(err)
	}
	defer rows.Close()

	list := make([]posts.Post, 0, limit)
	for rows.Next() {
		var p posts.Post
		if err := rows.Scan(&p.ID, &p.Title, &p.Content, &p.AuthorID, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, 0, mapError(err)
		}
		list = append(list, p)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, mapError(err)
	}
	return list, total, nil
}

// Update replaces title and content of a post owned by authorID.
//
// The WHERE guard scopes the write to the author. Whether or not the values
// actually changed (MySQL reports 0 affected rows for a no-op update and does
// not error), the author-scoped read-back decides the outcome: it returns the
// current row — so an identical-values update still succeeds with 200 — or
// ErrNotFound for a missing post. It can never return a post belonging to a
// different author. clientFoundRows is deliberately not required.
func (r *Repository) Update(ctx context.Context, id, authorID int64, title, content string) (posts.Post, error) {
	if _, err := r.db.ExecContext(ctx,
		"UPDATE posts SET title = ?, content = ? WHERE id = ? AND author_id = ?",
		title, content, id, authorID,
	); err != nil {
		return posts.Post{}, mapError(err)
	}
	return r.getByIDAndAuthor(ctx, id, authorID)
}

// Delete removes a post owned by authorID. A delete matching no row is not a
// driver error, so the affected-row count must be inspected: returning nil
// here would report success for a row that was never removed.
func (r *Repository) Delete(ctx context.Context, id, authorID int64) error {
	res, err := r.db.ExecContext(ctx,
		"DELETE FROM posts WHERE id = ? AND author_id = ?", id, authorID,
	)
	if err != nil {
		return mapError(err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return posts.ErrNotFound
	}
	return nil
}

func scanPost(row *sql.Row) (posts.Post, error) {
	var p posts.Post
	if err := row.Scan(&p.ID, &p.Title, &p.Content, &p.AuthorID, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return posts.Post{}, mapError(err)
	}
	return p, nil
}

// mapError translates driver-level failures into domain errors. Only "no rows"
// is a business condition; everything else keeps its identity, so an outage
// surfaces as 500 and a foreign-key violation (an insert with a bad author_id)
// is never misreported as a missing post.
func mapError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return posts.ErrNotFound
	}
	return err
}
