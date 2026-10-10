package identity

import "time"

// User is the persisted identity of an account. PasswordHash never leaves this
// package in an HTTP response; handlers map User to a public DTO.
type User struct {
	ID           int64
	Name         string
	Email        string
	PasswordHash string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
