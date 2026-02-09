// Package handler provides HTTP handlers for the Waverless API.
// This file implements the StatusEventHandler for status event API endpoints.
// It implements the status event API for Requirements 3.4, 3.5.
package handler

import (
	"net/http"
	"strconv"
	"time"

	"waverless/internal/service"
	"waverless/pkg/logger"

	"github.com/gin-gonic/gin"
)

// StatusEventHandler handles status event API requests.
// It provides endpoints for listing status events by endpoint or worker.
// Validates: Requirements 3.4, 3.5
type StatusEventHandler struct {
	service *service.StatusEventService
}

// NewStatusEventHandler creates a new StatusEventHandler.
// Parameters:
//   - service: The status event service for business logic.
//
// Returns a new StatusEventHandler instance.
func NewStatusEventHandler(service *service.StatusEventService) *StatusEventHandler {
	return &StatusEventHandler{
		service: service,
	}
}

// StatusEventResponse represents a status event in API responses.
type StatusEventResponse struct {
	ID         int64                  `json:"id"`
	WorkerID   string                 `json:"workerId"`
	Endpoint   string                 `json:"endpoint"`
	EventType  string                 `json:"eventType"`
	OldStatus  string                 `json:"oldStatus,omitempty"`
	NewStatus  string                 `json:"newStatus"`
	Phase      string                 `json:"phase,omitempty"`
	Reason     string                 `json:"reason,omitempty"`
	Message    string                 `json:"message,omitempty"`
	SpotStatus map[string]interface{} `json:"spotStatus,omitempty"`
	CreatedAt  string                 `json:"createdAt"`
}

// ListStatusEvents lists status events for an endpoint.
// @Summary List status events for endpoint
// @Description Get status events for a specific endpoint with optional filters
// @Tags Status Events
// @Produce json
// @Param endpoint path string true "Endpoint name"
// @Param limit query int false "Maximum number of events to return" default(50)
// @Param offset query int false "Number of events to skip" default(0)
// @Param start_time query string false "Filter events created at or after this time (RFC3339 format)"
// @Param end_time query string false "Filter events created at or before this time (RFC3339 format)"
// @Success 200 {array} StatusEventResponse
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/v1/endpoints/{endpoint}/status-events [get]
func (h *StatusEventHandler) ListStatusEvents(c *gin.Context) {
	ctx := c.Request.Context()
	endpoint := c.Param("name")

	if endpoint == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "endpoint is required"})
		return
	}

	// Parse query parameters
	limit := parseIntParam(c, "limit", 50)
	offset := parseIntParam(c, "offset", 0)
	startTime := parseTimeParam(c, "start_time")
	endTime := parseTimeParam(c, "end_time")

	// Build filter
	filter := &service.StatusEventFilter{
		Endpoint:  endpoint,
		Limit:     limit,
		Offset:    offset,
		StartTime: startTime,
		EndTime:   endTime,
	}

	// Query events
	events, err := h.service.ListEvents(ctx, filter)
	if err != nil {
		logger.ErrorCtx(ctx, "Failed to list status events for endpoint %s: %v", endpoint, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Convert to response format
	response := make([]StatusEventResponse, len(events))
	for i, event := range events {
		response[i] = toStatusEventResponse(&event)
	}

	c.JSON(http.StatusOK, response)
}

// GetWorkerStatusEvents lists status events for a specific worker.
// @Summary List status events for worker
// @Description Get status events for a specific worker with optional filters
// @Tags Status Events
// @Produce json
// @Param workerId path string true "Worker ID"
// @Param limit query int false "Maximum number of events to return" default(50)
// @Param offset query int false "Number of events to skip" default(0)
// @Param start_time query string false "Filter events created at or after this time (RFC3339 format)"
// @Param end_time query string false "Filter events created at or before this time (RFC3339 format)"
// @Success 200 {array} StatusEventResponse
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/v1/workers/{workerId}/status-events [get]
func (h *StatusEventHandler) GetWorkerStatusEvents(c *gin.Context) {
	ctx := c.Request.Context()
	workerID := c.Param("id")

	if workerID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "worker_id is required"})
		return
	}

	// Parse query parameters
	limit := parseIntParam(c, "limit", 50)
	offset := parseIntParam(c, "offset", 0)
	startTime := parseTimeParam(c, "start_time")
	endTime := parseTimeParam(c, "end_time")

	// Build filter
	filter := &service.StatusEventFilter{
		WorkerID:  workerID,
		Limit:     limit,
		Offset:    offset,
		StartTime: startTime,
		EndTime:   endTime,
	}

	// Query events
	events, err := h.service.ListEvents(ctx, filter)
	if err != nil {
		logger.ErrorCtx(ctx, "Failed to list status events for worker %s: %v", workerID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Convert to response format
	response := make([]StatusEventResponse, len(events))
	for i, event := range events {
		response[i] = toStatusEventResponse(&event)
	}

	c.JSON(http.StatusOK, response)
}

// toStatusEventResponse converts a service StatusEvent to a StatusEventResponse.
func toStatusEventResponse(event *service.StatusEvent) StatusEventResponse {
	response := StatusEventResponse{
		ID:        event.ID,
		WorkerID:  event.WorkerID,
		Endpoint:  event.Endpoint,
		EventType: string(event.EventType),
		OldStatus: event.OldStatus,
		NewStatus: event.NewStatus,
		Phase:     event.Phase,
		Reason:    event.Reason,
		Message:   event.Message,
		CreatedAt: event.CreatedAt.Format(time.RFC3339),
	}

	// Convert SpotStatus to map
	if event.SpotStatus != nil {
		response.SpotStatus = map[string]interface{}{
			"capacity":     string(event.SpotStatus.Capacity),
			"score":        event.SpotStatus.Score,
			"price":        event.SpotStatus.Price,
			"instanceType": event.SpotStatus.InstanceType,
		}
	}

	return response
}

// parseIntParam parses an integer query parameter with a default value.
func parseIntParam(c *gin.Context, name string, defaultValue int) int {
	valueStr := c.Query(name)
	if valueStr == "" {
		return defaultValue
	}
	value, err := strconv.Atoi(valueStr)
	if err != nil {
		return defaultValue
	}
	return value
}

// parseTimeParam parses a time query parameter in RFC3339 format.
func parseTimeParam(c *gin.Context, name string) *time.Time {
	valueStr := c.Query(name)
	if valueStr == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, valueStr)
	if err != nil {
		return nil
	}
	return &t
}
