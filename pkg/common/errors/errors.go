// Package errors defines application-level error types and error handling utilities
package errors

import (
	"errors"
	"fmt"
	"net/http"
)

// AppError represents an application error
type AppError struct {
	Code    int    `json:"code"`    // HTTP status code
	Message string `json:"message"` // User-friendly error message
	Err     error  `json:"-"`       // Original error (not serialized)
}

// Error implements the error interface
func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

// Unwrap supports errors.Unwrap
func (e *AppError) Unwrap() error {
	return e.Err
}

// HTTPCode returns the HTTP status code
func (e *AppError) HTTPCode() int {
	if e.Code == 0 {
		return http.StatusInternalServerError
	}
	return e.Code
}

// New creates a new application error
func New(code int, message string) *AppError {
	return &AppError{
		Code:    code,
		Message: message,
	}
}

// Wrap wraps an original error
func Wrap(err error, code int, message string) *AppError {
	return &AppError{
		Code:    code,
		Message: message,
		Err:     err,
	}
}

// WrapWithMessage wraps using the original error message
func WrapWithMessage(err error, code int) *AppError {
	if err == nil {
		return nil
	}
	return &AppError{
		Code:    code,
		Message: err.Error(),
		Err:     err,
	}
}

// Is checks if the error is of the specified type
func Is(err, target error) bool {
	return errors.Is(err, target)
}

// As attempts to convert the error to the specified type
func As(err error, target any) bool {
	return errors.As(err, target)
}

// Predefined errors
var (
	// Client errors (4xx)
	ErrBadRequest          = New(http.StatusBadRequest, "bad request")
	ErrUnauthorized        = New(http.StatusUnauthorized, "unauthorized")
	ErrForbidden           = New(http.StatusForbidden, "forbidden")
	ErrNotFound            = New(http.StatusNotFound, "not found")
	ErrMethodNotAllowed    = New(http.StatusMethodNotAllowed, "method not allowed")
	ErrConflict            = New(http.StatusConflict, "conflict")
	ErrUnprocessableEntity = New(http.StatusUnprocessableEntity, "unprocessable entity")
	ErrTooManyRequests     = New(http.StatusTooManyRequests, "too many requests")

	// Server errors (5xx)
	ErrInternal           = New(http.StatusInternalServerError, "internal server error")
	ErrNotImplemented     = New(http.StatusNotImplemented, "not implemented")
	ErrServiceUnavailable = New(http.StatusServiceUnavailable, "service unavailable")
	ErrGatewayTimeout     = New(http.StatusGatewayTimeout, "gateway timeout")
)

// Business errors
var (
	ErrTaskNotFound     = New(http.StatusNotFound, "task not found")
	ErrWorkerNotFound   = New(http.StatusNotFound, "worker not found")
	ErrEndpointNotFound = New(http.StatusNotFound, "endpoint not found")
	ErrSpecNotFound     = New(http.StatusNotFound, "spec not found")

	ErrTaskAlreadyCompleted = New(http.StatusConflict, "task already completed")
	ErrTaskAlreadyCancelled = New(http.StatusConflict, "task already cancelled")
	ErrWorkerOffline        = New(http.StatusServiceUnavailable, "worker offline")
	ErrEndpointNotReady     = New(http.StatusServiceUnavailable, "endpoint not ready")

	ErrInvalidTaskID   = New(http.StatusBadRequest, "invalid task id")
	ErrInvalidWorkerID = New(http.StatusBadRequest, "invalid worker id")
	ErrInvalidEndpoint = New(http.StatusBadRequest, "invalid endpoint")
	ErrInvalidInput    = New(http.StatusBadRequest, "invalid input")
	ErrMissingRequired = New(http.StatusBadRequest, "missing required field")
)

// NotFound creates a 404 error
func NotFound(resource string) *AppError {
	return New(http.StatusNotFound, fmt.Sprintf("%s not found", resource))
}

// BadRequest creates a 400 error
func BadRequest(message string) *AppError {
	return New(http.StatusBadRequest, message)
}

// Internal creates a 500 error
func Internal(message string) *AppError {
	return New(http.StatusInternalServerError, message)
}

// Conflict creates a 409 error
func Conflict(message string) *AppError {
	return New(http.StatusConflict, message)
}

// ServiceUnavailable creates a 503 error
func ServiceUnavailable(message string) *AppError {
	return New(http.StatusServiceUnavailable, message)
}
