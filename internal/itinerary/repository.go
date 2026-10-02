package itinerary

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"travel-planner/travel-planner-api/internal/apperror"
)

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

const columns = `i.id, i.trip_id, i.created_by_user_id, i.title, i.starts_at, i.ends_at, i.time_zone, i.notes,
	i.place_id, i.created_at, i.updated_at, CASE WHEN p.id IS NULL THEN NULL ELSE jsonb_build_object(
	'id',p.id,'provider',p.provider,'provider_place_id',p.provider_place_id,'name',p.name,'address',p.address,
	'latitude',p.latitude,'longitude',p.longitude,'time_zone',p.time_zone,'refreshed_at',p.refreshed_at,
	'created_at',p.created_at,'updated_at',p.updated_at) END`

func scan(row pgx.Row) (Activity, error) {
	var a Activity
	var placeJSON []byte
	err := row.Scan(&a.ID, &a.TripID, &a.CreatedByUserID, &a.Title, &a.StartsAt, &a.EndsAt, &a.TimeZone, &a.Notes, &a.PlaceID, &a.CreatedAt, &a.UpdatedAt, &placeJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, apperror.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		if pgErr.ConstraintName == "itinerary_items_place_id_fkey" {
			return a, apperror.ErrInvalid
		}
		return a, apperror.ErrNotFound
	}
	if err != nil {
		return a, err
	}
	if placeJSON != nil {
		if err := json.Unmarshal(placeJSON, &a.Place); err != nil {
			return a, err
		}
	}
	a.StartsAt, a.EndsAt = a.StartsAt.UTC(), a.EndsAt.UTC()
	a.CreatedAt, a.UpdatedAt = a.CreatedAt.UTC(), a.UpdatedAt.UTC()
	return a, err
}
func (r *Repository) Create(ctx context.Context, userID, tripID int64, in Input) (Activity, error) {
	return scan(r.pool.QueryRow(ctx, `WITH saved AS (INSERT INTO itinerary_items (created_by_user_id,trip_id,title,starts_at,ends_at,time_zone,notes,place_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING *) SELECT `+columns+` FROM saved i LEFT JOIN places p ON p.id=i.place_id`,
		userID, tripID, in.Title, in.StartsAt, in.EndsAt, in.TimeZone, in.Notes, in.PlaceID))
}

func (r *Repository) TripDates(ctx context.Context, tripID int64) (time.Time, time.Time, error) {
	var start, end time.Time
	err := r.pool.QueryRow(ctx, "SELECT start_date,end_date FROM trips WHERE id=$1", tripID).Scan(&start, &end)
	if errors.Is(err, pgx.ErrNoRows) {
		err = apperror.ErrNotFound
	}
	return start, end, err
}

func (r *Repository) Conflicts(ctx context.Context, tripID int64, limit, offset int) ([]Conflict, error) {
	rows, err := r.pool.Query(ctx, `SELECT a.id,b.id,
		GREATEST(a.starts_at,b.starts_at),LEAST(a.ends_at,b.ends_at)
		FROM itinerary_items a JOIN itinerary_items b ON a.trip_id=b.trip_id AND a.id<b.id
		WHERE a.trip_id=$1 AND a.starts_at<b.ends_at AND b.starts_at<a.ends_at
		ORDER BY GREATEST(a.starts_at,b.starts_at),a.id,b.id LIMIT $2 OFFSET $3`, tripID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Conflict, 0)
	for rows.Next() {
		var c Conflict
		if err := rows.Scan(&c.ActivityIDs[0], &c.ActivityIDs[1], &c.OverlapStartsAt, &c.OverlapEndsAt); err != nil {
			return nil, err
		}
		c.OverlapStartsAt, c.OverlapEndsAt = c.OverlapStartsAt.UTC(), c.OverlapEndsAt.UTC()
		result = append(result, c)
	}
	return result, rows.Err()
}
func (r *Repository) Get(ctx context.Context, tripID, id int64) (Activity, error) {
	return scan(r.pool.QueryRow(ctx, "SELECT "+columns+" FROM itinerary_items i LEFT JOIN places p ON p.id=i.place_id WHERE i.trip_id=$1 AND i.id=$2", tripID, id))
}
func (r *Repository) List(ctx context.Context, tripID int64, limit, offset int) ([]Activity, error) {
	var exists bool
	if err := r.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM trips WHERE id=$1)", tripID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, apperror.ErrNotFound
	}
	rows, err := r.pool.Query(ctx, "SELECT "+columns+" FROM itinerary_items i LEFT JOIN places p ON p.id=i.place_id WHERE i.trip_id=$1 ORDER BY i.starts_at,i.id LIMIT $2 OFFSET $3", tripID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Activity, 0)
	for rows.Next() {
		a, err := scan(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}
func (r *Repository) Update(ctx context.Context, tripID, id int64, in Input) (Activity, error) {
	return scan(r.pool.QueryRow(ctx, `WITH saved AS (UPDATE itinerary_items SET title=$3, starts_at=$4, ends_at=$5, time_zone=$6,
		notes=$7,place_id=$8,updated_at=now() WHERE trip_id=$1 AND id=$2 RETURNING *)
		SELECT `+columns+` FROM saved i LEFT JOIN places p ON p.id=i.place_id`, tripID, id, in.Title, in.StartsAt, in.EndsAt, in.TimeZone, in.Notes, in.PlaceID))
}
func (r *Repository) Delete(ctx context.Context, tripID, id int64) error {
	tag, err := r.pool.Exec(ctx, "DELETE FROM itinerary_items WHERE trip_id=$1 AND id=$2", tripID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperror.ErrNotFound
	}
	return nil
}
