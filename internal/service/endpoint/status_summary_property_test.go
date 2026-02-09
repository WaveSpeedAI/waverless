// Package endpoint provides property-based tests for the EndpointStatusSummary.
// These tests verify universal properties that should hold across all valid inputs.
//
// Feature: endpoint-status-tracking, Property 6: Endpoint Status Summary Computation
package endpoint

import (
	"testing"
	"time"

	"waverless/pkg/status"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// TestProperty_EndpointStatusSummaryComputation tests Property 6: Endpoint Status Summary Computation
//
// Property: For any Endpoint with a set of Workers, the computed EndpointStatusSummary SHALL satisfy:
// - total_workers equals the count of all workers
// - workers_by_status sums to total_workers
// - workers_by_phase counts only workers in PENDING status
// - If any worker has a failure, failure_details SHALL contain that worker's failure information
// - The summary SHALL be updated whenever any worker's status changes
//
// Feature: endpoint-status-tracking, Property 6: Endpoint Status Summary Computation
func TestProperty_EndpointStatusSummaryComputation(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 50

	properties := gopter.NewProperties(parameters)

	// Property 6a: total_workers equals the count of all workers added via AddWorkerStatus
	properties.Property("total_workers equals count of all workers added", prop.ForAll(
		func(statuses []string) bool {
			summary := NewEndpointStatusSummary()

			for _, s := range statuses {
				summary.AddWorkerStatus(s)
			}

			return summary.TotalWorkers == len(statuses)
		},
		genWorkerStatusList(),
	))

	// Property 6b: workers_by_status sums to total_workers
	properties.Property("workers_by_status sums to total_workers", prop.ForAll(
		func(statuses []string) bool {
			summary := NewEndpointStatusSummary()

			for _, s := range statuses {
				summary.AddWorkerStatus(s)
			}

			// Sum all values in WorkersByStatus
			sum := 0
			for _, count := range summary.WorkersByStatus {
				sum += count
			}

			return sum == summary.TotalWorkers
		},
		genWorkerStatusList(),
	))

	// Property 6c: workers_by_phase counts are independent of workers_by_status
	properties.Property("workers_by_phase counts are tracked independently", prop.ForAll(
		func(phases []status.PendingPhase) bool {
			summary := NewEndpointStatusSummary()

			for _, phase := range phases {
				summary.AddWorkerPhase(phase)
			}

			// Sum all values in WorkersByPhase
			sum := 0
			for _, count := range summary.WorkersByPhase {
				sum += count
			}

			return sum == len(phases)
		},
		genPendingPhaseList(),
	))

	// Property 6d: AddWorkerStatus correctly increments both TotalWorkers and status count
	properties.Property("AddWorkerStatus correctly increments counters", prop.ForAll(
		func(statusName string, count int) bool {
			summary := NewEndpointStatusSummary()

			for i := 0; i < count; i++ {
				summary.AddWorkerStatus(statusName)
			}

			return summary.TotalWorkers == count &&
				summary.WorkersByStatus[statusName] == count
		},
		genWorkerStatusName(),
		gen.IntRange(1, 20),
	))

	// Property 6e: AddWorkerPhase correctly increments phase count
	properties.Property("AddWorkerPhase correctly increments phase count", prop.ForAll(
		func(phase status.PendingPhase, count int) bool {
			summary := NewEndpointStatusSummary()

			for i := 0; i < count; i++ {
				summary.AddWorkerPhase(phase)
			}

			return summary.WorkersByPhase[string(phase)] == count
		},
		genPendingPhase(),
		gen.IntRange(1, 20),
	))

	// Property 6f: failure_details contains all added failure information
	properties.Property("failure_details contains all added failures", prop.ForAll(
		func(failures []WorkerFailureDetail) bool {
			summary := NewEndpointStatusSummary()

			for _, failure := range failures {
				summary.AddFailureDetail(failure)
			}

			// Check that all failures are present
			if len(summary.FailureDetails) != len(failures) {
				return false
			}

			// Verify each failure is in the summary
			for i, failure := range failures {
				if summary.FailureDetails[i].WorkerID != failure.WorkerID ||
					summary.FailureDetails[i].FailureType != failure.FailureType {
					return false
				}
			}

			return true
		},
		genWorkerFailureDetailList(),
	))

	// Property 6g: pending_details contains all added pending information
	properties.Property("pending_details contains all added pending workers", prop.ForAll(
		func(pendingDetails []WorkerPendingDetail) bool {
			summary := NewEndpointStatusSummary()

			for _, detail := range pendingDetails {
				summary.AddPendingDetail(detail)
			}

			// Check that all pending details are present
			if len(summary.PendingDetails) != len(pendingDetails) {
				return false
			}

			// Verify each pending detail is in the summary
			for i, detail := range pendingDetails {
				if summary.PendingDetails[i].WorkerID != detail.WorkerID ||
					summary.PendingDetails[i].Phase != detail.Phase {
					return false
				}
			}

			return true
		},
		genWorkerPendingDetailList(),
	))

	// Property 6h: HasFailedWorkers returns true iff there are failure details
	properties.Property("HasFailedWorkers returns true iff there are failures", prop.ForAll(
		func(failures []WorkerFailureDetail) bool {
			summary := NewEndpointStatusSummary()

			for _, failure := range failures {
				summary.AddFailureDetail(failure)
			}

			return summary.HasFailedWorkers() == (len(failures) > 0)
		},
		genWorkerFailureDetailList(),
	))

	// Property 6i: HasPendingWorkers returns true iff there are pending details
	properties.Property("HasPendingWorkers returns true iff there are pending workers", prop.ForAll(
		func(pendingDetails []WorkerPendingDetail) bool {
			summary := NewEndpointStatusSummary()

			for _, detail := range pendingDetails {
				summary.AddPendingDetail(detail)
			}

			return summary.HasPendingWorkers() == (len(pendingDetails) > 0)
		},
		genWorkerPendingDetailList(),
	))

	// Property 6j: SpotCapacity is correctly set and retrieved
	properties.Property("SpotCapacity is correctly set and retrieved", prop.ForAll(
		func(score int, price float64, instanceType string) bool {
			summary := NewEndpointStatusSummary()

			spotStatus := status.NewSpotStatus(score, price, instanceType)
			summary.SetSpotCapacity(spotStatus)

			return summary.SpotCapacity != nil &&
				summary.SpotCapacity.Score == score &&
				summary.SpotCapacity.Price == price &&
				summary.SpotCapacity.InstanceType == instanceType
		},
		gen.IntRange(1, 10),
		gen.Float64Range(0.01, 10.0),
		genInstanceType(),
	))

	properties.TestingRun(t)
}

// TestProperty_EndpointStatusSummaryMixedWorkers tests summary computation with mixed worker states
//
// Property: When workers have mixed statuses and phases, the summary SHALL correctly
// aggregate all information while maintaining consistency between counts and details.
//
// Feature: endpoint-status-tracking, Property 6: Endpoint Status Summary Computation
func TestProperty_EndpointStatusSummaryMixedWorkers(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 50

	properties := gopter.NewProperties(parameters)

	// Property: Mixed worker statuses are correctly counted
	properties.Property("mixed worker statuses are correctly counted", prop.ForAll(
		func(onlineCount, pendingCount, failedCount int) bool {
			summary := NewEndpointStatusSummary()

			// Add ONLINE workers
			for i := 0; i < onlineCount; i++ {
				summary.AddWorkerStatus("ONLINE")
			}

			// Add PENDING workers
			for i := 0; i < pendingCount; i++ {
				summary.AddWorkerStatus("PENDING")
			}

			// Add FAILED workers
			for i := 0; i < failedCount; i++ {
				summary.AddWorkerStatus("FAILED")
			}

			totalExpected := onlineCount + pendingCount + failedCount

			return summary.TotalWorkers == totalExpected &&
				summary.GetOnlineCount() == onlineCount &&
				summary.GetPendingCount() == pendingCount &&
				summary.GetFailedCount() == failedCount
		},
		gen.IntRange(0, 10),
		gen.IntRange(0, 10),
		gen.IntRange(0, 10),
	))

	// Property: Mixed pending phases are correctly counted
	properties.Property("mixed pending phases are correctly counted", prop.ForAll(
		func(schedulingCount, waitingNodeCount, pullingImageCount, initializingCount int) bool {
			summary := NewEndpointStatusSummary()

			// Add SCHEDULING phase workers
			for i := 0; i < schedulingCount; i++ {
				summary.AddWorkerPhase(status.PendingPhaseScheduling)
			}

			// Add WAITING_NODE phase workers
			for i := 0; i < waitingNodeCount; i++ {
				summary.AddWorkerPhase(status.PendingPhaseWaitingNode)
			}

			// Add PULLING_IMAGE phase workers
			for i := 0; i < pullingImageCount; i++ {
				summary.AddWorkerPhase(status.PendingPhasePullingImage)
			}

			// Add INITIALIZING phase workers
			for i := 0; i < initializingCount; i++ {
				summary.AddWorkerPhase(status.PendingPhaseInitializing)
			}

			totalPhases := schedulingCount + waitingNodeCount + pullingImageCount + initializingCount

			// Sum all phase counts
			sum := 0
			for _, count := range summary.WorkersByPhase {
				sum += count
			}

			return sum == totalPhases &&
				summary.WorkersByPhase[string(status.PendingPhaseScheduling)] == schedulingCount &&
				summary.WorkersByPhase[string(status.PendingPhaseWaitingNode)] == waitingNodeCount &&
				summary.WorkersByPhase[string(status.PendingPhasePullingImage)] == pullingImageCount &&
				summary.WorkersByPhase[string(status.PendingPhaseInitializing)] == initializingCount
		},
		gen.IntRange(0, 5),
		gen.IntRange(0, 5),
		gen.IntRange(0, 5),
		gen.IntRange(0, 5),
	))

	// Property: Pending details and failure details can coexist
	properties.Property("pending and failure details can coexist", prop.ForAll(
		func(pendingDetails []WorkerPendingDetail, failures []WorkerFailureDetail) bool {
			summary := NewEndpointStatusSummary()

			for _, detail := range pendingDetails {
				summary.AddPendingDetail(detail)
			}

			for _, failure := range failures {
				summary.AddFailureDetail(failure)
			}

			return len(summary.PendingDetails) == len(pendingDetails) &&
				len(summary.FailureDetails) == len(failures) &&
				summary.HasPendingWorkers() == (len(pendingDetails) > 0) &&
				summary.HasFailedWorkers() == (len(failures) > 0)
		},
		genWorkerPendingDetailList(),
		genWorkerFailureDetailList(),
	))

	properties.TestingRun(t)
}

// TestProperty_EndpointStatusSummaryToMap tests the ToMap conversion
//
// Property: The ToMap method SHALL correctly convert the summary to a map
// that preserves all information for JSON storage.
//
// Feature: endpoint-status-tracking, Property 6: Endpoint Status Summary Computation
func TestProperty_EndpointStatusSummaryToMap(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 50

	properties := gopter.NewProperties(parameters)

	// Property: ToMap preserves totalWorkers
	properties.Property("ToMap preserves totalWorkers", prop.ForAll(
		func(statuses []string) bool {
			summary := NewEndpointStatusSummary()

			for _, s := range statuses {
				summary.AddWorkerStatus(s)
			}

			m := summary.ToMap()
			totalWorkers, ok := m["totalWorkers"].(int)

			return ok && totalWorkers == summary.TotalWorkers
		},
		genWorkerStatusList(),
	))

	// Property: ToMap preserves workersByStatus
	properties.Property("ToMap preserves workersByStatus", prop.ForAll(
		func(statuses []string) bool {
			summary := NewEndpointStatusSummary()

			for _, s := range statuses {
				summary.AddWorkerStatus(s)
			}

			m := summary.ToMap()
			workersByStatus, ok := m["workersByStatus"].(map[string]int)

			if !ok {
				return false
			}

			// Verify all status counts match
			for status, count := range summary.WorkersByStatus {
				if workersByStatus[status] != count {
					return false
				}
			}

			return true
		},
		genWorkerStatusList(),
	))

	// Property: ToMap preserves workersByPhase
	properties.Property("ToMap preserves workersByPhase", prop.ForAll(
		func(phases []status.PendingPhase) bool {
			summary := NewEndpointStatusSummary()

			for _, phase := range phases {
				summary.AddWorkerPhase(phase)
			}

			m := summary.ToMap()
			workersByPhase, ok := m["workersByPhase"].(map[string]int)

			if !ok {
				return false
			}

			// Verify all phase counts match
			for phase, count := range summary.WorkersByPhase {
				if workersByPhase[phase] != count {
					return false
				}
			}

			return true
		},
		genPendingPhaseList(),
	))

	// Property: ToMap includes pendingDetails only when present
	properties.Property("ToMap includes pendingDetails only when present", prop.ForAll(
		func(pendingDetails []WorkerPendingDetail) bool {
			summary := NewEndpointStatusSummary()

			for _, detail := range pendingDetails {
				summary.AddPendingDetail(detail)
			}

			m := summary.ToMap()
			_, hasPendingDetails := m["pendingDetails"]

			// pendingDetails should be present only if there are pending workers
			return hasPendingDetails == (len(pendingDetails) > 0)
		},
		genWorkerPendingDetailList(),
	))

	// Property: ToMap includes failureDetails only when present
	properties.Property("ToMap includes failureDetails only when present", prop.ForAll(
		func(failures []WorkerFailureDetail) bool {
			summary := NewEndpointStatusSummary()

			for _, failure := range failures {
				summary.AddFailureDetail(failure)
			}

			m := summary.ToMap()
			_, hasFailureDetails := m["failureDetails"]

			// failureDetails should be present only if there are failures
			return hasFailureDetails == (len(failures) > 0)
		},
		genWorkerFailureDetailList(),
	))

	// Property: ToMap includes spotCapacity only when set
	properties.Property("ToMap includes spotCapacity only when set", prop.ForAll(
		func(setSpot bool, score int, price float64, instanceType string) bool {
			summary := NewEndpointStatusSummary()

			if setSpot {
				spotStatus := status.NewSpotStatus(score, price, instanceType)
				summary.SetSpotCapacity(spotStatus)
			}

			m := summary.ToMap()
			_, hasSpotCapacity := m["spotCapacity"]

			return hasSpotCapacity == setSpot
		},
		gen.Bool(),
		gen.IntRange(1, 10),
		gen.Float64Range(0.01, 10.0),
		genInstanceType(),
	))

	// Property: ToMap always includes lastUpdated
	properties.Property("ToMap always includes lastUpdated", prop.ForAll(
		func(statuses []string) bool {
			summary := NewEndpointStatusSummary()

			for _, s := range statuses {
				summary.AddWorkerStatus(s)
			}

			m := summary.ToMap()
			lastUpdated, ok := m["lastUpdated"].(string)

			return ok && lastUpdated != ""
		},
		genWorkerStatusList(),
	))

	properties.TestingRun(t)
}

// TestProperty_EndpointStatusSummaryHelperMethods tests helper methods
//
// Property: Helper methods SHALL correctly report the state of the summary.
//
// Feature: endpoint-status-tracking, Property 6: Endpoint Status Summary Computation
func TestProperty_EndpointStatusSummaryHelperMethods(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 50

	properties := gopter.NewProperties(parameters)

	// Property: GetOnlineCount returns correct count
	properties.Property("GetOnlineCount returns correct count", prop.ForAll(
		func(onlineCount, otherCount int) bool {
			summary := NewEndpointStatusSummary()

			for i := 0; i < onlineCount; i++ {
				summary.AddWorkerStatus("ONLINE")
			}

			for i := 0; i < otherCount; i++ {
				summary.AddWorkerStatus("PENDING")
			}

			return summary.GetOnlineCount() == onlineCount
		},
		gen.IntRange(0, 10),
		gen.IntRange(0, 10),
	))

	// Property: GetPendingCount returns correct count
	properties.Property("GetPendingCount returns correct count", prop.ForAll(
		func(pendingCount, otherCount int) bool {
			summary := NewEndpointStatusSummary()

			for i := 0; i < pendingCount; i++ {
				summary.AddWorkerStatus("PENDING")
			}

			for i := 0; i < otherCount; i++ {
				summary.AddWorkerStatus("ONLINE")
			}

			return summary.GetPendingCount() == pendingCount
		},
		gen.IntRange(0, 10),
		gen.IntRange(0, 10),
	))

	// Property: GetFailedCount returns correct count
	properties.Property("GetFailedCount returns correct count", prop.ForAll(
		func(failedCount, otherCount int) bool {
			summary := NewEndpointStatusSummary()

			for i := 0; i < failedCount; i++ {
				summary.AddWorkerStatus("FAILED")
			}

			for i := 0; i < otherCount; i++ {
				summary.AddWorkerStatus("ONLINE")
			}

			return summary.GetFailedCount() == failedCount
		},
		gen.IntRange(0, 10),
		gen.IntRange(0, 10),
	))

	// Property: UpdateTimestamp updates LastUpdated
	properties.Property("UpdateTimestamp updates LastUpdated", prop.ForAll(
		func(_ int) bool {
			summary := NewEndpointStatusSummary()
			originalTime := summary.LastUpdated

			// Wait a tiny bit to ensure time difference
			time.Sleep(time.Millisecond)

			summary.UpdateTimestamp()

			return !summary.LastUpdated.Before(originalTime)
		},
		gen.Const(0),
	))

	properties.TestingRun(t)
}

// TestProperty_NewWorkerDetailConstructors tests the constructor functions
//
// Property: Constructor functions SHALL correctly initialize all fields.
//
// Feature: endpoint-status-tracking, Property 6: Endpoint Status Summary Computation
func TestProperty_NewWorkerDetailConstructors(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 50

	properties := gopter.NewProperties(parameters)

	// Property: NewWorkerPendingDetail correctly initializes all fields
	properties.Property("NewWorkerPendingDetail correctly initializes all fields", prop.ForAll(
		func(workerID, podName string, phase status.PendingPhase, reason, message string) bool {
			since := time.Now()
			detail := NewWorkerPendingDetail(workerID, podName, phase, reason, message, since)

			return detail.WorkerID == workerID &&
				detail.PodName == podName &&
				detail.Phase == phase &&
				detail.Reason == reason &&
				detail.Message == message &&
				detail.Since.Equal(since)
		},
		genWorkerID(),
		genPodName(),
		genPendingPhase(),
		genReason(),
		genMessage(),
	))

	// Property: NewWorkerFailureDetail correctly initializes all fields
	properties.Property("NewWorkerFailureDetail correctly initializes all fields", prop.ForAll(
		func(workerID, podName, failureType, reason, suggestion string) bool {
			occurredAt := time.Now()
			detail := NewWorkerFailureDetail(workerID, podName, failureType, reason, suggestion, occurredAt)

			return detail.WorkerID == workerID &&
				detail.PodName == podName &&
				detail.FailureType == failureType &&
				detail.Reason == reason &&
				detail.Suggestion == suggestion &&
				detail.OccurredAt.Equal(occurredAt)
		},
		genWorkerID(),
		genPodName(),
		genFailureType(),
		genReason(),
		genSuggestion(),
	))

	properties.TestingRun(t)
}

// ============================================================================
// Generators for Status Summary property tests
// ============================================================================

// genWorkerID generates valid worker IDs
func genWorkerID() gopter.Gen {
	return gen.RegexMatch(`worker-[a-z0-9]{8}`).SuchThat(func(s string) bool {
		return len(s) >= 10
	})
}

// genPodName generates valid pod names
func genPodName() gopter.Gen {
	return gen.RegexMatch(`pod-[a-z0-9]{8}`).SuchThat(func(s string) bool {
		return len(s) >= 10
	})
}

// genWorkerStatusName generates valid worker status names
func genWorkerStatusName() gopter.Gen {
	return gen.OneConstOf(
		"ONLINE",
		"PENDING",
		"STARTING",
		"FAILED",
		"OFFLINE",
		"TERMINATED",
	)
}

// genWorkerStatusList generates a list of worker statuses
func genWorkerStatusList() gopter.Gen {
	return gen.SliceOfN(20, genWorkerStatusName())
}

// genPendingPhase generates valid pending phases
func genPendingPhase() gopter.Gen {
	return gen.OneConstOf(
		status.PendingPhaseScheduling,
		status.PendingPhaseWaitingNode,
		status.PendingPhasePullingImage,
		status.PendingPhaseInitializing,
	)
}

// genPendingPhaseList generates a list of pending phases
func genPendingPhaseList() gopter.Gen {
	return gen.SliceOfN(20, genPendingPhase())
}

// genFailureType generates valid failure types
func genFailureType() gopter.Gen {
	return gen.OneConstOf(
		"IMAGE_PULL_FAILED",
		"CONTAINER_CRASH",
		"RESOURCE_LIMIT",
		"TIMEOUT",
		"OOM_KILLED",
	)
}

// genReason generates valid reason strings
func genReason() gopter.Gen {
	return gen.OneConstOf(
		"",
		"Unschedulable",
		"ContainerCreating",
		"ImagePullBackOff",
		"PodInitializing",
		"NodeNotReady",
	)
}

// genMessage generates valid message strings
func genMessage() gopter.Gen {
	return gen.OneConstOf(
		"",
		"Waiting for node to be ready",
		"Pulling image from registry",
		"Init container running",
		"Pod scheduled successfully",
	)
}

// genSuggestion generates valid suggestion strings
func genSuggestion() gopter.Gen {
	return gen.OneConstOf(
		"Check container logs",
		"Increase memory limit",
		"Verify image name",
		"Check network connectivity",
	)
}

// genInstanceType generates valid AWS instance types
func genInstanceType() gopter.Gen {
	return gen.OneConstOf(
		"g4dn.xlarge",
		"g4dn.2xlarge",
		"g5.xlarge",
		"p3.2xlarge",
	)
}

// genWorkerPendingDetail generates a valid WorkerPendingDetail
func genWorkerPendingDetail() gopter.Gen {
	return gopter.CombineGens(
		genWorkerID(),
		genPodName(),
		genPendingPhase(),
		genReason(),
		genMessage(),
	).Map(func(vals []any) WorkerPendingDetail {
		return WorkerPendingDetail{
			WorkerID: vals[0].(string),
			PodName:  vals[1].(string),
			Phase:    vals[2].(status.PendingPhase),
			Reason:   vals[3].(string),
			Message:  vals[4].(string),
			Since:    time.Now(),
		}
	})
}

// genWorkerPendingDetailList generates a list of WorkerPendingDetail
func genWorkerPendingDetailList() gopter.Gen {
	return gen.SliceOfN(10, genWorkerPendingDetail())
}

// genWorkerFailureDetail generates a valid WorkerFailureDetail
func genWorkerFailureDetail() gopter.Gen {
	return gopter.CombineGens(
		genWorkerID(),
		genPodName(),
		genFailureType(),
		genReason(),
		genSuggestion(),
	).Map(func(vals []any) WorkerFailureDetail {
		return WorkerFailureDetail{
			WorkerID:    vals[0].(string),
			PodName:     vals[1].(string),
			FailureType: vals[2].(string),
			Reason:      vals[3].(string),
			Suggestion:  vals[4].(string),
			OccurredAt:  time.Now(),
		}
	})
}

// genWorkerFailureDetailList generates a list of WorkerFailureDetail
func genWorkerFailureDetailList() gopter.Gen {
	return gen.SliceOfN(10, genWorkerFailureDetail())
}
