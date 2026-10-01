package itinerary

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"travel-planner/travel-planner-api/internal/auth"
	"travel-planner/travel-planner-api/internal/httpx"
)

func currentUserID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	user, ok := auth.CurrentUser(r.Context())
	if !ok {
		httpx.JSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return 0, false
	}
	return user.ID, true
}

func Register(r chi.Router, s *Service) {
	r.Route("/trips/{tripID}/activities", func(r chi.Router) {
		r.Post("/", func(w http.ResponseWriter, r *http.Request) {
			userID, ok := currentUserID(w, r)
			if !ok {
				return
			}
			tripID, err := httpx.ID(r, "tripID")
			if err != nil {
				httpx.Error(w, err)
				return
			}
			var in Input
			if err := httpx.Decode(w, r, &in); err != nil {
				httpx.Error(w, err)
				return
			}
			a, err := s.Create(r.Context(), userID, tripID, in)
			if err != nil {
				httpx.Error(w, err)
				return
			}
			w.Header().Set("Location", fmt.Sprintf("/api/v1/trips/%d/activities/%d", tripID, a.ID))
			httpx.JSON(w, http.StatusCreated, a)
		})
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			userID, ok := currentUserID(w, r)
			if !ok {
				return
			}
			tripID, err := httpx.ID(r, "tripID")
			if err != nil {
				httpx.Error(w, err)
				return
			}
			limit, offset, err := httpx.Pagination(r)
			if err != nil {
				httpx.Error(w, err)
				return
			}
			items, err := s.List(r.Context(), userID, tripID, limit, offset)
			if err != nil {
				httpx.Error(w, err)
				return
			}
			httpx.JSON(w, http.StatusOK, items)
		})
		r.Get("/conflicts", func(w http.ResponseWriter, r *http.Request) {
			userID, ok := currentUserID(w, r)
			if !ok {
				return
			}
			tripID, err := httpx.ID(r, "tripID")
			if err != nil {
				httpx.Error(w, err)
				return
			}
			limit, offset, err := httpx.Pagination(r)
			if err != nil {
				httpx.Error(w, err)
				return
			}
			items, err := s.Conflicts(r.Context(), userID, tripID, limit, offset)
			if err != nil {
				httpx.Error(w, err)
				return
			}
			httpx.JSON(w, http.StatusOK, items)
		})
		r.Get("/{activityID}", func(w http.ResponseWriter, r *http.Request) {
			userID, ok := currentUserID(w, r)
			if !ok {
				return
			}
			tripID, id, err := ids(r)
			if err != nil {
				httpx.Error(w, err)
				return
			}
			a, err := s.Get(r.Context(), userID, tripID, id)
			if err != nil {
				httpx.Error(w, err)
				return
			}
			httpx.JSON(w, http.StatusOK, a)
		})
		r.Put("/{activityID}", func(w http.ResponseWriter, r *http.Request) {
			userID, ok := currentUserID(w, r)
			if !ok {
				return
			}
			tripID, id, err := ids(r)
			if err != nil {
				httpx.Error(w, err)
				return
			}
			var in Input
			if err := httpx.Decode(w, r, &in); err != nil {
				httpx.Error(w, err)
				return
			}
			a, err := s.Update(r.Context(), userID, tripID, id, in)
			if err != nil {
				httpx.Error(w, err)
				return
			}
			httpx.JSON(w, http.StatusOK, a)
		})
		r.Delete("/{activityID}", func(w http.ResponseWriter, r *http.Request) {
			userID, ok := currentUserID(w, r)
			if !ok {
				return
			}
			tripID, id, err := ids(r)
			if err != nil {
				httpx.Error(w, err)
				return
			}
			if err := s.Delete(r.Context(), userID, tripID, id); err != nil {
				httpx.Error(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
	})
}

func ids(r *http.Request) (int64, int64, error) {
	tripID, err := httpx.ID(r, "tripID")
	if err != nil {
		return 0, 0, err
	}
	id, err := httpx.ID(r, "activityID")
	return tripID, id, err
}
