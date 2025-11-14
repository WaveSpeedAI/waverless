package handler

import (
	"net/http"
	"strconv"
	"time"

	"waverless/internal/service"
	"waverless/pkg/interfaces"
	"waverless/pkg/logger"
	"waverless/pkg/store/mysql"

	"github.com/gin-gonic/gin"
)

// GPUUsageHandler handles GPU usage statistics HTTP requests
type GPUUsageHandler struct {
	gpuUsageService    *service.GPUUsageService
	taskRepo           *mysql.TaskRepository
	endpointRepo       *mysql.EndpointRepository
	deploymentProvider interfaces.DeploymentProvider
}

// NewGPUUsageHandler creates a new GPU usage handler
func NewGPUUsageHandler(
	gpuUsageService *service.GPUUsageService,
	taskRepo *mysql.TaskRepository,
	endpointRepo *mysql.EndpointRepository,
	deploymentProvider interfaces.DeploymentProvider,
) *GPUUsageHandler {
	return &GPUUsageHandler{
		gpuUsageService:    gpuUsageService,
		taskRepo:           taskRepo,
		endpointRepo:       endpointRepo,
		deploymentProvider: deploymentProvider,
	}
}

// GetMinuteStatistics retrieves minute-level GPU usage statistics
// @Summary Get minute-level GPU statistics
// @Description Get GPU usage statistics aggregated by minute
// @Tags gpu-usage
// @Produce json
// @Param scope_type query string false "Scope type: global, endpoint, or spec (default: global)"
// @Param scope_value query string false "Scope value (endpoint name or spec name)"
// @Param start_time query string false "Start time (RFC3339 format, default: 1 hour ago)"
// @Param end_time query string false "End time (RFC3339 format, default: now)"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/gpu-usage/minute [get]
func (h *GPUUsageHandler) GetMinuteStatistics(c *gin.Context) {
	scopeType := c.DefaultQuery("scope_type", "global")
	scopeValue := c.Query("scope_value")

	// Parse time range (default: last hour)
	endTime := time.Now()
	startTime := endTime.Add(-1 * time.Hour)

	if start := c.Query("start_time"); start != "" {
		if t, err := time.Parse(time.RFC3339, start); err == nil {
			startTime = t
		}
	}
	if end := c.Query("end_time"); end != "" {
		if t, err := time.Parse(time.RFC3339, end); err == nil {
			endTime = t
		}
	}

	var sv *string
	if scopeValue != "" {
		sv = &scopeValue
	}

	stats, err := h.gpuUsageService.GetMinuteStatistics(c.Request.Context(), scopeType, sv, startTime, endTime)
	if err != nil {
		logger.ErrorCtx(c.Request.Context(), "failed to get minute statistics: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Calculate summary metrics
	var totalGPUHours float64
	var totalTasks int
	for _, s := range stats {
		totalGPUHours += s.TotalGPUHours
		totalTasks += s.TotalTasks
	}

	c.JSON(http.StatusOK, gin.H{
		"data":       stats,
		"total":      len(stats),
		"start_time": startTime.Format(time.RFC3339),
		"end_time":   endTime.Format(time.RFC3339),
		"summary": gin.H{
			"total_gpu_hours": totalGPUHours,
			"total_tasks":     totalTasks,
		},
	})
}

// GetHourlyStatistics retrieves hourly GPU usage statistics
// @Summary Get hourly GPU statistics
// @Description Get GPU usage statistics aggregated by hour
// @Tags gpu-usage
// @Produce json
// @Param scope_type query string false "Scope type: global, endpoint, or spec (default: global)"
// @Param scope_value query string false "Scope value (endpoint name or spec name)"
// @Param start_time query string false "Start time (RFC3339 format, default: 24 hours ago)"
// @Param end_time query string false "End time (RFC3339 format, default: now)"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/gpu-usage/hourly [get]
func (h *GPUUsageHandler) GetHourlyStatistics(c *gin.Context) {
	scopeType := c.DefaultQuery("scope_type", "global")
	scopeValue := c.Query("scope_value")

	// Parse time range (default: last 24 hours)
	endTime := time.Now()
	startTime := endTime.Add(-24 * time.Hour)

	if start := c.Query("start_time"); start != "" {
		if t, err := time.Parse(time.RFC3339, start); err == nil {
			startTime = t
		}
	}
	if end := c.Query("end_time"); end != "" {
		if t, err := time.Parse(time.RFC3339, end); err == nil {
			endTime = t
		}
	}

	var sv *string
	if scopeValue != "" {
		sv = &scopeValue
	}

	stats, err := h.gpuUsageService.GetHourlyStatistics(c.Request.Context(), scopeType, sv, startTime, endTime)
	if err != nil {
		logger.ErrorCtx(c.Request.Context(), "failed to get hourly statistics: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Calculate summary metrics
	var totalGPUHours float64
	var totalTasks int
	var maxGPUCount int
	for _, s := range stats {
		totalGPUHours += s.TotalGPUHours
		totalTasks += s.TotalTasks
		if s.MaxGPUCount > maxGPUCount {
			maxGPUCount = s.MaxGPUCount
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"data":       stats,
		"total":      len(stats),
		"start_time": startTime.Format(time.RFC3339),
		"end_time":   endTime.Format(time.RFC3339),
		"summary": gin.H{
			"total_gpu_hours": totalGPUHours,
			"total_tasks":     totalTasks,
			"max_gpu_count":   maxGPUCount,
		},
	})
}

// GetDailyStatistics retrieves daily GPU usage statistics
// @Summary Get daily GPU statistics
// @Description Get GPU usage statistics aggregated by day
// @Tags gpu-usage
// @Produce json
// @Param scope_type query string false "Scope type: global, endpoint, or spec (default: global)"
// @Param scope_value query string false "Scope value (endpoint name or spec name)"
// @Param start_time query string false "Start date (RFC3339 format, default: 30 days ago)"
// @Param end_time query string false "End date (RFC3339 format, default: today)"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/gpu-usage/daily [get]
func (h *GPUUsageHandler) GetDailyStatistics(c *gin.Context) {
	scopeType := c.DefaultQuery("scope_type", "global")
	scopeValue := c.Query("scope_value")

	// Parse time range (default: last 30 days)
	endTime := time.Now()
	startTime := endTime.Add(-30 * 24 * time.Hour)

	if start := c.Query("start_time"); start != "" {
		if t, err := time.Parse(time.RFC3339, start); err == nil {
			startTime = t
		}
	}
	if end := c.Query("end_time"); end != "" {
		if t, err := time.Parse(time.RFC3339, end); err == nil {
			endTime = t
		}
	}

	var sv *string
	if scopeValue != "" {
		sv = &scopeValue
	}

	stats, err := h.gpuUsageService.GetDailyStatistics(c.Request.Context(), scopeType, sv, startTime, endTime)
	if err != nil {
		logger.ErrorCtx(c.Request.Context(), "failed to get daily statistics: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Calculate summary metrics
	var totalGPUHours float64
	var totalTasks int
	for _, s := range stats {
		totalGPUHours += s.TotalGPUHours
		totalTasks += s.TotalTasks
	}

	c.JSON(http.StatusOK, gin.H{
		"data":       stats,
		"total":      len(stats),
		"start_time": startTime.Format(time.RFC3339),
		"end_time":   endTime.Format(time.RFC3339),
		"summary": gin.H{
			"total_gpu_hours": totalGPUHours,
			"total_tasks":     totalTasks,
			"avg_gpu_hours_per_day": func() float64 {
				if len(stats) > 0 {
					return totalGPUHours / float64(len(stats))
				}
				return 0
			}(),
		},
	})
}

// TriggerAggregation manually triggers statistics aggregation
// @Summary Trigger statistics aggregation
// @Description Manually trigger GPU usage statistics aggregation for a time range
// @Tags gpu-usage
// @Produce json
// @Param granularity query string false "Granularity: minute, hourly, daily, or all (default: all)"
// @Param start_time query string false "Start time (RFC3339 format, default: 1 hour ago)"
// @Param end_time query string false "End time (RFC3339 format, default: now)"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/gpu-usage/aggregate [post]
func (h *GPUUsageHandler) TriggerAggregation(c *gin.Context) {
	granularity := c.DefaultQuery("granularity", "all")

	// Parse time range or use default (last hour)
	endTime := time.Now()
	startTime := endTime.Add(-1 * time.Hour)

	if start := c.Query("start_time"); start != "" {
		if t, err := time.Parse(time.RFC3339, start); err == nil {
			startTime = t
		}
	}
	if end := c.Query("end_time"); end != "" {
		if t, err := time.Parse(time.RFC3339, end); err == nil {
			endTime = t
		}
	}

	err := h.gpuUsageService.AggregateStatistics(c.Request.Context(), startTime, endTime, granularity)
	if err != nil {
		logger.ErrorCtx(c.Request.Context(), "failed to trigger aggregation: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":     "aggregation triggered successfully",
		"granularity": granularity,
		"range": gin.H{
			"start": startTime.Format(time.RFC3339),
			"end":   endTime.Format(time.RFC3339),
		},
	})
}

// BackfillHistoricalData backfills GPU usage records for historical tasks
// @Summary Backfill historical GPU usage data
// @Description Creates GPU usage records for completed tasks that don't have records yet
// @Tags gpu-usage
// @Produce json
// @Param batch_size query int false "Batch size for processing (default: 1000)"
// @Param max_tasks query int false "Maximum number of tasks to process (default: 0 = no limit)"
// @Success 200 {object} service.BackfillResult
// @Router /api/v1/gpu-usage/backfill [post]
func (h *GPUUsageHandler) BackfillHistoricalData(c *gin.Context) {
	// Parse parameters
	batchSize := 1000
	if bs := c.Query("batch_size"); bs != "" {
		if val, err := strconv.Atoi(bs); err == nil && val > 0 {
			batchSize = val
		}
	}

	maxTasks := 0
	if mt := c.Query("max_tasks"); mt != "" {
		if val, err := strconv.Atoi(mt); err == nil && val >= 0 {
			maxTasks = val
		}
	}

	logger.InfoCtx(c.Request.Context(), "Starting GPU usage backfill via API: batch_size=%d, max_tasks=%d", batchSize, maxTasks)

	// Call backfill service
	result, err := h.gpuUsageService.BackfillHistoricalData(
		c.Request.Context(),
		h.taskRepo,
		h.endpointRepo,
		h.deploymentProvider,
		batchSize,
		maxTasks,
	)

	if err != nil {
		logger.ErrorCtx(c.Request.Context(), "backfill failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   err.Error(),
			"message": "backfill failed",
		})
		return
	}

	c.JSON(http.StatusOK, result)
}
