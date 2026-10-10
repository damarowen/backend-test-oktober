package posts

import (
	"errors"
	"fmt"
)

// Business-condition errors. The HTTP layer maps these to status codes; the
// service never returns a transport-specific error.
var (
	// ErrNotFound signals a missing post (HTTP 404).
	ErrNotFound = errors.New("posts: post not found")
	// ErrForbidden signals that the post exists but belongs to another user
	// (HTTP 403). It is deliberately distinct from ErrNotFound so an
	// authenticated user gets 403 — not 404 — when touching someone else's post.
	ErrForbidden = errors.New("posts: forbidden")
	// ErrInvalidInput signals a validation failure (HTTP 400).
	ErrInvalidInput = errors.New("posts: invalid input")
)

// ValidationError carries a user-safe message describing what to fix. It
// unwraps to ErrInvalidInput, so errors.Is(err, ErrInvalidInput) still holds,
// while the HTTP layer can surface Message to the client verbatim.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func (e *ValidationError) Unwrap() error { return ErrInvalidInput }

// invalidf builds a ValidationError with a user-facing message. The wording
// must stay end-user friendly: no byte units, no internal detail.
func invalidf(format string, args ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}
