// Package middleware holds the HTTP middleware shared by the API routes.
package middleware

import (
	"context"
	"net/http"
	"strings"

	"app/internal/httpapi/response"
	"app/internal/identity"

	"github.com/gin-gonic/gin"
)

// userIDKey is an unexported context key type, so nothing outside this package
// can read or forge the authenticated identity.
type userIDKey struct{}

// Auth authenticates a request from an "Authorization: Bearer <token>" header
// and stores the verified user ID in the request context.
//
// Every rejection path aborts the Gin chain before returning: without
// c.Abort() the remaining handlers would still run and a protected handler
// could execute on an unauthenticated request.
func Auth(tokens identity.TokenService) gin.HandlerFunc {
	return func(c *gin.Context) {
		scheme, token, found := strings.Cut(c.GetHeader("Authorization"), " ")
		if !found || !strings.EqualFold(strings.TrimSpace(scheme), "Bearer") {
			abortUnauthorized(c)
			return
		}

		token = strings.TrimSpace(token)
		if token == "" {
			abortUnauthorized(c)
			return
		}

		userID, err := tokens.Verify(token)
		if err != nil {
			abortUnauthorized(c)
			return
		}

		c.Request = c.Request.WithContext(
			context.WithValue(c.Request.Context(), userIDKey{}, userID),
		)
		c.Next()
	}
}

// UserID returns the authenticated user ID placed in the request context by
// Auth. It is the only supported way to obtain the caller's identity: a user ID
// supplied in a request body or query is never trusted.
func UserID(c *gin.Context) (int64, bool) {
	id, ok := c.Request.Context().Value(userIDKey{}).(int64)
	return id, ok
}

func abortUnauthorized(c *gin.Context) {
	response.APIError(c, http.StatusUnauthorized, response.CodeUnauthorized, response.MsgUnauthorized)
	c.Abort()
}
