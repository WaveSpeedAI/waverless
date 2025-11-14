package worker

import (
	"context"
	"time"

	"waverless/internal/service"
	"waverless/pkg/logger"

	"go.uber.org/zap"
)

// ScalerConfig autoscaling configuration
type ScalerConfig struct {
	MinWorkers          int     // Minimum Worker count
	MaxWorkers          int     // Maximum Worker count
	ScaleUpThreshold    float64 // Scale up threshold (queue tasks / online Workers)
	ScaleDownThreshold  float64 // Scale down threshold
	CheckInterval       time.Duration
	CooldownPeriod      time.Duration // Cooldown period to avoid frequent scaling
}

// Scaler autoscaler
type Scaler struct {
	config        ScalerConfig
	taskService   *service.TaskService
	workerService *service.WorkerService
	lastScaleTime time.Time
}

// NewScaler creates autoscaler
func NewScaler(config ScalerConfig, taskService *service.TaskService, workerService *service.WorkerService) *Scaler {
	return &Scaler{
		config:        config,
		taskService:   taskService,
		workerService: workerService,
		lastScaleTime: time.Now(),
	}
}

// Start starts autoscaler
func (s *Scaler) Start(ctx context.Context) {
	ticker := time.NewTicker(s.config.CheckInterval)
	defer ticker.Stop()

	logger.Info("scaler started",
		zap.Duration("check_interval", s.config.CheckInterval),
		zap.Int("min_workers", s.config.MinWorkers),
		zap.Int("max_workers", s.config.MaxWorkers),
	)

	for {
		select {
		case <-ctx.Done():
			logger.Info("scaler stopped")
			return
		case <-ticker.C:
			if err := s.checkAndScale(ctx); err != nil {
				logger.Error("failed to check and scale", zap.Error(err))
			}
		}
	}
}

// checkAndScale checks and executes scaling
func (s *Scaler) checkAndScale(ctx context.Context) error {
	// NOTE: Worker cleanup is handled by dedicated background goroutine in main.go
	// No need to duplicate the cleanup here to avoid redundant Redis queries

	// Get current metrics (use default endpoint by default)
	// TODO: Support multi-endpoint scaling strategy
	pendingCount, err := s.taskService.GetPendingTaskCount(ctx, "default")
	if err != nil {
		return err
	}

	workerCount, err := s.workerService.GetOnlineWorkerCount(ctx)
	if err != nil {
		return err
	}

	logger.Debug("scaling metrics",
		zap.Int64("pending_tasks", pendingCount),
		zap.Int("online_workers", workerCount),
	)

	// Don't execute scaling within cooldown period
	if time.Since(s.lastScaleTime) < s.config.CooldownPeriod {
		return nil
	}

	// Calculate load ratio
	var loadRatio float64
	if workerCount > 0 {
		loadRatio = float64(pendingCount) / float64(workerCount)
	} else if pendingCount > 0 {
		loadRatio = float64(pendingCount)
	}

	// Check if scale up is needed
	if loadRatio > s.config.ScaleUpThreshold && workerCount < s.config.MaxWorkers {
		desiredWorkers := workerCount + 1
		if desiredWorkers > s.config.MaxWorkers {
			desiredWorkers = s.config.MaxWorkers
		}

		logger.Info("scale up triggered",
			zap.Float64("load_ratio", loadRatio),
			zap.Int("current_workers", workerCount),
			zap.Int("desired_workers", desiredWorkers),
		)

		s.lastScaleTime = time.Now()
		// TODO: Actual scale up operation (e.g., trigger K8s HPA or cloud service API)
		return nil
	}

	// Check if scale down is needed
	if loadRatio < s.config.ScaleDownThreshold && workerCount > s.config.MinWorkers {
		desiredWorkers := workerCount - 1
		if desiredWorkers < s.config.MinWorkers {
			desiredWorkers = s.config.MinWorkers
		}

		logger.Info("scale down triggered",
			zap.Float64("load_ratio", loadRatio),
			zap.Int("current_workers", workerCount),
			zap.Int("desired_workers", desiredWorkers),
		)

		s.lastScaleTime = time.Now()
		// TODO: Actual scale down operation
		return nil
	}

	return nil
}
