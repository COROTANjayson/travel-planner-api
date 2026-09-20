package itinerary

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"travel-planner/travel-planner-api/internal/apperror"
)

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

const columns = "id, trip_id, title, starts_at, ends_at, time_zone, notes, created_at, updated_at"

func scan(row pgx.Row) (Activity, error) {
	var a Activity
	err := row.Scan(&a.ID, &a.TripID, &a.Title, &a.StartsAt, &a.EndsAt, &a.TimeZone, &a.Notes, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, apperror.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return a, apperror.ErrNotFound
	}
	a.StartsAt, a.EndsAt = a.StartsAt.UTC(), a.EndsAt.UTC()
	a.CreatedAt, a.UpdatedAt = a.CreatedAt.UTC(), a.UpdatedAt.UTC()
	return a, err
}
func (r *Repository) Create(ctx context.Context, tripID int64, in Input) (Activity, error) {
	return scan(r.pool.QueryRow(ctx, "INSERT INTO itinerary_items (trip_id,title,starts_at,ends_at,time_zone,notes) VALUES ($1,$2,$3,$4,$5,$6) RETURNING "+columns, tripID, in.Title, in.StartsAt, in.EndsAt, in.TimeZone, in.Notes))
}
func (r *Repository) Get(ctx context.Context, tripID, id int64) (Activity, error) {
	return scan(r.pool.QueryRow(ctx, "SELECT "+columns+" FROM itinerary_items WHERE trip_id=$1 AND id=$2", tripID, id))
}
func (r *Repository) List(ctx context.Context, tripID int64, limit, offset int) ([]Activity, error) {
	var exists bool
	if err := r.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM trips WHERE id=$1)", tripID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, apperror.ErrNotFound
	}
	rows, err := r.pool.Query(ctx, "SELECT "+columns+" FROM itinerary_items WHERE trip_id=$1 ORDER BY starts_at,id LIMIT $2 OFFSET $3", tripID, limit, offset)
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
	return scan(r.pool.QueryRow(ctx, "UPDATE itinerary_items SET title=$3, starts_at=$4, ends_at=$5, time_zone=$6, notes=$7, updated_at=now() WHERE trip_id=$1 AND id=$2 RETURNING "+columns, tripID, id, in.Title, in.StartsAt, in.EndsAt, in.TimeZone, in.Notes))
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
