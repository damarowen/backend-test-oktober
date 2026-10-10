package comments

import (
	"context"
	"errors"
	"math"
	"strings"

	"app/internal/identity"
)

// Validation and pagination limits. Content is stored in TEXT (65,535 bytes);
// pagination mirrors the posts module (defaults 1/10, ceiling 100).
const (
	maxContentLength = 65535
	defaultPage      = 1
	defaultLimit     = 10
	maxLimit         = 100
)

// Service implements comment creation and listing. It depends on its own
// Repository port and on two minimal provider ports, so it stays independent of
// the posts and identity packages' services.
type Service struct {
	repo  Repository
	posts PostChecker
	users UserNameProvider
}

func NewService(repo Repository, posts PostChecker, users UserNameProvider) *Service {
	return &Service{repo: repo, posts: posts, users: users}
}

// Create validates the content, verifies the post exists, snapshots the
// author's display name, and stores the comment. The author identity always
// comes from the caller's arguments (the authenticated user), never from the
// comment body.
func (s *Service) Create(ctx context.Context, postID, authorID int64, content string) (Comment, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return Comment{}, invalidf("Content is required.")
	}
	// The storage limit is measured in bytes, but the message stays unit-free
	// per the user-facing wording rule.
	if len(content) > maxContentLength {
		return Comment{}, invalidf("Content is too long.")
	}

	exists, err := s.posts.Exists(ctx, postID)
	if err != nil {
		return Comment{}, err
	}
	if !exists {
		return Comment{}, ErrPostNotFound
	}

	name, err := s.users.UserName(ctx, authorID)
	if err != nil {
		// Only a genuinely missing user row is a 404; anything else is an
		// infrastructure failure and must not be masked as "author not found".
		if errors.Is(err, identity.ErrUserNotFound) {
			return Comment{}, ErrAuthorNotFound
		}
		return Comment{}, err
	}

	return s.repo.Create(ctx, Comment{
		PostID:     postID,
		AuthorID:   authorID,
		AuthorName: name,
		Content:    content,
	})
}

// ListByPost returns one page of a post's comments. A missing post is
// ErrPostNotFound; unset pagination (zero values) falls back to page 1 /
// limit 10; values out of range are a validation error.
func (s *Service) ListByPost(ctx context.Context, postID int64, page, limit int) ([]Comment, int64, error) {
	page, limit, err := normalizePagination(page, limit)
	if err != nil {
		return nil, 0, err
	}

	exists, err := s.posts.Exists(ctx, postID)
	if err != nil {
		return nil, 0, err
	}
	if !exists {
		return nil, 0, ErrPostNotFound
	}

	return s.repo.ListByPost(ctx, postID, page, limit)
}

// normalizePagination applies the defaults and rejects explicit out-of-range
// values. Zero means "not supplied" and is the only value that defaults.
func normalizePagination(page, limit int) (int, int, error) {
	if page == 0 {
		page = defaultPage
	} else if page < 0 {
		return 0, 0, invalidf("Page must be at least %d.", defaultPage)
	}

	if limit == 0 {
		limit = defaultLimit
	} else if limit < 0 || limit > maxLimit {
		return 0, 0, invalidf("Limit must be between 1 and %d.", maxLimit)
	}

	// Keep (page-1)*limit within int64: the adapter passes the offset to
	// MySQL, and an overflowed OFFSET would surface as an internal error
	// instead of a client error.
	if int64(page-1) > math.MaxInt64/int64(limit) {
		return 0, 0, invalidf("Page is too large.")
	}
	return page, limit, nil
}
