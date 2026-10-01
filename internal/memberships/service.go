package memberships

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"travel-planner/travel-planner-api/internal/apperror"
	"travel-planner/travel-planner-api/internal/auth"
)

type Store interface {
	Role(context.Context, int64, int64) (Role, error)
	ListMembers(context.Context, int64) ([]Participant, error)
	CreateInvitation(context.Context, int64, int64, string, Role, []byte, time.Time) (Invitation, error)
	ListInvitations(context.Context, int64) ([]Invitation, error)
	RevokeInvitation(context.Context, int64, int64) error
	AcceptInvitation(context.Context, auth.User, []byte, time.Time) (Participant, error)
	UpdateRole(context.Context, int64, int64, Role) (Participant, error)
	RemoveMember(context.Context, int64, int64) error
	TransferOwnership(context.Context, int64, int64, int64) error
}

type Service struct{ store Store }

func NewService(store Store) *Service { return &Service{store: store} }

func validRole(role Role) bool {
	return role == RoleEditor || role == RoleMember || role == RoleViewer
}

func normalizeEmail(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	parsed, err := mail.ParseAddress(value)
	if err != nil || parsed.Address != value || len(value) > 320 {
		return "", fmt.Errorf("%w: email must be a valid address", apperror.ErrInvalid)
	}
	return value, nil
}

func (s *Service) RequireParticipant(ctx context.Context, userID, tripID int64) error {
	_, err := s.store.Role(ctx, userID, tripID)
	return err
}

func (s *Service) RequireEditor(ctx context.Context, userID, tripID int64) error {
	role, err := s.store.Role(ctx, userID, tripID)
	if err != nil {
		return err
	}
	if role != RoleOwner && role != RoleEditor {
		return apperror.ErrForbidden
	}
	return nil
}

func (s *Service) RequireOwner(ctx context.Context, userID, tripID int64) error {
	role, err := s.store.Role(ctx, userID, tripID)
	if err != nil {
		return err
	}
	if role != RoleOwner {
		return apperror.ErrForbidden
	}
	return nil
}

func (s *Service) ListMembers(ctx context.Context, userID, tripID int64) ([]Participant, error) {
	if err := s.RequireParticipant(ctx, userID, tripID); err != nil {
		return nil, err
	}
	return s.store.ListMembers(ctx, tripID)
}

func (s *Service) CreateInvitation(ctx context.Context, userID, tripID int64, in InvitationInput) (Invitation, error) {
	if err := s.RequireOwner(ctx, userID, tripID); err != nil {
		return Invitation{}, err
	}
	email, err := normalizeEmail(in.Email)
	if err != nil || !validRole(in.Role) {
		if err != nil {
			return Invitation{}, err
		}
		return Invitation{}, fmt.Errorf("%w: role must be editor, member, or viewer", apperror.ErrInvalid)
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return Invitation{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	invitation, err := s.store.CreateInvitation(ctx, userID, tripID, email, in.Role, hash[:], time.Now().UTC().Add(7*24*time.Hour))
	if err == nil {
		invitation.Token = token
	}
	return invitation, err
}

func (s *Service) ListInvitations(ctx context.Context, userID, tripID int64) ([]Invitation, error) {
	if err := s.RequireOwner(ctx, userID, tripID); err != nil {
		return nil, err
	}
	return s.store.ListInvitations(ctx, tripID)
}

func (s *Service) RevokeInvitation(ctx context.Context, userID, tripID, invitationID int64) error {
	if err := s.RequireOwner(ctx, userID, tripID); err != nil {
		return err
	}
	return s.store.RevokeInvitation(ctx, tripID, invitationID)
}

func (s *Service) AcceptInvitation(ctx context.Context, user auth.User, token string) (Participant, error) {
	token = strings.TrimSpace(token)
	if token == "" || user.Email == nil || !user.EmailVerified {
		return Participant{}, fmt.Errorf("%w: invalid invitation", apperror.ErrInvalid)
	}
	hash := sha256.Sum256([]byte(token))
	return s.store.AcceptInvitation(ctx, user, hash[:], time.Now().UTC())
}

func (s *Service) UpdateRole(ctx context.Context, ownerUserID, tripID, userID int64, role Role) (Participant, error) {
	if err := s.RequireOwner(ctx, ownerUserID, tripID); err != nil {
		return Participant{}, err
	}
	if ownerUserID == userID {
		return Participant{}, fmt.Errorf("%w: owner role cannot be changed", apperror.ErrConflict)
	}
	if !validRole(role) {
		return Participant{}, fmt.Errorf("%w: role must be editor, member, or viewer", apperror.ErrInvalid)
	}
	return s.store.UpdateRole(ctx, tripID, userID, role)
}

func (s *Service) RemoveMember(ctx context.Context, callerUserID, tripID, userID int64) error {
	role, err := s.store.Role(ctx, callerUserID, tripID)
	if err != nil {
		return err
	}
	if role == RoleOwner && callerUserID == userID {
		return fmt.Errorf("%w: transfer ownership before leaving", apperror.ErrConflict)
	}
	if role != RoleOwner && callerUserID != userID {
		return apperror.ErrForbidden
	}
	return s.store.RemoveMember(ctx, tripID, userID)
}

func (s *Service) TransferOwnership(ctx context.Context, ownerUserID, tripID, newOwnerUserID int64) error {
	if err := s.RequireOwner(ctx, ownerUserID, tripID); err != nil {
		return err
	}
	if newOwnerUserID <= 0 || newOwnerUserID == ownerUserID {
		return fmt.Errorf("%w: new owner must be an existing member", apperror.ErrConflict)
	}
	return s.store.TransferOwnership(ctx, ownerUserID, tripID, newOwnerUserID)
}
