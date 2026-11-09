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
	for _, hash := range []string{"", "plain password", strings.Replace(first, "m=19456", "m=999999999", 1), passwordPrefix + "invalid$invalid", first + "$extra"} {
		if match, err := verifyPassword(hash, password); match || err == nil {
			t.Fatalf("accepted malformed hash %q", hash)
		}
	}
}

func TestCredentialValidation(t *testing.T) {
	for _, c := range []Credentials{{"Alice", "a long password"}, {"ab", "a long password"}, {" admin", "a long password"}, {"admin", "short"}, {"admin", strings.Repeat("x", 1025)}, {"admin", "invalid utf8\xff"}} {
		if validate(c) == nil {
			t.Fatalf("accepted invalid credentials for %s", c.Username)
		}
	}
	for _, password := range []string{strings.Repeat("密", 12), strings.Repeat(" ", 12), strings.Repeat("x", 1024)} {
		if err := validate(Credentials{"admin", password}); err != nil {
			t.Fatal(err)
		}
	}
}
