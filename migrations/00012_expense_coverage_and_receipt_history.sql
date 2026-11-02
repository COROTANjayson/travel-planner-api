-- +goose Up
ALTER TABLE expenses ADD COLUMN planned_coverer_user_id BIGINT REFERENCES users(id);
ALTER TABLE expenses ADD COLUMN planned_cover_amount_minor BIGINT;
ALTER TABLE expenses ADD CONSTRAINT planned_cover_pair CHECK (
 (planned_coverer_user_id IS NULL AND planned_cover_amount_minor IS NULL) OR
 (planned_coverer_user_id IS NOT NULL AND planned_cover_amount_minor IS NOT NULL AND planned_cover_amount_minor > 0 AND planned_cover_amount_minor <= amount_minor)
);
CREATE TABLE settlement_history (
 id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 settlement_id BIGINT NOT NULL REFERENCES settlements(id) ON DELETE CASCADE,
 actor_user_id BIGINT NOT NULL REFERENCES users(id),
 action TEXT NOT NULL CHECK(action IN ('received','corrected','reversed','allocated')),
 reason TEXT NOT NULL DEFAULT '',
 before_record JSONB,
 after_record JSONB,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX settlement_history_record ON settlement_history(settlement_id,id);

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM settlement_history) OR EXISTS(SELECT 1 FROM expenses WHERE planned_coverer_user_id IS NOT NULL) THEN
  RAISE EXCEPTION 'Coverage or receipt history exists; refusing lossy downgrade';
 END IF;
END $$;
-- +goose StatementEnd
DROP TABLE settlement_history;
ALTER TABLE expenses DROP CONSTRAINT planned_cover_pair, DROP COLUMN planned_coverer_user_id, DROP COLUMN planned_cover_amount_minor;
