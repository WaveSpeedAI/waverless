// Package status provides property-based tests for Spot Capacity Classification.
// These tests verify universal properties that should hold across all valid inputs.
//
// Feature: endpoint-status-tracking, Property 3: Spot Capacity Classification
// **Validates: Requirements 2.3**
package status

import (
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// TestProperty_SpotCapacityClassification tests Property 3: Spot Capacity Classification
//
// Property: For any Spot placement score in the range [1, 10], the capacity classification SHALL be:
// - AVAILABLE if score >= 7
// - LIMITED if score >= 4 and score < 7
// - CONSTRAINED if score < 4
//
// This classification SHALL be deterministic and consistent.
//
// Feature: endpoint-status-tracking, Property 3: Spot Capacity Classification
// **Validates: Requirements 2.3**
func TestProperty_SpotCapacityClassification(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 50

	properties := gopter.NewProperties(parameters)

	// Property 3a: Scores >= 7 always result in AVAILABLE
	properties.Property("scores >= 7 always result in AVAILABLE", prop.ForAll(
		func(score int) bool {
			result := ClassifySpotCapacity(score)
			return result == SpotCapacityAvailable
		},
		gen.IntRange(7, 10),
	))

	// Property 3b: Scores in range [4, 6] always result in LIMITED
	properties.Property("scores in range [4, 6] always result in LIMITED", prop.ForAll(
		func(score int) bool {
			result := ClassifySpotCapacity(score)
			return result == SpotCapacityLimited
		},
		gen.IntRange(4, 6),
	))

	// Property 3c: Scores < 4 always result in CONSTRAINED
	properties.Property("scores < 4 always result in CONSTRAINED", prop.ForAll(
		func(score int) bool {
			result := ClassifySpotCapacity(score)
			return result == SpotCapacityConstrained
		},
		gen.IntRange(1, 3),
	))

	// Property 3d: Classification is deterministic (same input always produces same output)
	properties.Property("classification is deterministic", prop.ForAll(
		func(score int) bool {
			result1 := ClassifySpotCapacity(score)
			result2 := ClassifySpotCapacity(score)
			return result1 == result2
		},
		gen.IntRange(1, 10),
	))

	// Property 3e: Result is always one of the valid capacity values
	properties.Property("result is always one of the valid capacity values", prop.ForAll(
		func(score int) bool {
			result := ClassifySpotCapacity(score)
			return result == SpotCapacityAvailable ||
				result == SpotCapacityLimited ||
				result == SpotCapacityConstrained
		},
		gen.IntRange(1, 10),
	))

	// Property 3f: Classification boundaries are correct
	// Score 7 is the boundary between LIMITED and AVAILABLE
	properties.Property("score 7 is boundary for AVAILABLE", prop.ForAll(
		func(_ int) bool {
			// Score 6 should be LIMITED
			result6 := ClassifySpotCapacity(6)
			// Score 7 should be AVAILABLE
			result7 := ClassifySpotCapacity(7)
			return result6 == SpotCapacityLimited && result7 == SpotCapacityAvailable
		},
		gen.Const(0),
	))

	// Property 3g: Score 4 is the boundary between CONSTRAINED and LIMITED
	properties.Property("score 4 is boundary for LIMITED", prop.ForAll(
		func(_ int) bool {
			// Score 3 should be CONSTRAINED
			result3 := ClassifySpotCapacity(3)
			// Score 4 should be LIMITED
			result4 := ClassifySpotCapacity(4)
			return result3 == SpotCapacityConstrained && result4 == SpotCapacityLimited
		},
		gen.Const(0),
	))

	// Property 3h: NewSpotStatus correctly classifies capacity
	properties.Property("NewSpotStatus correctly classifies capacity", prop.ForAll(
		func(score int, price float64, instanceType string) bool {
			status := NewSpotStatus(score, price, instanceType)
			expectedCapacity := ClassifySpotCapacity(score)
			return status.Capacity == expectedCapacity &&
				status.Score == score &&
				status.Price == price &&
				status.InstanceType == instanceType
		},
		gen.IntRange(1, 10),
		genSpotPrice(),
		genInstanceType(),
	))

	properties.TestingRun(t)
}

// TestProperty_SpotCapacityEdgeCases tests edge cases for Spot Capacity Classification
//
// Property: The classification SHALL handle edge cases correctly:
// - Minimum valid score (1) → CONSTRAINED
// - Maximum valid score (10) → AVAILABLE
// - Boundary scores (4, 7) → correct classification
//
// Feature: endpoint-status-tracking, Property 3: Spot Capacity Classification
// **Validates: Requirements 2.3**
func TestProperty_SpotCapacityEdgeCases(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 50

	properties := gopter.NewProperties(parameters)

	// Property: Minimum score (1) is CONSTRAINED
	properties.Property("minimum score 1 is CONSTRAINED", prop.ForAll(
		func(_ int) bool {
			return ClassifySpotCapacity(1) == SpotCapacityConstrained
		},
		gen.Const(0),
	))

	// Property: Maximum score (10) is AVAILABLE
	properties.Property("maximum score 10 is AVAILABLE", prop.ForAll(
		func(_ int) bool {
			return ClassifySpotCapacity(10) == SpotCapacityAvailable
		},
		gen.Const(0),
	))

	// Property: All scores in valid range produce valid results
	properties.Property("all valid scores produce valid results", prop.ForAll(
		func(score int) bool {
			result := ClassifySpotCapacity(score)
			// Verify the result matches the expected classification
			switch {
			case score >= 7:
				return result == SpotCapacityAvailable
			case score >= 4:
				return result == SpotCapacityLimited
			default:
				return result == SpotCapacityConstrained
			}
		},
		gen.IntRange(1, 10),
	))

	// Property: Scores outside valid range still produce valid capacity values
	// (defensive programming - function should not crash)
	properties.Property("scores outside valid range still produce valid capacity values", prop.ForAll(
		func(score int) bool {
			result := ClassifySpotCapacity(score)
			return result == SpotCapacityAvailable ||
				result == SpotCapacityLimited ||
				result == SpotCapacityConstrained
		},
		gen.OneGenOf(
			gen.IntRange(-10, 0),
			gen.IntRange(11, 20),
		),
	))

	properties.TestingRun(t)
}

// TestProperty_SpotCapacityConsistency tests consistency properties
//
// Property: The classification SHALL be consistent:
// - Higher scores never result in lower capacity classifications
// - The classification is monotonically non-decreasing with score
//
// Feature: endpoint-status-tracking, Property 3: Spot Capacity Classification
// **Validates: Requirements 2.3**
func TestProperty_SpotCapacityConsistency(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 50

	properties := gopter.NewProperties(parameters)

	// Property: Higher scores never result in lower capacity classifications
	// (monotonically non-decreasing)
	properties.Property("higher scores never result in lower capacity", prop.ForAll(
		func(score1, score2 int) bool {
			if score1 > score2 {
				score1, score2 = score2, score1 // Ensure score1 <= score2
			}
			result1 := ClassifySpotCapacity(score1)
			result2 := ClassifySpotCapacity(score2)
			return capacityOrder(result1) <= capacityOrder(result2)
		},
		gen.IntRange(1, 10),
		gen.IntRange(1, 10),
	))

	// Property: Adjacent scores have same or adjacent classifications
	properties.Property("adjacent scores have same or adjacent classifications", prop.ForAll(
		func(score int) bool {
			if score >= 10 {
				return true // No adjacent score above 10
			}
			result1 := ClassifySpotCapacity(score)
			result2 := ClassifySpotCapacity(score + 1)
			order1 := capacityOrder(result1)
			order2 := capacityOrder(result2)
			// Adjacent scores should have same or adjacent classifications
			return order2-order1 <= 1
		},
		gen.IntRange(1, 9),
	))

	properties.TestingRun(t)
}

// ============================================================================
// Helper functions for Spot Capacity property tests
// ============================================================================

// capacityOrder returns a numeric order for capacity values
// CONSTRAINED < LIMITED < AVAILABLE
func capacityOrder(capacity SpotCapacity) int {
	switch capacity {
	case SpotCapacityConstrained:
		return 0
	case SpotCapacityLimited:
		return 1
	case SpotCapacityAvailable:
		return 2
	default:
		return -1
	}
}

// ============================================================================
// Generators for Spot Capacity property tests
// ============================================================================

// genSpotPrice generates realistic Spot prices (USD/hour)
func genSpotPrice() gopter.Gen {
	return gen.Float64Range(0.01, 10.0)
}

// genInstanceType generates valid AWS instance type strings
func genInstanceType() gopter.Gen {
	return gen.OneConstOf(
		"g4dn.xlarge",
		"g4dn.2xlarge",
		"g4dn.4xlarge",
		"g5.xlarge",
		"g5.2xlarge",
		"p3.2xlarge",
		"p3.8xlarge",
		"p4d.24xlarge",
	)
}
