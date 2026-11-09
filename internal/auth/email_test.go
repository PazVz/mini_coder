package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestEmailValidation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		email string
		valid bool
	}{
		{"ordinary", "alice@example.com", true},
		{"case preserved", "Alice@EXAMPLE.COM", true},
		{"plus alias", "alice+work@example.com", true},
		{"dot atom", "alice.smith@example.com", true},
		{"ASCII punctuation", "a!#$%&'*+-/=?^_`{|}~@example.com", true},
		{"maximum local part", strings.Repeat("a", 64) + "@example.com", true},
		{"maximum address", strings.Repeat("a", 64) + "@" + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 61), true},
		{"missing", "", false},
		{"missing domain", "alice@", false},
		{"missing separator", "example.com", false},
		{"multiple separators", "alice@work@example.com", false},
		{"display name", "Alice <alice@example.com>", false},
		{"comment", "alice(comment)@example.com", false},
		{"multiple addresses", "a@example.com,b@example.com", false},
		{"header injection", "a@example.com\r\nBcc:b@example.com", false},
		{"local whitespace", "alice smith@example.com", false},
		{"Unicode local part", "用户@example.com", false},
		{"Unicode domain", "alice@例子.com", false},
		{"quoted local part", `"alice"@example.com`, false},
		{"empty local component", "alice..smith@example.com", false},
		{"IP literal", "alice@[127.0.0.1]", false},
		{"empty domain label", "alice@example..com", false},
		{"leading hyphen", "alice@-example.com", false},
		{"trailing hyphen", "alice@example-.com", false},
		{"domain underscore", "alice@exam_ple.com", false},
		{"long local part", strings.Repeat("a", 65) + "@example.com", false},
		{"long domain label", "a@" + strings.Repeat("b", 64) + ".com", false},
		{"long address", strings.Repeat("a", 64) + "@" + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 62), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := validEmail(tc.email); got != tc.valid {
				t.Fatalf("validEmail(%q) = %v, want %v", tc.email, got, tc.valid)
			}
		})
	}
	input := CreateUserInput{Username: "alice", Email: "  Alice+work@EXAMPLE.com \t", Password: "a long password"}
	email, err := validateCreateUser(input)
	if err != nil || email != "Alice+work@EXAMPLE.com" {
		t.Fatalf("validated email = %q, error = %v", email, err)
	}
	input.Email = ""
	_, err = validateCreateUser(input)
	var validation *ValidationError
	if !errors.As(err, &validation) || len(validation.Fields) != 1 || validation.Fields[0].Field != "email" {
		t.Fatalf("missing email validation = %v", err)
	}
}
