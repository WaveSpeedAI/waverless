package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"waverless/internal/service"
	"waverless/pkg/capacity"
	"waverless/pkg/interfaces"
	"waverless/pkg/logger"
	"waverless/pkg/store/mysql"
)

// SpecHandler handles spec CRUD APIs
type SpecHandler struct {
	specService  *service.SpecService
	capacityMgr  *capacity.Manager
	capacityRepo *mysql.SpecCapacityRepository
}

// NewSpecHandler creates a new spec handler
func NewSpecHandler(specService *service.SpecService) *SpecHandler {
	return &SpecHandler{
		specService: specService,
	}
}

// SetCapacityManager sets the capacity manager
func (h *SpecHandler) SetCapacityManager(mgr *capacity.Manager, repo *mysql.SpecCapacityRepository) {
	h.capacityMgr = mgr
	h.capacityRepo = repo
}

// CreateSpec creates a new spec
// @Summary Create spec
// @Description Create a new resource specification
// @Tags Specs
// @Accept json
// @Produce json
// @Param request body interfaces.CreateSpecRequest true "Spec creation request"
// @Success 200 {object} interfaces.SpecInfo
// @Router /api/v1/k8s/specs [post]
func (h *SpecHandler) CreateSpec(c *gin.Context) {
	var req interfaces.CreateSpecRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.ErrorCtx(c.Request.Context(), "Failed to bind create spec request: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	logger.InfoCtx(c.Request.Context(), "Creating spec: name=%s, category=%s", req.Name, req.Category)

	spec, err := h.specService.CreateSpec(c.Request.Context(), &req)
	if err != nil {
		logger.ErrorCtx(c.Request.Context(), "Failed to create spec: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	logger.InfoCtx(c.Request.Context(), "Successfully created spec: %s", req.Name)
	c.JSON(http.StatusOK, spec)
}

// GetSpec gets spec details
// @Summary Get spec details
// @Description Get detailed information for specified spec
// @Tags Specs
// @Produce json
// @Param name path string true "Spec name"
// @Success 200 {object} interfaces.SpecInfo
// @Router /api/v1/k8s/specs/{name} [get]
func (h *SpecHandler) GetSpec(c *gin.Context) {
	name := c.Param("name")

	spec, err := h.specService.GetSpec(c.Request.Context(), name)
	if err != nil {
		logger.ErrorCtx(c.Request.Context(), "Failed to get spec: %v", err)
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, spec)
}

// ListSpecs lists all specs
// @Summary List all specs
// @Description Get all available resource specs
// @Tags Specs
// @Produce json
// @Param category query string false "Filter by category (cpu, gpu)"
// @Success 200 {array} interfaces.SpecInfo
// @Router /api/v1/k8s/specs [get]
func (h *SpecHandler) ListSpecs(c *gin.Context) {
	category := c.Query("category")

	var specs []*interfaces.SpecInfo
	var err error

	if category != "" {
		specs, err = h.specService.ListSpecsByCategory(c.Request.Context(), category)
	} else {
		specs, err = h.specService.ListSpecs(c.Request.Context())
	}

	if err != nil {
		logger.ErrorCtx(c.Request.Context(), "Failed to list specs: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, specs)
}

// UpdateSpec updates a spec
// @Summary Update spec
// @Description Update an existing resource specification
// @Tags Specs
// @Accept json
// @Produce json
// @Param name path string true "Spec name"
// @Param request body interfaces.UpdateSpecRequest true "Spec update request"
// @Success 200 {object} interfaces.SpecInfo
// @Router /api/v1/k8s/specs/{name} [put]
func (h *SpecHandler) UpdateSpec(c *gin.Context) {
	name := c.Param("name")

	var req interfaces.UpdateSpecRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.ErrorCtx(c.Request.Context(), "Failed to bind update spec request: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	logger.InfoCtx(c.Request.Context(), "Updating spec: name=%s", name)

	spec, err := h.specService.UpdateSpec(c.Request.Context(), name, &req)
	if err != nil {
		logger.ErrorCtx(c.Request.Context(), "Failed to update spec: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	logger.InfoCtx(c.Request.Context(), "Successfully updated spec: %s", name)
	c.JSON(http.StatusOK, spec)
}

// DeleteSpec deletes a spec
// @Summary Delete spec
// @Description Delete a resource specification (soft delete)
// @Tags Specs
// @Param name path string true "Spec name"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/k8s/specs/{name} [delete]
func (h *SpecHandler) DeleteSpec(c *gin.Context) {
	name := c.Param("name")

	logger.InfoCtx(c.Request.Context(), "Deleting spec: name=%s", name)

	if err := h.specService.DeleteSpec(c.Request.Context(), name); err != nil {
		logger.ErrorCtx(c.Request.Context(), "Failed to delete spec: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	logger.InfoCtx(c.Request.Context(), "Successfully deleted spec: %s", name)
	c.JSON(http.StatusOK, gin.H{
		"message": "Spec deleted successfully",
		"name":    name,
	})
}

// ListSpecsWithCapacity lists all specs with capacity status
// @Summary List specs with capacity
// @Description Get all specs with their capacity availability status
// @Tags Specs
// @Produce json
// @Success 200 {array} interfaces.SpecWithCapacity
// @Router /api/v1/k8s/specs/capacity [get]
func (h *SpecHandler) ListSpecsWithCapacity(c *gin.Context) {
	specs, err := h.specService.ListSpecs(c.Request.Context())
	if err != nil {
		logger.ErrorCtx(c.Request.Context(), "Failed to list specs: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Get all capacity info
	var capMap = make(map[string]*interfaces.SpecWithCapacity)
	if h.capacityRepo != nil {
		caps, _ := h.capacityRepo.List(c.Request.Context())
		for _, cap := range caps {
			swc := &interfaces.SpecWithCapacity{
				Capacity:     interfaces.CapacityStatus(cap.Status),
				RunningCount: cap.RunningCount,
				PendingCount: cap.PendingCount,
			}
			if cap.SpotScore != nil {
				swc.SpotScore = *cap.SpotScore
			}
			if cap.SpotPrice != nil {
				swc.SpotPrice = cap.SpotPrice.InexactFloat64()
			}
			capMap[cap.SpecName] = swc
		}
	}

	result := make([]*interfaces.SpecWithCapacity, len(specs))
	for i, spec := range specs {
		if cap, ok := capMap[spec.Name]; ok {
			cap.SpecInfo = spec
			result[i] = cap
		} else {
			result[i] = &interfaces.SpecWithCapacity{
				SpecInfo: spec,
				Capacity: interfaces.CapacityAvailable,
			}
		}
	}

	c.JSON(http.StatusOK, result)
}

// GetSpecCapacity gets capacity status for a spec
// @Summary Get spec capacity
// @Description Get capacity availability status for a specific spec
// @Tags Specs
// @Produce json
// @Param name path string true "Spec name"
// @Success 200 {object} interfaces.CapacityEvent
// @Router /api/v1/k8s/specs/{name}/capacity [get]
func (h *SpecHandler) GetSpecCapacity(c *gin.Context) {
	name := c.Param("name")

	cap := interfaces.CapacityAvailable
	if h.capacityMgr != nil {
		cap = h.capacityMgr.GetStatus(name)
	}

	c.JSON(http.StatusOK, gin.H{
		"specName": name,
		"status":   cap,
	})
}
