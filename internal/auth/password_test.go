package auth

import (
	"strings"
	"testing"
)

func TestPasswordHash(t *testing.T) {
	password := "a long 密码 with spaces"
	first, second := hashPassword(password), hashPassword(password)
	if first == second {
		t.Fatal("password hashes reused a salt")
	}
	for _, hash := range []string{first, second} {
		match, err := verifyPassword(hash, password)
		if !match || err != nil {
			t.Fatalf("verify valid password: %v, %v", match, err)
		}
		match, err = verifyPassword(hash, "wrong password")
		if match || err != nil {
			t.Fatalf("verify wrong password: %v, %v", match, err)
		}
	}
	for _, tc := range []struct {
		name string
		hash string
	}{
		{"empty", ""},
		{"plaintext", "plain password"},
		{"unsupported parameters", strings.Replace(first, "m=19456", "m=999999999", 1)},
		{"invalid encoding", passwordPrefix + "invalid$invalid"},
		{"extra component", first + "$extra"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if match, err := verifyPassword(tc.hash, password); match || err == nil {
				t.Fatalf("verify malformed hash = %v, %v; want false and an error", match, err)
			}
		})
	}
}

func TestCredentialValidation(t *testing.T) {
	for _, tc := range []struct {
		name     string
		username string
		password string
	}{
		{"uppercase username", "Alice", "a long password"},
		{"short username", "ab", "a long password"},
		{"leading username space", " admin", "a long password"},
		{"short password", "admin", "short"},
		{"oversize password", "admin", strings.Repeat("x", 1025)},
		{"invalid UTF-8", "admin", "invalid utf8\xff"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			credentials := Credentials{Username: tc.username, Password: tc.password}
			if err := validateCredentials(credentials); err == nil {
				t.Fatal("validateCredentials succeeded, want a validation error")
			}
		})
	}
	for _, tc := range []struct {
		name     string
		password string
	}{
		{"Unicode characters", strings.Repeat("密", 12)},
		{"spaces preserved", strings.Repeat(" ", 12)},
		{"maximum bytes", strings.Repeat("x", 1024)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			credentials := Credentials{Username: "admin", Password: tc.password}
			if err := validateCredentials(credentials); err != nil {
				t.Fatalf("validateCredentials = %v, want nil", err)
			}
		})
	}
}
