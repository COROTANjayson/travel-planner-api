package expenses

import (
	"reflect"
	"testing"
)

func TestReservationAndRepayment(t *testing.T) {
	shares := []Share{{1, "80000"}, {2, "80000"}, {3, "80000"}, {4, "80000"}, {5, "80000"}}
	got, err := Allocate(100000, 1, shares)
	want := []Share{{1, "80000"}, {2, "5000"}, {3, "5000"}, {4, "5000"}, {5, "5000"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("reservation: %+v %v", got, err)
	}
	l := Ledger{Expenses: []Record{{ID: 1, ActivityID: 1, ActivityName: "Resort", Description: "Inn", Shares: shares}}, Payments: []Record{{ID: 1, ExpenseID: 1, PayerID: 1, Currency: "PHP", Allocations: got}}}
	d, err := debts(l)
	if err != nil || len(d) != 4 || d[0].Amount != "5000" {
		t.Fatalf("debts: %+v %v", d, err)
	}
	l.Settlements = []Record{{PaymentID: 1, FromID: 2, ToID: 1, Currency: "PHP", Amount: "2000"}}
	d, err = debts(l)
	if err != nil || d[0].Amount != "3000" {
		t.Fatalf("partial receipt: %+v %v", d, err)
	}
	l.Loans = []Record{{ID: 1, FromID: 1, ToID: 3, Amount: "35000", Currency: "PHP"}}
	d, err = debts(l)
	if err != nil || len(d) != 5 || d[4].FromID != 3 || d[4].ToID != 1 || d[4].Amount != "35000" {
		t.Fatalf("cash loan: %+v %v", d, err)
	}
}

func TestPaymentAllocationBoundaries(t *testing.T) {
	for _, tc := range []struct {
		amount int64
		shares []Share
		want   []Share
	}{
		{1, []Share{{3, "2"}, {1, "2"}, {2, "2"}}, []Share{{1, "1"}}},
		{3, []Share{{1, "1"}, {2, "2"}, {3, "3"}}, []Share{{1, "1"}, {2, "1"}, {3, "1"}}},
		{9223372036854775807, []Share{{1, "9223372036854775806"}, {2, "1"}}, []Share{{1, "9223372036854775806"}, {2, "1"}}},
	} {
		got, err := Allocate(tc.amount, 99, tc.shares)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("allocation: %+v want %+v: %v", got, tc.want, err)
		}
	}
	if _, err := Allocate(2, 1, []Share{{1, "1"}}); err == nil {
		t.Fatal("overpayment accepted")
	}
}

func TestPlannedCoverageValidation(t *testing.T) {
	in := writeInput{ExpenseInput: ExpenseInput{Description: "Inn", Currency: "PHP", Amount: "400000", Date: "2026-11-07", SplitMode: "equal", Shares: []Share{{UserID: 1}, {UserID: 2}}, PlannedCovererID: 1, PlannedCoverAmount: "100000"}}
	if _, err := validate(in, expense); err != nil {
		t.Fatal(err)
	}
	for _, planned := range []string{"", "0", "-1", "400001", "1.5"} {
		bad := in
		bad.PlannedCoverAmount = planned
		if _, err := validate(bad, expense); err == nil {
			t.Fatalf("accepted planned amount %q", planned)
		}
	}
	in.PlannedCovererID = 0
	if _, err := validate(in, expense); err == nil {
		t.Fatal("accepted coverage without coverer")
	}
	if validCorrectionReason(" ") || validCorrectionReason(string(make([]byte, 501))) {
		t.Fatal("invalid correction reason accepted")
	}
	if !validCorrectionReason("Correct the amount received") {
		t.Fatal("valid correction reason rejected")
	}
}
