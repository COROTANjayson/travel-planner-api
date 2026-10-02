-- +goose Up
CREATE TABLE places (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    provider TEXT NOT NULL DEFAULT 'osm' CHECK (provider = 'osm'),
    provider_place_id TEXT NOT NULL CHECK (provider_place_id ~ '^[NWR][1-9][0-9]*$'),
    name TEXT NOT NULL CHECK (length(trim(name)) > 0 AND octet_length(name) <= 300),
    address TEXT NOT NULL CHECK (length(trim(address)) > 0 AND octet_length(address) <= 2000),
    latitude DOUBLE PRECISION NOT NULL CHECK (latitude BETWEEN -90 AND 90),
    longitude DOUBLE PRECISION NOT NULL CHECK (longitude BETWEEN -180 AND 180),
    time_zone TEXT NOT NULL CHECK (length(trim(time_zone)) > 0),
    refreshed_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, provider_place_id)
);

ALTER TABLE itinerary_items ADD COLUMN place_id BIGINT REFERENCES places(id) ON DELETE SET NULL;
CREATE INDEX itinerary_items_place_idx ON itinerary_items (place_id) WHERE place_id IS NOT NULL;

CREATE TABLE place_search_cache (
    query TEXT PRIMARY KEY,
    results JSONB NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);

-- +goose Down
DROP TABLE place_search_cache;
ALTER TABLE itinerary_items DROP COLUMN place_id;
DROP TABLE places;
