package comments

import "time"

// Comment is a comment on a post. AuthorName is a creation-time snapshot taken
// from the identity module; later profile changes never rewrite it. There is no
// update endpoint, so there is no UpdatedAt.
type Comment struct {
	ID         int64
	PostID     int64
	AuthorID   int64
	AuthorName string
	Content    string
	CreatedAt  time.Time
}
