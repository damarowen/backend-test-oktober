// Package integration contains MySQL-backed integration tests for the
// repository adapters and the migration runner. The whole package is
// environment-gated: without TEST_MYSQL_DSN every test skips, so `go test ./...`
// stays green on machines (and CI) with no database.
package integration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"app/internal/identity"
	identitymysql "app/internal/identity/adapter/mysql"
	"app/internal/platform/database"
	"app/internal/posts"
	postsmysql "app/internal/posts/adapter/mysql"
)

// dsnEnv switches these tests on. It is deliberately different from DB_DSN so
// integration tests cannot be pointed at a production/dev database by accident.
const dsnEnv = "TEST_MYSQL_DSN"

// moduleRoot is app/ relative to this package directory (app/tests/integration).
const moduleRoot = "../.."

var (
	migrateOnce sync.Once
	migrateErr  error
)

// migratedDB returns a pool on the test database with the migrations applied
// once per process. It skips the test when TEST_MYSQL_DSN is unset.
func migratedDB(t *testing.T) *sql.DB {
	t.Helper()

	if os.Getenv(dsnEnv) == "" {
		t.Skipf("%s not set; skipping integration test", dsnEnv)
	}

	migrateOnce.Do(func() {
		if _, err := runMigrate(os.Getenv(dsnEnv)); err != nil {
			migrateErr = fmt.Errorf("apply migrations: %w", err)
		}
	})
	if migrateErr != nil {
		t.Fatalf("integration setup: %v", migrateErr)
	}

	db, err := database.Open(t.Context(), os.Getenv(dsnEnv))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// runMigrate executes the real migration command as a subprocess — cmd/migrate
// is package main and cannot be imported — and returns its combined output.
func runMigrate(dsn string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "run", "./cmd/migrate")
	cmd.Dir = moduleRoot
	cmd.Env = append(os.Environ(), "DB_DSN="+dsn)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("run migrate: %w\n%s", err, out)
	}
	return string(out), nil
}

// uniqueEmail keeps parallel runs and repeated runs from colliding on the
// unique email index.
func uniqueEmail(tag string) string {
	return fmt.Sprintf("it-%s-%d@example.com", tag, time.Now().UnixNano())
}

// cleanup removes the rows a test created, in foreign-key order. It runs after
// the test's context is canceled, so it uses its own background context.
func cleanup(t *testing.T, db *sql.DB, postIDs, userIDs []int64) {
	t.Helper()
	for _, id := range postIDs {
		if _, err := db.ExecContext(context.Background(), "DELETE FROM posts WHERE id = ?", id); err != nil {
			t.Errorf("cleanup post %d: %v", id, err)
		}
	}
	for _, id := range userIDs {
		if _, err := db.ExecContext(context.Background(), "DELETE FROM users WHERE id = ?", id); err != nil {
			t.Errorf("cleanup user %d: %v", id, err)
		}
	}
}

func TestIntegration_MigrateUp_Idempotent(t *testing.T) {
	migratedDB(t) // applies the migrations once (sync.Once)

	out, err := runMigrate(os.Getenv(dsnEnv))
	if err != nil {
		t.Fatalf("second migrate run: %v\n%s", err, out)
	}
	for _, version := range []string{"001_create_users", "002_create_posts", "003_create_comments"} {
		if want := version + " already applied"; !strings.Contains(out, want) {
			t.Errorf("second run output missing %q:\n%s", want, out)
		}
	}
}

func TestIntegration_UserEmailUniqueness(t *testing.T) {
	db := migratedDB(t)
	repo := identitymysql.NewRepository(db)

	email := uniqueEmail("unique")
	user, err := repo.Create(t.Context(), "First", email, "hash-one")
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	t.Cleanup(func() { cleanup(t, db, nil, []int64{user.ID}) })

	_, err = repo.Create(t.Context(), "Second", email, "hash-two")
	if !errors.Is(err, identity.ErrEmailTaken) {
		t.Fatalf("duplicate create: error = %v, want identity.ErrEmailTaken", err)
	}
}

func TestIntegration_UpdateNoChange_ReturnsPost(t *testing.T) {
	db := migratedDB(t)
	users := identitymysql.NewRepository(db)
	postRepo := postsmysql.NewRepository(db)

	user, err := users.Create(t.Context(), "Updater", uniqueEmail("upd"), "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	post, err := postRepo.Create(t.Context(), user.ID, "Same title", "Same content")
	if err != nil {
		t.Fatalf("create post: %v", err)
	}
	t.Cleanup(func() { cleanup(t, db, []int64{post.ID}, []int64{user.ID}) })

	updated, err := postRepo.Update(t.Context(), post.ID, user.ID, post.Title, post.Content)
	if err != nil {
		t.Fatalf("no-op update: %v", err)
	}
	if updated.ID != post.ID || updated.Title != post.Title || updated.Content != post.Content {
		t.Errorf("no-op update returned %+v, want unchanged id/title/content of %+v", updated, post)
	}
}

func TestIntegration_DeleteZeroRows_ReturnsNotFound(t *testing.T) {
	db := migratedDB(t)
	users := identitymysql.NewRepository(db)
	postRepo := postsmysql.NewRepository(db)

	user, err := users.Create(t.Context(), "Deleter", uniqueEmail("del"), "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	post, err := postRepo.Create(t.Context(), user.ID, "Keep me", "Body")
	if err != nil {
		t.Fatalf("create post: %v", err)
	}
	t.Cleanup(func() { cleanup(t, db, []int64{post.ID}, []int64{user.ID}) })

	tests := []struct {
		name   string
		id     int64
		author int64
	}{
		{"missing id", post.ID + 1_000_000, user.ID},
		{"wrong author", post.ID, user.ID + 1_000_000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := postRepo.Delete(t.Context(), tt.id, tt.author); !errors.Is(err, posts.ErrNotFound) {
				t.Fatalf("delete: error = %v, want posts.ErrNotFound", err)
			}
			if _, err := postRepo.GetByID(t.Context(), post.ID); err != nil {
				t.Errorf("post should still exist after a 0-row delete: %v", err)
			}
		})
	}
}
