package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"app/internal/identity"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const testSecret = "0123456789abcdef0123456789abcdef"

// protectedRoute wires Auth in front of a handler that records whether it ran.
// If the middleware fails to abort the chain, executed becomes true and the
// abort tests fail.
type protectedRoute struct {
	engine     *gin.Engine
	executed   bool
	recordedID int64
	sawID      bool
}

func newProtectedRoute(t *testing.T) *protectedRoute {
	t.Helper()
	gin.SetMode(gin.TestMode)

	tokens := identity.NewJWT(testSecret, time.Hour)
	route := &protectedRoute{}

	r := gin.New()
	r.GET("/private", Auth(tokens), func(c *gin.Context) {
		route.executed = true
		if id, ok := UserID(c); ok {
			route.recordedID, route.sawID = id, true
		}
		c.JSON(http.StatusOK, gin.H{"data": "ok"})
	})
	route.engine = r
	return route
}

func (p *protectedRoute) get(t *testing.T, authHeader string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/private", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	p.engine.ServeHTTP(rec, req)
	return rec
}

// craft signs a token directly so tests can build inputs the service would
// never produce (bad subjects, missing exp).
func craft(t *testing.T, method jwt.SigningMethod, key any, claims jwt.RegisteredClaims) string {
	t.Helper()
	signed, err := jwt.NewWithClaims(method, claims).SignedString(key)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

func validToken(t *testing.T, subject string) string {
	t.Helper()
	now := time.Now()
	return craft(t, jwt.SigningMethodHS256, []byte(testSecret), jwt.RegisteredClaims{
		Subject:   subject,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
	})
}

func TestAuth_MissingHeader_Returns401(t *testing.T) {
	p := newProtectedRoute(t)
	rec := p.get(t, "")

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want 401", rec.Code)
	}
	if p.executed {
		t.Error("protected handler must not run without a token")
	}
}

func TestAuth_MalformedHeader_Returns401(t *testing.T) {
	headers := []string{"Token abc", "Bearer", "Bearer ", "Basic dXNlcjpwYXNz"}

	for _, header := range headers {
		t.Run("header="+header, func(t *testing.T) {
			p := newProtectedRoute(t)
			if rec := p.get(t, header); rec.Code != http.StatusUnauthorized {
				t.Errorf("status: got %d, want 401", rec.Code)
			}
			if p.executed {
				t.Error("protected handler must not run for a malformed header")
			}
		})
	}
}

func TestAuth_InvalidToken_Returns401(t *testing.T) {
	p := newProtectedRoute(t)
	if rec := p.get(t, "Bearer not-a-jwt"); rec.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want 401", rec.Code)
	}
	if p.executed {
		t.Error("protected handler must not run for an invalid token")
	}
}

func TestAuth_ValidToken_SetsUserID(t *testing.T) {
	p := newProtectedRoute(t)
	rec := p.get(t, "Bearer "+validToken(t, "7"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rec.Code)
	}
	if !p.executed {
		t.Fatal("protected handler must run for a valid token")
	}
	if !p.sawID || p.recordedID != 7 {
		t.Errorf("UserID: got (%d, %v), want (7, true)", p.recordedID, p.sawID)
	}
}

// TestAuth_Failure_AbortsChain is the safety net: on every rejection the
// protected handler must never execute, not merely receive a 401 response.
func TestAuth_Failure_AbortsChain(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name   string
		header string
	}{
		{"missing header", ""},
		{"wrong scheme", "Token abc"},
		{"garbage token", "Bearer not-a-jwt"},
		{"empty token", "Bearer "},
		{"subject zero", "Bearer " + craft(t, jwt.SigningMethodHS256, []byte(testSecret), jwt.RegisteredClaims{
			Subject: "0", IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour))})},
		{"subject negative", "Bearer " + craft(t, jwt.SigningMethodHS256, []byte(testSecret), jwt.RegisteredClaims{
			Subject: "-5", IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour))})},
		{"subject garbage", "Bearer " + craft(t, jwt.SigningMethodHS256, []byte(testSecret), jwt.RegisteredClaims{
			Subject: "abc", IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour))})},
		{"missing exp", "Bearer " + craft(t, jwt.SigningMethodHS256, []byte(testSecret), jwt.RegisteredClaims{
			Subject: "7", IssuedAt: jwt.NewNumericDate(now)})},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newProtectedRoute(t)
			rec := p.get(t, tc.header)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status: got %d, want 401", rec.Code)
			}
			if p.executed {
				t.Error("protected handler executed — the middleware must abort the chain")
			}
		})
	}
}
