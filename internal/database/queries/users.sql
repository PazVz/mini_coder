-- name: CreateUser :one
WITH created AS (
    INSERT INTO users (username, password_hash, email)
    VALUES ($1, $2, $3)
    RETURNING *
), assigned AS (
    INSERT INTO user_roles (user_id, role)
    SELECT id, 'member' FROM created
)
SELECT * FROM created;

-- name: GetUserByID :one
SELECT *
FROM users
WHERE id = $1;

-- name: GetUserByUsername :one
SELECT *
FROM users
WHERE username = $1;
