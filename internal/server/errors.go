package server

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Error types.
var (
	ErrAccessDenied   = errors.New("access denied")
	ErrNotFound       = errors.New("not found")
	ErrInvalidRequest = errors.New("invalid request")
	ErrRateLimit      = errors.New("rate limit exceeded")
	ErrTimeout        = errors.New("request timeout")
)

// AccessError represents an error due to access denial.
type AccessError struct {
	Message    string
	StatusCode int
}

func (e *AccessError) Error() string {
	return fmt.Sprintf("access error (%d): %s", e.StatusCode, e.Message)
}

// IsAccessError checks if an error is an access error.
func IsAccessError(err error) bool {
	var accessErr *AccessError
	if errors.As(err, &accessErr) {
		return accessErr.StatusCode == http.StatusForbidden ||
			accessErr.StatusCode == http.StatusNotFound ||
			accessErr.StatusCode == http.StatusUnauthorized
	}
	if errors.Is(err, ErrAccessDenied) || errors.Is(err, ErrNotFound) {
		return true
	}
	// Check for GraphQL permission errors from the prx library.
	errStr := err.Error()
	return strings.Contains(errStr, "Resource not accessible by integration") ||
		strings.Contains(errStr, "Not Found") ||
		strings.Contains(errStr, "API rate limit exceeded")
}

// NewAccessError creates a new access error.
func NewAccessError(statusCode int, message string) error {
	return &AccessError{
		Message:    message,
		StatusCode: statusCode,
	}
}
