-- +goose Up
ALTER TABLE itinerary_items ADD COLUMN created_by_user_id BIGINT REFERENCES users(id);

UPDATE itinerary_items i SET created_by_user_id = t.owner_user_id
FROM trips t WHERE t.id = i.trip_id;

ALTER TABLE itinerary_items ALTER COLUMN created_by_user_id SET NOT NULL;
CREATE INDEX itinerary_items_creator_idx ON itinerary_items (created_by_user_id);

-- +goose Down
DROP INDEX itinerary_items_creator_idx;
ALTER TABLE itinerary_items DROP COLUMN created_by_user_id;
