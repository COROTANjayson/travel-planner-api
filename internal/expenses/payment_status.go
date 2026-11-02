package expenses

import (
	"context"
	"sort"
	"strconv"

	"travel-planner/travel-planner-api/internal/apperror"
)

type MemberPaymentStatus struct {
	UserID            int64  `json:"user_id"`
	Share             string `json:"share_minor"`
	Paid              string `json:"paid_minor"`
	Remaining         string `json:"remaining_minor"`
	BusinessRemaining string `json:"business_remaining_minor"`
	Receivable        string `json:"receivable_minor"`
	Credit            string `json:"credit_minor"`
	Reimbursements    []Debt `json:"reimbursements"`
}
type PaymentStatus struct {
	Expense           Record                `json:"expense"`
	BusinessPaid      string                `json:"business_paid_minor"`
	BusinessRemaining string                `json:"business_remaining_minor"`
	Members           []MemberPaymentStatus `json:"members"`
	Payments          []Record              `json:"payments"`
	Receipts          []Record              `json:"receipts"`
	People            []Person              `json:"people"`
}

func storedMinor(value string) int64 { n, _ := strconv.ParseInt(value, 10, 64); return n }
func minorText(value int64) string   { return strconv.FormatInt(value, 10) }

func paymentStatus(l Ledger, id int64) (PaymentStatus, error) {
	result := PaymentStatus{Members: []MemberPaymentStatus{}, Payments: []Record{}, Receipts: []Record{}, People: l.People}
	found := false
	for _, cost := range l.Expenses {
		if cost.ID == id {
			result.Expense = cost
			found = true
			break
		}
	}
	if !found {
		return result, apperror.ErrNotFound
	}
	shares, remaining := map[int64]int64{}, map[int64]int64{}
	for _, share := range result.Expense.Shares {
		shares[share.UserID] = storedMinor(share.Amount)
		remaining[share.UserID] = storedMinor(share.Amount)
	}
	sources := map[int64]bool{}
	var paid int64
	for _, payment := range l.Payments {
		if payment.ExpenseID != id {
			continue
		}
		result.Payments = append(result.Payments, payment)
		sources[payment.ID] = true
		paid += storedMinor(payment.Amount)
		if _, ok := remaining[payment.PayerID]; !ok {
			remaining[payment.PayerID] = 0
		}
		for _, allocation := range payment.Allocations {
			remaining[allocation.UserID] -= storedMinor(allocation.Amount)
		}
	}
	for _, receipt := range l.Settlements {
		if sources[receipt.PaymentID] {
			result.Receipts = append(result.Receipts, receipt)
		}
	}
	result.BusinessPaid = minorText(paid)
	result.BusinessRemaining = minorText(storedMinor(result.Expense.Amount) - paid)
	ids := []int64{}
	for id := range remaining {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, user := range ids {
		business := max(0, remaining[user])
		credit := max(0, -remaining[user])
		var reimburse, receivable int64
		member := MemberPaymentStatus{UserID: user, Share: minorText(shares[user]), Reimbursements: []Debt{}}
		for _, debt := range l.Debts {
			if !sources[debt.PaymentID] {
				continue
			}
			amount := storedMinor(debt.Amount)
			if debt.FromID == user {
				member.Reimbursements = append(member.Reimbursements, debt)
				reimburse += max(0, amount)
				credit += max(0, -amount)
			}
			if debt.ToID == user {
				receivable += max(0, amount)
			}
		}
		member.BusinessRemaining = minorText(business)
		member.Remaining = minorText(business + reimburse)
		member.Paid = minorText(max(0, shares[user]-business-reimburse))
		member.Receivable = minorText(receivable)
		member.Credit = minorText(credit)
		result.Members = append(result.Members, member)
	}
	return result, nil
}

func (r *Repository) PaymentStatus(ctx context.Context, trip, id int64) (PaymentStatus, error) {
	l, err := r.Ledger(ctx, trip)
	if err != nil {
		return PaymentStatus{}, err
	}
	return paymentStatus(l, id)
}

type PaymentPreview struct {
	Allocations       []Share       `json:"allocations"`
	OwnCovered        string        `json:"own_covered_minor"`
	Reimbursements    []Share       `json:"reimbursements"`
	BusinessRemaining string        `json:"business_remaining_minor"`
	After             PaymentStatus `json:"after"`
}

type CostPreview struct {
	Before PaymentStatus `json:"before"`
	After  PaymentStatus `json:"after"`
}

func (r *Repository) PreviewCost(ctx context.Context, actor, trip, id int64, in writeInput) (CostPreview, error) {
	var result CostPreview
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	roles, err := lockWriter(ctx, tx, actor, trip)
	if err != nil {
		return result, err
	}
	if !editor(roles[actor]) {
		return result, apperror.ErrForbidden
	}
	old, err := get(ctx, tx, trip, expense, id)
	if err != nil {
		return result, err
	}
	if old.Version != in.Version {
		return result, conflict("this expense changed; reload before reviewing")
	}
	activityName, err := validateActivityWrite(ctx, tx, actor, trip, expense, &in, roles, &old)
	if err != nil {
		return result, err
	}
	if err = participantsValid(ctx, tx, trip, expense, in, roles, &old); err != nil {
		return result, err
	}
	l, err := ledger(ctx, tx, trip)
	if err != nil {
		return result, err
	}
	result.Before, err = paymentStatus(l, id)
	if err != nil {
		return result, err
	}
	for i := range l.Expenses {
		if l.Expenses[i].ID == id {
			l.Expenses[i].Amount = in.Amount
			l.Expenses[i].Shares = in.Shares
			l.Expenses[i].Description = in.Description
			l.Expenses[i].Currency = in.Currency
			l.Expenses[i].ActivityID, l.Expenses[i].ActivityName = in.ActivityID, activityName
			l.Expenses[i].Date, l.Expenses[i].AmountBasis, l.Expenses[i].SplitMode = in.Date, in.AmountBasis, in.SplitMode
			l.Expenses[i].PlannedCovererID, l.Expenses[i].PlannedCoverAmount = in.PlannedCovererID, in.PlannedCoverAmount
			l.Expenses[i].PayerID = in.PayerID
		}
	}
	l.Debts, err = debts(l)
	if err != nil {
		return result, err
	}
	result.After, err = paymentStatus(l, id)
	return result, err
}

func (r *Repository) PreviewPayment(ctx context.Context, actor, trip int64, in writeInput) (PaymentPreview, error) {
	result := PaymentPreview{Reimbursements: []Share{}}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	roles, err := lockWriter(ctx, tx, actor, trip)
	if err != nil {
		return result, err
	}
	if _, err = validateActivityWrite(ctx, tx, actor, trip, businessPayment, &in, roles, nil); err != nil {
		return result, err
	}
	l, err := ledger(ctx, tx, trip)
	if err != nil {
		return result, err
	}
	l.Payments = append(l.Payments, Record{ID: -1, ExpenseID: in.ExpenseID, PayerID: in.PayerID, Currency: in.Currency, Amount: in.Amount, PaidOn: in.Date, Allocations: in.Allocations})
	l.Debts, err = debts(l)
	if err != nil {
		return result, err
	}
	result.After, err = paymentStatus(l, in.ExpenseID)
	if err != nil {
		return result, err
	}
	result.Allocations = in.Allocations
	result.OwnCovered = "0"
	result.BusinessRemaining = result.After.BusinessRemaining
	for _, share := range in.Allocations {
		if share.UserID == in.PayerID {
			result.OwnCovered = share.Amount
		} else {
			result.Reimbursements = append(result.Reimbursements, share)
		}
	}
	return result, nil
}
