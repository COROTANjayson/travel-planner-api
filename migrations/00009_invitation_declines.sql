-- +goose Up
ALTER TABLE trip_invitations ADD COLUMN declined_at TIMESTAMPTZ;

DROP INDEX trip_invitations_active_email_idx;
CREATE UNIQUE INDEX trip_invitations_active_email_idx
    ON trip_invitations (trip_id, email)
    WHERE accepted_at IS NULL AND revoked_at IS NULL AND declined_at IS NULL;

-- +goose Down
-- Preserve terminal decisions before restoring the old definition of active.
UPDATE trip_invitations SET revoked_at=COALESCE(revoked_at, declined_at)
    WHERE declined_at IS NOT NULL;
DROP INDEX trip_invitations_active_email_idx;
CREATE UNIQUE INDEX trip_invitations_active_email_idx
    ON trip_invitations (trip_id, email)
    WHERE accepted_at IS NULL AND revoked_at IS NULL;
ALTER TABLE trip_invitations DROP COLUMN declined_at;
