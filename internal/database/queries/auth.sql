-- name: GetSetupState :one
SELECT initialized FROM auth_setup WHERE singleton = true;

-- name: LockSetupState :one
SELECT initialized FROM auth_setup WHERE singleton = true FOR UPDATE;

-- name: CompleteSetup :exec
UPDATE auth_setup SET initialized = true WHERE singleton = true;

-- name: SetAdminRole :exec
UPDATE user_roles SET role = 'admin' WHERE user_id = $1;

-- name: GetAuthUserByUsername :one
SELECT u.*, r.role FROM users u JOIN user_roles r ON r.user_id = u.id
WHERE u.username = $1;

-- name: LockAuthUserByID :one
SELECT u.*, r.role FROM users u JOIN user_roles r ON r.user_id = u.id
WHERE u.id = $1 FOR UPDATE OF u;

-- name: CreateSession :one
INSERT INTO sessions (token_hash, user_id, expires_at)
VALUES ($1, $2, now() + interval '24 hours')
RETURNING expires_at;

-- name: GetSessionUser :one
SELECT u.id, u.username, u.status, u.created_at, u.updated_at, r.role
FROM sessions s JOIN users u ON u.id = s.user_id
JOIN user_roles r ON r.user_id = u.id
WHERE s.token_hash = $1 AND s.expires_at > now() AND u.status = 'active';

-- name: DeleteSession :exec
DELETE FROM sessions WHERE token_hash = $1;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE expires_at <= now();
