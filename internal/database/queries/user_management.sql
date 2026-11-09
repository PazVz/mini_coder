-- name: LockUserManagement :one
SELECT singleton FROM user_management_lock WHERE singleton = true FOR UPDATE;

-- name: GetManagedUser :one
SELECT u.id, u.username, u.status, u.created_at, u.updated_at, r.role
FROM users u JOIN user_roles r ON r.user_id = u.id
WHERE u.id = $1;

-- name: ListManagedUsers :many
SELECT u.id, u.username, u.status, u.created_at, u.updated_at, r.role
FROM users u JOIN user_roles r ON r.user_id = u.id
WHERE u.id > sqlc.arg(after_id)::bigint
ORDER BY u.id
LIMIT sqlc.arg(page_limit)::integer;

-- name: CountActiveAdmins :one
SELECT count(*) FROM users u JOIN user_roles r ON r.user_id = u.id
WHERE u.status = 'active' AND r.role = 'admin';

-- name: UpdateUserStatus :exec
UPDATE users SET status = $2, updated_at = clock_timestamp() WHERE id = $1;

-- name: UpdateUserRole :exec
WITH changed AS (
    UPDATE user_roles SET role = $2 WHERE user_id = $1 RETURNING user_id
)
UPDATE users SET updated_at = clock_timestamp()
WHERE id IN (SELECT user_id FROM changed);
