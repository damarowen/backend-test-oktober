package identityhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"app/internal/identity"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

// --- test doubles -----------------------------------------------------------

// fakeRepo is an in-memory identity.UserRepository. It mirrors the MySQL unique
// index by rejecting duplicate emails with ErrEmailTaken.
type fakeRepo struct {
	byMail map[string]identity.User
	nextID int64

	getByEmailErr error
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{byMail: make(map[string]identity.User), nextID: 1}
}

func (f *fakeRepo) Create(_ context.Context, name, email, passwordHash string) (identity.User, error) {
	if _, ok := f.byMail[email]; ok {
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
	f.byMail[email] = u
	return u, nil
}

func (f *fakeRepo) GetByEmail(_ context.Context, email string) (identity.User, error) {
	if f.getByEmailErr != nil {
		return identity.User{}, f.getByEmailErr
	}
	u, ok := f.byMail[email]
	if !ok {
		return identity.User{}, identity.ErrUserNotFound
	}
	return u, nil
}

func (f *fakeRepo) GetByID(_ context.Context, id int64) (identity.User, error) {
	for _, u := range f.byMail {
		if u.ID == id {
			return u, nil
		}
	}
	return identity.User{}, identity.ErrUserNotFound
}

type stubTokens struct{}

func (stubTokens) Issue(userID int64) (string, error) { return fmt.Sprintf("token-%d", userID), nil }

func (stubTokens) Verify(token string) (int64, error) {
	var id int64
	if _, err := fmt.Sscanf(token, "token-%d", &id); err != nil {
		return 0, identity.ErrInvalidCredentials
	}
	return id, nil
}

// --- helpers ----------------------------------------------------------------

const testTTL = 24 * time.Hour

func newTestRouter(t *testing.T) (*gin.Engine, *fakeRepo) {
	t.Helper()
	return newTestRouterWithTTL(t, testTTL)
}

func newTestRouterWithTTL(t *testing.T, ttl time.Duration) (*gin.Engine, *fakeRepo) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	repo := newFakeRepo()
	svc := identity.NewService(repo, stubTokens{})

	r := gin.New()
	NewHandler(svc, ttl).RegisterAuth(r.Group("/"))
	return r, repo
}

// seedUser stores a user whose password hash really matches password, so login
// exercises the real bcrypt comparison.
func seedUser(t *testing.T, repo *fakeRepo, name, email, password string) identity.User {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	u, err := repo.Create(context.Background(), name, email, string(hash))
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return u
}

func postJSON(t *testing.T, r *gin.Engine, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

type registerEnvelope struct {
	Data struct {
		ID        int64  `json:"id"`
		Name      string `json:"name"`
		Email     string `json:"email"`
		CreatedAt string `json:"created_at"`
	} `json:"data"`
}

type loginEnvelope struct {
	Data struct {
		Token     string `json:"token"`
		TokenType string `json:"token_type"`
		ExpiresIn int    `json:"expires_in"`
	} `json:"data"`
}

type errorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func decodeInto(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("response is not valid JSON: %v\nbody: %s", err, rec.Body.String())
	}
}

// --- tests ------------------------------------------------------------------

func TestRegister_Created_201(t *testing.T) {
	r, _ := newTestRouter(t)

	rec := postJSON(t, r, "/register", `{"name":"Ada","email":"ada@example.com","password":"secret123"}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want 201 (body: %s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "password_hash") {
		t.Errorf("response must not contain password_hash: %s", rec.Body.String())
	}

	var env registerEnvelope
	decodeInto(t, rec, &env)
	if env.Data.ID <= 0 {
		t.Errorf("id: got %d, want > 0", env.Data.ID)
	}
	if env.Data.Name != "Ada" || env.Data.Email != "ada@example.com" {
		t.Errorf("data: got %+v, want name=Ada email=ada@example.com", env.Data)
	}
	if env.Data.CreatedAt == "" {
		t.Error("created_at must be present")
	}
}

func TestRegister_MalformedJSON_Returns400(t *testing.T) {
	r, _ := newTestRouter(t)

	rec := postJSON(t, r, "/register", `{"name":`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", rec.Code)
	}
}

func TestRegister_InvalidFields_Returns400_ValidationError(t *testing.T) {
	r, _ := newTestRouter(t)

	rec := postJSON(t, r, "/register", `{"name":"Ada","email":"ada@example.com","password":"short"}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400 (body: %s)", rec.Code, rec.Body.String())
	}
	var env errorEnvelope
	decodeInto(t, rec, &env)
	if env.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("code: got %q, want VALIDATION_ERROR", env.Error.Code)
	}
	if env.Error.Message != "Password must be at least 8 characters." {
		t.Errorf("message: got %q, want the validation message", env.Error.Message)
	}
}

func TestRegister_DuplicateEmail_Returns409_Conflict(t *testing.T) {
	r, _ := newTestRouter(t)
	const body = `{"name":"Ada","email":"ada@example.com","password":"secret123"}`

	postJSON(t, r, "/register", body)
	rec := postJSON(t, r, "/register", body)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status: got %d, want 409 (body: %s)", rec.Code, rec.Body.String())
	}
	var env errorEnvelope
	decodeInto(t, rec, &env)
	if env.Error.Code != "CONFLICT" {
		t.Errorf("code: got %q, want CONFLICT", env.Error.Code)
	}
}

func TestLogin_OK_ReturnsToken(t *testing.T) {
	r, repo := newTestRouter(t)
	seedUser(t, repo, "Ada", "ada@example.com", "secret123")

	rec := postJSON(t, r, "/login", `{"email":"ada@example.com","password":"secret123"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var env loginEnvelope
	decodeInto(t, rec, &env)
	if env.Data.Token == "" {
		t.Error("token must not be empty")
	}
	if env.Data.TokenType != "Bearer" {
		t.Errorf("token_type: got %q, want Bearer", env.Data.TokenType)
	}
	if env.Data.ExpiresIn != 86400 {
		t.Errorf("expires_in: got %d, want 86400", env.Data.ExpiresIn)
	}
}

// TestLogin_ExpiresIn_FollowsTTL pins that expires_in is derived from the
// injected token TTL rather than hardcoded: a handler built with a 2h lifetime
// must advertise 7200, not the 86400 of the default.
func TestLogin_ExpiresIn_FollowsTTL(t *testing.T) {
	r, repo := newTestRouterWithTTL(t, 2*time.Hour)
	seedUser(t, repo, "Ada", "ada@example.com", "secret123")

	rec := postJSON(t, r, "/login", `{"email":"ada@example.com","password":"secret123"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var env loginEnvelope
	decodeInto(t, rec, &env)
	if env.Data.ExpiresIn != 7200 {
		t.Errorf("expires_in: got %d, want 7200 — it must follow the injected TTL", env.Data.ExpiresIn)
	}
}

// TestLogin_UnhandledError_Returns500_AndLogs pins that an error the service
// does not recognise becomes a generic 500 with no internal detail, while the
// cause is recorded server-side so a 500 is never silent.
func TestLogin_UnhandledError_Returns500_AndLogs(t *testing.T) {
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	r, repo := newTestRouter(t)
	repo.getByEmailErr = errors.New("dial tcp 127.0.0.1:3306: connect: connection refused")

	rec := postJSON(t, r, "/login", `{"email":"ada@example.com","password":"secret123"}`)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500 (body: %s)", rec.Code, rec.Body.String())
	}
	var env errorEnvelope
	decodeInto(t, rec, &env)
	if env.Error.Code != "INTERNAL_ERROR" {
		t.Errorf("code: got %q, want INTERNAL_ERROR", env.Error.Code)
	}
	if strings.Contains(rec.Body.String(), "connection refused") {
		t.Errorf("body must not leak internal detail: %s", rec.Body.String())
	}

	logged := buf.String()
	if !strings.Contains(logged, "identity: unhandled error") {
		t.Errorf("the unhandled error must be logged; log was %q", logged)
	}
	if !strings.Contains(logged, "connection refused") {
		t.Errorf("the log must carry the cause; log was %q", logged)
	}
}

func TestLogin_BadPassword_Returns401_GenericMessage(t *testing.T) {
	r, repo := newTestRouter(t)
	seedUser(t, repo, "Ada", "ada@example.com", "secret123")

	badPassword := postJSON(t, r, "/login", `{"email":"ada@example.com","password":"wrongpass"}`)
	unknownEmail := postJSON(t, r, "/login", `{"email":"nobody@example.com","password":"secret123"}`)

	for name, rec := range map[string]*httptest.ResponseRecorder{
		"bad password":  badPassword,
		"unknown email": unknownEmail,
	} {
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: status got %d, want 401 (body: %s)", name, rec.Code, rec.Body.String())
		}
	}

	var a, b errorEnvelope
	decodeInto(t, badPassword, &a)
	decodeInto(t, unknownEmail, &b)
	if a.Error.Message != b.Error.Message {
		t.Errorf("wrong-password and unknown-email messages differ: %q vs %q", a.Error.Message, b.Error.Message)
	}
	if a.Error.Code != "UNAUTHORIZED" {
		t.Errorf("code: got %q, want UNAUTHORIZED", a.Error.Code)
	}
}
