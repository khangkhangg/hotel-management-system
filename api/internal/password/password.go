// Package password hashes passwords with Argon2id and applies the sign-in password policy (spec 02).
package password

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

const (
	minLength  = 12
	memoryKiB  = 64 * 1024
	iterations = 3
	threads    = 2
	saltBytes  = 16
	keyBytes   = 32
)

var (
	ErrTooShort = errors.New("password: shorter than 12 characters")
	ErrBreached = errors.New("password: appears in a list of breached passwords")
)

// BreachChecker says whether a password appears in a known breach list.
type BreachChecker interface {
	Breached(ctx context.Context, plain string) (bool, error)
}

// Check applies the policy: at least 12 characters, no composition rules, and not breached.
// If the breach list cannot be reached the password is accepted and the failure is logged,
// because a down third-party service must not stop anyone from signing in.
func Check(ctx context.Context, bc BreachChecker, plain string) error {
	if utf8.RuneCountInString(plain) < minLength {
		return ErrTooShort
	}
	breached, err := bc.Breached(ctx, plain)
	if err != nil {
		log.Printf("password: breach check unavailable: %v", err)
		return nil
	}
	if breached {
		return ErrBreached
	}
	return nil
}

// Hash returns a PHC string: $argon2id$v=19$m=65536,t=3,p=2$<salt>$<key>.
func Hash(plain string) (string, error) {
	salt := make([]byte, saltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(plain), salt, iterations, memoryKiB, threads, keyBytes)
	enc := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, memoryKiB, iterations, threads, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// Verify reports whether plain matches hash. A malformed hash never matches.
func Verify(hash, plain string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil || m == 0 || t == 0 || p == 0 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) == 0 {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false
	}
	got := argon2.IDKey([]byte(plain), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}
