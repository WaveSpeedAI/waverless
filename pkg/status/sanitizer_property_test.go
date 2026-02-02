// Package status provides property-based tests for status sanitization functionality.
// These tests verify universal properties that should hold across all valid inputs.
//
// Feature: image-validation-and-status
// Property 5: Status sanitization and information hiding
// **Validates: Requirements 4.1, 4.3**
package status

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"waverless/pkg/interfaces"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// TestProperty_StatusSanitizationRemovesSensitiveInfo tests Property 5: Status Sanitization and Information Hiding
//
// Property: For any K8s error message containing sensitive information (node names,
// namespace names, internal IPs), the Status_Sanitizer SHALL produce a user-friendly
// message that does NOT contain any of the sensitive information while preserving
// the actionable meaning.
//
// Feature: image-validation-and-status, Property 5: Status sanitization and information hiding
// **Validates: Requirements 4.1, 4.3**
func TestProperty_StatusSanitizationRemovesSensitiveInfo(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 50

	properties := gopter.NewProperties(parameters)
	sanitizer := NewStatusSanitizer()

	// Property 5a: Node names are always removed from messages
	properties.Property("node names are always removed from messages", prop.ForAll(
		func(nodeName, prefix, suffix string) bool {
			message := prefix + nodeName + suffix
			result := sanitizer.SanitizeSensitiveInfo(message)
			// The original node name should not appear in the result
			return !strings.Contains(result, nodeName)
		},
		genNodeName(),
		genMessagePrefix(),
		genMessageSuffix(),
	))

	// Property 5b: Namespace names are always removed from messages
	properties.Property("namespace names are always removed from messages", prop.ForAll(
		func(nsName, prefix, suffix string) bool {
			message := prefix + nsName + suffix
			result := sanitizer.SanitizeSensitiveInfo(message)
			// Extract the actual namespace value (after namespace/ or ns/)
			parts := strings.Split(nsName, "/")
			if len(parts) > 1 {
				actualNs := parts[1]
				// The actual namespace value should not appear in the result
				return !strings.Contains(result, actualNs)
			}
			return true
		},
		genNamespaceName(),
		genMessagePrefix(),
		genMessageSuffix(),
	))

	// Property 5c: Internal IPs are always removed from messages
	properties.Property("internal IPs are always removed from messages", prop.ForAll(
		func(ip, prefix, suffix string) bool {
			message := prefix + ip + suffix
			result := sanitizer.SanitizeSensitiveInfo(message)
			// The original IP should not appear in the result
			return !strings.Contains(result, ip)
		},
		genInternalIP(),
		genMessagePrefix(),
		genMessageSuffix(),
	))

	// Property 5d: Pod names are always removed from messages
	properties.Property("pod names are always removed from messages", prop.ForAll(
		func(podName, prefix, suffix string) bool {
			message := prefix + podName + suffix
			result := sanitizer.SanitizeSensitiveInfo(message)
			// The original pod name should not appear in the result
			return !strings.Contains(result, podName)
		},
		genPodName(),
		genMessagePrefix(),
		genMessageSuffix(),
	))

	// Property 5e: Secret names are always removed from messages
	// We use constant secret names to avoid shrinking issues
	properties.Property("secret names are always removed from messages", prop.ForAll(
		func(secretIdx int, prefix, suffix string) bool {
			// Use predefined secret names to avoid shrinking issues
			secretNames := []string{
				"secret/my-registry-credentials",
				"secret/database-password",
				"secrets/api-key-secret",
				"secrets/tls-cert",
				"secret/aws-credentials",
			}
			secretName := secretNames[secretIdx%len(secretNames)]

			message := prefix + secretName + suffix
			result := sanitizer.SanitizeSensitiveInfo(message)

			// Extract the actual secret value
			var actualSecret string
			if strings.HasPrefix(secretName, "secrets/") {
				actualSecret = strings.TrimPrefix(secretName, "secrets/")
			} else {
				actualSecret = strings.TrimPrefix(secretName, "secret/")
			}

			// The actual secret value should not appear in the result
			return !strings.Contains(result, actualSecret)
		},
		gen.IntRange(0, 100),
		genMessagePrefix(),
		genMessageSuffix(),
	))

	// Property 5f: AWS ECR registry URLs are sanitized
	properties.Property("AWS ECR registry URLs are sanitized", prop.ForAll(
		func(accountID, region, prefix, suffix string) bool {
			ecrURL := fmt.Sprintf("%s.dkr.ecr.%s.amazonaws.com", accountID, region)
			message := prefix + ecrURL + suffix
			result := sanitizer.SanitizeSensitiveInfo(message)
			// The AWS account ID should not appear in the result
			return !strings.Contains(result, accountID)
		},
		genAWSAccountID(),
		genAWSRegion(),
		genMessagePrefix(),
		genMessageSuffix(),
	))

	// Property 5g: UUIDs are always removed from messages
	properties.Property("UUIDs are always removed from messages", prop.ForAll(
		func(uuid, prefix, suffix string) bool {
			message := prefix + uuid + suffix
			result := sanitizer.SanitizeSensitiveInfo(message)
			// The original UUID should not appear in the result
			return !strings.Contains(result, uuid)
		},
		genUUID(),
		genMessagePrefix(),
		genMessageSuffix(),
	))

	properties.TestingRun(t)
}

// TestProperty_SanitizationPreservesActionableMeaning tests that sanitization preserves actionable meaning
//
// Property: The sanitized message should still contain actionable information
// (error type indicators, general context) while removing sensitive details.
//
// Feature: image-validation-and-status, Property 5: Status sanitization and information hiding
// **Validates: Requirements 4.1, 4.3**
func TestProperty_SanitizationPreservesActionableMeaning(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 50

	properties := gopter.NewProperties(parameters)
	sanitizer := NewStatusSanitizer()

	// Property: Sanitization preserves non-sensitive words
	properties.Property("sanitization preserves non-sensitive words", prop.ForAll(
		func(actionWord string) bool {
			// Create a message with an action word and sensitive info
			message := actionWord + " on node-abc123"
			result := sanitizer.SanitizeSensitiveInfo(message)
			// The action word should still be present
			return strings.Contains(result, actionWord)
		},
		genActionWord(),
	))

	// Property: Sanitization does not alter messages without sensitive info
	properties.Property("sanitization does not alter messages without sensitive info", prop.ForAll(
		func(message string) bool {
			result := sanitizer.SanitizeSensitiveInfo(message)
			// If the message has no sensitive info, it should remain unchanged
			return result == message
		},
		genSafeMessage(),
	))

	// Property: Sanitization is idempotent
	properties.Property("sanitization is idempotent", prop.ForAll(
		func(message string) bool {
			result1 := sanitizer.SanitizeSensitiveInfo(message)
			result2 := sanitizer.SanitizeSensitiveInfo(result1)
			// Applying sanitization twice should give the same result
			return result1 == result2
		},
		genMessageWithSensitiveInfo(),
	))

	// Property: Sanitization is deterministic
	properties.Property("sanitization is deterministic", prop.ForAll(
		func(message string) bool {
			result1 := sanitizer.SanitizeSensitiveInfo(message)
			result2 := sanitizer.SanitizeSensitiveInfo(message)
			// Same input should always produce same output
			return result1 == result2
		},
		gen.AnyString(),
	))

	properties.TestingRun(t)
}

// TestProperty_ErrorMappingsProduceUserFriendlyMessages tests that error mappings produce user-friendly messages
//
// Property: For any known error reason, the Sanitize function SHALL produce a
// user-friendly message with non-empty UserMessage, Suggestion, and ErrorCode.
//
// Feature: image-validation-and-status, Property 5: Status sanitization and information hiding
// **Validates: Requirements 4.1, 4.3**
func TestProperty_ErrorMappingsProduceUserFriendlyMessages(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 50

	properties := gopter.NewProperties(parameters)
	sanitizer := NewStatusSanitizer()

	// Property: All failure types produce non-empty user messages
	properties.Property("all failure types produce non-empty user messages", prop.ForAll(
		func(failureType interfaces.FailureType, reason, message string) bool {
			result := sanitizer.Sanitize(failureType, reason, message)
			if result == nil {
				return false
			}
			// UserMessage, Suggestion, and ErrorCode should all be non-empty
			return len(result.UserMessage) > 0 &&
				len(result.Suggestion) > 0 &&
				len(result.ErrorCode) > 0
		},
		genFailureType(),
		gen.AnyString(),
		gen.AnyString(),
	))

	// Property: Known K8s reasons produce specific error codes
	properties.Property("known K8s reasons produce specific error codes", prop.ForAll(
		func(reason string) bool {
			result := sanitizer.Sanitize(interfaces.FailureTypeImagePull, reason, "")
			if result == nil {
				return false
			}
			// Should have a valid error code
			return len(result.ErrorCode) > 0 && strings.HasPrefix(result.ErrorCode, "IMG_")
		},
		genKnownK8sImagePullReason(),
	))

	// Property: User messages are non-empty (user-friendly)
	properties.Property("user messages are non-empty", prop.ForAll(
		func(failureType interfaces.FailureType, reason string) bool {
			result := sanitizer.Sanitize(failureType, reason, "")
			if result == nil {
				return false
			}
			// User message should be non-empty
			return len(result.UserMessage) > 0
		},
		genFailureType(),
		gen.AnyString(),
	))

	// Property: Sanitize is deterministic
	properties.Property("sanitize is deterministic", prop.ForAll(
		func(failureType interfaces.FailureType, reason, message string) bool {
			result1 := sanitizer.Sanitize(failureType, reason, message)
			result2 := sanitizer.Sanitize(failureType, reason, message)
			if result1 == nil || result2 == nil {
				return result1 == nil && result2 == nil
			}
			return result1.UserMessage == result2.UserMessage &&
				result1.Suggestion == result2.Suggestion &&
				result1.ErrorCode == result2.ErrorCode
		},
		genFailureType(),
		gen.AnyString(),
		gen.AnyString(),
	))

	properties.TestingRun(t)
}

// TestProperty_ComplexMessagesSanitization tests sanitization of complex messages with multiple sensitive items
//
// Property: For any message containing multiple types of sensitive information,
// ALL sensitive items SHALL be removed from the output.
//
// Feature: image-validation-and-status, Property 5: Status sanitization and information hiding
// **Validates: Requirements 4.1, 4.3**
func TestProperty_ComplexMessagesSanitization(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 30

	properties := gopter.NewProperties(parameters)
	sanitizer := NewStatusSanitizer()

	// Property: Messages with multiple sensitive items have all items removed
	properties.Property("messages with multiple sensitive items have all items removed", prop.ForAll(
		func(nodeName, ip, nsName string) bool {
			message := fmt.Sprintf("Error on %s at %s in %s", nodeName, ip, nsName)
			result := sanitizer.SanitizeSensitiveInfo(message)

			// None of the sensitive items should appear in the result
			nodeNameClean := nodeName
			if strings.HasPrefix(nodeName, "node/") {
				nodeNameClean = strings.TrimPrefix(nodeName, "node/")
			}

			ipClean := ip

			nsNameClean := nsName
			if strings.Contains(nsName, "/") {
				parts := strings.Split(nsName, "/")
				if len(parts) > 1 {
					nsNameClean = parts[1]
				}
			}

			return !strings.Contains(result, nodeNameClean) &&
				!strings.Contains(result, ipClean) &&
				!strings.Contains(result, nsNameClean)
		},
		genNodeName(),
		genInternalIP(),
		genNamespaceName(),
	))

	// Property: GCR project IDs are sanitized
	properties.Property("GCR project IDs are sanitized", prop.ForAll(
		func(projectID, imageName string) bool {
			gcrURL := fmt.Sprintf("gcr.io/%s/%s", projectID, imageName)
			message := "Failed to pull image " + gcrURL
			result := sanitizer.SanitizeSensitiveInfo(message)
			// The project ID should not appear in the result
			return !strings.Contains(result, projectID)
		},
		genGCPProjectID(),
		genSimpleImageName(),
	))

	properties.TestingRun(t)
}

// TestProperty_SanitizeWorkerFailureInfoIntegration tests the SanitizeWorkerFailureInfo method
//
// Property: The SanitizeWorkerFailureInfo method SHALL sanitize both the message
// and produce a user-friendly SanitizedMsg.
//
// Feature: image-validation-and-status, Property 5: Status sanitization and information hiding
// **Validates: Requirements 4.1, 4.3**
func TestProperty_SanitizeWorkerFailureInfoIntegration(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 50

	properties := gopter.NewProperties(parameters)
	sanitizer := NewStatusSanitizer()

	// Property: SanitizeWorkerFailureInfo removes sensitive info from message
	properties.Property("SanitizeWorkerFailureInfo removes sensitive info from message", prop.ForAll(
		func(failureType interfaces.FailureType, reason string, nodeName, ip string) bool {
			message := fmt.Sprintf("Error on %s at %s", nodeName, ip)
			info := &interfaces.WorkerFailureInfo{
				Type:    failureType,
				Reason:  reason,
				Message: message,
			}

			result := sanitizer.SanitizeWorkerFailureInfo(info)
			if result == nil {
				return false
			}

			// The sanitized message should not contain the sensitive info
			nodeNameClean := nodeName
			if strings.HasPrefix(nodeName, "node/") {
				nodeNameClean = strings.TrimPrefix(nodeName, "node/")
			}

			return !strings.Contains(result.Message, nodeNameClean) &&
				!strings.Contains(result.Message, ip) &&
				len(result.SanitizedMsg) > 0
		},
		genFailureType(),
		gen.AnyString(),
		genNodeName(),
		genInternalIP(),
	))

	// Property: SanitizeWorkerFailureInfo preserves type and reason
	properties.Property("SanitizeWorkerFailureInfo preserves type and reason", prop.ForAll(
		func(failureType interfaces.FailureType, reason, message string) bool {
			info := &interfaces.WorkerFailureInfo{
				Type:    failureType,
				Reason:  reason,
				Message: message,
			}

			result := sanitizer.SanitizeWorkerFailureInfo(info)
			if result == nil {
				return false
			}

			// Type and reason should be preserved
			return result.Type == failureType && result.Reason == reason
		},
		genFailureType(),
		gen.AnyString(),
		gen.AnyString(),
	))

	// Property: SanitizeWorkerFailureInfo handles nil input
	properties.Property("SanitizeWorkerFailureInfo handles nil input", prop.ForAll(
		func(_ int) bool {
			result := sanitizer.SanitizeWorkerFailureInfo(nil)
			return result == nil
		},
		gen.Const(0),
	))

	properties.TestingRun(t)
}

// ============================================================================
// Generators for sensitive information patterns
// ============================================================================

// genNodeName generates node names in various formats
// Patterns: node-xxx, ip-xxx-xxx-xxx-xxx, gke-xxx, eks-xxx, aks-xxx
func genNodeName() gopter.Gen {
	return gen.OneGenOf(
		// node-xxx format
		gen.RegexMatch(`[a-z][a-z0-9]{2,8}`).Map(func(s string) string {
			return "node-" + s
		}),
		// ip-xxx-xxx-xxx-xxx format (AWS style)
		gopter.CombineGens(
			gen.IntRange(1, 255),
			gen.IntRange(0, 255),
			gen.IntRange(0, 255),
			gen.IntRange(0, 255),
		).Map(func(vals []interface{}) string {
			return fmt.Sprintf("ip-%d-%d-%d-%d", vals[0], vals[1], vals[2], vals[3])
		}),
		// gke-xxx format (GKE style)
		gen.RegexMatch(`[a-z][a-z0-9]{2,8}`).Map(func(s string) string {
			return "gke-" + s
		}),
		// eks-xxx format (EKS style)
		gen.RegexMatch(`[a-z][a-z0-9]{2,8}`).Map(func(s string) string {
			return "eks-" + s
		}),
		// aks-xxx format (AKS style)
		gen.RegexMatch(`[a-z][a-z0-9]{2,8}`).Map(func(s string) string {
			return "aks-" + s
		}),
		// node/xxx format (K8s reference)
		gen.RegexMatch(`[a-z][a-z0-9]{2,8}`).Map(func(s string) string {
			return "node/" + s
		}),
	)
}

// genNamespaceName generates namespace names in various formats
// Patterns: namespace/xxx, ns/xxx
func genNamespaceName() gopter.Gen {
	return gen.OneGenOf(
		// namespace/xxx format
		gen.RegexMatch(`[a-z][a-z0-9]{2,12}`).Map(func(s string) string {
			return "namespace/" + s
		}),
		// ns/xxx format
		gen.RegexMatch(`[a-z][a-z0-9]{2,12}`).Map(func(s string) string {
			return "ns/" + s
		}),
	)
}

// genInternalIP generates internal IP addresses
// Patterns: 10.x.x.x, 172.16-31.x.x, 192.168.x.x
func genInternalIP() gopter.Gen {
	return gen.OneGenOf(
		// 10.x.x.x range
		gopter.CombineGens(
			gen.IntRange(0, 255),
			gen.IntRange(0, 255),
			gen.IntRange(0, 255),
		).Map(func(vals []interface{}) string {
			return fmt.Sprintf("10.%d.%d.%d", vals[0], vals[1], vals[2])
		}),
		// 172.16-31.x.x range
		gopter.CombineGens(
			gen.IntRange(16, 31),
			gen.IntRange(0, 255),
			gen.IntRange(0, 255),
		).Map(func(vals []interface{}) string {
			return fmt.Sprintf("172.%d.%d.%d", vals[0], vals[1], vals[2])
		}),
		// 192.168.x.x range
		gopter.CombineGens(
			gen.IntRange(0, 255),
			gen.IntRange(0, 255),
		).Map(func(vals []interface{}) string {
			return fmt.Sprintf("192.168.%d.%d", vals[0], vals[1])
		}),
	)
}

// genPodName generates pod names in various formats
// Patterns: pod/xxx, xxx-xxxxx-xxxxx (deployment style)
func genPodName() gopter.Gen {
	return gen.OneGenOf(
		// pod/xxx format
		gen.RegexMatch(`[a-z][a-z0-9]{2,12}`).Map(func(s string) string {
			return "pod/" + s
		}),
		// deployment-style pod name: name-hash-hash
		gopter.CombineGens(
			gen.RegexMatch(`[a-z][a-z0-9]{2,8}`),
			gen.RegexMatch(`[a-f0-9]{5,10}`),
			gen.RegexMatch(`[a-z0-9]{5}`),
		).Map(func(vals []interface{}) string {
			return fmt.Sprintf("%s-%s-%s", vals[0], vals[1], vals[2])
		}),
	)
}

// genSecretName generates secret names in various formats
// Patterns: secret/xxx, secrets/xxx
func genSecretName() gopter.Gen {
	return gen.OneGenOf(
		// secret/xxx format
		gen.RegexMatch(`[a-z][a-z0-9]{2,12}`).SuchThat(func(s string) bool {
			return len(s) >= 3
		}).Map(func(s string) string {
			return "secret/" + s
		}),
		// secrets/xxx format
		gen.RegexMatch(`[a-z][a-z0-9]{2,12}`).SuchThat(func(s string) bool {
			return len(s) >= 3
		}).Map(func(s string) string {
			return "secrets/" + s
		}),
	)
}

// genAWSAccountID generates AWS account IDs (12 digits)
func genAWSAccountID() gopter.Gen {
	return gen.RegexMatch(`[0-9]{12}`)
}

// genAWSRegion generates AWS region names
func genAWSRegion() gopter.Gen {
	return gen.OneConstOf(
		"us-east-1",
		"us-west-2",
		"eu-west-1",
		"ap-southeast-1",
		"ap-northeast-1",
	)
}

// genGCPProjectID generates GCP project IDs
func genGCPProjectID() gopter.Gen {
	return gen.RegexMatch(`[a-z][a-z0-9]{5,15}`)
}

// genUUID generates UUIDs
func genUUID() gopter.Gen {
	return gopter.CombineGens(
		gen.RegexMatch(`[a-f0-9]{8}`),
		gen.RegexMatch(`[a-f0-9]{4}`),
		gen.RegexMatch(`[a-f0-9]{4}`),
		gen.RegexMatch(`[a-f0-9]{4}`),
		gen.RegexMatch(`[a-f0-9]{12}`),
	).Map(func(vals []interface{}) string {
		return fmt.Sprintf("%s-%s-%s-%s-%s", vals[0], vals[1], vals[2], vals[3], vals[4])
	})
}

// genSimpleImageName generates simple image names for testing
func genSimpleImageName() gopter.Gen {
	return gen.RegexMatch(`[a-z][a-z0-9]{2,10}`)
}

// ============================================================================
// Generators for message components
// ============================================================================

// genMessagePrefix generates message prefixes
func genMessagePrefix() gopter.Gen {
	return gen.OneConstOf(
		"Error on ",
		"Failed to connect to ",
		"Cannot schedule pod on ",
		"Image pull failed on ",
		"Container crashed on ",
		"Resource limit exceeded on ",
		"Timeout waiting for ",
		"",
	)
}

// genMessageSuffix generates message suffixes
func genMessageSuffix() gopter.Gen {
	return gen.OneConstOf(
		" failed",
		" not responding",
		" is not ready",
		" timed out",
		"",
	)
}

// genActionWord generates action words that should be preserved
func genActionWord() gopter.Gen {
	return gen.OneConstOf(
		"Error",
		"Failed",
		"Cannot",
		"Unable",
		"Timeout",
		"Crashed",
		"Terminated",
		"Scheduled",
		"Running",
		"Pending",
	)
}

// genSafeMessage generates messages without sensitive information
func genSafeMessage() gopter.Gen {
	return gen.OneConstOf(
		"Image pull failed",
		"Container crashed with exit code 1",
		"Please check image name",
		"Resource limit exceeded",
		"Timeout waiting for container",
		"Authentication failed",
		"Invalid image format",
		"",
	)
}

// genMessageWithSensitiveInfo generates messages with various sensitive information
func genMessageWithSensitiveInfo() gopter.Gen {
	return gen.OneGenOf(
		// Message with node name
		genNodeName().Map(func(s string) string {
			return "Error on " + s
		}),
		// Message with IP
		genInternalIP().Map(func(s string) string {
			return "Connection to " + s + " failed"
		}),
		// Message with namespace
		genNamespaceName().Map(func(s string) string {
			return "Resource in " + s + " not found"
		}),
		// Message with pod name
		genPodName().Map(func(s string) string {
			return "Pod " + s + " crashed"
		}),
		// Message with secret
		genSecretName().Map(func(s string) string {
			return "Failed to mount " + s
		}),
		// Message with UUID
		genUUID().Map(func(s string) string {
			return "Resource " + s + " not found"
		}),
	)
}

// genFailureType generates failure types
func genFailureType() gopter.Gen {
	return gen.OneConstOf(
		interfaces.FailureTypeImagePull,
		interfaces.FailureTypeContainerCrash,
		interfaces.FailureTypeResourceLimit,
		interfaces.FailureTypeTimeout,
		interfaces.FailureTypeUnknown,
	)
}

// genKnownK8sImagePullReason generates known K8s image pull reasons
func genKnownK8sImagePullReason() gopter.Gen {
	return gen.OneConstOf(
		"ImagePullBackOff",
		"ErrImagePull",
		"InvalidImageName",
		"ImageInspectError",
		"RegistryUnavailable",
	)
}

// ============================================================================
// Helper functions
// ============================================================================

// containsChineseChar checks if a string contains Chinese characters
func containsChineseChar(s string) bool {
	for _, r := range s {
		if r >= 0x4e00 && r <= 0x9fff {
			return true
		}
	}
	return false
}

// sensitivePatternRegexes contains compiled regex patterns for sensitive info detection
var sensitivePatternRegexes = []*regexp.Regexp{
	regexp.MustCompile(`\b(?:node|ip|gke|aks|eks)[-_][a-zA-Z0-9][-a-zA-Z0-9_.]*\b`),
	regexp.MustCompile(`\bnode/[a-zA-Z0-9][-a-zA-Z0-9_.]*\b`),
	regexp.MustCompile(`\bnamespace[/:]?\s*[a-zA-Z0-9][-a-zA-Z0-9_.]*\b`),
	regexp.MustCompile(`\bns[/:]?\s*[a-zA-Z0-9][-a-zA-Z0-9_.]*\b`),
	regexp.MustCompile(`\b10\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`),
	regexp.MustCompile(`\b172\.(1[6-9]|2[0-9]|3[0-1])\.\d{1,3}\.\d{1,3}\b`),
	regexp.MustCompile(`\b192\.168\.\d{1,3}\.\d{1,3}\b`),
	regexp.MustCompile(`\bpod/[a-zA-Z0-9][-a-zA-Z0-9_.]*\b`),
	regexp.MustCompile(`\bsecret[/:]?\s*[a-zA-Z0-9][-a-zA-Z0-9_.]*\b`),
	regexp.MustCompile(`\b\d{12}\.dkr\.ecr\.[a-z0-9-]+\.amazonaws\.com\b`),
	regexp.MustCompile(`\bgcr\.io/[a-zA-Z0-9][-a-zA-Z0-9_]*/`),
	regexp.MustCompile(`\b[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}\b`),
}
