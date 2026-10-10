// Package httpapi composes the API's HTTP surface: it mounts every module's
// routes on one Gin engine, exposes the health endpoint, and keeps the JSON 404
// consistent with the shared response envelope.
package httpapi

import (
	"context"
	"log/slog"
	"net/http"

	commentshttp "app/internal/comments/adapter/http"
	"app/internal/httpapi/response"
	identityhttp "app/internal/identity/adapter/http"
	postshttp "app/internal/posts/adapter/http"

	"github.com/gin-gonic/gin"
)

// Handlers groups the HTTP handlers of every business module.
type Handlers struct {
	Identity *identityhttp.Handler
	Posts    *postshttp.Handler
	Comments *commentshttp.Handler
}

// Pinger reports database connectivity for GET /healthz. *sql.DB satisfies it;
// tests can supply a stub.
type Pinger interface {
	PingContext(ctx context.Context) error
}

// NewRouter mounts the public API: the module routes, the health check, and a
// JSON 404 for unknown paths. auth guards the write endpoints of posts and
// comments; reads stay public.
func NewRouter(h Handlers, auth gin.HandlerFunc, db Pinger) *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	r.GET("/healthz", healthz(db))

	h.Identity.RegisterAuth(r.Group("/"))
	h.Posts.Register(r.Group("/"), auth)
	h.Comments.Register(r.Group("/"), auth)

	r.NoRoute(notFound)

	return r
}

// healthz reports service health: 200 while the database answers a ping, 503
// with the error envelope otherwise, so an orchestrator can take the instance
// out of rotation.
func healthz(db Pinger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := db.PingContext(c.Request.Context()); err != nil {
			slog.ErrorContext(c.Request.Context(), "httpapi: healthz ping failed", "error", err)
			response.APIError(c, http.StatusServiceUnavailable, response.CodeInternal, response.MsgInternal)
			return
		}
		response.Data(c, http.StatusOK, gin.H{"status": "ok"})
	}
}

// notFound answers unmatched routes with the same error envelope the rest of
// the API uses, instead of Gin's default plain-text 404.
func notFound(c *gin.Context) {
	response.APIError(c, http.StatusNotFound, response.CodeNotFound, response.MsgNotFound)
}
