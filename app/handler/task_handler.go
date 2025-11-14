package handler

import (
	"net/http"
	"strconv"
	"time"

	"waverless/internal/model"
	"waverless/internal/service"
	"waverless/pkg/logger"

	"github.com/gin-gonic/gin"
)

// TaskHandler handles task operations
type TaskHandler struct {
	taskService   *service.TaskService
	workerService *service.WorkerService
}

// NewTaskHandler creates task handler
func NewTaskHandler(taskService *service.TaskService, workerService *service.WorkerService) *TaskHandler {
	return &TaskHandler{
		taskService:   taskService,
		workerService: workerService,
	}
}

// Status gets task status
// @Summary Get task status
// @Description Get task status by task ID
// @Tags tasks
// @Produce json
// @Param task_id path string true "Task ID"
// @Success 200 {object} model.TaskResponse
// @Router /status/{task_id} [get]
func (h *TaskHandler) Status(c *gin.Context) {
	taskID := c.Param("task_id")
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "task_id required"})
		return
	}

	resp, err := h.taskService.GetTaskStatus(c.Request.Context(), taskID)
	if err != nil {
		logger.ErrorCtx(c.Request.Context(), "failed to get task status, task_id: %s, error: %v", taskID, err)
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// Cancel cancels task
// @Summary Cancel task
// @Description Cancel task by task ID
// @Tags tasks
// @Param task_id path string true "Task ID"
// @Success 200 {object} map[string]string
// @Router /cancel/{task_id} [post]
func (h *TaskHandler) Cancel(c *gin.Context) {
	taskID := c.Param("task_id")
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "task_id required"})
		return
	}

	if err := h.taskService.CancelTask(c.Request.Context(), taskID); err != nil {
		logger.ErrorCtx(c.Request.Context(), "failed to cancel task, task_id: %s, error: %v", taskID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "task cancelled"})
}

// SubmitWithEndpoint submits task to specified endpoint
// @Summary Submit task to specified endpoint
// @Description Submit async task to specified endpoint queue
// @Tags tasks
// @Accept json
// @Produce json
// @Param endpoint path string true "Endpoint name"
// @Param request body model.SubmitRequest true "Task request"
// @Success 200 {object} model.SubmitResponse
// @Router /{endpoint}/submit [post]
func (h *TaskHandler) SubmitWithEndpoint(c *gin.Context) {
	endpoint := c.Param("endpoint")
	if endpoint == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "endpoint required"})
		return
	}

	var req model.SubmitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.ErrorCtx(c.Request.Context(), "invalid request: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	// Set endpoint
	req.Endpoint = endpoint

	resp, err := h.taskService.SubmitTask(c.Request.Context(), &req)
	if err != nil {
		logger.ErrorCtx(c.Request.Context(), "failed to submit task: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// SubmitSyncWithEndpoint submits task synchronously to specified endpoint
// @Summary Submit task synchronously to specified endpoint
// @Description Submit task to specified endpoint and wait for result
// @Tags tasks
// @Accept json
// @Produce json
// @Param endpoint path string true "Endpoint name"
// @Param wait query int false "Wait timeout in milliseconds (if not set, wait indefinitely)"
// @Param request body model.SubmitRequest true "Task request"
// @Success 200 {object} model.TaskResponse
// @Router /{endpoint}/runsync [post]
func (h *TaskHandler) SubmitSyncWithEndpoint(c *gin.Context) {
	endpoint := c.Param("endpoint")
	if endpoint == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "endpoint required"})
		return
	}

	var req model.SubmitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.ErrorCtx(c.Request.Context(), "invalid request: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	// Set endpoint
	req.Endpoint = endpoint

	// Read wait timeout from query parameter (milliseconds), if not set wait indefinitely
	var timeout time.Duration
	if waitParam := c.Query("wait"); waitParam != "" {
		if waitMs, err := time.ParseDuration(waitParam + "ms"); err == nil {
			timeout = waitMs
		}
	} else {
		// If wait parameter not set, use a very large timeout (equivalent to infinite wait)
		timeout = 24 * time.Hour // 24 hours
	}

	resp, err := h.taskService.SubmitTaskSync(c.Request.Context(), &req, timeout)
	if err != nil {
		logger.ErrorCtx(c.Request.Context(), "failed to submit task sync: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// ListTasks gets task list
// @Summary Get task list
// @Description Get task list, supports filtering by status, endpoint and task_id, supports pagination, returns tasks array and total count
// @Tags tasks
// @Produce json
// @Param status query string false "Task status (PENDING, IN_PROGRESS, COMPLETED, FAILED)"
// @Param endpoint query string false "Endpoint name"
// @Param task_id query string false "Task ID (exact match)"
// @Param limit query int false "Return count limit (default 20)"
// @Param offset query int false "Offset (default 0)"
// @Success 200 {object} map[string]interface{} "Return format: {tasks: [], total: 0, limit: 20, offset: 0}"
// @Router /tasks [get]
func (h *TaskHandler) ListTasks(c *gin.Context) {
	status := c.Query("status")
	endpoint := c.Query("endpoint")
	taskID := c.Query("task_id")

	limit := 100
	if limitParam := c.Query("limit"); limitParam != "" {
		if parsedLimit, err := strconv.Atoi(limitParam); err == nil && parsedLimit > 0 {
			limit = parsedLimit
		}
	}

	offset := 0
	if offsetParam := c.Query("offset"); offsetParam != "" {
		if parsedOffset, err := strconv.Atoi(offsetParam); err == nil && parsedOffset >= 0 {
			offset = parsedOffset
		}
	}

	tasks, total, err := h.taskService.ListTasks(c.Request.Context(), status, endpoint, taskID, limit, offset)
	if err != nil {
		logger.ErrorCtx(c.Request.Context(), "failed to list tasks: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"tasks": tasks,
		"total": total,
		"limit": limit,
		"offset": offset,
	})
}

// GetEndpointStats gets endpoint statistics
// @Summary Get endpoint statistics
// @Description Get queue status and worker information for specified endpoint
// @Tags tasks
// @Produce json
// @Param endpoint path string true "Endpoint name"
// @Success 200 {object} map[string]interface{}
// @Router /{endpoint}/stats [get]
func (h *TaskHandler) GetEndpointStats(c *gin.Context) {
	endpoint := c.Param("endpoint")
	if endpoint == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "endpoint required"})
		return
	}

	// Get pending count
	pending, err := h.taskService.GetPendingTaskCount(c.Request.Context(), endpoint)
	if err != nil {
		logger.ErrorCtx(c.Request.Context(), "failed to get pending count: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Get all workers (need to filter by endpoint)
	allWorkers, err := h.workerService.GetWorkerList(c.Request.Context())
	if err != nil {
		logger.ErrorCtx(c.Request.Context(), "failed to list workers: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Filter workers for this endpoint
	var workers []*model.Worker
	busyCount := 0
	inProgressCount := 0
	for _, w := range allWorkers {
		if w.Endpoint == endpoint {
			workers = append(workers, w)
			if w.Status == model.WorkerStatusBusy {
				busyCount++
			}
			inProgressCount += len(w.JobsInProgress)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"endpoint":      endpoint,
		"pending":       pending,
		"in_progress":   inProgressCount,
		"total_workers": len(workers),
		"busy_workers":  busyCount,
	})
}

// GetTaskExecutionHistory gets task execution history
// @Summary Get task execution history
// @Description Get task execution history records (worker, start time, end time, duration)
// @Tags tasks
// @Produce json
// @Param task_id path string true "Task ID"
// @Success 200 {object} map[string]interface{}
// @Router /tasks/{task_id}/execution-history [get]
func (h *TaskHandler) GetTaskExecutionHistory(c *gin.Context) {
	taskID := c.Param("task_id")
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "task_id required"})
		return
	}

	history, err := h.taskService.GetTaskExecutionHistory(c.Request.Context(), taskID)
	if err != nil {
		logger.ErrorCtx(c.Request.Context(), "failed to get task execution history, task_id: %s, error: %v", taskID, err)
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"task_id": taskID,
		"history": history,
	})
}

// GetTaskEvents gets all task events
// @Summary Get all task events
// @Description Get complete event log for task
// @Tags tasks
// @Produce json
// @Param task_id path string true "Task ID"
// @Success 200 {object} map[string]interface{}
// @Router /tasks/{task_id}/events [get]
func (h *TaskHandler) GetTaskEvents(c *gin.Context) {
	taskID := c.Param("task_id")
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "task_id required"})
		return
	}

	events, err := h.taskService.GetTaskEvents(c.Request.Context(), taskID)
	if err != nil {
		logger.ErrorCtx(c.Request.Context(), "failed to get task events, task_id: %s, error: %v", taskID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"task_id": taskID,
		"events":  events,
		"total":   len(events),
	})
}

// GetTaskTimeline gets task timeline
// @Summary Get task timeline
// @Description Get simplified timeline for task (key events)
// @Tags tasks
// @Produce json
// @Param task_id path string true "Task ID"
// @Success 200 {object} map[string]interface{}
// @Router /tasks/{task_id}/timeline [get]
func (h *TaskHandler) GetTaskTimeline(c *gin.Context) {
	taskID := c.Param("task_id")
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "task_id required"})
		return
	}

	timeline, err := h.taskService.GetTaskTimeline(c.Request.Context(), taskID)
	if err != nil {
		logger.ErrorCtx(c.Request.Context(), "failed to get task timeline, task_id: %s, error: %v", taskID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"task_id":  taskID,
		"timeline": timeline,
		"total":    len(timeline),
	})
}
