package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"travel-planner/travel-planner-api/internal/apperror"
)

func JSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func Error(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, apperror.ErrInvalid):
		JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, apperror.ErrNotFound):
		JSON(w, http.StatusNotFound, map[string]string{"error": "resource not found"})
	default:
		// Do not return or log raw database errors, which can contain user data.
		slog.Error("request failed", "error_type", fmt.Sprintf("%T", err))
		JSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
}

func Decode(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("%w: body must be a valid JSON object with supported fields (maximum 1 MiB)", apperror.ErrInvalid)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("%w: body must contain exactly one JSON object", apperror.ErrInvalid)
	}
	return nil
}

func ID(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("%w: %s must be a positive integer", apperror.ErrInvalid, name)
	}
	return id, nil
}

func Pagination(r *http.Request) (int, int, error) {
	limit, offset := 50, 0
	for name, target := range map[string]*int{"limit": &limit, "offset": &offset} {
		if value := r.URL.Query().Get(name); value != "" {
			n, err := strconv.Atoi(value)
			if err != nil {
				return 0, 0, fmt.Errorf("%w: invalid pagination", apperror.ErrInvalid)
			}
			*target = n
		}
	}
	if limit < 1 || limit > 100 || offset < 0 {
		return 0, 0, fmt.Errorf("%w: limit must be 1–100 and offset must be nonnegative", apperror.ErrInvalid)
	}
	return limit, offset, nil
}
