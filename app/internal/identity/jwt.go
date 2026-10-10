package identity

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// errInvalidToken is what every verification failure returns. It is
// deliberately generic so callers learn nothing about why a token failed.
var errInvalidToken = errors.New("identity: invalid token")

// JWT issues and verifies HS256-signed tokens carrying the user ID in the
// registered "sub" claim. It satisfies TokenService.
type JWT struct {
	secret []byte
	ttl    time.Duration
}

func NewJWT(secret string, ttl time.Duration) *JWT {
	return &JWT{secret: []byte(secret), ttl: ttl}
}

// Issue signs a token for the given user ID.
func (j *JWT) Issue(userID int64) (string, error) {
	if userID <= 0 {
		return "", errInvalidToken
	}

	now := time.Now()
	claims := jwt.RegisteredClaims{
		Subject:   strconv.FormatInt(userID, 10),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(j.ttl)),
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(j.secret)
	if err != nil {
		return "", fmt.Errorf("identity: sign token: %w", err)
	}
	return signed, nil
}

// Verify validates a token and returns the user ID it identifies.
//
// Strictness rules:
//   - HS256 only; the algorithm is never trusted from the token header.
//   - "exp" is required: a correctly signed token with no expiry is rejected.
//   - "sub" must parse to a positive int64; anything else is rejected.
//
// Every failure returns the same generic error.
func (j *JWT) Verify(token string) (int64, error) {
	claims := &jwt.RegisteredClaims{}

	parsed, err := jwt.ParseWithClaims(
		token,
		claims,
		func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, errInvalidToken
			}
			return j.secret, nil
		},
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithExpirationRequired(),
	)
	if err != nil || !parsed.Valid {
		return 0, errInvalidToken
	}

	userID, parseErr := strconv.ParseInt(claims.Subject, 10, 64)
	if parseErr != nil || userID <= 0 {
		return 0, errInvalidToken
	}

	return userID, nil
}
