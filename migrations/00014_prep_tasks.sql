-- +goose Up
CREATE TABLE prep_tasks (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    trip_id BIGINT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    created_by_user_id BIGINT NOT NULL REFERENCES users(id),
    title TEXT NOT NULL CHECK (length(trim(title)) > 0 AND octet_length(title) <= 200),
    kind TEXT NOT NULL CHECK (kind IN ('one_person', 'everyone')),
    assignee_user_id BIGINT REFERENCES users(id),
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT prep_tasks_everyone_fields CHECK (kind = 'one_person' OR (assignee_user_id IS NULL AND completed_at IS NULL)),
    CONSTRAINT prep_tasks_completion_needs_assignee CHECK (completed_at IS NULL OR assignee_user_id IS NOT NULL)
);
CREATE INDEX prep_tasks_trip_id_idx ON prep_tasks (trip_id, id);

CREATE TABLE prep_task_ticks (
    task_id BIGINT NOT NULL REFERENCES prep_tasks(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id),
    ticked_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (task_id, user_id)
);

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM prep_tasks) THEN
        RAISE EXCEPTION 'Prep tasks exist; preserve them before downgrading';
    END IF;
END $$;
-- +goose StatementEnd
DROP TABLE prep_task_ticks;
DROP TABLE prep_tasks;
