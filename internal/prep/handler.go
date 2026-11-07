package prep

import (
	"context"
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

func ids(r *http.Request) (int64, int64, error) {
	tripID, err := httpx.ID(r, "tripID")
	if err != nil {
		return 0, 0, err
	}
	id, err := httpx.ID(r, "taskID")
	return tripID, id, err
}

// action serves the routes that take no body and answer 204.
func action(do func(context.Context, int64, int64, int64) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := currentUserID(w, r)
		if !ok {
			return
		}
		tripID, id, err := ids(r)
		if err != nil {
			httpx.Error(w, err)
			return
		}
		if err := do(r.Context(), userID, tripID, id); err != nil {
			httpx.Error(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func Register(r chi.Router, s *Service) {
	r.Route("/trips/{tripID}/prep-tasks", func(r chi.Router) {
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
			tasks, err := s.List(r.Context(), userID, tripID)
			if err != nil {
				httpx.Error(w, err)
				return
			}
			httpx.JSON(w, http.StatusOK, tasks)
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
			var in CreateInput
			if err := httpx.Decode(w, r, &in); err != nil {
				httpx.Error(w, err)
				return
			}
			task, err := s.Create(r.Context(), userID, tripID, in)
			if err != nil {
				httpx.Error(w, err)
				return
			}
			w.Header().Set("Location", fmt.Sprintf("/api/v1/trips/%d/prep-tasks/%d", tripID, task.ID))
			httpx.JSON(w, http.StatusCreated, task)
		})
		r.Put("/{taskID}", func(w http.ResponseWriter, r *http.Request) {
			userID, ok := currentUserID(w, r)
			if !ok {
				return
			}
			tripID, id, err := ids(r)
			if err != nil {
				httpx.Error(w, err)
				return
			}
			var in UpdateInput
			if err := httpx.Decode(w, r, &in); err != nil {
				httpx.Error(w, err)
				return
			}
			task, err := s.Update(r.Context(), userID, tripID, id, in)
			if err != nil {
				httpx.Error(w, err)
				return
			}
			httpx.JSON(w, http.StatusOK, task)
		})
		r.Delete("/{taskID}", action(s.Delete))
		r.Put("/{taskID}/assignee", func(w http.ResponseWriter, r *http.Request) {
			userID, ok := currentUserID(w, r)
			if !ok {
				return
			}
			tripID, id, err := ids(r)
			if err != nil {
				httpx.Error(w, err)
				return
			}
			var in AssigneeInput
			if err := httpx.Decode(w, r, &in); err != nil {
				httpx.Error(w, err)
				return
			}
			if err := s.Assign(r.Context(), userID, tripID, id, in); err != nil {
				httpx.Error(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
		r.Put("/{taskID}/claim", action(s.Claim))
		r.Delete("/{taskID}/claim", action(s.Drop))
		r.Put("/{taskID}/completion", action(s.Complete))
		r.Delete("/{taskID}/completion", action(s.Reopen))
	})
}
