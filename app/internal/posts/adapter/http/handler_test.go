package postshttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"app/internal/httpapi/middleware"
	"app/internal/posts"

	"github.com/gin-gonic/gin"
)

// --- test doubles -----------------------------------------------------------

const tokenUser = 42

// stubTokens accepts exactly one token and resolves it to tokenUser. Real
// verification is pinned by the identity tests; here the middleware only needs
// to place a known identity in the context.
type stubTokens struct{}

func (stubTokens) Issue(int64) (string, error) { return "", errors.New("not used") }

func (stubTokens) Verify(token string) (int64, error) {
	if token != "good-token" {
		return 0, errors.New("invalid token")
	}
	return tokenUser, nil
}

// fakeRepo is an in-memory posts.Repository with the same ownership rule as the
// SQL adapter: Update and Delete only match rows owned by the given author.
type fakeRepo struct {
	stored map[int64]posts.Post
	nextID int64
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{stored: make(map[int64]posts.Post), nextID: 1}
}

func (f *fakeRepo) seed(authorID int64, title, content string) posts.Post {
	now := time.Now()
	p := posts.Post{ID: f.nextID, AuthorID: authorID, Title: title, Content: content, CreatedAt: now, UpdatedAt: now}
	f.nextID++
	f.stored[p.ID] = p
	return p
}

func (f *fakeRepo) Create(_ context.Context, authorID int64, title, content string) (posts.Post, error) {
	return f.seed(authorID, title, content), nil
}

func (f *fakeRepo) GetByID(_ context.Context, id int64) (posts.Post, error) {
	p, ok := f.stored[id]
	if !ok {
		return posts.Post{}, posts.ErrNotFound
	}
	return p, nil
}

func (f *fakeRepo) List(_ context.Context, page, limit int) ([]posts.Post, int64, error) {
	all := make([]posts.Post, 0, len(f.stored))
	for _, p := range f.stored {
		all = append(all, p)
	}
	return all, int64(len(all)), nil
}

func (f *fakeRepo) Update(_ context.Context, id, authorID int64, title, content string) (posts.Post, error) {
	p, ok := f.stored[id]
	if !ok || p.AuthorID != authorID {
		return posts.Post{}, posts.ErrNotFound
	}
	p.Title, p.Content = title, content
	f.stored[id] = p
	return p, nil
}

func (f *fakeRepo) Delete(_ context.Context, id, authorID int64) error {
	p, ok := f.stored[id]
	if !ok || p.AuthorID != authorID {
		return posts.ErrNotFound
	}
	delete(f.stored, id)
	return nil
}

// --- helpers ----------------------------------------------------------------

func newTestRouter(t *testing.T) (*gin.Engine, *fakeRepo) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	repo := newFakeRepo()
	svc := posts.NewService(repo)

	r := gin.New()
	NewHandler(svc).Register(r.Group("/"), middleware.Auth(stubTokens{}))
	return r, repo
}

func request(t *testing.T, r *gin.Engine, method, path, body string, authed bool) *httptest.ResponseRecorder {
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
	r.ServeHTTP(rec, req)
	return rec
}

type postData struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	AuthorID  int64     `json:"author_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type singleEnvelope struct {
	Data postData `json:"data"`
}

type listEnvelope struct {
	Data       []postData `json:"data"`
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

func TestCreatePost_NoAuth_Returns401(t *testing.T) {
	r, _ := newTestRouter(t)

	rec := request(t, r, http.MethodPost, "/posts", `{"title":"Hello","content":"Body"}`, false)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401 (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestCreatePost_Authenticated_Returns201_AuthorFromToken(t *testing.T) {
	r, repo := newTestRouter(t)

	// author_id in the body must be ignored: the author comes from the token.
	rec := request(t, r, http.MethodPost, "/posts",
		`{"title":"Hello","content":"Body","author_id":999}`, true)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want 201 (body: %s)", rec.Code, rec.Body.String())
	}

	env := decodeBody[singleEnvelope](t, rec)
	if env.Data.AuthorID != tokenUser {
		t.Errorf("author_id: got %d, want the token user %d", env.Data.AuthorID, tokenUser)
	}
	if env.Data.ID <= 0 {
		t.Errorf("id: got %d, want > 0", env.Data.ID)
	}
	if env.Data.Title != "Hello" || env.Data.Content != "Body" {
		t.Errorf("data: got %+v, want the submitted title/content", env.Data)
	}
	if len(repo.stored) != 1 {
		t.Errorf("stored posts: got %d, want 1", len(repo.stored))
	}
}

func TestCreatePost_InvalidBody_Returns400(t *testing.T) {
	r, _ := newTestRouter(t)

	rec := request(t, r, http.MethodPost, "/posts", `{"title":"   ","content":"Body"}`, true)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400 (body: %s)", rec.Code, rec.Body.String())
	}
	if code := decodeBody[errorEnvelope](t, rec).Error.Code; code != "VALIDATION_ERROR" {
		t.Errorf("code: got %q, want VALIDATION_ERROR", code)
	}
}

func TestGetPost_Found_Returns200(t *testing.T) {
	r, repo := newTestRouter(t)
	seed := repo.seed(1, "Hello", "Body")

	rec := request(t, r, http.MethodGet, fmt.Sprintf("/posts/%d", seed.ID), "", false)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	env := decodeBody[singleEnvelope](t, rec)
	if env.Data.ID != seed.ID || env.Data.AuthorID != 1 || env.Data.Title != "Hello" {
		t.Errorf("data: got %+v, want the seeded post", env.Data)
	}
}

func TestGetPost_Missing_Returns404(t *testing.T) {
	r, _ := newTestRouter(t)

	rec := request(t, r, http.MethodGet, "/posts/999", "", false)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404 (body: %s)", rec.Code, rec.Body.String())
	}
	if code := decodeBody[errorEnvelope](t, rec).Error.Code; code != "NOT_FOUND" {
		t.Errorf("code: got %q, want NOT_FOUND", code)
	}
}

func TestGetPost_InvalidID_Returns400(t *testing.T) {
	r, _ := newTestRouter(t)

	for _, path := range []string{"/posts/abc", "/posts/0", "/posts/-1"} {
		t.Run(path, func(t *testing.T) {
			rec := request(t, r, http.MethodGet, path, "", false)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status: got %d, want 400 (body: %s)", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestListPosts_DefaultPagination(t *testing.T) {
	r, repo := newTestRouter(t)
	repo.seed(1, "A", "a")
	repo.seed(1, "B", "b")

	rec := request(t, r, http.MethodGet, "/posts", "", false)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	env := decodeBody[listEnvelope](t, rec)
	if env.Pagination.Page != 1 || env.Pagination.Limit != 10 || env.Pagination.Total != 2 {
		t.Errorf("pagination: got %+v, want page=1 limit=10 total=2", env.Pagination)
	}
	if len(env.Data) != 2 {
		t.Errorf("data: got %d posts, want 2", len(env.Data))
	}
}

func TestListPosts_Empty_ReturnsEmptyArray(t *testing.T) {
	r, _ := newTestRouter(t)

	rec := request(t, r, http.MethodGet, "/posts", "", false)

	env := decodeBody[listEnvelope](t, rec)
	if env.Data == nil {
		t.Error("data must be [] for an empty list, never null")
	}
	if len(env.Data) != 0 {
		t.Errorf("data: got %d posts, want 0", len(env.Data))
	}
}

func TestListPosts_ExplicitPagination_Echoed(t *testing.T) {
	r, _ := newTestRouter(t)

	rec := request(t, r, http.MethodGet, "/posts?page=2&limit=5", "", false)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	env := decodeBody[listEnvelope](t, rec)
	if env.Pagination.Page != 2 || env.Pagination.Limit != 5 {
		t.Errorf("pagination: got %+v, want page=2 limit=5", env.Pagination)
	}
}

func TestListPosts_LimitOver100_Returns400(t *testing.T) {
	r, _ := newTestRouter(t)

	rec := request(t, r, http.MethodGet, "/posts?limit=101", "", false)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400 (body: %s)", rec.Code, rec.Body.String())
	}
	if code := decodeBody[errorEnvelope](t, rec).Error.Code; code != "VALIDATION_ERROR" {
		t.Errorf("code: got %q, want VALIDATION_ERROR", code)
	}
}

// TestListPosts_PresentPaginationOutOfRange_Returns400 pins the strict presence
// rule (requester decision, Option B): an explicitly supplied parameter must be
// in range; only an absent parameter may default.
func TestListPosts_PresentPaginationOutOfRange_Returns400(t *testing.T) {
	r, _ := newTestRouter(t)

	tests := []string{"?page=0", "?limit=0", "?page=-1", "?limit=-1", "?page=abc", "?limit=abc"}

	for _, query := range tests {
		t.Run(query, func(t *testing.T) {
			rec := request(t, r, http.MethodGet, "/posts"+query, "", false)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status: got %d, want 400 (body: %s)", rec.Code, rec.Body.String())
			}
			if code := decodeBody[errorEnvelope](t, rec).Error.Code; code != "VALIDATION_ERROR" {
				t.Errorf("code: got %q, want VALIDATION_ERROR", code)
			}
		})
	}
}

func TestUpdatePost_ByOwner_Returns200(t *testing.T) {
	r, repo := newTestRouter(t)
	seed := repo.seed(tokenUser, "Old", "Old body")

	rec := request(t, r, http.MethodPut, fmt.Sprintf("/posts/%d", seed.ID),
		`{"title":"New","content":"New body"}`, true)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	env := decodeBody[singleEnvelope](t, rec)
	if env.Data.Title != "New" || env.Data.Content != "New body" {
		t.Errorf("data: got %+v, want the updated title/content", env.Data)
	}
	if env.Data.AuthorID != tokenUser {
		t.Errorf("author_id: got %d, want %d (the author never changes)", env.Data.AuthorID, tokenUser)
	}
}

func TestUpdatePost_ByOtherUser_Returns403(t *testing.T) {
	r, repo := newTestRouter(t)
	seed := repo.seed(7, "Old", "Old body")

	rec := request(t, r, http.MethodPut, fmt.Sprintf("/posts/%d", seed.ID),
		`{"title":"Hijacked","content":"Hijacked body"}`, true)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status: got %d, want 403 (body: %s)", rec.Code, rec.Body.String())
	}
	if code := decodeBody[errorEnvelope](t, rec).Error.Code; code != "FORBIDDEN" {
		t.Errorf("code: got %q, want FORBIDDEN", code)
	}
	if stored := repo.stored[seed.ID]; stored.Title != "Old" {
		t.Error("a forbidden update must not modify the post")
	}
}

func TestUpdatePost_Missing_Returns404(t *testing.T) {
	r, _ := newTestRouter(t)

	rec := request(t, r, http.MethodPut, "/posts/999", `{"title":"New","content":"New body"}`, true)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404 (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestDeletePost_ByOwner_Returns204(t *testing.T) {
	r, repo := newTestRouter(t)
	seed := repo.seed(tokenUser, "Title", "Body")

	rec := request(t, r, http.MethodDelete, fmt.Sprintf("/posts/%d", seed.ID), "", true)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status: got %d, want 204 (body: %s)", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Errorf("204 must have no body, got %q", rec.Body.String())
	}
	if _, ok := repo.stored[seed.ID]; ok {
		t.Error("the post must be deleted")
	}
}

func TestDeletePost_ByOtherUser_Returns403(t *testing.T) {
	r, repo := newTestRouter(t)
	seed := repo.seed(7, "Title", "Body")

	rec := request(t, r, http.MethodDelete, fmt.Sprintf("/posts/%d", seed.ID), "", true)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status: got %d, want 403 (body: %s)", rec.Code, rec.Body.String())
	}
	if _, ok := repo.stored[seed.ID]; !ok {
		t.Error("a forbidden delete must not remove the post")
	}
}
