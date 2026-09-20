-- +goose Up
CREATE TABLE users (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    auth_subject TEXT NOT NULL UNIQUE CHECK (length(trim(auth_subject)) > 0 AND octet_length(auth_subject) <= 255),
    email TEXT CHECK (email IS NULL OR octet_length(email) <= 320),
    display_name TEXT NOT NULL DEFAULT '' CHECK (octet_length(display_name) <= 200),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE users;
