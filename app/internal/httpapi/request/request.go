// Package request parses HTTP request input shared by the module handlers: the
// pagination query pair, the positive :id path parameter, and the authenticated
// user. Every failure is written through the shared response envelope, so all
// modules answer identically.
package request

import (
	"fmt"
	"net/http"
	"strconv"

	"app/internal/httpapi/middleware"
	"app/internal/httpapi/response"

	"github.com/gin-gonic/gin"
)

// Pagination bounds (Global Constraints): defaults 1/10, ceiling 100.
const (
	defaultPage  = 1
	defaultLimit = 10
	maxLimit     = 100
)

// Pagination reads ?page and ?limit under the strict presence rule: an absent
// parameter falls back to the API default, while a present one must be an
// integer in range. Silently defaulting an explicitly supplied value would hide
// a client bug and contradict the documented contract.
func Pagination(c *gin.Context) (page, limit int, ok bool) {
	page, limit = defaultPage, defaultLimit
	q := c.Request.URL.Query()

	if q.Has("page") {
		n, err := strconv.Atoi(q.Get("page"))
		if err != nil || n < 1 {
			response.APIError(c, http.StatusBadRequest, response.CodeValidation,
				fmt.Sprintf("Page must be at least %d.", defaultPage))
			return 0, 0, false
		}
		page = n
	}

	if q.Has("limit") {
		n, err := strconv.Atoi(q.Get("limit"))
		if err != nil || n < 1 || n > maxLimit {
			response.APIError(c, http.StatusBadRequest, response.CodeValidation,
				fmt.Sprintf("Limit must be between 1 and %d.", maxLimit))
			return 0, 0, false
		}
		limit = n
	}

	return page, limit, true
}

// PostID parses the :id path parameter as a positive int64. Both the posts and
// the comments route families use :id for a post identifier, so the parsing and
// its error wording live in one place.
func PostID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		response.APIError(c, http.StatusBadRequest, response.CodeValidation, "Post ID must be a positive integer.")
		return 0, false
	}
	return id, true
}

// UserID returns the authenticated user placed in the context by the auth
// middleware. The route wiring guarantees it on protected routes; the fallback
// only fires if a protected route is ever registered without auth.
func UserID(c *gin.Context) (int64, bool) {
	id, ok := middleware.UserID(c)
	if !ok {
		response.APIError(c, http.StatusUnauthorized, response.CodeUnauthorized, response.MsgUnauthorized)
		return 0, false
	}
	return id, true
}
