-- +goose Up
CREATE TABLE users (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    username text NOT NULL,
    password_hash text NOT NULL,
    status text NOT NULL DEFAULT 'active',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT users_username_key UNIQUE (username),
    CONSTRAINT users_username_format CHECK (username COLLATE "C" ~ '^[a-z0-9][a-z0-9_-]{2,31}$'),
    CONSTRAINT users_password_hash_nonempty CHECK (length(btrim(password_hash)) > 0),
    CONSTRAINT users_status_check CHECK (status IN ('active', 'disabled'))
);

-- +goose Down
DROP TABLE users;
