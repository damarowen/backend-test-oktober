package identity

import (
	"context"
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

const validPassword = "password123" // 11 chars, letters + digits — valid under the 8-15 rule

func TestRegister_Success(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, &stubTokens{})

	u, err := svc.Register(context.Background(), "  Budi  ", "  Budi@Example.COM ", validPassword)
	if err != nil {
		t.Fatalf("Register() unexpected error: %v", err)
	}
	if u.Name != "Budi" {
		t.Errorf("Name: got %q, want %q", u.Name, "Budi")
	}
	if u.Email != "budi@example.com" {
		t.Errorf("Email: got %q, want normalized %q", u.Email, "budi@example.com")
	}
	if u.PasswordHash == validPassword {
		t.Error("PasswordHash must not be the plaintext password")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(validPassword)); err != nil {
		t.Errorf("stored hash does not verify against the password: %v", err)
	}
}

func TestRegister_InvalidInput(t *testing.T) {
	tests := []struct {
		name     string
		userName string
		email    string
		password string
	}{
		{"empty name", "", "a@b.com", validPassword},
		{"whitespace name", "   ", "a@b.com", validPassword},
		{"name too long", strings.Repeat("a", 101), "a@b.com", validPassword},
		{"invalid email", "Budi", "not-an-email", validPassword},
		{"email too long", "Budi", strings.Repeat("a", 64) + "@" + strings.Repeat("b", 200) + ".com", validPassword},
		{"empty password", "Budi", "a@b.com", ""},
		{"password too short", "Budi", "a@b.com", "short7"},
		{"password too long", "Budi", "a@b.com", strings.Repeat("a", 16)},
		{"password with symbol", "Budi", "a@b.com", "passw0rd!"},
		{"password with space", "Budi", "a@b.com", "pass word1"},
		{"password with non-ascii", "Budi", "a@b.com", "pässw0rd1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewService(newFakeRepo(), &stubTokens{})
			_, err := svc.Register(context.Background(), tt.userName, tt.email, tt.password)
			if !errors.Is(err, ErrInvalidInput) {
				t.Errorf("got %v, want ErrInvalidInput", err)
			}
		})
	}
}

func TestRegister_PasswordBoundaries(t *testing.T) {
	tests := []struct {
		password string
		wantOK   bool
	}{
		{strings.Repeat("a", 7), false},
		{strings.Repeat("a", 8), true},
		{strings.Repeat("a", 15), true},
		{strings.Repeat("a", 16), false},
	}

	for _, tt := range tests {
		svc := NewService(newFakeRepo(), &stubTokens{})
		_, err := svc.Register(context.Background(), "Budi", "a@b.com", tt.password)
		if tt.wantOK && err != nil {
			t.Errorf("password %d chars: got %v, want success", len(tt.password), err)
		}
		if !tt.wantOK && !errors.Is(err, ErrInvalidInput) {
			t.Errorf("password %d chars: got %v, want ErrInvalidInput", len(tt.password), err)
		}
	}
}

// TestRegister_PasswordMessages pins the user-facing wording. Messages must
// never mention "bytes" and must tell the user exactly what to fix.
func TestRegister_PasswordMessages(t *testing.T) {
	tests := []struct {
		name     string
		password string
		want     string
	}{
		{"too short", "short7", "Password must be at least 8 characters."},
		{"too long", strings.Repeat("a", 16), "Password must be at most 15 characters."},
		{"symbol", "passw0rd!", "Password can only contain letters and numbers."},
		{"space", "pass word1", "Password can only contain letters and numbers."},
		{"non-ascii", "pässw0rd1", "Password can only contain letters and numbers."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewService(newFakeRepo(), &stubTokens{})
			_, err := svc.Register(context.Background(), "Budi", "a@b.com", tt.password)
			if err == nil {
				t.Fatalf("password %q: expected error, got nil", tt.password)
			}
			if err.Error() != tt.want {
				t.Errorf("message: got %q, want %q", err.Error(), tt.want)
			}
			if strings.Contains(strings.ToLower(err.Error()), "byte") {
				t.Errorf("message %q must not use byte wording", err.Error())
			}
		})
	}
}

func TestRegister_DuplicateEmail(t *testing.T) {
	svc := NewService(newFakeRepo(), &stubTokens{})
	ctx := context.Background()

	if _, err := svc.Register(ctx, "Budi", "budi@example.com", validPassword); err != nil {
		t.Fatalf("first Register() unexpected error: %v", err)
	}

	// Different casing must still collide after normalization.
	_, err := svc.Register(ctx, "Budi Again", "BUDI@Example.com", validPassword)
	if !errors.Is(err, ErrEmailTaken) {
		t.Errorf("got %v, want ErrEmailTaken", err)
	}
}

func TestLogin_Success(t *testing.T) {
	tokens := &stubTokens{}
	svc := NewService(newFakeRepo(), tokens)
	ctx := context.Background()

	u, err := svc.Register(ctx, "Budi", "budi@example.com", validPassword)
	if err != nil {
		t.Fatalf("Register() unexpected error: %v", err)
	}

	token, err := svc.Login(ctx, " budi@example.com ", validPassword)
	if err != nil {
		t.Fatalf("Login() unexpected error: %v", err)
	}
	if token != "token-1" {
		t.Errorf("token: got %q, want %q", token, "token-1")
	}
	if tokens.issued != u.ID {
		t.Errorf("issued user ID: got %d, want %d", tokens.issued, u.ID)
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	svc := NewService(newFakeRepo(), &stubTokens{})
	ctx := context.Background()

	if _, err := svc.Register(ctx, "Budi", "budi@example.com", validPassword); err != nil {
		t.Fatalf("Register() unexpected error: %v", err)
	}

	_, err := svc.Login(ctx, "budi@example.com", "wrong-pass1")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("got %v, want ErrInvalidCredentials", err)
	}
}

func TestLogin_UnknownEmail(t *testing.T) {
	svc := NewService(newFakeRepo(), &stubTokens{})

	_, err := svc.Login(context.Background(), "nobody@example.com", validPassword)
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("got %v, want ErrInvalidCredentials (no user enumeration)", err)
	}
}

func TestLogin_RepositoryError_Propagates(t *testing.T) {
	repo := newFakeRepo()
	repo.getByEmailErr = errors.New("db down")
	svc := NewService(repo, &stubTokens{})

	_, err := svc.Login(context.Background(), "budi@example.com", validPassword)
	if err == nil || errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("infrastructure error must propagate (got %v), not be masked as invalid credentials", err)
	}
}

func TestUserName_Found(t *testing.T) {
	svc := NewService(newFakeRepo(), &stubTokens{})
	ctx := context.Background()

	u, err := svc.Register(ctx, "Budi", "budi@example.com", validPassword)
	if err != nil {
		t.Fatalf("Register() unexpected error: %v", err)
	}

	name, err := svc.UserName(ctx, u.ID)
	if err != nil {
		t.Fatalf("UserName() unexpected error: %v", err)
	}
	if name != "Budi" {
		t.Errorf("Name: got %q, want %q", name, "Budi")
	}
}

func TestUserName_NotFound(t *testing.T) {
	svc := NewService(newFakeRepo(), &stubTokens{})

	_, err := svc.UserName(context.Background(), 999)
	if !errors.Is(err, ErrUserNotFound) {
		t.Errorf("got %v, want ErrUserNotFound", err)
	}
}
