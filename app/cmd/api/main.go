// Command api is the Blog REST API server. It loads configuration, opens the
// MySQL pool, wires the three modules' adapters, services, and handlers by
// hand, and serves HTTP until it receives SIGINT or SIGTERM, then drains
// in-flight requests before closing the database pool.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"app/internal/comments"
	commentshttp "app/internal/comments/adapter/http"
	commentsmysql "app/internal/comments/adapter/mysql"
	"app/internal/config"
	"app/internal/httpapi"
	"app/internal/httpapi/middleware"
	"app/internal/identity"
	identityhttp "app/internal/identity/adapter/http"
	identitymysql "app/internal/identity/adapter/mysql"
	"app/internal/platform/database"
	"app/internal/posts"
	postshttp "app/internal/posts/adapter/http"
	postsmysql "app/internal/posts/adapter/mysql"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

// shutdownTimeout bounds the graceful drain after a stop signal.
const shutdownTimeout = 15 * time.Second

func main() {
	loadDotEnv()

	// Gin reads GIN_MODE once at package init -- before loadDotEnv has run --
	// so re-apply it here to make the .env value effective.
	if mode := os.Getenv(gin.EnvGinMode); mode != "" {
		gin.SetMode(mode)
	}

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "api: %v\n", err)
		os.Exit(1)
	}
}

func loadDotEnv() {
	dir, err := os.Getwd()
	if err != nil {
		return
	}
	for range 4 { // cmd/api -> cmd -> app -> repo root
		path := filepath.Join(dir, ".env")
		if _, err := os.Stat(path); err == nil {
			_ = godotenv.Load(path)
			return
		}
		dir = filepath.Dir(dir)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Canceled on SIGINT/SIGTERM; used for the startup connectivity check and
	// to trigger the graceful shutdown below.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := database.Open(ctx, cfg.DBDSN)
	if err != nil {
		return err
	}
	defer db.Close()

	// Manual dependency injection, linear and explicit:
	// adapters -> services -> handlers -> router.
	tokens := identity.NewJWT(cfg.JWTSecret, cfg.JWTTTL)
	identitySvc := identity.NewService(identitymysql.NewRepository(db), tokens)
	postsSvc := posts.NewService(postsmysql.NewRepository(db))
	commentsSvc := comments.NewService(commentsmysql.NewRepository(db), postsSvc, identitySvc)

	router := httpapi.NewRouter(
		httpapi.Handlers{
			Identity: identityhttp.NewHandler(identitySvc, cfg.JWTTTL),
			Posts:    postshttp.NewHandler(postsSvc),
			Comments: commentshttp.NewHandler(commentsSvc),
		},
		middleware.Auth(tokens),
		db,
	)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		slog.Info("api: listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
	}()

	select {
	case err := <-serveErr:
		return fmt.Errorf("listen: %w", err)
	case <-ctx.Done():
	}

	slog.Info("api: shutting down", "timeout", shutdownTimeout)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}
