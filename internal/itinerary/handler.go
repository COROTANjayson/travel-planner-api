package itinerary

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"travel-planner/travel-planner-api/internal/apperror"
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
		r.Get("/overlaps", func(w http.ResponseWriter, r *http.Request) {
			userID, ok := currentUserID(w, r)
			if !ok {
				return
			}
			tripID, err := httpx.ID(r, "tripID")
			if err != nil {
				httpx.Error(w, err)
				return
			}
			start, e1 := time.Parse(time.RFC3339Nano, r.URL.Query().Get("starts_at"))
			end, e2 := time.Parse(time.RFC3339Nano, r.URL.Query().Get("ends_at"))
			excludeID := int64(0)
			if value := r.URL.Query().Get("exclude_id"); value != "" {
				excludeID, err = strconv.ParseInt(value, 10, 64)
			}
			if e1 != nil || e2 != nil || err != nil || excludeID < 0 {
				httpx.Error(w, fmt.Errorf("invalid overlap query: %w", apperror.ErrInvalid))
				return
			}
			items, err := s.Overlaps(r.Context(), userID, tripID, start, end, excludeID)
			if err != nil {
				httpx.Error(w, err)
				return
			}
			httpx.JSON(w, http.StatusOK, items)
		})
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
			var from, before *time.Time
			fromText, beforeText := r.URL.Query().Get("start_from"), r.URL.Query().Get("start_before")
			if fromText != "" || beforeText != "" {
				start, e1 := time.Parse(time.RFC3339Nano, fromText)
				end, e2 := time.Parse(time.RFC3339Nano, beforeText)
				if e1 != nil || e2 != nil {
					httpx.Error(w, fmt.Errorf("%w: invalid activity date range", apperror.ErrInvalid))
					return
				}
				from, before = &start, &end
			}
			items, err := s.List(r.Context(), userID, tripID, limit, offset, from, before)
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
