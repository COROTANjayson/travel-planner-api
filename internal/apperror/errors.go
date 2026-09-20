package apperror

import "errors"

var (
	ErrInvalid  = errors.New("invalid input")
	ErrNotFound = errors.New("resource not found")
)
