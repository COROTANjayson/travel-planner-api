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
	called             bool
	input              Input
	startDate, endDate time.Time
	datesErr           error
}

type allowAuthorizer struct{}

func (allowAuthorizer) RequireParticipant(context.Context, int64, int64) error { return nil }
func (allowAuthorizer) RequireEditor(context.Context, int64, int64) error      { return nil }

func (r *recordingStore) Create(_ context.Context, userID, tripID int64, in Input) (Activity, error) {
	r.called, r.input = true, in
	return Activity{ID: 1, TripID: tripID, CreatedByUserID: userID, Input: in}, nil
}

func (r *recordingStore) Update(_ context.Context, tripID, id int64, in Input) (Activity, error) {
	r.called, r.input = true, in
	return Activity{ID: id, TripID: tripID, Input: in}, nil
}

func (r *recordingStore) TripDates(context.Context, int64) (time.Time, time.Time, error) {
	if r.startDate.IsZero() {
		return time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC), r.datesErr
	}
	return r.startDate, r.endDate, r.datesErr
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
			_, err := NewService(repo, allowAuthorizer{}).Create(context.Background(), 7, 1, in)
			if !errors.Is(err, apperror.ErrInvalid) || repo.called {
				t.Fatalf("invalid activity reached repository: %v", err)
			}
		})
	}
	repo := &recordingStore{}
	_, err := NewService(repo, allowAuthorizer{}).Create(context.Background(), 7, 1, valid)
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

func TestActivityTripBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, dateStart, dateEnd, start, end, zone string
		valid                                      bool
	}{
		{"inclusive dates", "2026-10-01", "2026-10-03", "2026-10-01T00:00:00+08:00", "2026-10-03T23:59:59+08:00", "Asia/Manila", true},
		{"before trip", "2026-10-01", "2026-10-03", "2026-09-30T23:59:59+08:00", "2026-10-01T01:00:00+08:00", "Asia/Manila", false},
		{"after trip", "2026-10-01", "2026-10-03", "2026-10-03T23:00:00+08:00", "2026-10-04T00:00:00+08:00", "Asia/Manila", false},
		{"midnight crossing", "2026-10-01", "2026-10-03", "2026-10-01T23:30:00+08:00", "2026-10-02T00:30:00+08:00", "Asia/Manila", true},
		{"different activity zone", "2026-10-01", "2026-10-01", "2026-10-01T23:00:00-07:00", "2026-10-01T23:30:00-07:00", "America/Los_Angeles", true},
		{"saved zone overrides offset", "2026-10-01", "2026-10-01", "2026-09-30T17:00:00-07:00", "2026-09-30T18:00:00-07:00", "Asia/Manila", true},
		{"saved zone rejects input date", "2026-10-01", "2026-10-01", "2026-10-01T00:00:00+08:00", "2026-10-01T01:00:00+08:00", "America/Los_Angeles", false},
		{"spring forward", "2026-03-08", "2026-03-08", "2026-03-08T01:30:00-05:00", "2026-03-08T03:30:00-04:00", "America/New_York", true},
		{"fall back", "2026-11-01", "2026-11-01", "2026-11-01T01:30:00-04:00", "2026-11-01T01:30:00-05:00", "America/New_York", true},
		{"spring day end", "2026-03-08", "2026-03-08", "2026-03-08T23:30:00-04:00", "2026-03-09T00:00:00-04:00", "America/New_York", false},
		{"fall day end", "2026-11-01", "2026-11-01", "2026-11-01T23:00:00-05:00", "2026-11-01T23:30:00-05:00", "America/New_York", true},
	} {
		for _, operation := range []string{"create", "update"} {
			t.Run(tc.name+"/"+operation, func(t *testing.T) {
				start, _ := time.Parse(time.RFC3339, tc.start)
				end, _ := time.Parse(time.RFC3339, tc.end)
				dateStart, _ := time.Parse(time.DateOnly, tc.dateStart)
				dateEnd, _ := time.Parse(time.DateOnly, tc.dateEnd)
				repo := &recordingStore{startDate: dateStart, endDate: dateEnd}
				s := NewService(repo, allowAuthorizer{})
				in := Input{Title: "Activity", StartsAt: start, EndsAt: end, TimeZone: tc.zone}
				var err error
				if operation == "create" {
					var a Activity
					a, err = s.Create(context.Background(), 7, 1, in)
					if tc.valid && a.CreatedByUserID != 7 {
						t.Fatal("creator was not taken from caller")
					}
				} else {
					_, err = s.Update(context.Background(), 7, 1, 2, in)
				}
				if tc.valid {
					if err != nil || !repo.called {
						t.Fatalf("valid activity rejected: %v", err)
					}
				} else if !errors.Is(err, apperror.ErrInvalid) || repo.called {
					t.Fatalf("out-of-trip activity reached write: %v", err)
				}
			})
		}
	}
}

func TestTripDateLoadFailurePreventsWrites(t *testing.T) {
	for _, failure := range []error{apperror.ErrNotFound, errors.New("database failure")} {
		repo := &recordingStore{datesErr: failure}
		s := NewService(repo, allowAuthorizer{})
		start := time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC)
		in := Input{Title: "Activity", StartsAt: start, EndsAt: start.Add(time.Hour), TimeZone: "Asia/Manila"}
		if _, err := s.Create(context.Background(), 7, 1, in); !errors.Is(err, failure) || repo.called {
			t.Fatalf("create ignored trip load failure: %v", err)
		}
		if _, err := s.Update(context.Background(), 7, 1, 2, in); !errors.Is(err, failure) || repo.called {
			t.Fatalf("update ignored trip load failure: %v", err)
		}
	}
}
