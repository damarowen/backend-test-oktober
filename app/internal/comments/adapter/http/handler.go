// Package commentshttp exposes the comments module over HTTP. It decodes
// requests, reads the authenticated user, and maps results through the shared
// response envelope; it holds no business rules itself.
package commentshttp

import (
	"log/slog"
	"net/http"
	"time"

	"app/internal/comments"
	"app/internal/httpapi/request"
	"app/internal/httpapi/response"

	"github.com/gin-gonic/gin"
)

// Handler serves the comments routes.
type Handler struct {
	svc *comments.Service
}

// NewHandler wires the comments service.
func NewHandler(svc *comments.Service) *Handler {
	return &Handler{svc: svc}
}

// Register mounts the comments routes: listing is public, creating requires
// auth, which supplies the author identity stored with the comment.
func (h *Handler) Register(r *gin.RouterGroup, auth gin.HandlerFunc) {
	r.GET("/posts/:id/comments", h.list)
	r.POST("/posts/:id/comments", auth, h.create)
}

// commentRequest decodes only the fields a client may set. author_id,
// author_name, and post_id have no member here: the author comes from the
// token, the name from Identity at creation time, and the post from the path.
type commentRequest struct {
	Content string `json:"content"`
}

// commentResponse is the public view of a comment.
type commentResponse struct {
	ID         int64     `json:"id"`
	PostID     int64     `json:"post_id"`
	AuthorID   int64     `json:"author_id"`
	AuthorName string    `json:"author_name"`
	Content    string    `json:"content"`
	CreatedAt  time.Time `json:"created_at"`
}

func newCommentResponse(c comments.Comment) commentResponse {
	return commentResponse{
		ID:         c.ID,
		PostID:     c.PostID,
		AuthorID:   c.AuthorID,
		AuthorName: c.AuthorName,
		Content:    c.Content,
		CreatedAt:  c.CreatedAt,
	}
}

func (h *Handler) create(c *gin.Context) {
	userID, ok := request.UserID(c)
	if !ok {
		return
	}
	postID, ok := request.PostID(c)
	if !ok {
		return
	}

	var req commentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.APIError(c, http.StatusBadRequest, response.CodeValidation, response.MsgValidation)
		return
	}

	comment, err := h.svc.Create(c.Request.Context(), postID, userID, req.Content)
	if err != nil {
		h.fail(c, err)
		return
	}
	response.Data(c, http.StatusCreated, newCommentResponse(comment))
}

func (h *Handler) list(c *gin.Context) {
	postID, ok := request.PostID(c)
	if !ok {
		return
	}
	page, limit, ok := request.Pagination(c)
	if !ok {
		return
	}

	list, total, err := h.svc.ListByPost(c.Request.Context(), postID, page, limit)
	if err != nil {
		h.fail(c, err)
		return
	}

	out := make([]commentResponse, 0, len(list))
	for _, comment := range list {
		out = append(out, newCommentResponse(comment))
	}
	response.Paginated(c, out, page, limit, total)
}

// fail maps a service error to its HTTP response. An unrecognised error still
// gets a generic 500, while the cause is logged server-side so an internal
// failure is never silently swallowed.
func (h *Handler) fail(c *gin.Context, err error) {
	if !response.FromError(c, err) {
		slog.ErrorContext(c.Request.Context(), "comments: unhandled error", "error", err)
	}
}
