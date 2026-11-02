-- +goose Up
CREATE TABLE expenses (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    trip_id BIGINT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    created_by_user_id BIGINT NOT NULL REFERENCES users(id),
    payer_user_id BIGINT NOT NULL REFERENCES users(id),
    description TEXT NOT NULL CHECK (length(trim(description)) > 0 AND octet_length(description) <= 200),
    currency TEXT NOT NULL CHECK (currency IN ('PHP','USD','EUR','GBP','JPY','SGD','THB','VND','IDR','MYR','AUD','CAD','CNY','HKD','KRW','TWD','CHF')),
    amount_minor BIGINT NOT NULL CHECK (amount_minor > 0),
    incurred_on DATE NOT NULL CHECK (incurred_on BETWEEN DATE '0001-01-01' AND DATE '9999-12-31'),
    split_mode TEXT NOT NULL CHECK (split_mode IN ('equal','exact')),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    idempotency_key TEXT NOT NULL CHECK (octet_length(idempotency_key) BETWEEN 1 AND 128),
    request_hash BYTEA NOT NULL CHECK (octet_length(request_hash) = 32),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ,
    UNIQUE (trip_id, created_by_user_id, idempotency_key)
);
CREATE TABLE expense_shares (
    expense_id BIGINT NOT NULL REFERENCES expenses(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id),
    amount_minor BIGINT NOT NULL CHECK (amount_minor >= 0),
    PRIMARY KEY (expense_id, user_id)
);
CREATE TABLE settlements (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    trip_id BIGINT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    created_by_user_id BIGINT NOT NULL REFERENCES users(id),
    from_user_id BIGINT NOT NULL REFERENCES users(id),
    to_user_id BIGINT NOT NULL REFERENCES users(id),
    currency TEXT NOT NULL CHECK (currency IN ('PHP','USD','EUR','GBP','JPY','SGD','THB','VND','IDR','MYR','AUD','CAD','CNY','HKD','KRW','TWD','CHF')),
    amount_minor BIGINT NOT NULL CHECK (amount_minor > 0),
    paid_on DATE NOT NULL CHECK (paid_on BETWEEN DATE '0001-01-01' AND DATE '9999-12-31'),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    idempotency_key TEXT NOT NULL CHECK (octet_length(idempotency_key) BETWEEN 1 AND 128),
    request_hash BYTEA NOT NULL CHECK (octet_length(request_hash) = 32),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ,
    CHECK (from_user_id <> to_user_id),
    UNIQUE (trip_id, created_by_user_id, idempotency_key)
);
CREATE INDEX expenses_trip_date_idx ON expenses (trip_id, incurred_on DESC, id DESC) WHERE deleted_at IS NULL;
CREATE INDEX settlements_trip_date_idx ON settlements (trip_id, paid_on DESC, id DESC) WHERE deleted_at IS NULL;

-- +goose Down
DROP TABLE settlements;
DROP TABLE expense_shares;
DROP TABLE expenses;
