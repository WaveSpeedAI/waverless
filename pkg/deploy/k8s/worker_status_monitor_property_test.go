// Package k8s provides property-based tests for K8s worker status monitoring functionality.
// These tests verify universal properties that should hold across all valid inputs.
//
// Feature: image-validation-and-status
// Property 4: Pod status tracking completeness
// **Validates: Requirements 3.1, 3.2, 3.3**
package k8s

import (
	"testing"
	"time"

	"waverless/pkg/interfaces"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// TestProperty_PodStatusTrackingCompleteness tests Property 4: Pod Status Tracking Completeness
//
// Property: For any Pod status change event, the Pod_Status_Monitor SHALL record all
// required fields (phase, reason, message, timestamp) in the Worker record, and correctly
// identify ImagePullBackOff/ErrImagePull states as image-related failures.
//
// Feature: image-validation-and-status, Property 4: Pod status tracking completeness
// **Validates: Requirements 3.1, 3.2, 3.3**
func TestProperty_PodStatusTrackingCompleteness(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 50

	properties := gopter.NewProperties(parameters)
	monitor := &K8sWorkerStatusMonitor{}

	// Property 4a: ImagePullBackOff/ErrImagePull states are always classified as IMAGE_PULL_FAILED
	properties.Property("ImagePullBackOff/ErrImagePull states are classified as IMAGE_PULL_FAILED", prop.ForAll(
		func(reason string, message string) bool {
			result := monitor.ClassifyK8sFailure(reason, message)
			return result == interfaces.FailureTypeImagePull
		},
		genImagePullFailureReason(),
		gen.AnyString(),
	))

	// Property 4b: CrashLoopBackOff/Error states are always classified as CONTAINER_CRASH
	properties.Property("CrashLoopBackOff/Error states are classified as CONTAINER_CRASH", prop.ForAll(
		func(reason string, message string) bool {
			result := monitor.ClassifyK8sFailure(reason, message)
			return result == interfaces.FailureTypeContainerCrash
		},
		genContainerCrashReason(),
		gen.AnyString(),
	))

	// Property 4c: Resource-related states are always classified as RESOURCE_LIMIT
	properties.Property("Resource-related states are classified as RESOURCE_LIMIT", prop.ForAll(
		func(reason string, message string) bool {
			result := monitor.ClassifyK8sFailure(reason, message)
			return result == interfaces.FailureTypeResourceLimit
		},
		genResourceLimitReason(),
		gen.AnyString(),
	))

	// Property 4d: Classification is deterministic - same input always produces same output
	properties.Property("classification is deterministic", prop.ForAll(
		func(reason, message string) bool {
			result1 := monitor.ClassifyK8sFailure(reason, message)
			result2 := monitor.ClassifyK8sFailure(reason, message)
			return result1 == result2
		},
		gen.AnyString(),
		gen.AnyString(),
	))

	properties.TestingRun(t)
}

// TestProperty_DetectFailureRecordsAllRequiredFields tests that detectFailure records all required fields
//
// Property: For any failure state, detectFailure SHALL return a WorkerFailureInfo with
// all required fields populated: Type, Reason, Message, and OccurredAt timestamp.
//
// Feature: image-validation-and-status, Property 4: Pod status tracking completeness
// **Validates: Requirements 3.1, 3.2, 3.3**
func TestProperty_DetectFailureRecordsAllRequiredFields(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 50

	properties := gopter.NewProperties(parameters)
	monitor := &K8sWorkerStatusMonitor{}

	// Property: For any failure PodInfo, detectFailure returns WorkerFailureInfo with all required fields
	properties.Property("detectFailure records all required fields for failure states", prop.ForAll(
		func(podName, reason, message, status, phase string) bool {
			info := &interfaces.PodInfo{
				Name:    podName,
				Reason:  reason,
				Message: message,
				Status:  status,
				Phase:   phase,
			}

			result := monitor.DetectFailure(info)
			if result == nil {
				// Not a failure state, which is valid
				return true
			}

			// All required fields must be populated
			// Type must be a valid FailureType
			validType := result.Type == interfaces.FailureTypeImagePull ||
				result.Type == interfaces.FailureTypeContainerCrash ||
				result.Type == interfaces.FailureTypeResourceLimit ||
				result.Type == interfaces.FailureTypeTimeout ||
				result.Type == interfaces.FailureTypeUnknown

			// Reason should be preserved from input
			reasonPreserved := result.Reason == reason

			// Message should be preserved from input
			messagePreserved := result.Message == message

			// OccurredAt should be set to a recent time (within last minute)
			timestampValid := !result.OccurredAt.IsZero() &&
				time.Since(result.OccurredAt) < time.Minute

			return validType && reasonPreserved && messagePreserved && timestampValid
		},
		genPodName(),
		genFailureReason(),
		gen.AnyString(),
		genPodStatus(),
		genPodPhase(),
	))

	// Property: detectFailure always returns nil for non-failure states
	properties.Property("detectFailure returns nil for non-failure states", prop.ForAll(
		func(podName, message, phase string) bool {
			info := &interfaces.PodInfo{
				Name:    podName,
				Reason:  "Ready",
				Message: message,
				Status:  "Running",
				Phase:   phase,
			}

			result := monitor.DetectFailure(info)
			return result == nil
		},
		genPodName(),
		gen.AnyString(),
		genNonFailurePhase(),
	))

	// Property: detectFailure handles nil input gracefully
	properties.Property("detectFailure handles nil input gracefully", prop.ForAll(
		func(_ int) bool {
			result := monitor.DetectFailure(nil)
			return result == nil
		},
		gen.Const(0),
	))

	properties.TestingRun(t)
}

// TestProperty_ImagePullFailureDetection tests that image pull failures are correctly detected
//
// Property: For any PodInfo with ImagePullBackOff or ErrImagePull reason/status,
// detectFailure SHALL return a WorkerFailureInfo with Type = IMAGE_PULL_FAILED.
//
// Feature: image-validation-and-status, Property 4: Pod status tracking completeness
// **Validates: Requirements 3.2, 3.3**
func TestProperty_ImagePullFailureDetection(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 50

	properties := gopter.NewProperties(parameters)
	monitor := &K8sWorkerStatusMonitor{}

	// Property: ImagePullBackOff in reason field is detected as IMAGE_PULL_FAILED
	properties.Property("ImagePullBackOff in reason is detected as IMAGE_PULL_FAILED", prop.ForAll(
		func(podName, message string) bool {
			info := &interfaces.PodInfo{
				Name:    podName,
				Reason:  "ImagePullBackOff",
				Message: message,
				Status:  "Waiting",
			}

			result := monitor.DetectFailure(info)
			return result != nil && result.Type == interfaces.FailureTypeImagePull
		},
		genPodName(),
		gen.AnyString(),
	))

	// Property: ErrImagePull in reason field is detected as IMAGE_PULL_FAILED
	properties.Property("ErrImagePull in reason is detected as IMAGE_PULL_FAILED", prop.ForAll(
		func(podName, message string) bool {
			info := &interfaces.PodInfo{
				Name:    podName,
				Reason:  "ErrImagePull",
				Message: message,
				Status:  "Waiting",
			}

			result := monitor.DetectFailure(info)
			return result != nil && result.Type == interfaces.FailureTypeImagePull
		},
		genPodName(),
		gen.AnyString(),
	))

	// Property: InvalidImageName in reason field is detected as IMAGE_PULL_FAILED
	properties.Property("InvalidImageName in reason is detected as IMAGE_PULL_FAILED", prop.ForAll(
		func(podName, message string) bool {
			info := &interfaces.PodInfo{
				Name:    podName,
				Reason:  "InvalidImageName",
				Message: message,
				Status:  "Waiting",
			}

			result := monitor.DetectFailure(info)
			return result != nil && result.Type == interfaces.FailureTypeImagePull
		},
		genPodName(),
		gen.AnyString(),
	))

	// Property: Image pull failure in status field is detected as IMAGE_PULL_FAILED
	properties.Property("Image pull failure in status field is detected as IMAGE_PULL_FAILED", prop.ForAll(
		func(podName, message string) bool {
			info := &interfaces.PodInfo{
				Name:    podName,
				Reason:  "",
				Message: message,
				Status:  "ErrImagePull",
			}

			result := monitor.DetectFailure(info)
			return result != nil && result.Type == interfaces.FailureTypeImagePull
		},
		genPodName(),
		gen.AnyString(),
	))

	properties.TestingRun(t)
}

// TestProperty_ContainerCrashDetection tests that container crash failures are correctly detected
//
// Property: For any PodInfo with CrashLoopBackOff or Error reason/status,
// detectFailure SHALL return a WorkerFailureInfo with Type = CONTAINER_CRASH.
//
// Feature: image-validation-and-status, Property 4: Pod status tracking completeness
// **Validates: Requirements 3.2, 3.3**
func TestProperty_ContainerCrashDetection(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 50

	properties := gopter.NewProperties(parameters)
	monitor := &K8sWorkerStatusMonitor{}

	// Property: CrashLoopBackOff in reason field is detected as CONTAINER_CRASH
	properties.Property("CrashLoopBackOff in reason is detected as CONTAINER_CRASH", prop.ForAll(
		func(podName, message string) bool {
			info := &interfaces.PodInfo{
				Name:    podName,
				Reason:  "CrashLoopBackOff",
				Message: message,
				Status:  "Waiting",
			}

			result := monitor.DetectFailure(info)
			return result != nil && result.Type == interfaces.FailureTypeContainerCrash
		},
		genPodName(),
		gen.AnyString(),
	))

	// Property: Error in reason field is detected as CONTAINER_CRASH
	properties.Property("Error in reason is detected as CONTAINER_CRASH", prop.ForAll(
		func(podName, message string) bool {
			info := &interfaces.PodInfo{
				Name:    podName,
				Reason:  "Error",
				Message: message,
				Status:  "Terminated",
			}

			result := monitor.DetectFailure(info)
			return result != nil && result.Type == interfaces.FailureTypeContainerCrash
		},
		genPodName(),
		gen.AnyString(),
	))

	// Property: OOMKilled in reason field is detected as CONTAINER_CRASH
	properties.Property("OOMKilled in reason is detected as CONTAINER_CRASH", prop.ForAll(
		func(podName, message string) bool {
			info := &interfaces.PodInfo{
				Name:    podName,
				Reason:  "OOMKilled",
				Message: message,
				Status:  "Terminated",
			}

			result := monitor.DetectFailure(info)
			return result != nil && result.Type == interfaces.FailureTypeContainerCrash
		},
		genPodName(),
		gen.AnyString(),
	))

	properties.TestingRun(t)
}

// TestProperty_ResourceLimitDetection tests that resource limit failures are correctly detected
//
// Property: For any PodInfo with OutOfMemory, OutOfCpu, or Unschedulable reason,
// detectFailure SHALL return a WorkerFailureInfo with Type = RESOURCE_LIMIT.
//
// Feature: image-validation-and-status, Property 4: Pod status tracking completeness
// **Validates: Requirements 3.2, 3.3**
func TestProperty_ResourceLimitDetection(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 50

	properties := gopter.NewProperties(parameters)
	monitor := &K8sWorkerStatusMonitor{}

	// Property: OutOfMemory in reason field is detected as RESOURCE_LIMIT
	properties.Property("OutOfMemory in reason is detected as RESOURCE_LIMIT", prop.ForAll(
		func(podName, message string) bool {
			info := &interfaces.PodInfo{
				Name:    podName,
				Reason:  "OutOfMemory",
				Message: message,
				Status:  "Failed",
			}

			result := monitor.DetectFailure(info)
			return result != nil && result.Type == interfaces.FailureTypeResourceLimit
		},
		genPodName(),
		gen.AnyString(),
	))

	// Property: Unschedulable in reason field is detected as RESOURCE_LIMIT
	properties.Property("Unschedulable in reason is detected as RESOURCE_LIMIT", prop.ForAll(
		func(podName, message string) bool {
			info := &interfaces.PodInfo{
				Name:    podName,
				Reason:  "Unschedulable",
				Message: message,
				Status:  "Pending",
			}

			result := monitor.DetectFailure(info)
			return result != nil && result.Type == interfaces.FailureTypeResourceLimit
		},
		genPodName(),
		gen.AnyString(),
	))

	// Property: FailedScheduling in reason field is detected as RESOURCE_LIMIT
	properties.Property("FailedScheduling in reason is detected as RESOURCE_LIMIT", prop.ForAll(
		func(podName, message string) bool {
			info := &interfaces.PodInfo{
				Name:    podName,
				Reason:  "FailedScheduling",
				Message: message,
				Status:  "Pending",
			}

			result := monitor.DetectFailure(info)
			return result != nil && result.Type == interfaces.FailureTypeResourceLimit
		},
		genPodName(),
		gen.AnyString(),
	))

	properties.TestingRun(t)
}

// TestProperty_ClassificationConsistency tests that classification is consistent across different input variations
//
// Property: Classification should be consistent regardless of message content when reason is explicit.
//
// Feature: image-validation-and-status, Property 4: Pod status tracking completeness
// **Validates: Requirements 3.2, 3.3**
func TestProperty_ClassificationConsistency(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 50

	properties := gopter.NewProperties(parameters)
	monitor := &K8sWorkerStatusMonitor{}

	// Property: Same reason with different messages produces same classification
	properties.Property("same reason with different messages produces same classification", prop.ForAll(
		func(reason, message1, message2 string) bool {
			result1 := monitor.ClassifyK8sFailure(reason, message1)
			result2 := monitor.ClassifyK8sFailure(reason, message2)
			return result1 == result2
		},
		genKnownFailureReason(),
		gen.AnyString(),
		gen.AnyString(),
	))

	// Property: Classification result is always a valid FailureType
	properties.Property("classification result is always a valid FailureType", prop.ForAll(
		func(reason, message string) bool {
			result := monitor.ClassifyK8sFailure(reason, message)
			return result == interfaces.FailureTypeImagePull ||
				result == interfaces.FailureTypeContainerCrash ||
				result == interfaces.FailureTypeResourceLimit ||
				result == interfaces.FailureTypeTimeout ||
				result == interfaces.FailureTypeUnknown
		},
		gen.AnyString(),
		gen.AnyString(),
	))

	// Property: Empty reason and message produces UNKNOWN type
	properties.Property("empty reason and message produces UNKNOWN type", prop.ForAll(
		func(_ int) bool {
			result := monitor.ClassifyK8sFailure("", "")
			return result == interfaces.FailureTypeUnknown
		},
		gen.Const(0),
	))

	properties.TestingRun(t)
}

// TestProperty_IsFailureStateConsistency tests the isFailureState function consistency
//
// Property: isFailureState should be consistent and deterministic for all inputs.
//
// Feature: image-validation-and-status, Property 4: Pod status tracking completeness
// **Validates: Requirements 3.1, 3.2**
func TestProperty_IsFailureStateConsistency(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 50

	properties := gopter.NewProperties(parameters)

	// Property: isFailureState is deterministic
	properties.Property("isFailureState is deterministic", prop.ForAll(
		func(reason, status string) bool {
			result1 := isFailureState(reason, status)
			result2 := isFailureState(reason, status)
			return result1 == result2
		},
		gen.AnyString(),
		gen.AnyString(),
	))

	// Property: Known failure reasons always return true
	properties.Property("known failure reasons always return true", prop.ForAll(
		func(reason string) bool {
			return isFailureState(reason, "")
		},
		genKnownFailureReason(),
	))

	// Property: Normal running states return false
	properties.Property("normal running states return false", prop.ForAll(
		func(status string) bool {
			return !isFailureState("Ready", status)
		},
		genNonFailureStatus(),
	))

	// Property: "Failed" status always returns true
	properties.Property("Failed status always returns true", prop.ForAll(
		func(reason string) bool {
			return isFailureState(reason, "Failed")
		},
		genNonFailureReason(),
	))

	properties.TestingRun(t)
}

// ============================================================================
// Generators for K8s-specific values
// ============================================================================

// genImagePullFailureReason generates K8s reasons that indicate image pull failures
func genImagePullFailureReason() gopter.Gen {
	return gen.OneConstOf(
		"ImagePullBackOff",
		"ErrImagePull",
		"InvalidImageName",
		"ImageInspectError",
	)
}

// genContainerCrashReason generates K8s reasons that indicate container crashes
func genContainerCrashReason() gopter.Gen {
	return gen.OneConstOf(
		"CrashLoopBackOff",
		"Error",
		"OOMKilled",
		"ContainerCannotRun",
		"CreateContainerError",
	)
}

// genResourceLimitReason generates K8s reasons that indicate resource limit issues
func genResourceLimitReason() gopter.Gen {
	return gen.OneConstOf(
		"OutOfMemory",
		"OutOfCpu",
		"Unschedulable",
		"FailedScheduling",
	)
}

// genKnownFailureReason generates all known K8s failure reasons
func genKnownFailureReason() gopter.Gen {
	return gen.OneConstOf(
		// Image pull failures
		"ImagePullBackOff",
		"ErrImagePull",
		"InvalidImageName",
		"ImageInspectError",
		// Container crash failures
		"CrashLoopBackOff",
		"Error",
		"OOMKilled",
		"ContainerCannotRun",
		"CreateContainerError",
		// Resource limit failures
		"OutOfMemory",
		"OutOfCpu",
		"Unschedulable",
		"FailedScheduling",
		// Other failures
		"FailedMount",
		"FailedAttachVolume",
	)
}

// genFailureReason generates failure reasons including known and random ones
func genFailureReason() gopter.Gen {
	return gen.OneGenOf(
		genKnownFailureReason(),
		gen.AnyString(),
	)
}

// genNonFailureReason generates reasons that are not failure states
func genNonFailureReason() gopter.Gen {
	return gen.OneConstOf(
		"Ready",
		"Started",
		"Scheduled",
		"Pulled",
		"Created",
		"ContainerCreating",
		"PodInitializing",
	)
}

// genPodName generates realistic pod names
func genPodName() gopter.Gen {
	return gen.OneGenOf(
		// Simple pod name
		gen.RegexMatch(`[a-z][a-z0-9]{2,10}`),
		// Deployment-style pod name: name-hash-hash
		gopter.CombineGens(
			gen.RegexMatch(`[a-z][a-z0-9]{2,8}`),
			gen.RegexMatch(`[a-f0-9]{5,10}`),
			gen.RegexMatch(`[a-z0-9]{5}`),
		).Map(func(vals []any) string {
			return vals[0].(string) + "-" + vals[1].(string) + "-" + vals[2].(string)
		}),
	)
}

// genPodStatus generates pod status values
func genPodStatus() gopter.Gen {
	return gen.OneConstOf(
		"Running",
		"Waiting",
		"Terminated",
		"Creating",
		"Pending",
		"Failed",
		"ImagePullBackOff",
		"ErrImagePull",
		"CrashLoopBackOff",
	)
}

// genNonFailureStatus generates non-failure status values
func genNonFailureStatus() gopter.Gen {
	return gen.OneConstOf(
		"Running",
		"Pending",
		"Creating",
		"ContainerCreating",
		"PodInitializing",
	)
}

// genPodPhase generates pod phase values
func genPodPhase() gopter.Gen {
	return gen.OneConstOf(
		"Pending",
		"Running",
		"Succeeded",
		"Failed",
		"Unknown",
	)
}

// genNonFailurePhase generates non-failure phase values
func genNonFailurePhase() gopter.Gen {
	return gen.OneConstOf(
		"Pending",
		"Running",
		"Succeeded",
	)
}
