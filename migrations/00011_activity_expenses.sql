-- +goose Up
ALTER TABLE expenses ALTER COLUMN payer_user_id DROP NOT NULL;
ALTER TABLE expenses ADD COLUMN activity_id BIGINT REFERENCES itinerary_items(id) ON DELETE SET NULL;
ALTER TABLE expenses ADD COLUMN activity_name TEXT NOT NULL DEFAULT '';
ALTER TABLE expenses ADD COLUMN amount_basis TEXT NOT NULL DEFAULT 'group' CHECK (amount_basis IN ('group','per_person'));
CREATE TABLE expense_payments (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    trip_id BIGINT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    expense_id BIGINT NOT NULL REFERENCES expenses(id),
    created_by_user_id BIGINT NOT NULL REFERENCES users(id),
    payer_user_id BIGINT NOT NULL REFERENCES users(id),
    currency TEXT NOT NULL,
    amount_minor BIGINT NOT NULL CHECK (amount_minor > 0),
    paid_on DATE NOT NULL,
    allocations JSONB NOT NULL CHECK (jsonb_typeof(allocations)='array'),
    version BIGINT NOT NULL DEFAULT 1,
    idempotency_key TEXT NOT NULL,
    request_hash BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ,
    legacy BOOLEAN NOT NULL DEFAULT false,
    UNIQUE(trip_id,created_by_user_id,idempotency_key)
);
INSERT INTO expense_payments(trip_id,expense_id,created_by_user_id,payer_user_id,currency,amount_minor,paid_on,allocations,idempotency_key,request_hash,created_at,updated_at,deleted_at,legacy)
SELECT e.trip_id,e.id,e.created_by_user_id,e.payer_user_id,e.currency,e.amount_minor,e.incurred_on,
 COALESCE((SELECT jsonb_agg(jsonb_build_object('user_id',s.user_id,'amount_minor',s.amount_minor::text) ORDER BY s.user_id) FROM expense_shares s WHERE s.expense_id=e.id),'[]'::jsonb),
 'migrated:'||e.id,e.request_hash,e.created_at,e.updated_at,e.deleted_at,true FROM expenses e;
CREATE TABLE expense_loans (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    trip_id BIGINT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    activity_id BIGINT REFERENCES itinerary_items(id) ON DELETE SET NULL,
    activity_name TEXT NOT NULL,
    description TEXT NOT NULL,
    created_by_user_id BIGINT NOT NULL REFERENCES users(id),
    from_user_id BIGINT NOT NULL REFERENCES users(id),
    to_user_id BIGINT NOT NULL REFERENCES users(id),
    currency TEXT NOT NULL,
    amount_minor BIGINT NOT NULL CHECK(amount_minor > 0),
    paid_on DATE NOT NULL,
    version BIGINT NOT NULL DEFAULT 1,
    idempotency_key TEXT NOT NULL,
    request_hash BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ,
    CHECK(from_user_id<>to_user_id),
    UNIQUE(trip_id,created_by_user_id,idempotency_key)
);
ALTER TABLE settlements ADD COLUMN payment_id BIGINT REFERENCES expense_payments(id);
ALTER TABLE settlements ADD COLUMN loan_id BIGINT REFERENCES expense_loans(id);
ALTER TABLE settlements ADD CONSTRAINT settlement_source CHECK (payment_id IS NULL OR loan_id IS NULL);
CREATE INDEX expense_payments_trip ON expense_payments(trip_id,expense_id) WHERE deleted_at IS NULL;
CREATE INDEX expense_loans_trip ON expense_loans(trip_id) WHERE deleted_at IS NULL;
-- Keep the last activity title even when an activity is renamed and later deleted.
-- +goose StatementBegin
CREATE FUNCTION expense_activity_title() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 UPDATE expenses SET activity_name=NEW.title WHERE activity_id=NEW.id;
 UPDATE expense_loans SET activity_name=NEW.title WHERE activity_id=NEW.id;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER expense_activity_title AFTER UPDATE OF title ON itinerary_items FOR EACH ROW EXECUTE FUNCTION expense_activity_title();

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM expenses WHERE payer_user_id IS NULL) OR EXISTS(SELECT 1 FROM expense_loans) OR EXISTS(SELECT 1 FROM expense_payments WHERE NOT legacy) THEN
  RAISE EXCEPTION 'Activity financial records require a forward migration; refusing lossy downgrade';
 END IF;
END $$;
-- +goose StatementEnd
DROP TRIGGER expense_activity_title ON itinerary_items;
DROP FUNCTION expense_activity_title();
ALTER TABLE settlements DROP CONSTRAINT settlement_source, DROP COLUMN payment_id, DROP COLUMN loan_id;
DROP TABLE expense_loans;
DROP TABLE expense_payments;
ALTER TABLE expenses DROP COLUMN activity_id, DROP COLUMN activity_name, DROP COLUMN amount_basis;
ALTER TABLE expenses ALTER COLUMN payer_user_id SET NOT NULL;
