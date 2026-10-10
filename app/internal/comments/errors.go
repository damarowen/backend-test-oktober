package comments

import (
	"errors"
	"fmt"
)

// Business-condition errors. The HTTP layer maps these to status codes; the
// service never returns a transport-specific error.
var (
	// ErrPostNotFound signals that the post a comment targets does not exist
	// (HTTP 404). It also covers the lost insert race: the post is deleted
	// between the service's existence check and the insert, and the adapter
	// maps the resulting foreign-key violation to this sentinel.
	ErrPostNotFound = errors.New("comments: post not found")
	// ErrInvalidInput signals a validation failure (HTTP 400).
	ErrInvalidInput = errors.New("comments: invalid input")
	// ErrAuthorNotFound signals that the authenticated user's row is missing
	// (HTTP 404) — the token outlived the account.
	ErrAuthorNotFound = errors.New("comments: author not found")
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
