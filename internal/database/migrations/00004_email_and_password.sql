-- +goose Up
-- Existing users keep NULL email until they complete the future verification flow.
ALTER TABLE users
    ADD COLUMN email text,
    ADD COLUMN email_key text COLLATE "C" GENERATED ALWAYS AS (lower(email COLLATE "C")) STORED,
    ADD COLUMN email_verified_at timestamptz,
    ADD CONSTRAINT users_email_key UNIQUE (email_key),
    ADD CONSTRAINT users_email_format CHECK (
        email IS NULL OR (
            octet_length(email) <= 254 AND email COLLATE "C" ~ '^[!-~]+@[!-~]+$'
        )
    ),
    ADD CONSTRAINT users_email_verification CHECK (email_verified_at IS NULL OR email IS NOT NULL),
    ALTER COLUMN password_hash DROP NOT NULL;

DROP TRIGGER users_revoke_sessions ON users;
DROP FUNCTION revoke_disabled_user_sessions();

-- +goose StatementBegin
CREATE FUNCTION revoke_user_sessions() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF (NEW.status = 'disabled' AND OLD.status <> 'disabled')
        OR NEW.password_hash IS DISTINCT FROM OLD.password_hash THEN
        DELETE FROM sessions WHERE user_id = NEW.id;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
-- Login and password changes lock the same user row before writing credentials/sessions.
CREATE TRIGGER users_revoke_sessions AFTER UPDATE OF status, password_hash ON users
    FOR EACH ROW EXECUTE FUNCTION revoke_user_sessions();

-- +goose Down
-- Refuse downgrade if a passwordless account exists; never invent credentials.
ALTER TABLE users ALTER COLUMN password_hash SET NOT NULL;

DROP TRIGGER users_revoke_sessions ON users;
DROP FUNCTION revoke_user_sessions();

-- +goose StatementBegin
CREATE FUNCTION revoke_disabled_user_sessions() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.status = 'disabled' AND OLD.status <> 'disabled' THEN
        DELETE FROM sessions WHERE user_id = NEW.id;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER users_revoke_sessions AFTER UPDATE OF status ON users
    FOR EACH ROW EXECUTE FUNCTION revoke_disabled_user_sessions();

ALTER TABLE users
    DROP COLUMN email_verified_at,
    DROP COLUMN email_key,
    DROP COLUMN email;
