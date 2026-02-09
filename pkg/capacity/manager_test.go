package capacity

import (
	"testing"

	"waverless/pkg/status"
)

// TestGetSpotStatus_NilRepo tests GetSpotStatus when repo is nil
func TestGetSpotStatus_NilRepo(t *testing.T) {
	manager := &Manager{
		repo:  nil,
		cache: make(map[string]cacheEntry),
	}

	// Test with nil repo and instance type
	result := manager.GetSpotStatus("g4dn.xlarge")
	if result != nil {
		t.Error("Expected nil when repo is nil")
	}

	// Test with nil repo and empty instance type
	result = manager.GetSpotStatus("")
	if result != nil {
		t.Error("Expected nil when repo is nil")
	}
}

// TestGetSpotStatusBySpec_NilRepo tests GetSpotStatusBySpec when repo is nil
func TestGetSpotStatusBySpec_NilRepo(t *testing.T) {
	manager := &Manager{
		repo:  nil,
		cache: make(map[string]cacheEntry),
	}

	result := manager.GetSpotStatusBySpec("gpu-small")
	if result != nil {
		t.Error("Expected nil when repo is nil")
	}
}

// TestGetSpotStatusBySpec_EmptySpecName tests GetSpotStatusBySpec when spec name is empty
func TestGetSpotStatusBySpec_EmptySpecName(t *testing.T) {
	manager := &Manager{
		repo:  nil,
		cache: make(map[string]cacheEntry),
	}

	result := manager.GetSpotStatusBySpec("")
	if result != nil {
		t.Error("Expected nil when spec name is empty")
	}
}

// TestSpotStatusClassification tests that SpotStatus is correctly classified
func TestSpotStatusClassification(t *testing.T) {
	testCases := []struct {
		score            int
		expectedCapacity status.SpotCapacity
	}{
		{10, status.SpotCapacityAvailable},
		{9, status.SpotCapacityAvailable},
		{8, status.SpotCapacityAvailable},
		{7, status.SpotCapacityAvailable},
		{6, status.SpotCapacityLimited},
		{5, status.SpotCapacityLimited},
		{4, status.SpotCapacityLimited},
		{3, status.SpotCapacityConstrained},
		{2, status.SpotCapacityConstrained},
		{1, status.SpotCapacityConstrained},
	}

	for _, tc := range testCases {
		spotStatus := status.NewSpotStatus(tc.score, 0.50, "g4dn.xlarge")
		if spotStatus.Capacity != tc.expectedCapacity {
			t.Errorf("Score %d: expected capacity %s, got %s",
				tc.score, tc.expectedCapacity, spotStatus.Capacity)
		}
	}
}

// TestManagerImplementsCapacityManagerInterface verifies that Manager implements
// the CapacityManager interface defined in the status package.
// Validates: Requirement 2.4
func TestManagerImplementsCapacityManagerInterface(t *testing.T) {
	// This test verifies at compile time that Manager implements CapacityManager
	var _ status.CapacityManager = (*Manager)(nil)
}

// TestNewSpotStatus tests the SpotStatus creation and classification
func TestNewSpotStatus(t *testing.T) {
	spotStatus := status.NewSpotStatus(8, 0.50, "g4dn.xlarge")
	if spotStatus == nil {
		t.Fatal("Expected non-nil SpotStatus")
	}
	if spotStatus.Score != 8 {
		t.Errorf("Expected score 8, got %d", spotStatus.Score)
	}
	if spotStatus.Capacity != status.SpotCapacityAvailable {
		t.Errorf("Expected capacity AVAILABLE, got %s", spotStatus.Capacity)
	}
	if spotStatus.InstanceType != "g4dn.xlarge" {
		t.Errorf("Expected instance type g4dn.xlarge, got %s", spotStatus.InstanceType)
	}
	if spotStatus.Price != 0.50 {
		t.Errorf("Expected price 0.50, got %f", spotStatus.Price)
	}
}
