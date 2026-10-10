package identity

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

// Password policy: 8-15 characters, letters and digits only. These rules and
// their messages reach the end user, so they use plain wording and never
// mention technical units. The 15-character maximum is far below bcrypt's
// input limit, so no truncation is possible.
const (
	minPasswordLength = 8
	maxPasswordLength = 15
	maxNameLength     = 50
	maxEmailLength    = 50
)

// passwordCharset restricts passwords to ASCII letters and digits.
var passwordCharset = regexp.MustCompile(`^[A-Za-z0-9]+$`)

// Service implements user registration, login, and identity lookup. It depends
// only on the UserRepository and TokenService ports.
type Service struct {
	repo   UserRepository
	tokens TokenService
}

func NewService(repo UserRepository, tokens TokenService) *Service {
	return &Service{repo: repo, tokens: tokens}
}

// Register validates and normalizes the input, hashes the password, and stores
// the user. Duplicate emails surface as ErrEmailTaken from the repository.
func (s *Service) Register(ctx context.Context, name, email, password string) (User, error) {
	name = strings.TrimSpace(name)
	email = normalizeEmail(email)

	if err := validateRegistration(name, email, password); err != nil {
		return User{}, err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, fmt.Errorf("identity: hash password: %w", err)
	}

	return s.repo.Create(ctx, name, email, string(hash))
}

// Login verifies credentials and returns a signed token. An unknown email and a
// wrong password both return ErrInvalidCredentials; infrastructure errors
// propagate so they are not misreported as 401.
func (s *Service) Login(ctx context.Context, email, password string) (string, error) {
	email = normalizeEmail(email)

	user, err := s.repo.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return "", ErrInvalidCredentials
		}
		return "", err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return "", ErrInvalidCredentials
	}

	return s.tokens.Issue(user.ID)
}

// UserName returns a user's display name, used by the comments module to store
// an author-name snapshot.
func (s *Service) UserName(ctx context.Context, userID int64) (string, error) {
	user, err := s.repo.GetByID(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return "", ErrUserNotFound
		}
		return "", err
	}
	return user.Name, nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func validateRegistration(name, email, password string) error {
	if name == "" || len([]rune(name)) > maxNameLength {
		return invalidf("Name must be between 1 and %d characters.", maxNameLength)
	}
	if !isValidEmail(email) {
		return invalidf("Email address is not valid.")
	}
	if len(email) > maxEmailLength {
		return invalidf("Email address must be at most %d characters.", maxEmailLength)
	}

	// Length is counted in characters. The charset check enforces ASCII, so
	// character count and byte count coincide for every accepted password.
	n := utf8.RuneCountInString(password)
	if n < minPasswordLength {
		return invalidf("Password must be at least %d characters.", minPasswordLength)
	}
	if !passwordCharset.MatchString(password) {
		return invalidf("Password can only contain letters and numbers.")
	}
	if n > maxPasswordLength {
		return invalidf("Password must be at most %d characters.", maxPasswordLength)
	}
	return nil
}

func isValidEmail(email string) bool {
	addr, err := mail.ParseAddress(email)
	if err != nil {
		return false
	}
	return addr.Address == email
}
