package auth

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store interface {
	GetOrCreate(context.Context, string, *string, string) (User, error)
}

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

const userColumns = "id, email, display_name, created_at, updated_at"

func scanUser(row pgx.Row) (User, error) {
	var user User
	err := row.Scan(&user.ID, &user.Email, &user.DisplayName, &user.CreatedAt, &user.UpdatedAt)
	user.CreatedAt = user.CreatedAt.UTC()
	user.UpdatedAt = user.UpdatedAt.UTC()
	return user, err
}

func (r *Repository) GetOrCreate(ctx context.Context, subject string, email *string, displayName string) (User, error) {
	user, err := scanUser(r.pool.QueryRow(ctx, "SELECT "+userColumns+" FROM users WHERE auth_subject=$1", subject))
	if err == nil {
		return user, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return User{}, err
	}
	return scanUser(r.pool.QueryRow(ctx, `
		INSERT INTO users (auth_subject, email, display_name) VALUES ($1,$2,$3)
		ON CONFLICT (auth_subject) DO UPDATE SET auth_subject=EXCLUDED.auth_subject
		RETURNING `+userColumns, subject, email, displayName))
}
