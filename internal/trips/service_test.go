package trips

import (
	"context"
	"errors"
	"testing"
	"travel-planner/travel-planner-api/internal/apperror"
)

type recordingStore struct {
	Store
	called      bool
	ownerUserID int64
	input       Input
}

func (r *recordingStore) Create(_ context.Context, ownerUserID int64, in Input) (Trip, error) {
	r.called, r.ownerUserID, r.input = true, ownerUserID, in
	return Trip{ID: 1, Input: in}, nil
}

func TestCreateValidation(t *testing.T) {
	valid := Input{Name: "  Cebu trip  ", Destination: "Cebu", StartDate: "2026-10-01", EndDate: "2026-10-03", TimeZone: "Asia/Manila"}
	for _, tc := range []struct {
		name   string
		change func(*Input)
	}{
		{"empty name", func(in *Input) { in.Name = " " }},
		{"invalid date", func(in *Input) { in.StartDate = "2026-02-30" }},
		{"reversed dates", func(in *Input) { in.EndDate = "2026-09-30" }},
		{"invalid zone", func(in *Input) { in.TimeZone = "Unknown/Zone" }},
		{"machine zone", func(in *Input) { in.TimeZone = "Local" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := valid
			tc.change(&in)
			repo := &recordingStore{}
			_, err := NewService(repo).Create(context.Background(), 42, in)
			if !errors.Is(err, apperror.ErrInvalid) || repo.called {
				t.Fatalf("invalid input reached repository: %v", err)
			}
		})
	}
	repo := &recordingStore{}
	_, err := NewService(repo).Create(context.Background(), 42, valid)
	if err != nil || !repo.called || repo.ownerUserID != 42 || repo.input.Name != "Cebu trip" {
		t.Fatalf("valid trip failed: %v", err)
	}
	valid.EndDate = valid.StartDate
	if _, err := validate(valid); err != nil {
		t.Fatalf("one-day trip rejected: %v", err)
	}
}
