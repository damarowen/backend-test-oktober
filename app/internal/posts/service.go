package posts

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"
)

// Validation and pagination limits. They mirror the Global Constraints: title
// trimmed non-empty ≤ 255 characters, content non-empty ≤ 65,535 bytes (fits
// the TEXT column), pagination defaults 1/10 with an explicit limit ceiling.
const (
	maxTitleLength   = 255
	maxContentLength = 65535
	defaultPage      = 1
	defaultLimit     = 10
	maxLimit         = 100
)

// Service implements post CRUD, pagination, and ownership rules. It depends
// only on the Repository port.
type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// Create validates the input and stores the post under the authenticated
// userID. The caller has no way to pass another author: the signature takes the
// ID that the auth middleware resolved.
func (s *Service) Create(ctx context.Context, userID int64, title, content string) (Post, error) {
	title = strings.TrimSpace(title)
	if err := validatePost(title, content); err != nil {
		return Post{}, err
	}
	return s.repo.Create(ctx, userID, title, content)
}

// Get returns a single post. A missing post surfaces as ErrNotFound from the
// repository.
func (s *Service) Get(ctx context.Context, id int64) (Post, error) {
	return s.repo.GetByID(ctx, id)
}

// List returns one page of posts. Unset pagination (zero values) falls back to
// page 1 / limit 10; values out of range are a validation error.
func (s *Service) List(ctx context.Context, page, limit int) ([]Post, int64, error) {
	page, limit, err := normalizePagination(page, limit)
	if err != nil {
		return nil, 0, err
	}
	return s.repo.List(ctx, page, limit)
}

// Update replaces title and content of an existing post. The service checks
// ownership before touching the row: a missing post is ErrNotFound, a post
// owned by another user is ErrForbidden. The repository repeats the
// `WHERE id = ? AND author_id = ?` guard as the real integrity backstop.
func (s *Service) Update(ctx context.Context, userID, id int64, title, content string) (Post, error) {
	title = strings.TrimSpace(title)
	if err := validatePost(title, content); err != nil {
		return Post{}, err
	}

	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Post{}, ErrNotFound
		}
		return Post{}, err
	}
	if existing.AuthorID != userID {
		return Post{}, ErrForbidden
	}

	return s.repo.Update(ctx, id, userID, title, content)
}

// Delete removes an existing post. Ownership follows the same rules as Update.
func (s *Service) Delete(ctx context.Context, userID, id int64) error {
	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if existing.AuthorID != userID {
		return ErrForbidden
	}
	return s.repo.Delete(ctx, id, userID)
}

// Exists reports whether a post exists. It satisfies the comments module's
// PostChecker port; infrastructure errors propagate so a database outage is
// never misread as a missing post.
func (s *Service) Exists(ctx context.Context, id int64) (bool, error) {
	_, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
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
	return page, limit, nil
}

// validatePost expects an already-trimmed title. Content is stored verbatim:
// leading and trailing whitespace inside a post body is meaningful.
func validatePost(title, content string) error {
	if title == "" {
		return invalidf("Title is required.")
	}
	if utf8.RuneCountInString(title) > maxTitleLength {
		return invalidf("Title must be at most %d characters.", maxTitleLength)
	}
	if content == "" {
		return invalidf("Content is required.")
	}
	// The storage limit is measured in bytes (TEXT holds 65,535), but the
	// message stays unit-free per the user-facing wording rule.
	if len(content) > maxContentLength {
		return invalidf("Content is too long.")
	}
	return nil
}
