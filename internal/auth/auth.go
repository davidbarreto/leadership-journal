// Package auth holds the password and token primitives. It wraps vetted
// libraries (argon2 from golang.org/x/crypto, crypto/rand, crypto/sha256) and
// implements no cryptography of its own.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	minPasswordLength = 12
	// An upper bound keeps hashing cost bounded if someone submits a huge body.
	maxPasswordLength = 1024
	saltLength        = 16
	keyLength         = 32
	tokenBytes        = 32
)

type params struct {
	memoryKiB   uint32
	iterations  uint32
	parallelism uint8
}

// defaultParams follow the OWASP guidance for argon2id (64 MiB, 2 passes).
var defaultParams = params{memoryKiB: 64 * 1024, iterations: 2, parallelism: 1}

// Limits applied when verifying, so a tampered stored hash cannot make the
// server allocate unbounded memory or burn unbounded CPU.
const (
	maxMemoryKiB   = 1 << 20 // 1 GiB
	maxIterations  = 10
	maxParallelism = 16
)

// ValidatePassword enforces the length policy.
func ValidatePassword(password string) error {
	switch {
	case len(password) < minPasswordLength:
		return fmt.Errorf("password must have at least %d characters", minPasswordLength)
	case len(password) > maxPasswordLength:
		return fmt.Errorf("password must have at most %d characters", maxPasswordLength)
	}
	return nil
}

// HashPassword returns an encoded argon2id hash with a random salt, in the
// standard "$argon2id$v=19$m=...,t=...,p=...$salt$hash" format.
func HashPassword(password string) (string, error) {
	return hashWith(password, defaultParams)
}

func hashWith(password string, p params) (string, error) {
	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, p.iterations, p.memoryKiB, p.parallelism, keyLength)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version,
		p.memoryKiB, p.iterations, p.parallelism,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword checks a password against an encoded hash in constant time.
// An error means the hash is unusable, which is different from a mismatch.
func VerifyPassword(password, encoded string) (bool, error) {
	p, salt, want, err := decode(encoded)
	if err != nil {
		return false, err
	}
	got := argon2.IDKey([]byte(password), salt, p.iterations, p.memoryKiB, p.parallelism, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

func decode(encoded string) (p params, salt, key []byte, err error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return p, nil, nil, errors.New("unsupported password hash format")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return p, nil, nil, errors.New("unsupported argon2 version")
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.memoryKiB, &p.iterations, &p.parallelism); err != nil {
		return p, nil, nil, errors.New("invalid argon2 parameters")
	}
	if p.memoryKiB == 0 || p.memoryKiB > maxMemoryKiB ||
		p.iterations == 0 || p.iterations > maxIterations ||
		p.parallelism == 0 || p.parallelism > maxParallelism {
		return p, nil, nil, errors.New("argon2 parameters out of range")
	}
	if salt, err = base64.RawStdEncoding.DecodeString(parts[4]); err != nil || len(salt) == 0 {
		return p, nil, nil, errors.New("invalid salt")
	}
	if key, err = base64.RawStdEncoding.DecodeString(parts[5]); err != nil || len(key) == 0 {
		return p, nil, nil, errors.New("invalid hash")
	}
	return p, salt, key, nil
}

// NewToken returns a random URL-safe token with 256 bits of entropy.
func NewToken() (string, error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// HashToken is the form in which a session token is stored. The token is
// random and high-entropy, so a plain SHA-256 is enough (no salt or stretching
// needed), and a stolen database copy cannot be replayed as a cookie.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Equal compares two secrets in constant time.
func Equal(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
