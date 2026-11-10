-- name: ReserveEmailDelivery :one
INSERT INTO email_delivery_limits (email_key, window_start, last_requested_at, attempts)
VALUES (lower(sqlc.arg(email)::text COLLATE "C"), clock_timestamp(), clock_timestamp(), 1)
ON CONFLICT (email_key) DO UPDATE SET
    window_start = CASE WHEN email_delivery_limits.window_start <= clock_timestamp() - interval '1 hour'
        THEN clock_timestamp() ELSE email_delivery_limits.window_start END,
    attempts = CASE WHEN email_delivery_limits.window_start <= clock_timestamp() - interval '1 hour'
        THEN 1 ELSE email_delivery_limits.attempts + 1 END,
    last_requested_at = clock_timestamp()
WHERE email_delivery_limits.last_requested_at <= clock_timestamp() - interval '1 minute'
    AND (email_delivery_limits.window_start <= clock_timestamp() - interval '1 hour' OR email_delivery_limits.attempts < 5)
RETURNING email_key;

-- name: EmailDeliveryRetryAfter :one
SELECT greatest(1, ceil(extract(epoch FROM (
    greatest(last_requested_at + interval '1 minute',
        CASE WHEN attempts >= 5 THEN window_start + interval '1 hour' ELSE last_requested_at END)
    - clock_timestamp())))::integer)::integer AS seconds
FROM email_delivery_limits WHERE email_key = lower(sqlc.arg(email)::text COLLATE "C");

-- name: DeleteRegistrationAction :exec
DELETE FROM email_actions WHERE purpose = 'register' AND email_key = lower(sqlc.arg(email)::text COLLATE "C");

-- name: DeleteUserEmailActions :exec
DELETE FROM email_actions WHERE user_id = $1;

-- name: CreateEmailAction :exec
INSERT INTO email_actions (token_hash, purpose, user_id, email) VALUES ($1, $2, $3, $4);

-- name: LockRegistrationAction :one
SELECT token_hash FROM email_actions WHERE token_hash = $1 AND purpose = 'register' FOR UPDATE;

-- name: ConsumeRegistrationAction :one
DELETE FROM email_actions WHERE token_hash = $1 AND purpose = 'register' AND expires_at > clock_timestamp()
RETURNING email;

-- name: ConsumeUserEmailAction :one
DELETE FROM email_actions WHERE token_hash = $1 AND user_id = $2
    AND purpose IN ('verify_email', 'change_email') AND expires_at > clock_timestamp()
RETURNING email, purpose;

-- name: DeleteEmailAction :exec
DELETE FROM email_actions WHERE token_hash = $1;

-- name: SetVerifiedEmail :exec
UPDATE users SET email = $2, email_verified_at = clock_timestamp(), updated_at = clock_timestamp() WHERE id = $1;

-- name: DeleteExpiredEmailActions :exec
DELETE FROM email_actions WHERE expires_at <= clock_timestamp();

-- name: DeleteExpiredEmailDeliveryLimits :exec
DELETE FROM email_delivery_limits WHERE window_start <= clock_timestamp() - interval '1 hour'
    AND last_requested_at <= clock_timestamp() - interval '1 minute';
