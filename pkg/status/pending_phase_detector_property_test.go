// Package status provides property-based tests for the PendingPhaseDetector.
// These tests verify universal properties that should hold across all valid inputs.
//
// Feature: endpoint-status-tracking, Property 1: Pending Phase Classification
package status

import (
	"testing"

	"waverless/pkg/interfaces"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// TestProperty_PendingPhaseClassification tests Property 1: Pending Phase Classification
//
// Property: For any Pod in Pending status with valid conditions and container statuses,
// the PendingPhaseDetector SHALL classify it into exactly one of the valid phases
// (SCHEDULING, WAITING_NODE, PULLING_IMAGE, INITIALIZING), and the classification
// SHALL be deterministic based on the input conditions.
//
// Specifically:
// - If PodScheduled condition is False with reason "Unschedulable", the phase SHALL be WAITING_NODE
// - If container status is Waiting with reason "ContainerCreating" or "ImagePullBackOff", the phase SHALL be PULLING_IMAGE
// - If init containers are running, the phase SHALL be INITIALIZING
// - Otherwise, the phase SHALL be SCHEDULING
//
// Feature: endpoint-status-tracking, Property 1: Pending Phase Classification
func TestProperty_PendingPhaseClassification(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 50

	properties := gopter.NewProperties(parameters)
	detector := NewPendingPhaseDetector(nil)

	// Property 1a: Result is always one of the valid phases
	properties.Property("result is always one of the valid phases", prop.ForAll(
		func(podInfo *interfaces.PodInfo, conditions []interfaces.PodCondition) bool {
			result := detector.DetectPhase(podInfo, conditions)
			if result == nil {
				return false
			}
			return result.Phase.IsValid()
		},
		genPendingPodInfo(),
		genPendingPodConditions(),
	))

	// Property 1b: Classification is deterministic
	properties.Property("classification is deterministic", prop.ForAll(
		func(podInfo *interfaces.PodInfo, conditions []interfaces.PodCondition) bool {
			result1 := detector.DetectPhase(podInfo, conditions)
			result2 := detector.DetectPhase(podInfo, conditions)
			if result1 == nil || result2 == nil {
				return result1 == nil && result2 == nil
			}
			return result1.Phase == result2.Phase
		},
		genPendingPodInfo(),
		genPendingPodConditions(),
	))

	// Property 1c: Unschedulable condition always results in WAITING_NODE
	properties.Property("unschedulable condition always results in WAITING_NODE", prop.ForAll(
		func(podInfo *interfaces.PodInfo, otherConditions []interfaces.PodCondition, message string) bool {
			// Create conditions with Unschedulable
			conditions := append(otherConditions, interfaces.PodCondition{
				Type:    "PodScheduled",
				Status:  "False",
				Reason:  "Unschedulable",
				Message: message,
			})

			result := detector.DetectPhase(podInfo, conditions)
			if result == nil {
				return false
			}
			return result.Phase == PendingPhaseWaitingNode
		},
		genPendingPodInfoWithoutImagePullStatus(),
		genPendingNonUnschedulableConditions(),
		gen.AnyString(),
	))

	// Property 1d: ContainerCreating reason always results in PULLING_IMAGE (when not unschedulable)
	properties.Property("ContainerCreating reason results in PULLING_IMAGE when not unschedulable", prop.ForAll(
		func(podName, message string) bool {
			podInfo := &interfaces.PodInfo{
				Name:    podName,
				Phase:   "Pending",
				Status:  "ContainerCreating",
				Reason:  "ContainerCreating",
				Message: message,
			}

			// No unschedulable condition
			conditions := []interfaces.PodCondition{
				{
					Type:   "PodScheduled",
					Status: "True",
				},
			}

			result := detector.DetectPhase(podInfo, conditions)
			if result == nil {
				return false
			}
			return result.Phase == PendingPhasePullingImage
		},
		genPendingPodName(),
		gen.AnyString(),
	))

	// Property 1e: ImagePullBackOff reason always results in PULLING_IMAGE (when not unschedulable)
	properties.Property("ImagePullBackOff reason results in PULLING_IMAGE when not unschedulable", prop.ForAll(
		func(podName, message string) bool {
			podInfo := &interfaces.PodInfo{
				Name:    podName,
				Phase:   "Pending",
				Status:  "ImagePullBackOff",
				Reason:  "ImagePullBackOff",
				Message: message,
			}

			// No unschedulable condition
			conditions := []interfaces.PodCondition{
				{
					Type:   "PodScheduled",
					Status: "True",
				},
			}

			result := detector.DetectPhase(podInfo, conditions)
			if result == nil {
				return false
			}
			return result.Phase == PendingPhasePullingImage
		},
		genPendingPodName(),
		gen.AnyString(),
	))

	// Property 1f: PodInitializing reason results in INITIALIZING (when not unschedulable or pulling image)
	properties.Property("PodInitializing reason results in INITIALIZING when not unschedulable or pulling image", prop.ForAll(
		func(podName, message string) bool {
			podInfo := &interfaces.PodInfo{
				Name:    podName,
				Phase:   "Pending",
				Status:  "Init:0/1",
				Reason:  "PodInitializing",
				Message: message,
			}

			// No unschedulable condition
			conditions := []interfaces.PodCondition{
				{
					Type:   "PodScheduled",
					Status: "True",
				},
			}

			result := detector.DetectPhase(podInfo, conditions)
			if result == nil {
				return false
			}
			return result.Phase == PendingPhaseInitializing
		},
		genPendingPodName(),
		gen.AnyString(),
	))

	// Property 1g: Init:X/Y status format results in INITIALIZING
	properties.Property("Init status format results in INITIALIZING when not unschedulable or pulling image", prop.ForAll(
		func(podName string, initCurrent, initTotal int) bool {
			// Ensure valid init container counts
			if initTotal < 1 {
				initTotal = 1
			}
			if initCurrent < 0 {
				initCurrent = 0
			}
			if initCurrent > initTotal {
				initCurrent = initTotal
			}

			podInfo := &interfaces.PodInfo{
				Name:   podName,
				Phase:  "Pending",
				Status: "Init:" + string(rune('0'+initCurrent)) + "/" + string(rune('0'+initTotal)),
			}

			// No unschedulable condition
			conditions := []interfaces.PodCondition{
				{
					Type:   "PodScheduled",
					Status: "True",
				},
			}

			result := detector.DetectPhase(podInfo, conditions)
			if result == nil {
				return false
			}
			return result.Phase == PendingPhaseInitializing
		},
		genPendingPodName(),
		gen.IntRange(0, 5),
		gen.IntRange(1, 5),
	))

	// Property 1h: Default case results in SCHEDULING
	properties.Property("default case results in SCHEDULING", prop.ForAll(
		func(podName string) bool {
			podInfo := &interfaces.PodInfo{
				Name:   podName,
				Phase:  "Pending",
				Status: "Pending",
			}

			// No special conditions
			conditions := []interfaces.PodCondition{}

			result := detector.DetectPhase(podInfo, conditions)
			if result == nil {
				return false
			}
			return result.Phase == PendingPhaseScheduling
		},
		genPendingPodName(),
	))

	// Property 1i: Nil podInfo results in SCHEDULING
	properties.Property("nil podInfo results in SCHEDULING", prop.ForAll(
		func(conditions []interfaces.PodCondition) bool {
			result := detector.DetectPhase(nil, conditions)
			if result == nil {
				return false
			}
			return result.Phase == PendingPhaseScheduling
		},
		genPendingPodConditions(),
	))

	properties.TestingRun(t)
}

// TestProperty_PendingPhasePriorityOrder tests that detection follows the correct priority order
//
// Property: The detection priority SHALL be:
// 1. Unschedulable → WAITING_NODE (highest priority)
// 2. ContainerCreating/ImagePullBackOff → PULLING_IMAGE
// 3. Init containers → INITIALIZING
// 4. Default → SCHEDULING (lowest priority)
//
// Feature: endpoint-status-tracking, Property 1: Pending Phase Classification
func TestProperty_PendingPhasePriorityOrder(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 50

	properties := gopter.NewProperties(parameters)
	detector := NewPendingPhaseDetector(nil)

	// Property: Unschedulable takes priority over ContainerCreating
	properties.Property("unschedulable takes priority over ContainerCreating", prop.ForAll(
		func(podName, message string) bool {
			// Pod has ContainerCreating status but also Unschedulable condition
			podInfo := &interfaces.PodInfo{
				Name:    podName,
				Phase:   "Pending",
				Status:  "ContainerCreating",
				Reason:  "ContainerCreating",
				Message: message,
			}

			conditions := []interfaces.PodCondition{
				{
					Type:   "PodScheduled",
					Status: "False",
					Reason: "Unschedulable",
				},
			}

			result := detector.DetectPhase(podInfo, conditions)
			if result == nil {
				return false
			}
			// Unschedulable should take priority
			return result.Phase == PendingPhaseWaitingNode
		},
		genPendingPodName(),
		gen.AnyString(),
	))

	// Property: Unschedulable takes priority over Init status
	properties.Property("unschedulable takes priority over Init status", prop.ForAll(
		func(podName, message string) bool {
			// Pod has Init status but also Unschedulable condition
			podInfo := &interfaces.PodInfo{
				Name:    podName,
				Phase:   "Pending",
				Status:  "Init:0/1",
				Reason:  "PodInitializing",
				Message: message,
			}

			conditions := []interfaces.PodCondition{
				{
					Type:   "PodScheduled",
					Status: "False",
					Reason: "Unschedulable",
				},
			}

			result := detector.DetectPhase(podInfo, conditions)
			if result == nil {
				return false
			}
			// Unschedulable should take priority
			return result.Phase == PendingPhaseWaitingNode
		},
		genPendingPodName(),
		gen.AnyString(),
	))

	// Property: ContainerCreating takes priority over Init status
	properties.Property("ContainerCreating takes priority over Init status", prop.ForAll(
		func(podName, message string) bool {
			// Pod has both ContainerCreating reason and Init status
			// Note: In practice this is unlikely, but we test the priority
			podInfo := &interfaces.PodInfo{
				Name:    podName,
				Phase:   "Pending",
				Status:  "Init:0/1",
				Reason:  "ContainerCreating", // ContainerCreating reason
				Message: message,
			}

			conditions := []interfaces.PodCondition{
				{
					Type:   "PodScheduled",
					Status: "True",
				},
			}

			result := detector.DetectPhase(podInfo, conditions)
			if result == nil {
				return false
			}
			// ContainerCreating should take priority over Init
			return result.Phase == PendingPhasePullingImage
		},
		genPendingPodName(),
		gen.AnyString(),
	))

	properties.TestingRun(t)
}

// TestProperty_PendingPhaseFromPodDetail tests detection from PodDetail with container statuses
//
// Property: When using DetectPhaseFromPodDetail, the detector SHALL correctly identify
// phases from container statuses in addition to pod-level information.
//
// Feature: endpoint-status-tracking, Property 1: Pending Phase Classification
func TestProperty_PendingPhaseFromPodDetail(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 50

	properties := gopter.NewProperties(parameters)
	detector := NewPendingPhaseDetector(nil)

	// Property: Container with ContainerCreating state results in PULLING_IMAGE
	properties.Property("container with ContainerCreating state results in PULLING_IMAGE", prop.ForAll(
		func(podName, containerName string) bool {
			podDetail := &interfaces.PodDetail{
				PodInfo: &interfaces.PodInfo{
					Name:   podName,
					Phase:  "Pending",
					Status: "Pending",
				},
				Containers: []interfaces.ContainerInfo{
					{
						Name:   containerName,
						State:  "Waiting",
						Reason: "ContainerCreating",
					},
				},
				Conditions: []interfaces.PodCondition{
					{
						Type:   "PodScheduled",
						Status: "True",
					},
				},
			}

			result := detector.DetectPhaseFromPodDetail(podDetail)
			if result == nil {
				return false
			}
			return result.Phase == PendingPhasePullingImage
		},
		genPendingPodName(),
		genPendingContainerName(),
	))

	// Property: Container with ImagePullBackOff state results in PULLING_IMAGE
	properties.Property("container with ImagePullBackOff state results in PULLING_IMAGE", prop.ForAll(
		func(podName, containerName string) bool {
			podDetail := &interfaces.PodDetail{
				PodInfo: &interfaces.PodInfo{
					Name:   podName,
					Phase:  "Pending",
					Status: "Pending",
				},
				Containers: []interfaces.ContainerInfo{
					{
						Name:   containerName,
						State:  "Waiting",
						Reason: "ImagePullBackOff",
					},
				},
				Conditions: []interfaces.PodCondition{
					{
						Type:   "PodScheduled",
						Status: "True",
					},
				},
			}

			result := detector.DetectPhaseFromPodDetail(podDetail)
			if result == nil {
				return false
			}
			return result.Phase == PendingPhasePullingImage
		},
		genPendingPodName(),
		genPendingContainerName(),
	))

	// Property: Running init container results in INITIALIZING
	properties.Property("running init container results in INITIALIZING", prop.ForAll(
		func(podName, initContainerName string) bool {
			podDetail := &interfaces.PodDetail{
				PodInfo: &interfaces.PodInfo{
					Name:   podName,
					Phase:  "Pending",
					Status: "Pending",
				},
				InitContainers: []interfaces.ContainerInfo{
					{
						Name:  initContainerName,
						State: "Running",
						Ready: false,
					},
				},
				Containers: []interfaces.ContainerInfo{
					{
						Name:  "main",
						State: "Waiting",
						Ready: false,
					},
				},
				Conditions: []interfaces.PodCondition{
					{
						Type:   "PodScheduled",
						Status: "True",
					},
				},
			}

			result := detector.DetectPhaseFromPodDetail(podDetail)
			if result == nil {
				return false
			}
			return result.Phase == PendingPhaseInitializing
		},
		genPendingPodName(),
		genPendingContainerName(),
	))

	// Property: Waiting init container (not ready) results in INITIALIZING
	properties.Property("waiting init container not ready results in INITIALIZING", prop.ForAll(
		func(podName, initContainerName string) bool {
			podDetail := &interfaces.PodDetail{
				PodInfo: &interfaces.PodInfo{
					Name:   podName,
					Phase:  "Pending",
					Status: "Pending",
				},
				InitContainers: []interfaces.ContainerInfo{
					{
						Name:  initContainerName,
						State: "Waiting",
						Ready: false,
					},
				},
				Containers: []interfaces.ContainerInfo{
					{
						Name:  "main",
						State: "Waiting",
						Ready: false,
					},
				},
				Conditions: []interfaces.PodCondition{
					{
						Type:   "PodScheduled",
						Status: "True",
					},
				},
			}

			result := detector.DetectPhaseFromPodDetail(podDetail)
			if result == nil {
				return false
			}
			return result.Phase == PendingPhaseInitializing
		},
		genPendingPodName(),
		genPendingContainerName(),
	))

	// Property: Nil PodDetail results in SCHEDULING
	properties.Property("nil PodDetail results in SCHEDULING", prop.ForAll(
		func(_ int) bool {
			result := detector.DetectPhaseFromPodDetail(nil)
			if result == nil {
				return false
			}
			return result.Phase == PendingPhaseScheduling
		},
		gen.Const(0),
	))

	properties.TestingRun(t)
}

// TestProperty_PendingPhaseInfoFields tests that PendingPhaseInfo fields are properly set
//
// Property: The returned PendingPhaseInfo SHALL always have:
// - A valid Phase
// - A non-zero Since timestamp
//
// Feature: endpoint-status-tracking, Property 1: Pending Phase Classification
func TestProperty_PendingPhaseInfoFields(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 50

	properties := gopter.NewProperties(parameters)
	detector := NewPendingPhaseDetector(nil)

	// Property: Result always has valid phase and non-zero timestamp
	properties.Property("result always has valid phase and non-zero timestamp", prop.ForAll(
		func(podInfo *interfaces.PodInfo, conditions []interfaces.PodCondition) bool {
			result := detector.DetectPhase(podInfo, conditions)
			if result == nil {
				return false
			}
			return result.Phase.IsValid() && !result.Since.IsZero()
		},
		genPendingPodInfo(),
		genPendingPodConditions(),
	))

	// Property: Reason is preserved from Unschedulable condition
	properties.Property("reason is preserved from Unschedulable condition", prop.ForAll(
		func(podName, message string) bool {
			podInfo := &interfaces.PodInfo{
				Name:   podName,
				Phase:  "Pending",
				Status: "Pending",
			}

			conditions := []interfaces.PodCondition{
				{
					Type:    "PodScheduled",
					Status:  "False",
					Reason:  "Unschedulable",
					Message: message,
				},
			}

			result := detector.DetectPhase(podInfo, conditions)
			if result == nil {
				return false
			}
			return result.Reason == "Unschedulable" && result.Message == message
		},
		genPendingPodName(),
		gen.AnyString(),
	))

	properties.TestingRun(t)
}

// ============================================================================
// Generators for PendingPhaseDetector property tests
// ============================================================================

// genPendingPodName generates valid pod names for pending phase tests
func genPendingPodName() gopter.Gen {
	return gen.RegexMatch(`[a-z][a-z0-9]{2,20}`).SuchThat(func(s string) bool {
		return len(s) >= 3
	})
}

// genPendingContainerName generates valid container names for pending phase tests
func genPendingContainerName() gopter.Gen {
	return gen.RegexMatch(`[a-z][a-z0-9]{2,15}`).SuchThat(func(s string) bool {
		return len(s) >= 3
	})
}

// genPendingPodInfo generates random PodInfo structures for pending phase tests
func genPendingPodInfo() gopter.Gen {
	return gopter.CombineGens(
		genPendingPodName(),
		genPendingPodPhase(),
		genPendingPodStatus(),
		genPendingPodReason(),
		gen.AnyString(),
	).Map(func(vals []interface{}) *interfaces.PodInfo {
		return &interfaces.PodInfo{
			Name:    vals[0].(string),
			Phase:   vals[1].(string),
			Status:  vals[2].(string),
			Reason:  vals[3].(string),
			Message: vals[4].(string),
		}
	})
}

// genPendingPodInfoWithoutImagePullStatus generates PodInfo without image pull related status
func genPendingPodInfoWithoutImagePullStatus() gopter.Gen {
	return gopter.CombineGens(
		genPendingPodName(),
		gen.AnyString(),
	).Map(func(vals []interface{}) *interfaces.PodInfo {
		return &interfaces.PodInfo{
			Name:    vals[0].(string),
			Phase:   "Pending",
			Status:  "Pending",
			Reason:  "",
			Message: vals[1].(string),
		}
	})
}

// genPendingPodPhase generates valid pod phases
func genPendingPodPhase() gopter.Gen {
	return gen.OneConstOf(
		"Pending",
		"Running",
		"Succeeded",
		"Failed",
		"Unknown",
	)
}

// genPendingPodStatus generates various pod statuses
func genPendingPodStatus() gopter.Gen {
	return gen.OneConstOf(
		"Pending",
		"ContainerCreating",
		"ImagePullBackOff",
		"Init:0/1",
		"Init:1/2",
		"Running",
		"Terminating",
		"",
	)
}

// genPendingPodReason generates various pod reasons
func genPendingPodReason() gopter.Gen {
	return gen.OneConstOf(
		"",
		"ContainerCreating",
		"ImagePullBackOff",
		"PodInitializing",
		"Unschedulable",
		"NodeNotReady",
	)
}

// genPendingPodConditions generates a slice of pod conditions for pending phase tests
func genPendingPodConditions() gopter.Gen {
	return gen.SliceOfN(3, genPendingPodCondition())
}

// genPendingPodCondition generates a single pod condition for pending phase tests
func genPendingPodCondition() gopter.Gen {
	return gopter.CombineGens(
		genPendingConditionType(),
		genPendingConditionStatus(),
		genPendingConditionReason(),
		gen.AnyString(),
	).Map(func(vals []interface{}) interfaces.PodCondition {
		return interfaces.PodCondition{
			Type:    vals[0].(string),
			Status:  vals[1].(string),
			Reason:  vals[2].(string),
			Message: vals[3].(string),
		}
	})
}

// genPendingNonUnschedulableConditions generates conditions that don't include Unschedulable
func genPendingNonUnschedulableConditions() gopter.Gen {
	return gen.SliceOfN(2, genPendingNonUnschedulableCondition())
}

// genPendingNonUnschedulableCondition generates a condition that is not Unschedulable
func genPendingNonUnschedulableCondition() gopter.Gen {
	return gopter.CombineGens(
		genPendingNonPodScheduledConditionType(),
		genPendingConditionStatus(),
		gen.AnyString(),
	).Map(func(vals []interface{}) interfaces.PodCondition {
		return interfaces.PodCondition{
			Type:    vals[0].(string),
			Status:  vals[1].(string),
			Reason:  "",
			Message: vals[2].(string),
		}
	})
}

// genPendingConditionType generates valid condition types
func genPendingConditionType() gopter.Gen {
	return gen.OneConstOf(
		"PodScheduled",
		"Initialized",
		"Ready",
		"ContainersReady",
	)
}

// genPendingNonPodScheduledConditionType generates condition types that are not PodScheduled
func genPendingNonPodScheduledConditionType() gopter.Gen {
	return gen.OneConstOf(
		"Initialized",
		"Ready",
		"ContainersReady",
	)
}

// genPendingConditionStatus generates valid condition statuses
func genPendingConditionStatus() gopter.Gen {
	return gen.OneConstOf(
		"True",
		"False",
		"Unknown",
	)
}

// genPendingConditionReason generates valid condition reasons
func genPendingConditionReason() gopter.Gen {
	return gen.OneConstOf(
		"",
		"Unschedulable",
		"NodeNotReady",
		"PodCompleted",
	)
}
