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
	TripDates(context.Context, int64) (time.Time, time.Time, error)
	Conflicts(context.Context, int64, int, int) ([]Conflict, error)
	List(context.Context, int64, int, int) ([]Activity, error)
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
	if in.StartsAt.IsZero() || in.EndsAt.IsZero() || !in.EndsAt.After(in.StartsAt) ||
		in.StartsAt.UTC().Year() < 1 || in.StartsAt.UTC().Year() > 9999 ||
		in.EndsAt.UTC().Year() < 1 || in.EndsAt.UTC().Year() > 9999 {
		return in, fmt.Errorf("%w: starts_at and ends_at must be RFC3339 timestamps with ends_at after starts_at", apperror.ErrInvalid)
	}
	if in.TimeZone == "" || in.TimeZone == "Local" {
		return in, fmt.Errorf("%w: time_zone must be an explicit IANA time zone", apperror.ErrInvalid)
	}
	in.StartsAt, in.EndsAt = in.StartsAt.UTC(), in.EndsAt.UTC()
	return in, nil
}
func (s *Service) validateForTrip(ctx context.Context, tripID int64, in Input) (Input, error) {
	in, err := validate(in)
	if err != nil {
		return in, err
	}
	zone, err := time.LoadLocation(in.TimeZone)
	if err != nil {
		return in, fmt.Errorf("%w: unknown time_zone", apperror.ErrInvalid)
	}
	startDate, endDate, err := s.store.TripDates(ctx, tripID)
	if err != nil {
		return in, err
	}
	for _, instant := range []time.Time{in.StartsAt, in.EndsAt} {
		year, month, day := instant.In(zone).Date()
		date := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
		if date.Before(startDate) || date.After(endDate) {
			return in, fmt.Errorf("%w: activity local dates must fall within trip dates", apperror.ErrInvalid)
		}
	}
	return in, nil
}

func (s *Service) Create(ctx context.Context, userID, tripID int64, in Input) (Activity, error) {
	if err := s.authz.RequireEditor(ctx, userID, tripID); err != nil {
		return Activity{}, err
	}
	in, err := s.validateForTrip(ctx, tripID, in)
	if err != nil {
		return Activity{}, err
	}
	return s.store.Create(ctx, userID, tripID, in)
}
func (s *Service) List(ctx context.Context, userID, tripID int64, limit, offset int) ([]Activity, error) {
	if err := s.authz.RequireParticipant(ctx, userID, tripID); err != nil {
		return nil, err
	}
	return s.store.List(ctx, tripID, limit, offset)
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
