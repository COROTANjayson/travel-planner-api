-- +goose Up
ALTER TABLE users ADD COLUMN email_verified BOOLEAN NOT NULL DEFAULT false;

CREATE TABLE trip_members (
    trip_id BIGINT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('editor', 'member', 'viewer')),
    joined_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (trip_id, user_id)
);

CREATE INDEX trip_members_user_trip_idx ON trip_members (user_id, trip_id DESC);

CREATE TABLE trip_invitations (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    trip_id BIGINT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    email TEXT NOT NULL CHECK (length(trim(email)) > 0 AND octet_length(email) <= 320),
    role TEXT NOT NULL CHECK (role IN ('editor', 'member', 'viewer')),
    token_hash BYTEA NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    invited_by_user_id BIGINT NOT NULL REFERENCES users(id),
    expires_at TIMESTAMPTZ NOT NULL,
    accepted_at TIMESTAMPTZ,
    accepted_by_user_id BIGINT REFERENCES users(id),
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((accepted_at IS NULL) = (accepted_by_user_id IS NULL))
);

CREATE UNIQUE INDEX trip_invitations_active_email_idx
    ON trip_invitations (trip_id, email)
    WHERE accepted_at IS NULL AND revoked_at IS NULL;

CREATE INDEX trip_invitations_trip_id_idx ON trip_invitations (trip_id, id DESC);

-- +goose Down
DROP TABLE trip_invitations;
DROP TABLE trip_members;
ALTER TABLE users DROP COLUMN email_verified;
