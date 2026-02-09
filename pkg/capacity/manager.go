package capacity

import (
	"context"
	"sync"
	"time"

	"waverless/pkg/interfaces"
	"waverless/pkg/logger"
	"waverless/pkg/status"
	"waverless/pkg/store/mysql"
	"waverless/pkg/store/mysql/model"
)

// PodCountProvider interface for Pod count statistics
type PodCountProvider interface {
	GetPodCountsBySpec(ctx context.Context) (map[string]PodCounts, error)
}

// PodCounts Pod count statistics
type PodCounts struct {
	Running int
	Pending int
}

// cacheEntry cache entry with status and reason
type cacheEntry struct {
	Status    interfaces.CapacityStatus
	Reason    string
	SpotScore int // cached spot score for priority comparison
}

type Manager struct {
	provider          Provider
	spotChecker       *AWSSpotChecker
	podCountProvider  PodCountProvider
	repo              *mysql.SpecCapacityRepository
	cache             map[string]cacheEntry
	cacheMu           sync.RWMutex
	pollInterval      time.Duration
	spotCheckInterval time.Duration
	callbacks         []func(interfaces.CapacityEvent)
}

func NewManager(provider Provider, repo *mysql.SpecCapacityRepository) *Manager {
	return &Manager{
		provider:          provider,
		repo:              repo,
		cache:             make(map[string]cacheEntry),
		pollInterval:      5 * time.Minute,
		spotCheckInterval: 10 * time.Minute,
	}
}

func (m *Manager) SetPollInterval(d time.Duration) {
	m.pollInterval = d
}

func (m *Manager) SetSpotCheckInterval(d time.Duration) {
	m.spotCheckInterval = d
}

func (m *Manager) SetPodCountProvider(p PodCountProvider) {
	m.podCountProvider = p
}

func (m *Manager) SetSpotChecker(checker *AWSSpotChecker) {
	m.spotChecker = checker
}

func (m *Manager) OnChange(callback func(interfaces.CapacityEvent)) {
	m.callbacks = append(m.callbacks, callback)
}

func (m *Manager) Start(ctx context.Context) error {
	// Load initial state
	if err := m.loadFromDB(ctx); err != nil {
		logger.WarnCtx(ctx, "Failed to load capacity from DB: %v", err)
	}

	// Start Pod count updater scheduled task
	go m.startPodCountUpdater(ctx)

	// Start Spot checker scheduled task
	go m.startSpotChecker(ctx)

	if m.provider != nil && m.provider.SupportsWatch() {
		return m.provider.Watch(ctx, m.handleEvent)
	}
	return m.startPolling(ctx)
}

func (m *Manager) loadFromDB(ctx context.Context) error {
	caps, err := m.repo.List(ctx)
	if err != nil {
		return err
	}
	m.cacheMu.Lock()
	defer m.cacheMu.Unlock()
	for _, c := range caps {
		m.cache[c.SpecName] = cacheEntry{
			Status: interfaces.CapacityStatus(c.Status),
			Reason: c.Reason,
		}
	}
	return nil
}

func (m *Manager) startPolling(ctx context.Context) error {
	if m.provider == nil {
		return nil
	}
	ticker := time.NewTicker(m.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			events, err := m.provider.CheckAll(ctx)
			if err != nil {
				logger.WarnCtx(ctx, "Capacity check failed: %v", err)
				continue
			}
			for _, e := range events {
				m.handleEvent(e)
			}
		}
	}
}

// startPodCountUpdater starts scheduled task to update Pod count statistics
func (m *Manager) startPodCountUpdater(ctx context.Context) {
	if m.podCountProvider == nil {
		return
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Execute immediately on first run
	m.updatePodCounts(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.updatePodCounts(ctx)
		}
	}
}

func (m *Manager) updatePodCounts(ctx context.Context) {
	if m.podCountProvider == nil {
		return
	}

	counts, err := m.podCountProvider.GetPodCountsBySpec(ctx)
	if err != nil {
		logger.WarnCtx(ctx, "Failed to get pod counts: %v", err)
		return
	}

	for specName, count := range counts {
		if err := m.repo.UpdateCounts(ctx, specName, count.Running, count.Pending); err != nil {
			logger.WarnCtx(ctx, "Failed to update pod counts for %s: %v", specName, err)
		}
	}
}

// startSpotChecker starts scheduled task to check Spot capacity and pricing
func (m *Manager) startSpotChecker(ctx context.Context) {
	if m.spotChecker == nil {
		return
	}

	ticker := time.NewTicker(m.spotCheckInterval)
	defer ticker.Stop()

	// Execute immediately on first run
	m.checkSpots(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.checkSpots(ctx)
		}
	}
}

func (m *Manager) checkSpots(ctx context.Context) {
	if m.spotChecker == nil {
		return
	}

	spots, err := m.spotChecker.CheckAllSpots(ctx)
	if err != nil {
		logger.WarnCtx(ctx, "Failed to check spots: %v", err)
		return
	}

	for _, spot := range spots {
		// Update Spot info to database (always update for display)
		if err := m.repo.UpdateSpotInfo(ctx, spot.SpecName, spot.Score, spot.Price, spot.InstanceType); err != nil {
			logger.WarnCtx(ctx, "Failed to update spot info for %s: %v", spot.SpecName, err)
		}

		logger.InfoCtx(ctx, "Spot check: spec=%s, instance=%s, score=%d, price=$%.4f/hr",
			spot.SpecName, spot.InstanceType, spot.Score, spot.Price)

		// Spot Score determines status:
		// - score <= 2: sold_out (very hard to get new instances)
		// - score 3-6: limited (difficult but possible)
		// - score >= 7: available (easy to get instances)
		var newStatus interfaces.CapacityStatus
		if spot.Score >= 7 {
			newStatus = interfaces.CapacityAvailable
		} else if spot.Score <= 2 {
			newStatus = interfaces.CapacitySoldOut
		} else {
			newStatus = interfaces.CapacityLimited
		}

		m.handleSpotEvent(ctx, spot.SpecName, spot.Score, newStatus)
	}
}

// handleSpotEvent handles spot score events with priority over nodeclaim events
func (m *Manager) handleSpotEvent(ctx context.Context, specName string, spotScore int, newStatus interfaces.CapacityStatus) {
	m.cacheMu.Lock()
	old := m.cache[specName]
	// Update spot score in cache
	m.cache[specName] = cacheEntry{
		Status:    newStatus,
		Reason:    "spot_score",
		SpotScore: spotScore,
	}
	m.cacheMu.Unlock()

	// Update DB when status or reason changes
	if old.Status != newStatus || old.Reason != "spot_score" {
		if err := m.repo.UpdateStatus(ctx, specName, model.CapacityStatus(newStatus), "spot_score"); err != nil {
			logger.WarnCtx(ctx, "Failed to update capacity status: %v", err)
		}
		logger.InfoCtx(ctx, "Capacity changed (spot): spec=%s, %s(%s) -> %s(spot_score, score=%d)",
			specName, old.Status, old.Reason, newStatus, spotScore)
		for _, cb := range m.callbacks {
			cb(interfaces.CapacityEvent{
				SpecName:  specName,
				Status:    newStatus,
				Reason:    "spot_score",
				UpdatedAt: time.Now(),
			})
		}
	}
}

func (m *Manager) handleEvent(event interfaces.CapacityEvent) {
	ctx := context.Background()

	m.cacheMu.Lock()
	old := m.cache[event.SpecName]

	// If current status is from spot_score with low score (<=2), nodeclaim events cannot override it
	// This ensures spot availability takes priority over individual node success
	if old.Reason == "spot_score" && old.SpotScore <= 2 && event.Reason == "nodeclaim" {
		m.cacheMu.Unlock()
		logger.InfoCtx(ctx, "Ignoring nodeclaim event for %s: spot_score=%d is too low, keeping status=%s",
			event.SpecName, old.SpotScore, old.Status)
		return
	}

	// For nodeclaim events, preserve the spot score
	newEntry := cacheEntry{Status: event.Status, Reason: event.Reason, SpotScore: old.SpotScore}
	m.cache[event.SpecName] = newEntry
	m.cacheMu.Unlock()

	// Update DB when status or reason changes
	if old.Status != event.Status || old.Reason != event.Reason {
		if err := m.repo.UpdateStatus(ctx, event.SpecName, model.CapacityStatus(event.Status), event.Reason); err != nil {
			logger.WarnCtx(ctx, "Failed to update capacity status: %v", err)
		}
		logger.InfoCtx(ctx, "Capacity changed: spec=%s, %s(%s) -> %s(%s)",
			event.SpecName, old.Status, old.Reason, event.Status, event.Reason)
		for _, cb := range m.callbacks {
			cb(event)
		}
	}
}

func (m *Manager) GetStatus(specName string) interfaces.CapacityStatus {
	m.cacheMu.RLock()
	defer m.cacheMu.RUnlock()
	if e, ok := m.cache[specName]; ok {
		return e.Status
	}
	return interfaces.CapacityAvailable
}

// ReportSuccess reports successful startup
func (m *Manager) ReportSuccess(ctx context.Context, specName string) {
	m.handleEvent(interfaces.CapacityEvent{
		SpecName:  specName,
		Status:    interfaces.CapacityAvailable,
		Reason:    "nodeclaim",
		UpdatedAt: time.Now(),
	})
}

// ReportFailure reports startup failure
func (m *Manager) ReportFailure(ctx context.Context, specName, reason string) {
	m.handleEvent(interfaces.CapacityEvent{
		SpecName:  specName,
		Status:    interfaces.CapacitySoldOut,
		Reason:    "nodeclaim:" + reason,
		UpdatedAt: time.Now(),
	})
}

// GetSpotStatus returns the current Spot capacity status for the given instance type.
// This method implements the status.CapacityManager interface.
// If instanceType is empty, it returns nil (no specific instance type to look up).
// If Spot information is not available for the instance type, it returns nil.
// Validates: Requirement 2.4
func (m *Manager) GetSpotStatus(instanceType string) *status.SpotStatus {
	if m.repo == nil {
		return nil
	}

	ctx := context.Background()

	// If instanceType is provided, try to find the spec with that instance type
	// Otherwise, we cannot determine which spec to look up
	if instanceType == "" {
		// Try to get any available spot info from cache
		// This is a fallback when instance type is not specified
		m.cacheMu.RLock()
		defer m.cacheMu.RUnlock()

		// Look for any spec with spot info in the database
		caps, err := m.repo.List(ctx)
		if err != nil {
			logger.WarnCtx(ctx, "Failed to list spec capacities for spot status: %v", err)
			return nil
		}

		// Return the first spec with valid spot info
		for _, cap := range caps {
			if cap.SpotScore != nil && cap.InstanceType != "" {
				price := 0.0
				if cap.SpotPrice != nil {
					price, _ = cap.SpotPrice.Float64()
				}
				return status.NewSpotStatus(*cap.SpotScore, price, cap.InstanceType)
			}
		}
		return nil
	}

	// Find spec by instance type
	caps, err := m.repo.List(ctx)
	if err != nil {
		logger.WarnCtx(ctx, "Failed to list spec capacities for spot status: %v", err)
		return nil
	}

	for _, cap := range caps {
		if cap.InstanceType == instanceType && cap.SpotScore != nil {
			price := 0.0
			if cap.SpotPrice != nil {
				price, _ = cap.SpotPrice.Float64()
			}
			return status.NewSpotStatus(*cap.SpotScore, price, cap.InstanceType)
		}
	}

	return nil
}

// GetSpotStatusBySpec returns the current Spot capacity status for the given spec name.
// This is a convenience method when you know the spec name instead of instance type.
// Validates: Requirement 2.4
func (m *Manager) GetSpotStatusBySpec(specName string) *status.SpotStatus {
	if m.repo == nil || specName == "" {
		return nil
	}

	ctx := context.Background()
	cap, err := m.repo.Get(ctx, specName)
	if err != nil {
		logger.WarnCtx(ctx, "Failed to get spec capacity for %s: %v", specName, err)
		return nil
	}

	if cap.SpotScore == nil {
		return nil
	}

	price := 0.0
	if cap.SpotPrice != nil {
		price, _ = cap.SpotPrice.Float64()
	}

	return status.NewSpotStatus(*cap.SpotScore, price, cap.InstanceType)
}
