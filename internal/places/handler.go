package places

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"travel-planner/travel-planner-api/internal/auth"
	"travel-planner/travel-planner-api/internal/httpx"
)

func Register(r chi.Router, s *Service) {
	r.Route("/places", func(r chi.Router) {
		r.Get("/search", func(w http.ResponseWriter, r *http.Request) {
			user, ok := auth.CurrentUser(r.Context())
			if !ok {
				httpx.JSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}
			results, err := s.Search(r.Context(), user.ID, r.URL.Query().Get("q"))
			if err != nil {
				httpx.Error(w, err)
				return
			}
			httpx.JSON(w, http.StatusOK, results)
		})
		r.Post("/resolve", func(w http.ResponseWriter, r *http.Request) {
			if _, ok := auth.CurrentUser(r.Context()); !ok {
				httpx.JSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}
			var input struct {
				ProviderPlaceID string `json:"provider_place_id"`
			}
			if err := httpx.Decode(w, r, &input); err != nil {
				httpx.Error(w, err)
				return
			}
			place, err := s.Resolve(r.Context(), input.ProviderPlaceID)
			if err != nil {
				httpx.Error(w, err)
				return
			}
			httpx.JSON(w, http.StatusOK, place)
		})
		r.Get("/{placeID}", func(w http.ResponseWriter, r *http.Request) {
			if _, ok := auth.CurrentUser(r.Context()); !ok {
				httpx.JSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}
			id, err := httpx.ID(r, "placeID")
			if err != nil {
				httpx.Error(w, err)
				return
			}
			place, err := s.Get(r.Context(), id)
			if err != nil {
				httpx.Error(w, err)
				return
			}
			httpx.JSON(w, http.StatusOK, place)
		})
	})
}
