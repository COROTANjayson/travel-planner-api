package trips

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"travel-planner/travel-planner-api/internal/apperror"
)

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

const columns = "id, name, destination, to_char(start_date, 'YYYY-MM-DD'), to_char(end_date, 'YYYY-MM-DD'), time_zone, created_at, updated_at"

func scan(row pgx.Row) (Trip, error) {
	var t Trip
	err := row.Scan(&t.ID, &t.Name, &t.Destination, &t.StartDate, &t.EndDate, &t.TimeZone, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return t, apperror.ErrNotFound
	}
	t.CreatedAt = t.CreatedAt.UTC()
	t.UpdatedAt = t.UpdatedAt.UTC()
	return t, err
}

func (r *Repository) Create(ctx context.Context, ownerUserID int64, in Input) (Trip, error) {
	return scan(r.pool.QueryRow(ctx, "INSERT INTO trips (owner_user_id, name, destination, start_date, end_date, time_zone) VALUES ($1,$2,$3,$4::text::date,$5::text::date,$6) RETURNING "+columns, ownerUserID, in.Name, in.Destination, in.StartDate, in.EndDate, in.TimeZone))
}
func (r *Repository) Get(ctx context.Context, ownerUserID, id int64) (Trip, error) {
	return scan(r.pool.QueryRow(ctx, "SELECT "+columns+" FROM trips WHERE owner_user_id=$1 AND id=$2", ownerUserID, id))
}
func (r *Repository) List(ctx context.Context, ownerUserID int64, limit, offset int) ([]Trip, error) {
	rows, err := r.pool.Query(ctx, "SELECT "+columns+" FROM trips WHERE owner_user_id=$1 ORDER BY id DESC LIMIT $2 OFFSET $3", ownerUserID, limit, offset)
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
func (r *Repository) Update(ctx context.Context, ownerUserID, id int64, in Input) (Trip, error) {
	return scan(r.pool.QueryRow(ctx, "UPDATE trips SET name=$3, destination=$4, start_date=$5::text::date, end_date=$6::text::date, time_zone=$7, updated_at=now() WHERE owner_user_id=$1 AND id=$2 RETURNING "+columns, ownerUserID, id, in.Name, in.Destination, in.StartDate, in.EndDate, in.TimeZone))
}
func (r *Repository) Delete(ctx context.Context, ownerUserID, id int64) error {
	tag, err := r.pool.Exec(ctx, "DELETE FROM trips WHERE owner_user_id=$1 AND id=$2", ownerUserID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperror.ErrNotFound
	}
	return nil
}
