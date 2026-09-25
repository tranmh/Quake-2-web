// Package auth implements accounts: argon2id password hashing, cookie
// sessions stored (hashed) in the database, a login rate limiter and the
// one-time WebSocket join tickets used by the game host.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Params are argon2id parameters.
type Params struct {
	Memory  uint32 // KiB
	Time    uint32
	Threads uint8
	SaltLen uint32
	KeyLen  uint32
}

// DefaultParams follow the OWASP recommendation (19 MiB, t=2, p=1).
var DefaultParams = Params{Memory: 19 * 1024, Time: 2, Threads: 1, SaltLen: 16, KeyLen: 32}

// ErrBadHash is returned for hashes not in PHC argon2id format.
var ErrBadHash = errors.New("auth: malformed password hash")

var b64 = base64.RawStdEncoding

// HashPassword returns a PHC string:
// $argon2id$v=19$m=19456,t=2,p=1$<salt>$<key>.
func HashPassword(password string, p Params) (string, error) {
	salt := make([]byte, p.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, p.Time, p.Memory, p.Threads, p.KeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.Memory, p.Time, p.Threads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// VerifyPassword checks password against a PHC argon2id hash in constant time.
func VerifyPassword(hash, password string) (bool, error) {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, ErrBadHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, ErrBadHash
	}
	var p Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.Memory, &p.Time, &p.Threads); err != nil {
		return false, ErrBadHash
	}
	if p.Memory == 0 || p.Memory > 1<<21 || p.Time == 0 || p.Time > 16 || p.Threads == 0 {
		return false, ErrBadHash
	}
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return false, ErrBadHash
	}
	key, err := b64.DecodeString(parts[5])
	if err != nil || len(key) == 0 || len(key) > 128 {
		return false, ErrBadHash
	}
	got := argon2.IDKey([]byte(password), salt, p.Time, p.Memory, p.Threads, uint32(len(key)))
	return subtle.ConstantTimeCompare(got, key) == 1, nil
}
