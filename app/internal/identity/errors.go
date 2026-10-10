package identity

import (
	"errors"
	"fmt"
)

// Business-condition errors. The HTTP layer maps these to status codes; the
// service never returns a transport-specific error.
var (
	// ErrEmailTaken signals a duplicate registration (HTTP 409).
	ErrEmailTaken = errors.New("identity: email already registered")
	// ErrInvalidCredentials covers both unknown email and wrong password so
	// callers cannot enumerate accounts (HTTP 401).
	ErrInvalidCredentials = errors.New("identity: invalid credentials")
	// ErrUserNotFound signals a missing user (HTTP 404).
	ErrUserNotFound = errors.New("identity: user not found")
	// ErrInvalidInput signals a validation failure (HTTP 400).
	ErrInvalidInput = errors.New("identity: invalid input")
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
