package expenses

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/jackc/pgx/v5"
	"travel-planner/travel-planner-api/internal/apperror"
)

func editor(role string) bool { return role == "owner" || role == "editor" }

func validateActivityWrite(ctx context.Context, tx pgx.Tx, actor, trip int64, k kind, in *writeInput, roles map[int64]string, old *Record) (string, error) {
	name := ""
	if old != nil {
		name = old.ActivityName
	}
	if in.ActivityID > 0 {
		if err := tx.QueryRow(ctx, `SELECT title FROM itinerary_items WHERE trip_id=$1 AND id=$2 FOR SHARE`, trip, in.ActivityID).Scan(&name); err != nil {
			if err == pgx.ErrNoRows {
				return "", invalid("activity must belong to this trip")
			}
			return "", err
		}
	}
	if k == expense {
		if !editor(roles[actor]) && ((old == nil && in.PlannedCovererID != 0) || (old != nil && (old.PlannedCovererID != in.PlannedCovererID || old.PlannedCoverAmount != in.PlannedCoverAmount))) {
			return "", apperror.ErrForbidden
		}
		if in.PlannedCovererID != 0 && roles[in.PlannedCovererID] == "" && (old == nil || old.PlannedCovererID != in.PlannedCovererID) {
			return "", invalid("planned coverer must belong to this trip")
		}
		if !editor(roles[actor]) && (in.PayerID != actor && (old == nil || old.PayerID != actor)) {
			return "", apperror.ErrForbidden
		}
		if in.ActivityID == 0 && in.PayerID == 0 && (old == nil || old.ActivityName == "") {
			return "", invalid("choose an activity")
		}
		if (in.ActivityID > 0 || (old != nil && old.ActivityName != "")) && !editor(roles[actor]) && ((old == nil && in.PayerID != actor) || (old != nil && (old.PayerID != actor || old.CreatorID != actor))) {
			return "", apperror.ErrForbidden
		}
		if old != nil {
			if old.ActivityID > 0 && in.ActivityID == 0 {
				return "", invalid("choose an activity for this cost")
			}
			if in.PayerID > 0 && in.PayerID != old.PayerID {
				return "", invalid("record or correct the business payment separately")
			}
			in.PayerID = old.PayerID
			var hasPayments bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM expense_payments WHERE expense_id=$1 AND deleted_at IS NULL)`, old.ID).Scan(&hasPayments); err != nil {
				return "", err
			}
			if hasPayments && old.Currency != in.Currency {
				return "", invalid("reverse payments before changing currency")
			}
		}
	}
	if k == loan {
		if !editor(roles[actor]) && in.FromID != actor {
			return "", apperror.ErrForbidden
		}
		if roles[in.FromID] == "" || roles[in.ToID] == "" {
			if old == nil || old.FromID != in.FromID || old.ToID != in.ToID {
				return "", invalid("loan travelers must be participants")
			}
		}
		if old != nil {
			var used bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM settlements WHERE loan_id=$1 AND deleted_at IS NULL)`, old.ID).Scan(&used); err != nil {
				return "", err
			}
			if used && (old.Currency != in.Currency || old.FromID != in.FromID || old.ToID != in.ToID) {
				return "", invalid("reverse repayments before changing loan people or currency")
			}
		}
	}
	if k == businessPayment {
		if !editor(roles[actor]) {
			return "", apperror.ErrForbidden
		}
		cost, err := get(ctx, tx, trip, expense, in.ExpenseID)
		if err != nil {
			return "", err
		}
		if cost.Currency != in.Currency {
			return "", invalid("payment currency must match the cost")
		}
		if roles[in.PayerID] == "" && (old == nil || old.PayerID != in.PayerID) {
			return "", invalid("payer must be a participant")
		}
		if old != nil && (old.ExpenseID != in.ExpenseID || old.PayerID != in.PayerID || old.Currency != in.Currency) {
			return "", invalid("reverse the payment before changing its cost or payer")
		}
		var used bool
		if old != nil {
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM settlements WHERE payment_id=$1 AND deleted_at IS NULL)`, old.ID).Scan(&used); err != nil {
				return "", err
			}
			if used {
				return "", conflict("reverse linked repayments before correcting this contribution")
			}
		}
		remaining := map[int64]int64{}
		var funded int64
		for _, s := range cost.Shares {
			v, _ := minor(s.Amount, true)
			remaining[s.UserID] = v
		}
		rows, err := tx.Query(ctx, `SELECT allocations FROM expense_payments WHERE expense_id=$1 AND deleted_at IS NULL AND id<>$2`, cost.ID, func() int64 {
			if old != nil {
				return old.ID
			}
			return 0
		}())
		if err != nil {
			return "", err
		}
		for rows.Next() {
			var data []byte
			if err = rows.Scan(&data); err != nil {
				rows.Close()
				return "", err
			}
			var a []Share
			if err = json.Unmarshal(data, &a); err != nil {
				rows.Close()
				return "", err
			}
			for _, s := range a {
				v, _ := minor(s.Amount, true)
				funded, err = checkedAdd(funded, v)
				if err != nil {
					rows.Close()
					return "", err
				}
				remaining[s.UserID] -= v
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return "", err
		}
		shares := []Share{}
		for id, v := range remaining {
			shares = append(shares, Share{id, strconv.FormatInt(max(0, v), 10)})
		}
		amount, _ := minor(in.Amount, false)
		costAmount, _ := minor(cost.Amount, false)
		if amount > costAmount-funded {
			return "", invalid("payment exceeds the remaining business amount")
		}
		if in.MemberID != 0 {
			if in.MemberID != in.PayerID || amount > remaining[in.MemberID] {
				return "", invalid("member payments must cover that member's remaining business share")
			}
			if len(in.Allocations) > 0 && (len(in.Allocations) != 1 || in.Allocations[0].UserID != in.MemberID || in.Allocations[0].Amount != in.Amount) {
				return "", invalid("member payment allocation must match the selected member")
			}
			in.Allocations = []Share{{UserID: in.MemberID, Amount: in.Amount}}
		}
		if len(in.Allocations) == 0 {
			in.Allocations, err = Allocate(amount, in.PayerID, shares)
			if err != nil {
				return "", err
			}
		} else {
			seen := map[int64]bool{}
			var sum int64
			for _, a := range in.Allocations {
				v, err := minor(a.Amount, true)
				if err != nil {
					return "", err
				}
				if seen[a.UserID] || v > max(0, remaining[a.UserID]) || a.UserID <= 0 {
					return "", invalid("allocations must be unique and within uncovered shares")
				}
				if _, ok := remaining[a.UserID]; !ok {
					return "", invalid("allocation user must share the cost")
				}
				seen[a.UserID] = true
				sum, err = checkedAdd(sum, v)
				if err != nil {
					return "", err
				}
			}
			if sum != amount {
				return "", invalid("allocations must equal payment amount")
			}
		}
	}
	if k == settlement {
		if old == nil && (in.PaymentID > 0 || in.LoanID > 0) && in.ToID != actor {
			return "", apperror.ErrForbidden
		}
		if old != nil {
			if old.PaymentID == 0 && old.LoanID == 0 && (in.PaymentID > 0 || in.LoanID > 0) && actor != old.ToID {
				var newReceipt bool
				if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM settlement_history WHERE settlement_id=$1 AND action='received')`, old.ID).Scan(&newReceipt); err != nil {
					return "", err
				}
				if newReceipt {
					return "", apperror.ErrForbidden
				}
			}
			if !validCorrectionReason(in.CorrectionReason) {
				return "", invalid("a correction reason (1 to 500 characters) is required")
			}
			if (old.PaymentID > 0 || old.LoanID > 0) && (old.PaymentID != in.PaymentID || old.LoanID != in.LoanID || old.FromID != in.FromID || old.ToID != in.ToID || old.Currency != in.Currency) {
				return "", invalid("reverse the receipt before changing its debt or people")
			}
		}
		if in.PaymentID > 0 || in.LoanID > 0 {
			if !editor(roles[actor]) && in.ToID != actor {
				return "", apperror.ErrForbidden
			}
			l, err := ledger(ctx, tx, trip)
			if err != nil {
				return "", err
			}
			var outstanding int64
			found := false
			for _, d := range l.Debts {
				if d.PaymentID == in.PaymentID && d.LoanID == in.LoanID && d.FromID == in.FromID && d.ToID == in.ToID && d.Currency == in.Currency {
					outstanding, _ = strconv.ParseInt(d.Amount, 10, 64)
					found = true
					break
				}
			}
			if old != nil && old.PaymentID == in.PaymentID && old.LoanID == in.LoanID && old.FromID == in.FromID && old.ToID == in.ToID && old.Currency == in.Currency {
				v, _ := minor(old.Amount, false)
				outstanding, err = checkedAdd(outstanding, v)
				if err != nil {
					return "", err
				}
			}
			v, _ := minor(in.Amount, false)
			if !found || v > outstanding {
				return "", invalid("repayment must match an outstanding debt and cannot exceed it")
			}
		} else if !editor(roles[actor]) && (old == nil || old.PaymentID > 0 || old.LoanID > 0) {
			return "", apperror.ErrForbidden
		}
	}
	return name, nil
}

func saveTransaction(ctx context.Context, tx pgx.Tx, actor, trip int64, k kind, id int64, in writeInput, key string, hash []byte, name string) (int64, error) {
	var err error
	if k == businessPayment {
		a, _ := json.Marshal(in.Allocations)
		if id == 0 {
			err = tx.QueryRow(ctx, `INSERT INTO expense_payments(trip_id,expense_id,created_by_user_id,payer_user_id,currency,amount_minor,paid_on,allocations,idempotency_key,request_hash) VALUES($1,$2,$3,$4,$5,$6::text::bigint,$7::text::date,$8::jsonb,$9,$10) RETURNING id`, trip, in.ExpenseID, actor, in.PayerID, in.Currency, in.Amount, in.Date, a, key, hash).Scan(&id)
		} else {
			_, err = tx.Exec(ctx, `UPDATE expense_payments SET amount_minor=$2::text::bigint,paid_on=$3::text::date,allocations=$4::jsonb,version=version+1,updated_at=now() WHERE id=$1`, id, in.Amount, in.Date, a)
		}
	} else {
		if id == 0 {
			err = tx.QueryRow(ctx, `INSERT INTO expense_loans(trip_id,activity_id,activity_name,description,created_by_user_id,from_user_id,to_user_id,currency,amount_minor,paid_on,idempotency_key,request_hash) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9::text::bigint,$10::text::date,$11,$12) RETURNING id`, trip, in.ActivityID, name, in.Description, actor, in.FromID, in.ToID, in.Currency, in.Amount, in.Date, key, hash).Scan(&id)
		} else {
			_, err = tx.Exec(ctx, `UPDATE expense_loans SET activity_id=$2,activity_name=$3,description=$4,from_user_id=$5,to_user_id=$6,currency=$7,amount_minor=$8::text::bigint,paid_on=$9::text::date,version=version+1,updated_at=now() WHERE id=$1`, id, in.ActivityID, name, in.Description, in.FromID, in.ToID, in.Currency, in.Amount, in.Date)
		}
	}
	return id, err
}

func validateRemoval(ctx context.Context, tx pgx.Tx, actor, trip int64, k kind, old Record, roles map[int64]string) error {
	if !editor(roles[actor]) && ((k == expense && old.ActivityName != "" && old.PayerID != actor) || (k == loan && old.FromID != actor)) {
		return apperror.ErrForbidden
	}
	if k == settlement && !editor(roles[actor]) && old.ToID != actor {
		return apperror.ErrForbidden
	}
	if k == businessPayment && !editor(roles[actor]) {
		return apperror.ErrForbidden
	}
	var used bool
	var err error
	if k == expense {
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM expense_payments WHERE expense_id=$1 AND deleted_at IS NULL)`, old.ID).Scan(&used)
	}
	if k == businessPayment {
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM settlements WHERE payment_id=$1 AND deleted_at IS NULL)`, old.ID).Scan(&used)
	}
	if k == loan {
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM settlements WHERE loan_id=$1 AND deleted_at IS NULL)`, old.ID).Scan(&used)
	}
	if err != nil {
		return err
	}
	if used {
		return conflict("reverse related payments before deleting this record")
	}
	return nil
}
