-- +goose Up
-- User management transactions lock this row before reading the actor and
-- counting active administrators. It coordinates all server processes.
CREATE TABLE user_management_lock (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton)
);
INSERT INTO user_management_lock DEFAULT VALUES;

-- +goose Down
DROP TABLE user_management_lock;
