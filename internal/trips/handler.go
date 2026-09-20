package trips

import (
	"net/http"
	"strconv"

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
	r.Post("/trips", func(w http.ResponseWriter, r *http.Request) {
		userID, ok := currentUserID(w, r)
		if !ok {
			return
		}
		var in Input
		if err := httpx.Decode(w, r, &in); err != nil {
			httpx.Error(w, err)
			return
		}
		t, err := s.Create(r.Context(), userID, in)
		if err != nil {
			httpx.Error(w, err)
			return
		}
		w.Header().Set("Location", "/api/v1/trips/"+strconv.FormatInt(t.ID, 10))
		httpx.JSON(w, http.StatusCreated, t)
	})
	r.Get("/trips", func(w http.ResponseWriter, r *http.Request) {
		userID, ok := currentUserID(w, r)
		if !ok {
			return
		}
		limit, offset, err := httpx.Pagination(r)
		if err != nil {
			httpx.Error(w, err)
			return
		}
		items, err := s.List(r.Context(), userID, limit, offset)
		if err != nil {
			httpx.Error(w, err)
			return
		}
		httpx.JSON(w, http.StatusOK, items)
	})
	r.Get("/trips/{tripID}", func(w http.ResponseWriter, r *http.Request) {
		userID, ok := currentUserID(w, r)
		if !ok {
			return
		}
		id, err := httpx.ID(r, "tripID")
		if err != nil {
			httpx.Error(w, err)
			return
		}
		t, err := s.Get(r.Context(), userID, id)
		if err != nil {
			httpx.Error(w, err)
			return
		}
		httpx.JSON(w, http.StatusOK, t)
	})
	r.Put("/trips/{tripID}", func(w http.ResponseWriter, r *http.Request) {
		userID, ok := currentUserID(w, r)
		if !ok {
			return
		}
		id, err := httpx.ID(r, "tripID")
		if err != nil {
			httpx.Error(w, err)
			return
		}
		var in Input
		if err := httpx.Decode(w, r, &in); err != nil {
			httpx.Error(w, err)
			return
		}
		t, err := s.Update(r.Context(), userID, id, in)
		if err != nil {
			httpx.Error(w, err)
			return
		}
		httpx.JSON(w, http.StatusOK, t)
	})
	r.Delete("/trips/{tripID}", func(w http.ResponseWriter, r *http.Request) {
		userID, ok := currentUserID(w, r)
		if !ok {
			return
		}
		id, err := httpx.ID(r, "tripID")
		if err != nil {
			httpx.Error(w, err)
			return
		}
		if err := s.Delete(r.Context(), userID, id); err != nil {
			httpx.Error(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
