package posts

import "context"

// Repository is the persistence port for posts. The MySQL adapter implements
// it; the service depends only on this interface.
//
// Contract: GetByID returns ErrNotFound when no such post exists. Update and
// Delete only touch a row matching both id and authorID; a missing row is
// ErrNotFound.
type Repository interface {
	Create(ctx context.Context, authorID int64, title, content string) (Post, error)
	GetByID(ctx context.Context, id int64) (Post, error)
	List(ctx context.Context, page, limit int) ([]Post, int64, error)
	Update(ctx context.Context, id, authorID int64, title, content string) (Post, error)
	Delete(ctx context.Context, id, authorID int64) error
}
