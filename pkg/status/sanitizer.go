// Package status provides status sanitization functionality for the Waverless platform.
// It converts provider-specific error messages to user-friendly messages and removes
// sensitive information from error messages.
package status

import (
	"regexp"
	"strings"

	"waverless/pkg/interfaces"
)

// StatusSanitizer converts provider-specific error messages to user-friendly messages
// and removes sensitive information from error messages.
// It implements the sanitization logic for Requirements 4.1, 4.2, 4.3.
type StatusSanitizer struct {
	// errorMappings maps failure types to provider-specific error mappings
	errorMappings map[interfaces.FailureType]map[string]SanitizedError
	// sensitivePatterns contains regex patterns for sensitive information
	sensitivePatterns []*sensitivePattern
}

// SanitizedError represents a user-friendly error message with suggestions.
type SanitizedError struct {
	// UserMessage is the user-friendly error message
	UserMessage string `json:"userMessage"`
	// Suggestion provides actionable advice for the user
	Suggestion string `json:"suggestion"`
	// ErrorCode is a unique code for this error type
	ErrorCode string `json:"errorCode"`
}

// sensitivePattern represents a pattern for sensitive information
type sensitivePattern struct {
	pattern     *regexp.Regexp
	replacement string
	description string
}

// ImagePullErrorMappings contains default mappings for IMAGE_PULL_FAILED type.
// These mappings cover both K8s and Novita specific error reasons.
// Validates: Requirements 4.1, 4.2
var ImagePullErrorMappings = map[string]SanitizedError{
	// K8s specific errors
	"ImagePullBackOff": {
		UserMessage: "Image pull failed, system is retrying",
		Suggestion:  "Please check if the image name is correct and if you have access permissions",
		ErrorCode:   "IMG_PULL_BACKOFF",
	},
	"ErrImagePull": {
		UserMessage: "Failed to pull image",
		Suggestion:  "Please verify the image exists and is accessible. For private images, configure access credentials",
		ErrorCode:   "IMG_PULL_ERROR",
	},
	"InvalidImageName": {
		UserMessage: "Invalid image name format",
		Suggestion:  "Please check the image name format, ensure it follows registry/repository:tag format",
		ErrorCode:   "IMG_INVALID_NAME",
	},
	"ImageInspectError": {
		UserMessage: "Failed to inspect image",
		Suggestion:  "Please verify the image exists and has correct format",
		ErrorCode:   "IMG_INSPECT_ERROR",
	},
	"RegistryUnavailable": {
		UserMessage: "Registry unavailable",
		Suggestion:  "Please try again later or check if the registry address is correct",
		ErrorCode:   "IMG_REGISTRY_UNAVAILABLE",
	},

	// Novita specific errors
	"image_pull_failed": {
		UserMessage: "Image pull failed",
		Suggestion:  "Please check the image name and access permissions",
		ErrorCode:   "IMG_PULL_FAILED",
	},
	"image_not_found": {
		UserMessage: "Image not found",
		Suggestion:  "Please verify the image name is correct and the image has been pushed to the registry",
		ErrorCode:   "IMG_NOT_FOUND",
	},
	"image_auth_failed": {
		UserMessage: "Image authentication failed",
		Suggestion:  "Please check if the registry credentials are correct",
		ErrorCode:   "IMG_AUTH_FAILED",
	},

	// Generic fallback
	"default": {
		UserMessage: "Image loading failed",
		Suggestion:  "Please check if the image configuration is correct",
		ErrorCode:   "IMG_ERROR",
	},
}

// ContainerCrashErrorMappings contains default mappings for CONTAINER_CRASH type.
var ContainerCrashErrorMappings = map[string]SanitizedError{
	// K8s specific errors
	"CrashLoopBackOff": {
		UserMessage: "Container keeps crashing after startup",
		Suggestion:  "Please check if the container startup command and configuration are correct",
		ErrorCode:   "CONTAINER_CRASH_LOOP",
	},
	"Error": {
		UserMessage: "Container runtime error",
		Suggestion:  "Please check container logs for detailed error information",
		ErrorCode:   "CONTAINER_ERROR",
	},
	"OOMKilled": {
		UserMessage: "Container terminated due to out of memory",
		Suggestion:  "Please increase memory limit or optimize application memory usage",
		ErrorCode:   "CONTAINER_OOM",
	},
	"ContainerCannotRun": {
		UserMessage: "Container cannot start",
		Suggestion:  "Please check container configuration and startup command",
		ErrorCode:   "CONTAINER_CANNOT_RUN",
	},

	// Novita specific errors
	"container_crashed": {
		UserMessage: "Container crashed",
		Suggestion:  "Please check if the application is working properly",
		ErrorCode:   "CONTAINER_CRASHED",
	},
	"container_exit_error": {
		UserMessage: "Container exited abnormally",
		Suggestion:  "Please check container logs for detailed error information",
		ErrorCode:   "CONTAINER_EXIT_ERROR",
	},

	// Generic fallback
	"default": {
		UserMessage: "Container runtime failed",
		Suggestion:  "Please check container configuration and logs",
		ErrorCode:   "CONTAINER_FAIL",
	},
}

// ResourceLimitErrorMappings contains default mappings for RESOURCE_LIMIT type.
var ResourceLimitErrorMappings = map[string]SanitizedError{
	// K8s specific errors
	"OutOfMemory": {
		UserMessage: "Insufficient memory resources",
		Suggestion:  "Please increase memory configuration or select a larger spec",
		ErrorCode:   "RESOURCE_OOM",
	},
	"OutOfCpu": {
		UserMessage: "Insufficient CPU resources",
		Suggestion:  "Please increase CPU configuration or select a larger spec",
		ErrorCode:   "RESOURCE_CPU",
	},
	"OutOfGpu": {
		UserMessage: "Insufficient GPU resources",
		Suggestion:  "Please try again later or select a different GPU type",
		ErrorCode:   "RESOURCE_GPU",
	},
	"Unschedulable": {
		UserMessage: "Cannot schedule to available node",
		Suggestion:  "Please try again later, system is looking for available resources",
		ErrorCode:   "RESOURCE_UNSCHEDULABLE",
	},

	// Novita specific errors
	"insufficient_resources": {
		UserMessage: "Insufficient resources",
		Suggestion:  "Please try again later or select a different spec",
		ErrorCode:   "RESOURCE_INSUFFICIENT",
	},
	"gpu_unavailable": {
		UserMessage: "GPU resources temporarily unavailable",
		Suggestion:  "Please try again later or select a different GPU type",
		ErrorCode:   "RESOURCE_GPU_UNAVAILABLE",
	},

	// Generic fallback
	"default": {
		UserMessage: "Resource limit reached",
		Suggestion:  "Please check resource configuration or try again later",
		ErrorCode:   "RESOURCE_LIMIT",
	},
}

// TimeoutErrorMappings contains default mappings for TIMEOUT type.
var TimeoutErrorMappings = map[string]SanitizedError{
	"ImagePullTimeout": {
		UserMessage: "Image pull timeout",
		Suggestion:  "Please check image size and network connection, or try again later",
		ErrorCode:   "TIMEOUT_IMAGE_PULL",
	},
	"StartupTimeout": {
		UserMessage: "Container startup timeout",
		Suggestion:  "Please check if the container startup command is correct, or increase startup timeout",
		ErrorCode:   "TIMEOUT_STARTUP",
	},
	"HealthCheckTimeout": {
		UserMessage: "Health check timeout",
		Suggestion:  "Please check health check configuration and application response time",
		ErrorCode:   "TIMEOUT_HEALTH_CHECK",
	},

	// Generic fallback
	"default": {
		UserMessage: "Operation timeout",
		Suggestion:  "Please try again later",
		ErrorCode:   "TIMEOUT",
	},
}

// UnknownErrorMappings contains default mappings for UNKNOWN type.
var UnknownErrorMappings = map[string]SanitizedError{
	"default": {
		UserMessage: "Unknown error occurred",
		Suggestion:  "Please contact technical support for help",
		ErrorCode:   "UNKNOWN_ERROR",
	},
}

// NewStatusSanitizer creates a new StatusSanitizer with default error mappings.
func NewStatusSanitizer() *StatusSanitizer {
	s := &StatusSanitizer{
		errorMappings:     make(map[interfaces.FailureType]map[string]SanitizedError),
		sensitivePatterns: buildDefaultSensitivePatterns(),
	}

	// Initialize default error mappings
	s.errorMappings[interfaces.FailureTypeImagePull] = ImagePullErrorMappings
	s.errorMappings[interfaces.FailureTypeContainerCrash] = ContainerCrashErrorMappings
	s.errorMappings[interfaces.FailureTypeResourceLimit] = ResourceLimitErrorMappings
	s.errorMappings[interfaces.FailureTypeTimeout] = TimeoutErrorMappings
	s.errorMappings[interfaces.FailureTypeUnknown] = UnknownErrorMappings

	return s
}

// buildDefaultSensitivePatterns builds the default patterns for sensitive information.
// These patterns are used to identify and redact sensitive information from error messages.
// Validates: Requirement 4.3
func buildDefaultSensitivePatterns() []*sensitivePattern {
	return []*sensitivePattern{
		// Node names - typically in format: node-xxx, ip-xxx-xxx-xxx-xxx, gke-xxx-xxx
		{
			pattern:     regexp.MustCompile(`\b(?:node|ip|gke|aks|eks)[-_][a-zA-Z0-9][-a-zA-Z0-9_.]*\b`),
			replacement: "[node]",
			description: "node name",
		},
		// Kubernetes node names with specific patterns
		{
			pattern:     regexp.MustCompile(`\bnode/[a-zA-Z0-9][-a-zA-Z0-9_.]*\b`),
			replacement: "node/[redacted]",
			description: "k8s node reference",
		},

		// Namespace names - typically in format: namespace/xxx or ns: xxx
		{
			pattern:     regexp.MustCompile(`\bnamespace[/:]?\s*[a-zA-Z0-9][-a-zA-Z0-9_.]*\b`),
			replacement: "namespace/[redacted]",
			description: "namespace name",
		},
		{
			pattern:     regexp.MustCompile(`\bns[/:]?\s*[a-zA-Z0-9][-a-zA-Z0-9_.]*\b`),
			replacement: "ns/[redacted]",
			description: "namespace abbreviation",
		},

		// Internal IP addresses (IPv4)
		// Private IP ranges: 10.x.x.x, 172.16-31.x.x, 192.168.x.x
		{
			pattern:     regexp.MustCompile(`\b10\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`),
			replacement: "[internal-ip]",
			description: "10.x.x.x private IP",
		},
		{
			pattern:     regexp.MustCompile(`\b172\.(1[6-9]|2[0-9]|3[0-1])\.\d{1,3}\.\d{1,3}\b`),
			replacement: "[internal-ip]",
			description: "172.16-31.x.x private IP",
		},
		{
			pattern:     regexp.MustCompile(`\b192\.168\.\d{1,3}\.\d{1,3}\b`),
			replacement: "[internal-ip]",
			description: "192.168.x.x private IP",
		},

		// Pod names - typically in format: podname-xxxxx-xxxxx
		{
			pattern:     regexp.MustCompile(`\bpod/[a-zA-Z0-9][-a-zA-Z0-9_.]*\b`),
			replacement: "pod/[redacted]",
			description: "pod reference",
		},
		{
			pattern:     regexp.MustCompile(`\b[a-zA-Z0-9][-a-zA-Z0-9]*-[a-f0-9]{5,10}-[a-z0-9]{5}\b`),
			replacement: "[pod-name]",
			description: "pod name with hash",
		},

		// Secret names and references
		// Note: The secrets?/ pattern must come BEFORE the secret[/:] pattern
		// to avoid partial matches on "secrets/xxx"
		{
			pattern:     regexp.MustCompile(`\bsecrets/[a-zA-Z0-9][-a-zA-Z0-9_.]*\b`),
			replacement: "secret/[redacted]",
			description: "secrets path (plural)",
		},
		{
			pattern:     regexp.MustCompile(`\bsecret/[a-zA-Z0-9][-a-zA-Z0-9_.]*\b`),
			replacement: "secret/[redacted]",
			description: "secret path",
		},
		{
			pattern:     regexp.MustCompile(`\bsecret[:]?\s+[a-zA-Z0-9][-a-zA-Z0-9_.]*\b`),
			replacement: "secret/[redacted]",
			description: "secret reference with colon",
		},

		// ConfigMap names and references
		{
			pattern:     regexp.MustCompile(`\bconfigmap[/:]?\s*[a-zA-Z0-9][-a-zA-Z0-9_.]*\b`),
			replacement: "configmap/[redacted]",
			description: "configmap reference",
		},

		// Service account names
		{
			pattern:     regexp.MustCompile(`\bserviceaccount[/:]?\s*[a-zA-Z0-9][-a-zA-Z0-9_.]*\b`),
			replacement: "serviceaccount/[redacted]",
			description: "service account reference",
		},
		{
			pattern:     regexp.MustCompile(`\bsa[/:]?\s*[a-zA-Z0-9][-a-zA-Z0-9_.]*\b`),
			replacement: "sa/[redacted]",
			description: "service account abbreviation",
		},

		// Kubernetes API server URLs
		{
			pattern:     regexp.MustCompile(`https?://[a-zA-Z0-9][-a-zA-Z0-9_.]*:\d+/api[/a-zA-Z0-9]*`),
			replacement: "[api-server]",
			description: "API server URL",
		},

		// Container registry credentials in URLs
		{
			pattern:     regexp.MustCompile(`https?://[^:]+:[^@]+@[a-zA-Z0-9][-a-zA-Z0-9_.]*`),
			replacement: "[registry-url]",
			description: "registry URL with credentials",
		},

		// AWS account IDs (12 digits)
		{
			pattern:     regexp.MustCompile(`\b\d{12}\.dkr\.ecr\.[a-z0-9-]+\.amazonaws\.com\b`),
			replacement: "[aws-ecr-registry]",
			description: "AWS ECR registry",
		},

		// GCP project IDs in GCR URLs
		{
			pattern:     regexp.MustCompile(`\bgcr\.io/[a-zA-Z0-9][-a-zA-Z0-9_]*/`),
			replacement: "gcr.io/[project]/",
			description: "GCR project",
		},

		// Azure subscription IDs (UUID format)
		{
			pattern:     regexp.MustCompile(`\b[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}\b`),
			replacement: "[uuid]",
			description: "UUID (possibly subscription/resource ID)",
		},

		// Kubernetes resource UIDs
		{
			pattern:     regexp.MustCompile(`\buid[=:]\s*[a-f0-9-]{36}\b`),
			replacement: "uid=[redacted]",
			description: "resource UID",
		},
	}
}

// Sanitize converts a provider-specific error to a user-friendly message.
// It looks up the error mapping based on failure type and reason.
// If no specific mapping is found, it returns the default mapping for the failure type.
//
// Parameters:
//   - failureType: The type of failure (IMAGE_PULL_FAILED, CONTAINER_CRASH, etc.)
//   - reason: The provider-specific reason (e.g., "ImagePullBackOff", "ErrImagePull")
//   - message: The provider-specific message (used for additional context, may contain sensitive info)
//
// Returns:
//   - A SanitizedError with user-friendly message and suggestion
//
// Validates: Requirements 4.1, 4.2
func (s *StatusSanitizer) Sanitize(failureType interfaces.FailureType, reason, message string) *SanitizedError {
	// Get mappings for this failure type
	mappings, ok := s.errorMappings[failureType]
	if !ok {
		// Fall back to unknown error mappings
		mappings = s.errorMappings[interfaces.FailureTypeUnknown]
	}

	// Look up specific reason
	if sanitized, ok := mappings[reason]; ok {
		return &sanitized
	}

	// Try case-insensitive match
	reasonLower := strings.ToLower(reason)
	for key, sanitized := range mappings {
		if strings.ToLower(key) == reasonLower {
			return &sanitized
		}
	}

	// Try partial match - check if reason contains any known key
	for key, sanitized := range mappings {
		if key != "default" && strings.Contains(reasonLower, strings.ToLower(key)) {
			return &sanitized
		}
	}

	// Check if message contains any known patterns
	messageLower := strings.ToLower(message)
	for key, sanitized := range mappings {
		if key != "default" && strings.Contains(messageLower, strings.ToLower(key)) {
			return &sanitized
		}
	}

	// Return default for this failure type
	if defaultErr, ok := mappings["default"]; ok {
		return &defaultErr
	}

	// Ultimate fallback
	return &SanitizedError{
		UserMessage: "An error occurred",
		Suggestion:  "Please contact technical support",
		ErrorCode:   "ERROR",
	}
}

// SanitizeSensitiveInfo removes sensitive information from a message.
// It uses regex patterns to identify and redact:
// - Node names
// - Namespace names
// - Internal IP addresses
// - Pod names
// - Secret names
// - Service account names
// - API server URLs
// - Registry credentials
// - Cloud provider specific identifiers (AWS account IDs, GCP project IDs, Azure subscription IDs)
//
// Parameters:
//   - message: The original message that may contain sensitive information
//
// Returns:
//   - The sanitized message with sensitive information redacted
//
// Validates: Requirement 4.3
func (s *StatusSanitizer) SanitizeSensitiveInfo(message string) string {
	if message == "" {
		return message
	}

	result := message
	for _, sp := range s.sensitivePatterns {
		result = sp.pattern.ReplaceAllString(result, sp.replacement)
	}

	return result
}

// AddErrorMapping adds a custom error mapping for a specific failure type and reason.
// This allows extending the default mappings with custom error messages.
func (s *StatusSanitizer) AddErrorMapping(failureType interfaces.FailureType, reason string, sanitized SanitizedError) {
	if _, ok := s.errorMappings[failureType]; !ok {
		s.errorMappings[failureType] = make(map[string]SanitizedError)
	}
	s.errorMappings[failureType][reason] = sanitized
}

// AddSensitivePattern adds a custom sensitive pattern for redaction.
// This allows extending the default patterns with custom patterns.
func (s *StatusSanitizer) AddSensitivePattern(pattern *regexp.Regexp, replacement, description string) {
	s.sensitivePatterns = append(s.sensitivePatterns, &sensitivePattern{
		pattern:     pattern,
		replacement: replacement,
		description: description,
	})
}

// GetErrorMapping returns the error mapping for a specific failure type and reason.
// Returns nil if no mapping is found.
func (s *StatusSanitizer) GetErrorMapping(failureType interfaces.FailureType, reason string) *SanitizedError {
	if mappings, ok := s.errorMappings[failureType]; ok {
		if sanitized, ok := mappings[reason]; ok {
			return &sanitized
		}
	}
	return nil
}

// SanitizeWorkerFailureInfo sanitizes a WorkerFailureInfo and returns a sanitized version.
// This is a convenience method that combines Sanitize and SanitizeSensitiveInfo.
func (s *StatusSanitizer) SanitizeWorkerFailureInfo(info *interfaces.WorkerFailureInfo) *interfaces.WorkerFailureInfo {
	if info == nil {
		return nil
	}

	sanitized := s.Sanitize(info.Type, info.Reason, info.Message)

	return &interfaces.WorkerFailureInfo{
		Type:         info.Type,
		Reason:       info.Reason,
		Message:      s.SanitizeSensitiveInfo(info.Message),
		SanitizedMsg: sanitized.UserMessage + ". " + sanitized.Suggestion,
		OccurredAt:   info.OccurredAt,
	}
}
