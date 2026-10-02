package places

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
	_ "time/tzdata"

	"travel-planner/travel-planner-api/internal/apperror"
)

type Store interface {
	Get(context.Context, int64) (Place, error)
	Upsert(context.Context, Candidate) (Place, error)
	CachedSearch(context.Context, string) ([]Candidate, bool, error)
	CacheSearch(context.Context, string, []Candidate) error
}

type userWindow struct {
	start time.Time
	count int
}

type Service struct {
	store    Store
	provider Provider
	zone     func(float64, float64) string
	mu       sync.Mutex
	// ponytail: process-local user counters suit one demo instance; move to shared storage when scaling.
	users map[int64]userWindow
}

func NewService(store Store, provider Provider, zone func(float64, float64) string) *Service {
	return &Service{store: store, provider: provider, zone: zone, users: make(map[int64]userWindow)}
}

func (s *Service) Search(ctx context.Context, userID int64, query string) ([]Candidate, error) {
	query = strings.TrimSpace(query)
	if len(query) < 2 || len(query) > 200 {
		return nil, fmt.Errorf("%w: q must be 2–200 bytes", apperror.ErrInvalid)
	}
	s.mu.Lock()
	window := s.users[userID]
	if time.Since(window.start) >= time.Minute {
		window = userWindow{start: time.Now()}
	}
	if window.count >= 10 {
		s.mu.Unlock()
		return nil, apperror.ErrRateLimited
	}
	window.count++
	s.users[userID] = window
	s.mu.Unlock()
	key := strings.ToLower(query)
	if cached, found, err := s.store.CachedSearch(ctx, key); err != nil {
		return nil, err
	} else if found {
		return cached, nil
	}
	results, err := s.provider.Search(ctx, query)
	if err != nil {
		return nil, err
	}
	for i := range results {
		if err := s.complete(&results[i]); err != nil {
			return nil, err
		}
	}
	if err := s.store.CacheSearch(ctx, key, results); err != nil {
		return nil, err
	}
	return results, nil
}

func (s *Service) complete(c *Candidate) error {
	if !referencePattern.MatchString(c.ProviderPlaceID) || !validCoordinates(c.Latitude, c.Longitude) ||
		strings.TrimSpace(c.Name) == "" || strings.TrimSpace(c.Address) == "" || len(c.Name) > 300 || len(c.Address) > 2000 {
		return apperror.ErrUnavailable
	}
	c.TimeZone = s.zone(c.Longitude, c.Latitude)
	if c.TimeZone == "" || c.TimeZone == "Local" {
		return apperror.ErrUnavailable
	}
	if _, err := time.LoadLocation(c.TimeZone); err != nil {
		return apperror.ErrUnavailable
	}
	return nil
}

func (s *Service) Resolve(ctx context.Context, ref string) (Place, error) {
	if !referencePattern.MatchString(ref) {
		return Place{}, fmt.Errorf("%w: provider_place_id must be an OSM object reference", apperror.ErrInvalid)
	}
	c, err := s.provider.Lookup(ctx, ref)
	if err != nil {
		return Place{}, err
	}
	if err := s.complete(&c); err != nil {
		return Place{}, err
	}
	return s.store.Upsert(ctx, c)
}

func (s *Service) Get(ctx context.Context, id int64) (Place, error) {
	return s.store.Get(ctx, id)
}
