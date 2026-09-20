package itinerary

import (
	"context"
	"errors"
	"testing"
	"time"
	"travel-planner/travel-planner-api/internal/apperror"
)

type recordingStore struct {
	Store
	called bool
	input  Input
}

func (r *recordingStore) Create(_ context.Context, tripID int64, in Input) (Activity, error) {
	r.called, r.input = true, in
	return Activity{ID: 1, TripID: tripID, Input: in}, nil
}

func TestActivityValidationAndUTC(t *testing.T) {
	start, _ := time.Parse(time.RFC3339, "2026-10-01T09:00:00+08:00")
	valid := Input{Title: "  Breakfast  ", StartsAt: start, EndsAt: start.Add(time.Hour), TimeZone: "Asia/Manila"}
	for _, tc := range []struct {
		name   string
		change func(*Input)
	}{
		{"empty title", func(in *Input) { in.Title = " " }},
		{"missing start", func(in *Input) { in.StartsAt = time.Time{} }},
		{"equal times", func(in *Input) { in.EndsAt = in.StartsAt }},
		{"reversed times", func(in *Input) { in.EndsAt = in.StartsAt.Add(-time.Hour) }},
		{"invalid zone", func(in *Input) { in.TimeZone = "Unknown/Zone" }},
		{"machine zone", func(in *Input) { in.TimeZone = "Local" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := valid
			tc.change(&in)
			repo := &recordingStore{}
			_, err := NewService(repo).Create(context.Background(), 1, in)
			if !errors.Is(err, apperror.ErrInvalid) || repo.called {
				t.Fatalf("invalid activity reached repository: %v", err)
			}
		})
	}
	repo := &recordingStore{}
	_, err := NewService(repo).Create(context.Background(), 1, valid)
	if err != nil || !repo.called {
		t.Fatalf("valid activity failed: %v", err)
	}
	if repo.input.StartsAt.Location() != time.UTC || repo.input.StartsAt.Hour() != 1 || repo.input.Title != "Breakfast" {
		t.Fatalf("activity not normalized: %+v", repo.input)
	}
	if repo.input.TimeZone != "Asia/Manila" {
		t.Fatal("presentation time zone lost")
	}
}
