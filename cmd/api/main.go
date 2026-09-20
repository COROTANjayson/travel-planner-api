package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"travel-planner/travel-planner-api/internal/auth"
	"travel-planner/travel-planner-api/internal/config"
	"travel-planner/travel-planner-api/internal/database"
	"travel-planner/travel-planner-api/internal/itinerary"
	"travel-planner/travel-planner-api/internal/server"
	"travel-planner/travel-planner-api/internal/trips"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(); err != nil {
		slog.Error("API stopped", "error", err.Error())
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pool, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	authenticator, err := auth.New(cfg.Auth0IssuerURL, cfg.Auth0Audience, auth.NewRepository(pool))
	if err != nil {
		return err
	}
	api := server.New(cfg.Port, trips.NewService(trips.NewRepository(pool)), itinerary.NewService(itinerary.NewRepository(pool)), authenticator.Middleware)
	errs := make(chan error, 1)
	go func() {
		slog.Info("starting local API", "address", api.Addr)
		errs <- api.ListenAndServe()
	}()
	select {
	case err := <-errs:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		stop()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := api.Shutdown(shutdownCtx); err != nil {
			_ = api.Close()
			return err
		}
		slog.Info("API shutdown complete")
		return nil
	}
}
