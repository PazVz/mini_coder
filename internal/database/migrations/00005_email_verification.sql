-- +goose Up
CREATE TABLE email_actions (
    token_hash bytea PRIMARY KEY CHECK (octet_length(token_hash) = 32),
    purpose text NOT NULL CHECK (purpose IN ('register', 'verify_email', 'change_email')),
    user_id bigint REFERENCES users(id) ON DELETE CASCADE,
    email text NOT NULL CHECK (octet_length(email) <= 254 AND email COLLATE "C" ~ '^[!-~]+@[!-~]+$'),
    email_key text COLLATE "C" GENERATED ALWAYS AS (lower(email COLLATE "C")) STORED,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at timestamptz NOT NULL DEFAULT clock_timestamp() + interval '30 minutes',
    CHECK (expires_at > created_at),
    CHECK ((purpose = 'register') = (user_id IS NULL))
);
CREATE UNIQUE INDEX email_actions_registration_key ON email_actions (email_key) WHERE purpose = 'register';
CREATE UNIQUE INDEX email_actions_user_key ON email_actions (user_id) WHERE user_id IS NOT NULL;
CREATE INDEX email_actions_expiry_idx ON email_actions (expires_at);

-- Delivery quotas survive token rotation, consumption, failures and server restarts.
CREATE TABLE email_delivery_limits (
    email_key text COLLATE "C" PRIMARY KEY,
    window_start timestamptz NOT NULL,
    last_requested_at timestamptz NOT NULL,
    attempts integer NOT NULL CHECK (attempts BETWEEN 1 AND 5)
);

-- User mutations and authenticated email flows all take the user row lock first.
-- +goose StatementBegin
CREATE FUNCTION invalidate_user_email_actions() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.email IS DISTINCT FROM OLD.email
        OR NEW.password_hash IS DISTINCT FROM OLD.password_hash
        OR (NEW.status = 'disabled' AND OLD.status <> 'disabled') THEN
        DELETE FROM email_actions WHERE user_id = NEW.id;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER users_invalidate_email_actions AFTER UPDATE OF email, password_hash, status ON users
    FOR EACH ROW EXECUTE FUNCTION invalidate_user_email_actions();

-- +goose Down
DROP TRIGGER users_invalidate_email_actions ON users;
DROP FUNCTION invalidate_user_email_actions();
DROP TABLE email_actions;
DROP TABLE email_delivery_limits;
