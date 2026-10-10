package identity

import "context"

// UserRepository is the persistence port for users. The MySQL adapter
// implements it; services depend only on this interface.
type UserRepository interface {
	Create(ctx context.Context, name, email, passwordHash string) (User, error)
	GetByEmail(ctx context.Context, email string) (User, error)
	GetByID(ctx context.Context, id int64) (User, error)
}

// TokenService issues and verifies authentication tokens. The JWT
// implementation (jwt.go) and test stubs both satisfy it.
type TokenService interface {
	Issue(userID int64) (string, error)
	Verify(token string) (int64, error)
}
