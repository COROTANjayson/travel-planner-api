package auth

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"travel-planner/travel-planner-api/internal/httpx"
)

func Register(r chi.Router) {
	r.Get("/me", getMe)
}

func getMe(w http.ResponseWriter, r *http.Request) {
	user, ok := CurrentUser(r.Context())
	if !ok {
		httpx.JSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	httpx.JSON(w, http.StatusOK, user)
}
