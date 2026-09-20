package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"travel-planner/travel-planner-api/internal/apperror"
	"travel-planner/travel-planner-api/internal/trips"
)

type failingTrips struct {
	trips.Store
	err error
}

func (s failingTrips) Get(context.Context, int64) (trips.Trip, error) { return trips.Trip{}, s.err }

func noAuth(next http.Handler) http.Handler { return next }

func TestHealthAndRequestValidation(t *testing.T) {
	handler := Router(nil, nil, noAuth)
	for index, tc := range []struct {
		method, path, body string
		status             int
	}{
		{"GET", "/health", "", 200},
		{"GET", "/unknown", "", 404},
		{"POST", "/health", "", 405},
		{"GET", "/api/v1/trips/not-an-id", "", 400},
		{"GET", "/api/v1/trips/0", "", 400},
		{"GET", "/api/v1/trips?limit=101", "", 400},
		{"GET", "/api/v1/trips?offset=-1", "", 400},
		{"POST", "/api/v1/trips", "{", 400},
		{"POST", "/api/v1/trips", "{\"unknown\":true}", 400},
		{"POST", "/api/v1/trips", "{} {}", 400},
		{"POST", "/api/v1/trips", "null", 400},
		{"POST", "/api/v1/trips", "{\"name\":\"" + strings.Repeat("a", 1<<20) + "\"}", 400},
		{"POST", "/api/v1/trips/1/activities", "{\"starts_at\":\"2026-10-01T09:00:00\"}", 400},
	} {
		t.Run(fmt.Sprintf("%d %s %s", index, tc.method, tc.path), func(t *testing.T) {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
			if w.Code != tc.status {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			if !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
				t.Fatal("response is not JSON")
			}
			if tc.path == "/health" && tc.method == "GET" && w.Body.String() != "{\"status\":\"ok\"}\n" {
				t.Fatal(w.Body.String())
			}
		})
	}
}

func TestRepositoryErrors(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
	}{
		{apperror.ErrNotFound, http.StatusNotFound},
		{errors.New("secret database detail"), http.StatusInternalServerError},
	} {
		handler := Router(trips.NewService(failingTrips{err: tc.err}), nil, noAuth)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/trips/1", nil))
		if w.Code != tc.status {
			t.Fatalf("status %d", w.Code)
		}
		if strings.Contains(w.Body.String(), "secret") {
			t.Fatal("database detail leaked")
		}
	}
}
