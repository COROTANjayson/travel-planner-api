package expenses

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"travel-planner/travel-planner-api/internal/apperror"
)

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool} }

type querier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func recordSQL(k kind) string {
	common := `e.id,e.trip_id,e.created_by_user_id,e.currency,e.amount_minor::text,e.version,e.created_at,e.updated_at,`
	var fields, users string
	if k == expense {
		fields = `e.description,to_char(e.incurred_on,'YYYY-MM-DD'),COALESCE(e.payer_user_id,0),e.split_mode,COALESCE(e.activity_id,0),e.activity_name,e.amount_basis,COALESCE(e.planned_coverer_user_id,0),COALESCE(e.planned_cover_amount_minor::text,''),
		COALESCE((SELECT jsonb_agg(jsonb_build_object('user_id',s.user_id,'amount_minor',s.amount_minor::text) ORDER BY s.user_id) FROM expense_shares s WHERE s.expense_id=e.id),'[]'::jsonb),`
		users = `u.id=e.created_by_user_id OR u.id=e.payer_user_id OR u.id=e.planned_coverer_user_id OR EXISTS(SELECT 1 FROM expense_shares s WHERE s.expense_id=e.id AND s.user_id=u.id)`
	} else if k == businessPayment {
		fields = `to_char(e.paid_on,'YYYY-MM-DD'),e.expense_id,e.payer_user_id,e.allocations,`
		users = `u.id=e.created_by_user_id OR u.id=e.payer_user_id OR EXISTS(SELECT 1 FROM jsonb_array_elements(e.allocations) a WHERE (a->>'user_id')::bigint=u.id)`
	} else if k == loan {
		fields = `to_char(e.paid_on,'YYYY-MM-DD'),e.from_user_id,e.to_user_id,COALESCE(e.activity_id,0),e.activity_name,e.description,`
		users = `u.id IN(e.created_by_user_id,e.from_user_id,e.to_user_id)`
	} else {
		fields = `to_char(e.paid_on,'YYYY-MM-DD'),e.from_user_id,e.to_user_id,COALESCE(e.payment_id,0),COALESCE(e.loan_id,0),`
		users = `u.id IN (e.created_by_user_id,e.from_user_id,e.to_user_id)`
	}
	return `SELECT ` + common + fields + `(SELECT COALESCE(jsonb_agg(jsonb_build_object('user_id',u.id,'display_name',u.display_name,'email',u.email) ORDER BY u.id),'[]'::jsonb) FROM users u WHERE ` + users + `) FROM ` + string(k) + ` e `
}
func scanRecord(row pgx.Row, k kind) (Record, error) {
	var result Record
	var shares, people []byte
	args := []any{&result.ID, &result.TripID, &result.CreatorID, &result.Currency, &result.Amount, &result.Version, &result.CreatedAt, &result.UpdatedAt}
	if k == expense {
		args = append(args, &result.Description, &result.Date, &result.PayerID, &result.SplitMode, &result.ActivityID, &result.ActivityName, &result.AmountBasis, &result.PlannedCovererID, &result.PlannedCoverAmount, &shares)
	} else if k == businessPayment {
		args = append(args, &result.PaidOn, &result.ExpenseID, &result.PayerID, &shares)
	} else if k == loan {
		args = append(args, &result.PaidOn, &result.FromID, &result.ToID, &result.ActivityID, &result.ActivityName, &result.Description)
	} else {
		args = append(args, &result.PaidOn, &result.FromID, &result.ToID, &result.PaymentID, &result.LoanID)
	}
	args = append(args, &people)
	if err := row.Scan(args...); errors.Is(err, pgx.ErrNoRows) {
		return result, apperror.ErrNotFound
	} else if err != nil {
		return result, err
	}
	if k == businessPayment {
		if err := json.Unmarshal(shares, &result.Allocations); err != nil {
			return result, err
		}
	}
	if k == expense {
		if err := json.Unmarshal(shares, &result.Shares); err != nil {
			return result, err
		}
	}
	if err := json.Unmarshal(people, &result.People); err != nil {
		return result, err
	}
	result.CreatedAt = result.CreatedAt.UTC()
	result.UpdatedAt = result.UpdatedAt.UTC()
	return result, nil
}
func get(ctx context.Context, q querier, trip int64, k kind, id int64) (Record, error) {
	return scanRecord(q.QueryRow(ctx, recordSQL(k)+`WHERE e.trip_id=$1 AND e.id=$2 AND e.deleted_at IS NULL`, trip, id), k)
}
func (r *Repository) Get(ctx context.Context, trip int64, k kind, id int64) (Record, error) {
	return get(ctx, r.pool, trip, k, id)
}
func (r *Repository) List(ctx context.Context, trip int64, k kind, currency string, limit, offset int) ([]Record, error) {
	date := "incurred_on"
	if k != expense {
		date = "paid_on"
	}
	rows, err := r.pool.Query(ctx, recordSQL(k)+`WHERE e.trip_id=$1 AND e.deleted_at IS NULL AND ($2='' OR e.currency=$2) ORDER BY e.`+date+` DESC,e.id DESC LIMIT $3 OFFSET $4`, trip, currency, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Record{}
	for rows.Next() {
		record, err := scanRecord(rows, k)
		if err != nil {
			return nil, err
		}
		result = append(result, record)
	}
	return result, rows.Err()
}

// Lock membership rows as well as the trip: removal/role changes do not lock trips.
func lockWriter(ctx context.Context, tx pgx.Tx, actor, trip int64) (map[int64]string, error) {
	var owner int64
	if err := tx.QueryRow(ctx, `SELECT owner_user_id FROM trips WHERE id=$1 FOR UPDATE`, trip).Scan(&owner); errors.Is(err, pgx.ErrNoRows) {
		return nil, apperror.ErrNotFound
	} else if err != nil {
		return nil, err
	}
	roles := map[int64]string{owner: "owner"}
	rows, err := tx.Query(ctx, `SELECT user_id,role FROM trip_members WHERE trip_id=$1 ORDER BY user_id FOR SHARE`, trip)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var role string
		if err := rows.Scan(&id, &role); err != nil {
			rows.Close()
			return nil, err
		}
		roles[id] = role
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if roles[actor] == "" {
		return nil, apperror.ErrNotFound
	}
	if roles[actor] == "viewer" {
		return nil, apperror.ErrForbidden
	}
	return roles, nil
}
func participantsValid(ctx context.Context, tx pgx.Tx, trip int64, k kind, in writeInput, roles map[int64]string, old *Record) error {
	allowed := map[int64]bool{}
	for id := range roles {
		allowed[id] = true
	}
	if k == settlement {
		people, err := historicalPeople(ctx, tx, trip)
		if err != nil {
			return err
		}
		for _, p := range people {
			allowed[p.UserID] = true
		}
		if !allowed[in.FromID] || !allowed[in.ToID] {
			return invalid("payment travelers must belong to this trip or its financial history")
		}
		return nil
	}
	if old != nil {
		for _, share := range old.Shares {
			allowed[share.UserID] = true
		}
	}
	if in.PayerID != 0 && roles[in.PayerID] == "" && (old == nil || old.PayerID != in.PayerID) {
		return invalid("payer must be a current participant")
	}
	for _, share := range in.Shares {
		if !allowed[share.UserID] {
			return invalid("share users must be current participants or retained from this expense")
		}
	}
	return nil
}
func (r *Repository) Save(ctx context.Context, actor, trip int64, k kind, id int64, in writeInput, key string, hash []byte) (Record, bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Record{}, false, err
	}
	defer tx.Rollback(ctx)
	result, created, err := saveTx(ctx, tx, actor, trip, k, id, in, key, hash)
	if err != nil {
		return Record{}, false, err
	}
	return result, created, tx.Commit(ctx)
}

func saveTx(ctx context.Context, tx pgx.Tx, actor, trip int64, k kind, id int64, in writeInput, key string, hash []byte) (Record, bool, error) {
	roles, err := lockWriter(ctx, tx, actor, trip)
	if err != nil {
		return Record{}, false, err
	}
	var old *Record
	created := id == 0
	if created {
		var existing int64
		var originalHash []byte
		var deleted *time.Time
		err := tx.QueryRow(ctx, `SELECT id,request_hash,deleted_at FROM `+string(k)+` WHERE trip_id=$1 AND created_by_user_id=$2 AND idempotency_key=$3`, trip, actor, key).Scan(&existing, &originalHash, &deleted)
		if err == nil {
			if !bytes.Equal(hash, originalHash) || deleted != nil {
				return Record{}, false, conflict("this submission key was already used; refresh the ledger before creating another record")
			}
			result, err := get(ctx, tx, trip, k, existing)
			return result, false, err
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return Record{}, false, err
		}
	} else {
		previous, err := get(ctx, tx, trip, k, id)
		if err != nil {
			return Record{}, false, err
		}
		old = &previous
		if !mayWrite(roles[actor], actor, previous.CreatorID) && !(k == settlement && (previous.PaymentID > 0 || previous.LoanID > 0) && previous.ToID == actor) {
			return Record{}, false, apperror.ErrForbidden
		}
		if in.Version != previous.Version {
			return Record{}, false, conflict("this record changed; reload the latest record before saving")
		}
	}
	activityName, err := validateActivityWrite(ctx, tx, actor, trip, k, &in, roles, old)
	if err != nil {
		return Record{}, false, err
	}
	if k == expense || k == settlement {
		if err := participantsValid(ctx, tx, trip, k, in, roles, old); err != nil {
			return Record{}, false, err
		}
	}
	if k == expense {
		if created {
			err = tx.QueryRow(ctx, `INSERT INTO expenses (trip_id,created_by_user_id,payer_user_id,description,currency,amount_minor,incurred_on,split_mode,idempotency_key,request_hash) VALUES ($1,$2,NULLIF($3,0),$4,$5,$6::text::bigint,$7::text::date,$8,$9,$10) RETURNING id`, trip, actor, in.PayerID, in.Description, in.Currency, in.Amount, in.Date, in.SplitMode, key, hash).Scan(&id)
		} else {
			_, err = tx.Exec(ctx, `UPDATE expenses SET payer_user_id=NULLIF($3,0),description=$4,currency=$5,amount_minor=$6::text::bigint,incurred_on=$7::text::date,split_mode=$8,version=version+1,updated_at=now(),planned_coverer_user_id=NULLIF($9,0),planned_cover_amount_minor=NULLIF($10,'')::bigint WHERE trip_id=$1 AND id=$2`, trip, id, in.PayerID, in.Description, in.Currency, in.Amount, in.Date, in.SplitMode, in.PlannedCovererID, in.PlannedCoverAmount)
		}
		if err != nil {
			return Record{}, false, err
		}
		if !created {
			if _, err := tx.Exec(ctx, `DELETE FROM expense_shares WHERE expense_id=$1`, id); err != nil {
				return Record{}, false, err
			}
		}
		for _, share := range in.Shares {
			if _, err := tx.Exec(ctx, `INSERT INTO expense_shares (expense_id,user_id,amount_minor) VALUES ($1,$2,$3::text::bigint)`, id, share.UserID, share.Amount); err != nil {
				return Record{}, false, err
			}
		}
		if _, err = tx.Exec(ctx, `UPDATE expenses SET activity_id=NULLIF($2,0),activity_name=$3,amount_basis=$4,planned_coverer_user_id=NULLIF($5,0),planned_cover_amount_minor=NULLIF($6,'')::bigint WHERE id=$1`, id, in.ActivityID, activityName, in.AmountBasis, in.PlannedCovererID, in.PlannedCoverAmount); err != nil {
			return Record{}, false, err
		}

		if created && in.PayerID > 0 {
			allocations, _ := json.Marshal(in.Shares)
			_, err = tx.Exec(ctx, `INSERT INTO expense_payments(trip_id,expense_id,created_by_user_id,payer_user_id,currency,amount_minor,paid_on,allocations,idempotency_key,request_hash,legacy) VALUES($1,$2,$3,$4,$5,$6::text::bigint,$7::text::date,$8::jsonb,$9,$10,$11)`, trip, id, actor, in.PayerID, in.Currency, in.Amount, in.Date, allocations, "purchase:"+strconv.FormatInt(id, 10), hash, in.ActivityID == 0)
			if err != nil {
				return Record{}, false, err
			}
		}
	} else if k == businessPayment || k == loan {
		id, err = saveTransaction(ctx, tx, actor, trip, k, id, in, key, hash, activityName)
		if err != nil {
			return Record{}, false, err
		}
	} else {
		if created {
			err = tx.QueryRow(ctx, `INSERT INTO settlements (trip_id,created_by_user_id,from_user_id,to_user_id,currency,amount_minor,paid_on,idempotency_key,request_hash) VALUES ($1,$2,$3,$4,$5,$6::text::bigint,$7::text::date,$8,$9) RETURNING id`, trip, actor, in.FromID, in.ToID, in.Currency, in.Amount, in.Date, key, hash).Scan(&id)
		} else {
			_, err = tx.Exec(ctx, `UPDATE settlements SET from_user_id=$3,to_user_id=$4,currency=$5,amount_minor=$6::text::bigint,paid_on=$7::text::date,version=version+1,updated_at=now() WHERE trip_id=$1 AND id=$2`, trip, id, in.FromID, in.ToID, in.Currency, in.Amount, in.Date)
		}
		if err != nil {
			return Record{}, false, err
		}
	}
	if k == settlement {
		if _, err = tx.Exec(ctx, `UPDATE settlements SET payment_id=NULLIF($2,0),loan_id=NULLIF($3,0) WHERE id=$1`, id, in.PaymentID, in.LoanID); err != nil {
			return Record{}, false, err
		}
	}
	if _, err := summary(ctx, tx, trip); err != nil {
		return Record{}, false, err
	}
	result, err := get(ctx, tx, trip, k, id)
	if err != nil {
		return Record{}, false, err
	}
	if k == settlement {
		action := "received"
		if old != nil {
			action = "corrected"
			if old.PaymentID == 0 && old.LoanID == 0 && (in.PaymentID > 0 || in.LoanID > 0) {
				action = "allocated"
			}
		}
		if err := appendReceiptHistory(ctx, tx, actor, id, action, in.CorrectionReason, old, &result); err != nil {
			return Record{}, false, err
		}
	}
	return result, created, nil
}
func (r *Repository) Delete(ctx context.Context, actor, trip int64, k kind, id, version int64, reason string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	roles, err := lockWriter(ctx, tx, actor, trip)
	if err != nil {
		return err
	}
	old, err := get(ctx, tx, trip, k, id)
	if err != nil {
		return err
	}
	if !mayWrite(roles[actor], actor, old.CreatorID) && !(k == settlement && (old.PaymentID > 0 || old.LoanID > 0) && old.ToID == actor) {
		return apperror.ErrForbidden
	}
	if old.Version != version {
		return conflict("this record changed; reload before deleting")
	}
	if k == settlement && !validCorrectionReason(reason) {
		return invalid("a correction reason (1 to 500 characters) is required")
	}
	if err := validateRemoval(ctx, tx, actor, trip, k, old, roles); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE `+string(k)+` SET deleted_at=now(),updated_at=now(),version=version+1 WHERE trip_id=$1 AND id=$2`, trip, id); err != nil {
		return err
	}
	if _, err := summary(ctx, tx, trip); err != nil {
		return err
	}
	if k == settlement {
		if err := appendReceiptHistory(ctx, tx, actor, id, "reversed", reason, &old, nil); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func historicalPeople(ctx context.Context, q querier, trip int64) ([]Person, error) {
	rows, err := q.Query(ctx, `SELECT u.id,u.display_name,u.email FROM users u WHERE u.id IN (
		SELECT payer_user_id FROM expenses WHERE trip_id=$1 UNION SELECT created_by_user_id FROM expenses WHERE trip_id=$1
		UNION SELECT s.user_id FROM expense_shares s JOIN expenses e ON e.id=s.expense_id WHERE e.trip_id=$1
		UNION SELECT from_user_id FROM settlements WHERE trip_id=$1 UNION SELECT to_user_id FROM settlements WHERE trip_id=$1
		UNION SELECT created_by_user_id FROM settlements WHERE trip_id=$1
        UNION SELECT payer_user_id FROM expense_payments WHERE trip_id=$1
        UNION SELECT (a->>'user_id')::bigint FROM expense_payments p CROSS JOIN jsonb_array_elements(p.allocations) a WHERE p.trip_id=$1
        UNION SELECT planned_coverer_user_id FROM expenses WHERE trip_id=$1 AND planned_coverer_user_id IS NOT NULL
 UNION SELECT h.actor_user_id FROM settlement_history h JOIN settlements s ON s.id=h.settlement_id WHERE s.trip_id=$1
 UNION SELECT from_user_id FROM expense_loans WHERE trip_id=$1
        UNION SELECT to_user_id FROM expense_loans WHERE trip_id=$1) ORDER BY u.id`, trip)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	people := []Person{}
	for rows.Next() {
		var p Person
		if err := rows.Scan(&p.UserID, &p.DisplayName, &p.Email); err != nil {
			return nil, err
		}
		people = append(people, p)
	}
	return people, rows.Err()
}
func summary(ctx context.Context, q querier, trip int64) (Summary, error) {
	result := Summary{Currencies: []CurrencyBalance{}, People: []Person{}}
	totals, err := q.Query(ctx, `SELECT sum(amount_minor)::text FROM expenses WHERE trip_id=$1 AND deleted_at IS NULL GROUP BY currency`, trip)
	if err != nil {
		return result, err
	}
	for totals.Next() {
		var total string
		if err := totals.Scan(&total); err != nil {
			totals.Close()
			return result, err
		}
		if _, err := minor(total, true); err != nil {
			totals.Close()
			return result, invalid("planned total exceeds the supported range")
		}
	}
	err = totals.Err()
	totals.Close()
	if err != nil {
		return result, err
	}
	rows, err := q.Query(ctx, `WITH movements AS (
		SELECT currency,payer_user_id AS user_id,amount_minor::numeric AS amount FROM expense_payments WHERE trip_id=$1 AND deleted_at IS NULL
        UNION ALL SELECT e.currency,(s->>'user_id')::bigint,-(s->>'amount_minor')::numeric FROM expense_payments e CROSS JOIN jsonb_array_elements(e.allocations) s WHERE e.trip_id=$1 AND e.deleted_at IS NULL
        UNION ALL SELECT currency,from_user_id,amount_minor::numeric FROM expense_loans WHERE trip_id=$1 AND deleted_at IS NULL
        UNION ALL SELECT currency,to_user_id,-amount_minor::numeric FROM expense_loans WHERE trip_id=$1 AND deleted_at IS NULL
		UNION ALL SELECT currency,from_user_id,amount_minor::numeric FROM settlements WHERE trip_id=$1 AND deleted_at IS NULL
		UNION ALL SELECT currency,to_user_id,-amount_minor::numeric FROM settlements WHERE trip_id=$1 AND deleted_at IS NULL
	), totals AS (SELECT currency,sum(amount_minor)::text AS spent FROM expense_payments WHERE trip_id=$1 AND deleted_at IS NULL GROUP BY currency)
	SELECT m.currency,m.user_id,sum(m.amount)::text,COALESCE(t.spent,'0') FROM movements m LEFT JOIN totals t USING(currency) GROUP BY m.currency,m.user_id,t.spent ORDER BY m.currency,m.user_id`, trip)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var code, amount, total string
		var user int64
		if err := rows.Scan(&code, &user, &amount, &total); err != nil {
			rows.Close()
			return result, err
		}
		if _, err := strconv.ParseInt(amount, 10, 64); err != nil {
			rows.Close()
			return result, invalid("balance exceeds the supported range")
		}
		if _, err := minor(total, true); err != nil {
			rows.Close()
			return result, invalid("spending total exceeds the supported range")
		}
		if len(result.Currencies) == 0 || result.Currencies[len(result.Currencies)-1].Currency != code {
			result.Currencies = append(result.Currencies, CurrencyBalance{Currency: code, Total: total, Balances: []Balance{}, Transfers: []Transfer{}})
		}
		last := &result.Currencies[len(result.Currencies)-1]
		last.Balances = append(last.Balances, Balance{user, amount})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	for i := range result.Currencies {
		transfers, err := suggest(result.Currencies[i].Balances)
		if err != nil {
			return result, err
		}
		result.Currencies[i].Transfers = transfers
	}
	result.People, err = historicalPeople(ctx, q, trip)
	return result, err
}
func (r *Repository) Summary(ctx context.Context, trip int64) (Summary, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Summary{}, err
	}
	defer tx.Rollback(ctx)
	result, err := summary(ctx, tx, trip)
	if err != nil {
		return Summary{}, err
	}
	return result, tx.Commit(ctx)
}
