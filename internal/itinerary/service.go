package itinerary

import (
	"context"
	"fmt"
	"strings"
	"time"
	_ "time/tzdata"

	"travel-planner/travel-planner-api/internal/apperror"
)

type Store interface {
	Create(context.Context, int64, int64, Input) (Activity, error)
	TripWindow(context.Context, int64) (TripWindow, error)
	Overlaps(context.Context, int64, time.Time, time.Time, int64) ([]Activity, error)
	Conflicts(context.Context, int64, int, int) ([]Conflict, error)
	List(context.Context, int64, int, int, *time.Time, *time.Time) ([]Activity, error)
	Get(context.Context, int64, int64) (Activity, error)
	Update(context.Context, int64, int64, Input) (Activity, error)
	Delete(context.Context, int64, int64) error
}
type Authorizer interface {
	RequireParticipant(context.Context, int64, int64) error
	RequireEditor(context.Context, int64, int64) error
}
type Service struct {
	store Store
	authz Authorizer
}

func NewService(store Store, authz Authorizer) *Service { return &Service{store: store, authz: authz} }

func validate(in Input) (Input, error) {
	in.Title = strings.TrimSpace(in.Title)
	in.TimeZone = strings.TrimSpace(in.TimeZone)
	if in.Title == "" || len(in.Title) > 200 || len(in.Notes) > 10000 {
		return in, fmt.Errorf("%w: title (up to 200 bytes) is required and notes must be at most 10000 bytes", apperror.ErrInvalid)
	}
	if in.PlaceID != nil && *in.PlaceID <= 0 {
		return in, fmt.Errorf("%w: place_id must be a positive integer", apperror.ErrInvalid)
	}
	if in.Category != nil {
		category := strings.TrimSpace(*in.Category)
		if category == "" || len(category) > 100 {
			return in, fmt.Errorf("%w: category must contain 1 to 100 bytes", apperror.ErrInvalid)
		}
		in.Category = &category
	}
	if in.Checklist != nil {
		if len(*in.Checklist) > 50 {
			return in, fmt.Errorf("%w: checklist must contain at most 50 items", apperror.ErrInvalid)
		}
		items := make([]ChecklistItem, len(*in.Checklist))
		for index, item := range *in.Checklist {
			item.Text = strings.TrimSpace(item.Text)
			if item.Text == "" || len(item.Text) > 200 {
				return in, fmt.Errorf("%w: checklist item text must contain 1 to 200 bytes", apperror.ErrInvalid)
			}
			items[index] = item
		}
		in.Checklist = &items
	}
	if in.StartsAt.IsZero() || in.EndsAt.IsZero() || !in.EndsAt.After(in.StartsAt) ||
		in.StartsAt.UTC().Year() < 1 || in.StartsAt.UTC().Year() > 9999 ||
		in.EndsAt.UTC().Year() < 1 || in.EndsAt.UTC().Year() > 9999 {
		return in, fmt.Errorf("%w: starts_at and ends_at must be RFC3339 timestamps with ends_at after starts_at", apperror.ErrInvalid)
	}
	in.StartsAt, in.EndsAt = in.StartsAt.UTC(), in.EndsAt.UTC()
	return in, nil
}
func (s *Service) validateForTrip(ctx context.Context, tripID int64, in Input) (Input, error) {
	in, err := validate(in)
	if err != nil {
		return in, err
	}
	window, err := s.store.TripWindow(ctx, tripID)
	if err != nil {
		return in, err
	}
	in.TimeZone = window.TimeZone
	zone, err := time.LoadLocation(window.TimeZone)
	if err != nil {
		return in, fmt.Errorf("%w: unknown time_zone", apperror.ErrInvalid)
	}
	if window.StartsAt != nil {
		if in.StartsAt.Before(*window.StartsAt) || in.EndsAt.After(*window.EndsAt) {
			return in, fmt.Errorf("%w: activity times must fall within trip times", apperror.ErrInvalid)
		}
		return in, nil
	}
	for _, instant := range []time.Time{in.StartsAt, in.EndsAt} {
		year, month, day := instant.In(zone).Date()
		date := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
		if date.Before(window.StartDate) || date.After(window.EndDate) {
			return in, fmt.Errorf("%w: activity local dates must fall within trip dates", apperror.ErrInvalid)
		}
	}
	return in, nil
}

func (s *Service) Overlaps(ctx context.Context, userID, tripID int64, start, end time.Time, excludeID int64) ([]Activity, error) {
	if err := s.authz.RequireParticipant(ctx, userID, tripID); err != nil {
		return nil, err
	}
	if start.IsZero() || !end.After(start) || excludeID < 0 {
		return nil, fmt.Errorf("%w: invalid overlap interval", apperror.ErrInvalid)
	}
	return s.store.Overlaps(ctx, tripID, start.UTC(), end.UTC(), excludeID)
}

func (s *Service) Create(ctx context.Context, userID, tripID int64, in Input) (Activity, error) {
	if err := s.authz.RequireEditor(ctx, userID, tripID); err != nil {
		return Activity{}, err
	}
	in, err := s.validateForTrip(ctx, tripID, in)
	if err != nil {
		return Activity{}, err
	}
	if in.Category == nil {
		category := "Activity"
		in.Category = &category
	}
	if in.Checklist == nil {
		items := []ChecklistItem{}
		in.Checklist = &items
	}
	return s.store.Create(ctx, userID, tripID, in)
}
func (s *Service) List(ctx context.Context, userID, tripID int64, limit, offset int, from, before *time.Time) ([]Activity, error) {
	if err := s.authz.RequireParticipant(ctx, userID, tripID); err != nil {
		return nil, err
	}
	if (from == nil) != (before == nil) || (from != nil && !before.After(*from)) {
		return nil, fmt.Errorf("%w: start_from and start_before must be an ordered pair", apperror.ErrInvalid)
	}
	return s.store.List(ctx, tripID, limit, offset, from, before)
}
func (s *Service) Get(ctx context.Context, userID, tripID, id int64) (Activity, error) {
	if err := s.authz.RequireParticipant(ctx, userID, tripID); err != nil {
		return Activity{}, err
	}
	return s.store.Get(ctx, tripID, id)
}
func (s *Service) Update(ctx context.Context, userID, tripID, id int64, in Input) (Activity, error) {
	if err := s.authz.RequireEditor(ctx, userID, tripID); err != nil {
		return Activity{}, err
	}
	in, err := s.validateForTrip(ctx, tripID, in)
	if err != nil {
		return Activity{}, err
	}
	return s.store.Update(ctx, tripID, id, in)
}
func (s *Service) Delete(ctx context.Context, userID, tripID, id int64) error {
	if err := s.authz.RequireEditor(ctx, userID, tripID); err != nil {
		return err
	}
	return s.store.Delete(ctx, tripID, id)
}

func (s *Service) Conflicts(ctx context.Context, userID, tripID int64, limit, offset int) ([]Conflict, error) {
	if err := s.authz.RequireParticipant(ctx, userID, tripID); err != nil {
		return nil, err
	}
	return s.store.Conflicts(ctx, tripID, limit, offset)
}
