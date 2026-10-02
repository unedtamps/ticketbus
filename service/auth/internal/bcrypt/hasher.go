package bcrypt

import (
	"golang.org/x/crypto/bcrypt"
)

// Hasher implements domain.PasswordHasher using bcrypt.
type Hasher struct{}

// NewHasher creates a new bcrypt hasher.
func NewHasher() *Hasher {
	return &Hasher{}
}

// Hash hashes a password with bcrypt.
func (h *Hasher) Hash(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// Verify checks a password against a bcrypt hash.
func (h *Hasher) Verify(password, hash string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}
