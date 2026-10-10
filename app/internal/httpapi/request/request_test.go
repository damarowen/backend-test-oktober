package request

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func testContext(t *testing.T, target string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, target, nil)
	return c, rec
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("response is not the error envelope: %v\nbody: %s", err, rec.Body.String())
	}
	return env.Error.Code
}

// TestPagination pins the strict presence rule — the single source of truth for
// every list endpoint: absent → defaults; present → integer in range.
func TestPagination(t *testing.T) {
	tests := []struct {
		name      string
		target    string
		wantPage  int
		wantLimit int
		wantOK    bool
	}{
		{"absent defaults", "/posts", 1, 10, true},
		{"explicit values pass", "/posts?page=2&limit=5", 2, 5, true},
		{"max limit accepted", "/posts?limit=100", 1, 100, true},
		{"page zero", "/posts?page=0", 0, 0, false},
		{"page negative", "/posts?page=-1", 0, 0, false},
		{"page non numeric", "/posts?page=abc", 0, 0, false},
		{"page empty value", "/posts?page=", 0, 0, false},
		{"limit zero", "/posts?limit=0", 0, 0, false},
		{"limit negative", "/posts?limit=-1", 0, 0, false},
		{"limit over max", "/posts?limit=101", 0, 0, false},
		{"limit non numeric", "/posts?limit=abc", 0, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, rec := testContext(t, tt.target)

			page, limit, ok := Pagination(c)

			if ok != tt.wantOK {
				t.Fatalf("ok: got %v, want %v (body: %s)", ok, tt.wantOK, rec.Body.String())
			}
			if !ok {
				if rec.Code != http.StatusBadRequest {
					t.Errorf("status: got %d, want 400", rec.Code)
				}
				if code := errorCode(t, rec); code != "VALIDATION_ERROR" {
					t.Errorf("code: got %q, want VALIDATION_ERROR", code)
				}
				return
			}
			if page != tt.wantPage || limit != tt.wantLimit {
				t.Errorf("got page=%d limit=%d, want %d/%d", page, limit, tt.wantPage, tt.wantLimit)
			}
		})
	}
}

func TestPostID(t *testing.T) {
	tests := []struct {
		raw    string
		want   int64
		wantOK bool
	}{
		{"42", 42, true},
		{"1", 1, true},
		{"abc", 0, false},
		{"0", 0, false},
		{"-1", 0, false},
		{"", 0, false},
	}

	for _, tt := range tests {
		t.Run("id="+tt.raw, func(t *testing.T) {
			c, rec := testContext(t, "/posts/x")
			c.Params = gin.Params{{Key: "id", Value: tt.raw}}

			id, ok := PostID(c)

			if ok != tt.wantOK {
				t.Fatalf("ok: got %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				if rec.Code != http.StatusBadRequest {
					t.Errorf("status: got %d, want 400", rec.Code)
				}
				return
			}
			if id != tt.want {
				t.Errorf("id: got %d, want %d", id, tt.want)
			}
		})
	}
}

// The success path of UserID requires the auth middleware (its context key is
// unexported), so it is covered by the module handler tests; here only the
// defensive failure path can be exercised directly.
func TestUserID_MissingContext_Writes401(t *testing.T) {
	c, rec := testContext(t, "/posts")

	id, ok := UserID(c)

	if ok {
		t.Fatalf("ok: got true (id %d), want false", id)
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want 401", rec.Code)
	}
	if code := errorCode(t, rec); code != "UNAUTHORIZED" {
		t.Errorf("code: got %q, want UNAUTHORIZED", code)
	}
}
