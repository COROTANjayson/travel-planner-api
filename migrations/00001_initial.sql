-- +goose Up
CREATE TABLE trips (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name TEXT NOT NULL CHECK (length(trim(name)) > 0 AND octet_length(name) <= 200),
    destination TEXT NOT NULL CHECK (length(trim(destination)) > 0 AND octet_length(destination) <= 300),
    start_date DATE NOT NULL,
    end_date DATE NOT NULL CHECK (end_date >= start_date),
    time_zone TEXT NOT NULL CHECK (length(time_zone) > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE itinerary_items (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    trip_id BIGINT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    title TEXT NOT NULL CHECK (length(trim(title)) > 0 AND octet_length(title) <= 200),
    starts_at TIMESTAMPTZ NOT NULL,
    ends_at TIMESTAMPTZ NOT NULL CHECK (ends_at > starts_at),
    time_zone TEXT NOT NULL CHECK (length(time_zone) > 0),
    notes TEXT NOT NULL DEFAULT '' CHECK (octet_length(notes) <= 10000),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX itinerary_items_trip_schedule_idx ON itinerary_items (trip_id, starts_at, id);

-- +goose Down
DROP TABLE itinerary_items;
DROP TABLE trips;
