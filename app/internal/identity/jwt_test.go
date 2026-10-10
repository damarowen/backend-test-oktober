package identity

import (
	"crypto/rand"
	"crypto/rsa"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const testSecret = "0123456789abcdef0123456789abcdef"

// signWith crafts a token with the given method, key, and claims so tests can
// exercise verification against deliberately malformed input.
func signWith(t *testing.T, method jwt.SigningMethod, key any, claims jwt.RegisteredClaims) string {
	t.Helper()
	signed, err := jwt.NewWithClaims(method, claims).SignedString(key)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

func validClaims(subject string) jwt.RegisteredClaims {
	now := time.Now()
	return jwt.RegisteredClaims{
		Subject:   subject,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
	}
}

func TestJWT_IssueAndVerify_RoundTrip(t *testing.T) {
	j := NewJWT(testSecret, time.Hour)

	token, err := j.Issue(42)
	if err != nil {
		t.Fatalf("Issue() unexpected error: %v", err)
	}
	if strings.Contains(token, "\n") {
		t.Error("token must be a single string")
	}

	id, err := j.Verify(token)
	if err != nil {
		t.Fatalf("Verify() unexpected error: %v", err)
	}
	if id != 42 {
		t.Errorf("user ID: got %d, want 42", id)
	}
}

func TestJWT_ExpiredTokenRejected(t *testing.T) {
	j := NewJWT(testSecret, -1*time.Minute)

	token, err := j.Issue(42)
	if err != nil {
		t.Fatalf("Issue() unexpected error: %v", err)
	}

	if _, err := j.Verify(token); err == nil {
		t.Error("expired token: expected error, got nil")
	}
}

// TestJWT_MissingExpiryRejected pins that a correctly signed token with no
// "exp" claim is rejected — jwt/v5 accepts missing exp unless the parse option
// requires it.
func TestJWT_MissingExpiryRejected(t *testing.T) {
	j := NewJWT(testSecret, time.Hour)
	now := time.Now()
	token := signWith(t, jwt.SigningMethodHS256, []byte(testSecret), jwt.RegisteredClaims{
		Subject:  "7",
		IssuedAt: jwt.NewNumericDate(now),
		// no ExpiresAt
	})

	if _, err := j.Verify(token); err == nil {
		t.Error("token without exp: expected error, got nil")
	}
}

func TestJWT_InvalidSubjectRejected(t *testing.T) {
	subjects := []string{"", "abc", "0", "-1", "1.5", " 7"}

	for _, subject := range subjects {
		t.Run("subject="+subject, func(t *testing.T) {
			j := NewJWT(testSecret, time.Hour)
			token := signWith(t, jwt.SigningMethodHS256, []byte(testSecret), validClaims(subject))

			if _, err := j.Verify(token); err == nil {
				t.Errorf("subject %q: expected error, got nil", subject)
			}
		})
	}
}

func TestJWT_TamperedSignatureRejected(t *testing.T) {
	j := NewJWT(testSecret, time.Hour)

	token, err := j.Issue(42)
	if err != nil {
		t.Fatalf("Issue() unexpected error: %v", err)
	}

	tampered := token[:len(token)-2] + "aa"
	if _, err := j.Verify(tampered); err == nil {
		t.Error("tampered token: expected error, got nil")
	}
}

func TestJWT_WrongAlgorithmRejected(t *testing.T) {
	j := NewJWT(testSecret, time.Hour)
	claims := validClaims("7")

	t.Run("alg none", func(t *testing.T) {
		token := signWith(t, jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, claims)
		if _, err := j.Verify(token); err == nil {
			t.Error("alg=none token: expected error, got nil")
		}
	})

	t.Run("alg RS256", func(t *testing.T) {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("generate RSA key: %v", err)
		}
		token := signWith(t, jwt.SigningMethodRS256, key, claims)
		if _, err := j.Verify(token); err == nil {
			t.Error("RS256 token: expected error, got nil")
		}
	})

	t.Run("secret mismatch", func(t *testing.T) {
		other := NewJWT("some-other-secret-value-32-bytes-ok", time.Hour)
		token, err := other.Issue(7)
		if err != nil {
			t.Fatalf("Issue() unexpected error: %v", err)
		}
		if _, err := j.Verify(token); err == nil {
			t.Error("token signed with another secret: expected error, got nil")
		}
	})
}
