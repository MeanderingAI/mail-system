package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"mail-server/internal/config"
)

// Authenticator handles user authentication
type Authenticator struct {
	Users map[string]config.User
}

// NewAuthenticator creates a new authenticator with user data
func NewAuthenticator(users map[string]config.User) *Authenticator {
	return &Authenticator{
		Users: users,
	}
}

// Authenticate verifies username and password
func (a *Authenticator) Authenticate(username, password string) bool {
	user, exists := a.Users[username]
	if !exists {
		return false
	}

	return user.Password == password
}

// GetUser returns user information if authenticated
func (a *Authenticator) GetUser(username string) (config.User, bool) {
	user, exists := a.Users[username]
	return user, exists
}

// GenerateMessageID generates a unique message ID
func GenerateMessageID(domain string) string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return hex.EncodeToString(bytes) + "@" + domain
}

// HashPassword creates a SHA256 hash of the password (simple implementation)
func HashPassword(password string) string {
	hash := sha256.Sum256([]byte(password))
	return hex.EncodeToString(hash[:])
}

// ValidateEmail performs basic email validation
func ValidateEmail(email string) error {
	if len(email) == 0 {
		return fmt.Errorf("email cannot be empty")
	}

	// Basic validation - contains @ symbol
	found := false
	for _, char := range email {
		if char == '@' {
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("invalid email format")
	}

	return nil
}
