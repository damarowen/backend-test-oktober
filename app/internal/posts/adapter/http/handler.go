// Package postshttp exposes the posts module over HTTP. It decodes requests,
// reads the authenticated user from the auth middleware, and maps results
// through the shared response envelope; it holds no business rules itself.
package postshttp

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"app/internal/httpapi/middleware"
	"app/internal/httpapi/response"
	"app/internal/posts"

	"github.com/gin-gonic/gin"
)

// Handler serves the posts routes.
type Handler struct {
	svc *posts.Service
}

// NewHandler wires the posts service.
func NewHandler(svc *posts.Service) *Handler {
	return &Handler{svc: svc}
}

// Register mounts the posts routes on the given group. Reads are public; every
// write requires auth, which supplies the author identity for create and the
// ownership user for update and delete.
func (h *Handler) Register(r *gin.RouterGroup, auth gin.HandlerFunc) {
	r.GET("/posts", h.list)
	r.GET("/posts/:id", h.get)
	r.POST("/posts", auth, h.create)
	r.PUT("/posts/:id", auth, h.update)
	r.DELETE("/posts/:id", auth, h.delete)
}

// postRequest decodes only the fields a client may set. Ownership fields such
// as author_id have no member here, so they can never be bound.
type postRequest struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

// postResponse is the public view of a post.
type postResponse struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	AuthorID  int64     `json:"author_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func newPostResponse(p posts.Post) postResponse {
	return postResponse{
		ID:        p.ID,
		Title:     p.Title,
		Content:   p.Content,
		AuthorID:  p.AuthorID,
		CreatedAt: p.CreatedAt,
		UpdatedAt: p.UpdatedAt,
	}
}

func (h *Handler) create(c *gin.Context) {
	userID, ok := userID(c)
	if !ok {
		return
	}

	var req postRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.APIError(c, http.StatusBadRequest, response.CodeValidation, response.MsgValidation)
		return
	}

	p, err := h.svc.Create(c.Request.Context(), userID, req.Title, req.Content)
	if err != nil {
		h.fail(c, err)
		return
	}
	response.Data(c, http.StatusCreated, newPostResponse(p))
}

func (h *Handler) get(c *gin.Context) {
	id, ok := postID(c)
	if !ok {
		return
	}

	p, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		h.fail(c, err)
		return
	}
	response.Data(c, http.StatusOK, newPostResponse(p))
}

func (h *Handler) list(c *gin.Context) {
	page, limit, ok := pagination(c)
	if !ok {
		return
	}

	list, total, err := h.svc.List(c.Request.Context(), page, limit)
	if err != nil {
		h.fail(c, err)
		return
	}

	out := make([]postResponse, 0, len(list))
	for _, p := range list {
		out = append(out, newPostResponse(p))
	}
	response.Paginated(c, out, page, limit, total)
}

func (h *Handler) update(c *gin.Context) {
	userID, ok := userID(c)
	if !ok {
		return
	}
	id, ok := postID(c)
	if !ok {
		return
	}

	var req postRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.APIError(c, http.StatusBadRequest, response.CodeValidation, response.MsgValidation)
		return
	}

	p, err := h.svc.Update(c.Request.Context(), userID, id, req.Title, req.Content)
	if err != nil {
		h.fail(c, err)
		return
	}
	response.Data(c, http.StatusOK, newPostResponse(p))
}

func (h *Handler) delete(c *gin.Context) {
	userID, ok := userID(c)
	if !ok {
		return
	}
	id, ok := postID(c)
	if !ok {
		return
	}

	if err := h.svc.Delete(c.Request.Context(), userID, id); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// userID reads the authenticated user placed in the context by the auth
// middleware. The route wiring guarantees it on every write route; the
// fallback only fires if a protected route is ever registered without auth.
func userID(c *gin.Context) (int64, bool) {
	id, ok := middleware.UserID(c)
	if !ok {
		response.APIError(c, http.StatusUnauthorized, response.CodeUnauthorized, response.MsgUnauthorized)
		return 0, false
	}
	return id, true
}

// postID parses the :id path parameter as a positive int64. A non-numeric or
// non-positive value is a client error, not a 404.
func postID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		response.APIError(c, http.StatusBadRequest, response.CodeValidation, "Post ID must be a positive integer.")
		return 0, false
	}
	return id, true
}

// pagination reads ?page and ?limit under the strict presence rule: an absent
// parameter gets the API default, while a present one must be an integer in
// range. Silently defaulting an explicitly supplied value would hide a client
// bug and contradict the documented contract.
func pagination(c *gin.Context) (page, limit int, ok bool) {
	const (
		defaultPage  = 1
		defaultLimit = 10
		maxLimit     = 100
	)

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

// fail maps a service error to its HTTP response. An unrecognised error still
// gets a generic 500, while the cause is logged server-side so an internal
// failure is never silently swallowed.
func (h *Handler) fail(c *gin.Context, err error) {
	if !response.FromError(c, err) {
		slog.ErrorContext(c.Request.Context(), "posts: unhandled error", "error", err)
	}
}
