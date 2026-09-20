package trips

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"travel-planner/travel-planner-api/internal/httpx"
)

func Register(r chi.Router, s *Service) {
	r.Post("/trips", func(w http.ResponseWriter, r *http.Request) {
		var in Input
		if err := httpx.Decode(w, r, &in); err != nil {
			httpx.Error(w, err)
			return
		}
		t, err := s.Create(r.Context(), in)
		if err != nil {
			httpx.Error(w, err)
			return
		}
		w.Header().Set("Location", "/api/v1/trips/"+strconv.FormatInt(t.ID, 10))
		httpx.JSON(w, http.StatusCreated, t)
	})
	r.Get("/trips", func(w http.ResponseWriter, r *http.Request) {
		limit, offset, err := httpx.Pagination(r)
		if err != nil {
			httpx.Error(w, err)
			return
		}
		items, err := s.List(r.Context(), limit, offset)
		if err != nil {
			httpx.Error(w, err)
			return
		}
		httpx.JSON(w, http.StatusOK, items)
	})
	r.Get("/trips/{tripID}", func(w http.ResponseWriter, r *http.Request) {
		id, err := httpx.ID(r, "tripID")
		if err != nil {
			httpx.Error(w, err)
			return
		}
		t, err := s.Get(r.Context(), id)
		if err != nil {
			httpx.Error(w, err)
			return
		}
		httpx.JSON(w, http.StatusOK, t)
	})
	r.Put("/trips/{tripID}", func(w http.ResponseWriter, r *http.Request) {
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
		t, err := s.Update(r.Context(), id, in)
		if err != nil {
			httpx.Error(w, err)
			return
		}
		httpx.JSON(w, http.StatusOK, t)
	})
	r.Delete("/trips/{tripID}", func(w http.ResponseWriter, r *http.Request) {
		id, err := httpx.ID(r, "tripID")
		if err != nil {
			httpx.Error(w, err)
			return
		}
		if err := s.Delete(r.Context(), id); err != nil {
			httpx.Error(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
