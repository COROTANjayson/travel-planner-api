package memberships

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"travel-planner/travel-planner-api/internal/auth"
	"travel-planner/travel-planner-api/internal/httpx"
)

func currentUser(w http.ResponseWriter, r *http.Request) (auth.User, bool) {
	user, ok := auth.CurrentUser(r.Context())
	if !ok {
		httpx.JSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}
	return user, ok
}

func Register(r chi.Router, service *Service) {
	r.Get("/trips/{tripID}/members", func(w http.ResponseWriter, r *http.Request) {
		user, ok := currentUser(w, r)
		if !ok {
			return
		}
		tripID, err := httpx.ID(r, "tripID")
		if err != nil {
			httpx.Error(w, err)
			return
		}
		members, err := service.ListMembers(r.Context(), user.ID, tripID)
		if err != nil {
			httpx.Error(w, err)
			return
		}
		httpx.JSON(w, http.StatusOK, members)
	})
	r.Post("/trips/{tripID}/invitations", func(w http.ResponseWriter, r *http.Request) {
		user, ok := currentUser(w, r)
		if !ok {
			return
		}
		tripID, err := httpx.ID(r, "tripID")
		if err != nil {
			httpx.Error(w, err)
			return
		}
		var input InvitationInput
		if err := httpx.Decode(w, r, &input); err != nil {
			httpx.Error(w, err)
			return
		}
		invitation, err := service.CreateInvitation(r.Context(), user.ID, tripID, input)
		if err != nil {
			httpx.Error(w, err)
			return
		}
		w.Header().Set("Location", fmt.Sprintf("/api/v1/trips/%d/invitations/%d", tripID, invitation.ID))
		httpx.JSON(w, http.StatusCreated, invitation)
	})
	r.Get("/trips/{tripID}/invitations", func(w http.ResponseWriter, r *http.Request) {
		user, ok := currentUser(w, r)
		if !ok {
			return
		}
		tripID, err := httpx.ID(r, "tripID")
		if err != nil {
			httpx.Error(w, err)
			return
		}
		invitations, err := service.ListInvitations(r.Context(), user.ID, tripID)
		if err != nil {
			httpx.Error(w, err)
			return
		}
		httpx.JSON(w, http.StatusOK, invitations)
	})
	r.Delete("/trips/{tripID}/invitations/{invitationID}", func(w http.ResponseWriter, r *http.Request) {
		user, ok := currentUser(w, r)
		if !ok {
			return
		}
		tripID, err := httpx.ID(r, "tripID")
		if err != nil {
			httpx.Error(w, err)
			return
		}
		invitationID, err := httpx.ID(r, "invitationID")
		if err != nil {
			httpx.Error(w, err)
			return
		}
		if err := service.RevokeInvitation(r.Context(), user.ID, tripID, invitationID); err != nil {
			httpx.Error(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	r.Post("/invitations/accept", func(w http.ResponseWriter, r *http.Request) {
		user, ok := currentUser(w, r)
		if !ok {
			return
		}
		var input AcceptInput
		if err := httpx.Decode(w, r, &input); err != nil {
			httpx.Error(w, err)
			return
		}
		member, err := service.AcceptInvitation(r.Context(), user, input.Token)
		if err != nil {
			httpx.Error(w, err)
			return
		}
		httpx.JSON(w, http.StatusOK, member)
	})
	r.Put("/trips/{tripID}/members/{userID}", func(w http.ResponseWriter, r *http.Request) {
		user, ok := currentUser(w, r)
		if !ok {
			return
		}
		tripID, err := httpx.ID(r, "tripID")
		if err != nil {
			httpx.Error(w, err)
			return
		}
		userID, err := httpx.ID(r, "userID")
		if err != nil {
			httpx.Error(w, err)
			return
		}
		var input RoleInput
		if err := httpx.Decode(w, r, &input); err != nil {
			httpx.Error(w, err)
			return
		}
		member, err := service.UpdateRole(r.Context(), user.ID, tripID, userID, input.Role)
		if err != nil {
			httpx.Error(w, err)
			return
		}
		httpx.JSON(w, http.StatusOK, member)
	})
	r.Delete("/trips/{tripID}/members/{userID}", func(w http.ResponseWriter, r *http.Request) {
		user, ok := currentUser(w, r)
		if !ok {
			return
		}
		tripID, err := httpx.ID(r, "tripID")
		if err != nil {
			httpx.Error(w, err)
			return
		}
		userID, err := httpx.ID(r, "userID")
		if err != nil {
			httpx.Error(w, err)
			return
		}
		if err := service.RemoveMember(r.Context(), user.ID, tripID, userID); err != nil {
			httpx.Error(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	r.Post("/trips/{tripID}/transfer-ownership", func(w http.ResponseWriter, r *http.Request) {
		user, ok := currentUser(w, r)
		if !ok {
			return
		}
		tripID, err := httpx.ID(r, "tripID")
		if err != nil {
			httpx.Error(w, err)
			return
		}
		var input TransferInput
		if err := httpx.Decode(w, r, &input); err != nil {
			httpx.Error(w, err)
			return
		}
		if err := service.TransferOwnership(r.Context(), user.ID, tripID, input.UserID); err != nil {
			httpx.Error(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
