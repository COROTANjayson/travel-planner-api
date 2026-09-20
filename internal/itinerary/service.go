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
	Create(context.Context, int64, Input) (Activity, error)
	List(context.Context, int64, int, int) ([]Activity, error)
	Get(context.Context, int64, int64) (Activity, error)
	Update(context.Context, int64, int64, Input) (Activity, error)
	Delete(context.Context, int64, int64) error
}
type Service struct{ store Store }

func NewService(store Store) *Service { return &Service{store: store} }

func validate(in Input) (Input, error) {
	in.Title = strings.TrimSpace(in.Title)
	in.TimeZone = strings.TrimSpace(in.TimeZone)
	if in.Title == "" || len(in.Title) > 200 || len(in.Notes) > 10000 {
		return in, fmt.Errorf("%w: title (up to 200 bytes) is required and notes must be at most 10000 bytes", apperror.ErrInvalid)
	}
	if in.StartsAt.IsZero() || in.EndsAt.IsZero() || !in.EndsAt.After(in.StartsAt) ||
		in.StartsAt.UTC().Year() < 1 || in.StartsAt.UTC().Year() > 9999 ||
		in.EndsAt.UTC().Year() < 1 || in.EndsAt.UTC().Year() > 9999 {
		return in, fmt.Errorf("%w: starts_at and ends_at must be RFC3339 timestamps with ends_at after starts_at", apperror.ErrInvalid)
	}
	if in.TimeZone == "" || in.TimeZone == "Local" {
		return in, fmt.Errorf("%w: time_zone must be an explicit IANA time zone", apperror.ErrInvalid)
	}
	if _, err := time.LoadLocation(in.TimeZone); err != nil {
		return in, fmt.Errorf("%w: unknown time_zone", apperror.ErrInvalid)
	}
	in.StartsAt, in.EndsAt = in.StartsAt.UTC(), in.EndsAt.UTC()
	return in, nil
}
func (s *Service) Create(ctx context.Context, tripID int64, in Input) (Activity, error) {
	in, err := validate(in)
	if err != nil {
		return Activity{}, err
	}
	return s.store.Create(ctx, tripID, in)
}
func (s *Service) List(ctx context.Context, tripID int64, limit, offset int) ([]Activity, error) {
	return s.store.List(ctx, tripID, limit, offset)
}
func (s *Service) Get(ctx context.Context, tripID, id int64) (Activity, error) {
	return s.store.Get(ctx, tripID, id)
}
func (s *Service) Update(ctx context.Context, tripID, id int64, in Input) (Activity, error) {
	in, err := validate(in)
	if err != nil {
		return Activity{}, err
	}
	return s.store.Update(ctx, tripID, id, in)
}
func (s *Service) Delete(ctx context.Context, tripID, id int64) error {
	return s.store.Delete(ctx, tripID, id)
}
