package places

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"travel-planner/travel-planner-api/internal/apperror"
)

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

const placeColumns = "id, provider, provider_place_id, name, address, latitude, longitude, time_zone, refreshed_at, created_at, updated_at"

func scan(row pgx.Row) (Place, error) {
	var p Place
	err := row.Scan(&p.ID, &p.Provider, &p.ProviderPlaceID, &p.Name, &p.Address, &p.Latitude, &p.Longitude, &p.TimeZone, &p.RefreshedAt, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, apperror.ErrNotFound
	}
	return p, err
}

func (r *Repository) Get(ctx context.Context, id int64) (Place, error) {
	return scan(r.pool.QueryRow(ctx, "SELECT "+placeColumns+" FROM places WHERE id=$1", id))
}

func (r *Repository) Upsert(ctx context.Context, c Candidate) (Place, error) {
	return scan(r.pool.QueryRow(ctx, `INSERT INTO places (provider, provider_place_id, name, address, latitude, longitude, time_zone, refreshed_at)
		VALUES ('osm',$1,$2,$3,$4,$5,$6,now())
		ON CONFLICT (provider,provider_place_id) DO UPDATE SET name=EXCLUDED.name,address=EXCLUDED.address,
		latitude=EXCLUDED.latitude,longitude=EXCLUDED.longitude,time_zone=EXCLUDED.time_zone,
		refreshed_at=now(),updated_at=now() RETURNING `+placeColumns,
		c.ProviderPlaceID, c.Name, c.Address, c.Latitude, c.Longitude, c.TimeZone))
}

func (r *Repository) CachedSearch(ctx context.Context, query string) ([]Candidate, bool, error) {
	var data []byte
	err := r.pool.QueryRow(ctx, "SELECT results FROM place_search_cache WHERE query=$1 AND expires_at>now()", query).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var results []Candidate
	if err := json.Unmarshal(data, &results); err != nil {
		return nil, false, err
	}
	return results, true, nil
}

func (r *Repository) CacheSearch(ctx context.Context, query string, results []Candidate) error {
	data, err := json.Marshal(results)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `INSERT INTO place_search_cache (query,results,expires_at) VALUES ($1,$2,$3)
		ON CONFLICT (query) DO UPDATE SET results=EXCLUDED.results,expires_at=EXCLUDED.expires_at`, query, data, time.Now().Add(24*time.Hour))
	return err
}
