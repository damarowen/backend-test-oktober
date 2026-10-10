package commentshttp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"app/internal/comments"
	"app/internal/httpapi/middleware"

	"github.com/gin-gonic/gin"
)

// --- test doubles -----------------------------------------------------------

const tokenUser = 42

type stubTokens struct{}

func (stubTokens) Issue(int64) (string, error) { return "", errors.New("not used") }

func (stubTokens) Verify(token string) (int64, error) {
	if token != "good-token" {
		return 0, errors.New("invalid token")
	}
	return tokenUser, nil
}

// fakeRepo is an in-memory comments.Repository that filters by post id like the
// SQL adapter does.
type fakeRepo struct {
	stored map[int64]comments.Comment
	nextID int64
}

func (f *fakeRepo) Create(_ context.Context, c comments.Comment) (comments.Comment, error) {
	c.ID = f.nextID
	f.nextID++
	c.CreatedAt = time.Now()
	f.stored[c.ID] = c
	return c, nil
}

func (f *fakeRepo) ListByPost(_ context.Context, postID int64, page, limit int) ([]comments.Comment, int64, error) {
	all := make([]comments.Comment, 0, len(f.stored))
	for _, c := range f.stored {
		if c.PostID == postID {
			all = append(all, c)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	return all, int64(len(all)), nil
}

func (f *fakeRepo) seed(postID, authorID int64, authorName, content string) comments.Comment {
	c := comments.Comment{ID: f.nextID, PostID: postID, AuthorID: authorID, AuthorName: authorName, Content: content, CreatedAt: time.Now()}
	f.nextID++
	f.stored[c.ID] = c
	return c
}

// fakePosts is a toggleable PostChecker.
type fakePosts struct{ exists bool }

func (f *fakePosts) Exists(_ context.Context, _ int64) (bool, error) { return f.exists, nil }

// fakeUsers returns a fixed display name — the snapshot the handler must store.
type fakeUsers struct{ name string }

func (f *fakeUsers) UserName(_ context.Context, _ int64) (string, error) { return f.name, nil }

// --- helpers ----------------------------------------------------------------

type env struct {
	router *gin.Engine
	repo   *fakeRepo
	posts  *fakePosts
}

func newTestEnv(t *testing.T) *env {
	t.Helper()
	gin.SetMode(gin.TestMode)

	repo := &fakeRepo{stored: make(map[int64]comments.Comment), nextID: 1}
	posts := &fakePosts{exists: true}
	svc := comments.NewService(repo, posts, &fakeUsers{name: "Budi"})

	router := gin.New()
	NewHandler(svc).Register(router.Group("/"), middleware.Auth(stubTokens{}))
	return &env{router: router, repo: repo, posts: posts}
}

func (e *env) request(t *testing.T, method, path, body string, authed bool) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if authed {
		req.Header.Set("Authorization", "Bearer good-token")
	}

	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

type commentData struct {
	ID         int64     `json:"id"`
	PostID     int64     `json:"post_id"`
	AuthorID   int64     `json:"author_id"`
	AuthorName string    `json:"author_name"`
	Content    string    `json:"content"`
	CreatedAt  time.Time `json:"created_at"`
}

type singleEnvelope struct {
	Data commentData `json:"data"`
}

type listEnvelope struct {
	Data       []commentData `json:"data"`
	Pagination struct {
		Page  int   `json:"page"`
		Limit int   `json:"limit"`
		Total int64 `json:"total"`
	} `json:"pagination"`
}

type errorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func decodeBody[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("response is not valid JSON: %v\nbody: %s", err, rec.Body.String())
	}
	return v
}

// --- tests ------------------------------------------------------------------

func TestCreateComment_NoAuth_Returns401(t *testing.T) {
	e := newTestEnv(t)

	rec := e.request(t, http.MethodPost, "/posts/1/comments", `{"content":"hi"}`, false)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401 (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestCreateComment_ExtraAuthorFields_Ignored_Returns201WithTokenAuthor(t *testing.T) {
	e := newTestEnv(t)

	// author_id/author_name/post_id in the body must be ignored: the author
	// comes from the token, the name from the identity snapshot, the post from
	// the path.
	rec := e.request(t, http.MethodPost, "/posts/1/comments",
		`{"content":"hi","author_id":999,"author_name":"Hacker","post_id":777}`, true)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want 201 (body: %s)", rec.Code, rec.Body.String())
	}

	env := decodeBody[singleEnvelope](t, rec)
	if env.Data.AuthorID != tokenUser {
		t.Errorf("author_id: got %d, want the token user %d", env.Data.AuthorID, tokenUser)
	}
	if env.Data.AuthorName != "Budi" {
		t.Errorf("author_name: got %q, want the provider snapshot %q", env.Data.AuthorName, "Budi")
	}
	if env.Data.PostID != 1 {
		t.Errorf("post_id: got %d, want the path post 1", env.Data.PostID)
	}
	if env.Data.Content != "hi" {
		t.Errorf("content: got %q, want %q", env.Data.Content, "hi")
	}
}

func TestCreateComment_EmptyContent_Returns400(t *testing.T) {
	e := newTestEnv(t)

	rec := e.request(t, http.MethodPost, "/posts/1/comments", `{"content":"   "}`, true)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400 (body: %s)", rec.Code, rec.Body.String())
	}
	if code := decodeBody[errorEnvelope](t, rec).Error.Code; code != "VALIDATION_ERROR" {
		t.Errorf("code: got %q, want VALIDATION_ERROR", code)
	}
}

func TestCreateComment_MissingPost_Returns404(t *testing.T) {
	e := newTestEnv(t)
	e.posts.exists = false

	rec := e.request(t, http.MethodPost, "/posts/999/comments", `{"content":"hi"}`, true)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404 (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestListComments_OK_ReturnsAuthorIDAndName(t *testing.T) {
	e := newTestEnv(t)
	e.repo.seed(1, 7, "Budi", "first!")
	e.repo.seed(2, 8, "Other", "other post comment")

	rec := e.request(t, http.MethodGet, "/posts/1/comments", "", false)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	env := decodeBody[listEnvelope](t, rec)
	if len(env.Data) != 1 {
		t.Fatalf("data: got %d comments, want 1", len(env.Data))
	}
	if env.Data[0].AuthorID != 7 || env.Data[0].AuthorName != "Budi" {
		t.Errorf("data[0]: got author_id=%d author_name=%q, want 7/Budi", env.Data[0].AuthorID, env.Data[0].AuthorName)
	}
	if env.Pagination.Total != 1 {
		t.Errorf("pagination.total: got %d, want 1", env.Pagination.Total)
	}
}

func TestListComments_MissingPost_Returns404(t *testing.T) {
	e := newTestEnv(t)
	e.posts.exists = false

	rec := e.request(t, http.MethodGet, "/posts/999/comments", "", false)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404 (body: %s)", rec.Code, rec.Body.String())
	}
	if code := decodeBody[errorEnvelope](t, rec).Error.Code; code != "NOT_FOUND" {
		t.Errorf("code: got %q, want NOT_FOUND", code)
	}
}

func TestListComments_Empty_ReturnsEmptyArray(t *testing.T) {
	e := newTestEnv(t)

	rec := e.request(t, http.MethodGet, "/posts/1/comments", "", false)

	env := decodeBody[listEnvelope](t, rec)
	if env.Data == nil {
		t.Error("data must be [] for an empty list, never null")
	}
	if len(env.Data) != 0 {
		t.Errorf("data: got %d comments, want 0", len(env.Data))
	}
}
