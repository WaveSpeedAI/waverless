package mysql

import "waverless/pkg/store/mysql/model"

// Re-export types from model package for backward compatibility
// Gradually migrate code to use model package directly

type (
	// Database models
	Endpoint         = model.Endpoint
	Task             = model.Task
	TaskEvent        = model.TaskEvent
	AutoscalerConfig = model.AutoscalerConfig
	ScalingEvent     = model.ScalingEvent

	// Custom JSON types
	JSONMap         = model.JSONMap
	JSONStringArray = model.JSONStringArray
)

// Re-export helper functions
var (
	StringMapToJSONMap = model.StringMapToJSONMap
	JSONMapToStringMap = model.JSONMapToStringMap
)
