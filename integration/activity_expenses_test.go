package integration

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"travel-planner/travel-planner-api/internal/expenses"
	"travel-planner/travel-planner-api/internal/itinerary"
)

func activityExpenseScenario(t *testing.T, ctx context.Context, pool *pgxpool.Pool, trip int64, users []int64, request func(int64, string, string, any, string, int, any)) {
	t.Helper()
	owner, editor, member, viewer, fifth := users[0], users[1], users[2], users[3], users[4]
	if _, err := pool.Exec(ctx, `INSERT INTO trip_members(trip_id,user_id,role) VALUES($1,$2,'member')`, trip, fifth); err != nil {
		t.Fatal(err)
	}
	base := fmt.Sprintf("/api/v1/trips/%d", trip)
	var a itinerary.Activity
	request(owner, "POST", base+"/activities", map[string]any{"title": "Resort", "starts_at": "2026-10-01T01:00:00Z", "ends_at": "2026-10-01T10:00:00Z", "time_zone": "Asia/Manila"}, "", 201, &a)
	shares := []expenses.Share{}
	for _, id := range users {
		shares = append(shares, expenses.Share{UserID: id})
	}
	in := expenses.ExpenseInput{ActivityID: a.ID, Description: "Inn", Currency: "PHP", Amount: "400000", Date: "2026-09-01", SplitMode: "equal", Shares: shares}
	request(member, "POST", base+"/expenses", in, "shared-member", 403, nil)
	request(viewer, "POST", base+"/expenses", in, "shared-viewer", 403, nil)
	entrance := in
	entrance.Description = "Entrance"
	entrance.Amount = "200000"
	entrance.AmountBasis = "per_person"
	request(owner, "POST", base+"/expenses", entrance, "planned-entrance", 201, nil)
	var inn expenses.Record
	request(owner, "POST", base+"/expenses", in, "planned-inn", 201, &inn)
	var l expenses.Ledger
	request(owner, "GET", base+"/expense-ledger", nil, "", 200, &l)
	if len(l.Payments) != 0 || len(l.Debts) != 0 {
		t.Fatal("planning created payments or member debts")
	}
	payment := expenses.TransactionInput{ExpenseID: inn.ID, PayerID: owner, Currency: "PHP", Amount: "100000", Date: "2026-09-01"}
	request(member, "POST", base+"/expense_payments", payment, "not-editor", 403, nil)
	var deposit expenses.Record
	request(editor, "POST", base+"/expense_payments", payment, "reservation", 201, &deposit)
	request(editor, "POST", base+"/expense_payments", payment, "reservation", 200, nil)
	request(viewer, "GET", base+"/expense-ledger", nil, "", 200, &l)
	if len(l.Payments) != 1 || len(l.Debts) != 4 || l.Payments[0].Allocations[0].Amount != "80000" {
		t.Fatalf("deposit: %+v", l)
	}
	for _, d := range l.Debts {
		if d.Amount != "5000" {
			t.Fatalf("expected four 50 reimbursements: %+v", l.Debts)
		}
	}
	manual := expenses.TransactionInput{ExpenseID: inn.ID, PayerID: editor, Currency: "PHP", Amount: "10000", Date: "2026-09-01", Allocations: []expenses.Share{{UserID: member, Amount: "10000"}}}
	var manualPayment expenses.Record
	request(owner, "POST", base+"/expense_payments", manual, "manual-coverage", 201, &manualPayment)
	if len(manualPayment.Allocations) != 1 || manualPayment.Allocations[0].UserID != member {
		t.Fatal("manual coverage was not preserved")
	}
	repayment := expenses.SettlementInput{PaymentID: deposit.ID, FromID: member, ToID: owner, Currency: "PHP", Amount: "2500", Date: "2026-09-02"}
	request(member, "POST", base+"/settlements", repayment, "self-report", 403, nil)
	var receipt expenses.Record
	request(owner, "POST", base+"/settlements", repayment, "received", 201, &receipt)
	request(owner, "POST", base+"/settlements", repayment, "received", 200, nil)
	repayment.Amount = "5001"
	request(owner, "POST", base+"/settlements", repayment, "overpay", 400, nil)
	repayment.Amount = "100"
	repayment.LoanID = 123
	request(owner, "POST", base+"/settlements", repayment, "two-sources", 400, nil)
	cover := expenses.ExpenseInput{ActivityID: a.ID, Description: "Food", Currency: "PHP", Amount: "35000", Date: "2026-10-01", PayerID: member, SplitMode: "equal", Shares: []expenses.Share{{UserID: viewer}}}
	var food expenses.Record
	request(member, "POST", base+"/expenses", cover, "food-cover", 201, &food)
	otherFood := cover
	otherFood.PayerID = owner
	otherFood.Amount = "30000"
	otherFood.Shares = []expenses.Share{{UserID: editor}}
	request(owner, "POST", base+"/expenses", otherFood, "other-food", 201, nil)
	cover.PayerID = owner
	request(member, "POST", base+"/expenses", cover, "false-payer", 403, nil)
	cash := expenses.TransactionInput{ActivityID: a.ID, Description: "Cash", Currency: "PHP", Amount: "12500", Date: "2026-10-01", FromID: member, ToID: editor}
	var cashRecord expenses.Record
	request(member, "POST", base+"/expense_loans", cash, "cash", 201, &cashRecord)
	cash.FromID = owner
	request(member, "POST", base+"/expense_loans", cash, "false-lender", 403, nil)
	var summary expenses.Summary
	request(owner, "GET", base+"/balances", nil, "", 200, &summary)
	if summary.Currencies[0].Total != "175000" {
		t.Fatalf("loan added spending: %+v", summary)
	}
	legacyReceipt := expenses.SettlementInput{FromID: member, ToID: owner, Currency: "PHP", Amount: "2500", Date: "2026-09-03"}
	var legacy expenses.Record
	request(owner, "POST", base+"/settlements", legacyReceipt, "old-receipt", 201, &legacy)
	before := append([]expenses.Balance(nil), summary.Currencies[0].Balances...)
	legacyReceipt.PaymentID = deposit.ID
	legacyReceipt.Version = legacy.Version
	legacyReceipt.CorrectionReason = "Associate existing receipt"
	request(owner, "PUT", fmt.Sprintf("%s/settlements/%d", base, legacy.ID), legacyReceipt, "", 200, nil)
	request(owner, "GET", base+"/balances", nil, "", 200, &summary)
	for i, b := range summary.Currencies[0].Balances {
		expected := before[i].Amount
		if b.UserID == member {
			v, _ := strconv.ParseInt(expected, 10, 64)
			expected = strconv.FormatInt(v+2500, 10)
		}
		if b.UserID == owner {
			v, _ := strconv.ParseInt(expected, 10, 64)
			expected = strconv.FormatInt(v-2500, 10)
		}
		if b.Amount != expected {
			t.Fatal("allocating receipt duplicated its movement")
		}
	}
	in.Amount = "350000"
	in.Version = inn.Version
	request(editor, "PUT", fmt.Sprintf("%s/expenses/%d", base, inn.ID), in, "", 200, &inn)
	payment.Amount = "240001"
	request(owner, "POST", base+"/expense_payments", payment, "above-adjusted-bill", 400, nil)
	request(owner, "GET", base+"/expense-ledger", nil, "", 200, &l)
	if l.Payments[0].Amount != "100000" || l.Payments[0].Allocations[0].Amount != "80000" {
		t.Fatal("cost edit rewrote contribution")
	}
	request(editor, "PUT", fmt.Sprintf("%s/expenses/%d", base, inn.ID), in, "", 409, nil)
	request(owner, "DELETE", fmt.Sprintf("%s/expenses/%d?version=%d", base, inn.ID, inn.Version), nil, "", 409, nil)
	request(owner, "DELETE", fmt.Sprintf("%s/activities/%d", base, a.ID), nil, "", 204, nil)
	l = expenses.Ledger{}
	request(owner, "GET", base+"/expense-ledger", nil, "", 200, &l)
	if l.Expenses[0].ActivityID != 0 || l.Expenses[0].ActivityName != "Resort" || l.Loans[0].ActivityName != "Resort" {
		t.Fatal("activity deletion lost financial history")
	}

	// Alex is an ordinary member; a different editor records business payments.
	request(owner, "POST", base+"/activities", map[string]any{"title": "Payment roles", "starts_at": "2026-10-01T01:00:00Z", "ends_at": "2026-10-01T10:00:00Z", "time_zone": "Asia/Manila"}, "", 201, &a)
	planned := expenses.ExpenseInput{ActivityID: a.ID, Description: "Inn roles", Currency: "PHP", Amount: "400000", Date: "2026-09-01", SplitMode: "equal", Shares: shares, PlannedCovererID: fifth, PlannedCoverAmount: "100000"}
	var roleCost expenses.Record
	request(editor, "POST", base+"/expenses", planned, "planned-coverer", 201, &roleCost)
	request(editor, "POST", base+"/expenses", planned, "planned-coverer", 200, nil)
	if roleCost.PlannedCovererID != fifth || roleCost.PlannedCoverAmount != "100000" {
		t.Fatal("planned cover was not persisted")
	}
	request(owner, "GET", base+"/expense-ledger", nil, "", 200, &l)
	for _, p := range l.Payments {
		if p.ExpenseID == roleCost.ID {
			t.Fatal("planned cover created a payment")
		}
	}
	actual := expenses.TransactionInput{ExpenseID: roleCost.ID, PayerID: fifth, Currency: "PHP", Amount: "100000", Date: "2026-09-01"}
	var roleDeposit expenses.Record
	request(fifth, "POST", base+"/expense_payments", actual, "member-business", 403, nil)
	request(editor, "POST", base+"/expense_payments", actual, "editor-business", 201, &roleDeposit)
	request(viewer, "GET", fmt.Sprintf("%s/expenses/%d", base, roleCost.ID), nil, "", 200, &roleCost)
	if roleCost.PlannedCoverAmount != "100000" {
		t.Fatal("recording payment changed the coverage plan")
	}
	actual.PayerID = member
	actual.Amount = "75000"
	actual.Allocations = []expenses.Share{{UserID: member, Amount: "75000"}}
	request(editor, "POST", base+"/expense_payments", actual, "bea-business", 201, nil)
	receive := expenses.SettlementInput{PaymentID: roleDeposit.ID, FromID: member, ToID: fifth, Currency: "PHP", Amount: "2500", Date: "2026-09-02"}
	request(owner, "POST", base+"/settlements", receive, "owner-cannot-receive", 403, nil)
	request(editor, "POST", base+"/settlements", receive, "editor-cannot-receive", 403, nil)
	request(member, "POST", base+"/settlements", receive, "debtor-cannot-receive", 403, nil)
	var received expenses.Record
	request(fifth, "POST", base+"/settlements", receive, "alex-received", 201, &received)
	request(fifth, "POST", base+"/settlements", receive, "alex-received", 200, nil)
	receive.Version = received.Version
	receive.Amount = "2000"
	receiptPath := fmt.Sprintf("%s/settlements/%d", base, received.ID)
	request(editor, "PUT", receiptPath, receive, "", 400, nil)
	receive.CorrectionReason = "Correct the amount received"
	request(member, "PUT", receiptPath, receive, "", 403, nil)
	request(editor, "PUT", receiptPath, receive, "", 200, &received)
	request(editor, "DELETE", fmt.Sprintf("%s?version=%d", receiptPath, received.Version), nil, "", 400, nil)
	request(editor, "DELETE", fmt.Sprintf("%s?version=%d&correction_reason=Duplicate", receiptPath, received.Version), nil, "", 204, nil)
	request(viewer, "GET", base+"/expense-ledger", nil, "", 200, &l)
	if len(l.ReversedSettlements) != 1 || l.ReversedSettlements[0].ID != received.ID {
		t.Fatal("reversed receipt history is not discoverable")
	}
	var history []expenses.ReceiptHistory
	request(viewer, "GET", receiptPath+"/history", nil, "", 200, &history)
	if len(history) != 3 || history[0].ActorID != fifth || history[1].ActorID != editor || history[2].Action != "reversed" || history[1].Before.Amount != "2500" || history[1].After.Amount != "2000" {
		t.Fatalf("receipt history: %+v", history)
	}
	receive.Version = 0
	receive.Amount = "5000"
	receive.CorrectionReason = ""
	request(fifth, "POST", base+"/settlements", receive, "alex-fully-received", 201, nil)
	request(viewer, "GET", base+"/expense-ledger", nil, "", 200, &l)
	for _, d := range l.Debts {
		if d.PaymentID == roleDeposit.ID && d.FromID == member && d.Amount != "0" {
			t.Fatal("full reimbursement did not settle Bea's debt")
		}
	}
	activityPlanScenario(t, ctx, pool, trip, users, request)
	// Leave the original ledger fixture untouched for its existing contract tests.
	for _, table := range []string{"settlements", "expense_payments", "expense_loans", "expense_shares", "expenses"} {
		query := `DELETE FROM ` + table + ` WHERE trip_id=$1`
		if table == "expense_shares" {
			query = `DELETE FROM expense_shares WHERE expense_id IN(SELECT id FROM expenses WHERE trip_id=$1)`
		}
		if _, err := pool.Exec(ctx, query, trip); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `DELETE FROM trip_members WHERE trip_id=$1 AND user_id=$2`, trip, fifth); err != nil {
		t.Fatal(err)
	}
}
