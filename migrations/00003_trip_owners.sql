-- +goose Up
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM trips) THEN
        RAISE EXCEPTION 'cannot add trip ownership while legacy trips exist; reset local trip data or perform a reviewed one-off owner assignment, then rerun the migration';
    END IF;
END $$;
-- +goose StatementEnd

ALTER TABLE trips
    ADD COLUMN owner_user_id BIGINT NOT NULL REFERENCES users(id);

CREATE INDEX trips_owner_id_idx ON trips (owner_user_id, id DESC);

-- +goose Down
DROP INDEX trips_owner_id_idx;
ALTER TABLE trips DROP COLUMN owner_user_id;
