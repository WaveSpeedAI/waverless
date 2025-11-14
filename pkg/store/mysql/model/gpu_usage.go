package model

import "time"

// GPUUsageRecord represents a detailed GPU usage record at task level
type GPUUsageRecord struct {
	ID              int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	TaskID          string    `gorm:"column:task_id;not null;index" json:"task_id"`
	Endpoint        string    `gorm:"column:endpoint;not null;index" json:"endpoint"`
	WorkerID        *string   `gorm:"column:worker_id" json:"worker_id"`
	SpecName        *string   `gorm:"column:spec_name;index" json:"spec_name"`
	GPUCount        int       `gorm:"column:gpu_count;not null;default:0" json:"gpu_count"`
	GPUType         *string   `gorm:"column:gpu_type" json:"gpu_type"`
	GPUMemoryGB     *int      `gorm:"column:gpu_memory_gb" json:"gpu_memory_gb"`
	StartedAt       time.Time `gorm:"column:started_at;not null;index" json:"started_at"`
	CompletedAt     time.Time `gorm:"column:completed_at;not null;index" json:"completed_at"`
	DurationSeconds int       `gorm:"column:duration_seconds;not null" json:"duration_seconds"`
	GPUHours        float64   `gorm:"column:gpu_hours;not null;type:decimal(10,4)" json:"gpu_hours"`
	Status          string    `gorm:"column:status;not null" json:"status"`
	CreatedAt       time.Time `gorm:"column:created_at;not null;index" json:"created_at"`
}

// TableName returns the table name for GPUUsageRecord
func (GPUUsageRecord) TableName() string {
	return "gpu_usage_records"
}

// GPUUsageStatisticsMinute represents GPU usage statistics aggregated by minute
type GPUUsageStatisticsMinute struct {
	ID             int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	TimeBucket     time.Time `gorm:"column:time_bucket;not null;uniqueIndex:uk_time_scope" json:"time_bucket"`
	ScopeType      string    `gorm:"column:scope_type;not null;uniqueIndex:uk_time_scope" json:"scope_type"`
	ScopeValue     *string   `gorm:"column:scope_value;uniqueIndex:uk_time_scope" json:"scope_value"`
	ScopeValueKey  string    `gorm:"column:scope_value_key;->" json:"-"` // Generated column, read-only

	// Task metrics
	TotalTasks     int `gorm:"column:total_tasks;default:0" json:"total_tasks"`
	CompletedTasks int `gorm:"column:completed_tasks;default:0" json:"completed_tasks"`
	FailedTasks    int `gorm:"column:failed_tasks;default:0" json:"failed_tasks"`

	// GPU usage metrics
	TotalGPUSeconds float64 `gorm:"column:total_gpu_seconds;default:0;type:decimal(12,2)" json:"total_gpu_seconds"`
	TotalGPUHours   float64 `gorm:"column:total_gpu_hours;default:0;type:decimal(10,4)" json:"total_gpu_hours"`
	AvgGPUCount     float64 `gorm:"column:avg_gpu_count;default:0;type:decimal(10,2)" json:"avg_gpu_count"`
	MaxGPUCount     int     `gorm:"column:max_gpu_count;default:0" json:"max_gpu_count"`

	// Time range
	PeriodStart time.Time `gorm:"column:period_start;not null" json:"period_start"`
	PeriodEnd   time.Time `gorm:"column:period_end;not null" json:"period_end"`

	UpdatedAt time.Time `gorm:"column:updated_at;not null" json:"updated_at"`
}

// TableName returns the table name for GPUUsageStatisticsMinute
func (GPUUsageStatisticsMinute) TableName() string {
	return "gpu_usage_statistics_minute"
}

// GPUUsageStatisticsHourly represents GPU usage statistics aggregated by hour
type GPUUsageStatisticsHourly struct {
	ID            int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	TimeBucket    time.Time `gorm:"column:time_bucket;not null;uniqueIndex:uk_time_scope" json:"time_bucket"`
	ScopeType     string    `gorm:"column:scope_type;not null;uniqueIndex:uk_time_scope" json:"scope_type"`
	ScopeValue    *string   `gorm:"column:scope_value;uniqueIndex:uk_time_scope" json:"scope_value"`
	ScopeValueKey string    `gorm:"column:scope_value_key;->" json:"-"` // Generated column, read-only

	// Task metrics
	TotalTasks     int `gorm:"column:total_tasks;default:0" json:"total_tasks"`
	CompletedTasks int `gorm:"column:completed_tasks;default:0" json:"completed_tasks"`
	FailedTasks    int `gorm:"column:failed_tasks;default:0" json:"failed_tasks"`

	// GPU usage metrics
	TotalGPUHours float64 `gorm:"column:total_gpu_hours;default:0;type:decimal(10,4)" json:"total_gpu_hours"`
	AvgGPUCount   float64 `gorm:"column:avg_gpu_count;default:0;type:decimal(10,2)" json:"avg_gpu_count"`
	MaxGPUCount   int     `gorm:"column:max_gpu_count;default:0" json:"max_gpu_count"`

	// Peak minute info
	PeakMinute   *time.Time `gorm:"column:peak_minute" json:"peak_minute"`
	PeakGPUHours *float64   `gorm:"column:peak_gpu_hours;type:decimal(10,4)" json:"peak_gpu_hours"`

	// Time range
	PeriodStart time.Time `gorm:"column:period_start;not null" json:"period_start"`
	PeriodEnd   time.Time `gorm:"column:period_end;not null" json:"period_end"`

	UpdatedAt time.Time `gorm:"column:updated_at;not null" json:"updated_at"`
}

// TableName returns the table name for GPUUsageStatisticsHourly
func (GPUUsageStatisticsHourly) TableName() string {
	return "gpu_usage_statistics_hourly"
}

// GPUUsageStatisticsDaily represents GPU usage statistics aggregated by day
type GPUUsageStatisticsDaily struct {
	ID            int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	TimeBucket    time.Time `gorm:"column:time_bucket;not null;type:date;uniqueIndex:uk_time_scope" json:"time_bucket"`
	ScopeType     string    `gorm:"column:scope_type;not null;uniqueIndex:uk_time_scope" json:"scope_type"`
	ScopeValue    *string   `gorm:"column:scope_value;uniqueIndex:uk_time_scope" json:"scope_value"`
	ScopeValueKey string    `gorm:"column:scope_value_key;->" json:"-"` // Generated column, read-only

	// Task metrics
	TotalTasks     int `gorm:"column:total_tasks;default:0" json:"total_tasks"`
	CompletedTasks int `gorm:"column:completed_tasks;default:0" json:"completed_tasks"`
	FailedTasks    int `gorm:"column:failed_tasks;default:0" json:"failed_tasks"`

	// GPU usage metrics
	TotalGPUHours float64 `gorm:"column:total_gpu_hours;default:0;type:decimal(10,4)" json:"total_gpu_hours"`
	AvgGPUCount   float64 `gorm:"column:avg_gpu_count;default:0;type:decimal(10,2)" json:"avg_gpu_count"`
	MaxGPUCount   int     `gorm:"column:max_gpu_count;default:0" json:"max_gpu_count"`

	// GPU utilization
	AvailableGPUHours *float64 `gorm:"column:available_gpu_hours;type:decimal(10,4)" json:"available_gpu_hours"`
	UtilizationRate   *float64 `gorm:"column:utilization_rate;type:decimal(5,2)" json:"utilization_rate"`

	// Peak hour info
	PeakHour     *time.Time `gorm:"column:peak_hour" json:"peak_hour"`
	PeakGPUHours *float64   `gorm:"column:peak_gpu_hours;type:decimal(10,4)" json:"peak_gpu_hours"`

	// Time range
	PeriodStart time.Time `gorm:"column:period_start;not null" json:"period_start"`
	PeriodEnd   time.Time `gorm:"column:period_end;not null" json:"period_end"`

	UpdatedAt time.Time `gorm:"column:updated_at;not null" json:"updated_at"`
}

// TableName returns the table name for GPUUsageStatisticsDaily
func (GPUUsageStatisticsDaily) TableName() string {
	return "gpu_usage_statistics_daily"
}

