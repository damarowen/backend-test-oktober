// Package identityhttp exposes the identity module over HTTP. It decodes
// requests, calls the service, and maps the result through the shared response
// envelope; it holds no business rules itself.
package identityhttp

import (
	"log/slog"
	"net/http"
	"time"

	"app/internal/httpapi/response"
	"app/internal/identity"

	"github.com/gin-gonic/gin"
)

// Handler serves the identity module's public routes.
type Handler struct {
	svc *identity.Service
	ttl time.Duration
}

// NewHandler wires the identity service. ttl is reported to clients as
// expires_in and must be the same lifetime the token service was built with.
func NewHandler(svc *identity.Service, ttl time.Duration) *Handler {
	return &Handler{svc: svc, ttl: ttl}
}

// RegisterAuth mounts the public authentication routes on the given group.
func (h *Handler) RegisterAuth(r *gin.RouterGroup) {
	r.POST("/register", h.register)
	r.POST("/login", h.login)
}

// registerRequest decodes only the fields a client may set. Unknown fields
// (including anything ownership-related) are ignored, never bound.
type registerRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

// loginRequest likewise ignores every field a client should not be able to set.
type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// userResponse is the public view of a user. PasswordHash has no field here, so
// it can never be serialized.
type userResponse struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

type tokenResponse struct {
	Token     string `json:"token"`
	TokenType string `json:"token_type"`
	ExpiresIn int    `json:"expires_in"`
}

func (h *Handler) register(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.APIError(c, http.StatusBadRequest, response.CodeValidation, response.MsgValidation)
		return
	}

	user, err := h.svc.Register(c.Request.Context(), req.Name, req.Email, req.Password)
	if err != nil {
		h.fail(c, err)
		return
	}

	response.Data(c, http.StatusCreated, userResponse{
		ID:        user.ID,
		Name:      user.Name,
		Email:     user.Email,
		CreatedAt: user.CreatedAt,
	})
}

func (h *Handler) login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.APIError(c, http.StatusBadRequest, response.CodeValidation, response.MsgValidation)
		return
	}

	token, err := h.svc.Login(c.Request.Context(), req.Email, req.Password)
	if err != nil {
		h.fail(c, err)
		return
	}

	response.Data(c, http.StatusOK, tokenResponse{
		Token:     token,
		TokenType: "Bearer",
		ExpiresIn: int(h.ttl.Seconds()),
	})
}

// fail maps a service error to its HTTP response. An error the mapping does not
// recognise is returned to the client as a generic 500, while its cause is
// logged server-side so an internal failure is never silently swallowed.
func (h *Handler) fail(c *gin.Context, err error) {
	if !response.FromError(c, err) {
		slog.ErrorContext(c.Request.Context(), "identity: unhandled error", "error", err)
	}
}
