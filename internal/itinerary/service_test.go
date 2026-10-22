package itinerary

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
	"travel-planner/travel-planner-api/internal/apperror"
)

type recordingStore struct {
	Store
	called               bool
	input                Input
	startDate, endDate   time.Time
	datesErr             error
	tripZone             string
	listFrom, listBefore *time.Time
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

func (r *recordingStore) List(_ context.Context, _ int64, _, _ int, from, before *time.Time) ([]Activity, error) {
	r.called, r.listFrom, r.listBefore = true, from, before
	return []Activity{}, nil
}

func TestListDateRange(t *testing.T) {
	store := &recordingStore{}
	service := NewService(store, allowAuthorizer{})
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	before := from.Add(24 * time.Hour)
	for _, bounds := range [][2]*time.Time{{&from, nil}, {&before, &from}, {&from, &from}} {
		store.called = false
		if _, err := service.List(context.Background(), 1, 2, 100, 0, bounds[0], bounds[1]); !errors.Is(err, apperror.ErrInvalid) || store.called {
			t.Fatalf("invalid range reached repository: %v", err)
		}
	}
	if _, err := service.List(context.Background(), 1, 2, 100, 0, &from, &before); err != nil || !store.called || !store.listFrom.Equal(from) || !store.listBefore.Equal(before) {
		t.Fatalf("valid range not forwarded: %v", err)
	}
}

func (r *recordingStore) TripDates(context.Context, int64) (time.Time, time.Time, error) {
	if r.startDate.IsZero() {
		return time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC), r.datesErr
	}
	return r.startDate, r.endDate, r.datesErr
}

func (r *recordingStore) TripWindow(ctx context.Context, id int64) (TripWindow, error) {
	start, end, err := r.TripDates(ctx, id)
	zone := r.tripZone
	if zone == "" {
		zone = "Asia/Manila"
	}
	return TripWindow{StartDate: start, EndDate: end, TimeZone: zone}, err
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
				repo := &recordingStore{startDate: dateStart, endDate: dateEnd, tripZone: tc.zone}
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

func TestCategoryAndChecklist(t *testing.T) {
	start := time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC)
	input := Input{Title: "Outing", StartsAt: start, EndsAt: start.Add(time.Hour)}
	store := &recordingStore{}
	service := NewService(store, allowAuthorizer{})
	created, err := service.Create(context.Background(), 1, 2, input)
	if err != nil || created.Category == nil || *created.Category != "Activity" || created.Checklist == nil || *created.Checklist == nil || len(*created.Checklist) != 0 {
		t.Fatalf("creation defaults = %+v, %v", created, err)
	}
	if _, err := service.Update(context.Background(), 1, 2, 3, input); err != nil || store.input.Category != nil || store.input.Checklist != nil {
		t.Fatalf("omitted additions must reach update unchanged: %+v, %v", store.input, err)
	}
	category := "  Shopping  "
	items := []ChecklistItem{{Text: "  Buy tickets  ", Done: true}, {Text: "Buy tickets"}}
	input.Category, input.Checklist = &category, &items
	if _, err := service.Update(context.Background(), 1, 2, 3, input); err != nil {
		t.Fatal(err)
	}
	if *store.input.Category != "Shopping" || !reflect.DeepEqual(*store.input.Checklist, []ChecklistItem{{Text: "Buy tickets", Done: true}, {Text: "Buy tickets"}}) {
		t.Fatalf("normalization lost order or completion: %+v", store.input)
	}
	if category != "  Shopping  " || items[0].Text != "  Buy tickets  " {
		t.Fatal("normalization mutated the caller's draft")
	}
	for _, tc := range []struct {
		name     string
		category string
		items    []ChecklistItem
		valid    bool
	}{
		{"blank category", " \t", nil, false},
		{"long category", strings.Repeat("x", 101), nil, false},
		{"category byte limit", strings.Repeat("é", 51), nil, false},
		{"blank item", "Activity", []ChecklistItem{{Text: " \n"}}, false},
		{"long item", "Activity", []ChecklistItem{{Text: strings.Repeat("x", 201)}}, false},
		{"item byte limit", "Activity", []ChecklistItem{{Text: strings.Repeat("é", 101)}}, false},
		{"too many items", "Activity", make([]ChecklistItem, 51), false},
		{"empty list", "Rest", []ChecklistItem{}, true},
		{"exact text limits", strings.Repeat("é", 50), []ChecklistItem{{Text: strings.Repeat("é", 100)}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := input
			in.Category, in.Checklist = &tc.category, &tc.items
			for _, create := range []bool{true, false} {
				store.called = false
				var err error
				if create {
					_, err = service.Create(context.Background(), 1, 2, in)
				} else {
					_, err = service.Update(context.Background(), 1, 2, 3, in)
				}
				if tc.valid && (err != nil || !store.called) {
					t.Fatalf("valid input rejected: %v", err)
				}
				if !tc.valid && (!errors.Is(err, apperror.ErrInvalid) || store.called) {
					t.Fatalf("invalid input reached persistence: %v", err)
				}
			}
		})
	}
	items = make([]ChecklistItem, 50)
	for index := range items {
		items[index].Text = "Step"
	}
	if _, err := service.Create(context.Background(), 1, 2, input); err != nil {
		t.Fatalf("50 items rejected: %v", err)
	}
}
