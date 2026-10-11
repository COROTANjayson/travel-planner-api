package trips

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"travel-planner/travel-planner-api/internal/apperror"
)

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

const columns = "id, name, destination, to_char(start_date, 'YYYY-MM-DD'), to_char(end_date, 'YYYY-MM-DD'), time_zone, destination_place_id, starts_at, ends_at, created_at, updated_at"

func scan(row pgx.Row) (Trip, error) {
	var t Trip
	err := row.Scan(&t.ID, &t.Name, &t.Destination, &t.StartDate, &t.EndDate, &t.TimeZone, &t.DestinationPlaceID, &t.StartsAt, &t.EndsAt, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return t, apperror.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" && pgErr.ConstraintName == "trips_destination_place_id_fkey" {
		return t, apperror.ErrInvalid
	}
	if err != nil {
		return t, err
	}
	t.CreatedAt = t.CreatedAt.UTC()
	t.UpdatedAt = t.UpdatedAt.UTC()
	if t.StartsAt != nil {
		v := t.StartsAt.UTC()
		t.StartsAt = &v
	}
	if t.EndsAt != nil {
		v := t.EndsAt.UTC()
		t.EndsAt = &v
	}
	return t, err
}

func (r *Repository) Destination(ctx context.Context, id int64) (Destination, error) {
	var d Destination
	err := r.pool.QueryRow(ctx, "SELECT name,kind,time_zone FROM places WHERE id=$1", id).Scan(&d.Name, &d.Kind, &d.TimeZone)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, apperror.ErrInvalid
	}
	return d, err
}

func (r *Repository) Create(ctx context.Context, ownerUserID int64, in Input) (Trip, error) {
	return scan(r.pool.QueryRow(ctx, "INSERT INTO trips (owner_user_id, name, destination, start_date, end_date, time_zone, destination_place_id, starts_at, ends_at) VALUES ($1,$2,$3,$4::text::date,$5::text::date,$6,$7,$8,$9) RETURNING "+columns, ownerUserID, in.Name, in.Destination, in.StartDate, in.EndDate, in.TimeZone, in.DestinationPlaceID, in.StartsAt, in.EndsAt))
}
func (r *Repository) Get(ctx context.Context, id int64) (Trip, error) {
	return scan(r.pool.QueryRow(ctx, "SELECT "+columns+" FROM trips WHERE id=$1", id))
}
func (r *Repository) List(ctx context.Context, ownerUserID int64, limit, offset int) ([]Trip, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+columns+` FROM trips t
		WHERE t.owner_user_id=$1 OR EXISTS(SELECT 1 FROM trip_members tm WHERE tm.trip_id=t.id AND tm.user_id=$1)
		ORDER BY t.id DESC LIMIT $2 OFFSET $3`, ownerUserID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Trip, 0)
	for rows.Next() {
		t, err := scan(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, t)
	}
	return result, rows.Err()
}
func (r *Repository) Update(ctx context.Context, id int64, in Input) (Trip, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Trip{}, err
	}
	defer tx.Rollback(ctx)
	var exists int64
	if err := tx.QueryRow(ctx, "SELECT id FROM trips WHERE id=$1 FOR UPDATE", id).Scan(&exists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Trip{}, apperror.ErrNotFound
		}
		return Trip{}, err
	}
	var invalid bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM itinerary_items WHERE trip_id=$1 AND (
		($2::timestamptz IS NOT NULL AND (starts_at<$2 OR ends_at>$3::timestamptz)) OR
		($2::timestamptz IS NULL AND ((starts_at AT TIME ZONE $6)::date<$4::text::date OR (ends_at AT TIME ZONE $6)::date>$5::text::date))))`,
		id, in.StartsAt, in.EndsAt, in.StartDate, in.EndDate, in.TimeZone).Scan(&invalid); err != nil {
		return Trip{}, err
	}
	if invalid {
		return Trip{}, fmt.Errorf("%w: trip window excludes an existing activity", apperror.ErrConflict)
	}
	t, err := scan(tx.QueryRow(ctx, "UPDATE trips SET name=$2, destination=$3, start_date=$4::text::date, end_date=$5::text::date, time_zone=$6, destination_place_id=$7, starts_at=$8, ends_at=$9, updated_at=now() WHERE id=$1 RETURNING "+columns, id, in.Name, in.Destination, in.StartDate, in.EndDate, in.TimeZone, in.DestinationPlaceID, in.StartsAt, in.EndsAt))
	if err != nil {
		return Trip{}, err
	}
	if _, err := tx.Exec(ctx, "UPDATE itinerary_items SET time_zone=$2, updated_at=now() WHERE trip_id=$1 AND time_zone<>$2", id, in.TimeZone); err != nil {
		return Trip{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Trip{}, err
	}
	return t, nil
}
func (r *Repository) Delete(ctx context.Context, id int64) error {
	tag, err := r.pool.Exec(ctx, "DELETE FROM trips WHERE id=$1", id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperror.ErrNotFound
	}
	return nil
}
