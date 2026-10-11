-- +goose Up
ALTER TABLE places ADD COLUMN kind TEXT NOT NULL DEFAULT 'other'
    CHECK (kind IN ('city', 'country', 'other'));
ALTER TABLE trips ADD COLUMN destination_place_id BIGINT REFERENCES places(id) ON DELETE SET NULL;
ALTER TABLE trips ADD COLUMN starts_at TIMESTAMPTZ;
ALTER TABLE trips ADD COLUMN ends_at TIMESTAMPTZ;
ALTER TABLE trips ADD CONSTRAINT trips_precise_window_check
    CHECK ((starts_at IS NULL AND ends_at IS NULL) OR
           (starts_at IS NOT NULL AND ends_at IS NOT NULL AND ends_at > starts_at));

-- +goose Down
ALTER TABLE trips DROP CONSTRAINT trips_precise_window_check;
ALTER TABLE trips DROP COLUMN ends_at;
ALTER TABLE trips DROP COLUMN starts_at;
ALTER TABLE trips DROP COLUMN destination_place_id;
ALTER TABLE places DROP COLUMN kind;
