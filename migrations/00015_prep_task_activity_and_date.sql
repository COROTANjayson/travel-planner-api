-- +goose Up
ALTER TABLE prep_tasks
    ADD COLUMN activity_id BIGINT REFERENCES itinerary_items(id) ON DELETE SET NULL,
    ADD COLUMN task_date DATE;
CREATE INDEX prep_tasks_activity_id_idx ON prep_tasks (activity_id) WHERE activity_id IS NOT NULL;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM prep_tasks WHERE activity_id IS NOT NULL OR task_date IS NOT NULL) THEN
        RAISE EXCEPTION 'Prep tasks have activities or dates; preserve them before downgrading';
    END IF;
END $$;
-- +goose StatementEnd
DROP INDEX prep_tasks_activity_id_idx;
ALTER TABLE prep_tasks DROP COLUMN task_date, DROP COLUMN activity_id;
