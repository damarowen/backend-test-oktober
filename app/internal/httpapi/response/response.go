// Package response defines the API's uniform JSON envelope for data,
// paginated lists, and errors, and maps business errors to HTTP status codes.
package response

import (
	"errors"
	"net/http"

	"app/internal/comments"
	"app/internal/identity"
	"app/internal/posts"

	"github.com/gin-gonic/gin"
)

// Error codes exposed in the error envelope.
const (
	CodeValidation   = "VALIDATION_ERROR"
	CodeUnauthorized = "UNAUTHORIZED"
	CodeForbidden    = "FORBIDDEN"
	CodeNotFound     = "NOT_FOUND"
	CodeConflict     = "CONFLICT"
	CodeInternal     = "INTERNAL_ERROR"
)

// Generic messages for conditions that must not leak internal detail.
const (
	MsgValidation   = "The request is invalid."
	MsgUnauthorized = "Authentication is required."
	MsgInvalidCredentials= "Invalid credentials, please check email or password and try again."
	MsgForbidden = "You do not have permission to perform this action."
	MsgNotFound  = "The requested resource was not found."
	MsgConflict  = "The request conflicts with the current state."
	MsgInternal  = "An internal error occurred."
)

type dataBody struct {
	Data any `json:"data"`
}

type paginatedBody struct {
	Data       any            `json:"data"`
	Pagination paginationMeta `json:"pagination"`
}

type paginationMeta struct {
	Page  int   `json:"page"`
	Limit int   `json:"limit"`
	Total int64 `json:"total"`
}

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Data writes a single-resource success envelope.
func Data(c *gin.Context, status int, data any) {
	c.JSON(status, dataBody{Data: data})
}

// Paginated writes a list envelope with pagination metadata.
func Paginated(c *gin.Context, data any, page, limit int, total int64) {
	c.JSON(http.StatusOK, paginatedBody{
		Data:       data,
		Pagination: paginationMeta{Page: page, Limit: limit, Total: total},
	})
}

// APIError writes an error envelope.
func APIError(c *gin.Context, status int, code, message string) {
	c.JSON(status, errorBody{Error: errorDetail{Code: code, Message: message}})
}

// FromError maps a business error to its HTTP response. It returns true when
// err matched a known business condition. Unhandled errors still get a generic
// 500 response, and the raw error text is never exposed.
func FromError(c *gin.Context, err error) bool {
	switch {
	case errors.Is(err, identity.ErrInvalidInput), errors.Is(err, posts.ErrInvalidInput), errors.Is(err, comments.ErrInvalidInput):
		APIError(c, http.StatusBadRequest, CodeValidation, validationMessage(err))
		return true
	case errors.Is(err, identity.ErrInvalidCredentials):
		APIError(c, http.StatusUnauthorized, CodeUnauthorized, MsgInvalidCredentials)
		return true
	case errors.Is(err, posts.ErrForbidden):
		APIError(c, http.StatusForbidden, CodeForbidden, MsgForbidden)
		return true
	case errors.Is(err, identity.ErrUserNotFound), errors.Is(err, posts.ErrNotFound),
		errors.Is(err, comments.ErrPostNotFound), errors.Is(err, comments.ErrAuthorNotFound):
		APIError(c, http.StatusNotFound, CodeNotFound, MsgNotFound)
		return true
	case errors.Is(err, identity.ErrEmailTaken):
		APIError(c, http.StatusConflict, CodeConflict, MsgConflict)
		return true
	default:
		APIError(c, http.StatusInternalServerError, CodeInternal, MsgInternal)
		return false
	}
}

// validationMessage returns the user-safe text of a ValidationError from any
// module, or the generic message when the error carries no user-facing wording.
func validationMessage(err error) string {
	var identityErr *identity.ValidationError
	if errors.As(err, &identityErr) && identityErr.Message != "" {
		return identityErr.Message
	}

	var postsErr *posts.ValidationError
	if errors.As(err, &postsErr) && postsErr.Message != "" {
		return postsErr.Message
	}

	var commentsErr *comments.ValidationError
	if errors.As(err, &commentsErr) && commentsErr.Message != "" {
		return commentsErr.Message
	}

	return MsgValidation
}
