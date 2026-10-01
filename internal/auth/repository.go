package auth

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store interface {
	GetOrCreate(context.Context, string, *string, bool, string) (User, error)
}

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

const userColumns = "id, email, email_verified, display_name, created_at, updated_at"

func scanUser(row pgx.Row) (User, error) {
	var user User
	err := row.Scan(&user.ID, &user.Email, &user.EmailVerified, &user.DisplayName, &user.CreatedAt, &user.UpdatedAt)
	user.CreatedAt = user.CreatedAt.UTC()
	user.UpdatedAt = user.UpdatedAt.UTC()
	return user, err
}

func (r *Repository) GetOrCreate(ctx context.Context, subject string, email *string, emailVerified bool, displayName string) (User, error) {
	return scanUser(r.pool.QueryRow(ctx, `
		INSERT INTO users (auth_subject, email, email_verified, display_name) VALUES ($1,$2,$3,$4)
		ON CONFLICT (auth_subject) DO UPDATE SET
			email=EXCLUDED.email,
			email_verified=EXCLUDED.email_verified,
			updated_at=CASE WHEN users.email IS DISTINCT FROM EXCLUDED.email OR users.email_verified IS DISTINCT FROM EXCLUDED.email_verified THEN now() ELSE users.updated_at END
		RETURNING `+userColumns, subject, email, emailVerified, displayName))
}
