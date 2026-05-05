// Package domain holds the entities and invariants of VisionLoop's core
// model. It depends on no other internal package (ADR-0001).
package domain

import "errors"

// Errors returned by the domain layer. Handlers translate them to HTTP.
var (
	ErrNotFound      = errors.New("resource not found")
	ErrUnauthorized  = errors.New("unauthorized")
	ErrForbidden     = errors.New("forbidden")
	ErrConflict      = errors.New("conflict")
	ErrInvalidInput  = errors.New("invalid input")
	ErrTooLarge      = errors.New("payload too large")
	ErrUnsupportedCT = errors.New("unsupported content type")
)
