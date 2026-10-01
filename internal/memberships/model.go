package memberships

import "time"

type Role string

const (
	RoleOwner  Role = "owner"
	RoleEditor Role = "editor"
	RoleMember Role = "member"
	RoleViewer Role = "viewer"
)

type Participant struct {
	UserID      int64   `json:"user_id"`
	Email       *string `json:"email"`
	DisplayName string  `json:"display_name"`
	Role        Role    `json:"role"`
}

type Invitation struct {
	ID              int64      `json:"id"`
	TripID          int64      `json:"trip_id"`
	Email           string     `json:"email"`
	Role            Role       `json:"role"`
	InvitedByUserID int64      `json:"invited_by_user_id"`
	ExpiresAt       time.Time  `json:"expires_at"`
	AcceptedAt      *time.Time `json:"accepted_at"`
	RevokedAt       *time.Time `json:"revoked_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	Token           string     `json:"token,omitempty"`
}

type InvitationInput struct {
	Email string `json:"email"`
	Role  Role   `json:"role"`
}

type AcceptInput struct {
	Token string `json:"token"`
}

type RoleInput struct {
	Role Role `json:"role"`
}

type TransferInput struct {
	UserID int64 `json:"user_id"`
}
