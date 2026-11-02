-- +goose Up
CREATE TABLE activity_plan_requests (
 trip_id BIGINT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
 actor_user_id BIGINT NOT NULL REFERENCES users(id),
 request_key TEXT NOT NULL,
 request_hash BYTEA NOT NULL CHECK(octet_length(request_hash)=32),
 activity_id BIGINT REFERENCES itinerary_items(id) ON DELETE SET NULL,
 response JSONB NOT NULL,
 PRIMARY KEY(trip_id,actor_user_id,request_key)
);

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM activity_plan_requests) THEN
  RAISE EXCEPTION 'Activity submission history exists; preserve request keys before downgrading';
 END IF;
END $$;
-- +goose StatementEnd
DROP TABLE activity_plan_requests;
