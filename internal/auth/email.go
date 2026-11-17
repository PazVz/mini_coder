package auth

import (
	"net/mail"
	"strings"
)

// CreateUserInput supplies the required profile and local credentials for provisioning.
// Email ownership is not established by setup or administrator provisioning.
type CreateUserInput struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

func validateCreateUser(input CreateUserInput) (string, error) {
	if err := validateCredentials(Credentials{Username: input.Username, Password: input.Password}); err != nil {
		return "", err
	}
	return validatedEmail(input.Email)
}

func validEmail(email string) bool {
	if len(email) > 254 {
		return false
	}
	for _, char := range email {
		if char < '!' || char > '~' {
			return false
		}
	}
	address, err := mail.ParseAddress(email)
	if err != nil || address.Name != "" || address.Address != email {
		return false
	}
	local, domain, ok := strings.Cut(email, "@")
	if !ok || len(local) > 64 {
		return false
	}
	// DNS names only; quoted local parts, address literals and SMTPUTF8 are not supported.
	for _, label := range strings.Split(domain, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, char := range label {
			if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '-') {
				return false
			}
		}
	}
	return true
}
