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
	Create(context.Context, Input) (Trip, error)
	List(context.Context, int, int) ([]Trip, error)
	Get(context.Context, int64) (Trip, error)
	Update(context.Context, int64, Input) (Trip, error)
	Delete(context.Context, int64) error
}

type Service struct{ store Store }

func NewService(store Store) *Service { return &Service{store: store} }

func validate(in Input) (Input, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Destination = strings.TrimSpace(in.Destination)
	in.TimeZone = strings.TrimSpace(in.TimeZone)
	if in.Name == "" || len(in.Name) > 200 || in.Destination == "" || len(in.Destination) > 300 {
		return in, fmt.Errorf("%w: name (up to 200 bytes) and destination (up to 300 bytes) are required", apperror.ErrInvalid)
	}
	start, e1 := time.Parse(time.DateOnly, in.StartDate)
	end, e2 := time.Parse(time.DateOnly, in.EndDate)
	if e1 != nil || e2 != nil || start.Year() < 1 || end.Year() < 1 || end.Before(start) {
		return in, fmt.Errorf("%w: dates must use YYYY-MM-DD and end_date must be on or after start_date", apperror.ErrInvalid)
	}
	if in.TimeZone == "" || in.TimeZone == "Local" {
		return in, fmt.Errorf("%w: time_zone must be an explicit IANA time zone", apperror.ErrInvalid)
	}
	if _, err := time.LoadLocation(in.TimeZone); err != nil {
		return in, fmt.Errorf("%w: unknown time_zone", apperror.ErrInvalid)
	}
	return in, nil
}

func (s *Service) Create(ctx context.Context, in Input) (Trip, error) {
	in, err := validate(in)
	if err != nil {
		return Trip{}, err
	}
	return s.store.Create(ctx, in)
}
func (s *Service) List(ctx context.Context, limit, offset int) ([]Trip, error) {
	return s.store.List(ctx, limit, offset)
}
func (s *Service) Get(ctx context.Context, id int64) (Trip, error) { return s.store.Get(ctx, id) }
func (s *Service) Update(ctx context.Context, id int64, in Input) (Trip, error) {
	in, err := validate(in)
	if err != nil {
		return Trip{}, err
	}
	return s.store.Update(ctx, id, in)
}
func (s *Service) Delete(ctx context.Context, id int64) error { return s.store.Delete(ctx, id) }
