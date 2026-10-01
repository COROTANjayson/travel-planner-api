package memberships

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"travel-planner/travel-planner-api/internal/apperror"
	"travel-planner/travel-planner-api/internal/auth"
)

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func invalidInvitation() error { return fmt.Errorf("%w: invalid invitation", apperror.ErrInvalid) }

func (r *Repository) Role(ctx context.Context, userID, tripID int64) (Role, error) {
	var role Role
	err := r.pool.QueryRow(ctx, `
		SELECT CASE WHEN t.owner_user_id=$1 THEN 'owner' ELSE tm.role END
		FROM trips t
		LEFT JOIN trip_members tm ON tm.trip_id=t.id AND tm.user_id=$1
		WHERE t.id=$2 AND (t.owner_user_id=$1 OR tm.user_id IS NOT NULL)`, userID, tripID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", apperror.ErrNotFound
	}
	return role, err
}

func scanParticipant(row pgx.Row) (Participant, error) {
	var participant Participant
	err := row.Scan(&participant.UserID, &participant.Email, &participant.DisplayName, &participant.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		return participant, apperror.ErrNotFound
	}
	return participant, err
}

func participant(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, tripID, userID int64) (Participant, error) {
	return scanParticipant(q.QueryRow(ctx, `
		SELECT u.id,u.email,u.display_name,
			CASE WHEN t.owner_user_id=u.id THEN 'owner' ELSE tm.role END
		FROM users u
		JOIN trips t ON t.id=$1
		LEFT JOIN trip_members tm ON tm.trip_id=t.id AND tm.user_id=u.id
		WHERE u.id=$2 AND (t.owner_user_id=u.id OR tm.user_id IS NOT NULL)`, tripID, userID))
}

func (r *Repository) ListMembers(ctx context.Context, tripID int64) ([]Participant, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT u.id,u.email,u.display_name,p.role
		FROM (
			SELECT owner_user_id AS user_id, 'owner'::text AS role, 0 AS rank FROM trips WHERE id=$1
			UNION ALL
			SELECT user_id,role,1 FROM trip_members WHERE trip_id=$1
		) p JOIN users u ON u.id=p.user_id
		ORDER BY p.rank,u.id`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Participant, 0)
	for rows.Next() {
		member, err := scanParticipant(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, member)
	}
	return result, rows.Err()
}

const invitationColumns = "id,trip_id,email,role,invited_by_user_id,expires_at,accepted_at,revoked_at,created_at,updated_at"

func scanInvitation(row pgx.Row) (Invitation, error) {
	var invitation Invitation
	err := row.Scan(&invitation.ID, &invitation.TripID, &invitation.Email, &invitation.Role, &invitation.InvitedByUserID, &invitation.ExpiresAt, &invitation.AcceptedAt, &invitation.RevokedAt, &invitation.CreatedAt, &invitation.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return invitation, apperror.ErrNotFound
	}
	invitation.ExpiresAt = invitation.ExpiresAt.UTC()
	invitation.CreatedAt = invitation.CreatedAt.UTC()
	invitation.UpdatedAt = invitation.UpdatedAt.UTC()
	if invitation.AcceptedAt != nil {
		value := invitation.AcceptedAt.UTC()
		invitation.AcceptedAt = &value
	}
	if invitation.RevokedAt != nil {
		value := invitation.RevokedAt.UTC()
		invitation.RevokedAt = &value
	}
	return invitation, err
}

func (r *Repository) CreateInvitation(ctx context.Context, ownerUserID, tripID int64, email string, role Role, tokenHash []byte, expiresAt time.Time) (Invitation, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Invitation{}, err
	}
	defer tx.Rollback(ctx)
	var currentOwner int64
	if err := tx.QueryRow(ctx, "SELECT owner_user_id FROM trips WHERE id=$1 FOR UPDATE", tripID).Scan(&currentOwner); errors.Is(err, pgx.ErrNoRows) {
		return Invitation{}, apperror.ErrNotFound
	} else if err != nil {
		return Invitation{}, err
	}
	if currentOwner != ownerUserID {
		return Invitation{}, apperror.ErrForbidden
	}
	var alreadyParticipant bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM users u
			WHERE lower(trim(u.email))=$2 AND (
				u.id=$3 OR EXISTS(SELECT 1 FROM trip_members tm WHERE tm.trip_id=$1 AND tm.user_id=u.id)
			)
		)`, tripID, email, currentOwner).Scan(&alreadyParticipant); err != nil {
		return Invitation{}, err
	}
	if alreadyParticipant {
		return Invitation{}, fmt.Errorf("%w: user is already a participant", apperror.ErrConflict)
	}
	if _, err := tx.Exec(ctx, `UPDATE trip_invitations SET revoked_at=now(),updated_at=now()
		WHERE trip_id=$1 AND email=$2 AND accepted_at IS NULL AND revoked_at IS NULL`, tripID, email); err != nil {
		return Invitation{}, err
	}
	invitation, err := scanInvitation(tx.QueryRow(ctx, `INSERT INTO trip_invitations
		(trip_id,email,role,token_hash,invited_by_user_id,expires_at) VALUES ($1,$2,$3,$4,$5,$6)
		RETURNING `+invitationColumns, tripID, email, role, tokenHash, ownerUserID, expiresAt))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return Invitation{}, fmt.Errorf("%w: active invitation already exists", apperror.ErrConflict)
		}
		return Invitation{}, err
	}
	return invitation, tx.Commit(ctx)
}

func (r *Repository) ListInvitations(ctx context.Context, tripID int64) ([]Invitation, error) {
	rows, err := r.pool.Query(ctx, "SELECT "+invitationColumns+" FROM trip_invitations WHERE trip_id=$1 ORDER BY id DESC", tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Invitation, 0)
	for rows.Next() {
		invitation, err := scanInvitation(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, invitation)
	}
	return result, rows.Err()
}

func (r *Repository) RevokeInvitation(ctx context.Context, tripID, invitationID int64) error {
	tag, err := r.pool.Exec(ctx, `UPDATE trip_invitations SET revoked_at=now(),updated_at=now()
		WHERE trip_id=$1 AND id=$2 AND accepted_at IS NULL AND revoked_at IS NULL`, tripID, invitationID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperror.ErrNotFound
	}
	return nil
}

func (r *Repository) AcceptInvitation(ctx context.Context, user auth.User, tokenHash []byte, now time.Time) (Participant, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Participant{}, err
	}
	defer tx.Rollback(ctx)
	var tripID int64
	if err := tx.QueryRow(ctx, "SELECT trip_id FROM trip_invitations WHERE token_hash=$1", tokenHash).Scan(&tripID); errors.Is(err, pgx.ErrNoRows) {
		return Participant{}, invalidInvitation()
	} else if err != nil {
		return Participant{}, err
	}
	var ownerUserID int64
	if err := tx.QueryRow(ctx, "SELECT owner_user_id FROM trips WHERE id=$1 FOR UPDATE", tripID).Scan(&ownerUserID); err != nil {
		return Participant{}, invalidInvitation()
	}
	var email string
	var role Role
	var expiresAt time.Time
	var acceptedAt, revokedAt *time.Time
	var acceptedBy *int64
	if err := tx.QueryRow(ctx, `SELECT email,role,expires_at,accepted_at,revoked_at,accepted_by_user_id
		FROM trip_invitations WHERE token_hash=$1 FOR UPDATE`, tokenHash).Scan(&email, &role, &expiresAt, &acceptedAt, &revokedAt, &acceptedBy); err != nil {
		return Participant{}, invalidInvitation()
	}
	if acceptedAt != nil {
		if acceptedBy == nil || *acceptedBy != user.ID {
			return Participant{}, invalidInvitation()
		}
		member, err := participant(ctx, tx, tripID, user.ID)
		if err != nil {
			return Participant{}, invalidInvitation()
		}
		return member, tx.Commit(ctx)
	}
	userEmail := strings.ToLower(strings.TrimSpace(*user.Email))
	if revokedAt != nil || !expiresAt.After(now) || userEmail != email || ownerUserID == user.ID {
		return Participant{}, invalidInvitation()
	}
	if _, err := tx.Exec(ctx, `INSERT INTO trip_members (trip_id,user_id,role) VALUES ($1,$2,$3)
		ON CONFLICT (trip_id,user_id) DO UPDATE SET role=EXCLUDED.role,updated_at=now()`, tripID, user.ID, role); err != nil {
		return Participant{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE trip_invitations SET accepted_at=$2,accepted_by_user_id=$3,updated_at=$2 WHERE token_hash=$1`, tokenHash, now, user.ID); err != nil {
		return Participant{}, err
	}
	member, err := participant(ctx, tx, tripID, user.ID)
	if err != nil {
		return Participant{}, err
	}
	return member, tx.Commit(ctx)
}

func (r *Repository) UpdateRole(ctx context.Context, tripID, userID int64, role Role) (Participant, error) {
	tag, err := r.pool.Exec(ctx, "UPDATE trip_members SET role=$3,updated_at=now() WHERE trip_id=$1 AND user_id=$2", tripID, userID, role)
	if err != nil {
		return Participant{}, err
	}
	if tag.RowsAffected() == 0 {
		return Participant{}, apperror.ErrNotFound
	}
	return participant(ctx, r.pool, tripID, userID)
}

func (r *Repository) RemoveMember(ctx context.Context, tripID, userID int64) error {
	tag, err := r.pool.Exec(ctx, "DELETE FROM trip_members WHERE trip_id=$1 AND user_id=$2", tripID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperror.ErrNotFound
	}
	return nil
}

func (r *Repository) TransferOwnership(ctx context.Context, ownerUserID, tripID, newOwnerUserID int64) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var currentOwner int64
	if err := tx.QueryRow(ctx, "SELECT owner_user_id FROM trips WHERE id=$1 FOR UPDATE", tripID).Scan(&currentOwner); errors.Is(err, pgx.ErrNoRows) {
		return apperror.ErrNotFound
	} else if err != nil {
		return err
	}
	if currentOwner != ownerUserID {
		return apperror.ErrForbidden
	}
	var exists bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM trip_members WHERE trip_id=$1 AND user_id=$2)", tripID, newOwnerUserID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("%w: new owner must be an existing member", apperror.ErrConflict)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO trip_members (trip_id,user_id,role) VALUES ($1,$2,'editor')
		ON CONFLICT (trip_id,user_id) DO UPDATE SET role='editor',updated_at=now()`, tripID, ownerUserID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "DELETE FROM trip_members WHERE trip_id=$1 AND user_id=$2", tripID, newOwnerUserID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "UPDATE trips SET owner_user_id=$2,updated_at=now() WHERE id=$1", tripID, newOwnerUserID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
