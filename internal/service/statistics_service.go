package service

import (
	"context"
	"fmt"

	"waverless/pkg/logger"
	"waverless/pkg/store/mysql"
	"waverless/pkg/store/mysql/model"
)

// StatisticsService provides task statistics operations
type StatisticsService struct {
	statsRepo *mysql.TaskStatisticsRepository
}

// NewStatisticsService creates a new statistics service
func NewStatisticsService(statsRepo *mysql.TaskStatisticsRepository) *StatisticsService {
	return &StatisticsService{
		statsRepo: statsRepo,
	}
}

// GetOverviewStatistics retrieves global task statistics for dashboard
func (s *StatisticsService) GetOverviewStatistics(ctx context.Context) (*model.TaskStatistics, error) {
	stats, err := s.statsRepo.GetGlobalStatistics(ctx)
	if err != nil {
		logger.WarnCtx(ctx, "failed to get cached global statistics, will refresh: %v", err)
		// If cached stats not found, refresh and retry
		if refreshErr := s.statsRepo.RefreshGlobalStatistics(ctx); refreshErr != nil {
			return nil, fmt.Errorf("failed to refresh global statistics: %w", refreshErr)
		}
		stats, err = s.statsRepo.GetGlobalStatistics(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get global statistics after refresh: %w", err)
		}
	}
	return stats, nil
}

// GetEndpointStatistics retrieves task statistics for a specific endpoint
func (s *StatisticsService) GetEndpointStatistics(ctx context.Context, endpoint string) (*model.TaskStatistics, error) {
	stats, err := s.statsRepo.GetEndpointStatistics(ctx, endpoint)
	if err != nil {
		logger.WarnCtx(ctx, "failed to get cached endpoint statistics for %s, will refresh: %v", endpoint, err)
		// If cached stats not found, refresh and retry
		if refreshErr := s.statsRepo.RefreshEndpointStatistics(ctx, endpoint); refreshErr != nil {
			return nil, fmt.Errorf("failed to refresh endpoint statistics: %w", refreshErr)
		}
		stats, err = s.statsRepo.GetEndpointStatistics(ctx, endpoint)
		if err != nil {
			return nil, fmt.Errorf("failed to get endpoint statistics after refresh: %w", err)
		}
	}
	return stats, nil
}

// GetTopEndpointStatistics retrieves top N endpoints by task volume
func (s *StatisticsService) GetTopEndpointStatistics(ctx context.Context, limit int) ([]*model.TaskStatistics, error) {
	if limit <= 0 {
		limit = 10
	}

	stats, err := s.statsRepo.ListTopEndpoints(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get top endpoint statistics: %w", err)
	}

	return stats, nil
}

// RefreshAllStatistics manually refreshes all statistics (global + all endpoints)
// This can be called periodically or on-demand
func (s *StatisticsService) RefreshAllStatistics(ctx context.Context) error {
	// Refresh global statistics
	if err := s.statsRepo.RefreshGlobalStatistics(ctx); err != nil {
		return fmt.Errorf("failed to refresh global statistics: %w", err)
	}

	// Refresh all endpoint statistics
	if err := s.statsRepo.RefreshAllEndpointStatistics(ctx); err != nil {
		return fmt.Errorf("failed to refresh endpoint statistics: %w", err)
	}

	logger.InfoCtx(ctx, "all statistics refreshed successfully")
	return nil
}

// UpdateStatisticsOnTaskStatusChange updates statistics when a task status changes
// This should be called asynchronously after task status updates
func (s *StatisticsService) UpdateStatisticsOnTaskStatusChange(ctx context.Context, endpoint string, fromStatus, toStatus string) {
	if err := s.statsRepo.IncrementStatistics(ctx, endpoint, fromStatus, toStatus); err != nil {
		logger.ErrorCtx(ctx, "failed to update statistics for status change (endpoint: %s, from: %s, to: %s): %v",
			endpoint, fromStatus, toStatus, err)
	} else {
		logger.DebugCtx(ctx, "statistics updated for status change: endpoint=%s, from=%s, to=%s",
			endpoint, fromStatus, toStatus)
	}
}
