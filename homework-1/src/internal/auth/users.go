package auth

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	ErrUserExists         = errors.New("username already taken")
	ErrInvalidCredentials = errors.New("invalid username or password")
)

// PBKDF2 parameters (OWASP 2023 recommendation for PBKDF2-HMAC-SHA256).
const (
	pbkdf2Iterations = 600_000
	saltLen          = 16
	keyLen           = 32
)

var usernameRe = regexp.MustCompile(`^[a-zA-Z0-9_.-]{3,32}$`)

// User is a registered API user. The hash is never serialized.
type User struct {
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"createdAt"`
	hash      string
}

// Users is an in-memory user registry.
type Users struct {
	mu    sync.RWMutex
	users map[string]User
	iter  int
	dummy string // hash used to keep login timing equal for unknown users
}

// NewUsers creates a registry. iterations <= 0 uses the secure default;
// tests pass a small number to stay fast.
func NewUsers(iterations int) *Users {
	iter := iterations
	if iter <= 0 {
		iter = pbkdf2Iterations
	}
	u := &Users{users: map[string]User{}, iter: iter}
	u.dummy, _ = u.hashPassword("dummy-password")
	return u
}

// ValidateCredentials returns human-readable problems with a username/password pair.
func ValidateCredentials(username, password string) map[string]string {
	errs := map[string]string{}
	if !usernameRe.MatchString(username) {
		errs["username"] = "Username must be 3-32 characters: letters, digits, '_', '.', '-'"
	}
	if n := len(password); n < 8 || n > 72 {
		errs["password"] = "Password must be 8-72 characters long"
	}
	return errs
}

// Register creates a user. Usernames are case-insensitive.
func (u *Users) Register(username, password string) (User, error) {
	key := strings.ToLower(username)
	hash, err := u.hashPassword(password) // hash outside the lock: it's slow on purpose
	if err != nil {
		return User{}, err
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if _, ok := u.users[key]; ok {
		return User{}, ErrUserExists
	}
	user := User{Username: key, CreatedAt: time.Now().UTC(), hash: hash}
	u.users[key] = user
	return user, nil
}

// Authenticate checks a password and returns the user.
func (u *Users) Authenticate(username, password string) (User, error) {
	u.mu.RLock()
	user, ok := u.users[strings.ToLower(username)]
	u.mu.RUnlock()
	hash := user.hash
	if !ok {
		hash = u.dummy // still do the work so response time doesn't reveal which usernames exist
	}
	if !u.checkPassword(password, hash) || !ok {
		return User{}, ErrInvalidCredentials
	}
	return user, nil
}

// hash format: pbkdf2-sha256$<iterations>$<salt b64>$<key b64>
func (u *Users) hashPassword(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, u.iter, keyLen)
	if err != nil {
		return "", err
	}
	enc := base64.RawStdEncoding
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", u.iter, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

func (u *Users) checkPassword(password, encoded string) bool {
	var iter int
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	if _, err := fmt.Sscan(parts[1], &iter); err != nil {
		return false
	}
	enc := base64.RawStdEncoding
	salt, err1 := enc.DecodeString(parts[2])
	want, err2 := enc.DecodeString(parts[3])
	if err1 != nil || err2 != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iter, len(want))
	return err == nil && hmac.Equal(got, want)
}
