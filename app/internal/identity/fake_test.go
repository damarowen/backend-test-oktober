package identity

import (
	"context"
	"fmt"
	"sync"
)

// fakeRepo is a hand-rolled in-memory UserRepository used for isolated service
// tests. It enforces the same uniqueness rule the MySQL unique index enforces.
type fakeRepo struct {
	mu     sync.Mutex
	users  map[int64]User
	byMail map[string]int64
	nextID int64

	createErr     error
	getByEmailErr error
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		users:  make(map[int64]User),
		byMail: make(map[string]int64),
		nextID: 1,
	}
}

func (f *fakeRepo) Create(_ context.Context, name, email, passwordHash string) (User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.createErr != nil {
		return User{}, f.createErr
	}
	if _, ok := f.byMail[email]; ok {
		return User{}, ErrEmailTaken
	}

	u := User{ID: f.nextID, Name: name, Email: email, PasswordHash: passwordHash}
	f.nextID++
	f.users[u.ID] = u
	f.byMail[u.Email] = u.ID
	return u, nil
}

func (f *fakeRepo) GetByEmail(_ context.Context, email string) (User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.getByEmailErr != nil {
		return User{}, f.getByEmailErr
	}
	id, ok := f.byMail[email]
	if !ok {
		return User{}, ErrUserNotFound
	}
	return f.users[id], nil
}

func (f *fakeRepo) GetByID(_ context.Context, id int64) (User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	u, ok := f.users[id]
	if !ok {
		return User{}, ErrUserNotFound
	}
	return u, nil
}

// stubTokens is a deterministic TokenService that records the issued user ID.
type stubTokens struct {
	issued   int64
	issueErr error
}

func (t *stubTokens) Issue(userID int64) (string, error) {
	if t.issueErr != nil {
		return "", t.issueErr
	}
	t.issued = userID
	return fmt.Sprintf("token-%d", userID), nil
}

func (t *stubTokens) Verify(token string) (int64, error) {
	var id int64
	if _, err := fmt.Sscanf(token, "token-%d", &id); err != nil {
		return 0, ErrInvalidCredentials
	}
	return id, nil
}
