package places

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tzf "github.com/ringsaturn/tzf/v2"
	"travel-planner/travel-planner-api/internal/apperror"
)

const sample = `[{"osm_type":"node","osm_id":123,"display_name":"Manila City Hall, Manila, Philippines","lat":"14.5946","lon":"120.9780","namedetails":{"name":"Manila City Hall"}}]`

func TestNominatimResponses(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		pause      time.Duration
		want       error
	}{
		{name: "success", body: sample},
		{name: "empty", body: `[]`},
		{name: "quota", status: 429, want: apperror.ErrUnavailable},
		{name: "malformed", body: `[{"osm_type":"node","osm_id":123}]`, want: apperror.ErrUnavailable},
		{name: "invalid coordinate", body: strings.Replace(sample, `"14.5946"`, `"91"`, 1), want: apperror.ErrUnavailable},
		{name: "timeout", pause: 30 * time.Millisecond, want: apperror.ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("User-Agent") != userAgent || r.URL.Path != "/search" || r.URL.Query().Get("limit") != "10" {
					t.Errorf("unexpected provider request: %s", r.URL)
				}
				time.Sleep(tc.pause)
				if tc.status != 0 {
					w.WriteHeader(tc.status)
				}
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			results, err := NewNominatim(server.URL, &http.Client{Timeout: 10 * time.Millisecond}).Search(context.Background(), "Manila")
			if !errors.Is(err, tc.want) {
				if tc.want != nil || err != nil {
					t.Fatalf("error = %v, want %v", err, tc.want)
				}
			}
			if tc.name == "success" && (len(results) != 1 || results[0].ProviderPlaceID != "N123" || results[0].Name != "Manila City Hall") {
				t.Fatalf("normalized results: %+v", results)
			}
			if tc.name == "empty" && (results == nil || len(results) != 0) {
				t.Fatalf("empty results: %+v", results)
			}
		})
	}
}

type memoryStore struct {
	places  map[string]Place
	cache   map[string][]Candidate
	upserts int
}

func (m *memoryStore) Get(_ context.Context, id int64) (Place, error) {
	for _, place := range m.places {
		if place.ID == id {
			return place, nil
		}
	}
	return Place{}, apperror.ErrNotFound
}
func (m *memoryStore) Upsert(_ context.Context, candidate Candidate) (Place, error) {
	m.upserts++
	place := m.places[candidate.ProviderPlaceID]
	if place.ID == 0 {
		place.ID = int64(len(m.places) + 1)
	}
	place.Provider, place.Candidate, place.RefreshedAt = "osm", candidate, time.Now()
	m.places[candidate.ProviderPlaceID] = place
	return place, nil
}
func (m *memoryStore) CachedSearch(_ context.Context, key string) ([]Candidate, bool, error) {
	items, ok := m.cache[key]
	return items, ok, nil
}
func (m *memoryStore) CacheSearch(_ context.Context, key string, items []Candidate) error {
	m.cache[key] = items
	return nil
}

type fakeProvider struct {
	searches, lookups int
	candidate         Candidate
}

func (f *fakeProvider) Search(context.Context, string) ([]Candidate, error) {
	f.searches++
	return []Candidate{f.candidate}, nil
}
func (f *fakeProvider) Lookup(context.Context, string) (Candidate, error) {
	f.lookups++
	return f.candidate, nil
}

func TestSearchCacheResolveAndLimits(t *testing.T) {
	finder, err := tzf.NewEmbeddedFinder()
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryStore{places: map[string]Place{}, cache: map[string][]Candidate{}}
	provider := &fakeProvider{candidate: Candidate{ProviderPlaceID: "N123", Name: "City Hall", Address: "Manila", Latitude: 14.5946, Longitude: 120.978}}
	service := NewService(store, provider, finder.GetTimezoneName)
	for i := 0; i < 10; i++ {
		query := "Manila"
		if i%2 == 1 {
			query = " manila "
		}
		items, err := service.Search(context.Background(), 7, query)
		if err != nil || len(items) != 1 || items[0].TimeZone != "Asia/Manila" {
			t.Fatalf("search: %+v, %v", items, err)
		}
	}
	if provider.searches != 1 {
		t.Fatalf("cache missed: %d provider calls", provider.searches)
	}
	if _, err := service.Search(context.Background(), 7, "Manila"); !errors.Is(err, apperror.ErrRateLimited) {
		t.Fatalf("user limit: %v", err)
	}
	for i := 0; i < 2; i++ {
		place, err := service.Resolve(context.Background(), "N123")
		if err != nil || place.ID != 1 || place.TimeZone != "Asia/Manila" {
			t.Fatalf("resolve: %+v, %v", place, err)
		}
	}
	if provider.lookups != 2 || store.upserts != 2 || len(store.places) != 1 {
		t.Fatal("resolve did not refresh and deduplicate")
	}
	if _, err := service.Resolve(context.Background(), "https://example.com"); !errors.Is(err, apperror.ErrInvalid) {
		t.Fatalf("invalid reference: %v", err)
	}
	provider.candidate.Latitude = 100
	if _, err := service.Resolve(context.Background(), "N123"); !errors.Is(err, apperror.ErrUnavailable) {
		t.Fatalf("invalid coordinates: %v", err)
	}
	provider.candidate.Latitude = 14.5946
	badZone := NewService(store, provider, func(float64, float64) string { return "Unknown/Zone" })
	if _, err := badZone.Resolve(context.Background(), "N123"); !errors.Is(err, apperror.ErrUnavailable) {
		t.Fatalf("invalid time zone: %v", err)
	}
}
