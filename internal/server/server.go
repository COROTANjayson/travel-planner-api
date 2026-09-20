package server

import (
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"travel-planner/travel-planner-api/internal/httpx"
	"travel-planner/travel-planner-api/internal/itinerary"
	"travel-planner/travel-planner-api/internal/trips"
)

func New(port string, tripService *trips.Service, itineraryService *itinerary.Service) *http.Server {
	return &http.Server{
		Addr:              net.JoinHostPort("127.0.0.1", port),
		Handler:           Router(tripService, itineraryService),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

func Router(tripService *trips.Service, itineraryService *itinerary.Service) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, requestLog, recoverJSON, middleware.Timeout(25*time.Second))
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Route("/api/v1", func(r chi.Router) {
		trips.Register(r, tripService)
		itinerary.Register(r, itineraryService)
	})
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, http.StatusNotFound, map[string]string{"error": "route not found"})
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	})
	return r
}

func requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		slog.Info("http request", "request_id", middleware.GetReqID(r.Context()), "method", r.Method, "path", r.URL.Path, "status", ww.Status(), "duration_ms", time.Since(start).Milliseconds())
	})
}

func recoverJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				if err == http.ErrAbortHandler {
					panic(err)
				}
				slog.Error("request panic", "request_id", middleware.GetReqID(r.Context()))
				httpx.JSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}
