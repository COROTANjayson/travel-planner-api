package trips

import (
	"context"
	"fmt"
	"strings"
	"time"
	_ "time/tzdata"

	"travel-planner/travel-planner-api/internal/apperror"
)

type Store interface {
	Destination(context.Context, int64) (Destination, error)
	Create(context.Context, int64, Input) (Trip, error)
	List(context.Context, int64, int, int) ([]Trip, error)
	Get(context.Context, int64) (Trip, error)
	Update(context.Context, int64, Input) (Trip, error)
	Delete(context.Context, int64) error
}

type Authorizer interface {
	RequireParticipant(context.Context, int64, int64) error
	RequireEditor(context.Context, int64, int64) error
	RequireOwner(context.Context, int64, int64) error
}

type Service struct {
	store Store
	authz Authorizer
}

func NewService(store Store, authz Authorizer) *Service { return &Service{store: store, authz: authz} }

func (s *Service) validate(ctx context.Context, in Input) (Input, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 200 || in.DestinationPlaceID == nil || *in.DestinationPlaceID <= 0 {
		return in, fmt.Errorf("%w: name and destination_place_id are required", apperror.ErrInvalid)
	}
	if (in.StartsAt == nil) != (in.EndsAt == nil) {
		return in, fmt.Errorf("%w: provide both exact times or leave both unset", apperror.ErrInvalid)
	}
	if in.StartsAt != nil && (in.StartsAt.IsZero() || !in.EndsAt.After(*in.StartsAt) ||
		in.StartsAt.UTC().Year() < 1 || in.EndsAt.UTC().Year() > 9999) {
		return in, fmt.Errorf("%w: starts_at and ends_at must be ordered RFC3339 timestamps", apperror.ErrInvalid)
	}
	place, err := s.store.Destination(ctx, *in.DestinationPlaceID)
	if err != nil {
		return in, err
	}
	if (place.Kind != "city" && place.Kind != "country") || place.Name == "" || len(place.Name) > 300 {
		return in, fmt.Errorf("%w: destination must be a city or country", apperror.ErrInvalid)
	}
	zone, err := time.LoadLocation(place.TimeZone)
	if err != nil || place.TimeZone == "Local" {
		return in, fmt.Errorf("%w: invalid destination time zone", apperror.ErrInvalid)
	}
	in.Destination, in.TimeZone = place.Name, place.TimeZone
	if in.StartsAt != nil {
		start, end := in.StartsAt.UTC(), in.EndsAt.UTC()
		startDate, endDate := start.In(zone).Format(time.DateOnly), end.In(zone).Format(time.DateOnly)
		if (in.StartDate != "" && in.StartDate != startDate) || (in.EndDate != "" && in.EndDate != endDate) {
			return in, fmt.Errorf("%w: exact times must match the selected trip dates", apperror.ErrInvalid)
		}
		in.StartsAt, in.EndsAt = &start, &end
		in.StartDate, in.EndDate = startDate, endDate
	}
	startDate, startErr := time.Parse(time.DateOnly, in.StartDate)
	endDate, endErr := time.Parse(time.DateOnly, in.EndDate)
	if startErr != nil || endErr != nil || startDate.Year() < 1 || endDate.Year() > 9999 || startDate.After(endDate) {
		return in, fmt.Errorf("%w: invalid trip dates", apperror.ErrInvalid)
	}
	return in, nil
}

func (s *Service) Create(ctx context.Context, ownerUserID int64, in Input) (Trip, error) {
	in, err := s.validate(ctx, in)
	if err != nil {
		return Trip{}, err
	}
	return s.store.Create(ctx, ownerUserID, in)
}
func (s *Service) List(ctx context.Context, ownerUserID int64, limit, offset int) ([]Trip, error) {
	return s.store.List(ctx, ownerUserID, limit, offset)
}
func (s *Service) Get(ctx context.Context, ownerUserID, id int64) (Trip, error) {
	if err := s.authz.RequireParticipant(ctx, ownerUserID, id); err != nil {
		return Trip{}, err
	}
	return s.store.Get(ctx, id)
}
func (s *Service) Update(ctx context.Context, ownerUserID, id int64, in Input) (Trip, error) {
	if err := s.authz.RequireEditor(ctx, ownerUserID, id); err != nil {
		return Trip{}, err
	}
	in, err := s.validate(ctx, in)
	if err != nil {
		return Trip{}, err
	}
	return s.store.Update(ctx, id, in)
}
func (s *Service) Delete(ctx context.Context, ownerUserID, id int64) error {
	if err := s.authz.RequireOwner(ctx, ownerUserID, id); err != nil {
		return err
	}
	return s.store.Delete(ctx, id)
}
