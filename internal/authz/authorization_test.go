package authz

import (
	"errors"
	"testing"
)

func TestRolePermissions(t *testing.T) {
	for _, tc := range []struct {
		permission   Permission
		adminAllowed bool
	}{
		{UserCreate, true},
		{UserList, true},
		{UserUpdateStatus, true},
		{UserUpdateRole, true},
		{"user:delete", false},
		{"workspace:connect", false},
		{"", false},
	} {
		for _, role := range []string{"admin", "member", "unknown", ""} {
			t.Run(role+"/"+string(tc.permission), func(t *testing.T) {
				err := Authorize(role, tc.permission)
				allowed := role == "admin" && tc.adminAllowed
				if allowed && err != nil {
					t.Fatalf("Authorize = %v, want permission granted", err)
				}
				if !allowed && !errors.Is(err, ErrForbidden) {
					t.Fatalf("Authorize = %v, want ErrForbidden", err)
				}
			})
		}
	}
}
