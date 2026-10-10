package response

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"app/internal/comments"
	"app/internal/identity"
	"app/internal/posts"

	"github.com/gin-gonic/gin"
)

// run builds a single-route engine that writes the error mapping for err and
// returns the recorded response.
func run(t *testing.T, err error) (int, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/x", nil)

	FromError(c, err)
	return rec.Code, rec.Body.String()
}

type errorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func decodeError(t *testing.T, body string) errorEnvelope {
	t.Helper()
	var env errorEnvelope
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("response is not the error envelope: %v\nbody: %s", err, body)
	}
	return env
}

func TestFromError_MapsSentinels(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		status   int
		wantCode string
	}{
		{"validation", identity.ErrInvalidInput, http.StatusBadRequest, "VALIDATION_ERROR"},
		{"credentials", identity.ErrInvalidCredentials, http.StatusUnauthorized, "UNAUTHORIZED"},
		{"user not found", identity.ErrUserNotFound, http.StatusNotFound, "NOT_FOUND"},
		{"email taken", identity.ErrEmailTaken, http.StatusConflict, "CONFLICT"},
		{"posts validation", posts.ErrInvalidInput, http.StatusBadRequest, "VALIDATION_ERROR"},
		{"posts forbidden", posts.ErrForbidden, http.StatusForbidden, "FORBIDDEN"},
		{"posts not found", posts.ErrNotFound, http.StatusNotFound, "NOT_FOUND"},
		{"comments validation", comments.ErrInvalidInput, http.StatusBadRequest, "VALIDATION_ERROR"},
		{"comments post not found", comments.ErrPostNotFound, http.StatusNotFound, "NOT_FOUND"},
		{"comments author not found", comments.ErrAuthorNotFound, http.StatusNotFound, "NOT_FOUND"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := run(t, tt.err)
			if status != tt.status {
				t.Errorf("status: got %d, want %d", status, tt.status)
			}
			if code := decodeError(t, body).Error.Code; code != tt.wantCode {
				t.Errorf("code: got %q, want %q", code, tt.wantCode)
			}
		})
	}
}

func TestFromError_ValidationMessage_Surfaced(t *testing.T) {
	want := "Password must be at least 8 characters."
	status, body := run(t, &identity.ValidationError{Message: want})

	if status != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", status, http.StatusBadRequest)
	}
	if msg := decodeError(t, body).Error.Message; msg != want {
		t.Errorf("message: got %q, want %q", msg, want)
	}
	if strings.Contains(strings.ToLower(body), "byte") {
		t.Errorf("body %q must not use byte wording", body)
	}
}

// TestFromError_PostsValidationMessage_Surfaced pins that a posts validation
// error reaches the client with its specific wording, not the generic message.
func TestFromError_PostsValidationMessage_Surfaced(t *testing.T) {
	want := "Title is required."
	status, body := run(t, &posts.ValidationError{Message: want})

	if status != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", status, http.StatusBadRequest)
	}
	if msg := decodeError(t, body).Error.Message; msg != want {
		t.Errorf("message: got %q, want %q", msg, want)
	}
}

// TestFromError_CommentsValidationMessage_Surfaced pins that a comments
// validation error reaches the client with its specific wording.
func TestFromError_CommentsValidationMessage_Surfaced(t *testing.T) {
	want := "Content is required."
	status, body := run(t, &comments.ValidationError{Message: want})

	if status != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", status, http.StatusBadRequest)
	}
	if msg := decodeError(t, body).Error.Message; msg != want {
		t.Errorf("message: got %q, want %q", msg, want)
	}
}

func TestFromError_Unknown_Returns500Generic(t *testing.T) {
	status, body := run(t, errors.New("dial tcp 10.0.0.5:3306: connection refused"))

	if status != http.StatusInternalServerError {
		t.Errorf("status: got %d, want %d", status, http.StatusInternalServerError)
	}
	env := decodeError(t, body)
	if env.Error.Code != "INTERNAL_ERROR" {
		t.Errorf("code: got %q, want INTERNAL_ERROR", env.Error.Code)
	}
	if strings.Contains(body, "dial tcp") || strings.Contains(body, "10.0.0.5") {
		t.Errorf("body %q must not leak internal error detail", body)
	}
}

func TestPaginated_Envelope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/x", nil)

	Paginated(c, []string{"a", "b"}, 2, 10, 25)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rec.Code)
	}
	var env struct {
		Data       []string `json:"data"`
		Pagination struct {
			Page  int   `json:"page"`
			Limit int   `json:"limit"`
			Total int64 `json:"total"`
		} `json:"pagination"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("body is not the paginated envelope: %v", err)
	}
	if env.Pagination.Page != 2 || env.Pagination.Limit != 10 || env.Pagination.Total != 25 {
		t.Errorf("pagination: got %+v, want page=2 limit=10 total=25", env.Pagination)
	}
	if len(env.Data) != 2 {
		t.Errorf("data: got %v, want 2 items", env.Data)
	}
}
