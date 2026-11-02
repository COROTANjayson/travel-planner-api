package integration

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"travel-planner/travel-planner-api/internal/expenses"
	"travel-planner/travel-planner-api/internal/itinerary"
)

func activityPlanScenario(t *testing.T, ctx context.Context, pool *pgxpool.Pool, trip int64, users []int64, request func(int64, string, string, any, string, int, any)) {
	t.Helper()
	owner, editor, bea, viewer, alex := users[0], users[1], users[2], users[3], users[4]
	base := fmt.Sprintf("/api/v1/trips/%d", trip)
	shares := []expenses.Share{}
	for _, id := range users {
		shares = append(shares, expenses.Share{UserID: id})
	}
	entrance := expenses.CostChange{ExpenseInput: expenses.ExpenseInput{Description: "Plan entrance", Currency: "PHP", Amount: "200000", Date: "2026-10-01", AmountBasis: "per_person", SplitMode: "equal", Shares: shares}}
	inn := entrance
	inn.Description = "Plan Inn"
	inn.Amount = "400000"
	inn.AmountBasis = "group"
	inn.PlannedCovererID = alex
	inn.PlannedCoverAmount = "100000"
	plan := map[string]any{"title": "Atomic resort", "starts_at": "2026-10-01T01:00:00Z", "ends_at": "2026-10-01T10:00:00Z", "time_zone": "Asia/Manila", "expense_changes": []expenses.CostChange{entrance, inn}}
	request(bea, "POST", base+"/activities", plan, "member-plan", 403, nil)
	request(viewer, "POST", base+"/activities", plan, "viewer-plan", 403, nil)
	request(editor, "POST", base+"/activities", plan, "", 400, nil)
	bad := inn
	bad.Amount = "0"
	plan["expense_changes"] = []expenses.CostChange{entrance, bad}
	request(editor, "POST", base+"/activities", plan, "invalid-plan", 400, nil)
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM itinerary_items WHERE trip_id=$1 AND title='Atomic resort'`, trip).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed plan saved activity: %d %v", count, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM expenses WHERE trip_id=$1 AND description='Plan entrance'`, trip).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed plan saved partial expenses: %d %v", count, err)
	}
	plan["expense_changes"] = []expenses.CostChange{entrance, inn}
	var activity, replayed itinerary.Activity
	request(editor, "POST", base+"/activities", plan, "atomic-plan", 201, &activity)
	request(editor, "POST", base+"/activities", plan, "atomic-plan", 201, &replayed)
	if activity.ID != replayed.ID {
		t.Fatal("retry duplicated activity")
	}
	plan["title"] = "Different plan"
	request(editor, "POST", base+"/activities", plan, "atomic-plan", 409, nil)
	plan["title"] = "Atomic resort"
	var ledger expenses.Ledger
	request(viewer, "GET", base+"/expense-ledger", nil, "", 200, &ledger)
	var cost expenses.Record
	costs := 0
	for _, e := range ledger.Expenses {
		if e.ActivityID == activity.ID {
			costs++
			if e.Description == "Plan Inn" {
				cost = e
			}
		}
	}
	if costs != 2 {
		t.Fatalf("expected exactly two costs after retry, got %d", costs)
	}
	for _, other := range ledger.Expenses {
		if other.ActivityID != activity.ID {
			foreign := inn
			foreign.ID, foreign.Version = other.ID, other.Version
			plan["expense_changes"] = []expenses.CostChange{foreign}
			request(editor, "PUT", fmt.Sprintf("%s/activities/%d", base, activity.ID), plan, "foreign-cost-plan", 400, nil)
			break
		}
	}
	for _, p := range ledger.Payments {
		if p.ExpenseID == cost.ID {
			t.Fatal("planning recorded a payment")
		}
	}
	path := fmt.Sprintf("%s/expenses/%d", base, cost.ID)
	payment := expenses.TransactionInput{ExpenseID: cost.ID, PayerID: alex, Currency: "PHP", Amount: "100000", Date: "2026-10-01"}
	request(bea, "POST", path+"/payment-preview", payment, "", 403, nil)
	var preview expenses.PaymentPreview
	request(editor, "POST", path+"/payment-preview", payment, "", 200, &preview)
	if preview.OwnCovered != "80000" || preview.BusinessRemaining != "300000" || len(preview.Reimbursements) != 4 {
		t.Fatalf("reservation preview: %+v", preview)
	}
	for _, s := range preview.Reimbursements {
		if s.Amount != "5000" {
			t.Fatal("reservation preview must allocate four 50 reimbursements")
		}
	}
	var status expenses.PaymentStatus
	request(viewer, "GET", path+"/payment-status", nil, "", 200, &status)
	if len(status.Payments) != 0 {
		t.Fatal("preview wrote a payment")
	}
	var deposit expenses.Record
	request(editor, "POST", base+"/expense_payments", payment, "plan-reservation", 201, &deposit)
	payment.PayerID = bea
	payment.MemberID = bea
	payment.Amount = "75001"
	request(editor, "POST", path+"/payment-preview", payment, "", 400, nil)
	request(editor, "POST", base+"/expense_payments", payment, "over-member", 400, nil)
	payment.Amount = "75000"
	request(editor, "POST", path+"/payment-preview", payment, "", 200, &preview)
	request(editor, "POST", base+"/expense_payments", payment, "plan-bea", 201, nil)
	request(viewer, "GET", path+"/payment-status", nil, "", 200, &status)
	for _, m := range status.Members {
		if m.UserID == bea && (m.Paid != "75000" || m.Remaining != "5000" || m.BusinessRemaining != "0") {
			t.Fatalf("Bea payment status: %+v", m)
		}
		if m.UserID == alex && (m.Paid != "80000" || m.Receivable != "20000") {
			t.Fatalf("Alex payment status: %+v", m)
		}
	}
	receipt := expenses.SettlementInput{PaymentID: deposit.ID, FromID: bea, ToID: alex, Currency: "PHP", Amount: "2500", Date: "2026-10-01"}
	request(editor, "POST", base+"/settlements", receipt, "planner-not-recipient", 403, nil)
	request(alex, "POST", base+"/settlements", receipt, "plan-half", 201, nil)
	request(alex, "POST", base+"/settlements", receipt, "plan-half", 200, nil)
	request(viewer, "GET", path+"/payment-status", nil, "", 200, &status)
	for _, m := range status.Members {
		if m.UserID == bea && (m.Paid != "77500" || m.Remaining != "2500") {
			t.Fatalf("partial receipt: %+v", m)
		}
	}
	request(alex, "POST", base+"/settlements", receipt, "plan-rest", 201, nil)
	request(viewer, "GET", path+"/payment-status", nil, "", 200, &status)
	if status.BusinessPaid != "175000" {
		t.Fatal("reimbursements increased business payments")
	}
	for _, m := range status.Members {
		if m.UserID == bea && (m.Paid != "80000" || m.Remaining != "0") {
			t.Fatalf("full receipt: %+v", m)
		}
	}
	inn.ActivityID = activity.ID
	inn.ID = cost.ID
	inn.Version = cost.Version
	inn.Amount = "200000"
	var adjustment expenses.CostPreview
	request(bea, "POST", path+"/cost-preview", inn.ExpenseInput, "", 403, nil)
	request(editor, "POST", path+"/cost-preview", inn.ExpenseInput, "", 200, &adjustment)
	if adjustment.Before.BusinessPaid != adjustment.After.BusinessPaid || adjustment.After.Expense.Amount != "200000" {
		t.Fatal("cost preview did not preserve payments")
	}
	// One stale cost rolls back the activity and all costs in the batch.
	plan["title"] = "Should roll back"
	inn.Version = 999
	plan["expense_changes"] = []expenses.CostChange{inn}
	activityPath := fmt.Sprintf("%s/activities/%d", base, activity.ID)
	request(editor, "PUT", activityPath, plan, "stale-plan", 409, nil)
	request(viewer, "GET", activityPath, nil, "", 200, &replayed)
	if replayed.Title != "Atomic resort" {
		t.Fatal("stale cost left changed activity")
	}
	inn.Version = cost.Version
	plan["expense_changes"] = []expenses.CostChange{inn}
	plan["title"] = "Adjusted resort"
	request(editor, "PUT", activityPath, plan, "adjust-plan", 200, nil)
	request(editor, "PUT", activityPath, plan, "adjust-plan", 200, nil)
	request(viewer, "GET", path+"/payment-status", nil, "", 200, &status)
	if len(status.Payments) != 2 || status.BusinessPaid != "175000" || status.Expense.Amount != "200000" {
		t.Fatal("editing lost or duplicated payments")
	}
	for _, m := range status.Members {
		if m.UserID == alex && m.Credit != "40000" {
			t.Fatalf("adjusted cost lost Alex credit: %+v", m)
		}
	}
	request(owner, "DELETE", activityPath, nil, "", 204, nil)
	request(viewer, "GET", path+"/payment-status", nil, "", 200, &status)
	if status.Expense.ActivityName != "Adjusted resort" || len(status.Payments) != 2 {
		t.Fatal("removed activity lost financial history")
	}
}
