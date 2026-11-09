// Package authz defines the permissions granted to the built-in roles.
package authz

import "errors"

// ErrForbidden indicates that the current role cannot perform an operation.
var ErrForbidden = errors.New("forbidden")

// Permission identifies an operation on a resource type.
type Permission string

// User management permissions are granted only to administrators.
const (
	UserCreate       Permission = "user:create"
	UserList         Permission = "user:list"
	UserUpdateStatus Permission = "user:update_status"
	UserUpdateRole   Permission = "user:update_role"
)

// Authorize checks the fixed role permissions and denies unknown operations.
func Authorize(role string, permission Permission) error {
	if role == "admin" {
		switch permission {
		case UserCreate, UserList, UserUpdateStatus, UserUpdateRole:
			return nil
		}
	}
	return ErrForbidden
}
