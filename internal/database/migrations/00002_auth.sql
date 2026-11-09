-- +goose Up
CREATE TABLE user_roles (
    user_id bigint PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    role text NOT NULL CHECK (role IN ('admin', 'member'))
);
INSERT INTO user_roles (user_id, role) SELECT id, 'member' FROM users;

-- A permanent singleton records setup independently of later user deletion.
CREATE TABLE auth_setup (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    initialized boolean NOT NULL DEFAULT false
);
INSERT INTO auth_setup DEFAULT VALUES;

CREATE TABLE sessions (
    token_hash bytea PRIMARY KEY CHECK (octet_length(token_hash) = 32),
    user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL CHECK (expires_at > created_at)
);
CREATE INDEX sessions_user_id_idx ON sessions (user_id);
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);

-- Also applies to administrative SQL updates, until a user management API exists.
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

-- +goose Down
DROP TRIGGER users_revoke_sessions ON users;
DROP FUNCTION revoke_disabled_user_sessions();
DROP TABLE sessions;
DROP TABLE auth_setup;
DROP TABLE user_roles;
