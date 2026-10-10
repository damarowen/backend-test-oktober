// Package postshttp exposes the posts module over HTTP. It decodes requests,
// reads the authenticated user from the auth middleware, and maps results
// through the shared response envelope; it holds no business rules itself.
package postshttp

import (
	"log/slog"
	"net/http"
	"time"

	"app/internal/httpapi/request"
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
	userID, ok := request.UserID(c)
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
	id, ok := request.PostID(c)
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
	page, limit, ok := request.Pagination(c)
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
	userID, ok := request.UserID(c)
	if !ok {
		return
	}
	id, ok := request.PostID(c)
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
	userID, ok := request.UserID(c)
	if !ok {
		return
	}
	id, ok := request.PostID(c)
	if !ok {
		return
	}

	if err := h.svc.Delete(c.Request.Context(), userID, id); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// fail maps a service error to its HTTP response. An unrecognised error still
// gets a generic 500, while the cause is logged server-side so an internal
// failure is never silently swallowed.
func (h *Handler) fail(c *gin.Context, err error) {
	if !response.FromError(c, err) {
		slog.ErrorContext(c.Request.Context(), "posts: unhandled error", "error", err)
	}
}
