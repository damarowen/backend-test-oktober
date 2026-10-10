package posts

import "time"

// Post is a blog post authored by a user. AuthorID is set from the
// authenticated user at creation and never changed afterwards.
type Post struct {
	ID        int64
	AuthorID  int64
	Title     string
	Content   string
	CreatedAt time.Time
	UpdatedAt time.Time
}
