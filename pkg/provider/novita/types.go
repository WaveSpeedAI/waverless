package novita

// Novita API types based on https://novita.ai/docs/api-reference/serverless-create-endpoint

// ========================================
// Request Types (for Create/Update API)
// ========================================

// CreateEndpointRequest represents the request to create a Novita endpoint
type CreateEndpointRequest struct {
	Endpoint EndpointCreateConfig `json:"endpoint"`
}

// EndpointCreateConfig represents the endpoint configuration for create/update requests
type EndpointCreateConfig struct {
	Name         string          `json:"name,omitempty"`         // Endpoint name
	AppName      string          `json:"appName,omitempty"`      // Application name (appears in URL)
	WorkerConfig WorkerConfig    `json:"workerConfig"`           // Worker configuration
	Ports        []PortConfig    `json:"ports"`                  // HTTP ports
	Policy       PolicyConfig    `json:"policy"`                 // Auto-scaling policy
	Image        ImageConfig     `json:"image"`                  // Image information
	Products     []ProductConfig `json:"products"`               // Product information
	RootfsSize   int             `json:"rootfsSize"`             // System disk size (GB)
	VolumeMounts []VolumeMount   `json:"volumeMounts,omitempty"` // Storage information
	ClusterID    string          `json:"clusterID,omitempty"`    // Cluster ID
	Envs         []EnvVar        `json:"envs,omitempty"`         // Environment variables
	Healthy      *HealthCheck    `json:"healthy,omitempty"`      // Health check endpoint
}

// ========================================
// Response Types (from Get/List API)
// ========================================

// EndpointConfig represents the full endpoint data from Get/List Endpoint API
type EndpointConfig struct {
	ID           string               `json:"id"`           // Endpoint ID
	Name         string               `json:"name"`         // Endpoint name
	AppName      string               `json:"appName"`      // Application name
	State        StateInfo            `json:"state"`        // Endpoint state
	URL          string               `json:"url"`          // Endpoint URL
	WorkerConfig WorkerConfigResponse `json:"workerConfig"` // Worker configuration (from API response)
	Policy       PolicyDetails        `json:"policy"`       // Auto-scaling policy
	Image        ImageDetails         `json:"image"`        // Image information
	RootfsSize   int                  `json:"rootfsSize"`   // System disk size (GB)
	VolumeMounts []VolumeMount        `json:"volumeMounts"` // Storage information
	Envs         []EnvVar             `json:"envs"`         // Environment variables
	Ports        []PortDetails        `json:"ports"`        // Port information
	Workers      []WorkerInfo         `json:"workers"`      // Worker information
	Products     []ProductConfig      `json:"products"`     // Product information
	Healthy      *HealthCheckDetails  `json:"healthy"`      // Health check configuration
	ClusterID    string               `json:"clusterID"`    // Cluster ID
	Log          string               `json:"log"`          // Log path
}

// ========================================
// Common Types
// ========================================

// StateInfo represents the state information from Get Endpoint response
type StateInfo struct {
	State   string `json:"state"`   // State: "serving", "stopped", "failed", etc.
	Error   string `json:"error"`   // Error code if any
	Message string `json:"message"` // State message
}

// WorkerConfig represents worker configuration (for Create API - int types)
type WorkerConfig struct {
	MinNum         int    `json:"minNum"`                   // Minimum number of workers
	MaxNum         int    `json:"maxNum"`                   // Maximum number of workers
	FreeTimeout    int    `json:"freeTimeout"`              // Idle timeout (seconds)
	MaxConcurrent  int    `json:"maxConcurrent"`            // Maximum concurrency
	GPUNum         int    `json:"gpuNum"`                   // Number of GPUs per worker
	RequestTimeout int    `json:"requestTimeout,omitempty"` // Request timeout (seconds)
	CudaVersion    string `json:"cudaVersion,omitempty"`    // CUDA version
}

// WorkerConfigResponse represents worker configuration from API responses (Get/List - string types)
type WorkerConfigResponse struct {
	MinNum         int    `json:"minNum"`         // Minimum number of workers
	MaxNum         int    `json:"maxNum"`         // Maximum number of workers
	FreeTimeout    string `json:"freeTimeout"`    // Idle timeout (string: "300")
	MaxConcurrent  string `json:"maxConcurrent"`  // Maximum concurrency (string: "1")
	GPUNum         int    `json:"gpuNum"`         // Number of GPUs per worker
	RequestTimeout int    `json:"requestTimeout"` // Request timeout (seconds)
	CudaVersion    string `json:"cudaVersion"`    // CUDA version
}

// PolicyDetails represents policy configuration in Get Endpoint response
type PolicyDetails struct {
	Type  string `json:"type"`  // Policy type
	Value string `json:"value"` // Policy value (string in response)
}

// ImageDetails represents image configuration in Get Endpoint response
type ImageDetails struct {
	Image   string `json:"image"`   // Image URL
	AuthID  string `json:"authId"`  // Private image credential ID
	Command string `json:"command"` // Container startup command
}

// PortDetails represents port configuration in Get Endpoint response
type PortDetails struct {
	Port int `json:"port"` // Port number (int in response)
}

// WorkerInfo represents individual worker information
type WorkerInfo struct {
	ID      string    `json:"id"`      // Worker ID
	State   StateInfo `json:"state"`   // Worker state
	Log     string    `json:"log"`     // Log path
	Metrics string    `json:"metrics"` // Metrics path
	Healthy bool      `json:"healthy"` // Health status
}

// HealthCheckDetails represents health check configuration in Get Endpoint response
type HealthCheckDetails struct {
	Path             string `json:"path"`             // Health check path
	InitialDelay     int    `json:"initialDelay"`     // Initial delay in seconds
	Period           int    `json:"period"`           // Check period in seconds
	Timeout          int    `json:"timeout"`          // Timeout in seconds
	SuccessThreshold int    `json:"successThreshold"` // Success threshold
	FailureThreshold int    `json:"failureThreshold"` // Failure threshold
}

// PolicyConfig represents auto-scaling policy (for create/update requests - int value)
type PolicyConfig struct {
	Type  string `json:"type"`  // Policy type: "queue" or "concurrency"
	Value int    `json:"value"` // Policy value
}

// ImageConfig represents image configuration (for create/update requests)
type ImageConfig struct {
	Image   string `json:"image"`             // Image URL
	AuthID  string `json:"authId,omitempty"`  // Private image credential ID
	Command string `json:"command,omitempty"` // Container startup command
}

// PortConfig represents port configuration
type PortConfig struct {
	Port int `json:"port"` // HTTP port
}

// ProductConfig represents product configuration
type ProductConfig struct {
	ID string `json:"id"` // Product ID
}

// VolumeMount represents storage configuration
type VolumeMount struct {
	Type      string `json:"type"`                // Storage type: "local" or "network"
	Size      int    `json:"size,omitempty"`      // Local storage size (GB)
	ID        string `json:"id,omitempty"`        // Network storage ID
	MountPath string `json:"mountPath,omitempty"` // Mount path
}

// EnvVar represents environment variable
type EnvVar struct {
	Key   string `json:"key"`   // Environment variable name
	Value string `json:"value"` // Environment variable value
}

// HealthCheck represents health check configuration (simple version for create)
type HealthCheck struct {
	Path string `json:"path"` // Health check path
}

// CreateEndpointResponse represents the response from creating an endpoint
type CreateEndpointResponse struct {
	ID string `json:"id"` // Created endpoint ID
}

// UpdateEndpointRequest represents the request to update an endpoint
// Note: Update API uses a flattened structure with string types for worker config
type UpdateEndpointRequest struct {
	ID           string               `json:"id"`                // Endpoint ID
	Name         string               `json:"name"`              // Endpoint name
	AppName      string               `json:"appName"`           // Application name
	WorkerConfig WorkerConfigResponse `json:"workerConfig"`      // Worker configuration (string types)
	Policy       PolicyResponse       `json:"policy"`            // Auto-scaling policy (string value)
	Image        ImageConfig          `json:"image"`             // Image configuration
	RootfsSize   int                  `json:"rootfsSize"`        // System disk size (GB)
	VolumeMounts []VolumeMount        `json:"volumeMounts"`      // Storage information
	Envs         []EnvVar             `json:"envs,omitempty"`    // Environment variables
	Ports        []PortConfig         `json:"ports"`             // HTTP ports
	Workers      []WorkerInfo         `json:"workers"`           // Workers list (can be null)
	Products     []ProductConfig      `json:"products"`          // Product information
	Healthy      *HealthCheck         `json:"healthy,omitempty"` // Health check configuration
}

// PolicyResponse represents policy configuration from API responses (string value)
type PolicyResponse struct {
	Type  string `json:"type"`  // Policy type: "queue" or "concurrency"
	Value string `json:"value"` // Policy value (string: "60")
}

// GetEndpointResponse represents the response from getting an endpoint
// The actual API response has only one field: "endpoint"
type GetEndpointResponse struct {
	Endpoint EndpointConfig `json:"endpoint"` // Endpoint data
}

// ListEndpointsResponse represents the response from listing endpoints
type ListEndpointsResponse struct {
	Endpoints []EndpointListItem `json:"endpoints"` // List of endpoints
	Total     int                `json:"total"`     // Total count
}

// EndpointListItem represents a single endpoint in the list
// Note: Novita's ListEndpoints API returns full endpoint details with string types
type EndpointListItem struct {
	ID           string               `json:"id"`           // Endpoint ID
	Name         string               `json:"name"`         // Endpoint name
	AppName      string               `json:"appName"`      // Application name
	State        StateInfo            `json:"state"`        // Current state
	WorkerConfig WorkerConfigResponse `json:"workerConfig"` // Worker configuration (string types)
	Workers      []WorkerInfo         `json:"workers"`      // Workers list (full details)
	Policy       PolicyDetails        `json:"policy"`       // Policy configuration (string value)
	Image        ImageDetails         `json:"image"`        // Image configuration
	CreatedAt    string               `json:"createdAt"`    // Creation time
	UpdatedAt    string               `json:"updatedAt"`    // Last update time
}

// DeleteEndpointRequest represents the request to delete an endpoint
type DeleteEndpointRequest struct {
	ID string `json:"id"` // Endpoint ID
}

// ErrorResponse represents an error response from Novita API
type ErrorResponse struct {
	Code    int    `json:"code"`    // Error code
	Message string `json:"message"` // Error message
}

// ========================================
// Container Registry Auth Types
// ========================================

// CreateRegistryAuthRequest represents the request to create a container registry auth
type CreateRegistryAuthRequest struct {
	Name     string `json:"name"`     // Auth name (registry URL)
	Username string `json:"username"` // Username
	Password string `json:"password"` // Password
}

// CreateRegistryAuthResponse represents the response from creating a registry auth
type CreateRegistryAuthResponse struct {
	ID string `json:"id"` // Created auth ID
}

// ListRegistryAuthsResponse represents the response from listing registry auths
type ListRegistryAuthsResponse struct {
	Data []RegistryAuthItem `json:"data"` // List of registry auths
}

// RegistryAuthItem represents a single registry auth item
type RegistryAuthItem struct {
	ID       string `json:"id"`       // Auth ID
	Name     string `json:"name"`     // Auth name (registry URL)
	Username string `json:"username"` // Username
	Password string `json:"password"` // Password (may be masked)
}

// DeleteRegistryAuthRequest represents the request to delete a registry auth
type DeleteRegistryAuthRequest struct {
	ID string `json:"id"` // Auth ID to delete
}

// ========================================
// Worker Drain Types
// ========================================

// DrainWorkerRequest represents the request to drain a worker
type DrainWorkerRequest struct {
	WorkerID string `json:"workerID"` // Worker ID to drain
	Drain    bool   `json:"drain"`    // true to drain, false to undrain
}

// DrainWorkerResponse represents the response from draining a worker
type DrainWorkerResponse struct {
	Success bool   `json:"success,omitempty"`
	Message string `json:"message,omitempty"`
}
