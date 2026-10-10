package comments

import "context"

// PostChecker reports whether a post exists. It is satisfied by
// posts.Service.Exists; the interface is defined here so the comments module
// does not depend on the posts package.
type PostChecker interface {
	Exists(ctx context.Context, postID int64) (bool, error)
}

// UserNameProvider returns a user's display name for the author snapshot. It is
// satisfied by identity.Service.UserName.
type UserNameProvider interface {
	UserName(ctx context.Context, userID int64) (string, error)
}

// Repository is the persistence port for comments. The MySQL adapter implements
// it; the service depends only on this interface.
//
// Contract: ListByPost returns the comments of one post plus the total count,
// and an empty (non-nil) slice when there are none.
type Repository interface {
	Create(ctx context.Context, c Comment) (Comment, error)
	ListByPost(ctx context.Context, postID int64, page, limit int) ([]Comment, int64, error)
}
