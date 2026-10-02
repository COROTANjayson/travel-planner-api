package apperror

import "errors"

var (
	ErrInvalid     = errors.New("invalid input")
	ErrNotFound    = errors.New("resource not found")
	ErrForbidden   = errors.New("forbidden")
	ErrConflict    = errors.New("conflict")
	ErrRateLimited = errors.New("rate limited")
	ErrUnavailable = errors.New("provider unavailable")
)
