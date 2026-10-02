package places

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"travel-planner/travel-planner-api/internal/apperror"
)

const userAgent = "TravelPlannerPortfolio/1.0 (place search)"

var referencePattern = regexp.MustCompile(`^[NWR][1-9][0-9]*$`)

type Provider interface {
	Search(context.Context, string) ([]Candidate, error)
	Lookup(context.Context, string) (Candidate, error)
}

type Nominatim struct {
	endpoint string
	client   *http.Client
	mu       sync.Mutex
	next     time.Time
}

func NewNominatim(endpoint string, client *http.Client) *Nominatim {
	return &Nominatim{endpoint: strings.TrimRight(endpoint, "/"), client: client}
}

type nominatimResult struct {
	OSMType     string `json:"osm_type"`
	OSMID       int64  `json:"osm_id"`
	DisplayName string `json:"display_name"`
	Lat         string `json:"lat"`
	Lon         string `json:"lon"`
	NameDetails struct {
		Name string `json:"name"`
	} `json:"namedetails"`
}

func (n *Nominatim) fetch(ctx context.Context, path string, values url.Values) ([]Candidate, error) {
	// ponytail: one process owns the public provider limit; use a shared limiter before multiple API replicas.
	n.mu.Lock()
	wait := time.Until(n.next)
	if wait > 2*time.Second {
		n.mu.Unlock()
		return nil, apperror.ErrRateLimited
	}
	if wait < 0 {
		wait = 0
	}
	n.next = time.Now().Add(wait + time.Second)
	n.mu.Unlock()
	if wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return nil, apperror.ErrUnavailable
		}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, n.endpoint+path+"?"+values.Encode(), nil)
	if err != nil {
		return nil, apperror.ErrUnavailable
	}
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("Accept", "application/json")
	response, err := n.client.Do(request)
	if err != nil {
		return nil, apperror.ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, apperror.ErrUnavailable
	}
	var raw []nominatimResult
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20))
	if err := decoder.Decode(&raw); err != nil || raw == nil {
		return nil, apperror.ErrUnavailable
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, apperror.ErrUnavailable
	}
	results := make([]Candidate, 0, len(raw))
	for _, item := range raw {
		kind := strings.ToUpper(item.OSMType)
		if kind != "NODE" && kind != "WAY" && kind != "RELATION" || item.OSMID < 1 {
			continue // Nominatim also returns postcodes without stable OSM object IDs.
		}
		lat, latErr := strconv.ParseFloat(item.Lat, 64)
		lon, lonErr := strconv.ParseFloat(item.Lon, 64)
		name := strings.TrimSpace(item.NameDetails.Name)
		address := strings.TrimSpace(item.DisplayName)
		if name == "" {
			name = strings.TrimSpace(strings.SplitN(address, ",", 2)[0])
		}
		if latErr != nil || lonErr != nil || !validCoordinates(lat, lon) || name == "" || address == "" || len(name) > 300 || len(address) > 2000 {
			return nil, apperror.ErrUnavailable
		}
		results = append(results, Candidate{ProviderPlaceID: kind[:1] + strconv.FormatInt(item.OSMID, 10), Name: name, Address: address, Latitude: lat, Longitude: lon})
	}
	return results, nil
}

func validCoordinates(lat, lon float64) bool {
	return !math.IsNaN(lat) && !math.IsNaN(lon) && lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180
}

func (n *Nominatim) Search(ctx context.Context, query string) ([]Candidate, error) {
	return n.fetch(ctx, "/search", url.Values{"q": {query}, "format": {"jsonv2"}, "limit": {"10"}, "namedetails": {"1"}})
}

func (n *Nominatim) Lookup(ctx context.Context, ref string) (Candidate, error) {
	if !referencePattern.MatchString(ref) {
		return Candidate{}, apperror.ErrInvalid
	}
	items, err := n.fetch(ctx, "/lookup", url.Values{"osm_ids": {ref}, "format": {"jsonv2"}, "namedetails": {"1"}})
	if err != nil {
		return Candidate{}, err
	}
	if len(items) == 0 {
		return Candidate{}, apperror.ErrNotFound
	}
	if len(items) != 1 || items[0].ProviderPlaceID != ref {
		return Candidate{}, apperror.ErrUnavailable
	}
	return items[0], nil
}
