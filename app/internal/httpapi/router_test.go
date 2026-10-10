package httpapi

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
	commentshttp "app/internal/comments/adapter/http"
	"app/internal/httpapi/middleware"
	"app/internal/httpapi/response"
	"app/internal/identity"
	identityhttp "app/internal/identity/adapter/http"
	"app/internal/posts"
	postshttp "app/internal/posts/adapter/http"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

// --- test doubles -----------------------------------------------------------

const (
	testUserID   = int64(42)
	testEmail    = "budi@example.com"
	testPassword = "secret123"
)

// fakeUserRepo is an in-memory identity.UserRepository.
type fakeUserRepo struct {
	users   map[int64]identity.User
	byEmail map[string]int64
	nextID  int64
}

func newFakeUserRepo() *fakeUserRepo {
	return &fakeUserRepo{
		users:   make(map[int64]identity.User),
		byEmail: make(map[string]int64),
		nextID:  1,
	}
}

func (f *fakeUserRepo) Create(_ context.Context, name, email, passwordHash string) (identity.User, error) {
	if _, ok := f.byEmail[email]; ok {
		return identity.User{}, identity.ErrEmailTaken
	}
	u := identity.User{
		ID:           f.nextID,
		Name:         name,
		Email:        email,
		PasswordHash: passwordHash,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	f.nextID++
	f.users[u.ID] = u
	f.byEmail[u.Email] = u.ID
	return u, nil
}

func (f *fakeUserRepo) GetByEmail(_ context.Context, email string) (identity.User, error) {
	id, ok := f.byEmail[email]
	if !ok {
		return identity.User{}, identity.ErrUserNotFound
	}
	return f.users[id], nil
}

func (f *fakeUserRepo) GetByID(_ context.Context, id int64) (identity.User, error) {
	u, ok := f.users[id]
	if !ok {
		return identity.User{}, identity.ErrUserNotFound
	}
	return u, nil
}

// seed stores a user with a real bcrypt hash of the given password, so login
// exercises the same comparison path as production.
func (f *fakeUserRepo) seed(t *testing.T, id int64, name, email, password string) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash seed password: %v", err)
	}
	u := identity.User{
		ID:           id,
		Name:         name,
		Email:        email,
		PasswordHash: string(hash),
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	f.users[id] = u
	f.byEmail[email] = id
	if id >= f.nextID {
		f.nextID = id + 1
	}
}

// fakePostRepo is an in-memory posts.Repository.
type fakePostRepo struct {
	posts  map[int64]posts.Post
	nextID int64
}

func newFakePostRepo() *fakePostRepo {
	return &fakePostRepo{posts: make(map[int64]posts.Post), nextID: 1}
}

func (f *fakePostRepo) Create(_ context.Context, authorID int64, title, content string) (posts.Post, error) {
	p := posts.Post{
		ID:        f.nextID,
		AuthorID:  authorID,
		Title:     title,
		Content:   content,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	f.nextID++
	f.posts[p.ID] = p
	return p, nil
}

func (f *fakePostRepo) GetByID(_ context.Context, id int64) (posts.Post, error) {
	p, ok := f.posts[id]
	if !ok {
		return posts.Post{}, posts.ErrNotFound
	}
	return p, nil
}

func (f *fakePostRepo) List(_ context.Context, page, limit int) ([]posts.Post, int64, error) {
	all := make([]posts.Post, 0, len(f.posts))
	for _, p := range f.posts {
		all = append(all, p)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	return pageOf(all, page, limit)
}

func (f *fakePostRepo) Update(_ context.Context, id, authorID int64, title, content string) (posts.Post, error) {
	p, ok := f.posts[id]
	if !ok || p.AuthorID != authorID {
		return posts.Post{}, posts.ErrNotFound
	}
	p.Title, p.Content, p.UpdatedAt = title, content, time.Now()
	f.posts[id] = p
	return p, nil
}

func (f *fakePostRepo) Delete(_ context.Context, id, authorID int64) error {
	p, ok := f.posts[id]
	if !ok || p.AuthorID != authorID {
		return posts.ErrNotFound
	}
	delete(f.posts, id)
	return nil
}

func (f *fakePostRepo) seed(id, authorID int64, title, content string) {
	f.posts[id] = posts.Post{
		ID:        id,
		AuthorID:  authorID,
		Title:     title,
		Content:   content,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if id >= f.nextID {
		f.nextID = id + 1
	}
}

// fakeCommentRepo is an in-memory comments.Repository.
type fakeCommentRepo struct {
	comments map[int64]comments.Comment
	nextID   int64
}

func newFakeCommentRepo() *fakeCommentRepo {
	return &fakeCommentRepo{comments: make(map[int64]comments.Comment), nextID: 1}
}

func (f *fakeCommentRepo) Create(_ context.Context, c comments.Comment) (comments.Comment, error) {
	c.ID = f.nextID
	f.nextID++
	c.CreatedAt = time.Now()
	f.comments[c.ID] = c
	return c, nil
}

func (f *fakeCommentRepo) ListByPost(_ context.Context, postID int64, page, limit int) ([]comments.Comment, int64, error) {
	all := make([]comments.Comment, 0, len(f.comments))
	for _, c := range f.comments {
		if c.PostID == postID {
			all = append(all, c)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	return pageOf(all, page, limit)
}

// pageOf applies the same page/limit slicing the repositories do.
func pageOf[T any](all []T, page, limit int) ([]T, int64, error) {
	total := int64(len(all))
	start := (page - 1) * limit
	if start >= len(all) {
		return []T{}, total, nil
	}
	end := min(start+limit, len(all))
	return all[start:end], total, nil
}

// stubPinger lets a test force the health check's database ping outcome.
type stubPinger struct{ err error }

func (p stubPinger) PingContext(context.Context) error { return p.err }

// --- helpers ----------------------------------------------------------------

type testEnv struct {
	router *gin.Engine
	tokens *identity.JWT
}

// newTestEnv builds the production router over real services with in-memory
// repositories. pingErr controls the stub Pinger behind GET /healthz.
func newTestEnv(t *testing.T, pingErr error) *testEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)

	// gin.Logger writes to DefaultWriter; keep the test transcript clean.
	prevWriter := gin.DefaultWriter
	gin.DefaultWriter = io.Discard
	t.Cleanup(func() { gin.DefaultWriter = prevWriter })

	users := newFakeUserRepo()
	users.seed(t, testUserID, "Budi", testEmail, testPassword)

	postsRepo := newFakePostRepo()
	postsRepo.seed(1, testUserID, "First post", "First body")
	postsRepo.seed(2, testUserID, "Second post", "Second body")

	tokens := identity.NewJWT("router-test-secret-long-enough-1234", time.Hour)
	identitySvc := identity.NewService(users, tokens)
	postsSvc := posts.NewService(postsRepo)
	commentsSvc := comments.NewService(newFakeCommentRepo(), postsSvc, identitySvc)

	router := NewRouter(
		Handlers{
			Identity: identityhttp.NewHandler(identitySvc, time.Hour),
			Posts:    postshttp.NewHandler(postsSvc),
			Comments: commentshttp.NewHandler(commentsSvc),
		},
		middleware.Auth(tokens),
		stubPinger{err: pingErr},
	)

	return &testEnv{router: router, tokens: tokens}
}

// mustIssue returns a valid bearer token for the seeded test user.
func (e *testEnv) mustIssue(t *testing.T) string {
	t.Helper()
	token, err := e.tokens.Issue(testUserID)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	return token
}

func (e *testEnv) do(t *testing.T, method, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)
	return w
}

// errorCode decodes the error envelope and returns its code.
func errorCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error envelope: %v (body %q)", err, w.Body.String())
	}
	return body.Error.Code
}

// --- tests ------------------------------------------------------------------

func TestRouter_RouteRegistration(t *testing.T) {
	env := newTestEnv(t, nil)
	token := env.mustIssue(t)

	tests := []struct {
		name   string
		method string
		path   string
		body   string
		token  string
		want   int
	}{
		{"register", http.MethodPost, "/register", `{"name":"New User","email":"new@example.com","password":"secret123"}`, "", http.StatusCreated},
		{"login", http.MethodPost, "/login", `{"email":"` + testEmail + `","password":"` + testPassword + `"}`, "", http.StatusOK},
		{"create post", http.MethodPost, "/posts", `{"title":"New post","content":"Body"}`, token, http.StatusCreated},
		{"list posts", http.MethodGet, "/posts", "", "", http.StatusOK},
		{"get post", http.MethodGet, "/posts/1", "", "", http.StatusOK},
		{"update post", http.MethodPut, "/posts/1", `{"title":"Updated","content":"Body"}`, token, http.StatusOK},
		{"delete post", http.MethodDelete, "/posts/2", "", token, http.StatusNoContent},
		{"create comment", http.MethodPost, "/posts/1/comments", `{"content":"Nice"}`, token, http.StatusCreated},
		{"list comments", http.MethodGet, "/posts/1/comments", "", "", http.StatusOK},
		{"healthz", http.MethodGet, "/healthz", "", "", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := env.do(t, tt.method, tt.path, tt.body, tt.token)
			if w.Code != tt.want {
				t.Errorf("%s %s: status = %d, want %d (body %s)", tt.method, tt.path, w.Code, tt.want, w.Body.String())
			}
		})
	}
}

func TestHealthz_OK(t *testing.T) {
	env := newTestEnv(t, nil)

	w := env.do(t, http.MethodGet, "/healthz", "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /healthz: status = %d, want %d (body %s)", w.Code, http.StatusOK, w.Body.String())
	}

	var body struct {
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode healthz body: %v (body %q)", err, w.Body.String())
	}
	if body.Data.Status != "ok" {
		t.Errorf("GET /healthz: data.status = %q, want %q", body.Data.Status, "ok")
	}
}

func TestHealthz_DBPingFails_Returns503(t *testing.T) {
	env := newTestEnv(t, errors.New("connection refused"))

	w := env.do(t, http.MethodGet, "/healthz", "", "")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET /healthz: status = %d, want %d (body %s)", w.Code, http.StatusServiceUnavailable, w.Body.String())
	}
	if code := errorCode(t, w); code != response.CodeInternal {
		t.Errorf("GET /healthz: error.code = %q, want %q", code, response.CodeInternal)
	}
}

func TestUnknownRoute_ReturnsErrorEnvelope(t *testing.T) {
	env := newTestEnv(t, nil)

	w := env.do(t, http.MethodGet, "/does-not-exist", "", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("GET /does-not-exist: status = %d, want %d", w.Code, http.StatusNotFound)
	}
	if code := errorCode(t, w); code != response.CodeNotFound {
		t.Errorf("GET /does-not-exist: error.code = %q, want %q", code, response.CodeNotFound)
	}
}

func TestProtectedRoutes_MissingToken_Returns401(t *testing.T) {
	env := newTestEnv(t, nil)

	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"create post", http.MethodPost, "/posts", `{"title":"T","content":"C"}`},
		{"update post", http.MethodPut, "/posts/1", `{"title":"T","content":"C"}`},
		{"delete post", http.MethodDelete, "/posts/1", ""},
		{"create comment", http.MethodPost, "/posts/1/comments", `{"content":"C"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := env.do(t, tt.method, tt.path, tt.body, "")
			if w.Code != http.StatusUnauthorized {
				t.Errorf("%s %s: status = %d, want %d", tt.method, tt.path, w.Code, http.StatusUnauthorized)
			}
			if code := errorCode(t, w); code != response.CodeUnauthorized {
				t.Errorf("%s %s: error.code = %q, want %q", tt.method, tt.path, code, response.CodeUnauthorized)
			}
		})
	}
}
