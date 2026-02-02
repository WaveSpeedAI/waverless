package status

import (
	"regexp"
	"testing"

	"waverless/pkg/interfaces"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewStatusSanitizer tests the StatusSanitizer constructor.
func TestNewStatusSanitizer(t *testing.T) {
	sanitizer := NewStatusSanitizer()
	require.NotNil(t, sanitizer)
	assert.NotNil(t, sanitizer.errorMappings)
	assert.NotNil(t, sanitizer.sensitivePatterns)

	// Verify default mappings are loaded
	assert.Contains(t, sanitizer.errorMappings, interfaces.FailureTypeImagePull)
	assert.Contains(t, sanitizer.errorMappings, interfaces.FailureTypeContainerCrash)
	assert.Contains(t, sanitizer.errorMappings, interfaces.FailureTypeResourceLimit)
	assert.Contains(t, sanitizer.errorMappings, interfaces.FailureTypeTimeout)
	assert.Contains(t, sanitizer.errorMappings, interfaces.FailureTypeUnknown)
}

// TestSanitize_ImagePullErrors tests sanitization of image pull errors.
// Validates: Requirements 4.1, 4.2
func TestSanitize_ImagePullErrors(t *testing.T) {
	sanitizer := NewStatusSanitizer()

	testCases := []struct {
		name            string
		failureType     interfaces.FailureType
		reason          string
		message         string
		expectedMessage string
		expectedCode    string
	}{
		{
			name:            "ImagePullBackOff - K8s",
			failureType:     interfaces.FailureTypeImagePull,
			reason:          "ImagePullBackOff",
			message:         "Back-off pulling image \"nginx:invalid\"",
			expectedMessage: "Image pull failed, system is retrying",
			expectedCode:    "IMG_PULL_BACKOFF",
		},
		{
			name:            "ErrImagePull - K8s",
			failureType:     interfaces.FailureTypeImagePull,
			reason:          "ErrImagePull",
			message:         "rpc error: code = Unknown desc = Error response from daemon",
			expectedMessage: "Failed to pull image",
			expectedCode:    "IMG_PULL_ERROR",
		},
		{
			name:            "InvalidImageName - K8s",
			failureType:     interfaces.FailureTypeImagePull,
			reason:          "InvalidImageName",
			message:         "Invalid image name",
			expectedMessage: "Invalid image name format",
			expectedCode:    "IMG_INVALID_NAME",
		},
		{
			name:            "image_pull_failed - Novita",
			failureType:     interfaces.FailureTypeImagePull,
			reason:          "image_pull_failed",
			message:         "Failed to pull image",
			expectedMessage: "Image pull failed",
			expectedCode:    "IMG_PULL_FAILED",
		},
		{
			name:            "image_not_found - Novita",
			failureType:     interfaces.FailureTypeImagePull,
			reason:          "image_not_found",
			message:         "Image not found in registry",
			expectedMessage: "Image not found",
			expectedCode:    "IMG_NOT_FOUND",
		},
		{
			name:            "unknown reason - fallback to default",
			failureType:     interfaces.FailureTypeImagePull,
			reason:          "SomeUnknownReason",
			message:         "Some unknown error",
			expectedMessage: "Image loading failed",
			expectedCode:    "IMG_ERROR",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := sanitizer.Sanitize(tc.failureType, tc.reason, tc.message)
			require.NotNil(t, result)
			assert.Equal(t, tc.expectedMessage, result.UserMessage)
			assert.Equal(t, tc.expectedCode, result.ErrorCode)
			assert.NotEmpty(t, result.Suggestion)
		})
	}
}

// TestSanitize_ContainerCrashErrors tests sanitization of container crash errors.
// Validates: Requirements 4.1
func TestSanitize_ContainerCrashErrors(t *testing.T) {
	sanitizer := NewStatusSanitizer()

	testCases := []struct {
		name            string
		reason          string
		expectedMessage string
		expectedCode    string
	}{
		{
			name:            "CrashLoopBackOff",
			reason:          "CrashLoopBackOff",
			expectedMessage: "Container keeps crashing after startup",
			expectedCode:    "CONTAINER_CRASH_LOOP",
		},
		{
			name:            "OOMKilled",
			reason:          "OOMKilled",
			expectedMessage: "Container terminated due to out of memory",
			expectedCode:    "CONTAINER_OOM",
		},
		{
			name:            "container_crashed - Novita",
			reason:          "container_crashed",
			expectedMessage: "Container crashed",
			expectedCode:    "CONTAINER_CRASHED",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := sanitizer.Sanitize(interfaces.FailureTypeContainerCrash, tc.reason, "")
			require.NotNil(t, result)
			assert.Equal(t, tc.expectedMessage, result.UserMessage)
			assert.Equal(t, tc.expectedCode, result.ErrorCode)
		})
	}
}

// TestSanitize_ResourceLimitErrors tests sanitization of resource limit errors.
// Validates: Requirements 4.1
func TestSanitize_ResourceLimitErrors(t *testing.T) {
	sanitizer := NewStatusSanitizer()

	testCases := []struct {
		name            string
		reason          string
		expectedMessage string
		expectedCode    string
	}{
		{
			name:            "OutOfMemory",
			reason:          "OutOfMemory",
			expectedMessage: "Insufficient memory resources",
			expectedCode:    "RESOURCE_OOM",
		},
		{
			name:            "OutOfGpu",
			reason:          "OutOfGpu",
			expectedMessage: "Insufficient GPU resources",
			expectedCode:    "RESOURCE_GPU",
		},
		{
			name:            "insufficient_resources - Novita",
			reason:          "insufficient_resources",
			expectedMessage: "Insufficient resources",
			expectedCode:    "RESOURCE_INSUFFICIENT",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := sanitizer.Sanitize(interfaces.FailureTypeResourceLimit, tc.reason, "")
			require.NotNil(t, result)
			assert.Equal(t, tc.expectedMessage, result.UserMessage)
			assert.Equal(t, tc.expectedCode, result.ErrorCode)
		})
	}
}

// TestSanitize_TimeoutErrors tests sanitization of timeout errors.
// Validates: Requirements 4.1
func TestSanitize_TimeoutErrors(t *testing.T) {
	sanitizer := NewStatusSanitizer()

	testCases := []struct {
		name            string
		reason          string
		expectedMessage string
		expectedCode    string
	}{
		{
			name:            "ImagePullTimeout",
			reason:          "ImagePullTimeout",
			expectedMessage: "Image pull timeout",
			expectedCode:    "TIMEOUT_IMAGE_PULL",
		},
		{
			name:            "StartupTimeout",
			reason:          "StartupTimeout",
			expectedMessage: "Container startup timeout",
			expectedCode:    "TIMEOUT_STARTUP",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := sanitizer.Sanitize(interfaces.FailureTypeTimeout, tc.reason, "")
			require.NotNil(t, result)
			assert.Equal(t, tc.expectedMessage, result.UserMessage)
			assert.Equal(t, tc.expectedCode, result.ErrorCode)
		})
	}
}

// TestSanitize_UnknownFailureType tests sanitization with unknown failure type.
func TestSanitize_UnknownFailureType(t *testing.T) {
	sanitizer := NewStatusSanitizer()

	result := sanitizer.Sanitize(interfaces.FailureTypeUnknown, "SomeReason", "Some message")
	require.NotNil(t, result)
	assert.Equal(t, "Unknown error occurred", result.UserMessage)
	assert.Equal(t, "UNKNOWN_ERROR", result.ErrorCode)
}

// TestSanitize_CaseInsensitiveMatch tests case-insensitive reason matching.
func TestSanitize_CaseInsensitiveMatch(t *testing.T) {
	sanitizer := NewStatusSanitizer()

	testCases := []struct {
		name         string
		reason       string
		expectedCode string
	}{
		{"lowercase", "imagepullbackoff", "IMG_PULL_BACKOFF"},
		{"uppercase", "IMAGEPULLBACKOFF", "IMG_PULL_BACKOFF"},
		{"mixed case", "ImagePullBackOff", "IMG_PULL_BACKOFF"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := sanitizer.Sanitize(interfaces.FailureTypeImagePull, tc.reason, "")
			require.NotNil(t, result)
			assert.Equal(t, tc.expectedCode, result.ErrorCode)
		})
	}
}

// TestSanitize_PartialMatch tests partial reason matching.
func TestSanitize_PartialMatch(t *testing.T) {
	sanitizer := NewStatusSanitizer()

	// Reason contains "ImagePullBackOff" as substring
	result := sanitizer.Sanitize(interfaces.FailureTypeImagePull, "ContainerImagePullBackOffError", "")
	require.NotNil(t, result)
	assert.Equal(t, "IMG_PULL_BACKOFF", result.ErrorCode)
}

// TestSanitize_MessageMatch tests matching based on message content.
func TestSanitize_MessageMatch(t *testing.T) {
	sanitizer := NewStatusSanitizer()

	// Unknown reason but message contains known pattern
	result := sanitizer.Sanitize(interfaces.FailureTypeImagePull, "UnknownReason", "Error: ImagePullBackOff occurred")
	require.NotNil(t, result)
	assert.Equal(t, "IMG_PULL_BACKOFF", result.ErrorCode)
}

// TestSanitizeSensitiveInfo_NodeNames tests removal of node names.
// Validates: Requirement 4.3
func TestSanitizeSensitiveInfo_NodeNames(t *testing.T) {
	sanitizer := NewStatusSanitizer()

	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "node name with prefix",
			input:    "Failed to schedule pod on node-abc123",
			expected: "Failed to schedule pod on [node]",
		},
		{
			name:     "ip-based node name",
			input:    "Pod scheduled on ip-192-168-1-100.ec2.internal",
			expected: "Pod scheduled on [node]",
		},
		{
			name:     "gke node name",
			input:    "Running on gke-cluster-default-pool-abc123",
			expected: "Running on [node]",
		},
		{
			name:     "eks node name",
			input:    "Node eks-node-group-12345 is not ready",
			expected: "Node [node] is not ready",
		},
		{
			name:     "aks node name",
			input:    "Scheduled on aks-nodepool1-12345678-vmss000000",
			expected: "Scheduled on [node]",
		},
		{
			name:     "k8s node reference",
			input:    "Error from node/my-cluster-worker-1",
			expected: "Error from node/[redacted]",
		},
		{
			name:     "k8s node reference with node prefix in name",
			input:    "Error from node/node-abc123",
			expected: "Error from node/[node]",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := sanitizer.SanitizeSensitiveInfo(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// TestSanitizeSensitiveInfo_Namespaces tests removal of namespace names.
// Validates: Requirement 4.3
func TestSanitizeSensitiveInfo_Namespaces(t *testing.T) {
	sanitizer := NewStatusSanitizer()

	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "namespace with slash",
			input:    "Pod in namespace/production failed",
			expected: "Pod in namespace/[redacted] failed",
		},
		{
			name:     "namespace with colon",
			input:    "Error in namespace: my-secret-ns",
			expected: "Error in namespace/[redacted]",
		},
		{
			name:     "ns abbreviation",
			input:    "Resource in ns/kube-system",
			expected: "Resource in ns/[redacted]",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := sanitizer.SanitizeSensitiveInfo(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// TestSanitizeSensitiveInfo_InternalIPs tests removal of internal IP addresses.
// Validates: Requirement 4.3
func TestSanitizeSensitiveInfo_InternalIPs(t *testing.T) {
	sanitizer := NewStatusSanitizer()

	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "10.x.x.x IP",
			input:    "Connection to 10.0.0.1 failed",
			expected: "Connection to [internal-ip] failed",
		},
		{
			name:     "172.16.x.x IP",
			input:    "Server at 172.16.0.100 not responding",
			expected: "Server at [internal-ip] not responding",
		},
		{
			name:     "172.31.x.x IP",
			input:    "Node IP: 172.31.255.255",
			expected: "Node IP: [internal-ip]",
		},
		{
			name:     "192.168.x.x IP",
			input:    "Internal address 192.168.1.1",
			expected: "Internal address [internal-ip]",
		},
		{
			name:     "multiple IPs",
			input:    "From 10.0.0.1 to 192.168.1.100",
			expected: "From [internal-ip] to [internal-ip]",
		},
		{
			name:     "public IP should not be redacted",
			input:    "External IP: 8.8.8.8",
			expected: "External IP: 8.8.8.8",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := sanitizer.SanitizeSensitiveInfo(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// TestSanitizeSensitiveInfo_PodNames tests removal of pod names.
// Validates: Requirement 4.3
func TestSanitizeSensitiveInfo_PodNames(t *testing.T) {
	sanitizer := NewStatusSanitizer()

	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "pod reference",
			input:    "Error in pod/my-app-deployment-abc123",
			expected: "Error in pod/[redacted]",
		},
		{
			name:     "pod name with hash",
			input:    "Pod my-deployment-5d4f6b7c8d-x9z2k failed",
			expected: "Pod [pod-name] failed",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := sanitizer.SanitizeSensitiveInfo(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// TestSanitizeSensitiveInfo_Secrets tests removal of secret references.
// Validates: Requirement 4.3
func TestSanitizeSensitiveInfo_Secrets(t *testing.T) {
	sanitizer := NewStatusSanitizer()

	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "secret reference",
			input:    "Failed to mount secret/my-registry-credentials",
			expected: "Failed to mount secret/[redacted]",
		},
		{
			name:     "secrets path",
			input:    "Error reading secrets/database-password",
			expected: "Error reading secret/[redacted]",
		},
		{
			name:     "secret with colon",
			input:    "Missing secret: api-key-secret",
			expected: "Missing secret/[redacted]",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := sanitizer.SanitizeSensitiveInfo(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// TestSanitizeSensitiveInfo_CloudProviderIDs tests removal of cloud provider identifiers.
// Validates: Requirement 4.3
func TestSanitizeSensitiveInfo_CloudProviderIDs(t *testing.T) {
	sanitizer := NewStatusSanitizer()

	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "AWS ECR registry",
			input:    "Pulling from 123456789012.dkr.ecr.us-east-1.amazonaws.com/myrepo",
			expected: "Pulling from [aws-ecr-registry]/myrepo",
		},
		{
			name:     "GCR project",
			input:    "Image gcr.io/my-project-123/myimage not found",
			expected: "Image gcr.io/[project]/myimage not found",
		},
		{
			name:     "UUID (subscription/resource ID)",
			input:    "Resource a1b2c3d4-e5f6-7890-abcd-ef1234567890 not found",
			expected: "Resource [uuid] not found",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := sanitizer.SanitizeSensitiveInfo(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// TestSanitizeSensitiveInfo_EmptyString tests handling of empty string.
func TestSanitizeSensitiveInfo_EmptyString(t *testing.T) {
	sanitizer := NewStatusSanitizer()
	result := sanitizer.SanitizeSensitiveInfo("")
	assert.Equal(t, "", result)
}

// TestSanitizeSensitiveInfo_NoSensitiveInfo tests messages without sensitive info.
func TestSanitizeSensitiveInfo_NoSensitiveInfo(t *testing.T) {
	sanitizer := NewStatusSanitizer()

	testCases := []struct {
		name  string
		input string
	}{
		{"simple error", "Image pull failed"},
		{"generic message", "Container crashed with exit code 1"},
		{"user-friendly message", "Please check image name"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := sanitizer.SanitizeSensitiveInfo(tc.input)
			assert.Equal(t, tc.input, result)
		})
	}
}

// TestSanitizeSensitiveInfo_ComplexMessage tests sanitization of complex messages with multiple sensitive items.
// Validates: Requirement 4.3
func TestSanitizeSensitiveInfo_ComplexMessage(t *testing.T) {
	sanitizer := NewStatusSanitizer()

	input := "Failed to pull image from 123456789012.dkr.ecr.us-east-1.amazonaws.com/myrepo on node-abc123 (10.0.0.1) in namespace/production using secret/registry-creds"
	result := sanitizer.SanitizeSensitiveInfo(input)

	// Verify all sensitive info is removed
	assert.NotContains(t, result, "123456789012")
	assert.NotContains(t, result, "node-abc123")
	assert.NotContains(t, result, "10.0.0.1")
	assert.NotContains(t, result, "production")
	assert.NotContains(t, result, "registry-creds")

	// Verify replacements are present
	assert.Contains(t, result, "[aws-ecr-registry]")
	assert.Contains(t, result, "[node]")
	assert.Contains(t, result, "[internal-ip]")
	assert.Contains(t, result, "namespace/[redacted]")
	assert.Contains(t, result, "secret/[redacted]")
}

// TestAddErrorMapping tests adding custom error mappings.
func TestAddErrorMapping(t *testing.T) {
	sanitizer := NewStatusSanitizer()

	customError := SanitizedError{
		UserMessage: "Custom error message",
		Suggestion:  "Custom suggestion",
		ErrorCode:   "CUSTOM_ERROR",
	}

	sanitizer.AddErrorMapping(interfaces.FailureTypeImagePull, "CustomReason", customError)

	result := sanitizer.Sanitize(interfaces.FailureTypeImagePull, "CustomReason", "")
	require.NotNil(t, result)
	assert.Equal(t, "Custom error message", result.UserMessage)
	assert.Equal(t, "CUSTOM_ERROR", result.ErrorCode)
}

// TestAddSensitivePattern tests adding custom sensitive patterns.
func TestAddSensitivePattern(t *testing.T) {
	sanitizer := NewStatusSanitizer()

	// Add custom pattern for API keys
	pattern := regexp.MustCompile(`\bapi-key-[a-zA-Z0-9]+\b`)
	sanitizer.AddSensitivePattern(pattern, "[api-key]", "API key")

	input := "Authentication failed with api-key-abc123xyz"
	result := sanitizer.SanitizeSensitiveInfo(input)
	assert.Equal(t, "Authentication failed with [api-key]", result)
}

// TestGetErrorMapping tests retrieving error mappings.
func TestGetErrorMapping(t *testing.T) {
	sanitizer := NewStatusSanitizer()

	t.Run("existing mapping", func(t *testing.T) {
		result := sanitizer.GetErrorMapping(interfaces.FailureTypeImagePull, "ImagePullBackOff")
		require.NotNil(t, result)
		assert.Equal(t, "IMG_PULL_BACKOFF", result.ErrorCode)
	})

	t.Run("non-existing mapping", func(t *testing.T) {
		result := sanitizer.GetErrorMapping(interfaces.FailureTypeImagePull, "NonExistentReason")
		assert.Nil(t, result)
	})

	t.Run("non-existing failure type", func(t *testing.T) {
		result := sanitizer.GetErrorMapping("INVALID_TYPE", "SomeReason")
		assert.Nil(t, result)
	})
}

// TestSanitizeWorkerFailureInfo tests the convenience method for sanitizing WorkerFailureInfo.
func TestSanitizeWorkerFailureInfo(t *testing.T) {
	sanitizer := NewStatusSanitizer()

	t.Run("nil input", func(t *testing.T) {
		result := sanitizer.SanitizeWorkerFailureInfo(nil)
		assert.Nil(t, result)
	})

	t.Run("valid input", func(t *testing.T) {
		info := &interfaces.WorkerFailureInfo{
			Type:    interfaces.FailureTypeImagePull,
			Reason:  "ImagePullBackOff",
			Message: "Failed on node-abc123 at 10.0.0.1",
		}

		result := sanitizer.SanitizeWorkerFailureInfo(info)
		require.NotNil(t, result)

		// Verify type and reason are preserved
		assert.Equal(t, interfaces.FailureTypeImagePull, result.Type)
		assert.Equal(t, "ImagePullBackOff", result.Reason)

		// Verify message is sanitized
		assert.NotContains(t, result.Message, "node-abc123")
		assert.NotContains(t, result.Message, "10.0.0.1")
		assert.Contains(t, result.Message, "[node]")
		assert.Contains(t, result.Message, "[internal-ip]")

		// Verify sanitized message is set
		assert.Contains(t, result.SanitizedMsg, "Image pull failed")
	})
}

// TestDefaultErrorMappings verifies all default error mappings have required fields.
func TestDefaultErrorMappings(t *testing.T) {
	allMappings := []map[string]SanitizedError{
		ImagePullErrorMappings,
		ContainerCrashErrorMappings,
		ResourceLimitErrorMappings,
		TimeoutErrorMappings,
		UnknownErrorMappings,
	}

	for i, mappings := range allMappings {
		for reason, err := range mappings {
			t.Run(reason, func(t *testing.T) {
				assert.NotEmpty(t, err.UserMessage, "Mapping %d, reason %s: UserMessage should not be empty", i, reason)
				assert.NotEmpty(t, err.Suggestion, "Mapping %d, reason %s: Suggestion should not be empty", i, reason)
				assert.NotEmpty(t, err.ErrorCode, "Mapping %d, reason %s: ErrorCode should not be empty", i, reason)
			})
		}
	}
}

// TestSanitize_RequirementMapping tests the specific mappings required by Requirement 4.2.
// Validates: Requirement 4.2
func TestSanitize_RequirementMapping(t *testing.T) {
	sanitizer := NewStatusSanitizer()

	// Requirement 4.2 specifies these exact mappings:
	// "ImagePullBackOff" → "Image pull failed, please check image name and access permissions"
	// "ErrImagePull" → "Unable to pull image, please confirm image exists and is accessible"
	// "InvalidImageName" → "Invalid image name format"

	t.Run("ImagePullBackOff mapping", func(t *testing.T) {
		result := sanitizer.Sanitize(interfaces.FailureTypeImagePull, "ImagePullBackOff", "")
		require.NotNil(t, result)
		// The user message + suggestion should convey the required meaning
		fullMessage := result.UserMessage + ". " + result.Suggestion
		assert.Contains(t, fullMessage, "Image pull failed")
		assert.Contains(t, fullMessage, "image name")
		assert.Contains(t, fullMessage, "access")
	})

	t.Run("ErrImagePull mapping", func(t *testing.T) {
		result := sanitizer.Sanitize(interfaces.FailureTypeImagePull, "ErrImagePull", "")
		require.NotNil(t, result)
		fullMessage := result.UserMessage + ". " + result.Suggestion
		assert.Contains(t, fullMessage, "pull image")
		assert.Contains(t, fullMessage, "image exists")
		assert.Contains(t, fullMessage, "accessible")
	})

	t.Run("InvalidImageName mapping", func(t *testing.T) {
		result := sanitizer.Sanitize(interfaces.FailureTypeImagePull, "InvalidImageName", "")
		require.NotNil(t, result)
		assert.Contains(t, result.UserMessage, "Invalid image name format")
	})
}
