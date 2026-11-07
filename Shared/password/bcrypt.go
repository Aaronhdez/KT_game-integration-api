package password

import (
	"errors"
	"fmt"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
	"golang.org/x/crypto/bcrypt"
)

// maxPasswordBytes defines the maximum allowed length for a password.
const maxPasswordBytes = 72

type Bcrypt struct{}

var _ domain.PasswordHasher = Bcrypt{}

// NewBcrypt creates a new instance of the Bcrypt password hasher.
func NewBcrypt() Bcrypt {
	return Bcrypt{}
}

// Verify checks if the provided password matches the hashed password.
// It returns an error if:
// - Password is too long
// - Password does not match
// - There is an issue comparing the hash
func (Bcrypt) Verify(passwordHash, password string) error {
	if len(password) > maxPasswordBytes {
		return fmt.Errorf("password exceeds maximum length of %d: %w", maxPasswordBytes, domain.ErrInvalidCredentials)
	}
	err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password))
	if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		return domain.ErrInvalidCredentials
	}
	if err != nil {
		return fmt.Errorf("error comparing password hash: %w", err)
	}
	return nil
}
