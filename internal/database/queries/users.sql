-- name: CreateUser :one
WITH created AS (
    INSERT INTO users (username, password_hash)
    VALUES ($1, $2)
    RETURNING id, username, password_hash, status, created_at, updated_at
), assigned AS (
    INSERT INTO user_roles (user_id, role)
    SELECT id, 'member' FROM created
)
SELECT * FROM created;

-- name: GetUserByID :one
SELECT id, username, password_hash, status, created_at, updated_at
FROM users
WHERE id = $1;

-- name: GetUserByUsername :one
SELECT id, username, password_hash, status, created_at, updated_at
FROM users
WHERE username = $1;
