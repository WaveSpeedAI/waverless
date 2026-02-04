// Package validator provides unified parameter validation functionality
package validator

import (
	"fmt"
	"regexp"
	"strings"
)

// ValidationError represents a validation error
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ValidationErrors represents multiple validation errors
type ValidationErrors []*ValidationError

func (e ValidationErrors) Error() string {
	if len(e) == 0 {
		return ""
	}
	var msgs []string
	for _, err := range e {
		msgs = append(msgs, err.Error())
	}
	return strings.Join(msgs, "; ")
}

// HasErrors checks if there are any errors
func (e ValidationErrors) HasErrors() bool {
	return len(e) > 0
}

// Validator is a parameter validator
type Validator struct {
	errors ValidationErrors
}

// New creates a new validator
func New() *Validator {
	return &Validator{
		errors: make(ValidationErrors, 0),
	}
}

// Required validates required fields
func (v *Validator) Required(field, value string) *Validator {
	if strings.TrimSpace(value) == "" {
		v.errors = append(v.errors, &ValidationError{
			Field:   field,
			Message: "is required",
		})
	}
	return v
}

// MinLength validates minimum length
func (v *Validator) MinLength(field, value string, min int) *Validator {
	if len(value) < min {
		v.errors = append(v.errors, &ValidationError{
			Field:   field,
			Message: fmt.Sprintf("must be at least %d characters", min),
		})
	}
	return v
}

// MaxLength validates maximum length
func (v *Validator) MaxLength(field, value string, max int) *Validator {
	if len(value) > max {
		v.errors = append(v.errors, &ValidationError{
			Field:   field,
			Message: fmt.Sprintf("must be at most %d characters", max),
		})
	}
	return v
}

// Pattern validates against a regular expression
func (v *Validator) Pattern(field, value, pattern, message string) *Validator {
	if value == "" {
		return v // Empty value is validated by Required
	}
	matched, err := regexp.MatchString(pattern, value)
	if err != nil || !matched {
		v.errors = append(v.errors, &ValidationError{
			Field:   field,
			Message: message,
		})
	}
	return v
}

// Range validates numeric range
func (v *Validator) Range(field string, value, min, max int) *Validator {
	if value < min || value > max {
		v.errors = append(v.errors, &ValidationError{
			Field:   field,
			Message: fmt.Sprintf("must be between %d and %d", min, max),
		})
	}
	return v
}

// Min validates minimum value
func (v *Validator) Min(field string, value, min int) *Validator {
	if value < min {
		v.errors = append(v.errors, &ValidationError{
			Field:   field,
			Message: fmt.Sprintf("must be at least %d", min),
		})
	}
	return v
}

// Max validates maximum value
func (v *Validator) Max(field string, value, max int) *Validator {
	if value > max {
		v.errors = append(v.errors, &ValidationError{
			Field:   field,
			Message: fmt.Sprintf("must be at most %d", max),
		})
	}
	return v
}

// In validates if value is in allowed list
func (v *Validator) In(field, value string, allowed []string) *Validator {
	if value == "" {
		return v // Empty value is validated by Required
	}
	for _, a := range allowed {
		if value == a {
			return v
		}
	}
	v.errors = append(v.errors, &ValidationError{
		Field:   field,
		Message: fmt.Sprintf("must be one of: %s", strings.Join(allowed, ", ")),
	})
	return v
}

// Custom performs custom validation
func (v *Validator) Custom(field string, valid bool, message string) *Validator {
	if !valid {
		v.errors = append(v.errors, &ValidationError{
			Field:   field,
			Message: message,
		})
	}
	return v
}

// Errors returns all errors
func (v *Validator) Errors() ValidationErrors {
	return v.errors
}

// HasErrors checks if there are any errors
func (v *Validator) HasErrors() bool {
	return len(v.errors) > 0
}

// Error returns the first error
func (v *Validator) Error() error {
	if len(v.errors) == 0 {
		return nil
	}
	return v.errors[0]
}

// AllErrors returns all errors as a single error
func (v *Validator) AllErrors() error {
	if len(v.errors) == 0 {
		return nil
	}
	return v.errors
}

// Common regular expressions
const (
	// EndpointNamePattern - Endpoint name format: lowercase letters, numbers, hyphens, starting with a letter
	EndpointNamePattern = `^[a-z][a-z0-9-]*[a-z0-9]$`

	// ImageRefPattern - Image reference format
	ImageRefPattern = `^[a-zA-Z0-9][a-zA-Z0-9._/-]*[a-zA-Z0-9](:[a-zA-Z0-9._-]+)?(@sha256:[a-f0-9]{64})?$`

	// UUIDPattern - UUID format
	UUIDPattern = `^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`
)

// ValidateEndpointName validates endpoint name
func ValidateEndpointName(name string) error {
	v := New().
		Required("name", name).
		MinLength("name", name, 3).
		MaxLength("name", name, 63).
		Pattern("name", name, EndpointNamePattern, "must start with a letter, contain only lowercase letters, numbers, and hyphens")
	return v.AllErrors()
}

// ValidateImageRef validates image reference
func ValidateImageRef(image string) error {
	v := New().
		Required("image", image).
		Pattern("image", image, ImageRefPattern, "invalid image reference format")
	return v.AllErrors()
}

// ValidateReplicas validates replica count
func ValidateReplicas(replicas int) error {
	v := New().
		Range("replicas", replicas, 0, 100)
	return v.AllErrors()
}
