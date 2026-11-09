package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// Fixed, versioned parameters keep verification bounded even for a corrupt hash.
// Parameter migration belongs alongside a future password rehash policy.
const passwordPrefix = "$argon2id$v=19$m=19456,t=2,p=1$"

func hashPassword(password string) string {
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	return passwordPrefix + base64.RawStdEncoding.EncodeToString(salt) + "$" +
		base64.RawStdEncoding.EncodeToString(passwordKey(password, salt))
}

func passwordKey(password string, salt []byte) []byte {
	return argon2.IDKey([]byte(password), salt, 2, 19*1024, 1, 32)
}

func verifyPassword(encoded, password string) (bool, error) {
	if !strings.HasPrefix(encoded, passwordPrefix) {
		return false, errors.New("unsupported password hash format")
	}
	parts := strings.Split(strings.TrimPrefix(encoded, passwordPrefix), "$")
	if len(parts) != 2 {
		return false, errors.New("invalid password hash")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[0])
	if err != nil || len(salt) != 16 {
		return false, errors.New("invalid password salt")
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[1])
	if err != nil || len(key) != 32 {
		return false, errors.New("invalid password key")
	}
	return subtle.ConstantTimeCompare(key, passwordKey(password, salt)) == 1, nil
}

func validPassword(password string) bool {
	return utf8.ValidString(password) && utf8.RuneCountInString(password) >= 12 && len(password) <= 1024
}
