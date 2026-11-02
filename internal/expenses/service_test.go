package expenses

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"

	"travel-planner/travel-planner-api/internal/apperror"
)

func TestSplitsAndValidation(t *testing.T) {
	valid := writeInput{ExpenseInput: ExpenseInput{Description: " Meal ", Currency: "PHP", Amount: "1", Date: "2026-09-01", PayerID: 1, SplitMode: "equal", Shares: []Share{{UserID: 3}, {UserID: 1}, {UserID: 2}}}}
	got, err := validate(valid, expense)
	if err != nil || got.Description != "Meal" || !reflect.DeepEqual(got.Shares, []Share{{1, "1"}, {2, "0"}, {3, "0"}}) {
		t.Fatalf("tiny equal split: %+v %v", got, err)
	}
	if valid.Shares[0].UserID != 3 || valid.Shares[0].Amount != "" {
		t.Fatal("validation mutated the caller's shares")
	}
	for _, tc := range []struct {
		name   string
		change func(*writeInput)
	}{
		{"duplicate", func(in *writeInput) { in.Shares = []Share{{UserID: 1}, {UserID: 1}} }},
		{"no shares", func(in *writeInput) { in.Shares = nil }},
		{"equal amount supplied", func(in *writeInput) { in.Shares = []Share{{1, "1"}} }},
		{"negative", func(in *writeInput) { in.Amount = "-1" }},
		{"zero", func(in *writeInput) { in.Amount = "0" }},
		{"decimal", func(in *writeInput) { in.Amount = "1.5" }},
		{"noncanonical", func(in *writeInput) { in.Amount = "01" }},
		{"overflow", func(in *writeInput) { in.Amount = "9223372036854775808" }},
		{"currency", func(in *writeInput) { in.Currency = "XYZ" }},
		{"date", func(in *writeInput) { in.Date = "2026-02-30" }},
		{"year zero", func(in *writeInput) { in.Date = "0000-01-01" }},
		{"exact mismatch", func(in *writeInput) { in.SplitMode = "exact"; in.Shares = []Share{{1, "0"}} }},
		{"exact overflow", func(in *writeInput) {
			in.SplitMode = "exact"
			in.Shares = []Share{{1, "9223372036854775807"}, {2, "1"}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := valid
			tc.change(&in)
			if _, err := validate(in, expense); !errors.Is(err, apperror.ErrInvalid) {
				t.Fatalf("wanted invalid: %v", err)
			}
		})
	}
	valid.Amount = "9223372036854775807"
	if _, err := validate(valid, expense); err != nil {
		t.Fatal(err)
	}
	valid.SplitMode = "exact"
	valid.Shares = []Share{{1, "9223372036854775807"}, {2, "0"}}
	if _, err := validate(valid, expense); err != nil {
		t.Fatal(err)
	}
	if _, err := checkedAdd(math.MaxInt64, 1); err == nil {
		t.Fatal("positive overflow accepted")
	}
	if _, err := checkedAdd(math.MinInt64, -1); err == nil {
		t.Fatal("negative overflow accepted")
	}
	if _, err := validate(writeInput{ExpenseInput: ExpenseInput{Currency: "JPY", Amount: "1", Date: "2026-11-01"}, FromID: 1, ToID: 1}, settlement); err == nil {
		t.Fatal("self payment accepted")
	}
}

func TestSuggestionsAndPermissions(t *testing.T) {
	got, err := suggest([]Balance{{3, "5"}, {1, "-8"}, {2, "3"}})
	if err != nil || !reflect.DeepEqual(got, []Transfer{{1, 2, "3"}, {1, 3, "5"}}) {
		t.Fatalf("%+v %v", got, err)
	}
	got, err = suggest([]Balance{{1, "-9223372036854775808"}, {2, "9223372036854775807"}, {3, "1"}})
	if err != nil || len(got) != 2 || got[1].Amount != "1" {
		t.Fatalf("minimum balance: %+v %v", got, err)
	}
	for _, role := range []string{"owner", "editor", "member", "viewer", ""} {
		if mayWrite(role, 1, 1) != (role == "owner" || role == "editor" || role == "member") {
			t.Fatal(role)
		}
		if mayWrite(role, 1, 2) != (role == "owner" || role == "editor") {
			t.Fatal(role)
		}
	}
	if len(currencies) != 17 {
		t.Fatal("currency metadata changed without updating its contract")
	}
	for _, c := range currencies {
		if (c.Code == "JPY" || c.Code == "VND" || c.Code == "KRW") != (c.Decimals == 0) {
			t.Fatal(c)
		}
	}
}

type allow struct{}

func (allow) RequireParticipant(context.Context, int64, int64) error { return nil }

type capture struct {
	Store
	hash  []byte
	calls int
}

func (c *capture) Save(_ context.Context, _ int64, _ int64, _ kind, _ int64, _ writeInput, _ string, hash []byte) (Record, bool, error) {
	c.hash = append([]byte(nil), hash...)
	c.calls++
	return Record{}, true, nil
}
func TestCanonicalCreationAndVersion(t *testing.T) {
	store := &capture{}
	s := NewService(store, allow{})
	in := writeInput{ExpenseInput: ExpenseInput{Description: " Meal ", Currency: "PHP", Amount: "101", Date: "2026-01-01", PayerID: 1, SplitMode: "equal", Shares: []Share{{UserID: 2}, {UserID: 1}}}}
	if _, _, err := s.save(context.Background(), 1, 1, expense, 0, in, ""); err == nil {
		t.Fatal("missing key accepted")
	}
	if _, _, err := s.save(context.Background(), 1, 1, expense, 1, in, ""); err == nil {
		t.Fatal("missing version accepted")
	}
	if _, _, err := s.save(context.Background(), 1, 1, expense, 0, in, "key"); err != nil {
		t.Fatal(err)
	}
	hash := append([]byte(nil), store.hash...)
	in.Description = "Meal"
	in.Shares = []Share{{UserID: 1}, {UserID: 2}}
	if _, _, err := s.save(context.Background(), 1, 1, expense, 0, in, "key"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(hash, store.hash) {
		t.Fatal("equivalent inputs produced different hashes")
	}
	if store.calls != 2 {
		t.Fatal("invalid input reached persistence")
	}
}
