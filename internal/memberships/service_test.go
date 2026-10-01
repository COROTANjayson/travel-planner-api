package memberships

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"travel-planner/travel-planner-api/internal/apperror"
	"travel-planner/travel-planner-api/internal/auth"
)

type testStore struct {
	Store
	role       Role
	called     bool
	email      string
	tokenHash  []byte
	expiresAt  time.Time
	invitation Invitation
}

func (s *testStore) Role(context.Context, int64, int64) (Role, error) {
	if s.role == "" {
		return "", apperror.ErrNotFound
	}
	return s.role, nil
}

func (s *testStore) CreateInvitation(_ context.Context, _, _ int64, email string, _ Role, tokenHash []byte, expiresAt time.Time) (Invitation, error) {
	s.called, s.email, s.tokenHash, s.expiresAt = true, email, append([]byte(nil), tokenHash...), expiresAt
	return s.invitation, nil
}

func (s *testStore) AcceptInvitation(context.Context, auth.User, []byte, time.Time) (Participant, error) {
	s.called = true
	return Participant{UserID: 2, Role: RoleEditor}, nil
}

func TestAuthorizationMatrix(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		role                   Role
		participant, edit, own error
	}{
		{RoleOwner, nil, nil, nil},
		{RoleEditor, nil, nil, apperror.ErrForbidden},
		{RoleMember, nil, apperror.ErrForbidden, apperror.ErrForbidden},
		{RoleViewer, nil, apperror.ErrForbidden, apperror.ErrForbidden},
		{"", apperror.ErrNotFound, apperror.ErrNotFound, apperror.ErrNotFound},
	} {
		service := NewService(&testStore{role: tc.role})
		for name, check := range map[string]struct {
			got  error
			want error
		}{
			"participant": {service.RequireParticipant(ctx, 1, 1), tc.participant},
			"editor":      {service.RequireEditor(ctx, 1, 1), tc.edit},
			"owner":       {service.RequireOwner(ctx, 1, 1), tc.own},
		} {
			if !errors.Is(check.got, check.want) || (check.want == nil && check.got != nil) {
				t.Fatalf("%s as %q: got %v, want %v", name, tc.role, check.got, check.want)
			}
		}
	}
}

func TestInvitationValidationAndToken(t *testing.T) {
	store := &testStore{role: RoleOwner, invitation: Invitation{ID: 1}}
	service := NewService(store)
	before := time.Now().UTC()
	invitation, err := service.CreateInvitation(context.Background(), 1, 2, InvitationInput{Email: " Traveler@Example.COM ", Role: RoleEditor})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(invitation.Token)
	if err != nil || len(raw) != 32 || len(store.tokenHash) != 32 {
		t.Fatalf("invalid generated token: %v, %d raw bytes, %d hash bytes", err, len(raw), len(store.tokenHash))
	}
	if store.email != "traveler@example.com" || store.expiresAt.Before(before.Add(7*24*time.Hour-time.Second)) {
		t.Fatalf("invitation was not normalized: %q, %s", store.email, store.expiresAt)
	}
	for _, input := range []InvitationInput{{Email: "not an email", Role: RoleEditor}, {Email: "a@example.com", Role: RoleOwner}} {
		store.called = false
		if _, err := service.CreateInvitation(context.Background(), 1, 2, input); !errors.Is(err, apperror.ErrInvalid) || store.called {
			t.Fatalf("invalid invitation reached repository: %v", err)
		}
	}
}

func TestAcceptanceRequiresVerifiedEmail(t *testing.T) {
	email := "traveler@example.com"
	store := &testStore{}
	service := NewService(store)
	for _, user := range []auth.User{{ID: 1}, {ID: 1, Email: &email}} {
		if _, err := service.AcceptInvitation(context.Background(), user, "token"); !errors.Is(err, apperror.ErrInvalid) || store.called {
			t.Fatalf("unverified user reached repository: %v", err)
		}
	}
	user := auth.User{ID: 1, Email: &email, EmailVerified: true}
	if _, err := service.AcceptInvitation(context.Background(), user, "token"); err != nil || !store.called {
		t.Fatalf("verified user could not accept: %v", err)
	}
}
