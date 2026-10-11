package trips

import (
	"context"
	"errors"
	"testing"
	"time"
	"travel-planner/travel-planner-api/internal/apperror"
)

type recordingStore struct {
	Store
	called      bool
	ownerUserID int64
	input       Input
	kind        string
}

type allowAuthorizer struct{}

func (allowAuthorizer) RequireParticipant(context.Context, int64, int64) error { return nil }
func (allowAuthorizer) RequireEditor(context.Context, int64, int64) error      { return nil }
func (allowAuthorizer) RequireOwner(context.Context, int64, int64) error       { return nil }

func (r *recordingStore) Create(_ context.Context, ownerUserID int64, in Input) (Trip, error) {
	r.called, r.ownerUserID, r.input = true, ownerUserID, in
	return Trip{ID: 1, Input: in}, nil
}
func (r *recordingStore) Destination(context.Context, int64) (Destination, error) {
	kind := r.kind
	if kind == "" {
		kind = "city"
	}
	return Destination{Name: "Cebu", Kind: kind, TimeZone: "Asia/Manila"}, nil
}

func TestCreateValidation(t *testing.T) {
	id := int64(1)
	start, _ := time.Parse(time.RFC3339, "2026-10-01T09:00:00+08:00")
	end, _ := time.Parse(time.RFC3339, "2026-10-03T18:00:00+08:00")
	valid := Input{Name: "  Cebu trip  ", DestinationPlaceID: &id, StartsAt: &start, EndsAt: &end}
	for _, tc := range []struct {
		name   string
		change func(*Input)
	}{
		{"empty name", func(in *Input) { in.Name = " " }},
		{"missing destination", func(in *Input) { in.DestinationPlaceID = nil }},
		{"missing time", func(in *Input) { in.StartsAt = nil }},
		{"reversed times", func(in *Input) { in.StartsAt, in.EndsAt = in.EndsAt, in.StartsAt }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := valid
			tc.change(&in)
			repo := &recordingStore{}
			_, err := NewService(repo, allowAuthorizer{}).Create(context.Background(), 42, in)
			if !errors.Is(err, apperror.ErrInvalid) || repo.called {
				t.Fatalf("invalid input reached repository: %v", err)
			}
		})
	}
	repo := &recordingStore{}
	_, err := NewService(repo, allowAuthorizer{}).Create(context.Background(), 42, valid)
	if err != nil || !repo.called || repo.ownerUserID != 42 || repo.input.Name != "Cebu trip" || repo.input.Destination != "Cebu" || repo.input.TimeZone != "Asia/Manila" {
		t.Fatalf("valid trip failed: %v", err)
	}
	oneDay := start.Add(time.Hour)
	valid.EndsAt = &oneDay
	if _, err := NewService(repo, allowAuthorizer{}).validate(context.Background(), valid); err != nil {
		t.Fatalf("same-day trip rejected: %v", err)
	}
	repo.kind = "other"
	if _, err := NewService(repo, allowAuthorizer{}).validate(context.Background(), valid); !errors.Is(err, apperror.ErrInvalid) {
		t.Fatalf("non-destination place accepted: %v", err)
	}
}

func TestDateOnlyTrips(t *testing.T) {
	id := int64(1)
	s := NewService(&recordingStore{}, allowAuthorizer{})
	for _, tc := range []struct {
		start, end string
		valid      bool
	}{
		{"2026-10-01", "2026-10-03", true},
		{"2026-10-01", "2026-10-01", true},
		{"2026-10-03", "2026-10-01", false},
		{"2026-02-30", "2026-03-01", false},
		{"", "2026-10-03", false},
		{"0000-01-01", "2026-10-03", false},
	} {
		in := Input{Name: "Dates only", DestinationPlaceID: &id, StartDate: tc.start, EndDate: tc.end}
		got, err := s.validate(context.Background(), in)
		if (err == nil) != tc.valid {
			t.Fatalf("%s/%s: %v", tc.start, tc.end, err)
		}
		if tc.valid && (got.StartsAt != nil || got.EndsAt != nil || got.StartDate != tc.start || got.EndDate != tc.end || got.TimeZone != "Asia/Manila") {
			t.Fatal("date-only trip changed")
		}
	}
	start, _ := time.Parse(time.RFC3339, "2026-10-01T01:00:00Z")
	end := start.Add(time.Hour)
	in := Input{Name: "Exact", DestinationPlaceID: &id, StartDate: "2026-10-02", EndDate: "2026-10-02", StartsAt: &start, EndsAt: &end}
	if _, err := s.validate(context.Background(), in); !errors.Is(err, apperror.ErrInvalid) {
		t.Fatal("mismatched dates accepted")
	}
}
