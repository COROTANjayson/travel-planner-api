-- +goose Up
ALTER TABLE itinerary_items
    ADD COLUMN category TEXT NOT NULL DEFAULT 'Activity'
        CHECK (octet_length(btrim(category)) BETWEEN 1 AND 100),
    ADD COLUMN checklist JSONB NOT NULL DEFAULT '[]'::jsonb
        CHECK (jsonb_typeof(checklist) = 'array' AND jsonb_array_length(checklist) <= 50);

-- +goose Down
ALTER TABLE itinerary_items DROP COLUMN checklist, DROP COLUMN category;
