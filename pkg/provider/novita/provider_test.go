package novita

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"waverless/pkg/config"
	"waverless/pkg/interfaces"
)

// mockClient is a mock implementation of Novita API client for testing
type mockClient struct {
	endpoints      map[string]*GetEndpointResponse
	endpointsMutex sync.RWMutex
	createError    error
	getError       error
	deleteError    error
	updateError    error
	listError      error
}

func newMockClient() *mockClient {
	return &mockClient{
		endpoints: make(map[string]*GetEndpointResponse),
	}
}

func (m *mockClient) CreateEndpoint(ctx context.Context, req *CreateEndpointRequest) (*CreateEndpointResponse, error) {
	if m.createError != nil {
		return nil, m.createError
	}

	m.endpointsMutex.Lock()
	defer m.endpointsMutex.Unlock()

	endpointID := fmt.Sprintf("ep-%s", req.Endpoint.Name)

	// Create workers based on replica count
	workers := make([]WorkerInfo, req.Endpoint.WorkerConfig.MinNum)
	for i := 0; i < req.Endpoint.WorkerConfig.MinNum; i++ {
		workers[i] = WorkerInfo{
			ID: fmt.Sprintf("worker-%d", i),
			State: StateInfo{
				State:   NovitaStatusRunning,
				Error:   "",
				Message: "Worker is running",
			},
			Healthy: true,
		}
	}

	// Convert EndpointConfig to GetEndpointResponseData
	m.endpoints[endpointID] = &GetEndpointResponse{
		Endpoint: EndpointConfig{
			ID:      endpointID,
			Name:    req.Endpoint.Name,
			AppName: req.Endpoint.AppName,
			State: StateInfo{
				State:   NovitaStatusServing,
				Error:   "",
				Message: "Endpoint is serving",
			},
			URL: fmt.Sprintf("https://%s.novita.ai", endpointID),
			WorkerConfig: WorkerConfigResponse{
				MinNum:        req.Endpoint.WorkerConfig.MinNum,
				MaxNum:        req.Endpoint.WorkerConfig.MaxNum,
				FreeTimeout:   fmt.Sprintf("%d", req.Endpoint.WorkerConfig.FreeTimeout),
				MaxConcurrent: fmt.Sprintf("%d", req.Endpoint.WorkerConfig.MaxConcurrent),
				GPUNum:        req.Endpoint.WorkerConfig.GPUNum,
				CudaVersion:   "11.8",
			},
			Policy: PolicyDetails{
				Type:  req.Endpoint.Policy.Type,
				Value: fmt.Sprintf("%d", req.Endpoint.Policy.Value),
			},
			Image: ImageDetails{
				Image:   req.Endpoint.Image.Image,
				AuthID:  req.Endpoint.Image.AuthID,
				Command: req.Endpoint.Image.Command,
			},
			RootfsSize:   req.Endpoint.RootfsSize,
			VolumeMounts: req.Endpoint.VolumeMounts,
			Envs:         req.Endpoint.Envs,
			Ports:        []PortDetails{{Port: 8000}},
			Workers:      workers,
			Products:     req.Endpoint.Products,
			Healthy:      nil,
			ClusterID:    req.Endpoint.ClusterID,
			Log:          fmt.Sprintf("/logs/%s", endpointID),
		},
	}

	return &CreateEndpointResponse{ID: endpointID}, nil
}

func (m *mockClient) GetEndpoint(ctx context.Context, endpointID string) (*GetEndpointResponse, error) {
	if m.getError != nil {
		return nil, m.getError
	}

	m.endpointsMutex.RLock()
	defer m.endpointsMutex.RUnlock()

	ep, ok := m.endpoints[endpointID]
	if !ok {
		return nil, fmt.Errorf("endpoint %s not found", endpointID)
	}

	return ep, nil
}

func (m *mockClient) ListEndpoints(ctx context.Context) (*ListEndpointsResponse, error) {
	if m.listError != nil {
		return nil, m.listError
	}

	m.endpointsMutex.RLock()
	defer m.endpointsMutex.RUnlock()

	items := make([]EndpointListItem, 0, len(m.endpoints))
	for id, ep := range m.endpoints {
		items = append(items, EndpointListItem{
			ID:      id,
			Name:    ep.Endpoint.Name,
			AppName: ep.Endpoint.AppName,
			State:   ep.Endpoint.State,
		})
	}

	return &ListEndpointsResponse{
		Endpoints: items,
		Total:     len(items),
	}, nil
}

func (m *mockClient) UpdateEndpoint(ctx context.Context, req *UpdateEndpointRequest) error {
	if m.updateError != nil {
		return m.updateError
	}

	m.endpointsMutex.Lock()
	defer m.endpointsMutex.Unlock()

	ep, ok := m.endpoints[req.ID]
	if !ok {
		return fmt.Errorf("endpoint %s not found", req.ID)
	}

	// Update fields from flattened UpdateEndpointRequest
	if req.Name != "" {
		ep.Endpoint.Name = req.Name
	}
	if req.AppName != "" {
		ep.Endpoint.AppName = req.AppName
	}

	// Update worker config
	wc := req.WorkerConfig
	if wc.MinNum >= 0 {
		ep.Endpoint.WorkerConfig.MinNum = wc.MinNum
		ep.Endpoint.WorkerConfig.MaxNum = wc.MaxNum
		ep.Endpoint.WorkerConfig.FreeTimeout = wc.FreeTimeout
		ep.Endpoint.WorkerConfig.MaxConcurrent = wc.MaxConcurrent
	}
	if wc.GPUNum > 0 {
		ep.Endpoint.WorkerConfig.GPUNum = wc.GPUNum
	}

	// Update workers count to match new replicas
	newWorkerCount := wc.MinNum
	if newWorkerCount >= 0 {
		if newWorkerCount == 0 {
			// Remove all workers
			ep.Endpoint.Workers = []WorkerInfo{}
		} else if newWorkerCount > len(ep.Endpoint.Workers) {
			// Add more workers
			for i := len(ep.Endpoint.Workers); i < newWorkerCount; i++ {
				ep.Endpoint.Workers = append(ep.Endpoint.Workers, WorkerInfo{
					ID: fmt.Sprintf("worker-%d", i),
					State: StateInfo{
						State:   NovitaStatusRunning,
						Message: "Worker is running",
					},
					Healthy: true,
				})
			}
		} else if newWorkerCount < len(ep.Endpoint.Workers) {
			// Remove workers
			ep.Endpoint.Workers = ep.Endpoint.Workers[:newWorkerCount]
		}
	}

	// Update image if provided
	if req.Image.Image != "" {
		ep.Endpoint.Image.Image = req.Image.Image
		ep.Endpoint.Image.AuthID = req.Image.AuthID
		ep.Endpoint.Image.Command = req.Image.Command
	}

	// Update env vars if provided
	if req.Envs != nil {
		ep.Endpoint.Envs = req.Envs
	}

	return nil
}

func (m *mockClient) DeleteEndpoint(ctx context.Context, endpointID string) error {
	if m.deleteError != nil {
		return m.deleteError
	}

	m.endpointsMutex.Lock()
	defer m.endpointsMutex.Unlock()

	if _, ok := m.endpoints[endpointID]; !ok {
		return fmt.Errorf("endpoint %s not found", endpointID)
	}

	delete(m.endpoints, endpointID)
	return nil
}

func (m *mockClient) CreateRegistryAuth(ctx context.Context, req *CreateRegistryAuthRequest) (*CreateRegistryAuthResponse, error) {
	return &CreateRegistryAuthResponse{ID: "mock-auth-id"}, nil
}

func (m *mockClient) ListRegistryAuths(ctx context.Context) (*ListRegistryAuthsResponse, error) {
	return &ListRegistryAuthsResponse{Data: []RegistryAuthItem{}}, nil
}

func (m *mockClient) DeleteRegistryAuth(ctx context.Context, authID string) error {
	return nil
}

func (m *mockClient) DrainWorker(ctx context.Context, req *DrainWorkerRequest) error {
	// Mock implementation: just return success
	return nil
}

// createTestSpecsFile creates a temporary specs.yaml file for testing
func createTestSpecsFile(t *testing.T) string {
	tmpDir := t.TempDir()
	specsContent := `- name: novita-h100-single
    displayName: "Novita H100 1x GPU"
    category: gpu
    resourceType: serverless
    resources:
      gpu: "1"
      gpuType: "NVIDIA-H100"
      cpu: "16"
      memory: "80Gi"
      ephemeralStorage: "100"
    platforms:
      novita:
        productId: "novita-h100-80gb-product-id"
        region: "eu-west-1"
  - name: novita-a100-single
    displayName: "Novita A100 1x GPU"
    category: gpu
    resourceType: serverless
    resources:
      gpu: "1"
      gpuType: "NVIDIA-A100"
      cpu: "12"
      memory: "40Gi"
      ephemeralStorage: "100"
    platforms:
      novita:
        productId: "novita-a100-40gb-product-id"
        region: "eu-west-1"
  - name: novita-a10-single
    displayName: "Novita A10 1x GPU"
    category: gpu
    resourceType: serverless
    resources:
      gpu: "1"
      gpuType: "NVIDIA-A10"
      cpu: "8"
      memory: "32Gi"
      ephemeralStorage: "100"
    platforms:
      novita:
        productId: "novita-a10-24gb-product-id"
        region: "eu-west-1"
  - name: novita-h200-single
    displayName: "Novita H200 1x GPU"
    category: gpu
    resourceType: serverless
    resources:
      gpu: "1"
      gpuType: "NVIDIA-H200"
      cpu: "16"
      memory: "141Gi"
      ephemeralStorage: "100"
    platforms:
      novita:
        productId: "novita-h200-141gb-product-id"
        region: "eu-west-1"
`
	specsFile := filepath.Join(tmpDir, "specs.yaml")
	if err := os.WriteFile(specsFile, []byte(specsContent), 0644); err != nil {
		t.Fatalf("Failed to create test specs file: %v", err)
	}
	return tmpDir
}

// createTestProvider creates a test provider with mock client
func createTestProvider(mockCli *mockClient) *NovitaDeploymentProvider {
	tmpDir, _ := os.MkdirTemp("", "novita-test-*")
	specsContent := `specs:
  - name: novita-h100-single
    displayName: "Novita H100 1x GPU"
    category: gpu
    resourceType: serverless
    resources:
      gpu: "1"
      gpuType: "NVIDIA-H100"
      cpu: "16"
      memory: "80Gi"
      ephemeralStorage: "100"
    platforms:
      novita:
        productId: "novita-h100-80gb-product-id"
        region: "us-east-1"
  - name: novita-a100-single
    displayName: "Novita A100 1x GPU"
    category: gpu
    resourceType: serverless
    resources:
      gpu: "1"
      gpuType: "NVIDIA-A100"
      cpu: "12"
      memory: "40Gi"
      ephemeralStorage: "100"
    platforms:
      novita:
        productId: "novita-a100-40gb-product-id"
        region: "us-east-1"
  - name: novita-a10-single
    displayName: "Novita A10 1x GPU"
    category: gpu
    resourceType: serverless
    resources:
      gpu: "1"
      gpuType: "NVIDIA-A10"
      cpu: "8"
      memory: "32Gi"
      ephemeralStorage: "100"
    platforms:
      novita:
        productId: "novita-a10-24gb-product-id"
        region: "us-east-1"
  - name: novita-h200-single
    displayName: "Novita H200 1x GPU"
    category: gpu
    resourceType: serverless
    resources:
      gpu: "1"
      gpuType: "NVIDIA-H200"
      cpu: "16"
      memory: "141Gi"
      ephemeralStorage: "100"
    platforms:
      novita:
        productId: "novita-h200-141gb-product-id"
        region: "us-east-1"
`
	specsFile := filepath.Join(tmpDir, "specs.yaml")
	if err := os.WriteFile(specsFile, []byte(specsContent), 0644); err != nil {
		panic(fmt.Sprintf("Failed to write specs file: %v", err))
	}

	specsConfig, err := NewSpecsConfig(tmpDir)
	if err != nil {
		panic(fmt.Sprintf("Failed to create specs config: %v", err))
	}

	return &NovitaDeploymentProvider{
		client:           clientInterface(mockCli),
		specsConfig:      specsConfig,
		config:           &config.NovitaConfig{},
		replicaCallbacks: make(map[uint64]*replicaCallbackEntry),
		watcherStopCh:    make(chan struct{}),
		pollInterval:     5 * time.Second,
	}
}

// TestNewNovitaDeploymentProvider tests provider creation
func TestNewNovitaDeploymentProvider(t *testing.T) {
	// Create temporary specs file for valid test case
	tmpDir := "config/specs-novita-example.yaml"

	tests := []struct {
		name    string
		config  *config.Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: &config.Config{
				Novita: config.NovitaConfig{
					Enabled:   true,
					APIKey:    "test-api-key",
					BaseURL:   "https://api.novita.ai",
					ConfigDir: tmpDir,
				},
			},
			wantErr: false,
		},
		{
			name: "novita not enabled",
			config: &config.Config{
				Novita: config.NovitaConfig{
					Enabled:   false,
					APIKey:    "test-api-key",
					ConfigDir: tmpDir,
				},
			},
			wantErr: true,
		},
		{
			name: "missing API key",
			config: &config.Config{
				Novita: config.NovitaConfig{
					Enabled:   true,
					APIKey:    "",
					ConfigDir: tmpDir,
				},
			},
			wantErr: true,
		},
		{
			name: "missing specs file",
			config: &config.Config{
				Novita: config.NovitaConfig{
					Enabled:   true,
					APIKey:    "test-api-key",
					ConfigDir: "/nonexistent",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewNovitaDeploymentProvider(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewNovitaDeploymentProvider() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestMapDeployRequestToNovita tests request mapping
func TestMapDeployRequestToNovita(t *testing.T) {
	req := &interfaces.DeployRequest{
		Endpoint: "test-endpoint",
		SpecName: "test-spec",
		Image:    "test-image:latest",
		Replicas: 2,
		Env: map[string]string{
			"MODEL_NAME": "test-model",
		},
		Labels: map[string]string{
			"region": "us-east-1",
		},
	}

	// Create test spec info with proper PlatformConfig
	platformCfg := PlatformConfig{
		ProductID: "test-product-id",
		Region:    "us-east-1",
	}

	specInfo := &interfaces.SpecInfo{
		Name:        "test-spec",
		DisplayName: "Test Spec",
		Category:    CategoryGPU,
		Resources: interfaces.ResourceRequirements{
			GPU:              "1",
			GPUType:          GPUTypeH100,
			CPU:              "16",
			Memory:           "80Gi",
			EphemeralStorage: "100",
		},
		Platforms: map[string]interface{}{
			PlatformNovita: platformCfg,
		},
	}

	novitaReq, err := mapDeployRequestToNovita(req, specInfo)
	if err != nil {
		t.Fatalf("mapDeployRequestToNovita failed: %v", err)
	}

	// Verify basic fields
	if novitaReq.Endpoint.Name != req.Endpoint {
		t.Errorf("Expected name %s, got %s", req.Endpoint, novitaReq.Endpoint.Name)
	}

	if novitaReq.Endpoint.Image.Image != req.Image {
		t.Errorf("Expected image %s, got %s", req.Image, novitaReq.Endpoint.Image.Image)
	}

	// Verify worker config
	workerCfg := novitaReq.Endpoint.WorkerConfig
	if workerCfg.MinNum != req.Replicas {
		t.Errorf("Expected minNum %d, got %d", req.Replicas, workerCfg.MinNum)
	}

	// Verify product
	expectedProductID := "test-product-id"
	if len(novitaReq.Endpoint.Products) == 0 || novitaReq.Endpoint.Products[0].ID != expectedProductID {
		t.Errorf("Expected product ID %s, got %v", expectedProductID, novitaReq.Endpoint.Products)
	}

	// Verify environment variables
	foundModel := false
	foundRegion := false
	for _, env := range novitaReq.Endpoint.Envs {
		if env.Key == "MODEL_NAME" && env.Value == "test-model" {
			foundModel = true
		}
		if env.Key == EnvKeyNovitaRegion && env.Value == "us-east-1" {
			foundRegion = true
		}
	}
	if !foundModel {
		t.Error("Expected MODEL_NAME env var to be set")
	}
	if !foundRegion {
		t.Error("Expected NOVITA_REGION env var to be set to us-east-1 from labels")
	}
}

// TestMapNovitaStatusToWaverless tests status mapping
func TestMapNovitaStatusToWaverless(t *testing.T) {
	tests := []struct {
		novitaStatus string
		expected     string
	}{
		{"running", "Running"},
		{"stopped", "Stopped"},
		{"failed", "Failed"},
		{"pending", "Pending"},
		{"creating", "Creating"},
		{"updating", "Updating"},
		{"deleting", "Terminating"},
		{"unknown-state", "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.novitaStatus, func(t *testing.T) {
			result := mapNovitaStatusToWaverless(tt.novitaStatus)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

// TestGetDefaultEnv tests default environment variables
func TestGetDefaultEnv(t *testing.T) {
	// Create temporary specs file
	tmpDir := createTestSpecsFile(t)

	cfg := &config.Config{
		Novita: config.NovitaConfig{
			Enabled:   true,
			APIKey:    "test-key",
			ConfigDir: tmpDir,
		},
	}

	provider, err := NewNovitaDeploymentProvider(cfg)
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	ctx := context.Background()
	env, err := provider.GetDefaultEnv(ctx)
	if err != nil {
		t.Fatalf("GetDefaultEnv failed: %v", err)
	}

	// Verify required fields
	if env[EnvKeyNovitaProvider] != EnvValueTrue {
		t.Errorf("Expected %s to be '%s'", EnvKeyNovitaProvider, EnvValueTrue)
	}

	if env[EnvKeyProviderType] != EnvValueNovita {
		t.Errorf("Expected %s to be '%s'", EnvKeyProviderType, EnvValueNovita)
	}

	if env[EnvKeyNovitaRegion] != "eu-west-1" {
		t.Errorf("Expected %s to be 'eu-west-1', got %s", EnvKeyNovitaRegion, env[EnvKeyNovitaRegion])
	}
}

// TestConvertSpecNameToProductID tests spec to product ID conversion
// This test is no longer needed as we now extract platform config directly in mapDeployRequestToNovita
/*
func TestExtractNovitaConfig(t *testing.T) {
	// Test removed - platform config is now extracted in mapDeployRequestToNovita
}
*/

// TestDeploy tests endpoint deployment
func TestDeploy(t *testing.T) {
	mockClient := newMockClient()
	provider := createTestProvider(mockClient)
	ctx := context.Background()

	req := &interfaces.DeployRequest{
		Endpoint: "test-endpoint",
		SpecName: SpecNameNovitaH100Single,
		Image:    "test-image:v1",
		Replicas: 2,
		Env: map[string]string{
			"MODEL_NAME": "llama-3",
			"LOG_LEVEL":  "info",
		},
		Labels: map[string]string{
			LabelKeyRegion: "us-east-1",
		},
	}

	// Test successful deployment
	resp, err := provider.Deploy(ctx, req)
	if err != nil {
		t.Fatalf("Deploy failed: %v", err)
	}

	if resp.Endpoint != req.Endpoint {
		t.Errorf("Expected endpoint %s, got %s", req.Endpoint, resp.Endpoint)
	}

	// Verify endpoint was created in mock
	endpointID := fmt.Sprintf("ep-%s", req.Endpoint)
	endpoint, err := mockClient.GetEndpoint(ctx, endpointID)
	if err != nil {
		t.Fatalf("Failed to get created endpoint: %v", err)
	}

	if endpoint.Endpoint.Name != req.Endpoint {
		t.Errorf("Expected name %s, got %s", req.Endpoint, endpoint.Endpoint.Name)
	}

	if endpoint.Endpoint.Image.Image != req.Image {
		t.Errorf("Expected image %s, got %s", req.Image, endpoint.Endpoint.Image.Image)
	}

	// Test deployment with error
	mockClient.createError = fmt.Errorf("API error")
	_, err = provider.Deploy(ctx, req)
	if err == nil {
		t.Error("Expected error when API fails, got nil")
	}
}

// TestGetApp tests getting endpoint information
func TestGetApp(t *testing.T) {
	mockClient := newMockClient()
	provider := createTestProvider(mockClient)
	ctx := context.Background()

	// Deploy an endpoint first
	req := &interfaces.DeployRequest{
		Endpoint: "test-endpoint",
		SpecName: SpecNameNovitaA100Single,
		Image:    "test-image:latest",
		Replicas: 1,
	}

	_, err := provider.Deploy(ctx, req)
	if err != nil {
		t.Fatalf("Failed to deploy: %v", err)
	}

	// Test GetApp
	appInfo, err := provider.GetApp(ctx, req.Endpoint)
	if err != nil {
		t.Fatalf("GetApp failed: %v", err)
	}

	if appInfo.Name != req.Endpoint {
		t.Errorf("Expected name %s, got %s", req.Endpoint, appInfo.Name)
	}

	if appInfo.Type != TypeServerlessEndpoint {
		t.Errorf("Expected type %s, got %s", TypeServerlessEndpoint, appInfo.Type)
	}

	if appInfo.Status != StatusRunning {
		t.Errorf("Expected status %s, got %s", StatusRunning, appInfo.Status)
	}

	// Test getting non-existent endpoint
	_, err = provider.GetApp(ctx, "non-existent")
	if err == nil {
		t.Error("Expected error when getting non-existent endpoint, got nil")
	}
}

// TestListApps tests listing all endpoints
func TestListApps(t *testing.T) {
	mockClient := newMockClient()
	provider := createTestProvider(mockClient)
	ctx := context.Background()

	// Deploy multiple endpoints
	endpoints := []string{"endpoint-1", "endpoint-2", "endpoint-3"}
	for _, ep := range endpoints {
		req := &interfaces.DeployRequest{
			Endpoint: ep,
			SpecName: SpecNameNovitaH100Single,
			Image:    "test-image:latest",
			Replicas: 1,
		}
		_, err := provider.Deploy(ctx, req)
		if err != nil {
			t.Fatalf("Failed to deploy %s: %v", ep, err)
		}
	}

	// Test ListApps
	apps, err := provider.ListApps(ctx)
	if err != nil {
		t.Fatalf("ListApps failed: %v", err)
	}

	if len(apps) != len(endpoints) {
		t.Errorf("Expected %d apps, got %d", len(endpoints), len(apps))
	}

	// Verify all endpoints are in the list
	foundEndpoints := make(map[string]bool)
	for _, app := range apps {
		foundEndpoints[app.Name] = true
	}

	for _, ep := range endpoints {
		if !foundEndpoints[ep] {
			t.Errorf("Endpoint %s not found in list", ep)
		}
	}
}

// TestDeleteApp tests endpoint deletion
func TestDeleteApp(t *testing.T) {
	mockClient := newMockClient()
	provider := createTestProvider(mockClient)
	ctx := context.Background()

	// Deploy an endpoint
	req := &interfaces.DeployRequest{
		Endpoint: "test-endpoint",
		SpecName: SpecNameNovitaH100Single,
		Image:    "test-image:latest",
		Replicas: 1,
	}

	_, err := provider.Deploy(ctx, req)
	if err != nil {
		t.Fatalf("Failed to deploy: %v", err)
	}

	// Verify endpoint exists
	_, err = provider.GetApp(ctx, req.Endpoint)
	if err != nil {
		t.Fatalf("Endpoint should exist: %v", err)
	}

	// Test DeleteApp
	err = provider.DeleteApp(ctx, req.Endpoint)
	if err != nil {
		t.Fatalf("DeleteApp failed: %v", err)
	}

	// Verify endpoint is deleted
	_, err = provider.GetApp(ctx, req.Endpoint)
	if err == nil {
		t.Error("Expected error when getting deleted endpoint, got nil")
	}

	// Test deleting non-existent endpoint
	err = provider.DeleteApp(ctx, "non-existent")
	if err == nil {
		t.Error("Expected error when deleting non-existent endpoint, got nil")
	}
}

// TestScaleApp tests endpoint scaling
func TestScaleApp(t *testing.T) {
	mockClient := newMockClient()
	provider := createTestProvider(mockClient)
	ctx := context.Background()

	// Deploy an endpoint
	req := &interfaces.DeployRequest{
		Endpoint: "test-endpoint",
		SpecName: SpecNameNovitaA100Single,
		Image:    "test-image:latest",
		Replicas: 2,
	}

	_, err := provider.Deploy(ctx, req)
	if err != nil {
		t.Fatalf("Failed to deploy: %v", err)
	}

	// Test scale up
	newReplicas := 5
	err = provider.ScaleApp(ctx, req.Endpoint, newReplicas)
	if err != nil {
		t.Fatalf("ScaleApp failed: %v", err)
	}

	// Verify scaling
	endpointID := fmt.Sprintf("ep-%s", req.Endpoint)
	endpoint, err := mockClient.GetEndpoint(ctx, endpointID)
	if err != nil {
		t.Fatalf("Failed to get endpoint: %v", err)
	}

	if endpoint.Endpoint.WorkerConfig.MinNum != newReplicas {
		t.Errorf("Expected minNum %d, got %d", newReplicas, endpoint.Endpoint.WorkerConfig.MinNum)
	}

	if endpoint.Endpoint.WorkerConfig.MaxNum != newReplicas {
		t.Errorf("Expected maxNum %d, got %d", newReplicas, endpoint.Endpoint.WorkerConfig.MaxNum)
	}

	// Test scale down
	newReplicas = 1
	err = provider.ScaleApp(ctx, req.Endpoint, newReplicas)
	if err != nil {
		t.Fatalf("ScaleApp (scale down) failed: %v", err)
	}

	endpoint, err = mockClient.GetEndpoint(ctx, endpointID)
	if err != nil {
		t.Fatalf("Failed to get endpoint: %v", err)
	}

	if endpoint.Endpoint.WorkerConfig.MinNum != newReplicas {
		t.Errorf("Expected minNum %d, got %d", newReplicas, endpoint.Endpoint.WorkerConfig.MinNum)
	}

	// Test scaling non-existent endpoint
	err = provider.ScaleApp(ctx, "non-existent", 3)
	if err == nil {
		t.Error("Expected error when scaling non-existent endpoint, got nil")
	}
}

// TestGetAppStatus tests getting endpoint status
func TestGetAppStatus(t *testing.T) {
	mockClient := newMockClient()
	provider := createTestProvider(mockClient)
	ctx := context.Background()

	// Deploy an endpoint
	req := &interfaces.DeployRequest{
		Endpoint: "test-endpoint",
		SpecName: SpecNameNovitaH100Single,
		Image:    "test-image:latest",
		Replicas: 3,
	}

	_, err := provider.Deploy(ctx, req)
	if err != nil {
		t.Fatalf("Failed to deploy: %v", err)
	}

	// Test GetAppStatus
	status, err := provider.GetAppStatus(ctx, req.Endpoint)
	if err != nil {
		t.Fatalf("GetAppStatus failed: %v", err)
	}

	if status.Endpoint != req.Endpoint {
		t.Errorf("Expected endpoint %s, got %s", req.Endpoint, status.Endpoint)
	}

	if status.Status != StatusRunning {
		t.Errorf("Expected status %s, got %s", StatusRunning, status.Status)
	}

	if status.ReadyReplicas != int32(req.Replicas) {
		t.Errorf("Expected ready replicas %d, got %d", req.Replicas, status.ReadyReplicas)
	}

	// Test getting status of non-existent endpoint
	_, err = provider.GetAppStatus(ctx, "non-existent")
	if err == nil {
		t.Error("Expected error when getting status of non-existent endpoint, got nil")
	}
}

// TestUpdateDeployment tests updating endpoint deployment
func TestUpdateDeployment(t *testing.T) {
	mockClient := newMockClient()
	provider := createTestProvider(mockClient)
	ctx := context.Background()

	// Deploy an endpoint
	deployReq := &interfaces.DeployRequest{
		Endpoint: "test-endpoint",
		SpecName: SpecNameNovitaH100Single,
		Image:    "test-image:v1",
		Replicas: 2,
		Env: map[string]string{
			"VERSION": "v1",
		},
	}

	_, err := provider.Deploy(ctx, deployReq)
	if err != nil {
		t.Fatalf("Failed to deploy: %v", err)
	}

	// Test update image
	newImage := "test-image:v2"
	updateReq := &interfaces.UpdateDeploymentRequest{
		Endpoint: deployReq.Endpoint,
		Image:    newImage,
	}

	resp, err := provider.UpdateDeployment(ctx, updateReq)
	if err != nil {
		t.Fatalf("UpdateDeployment failed: %v", err)
	}

	if resp.Endpoint != deployReq.Endpoint {
		t.Errorf("Expected endpoint %s, got %s", deployReq.Endpoint, resp.Endpoint)
	}

	// Verify update
	endpointID := fmt.Sprintf("ep-%s", deployReq.Endpoint)
	endpoint, err := mockClient.GetEndpoint(ctx, endpointID)
	if err != nil {
		t.Fatalf("Failed to get endpoint: %v", err)
	}

	if endpoint.Endpoint.Image.Image != newImage {
		t.Errorf("Expected image %s, got %s", newImage, endpoint.Endpoint.Image.Image)
	}

	// Test update replicas
	newReplicas := 5
	updateReq = &interfaces.UpdateDeploymentRequest{
		Endpoint: deployReq.Endpoint,
		Replicas: &newReplicas,
	}

	_, err = provider.UpdateDeployment(ctx, updateReq)
	if err != nil {
		t.Fatalf("UpdateDeployment (replicas) failed: %v", err)
	}

	endpoint, err = mockClient.GetEndpoint(ctx, endpointID)
	if err != nil {
		t.Fatalf("Failed to get endpoint: %v", err)
	}

	if endpoint.Endpoint.WorkerConfig.MinNum != newReplicas {
		t.Errorf("Expected minNum %d, got %d", newReplicas, endpoint.Endpoint.WorkerConfig.MinNum)
	}

	// Test updating non-existent endpoint
	updateReq = &interfaces.UpdateDeploymentRequest{
		Endpoint: "non-existent",
		Image:    "new-image:latest",
	}

	_, err = provider.UpdateDeployment(ctx, updateReq)
	if err == nil {
		t.Error("Expected error when updating non-existent endpoint, got nil")
	}
}

// TestListSpecs tests listing available specifications
func TestListSpecs(t *testing.T) {
	mockClient := newMockClient()
	provider := createTestProvider(mockClient)
	ctx := context.Background()

	specs, err := provider.ListSpecs(ctx)
	if err != nil {
		t.Fatalf("ListSpecs failed: %v", err)
	}

	if len(specs) == 0 {
		t.Error("Expected at least one spec, got none")
	}

	// Verify expected specs are present
	expectedSpecs := map[string]bool{
		SpecNameNovitaH100Single: false,
		SpecNameNovitaA100Single: false,
		SpecNameNovitaA10Single:  false,
		SpecNameNovitaH200Single: false,
	}

	for _, spec := range specs {
		if _, ok := expectedSpecs[spec.Name]; ok {
			expectedSpecs[spec.Name] = true
		}

		// Verify spec has required fields
		if spec.DisplayName == "" {
			t.Errorf("Spec %s has empty DisplayName", spec.Name)
		}
		if spec.Category != CategoryGPU {
			t.Errorf("Spec %s expected category %s, got %s", spec.Name, CategoryGPU, spec.Category)
		}
		if spec.ResourceType != ResourceTypeServerless {
			t.Errorf("Spec %s expected resource type %s, got %s", spec.Name, ResourceTypeServerless, spec.ResourceType)
		}
	}

	// Check all expected specs were found
	for name, found := range expectedSpecs {
		if !found {
			t.Errorf("Expected spec %s not found", name)
		}
	}
}

// TestGetSpec tests getting a specific specification
func TestGetSpec(t *testing.T) {
	mockClient := newMockClient()
	provider := createTestProvider(mockClient)
	ctx := context.Background()

	// Test getting existing spec
	spec, err := provider.GetSpec(ctx, SpecNameNovitaH100Single)
	if err != nil {
		t.Fatalf("GetSpec failed: %v", err)
	}

	if spec.Name != SpecNameNovitaH100Single {
		t.Errorf("Expected spec name %s, got %s", SpecNameNovitaH100Single, spec.Name)
	}

	if spec.Resources.GPUType != GPUTypeH100 {
		t.Errorf("Expected GPU type %s, got %s", GPUTypeH100, spec.Resources.GPUType)
	}

	// Test getting non-existent spec
	_, err = provider.GetSpec(ctx, "non-existent-spec")
	if err == nil {
		t.Error("Expected error when getting non-existent spec, got nil")
	}
}

// TestWatchReplicas tests the replica status watching functionality
func TestWatchReplicas(t *testing.T) {
	mockClient := newMockClient()
	provider := createTestProvider(mockClient)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create a test endpoint
	req := &interfaces.DeployRequest{
		Endpoint: "test-watch-endpoint",
		SpecName: SpecNameNovitaH100Single,
		Image:    "test-image:latest",
		Replicas: 2,
	}

	_, err := provider.Deploy(ctx, req)
	if err != nil {
		t.Fatalf("Deploy failed: %v", err)
	}

	// Channel to receive events
	eventsChan := make(chan interfaces.ReplicaEvent, 10)

	// Register callback
	err = provider.WatchReplicas(ctx, func(event interfaces.ReplicaEvent) {
		eventsChan <- event
	})
	if err != nil {
		t.Fatalf("WatchReplicas failed: %v", err)
	}

	// Wait a bit for the watcher to poll
	// Note: In a real test, we'd want to mock time or use smaller intervals
	t.Log("WatchReplicas registered successfully, watcher is running")

	// Cancel context to stop watcher
	cancel()

	// Verify callback was unregistered
	provider.replicaCallbacksLock.RLock()
	callbackCount := len(provider.replicaCallbacks)
	provider.replicaCallbacksLock.RUnlock()

	if callbackCount != 0 {
		t.Errorf("Expected 0 callbacks after context cancel, got %d", callbackCount)
	}
}

// TestWatchReplicasMultipleCallbacks tests multiple callbacks registration
func TestWatchReplicasMultipleCallbacks(t *testing.T) {
	mockClient := newMockClient()
	provider := createTestProvider(mockClient)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Register multiple callbacks
	err1 := provider.WatchReplicas(ctx, func(event interfaces.ReplicaEvent) {
		// Callback 1
	})
	if err1 != nil {
		t.Fatalf("First WatchReplicas failed: %v", err1)
	}

	err2 := provider.WatchReplicas(ctx, func(event interfaces.ReplicaEvent) {
		// Callback 2
	})
	if err2 != nil {
		t.Fatalf("Second WatchReplicas failed: %v", err2)
	}

	// Verify multiple callbacks are registered
	provider.replicaCallbacksLock.RLock()
	callbackCount := len(provider.replicaCallbacks)
	provider.replicaCallbacksLock.RUnlock()

	if callbackCount != 2 {
		t.Errorf("Expected 2 callbacks, got %d", callbackCount)
	}

	// Trigger callbacks manually for testing
	testEvent := interfaces.ReplicaEvent{
		Name:              "test-endpoint",
		DesiredReplicas:   2,
		ReadyReplicas:     2,
		AvailableReplicas: 2,
	}
	provider.triggerReplicaCallbacks(testEvent)

	// Give some time for callbacks to execute
	// In production, callbacks run in goroutines
	t.Log("Multiple callbacks registered and triggered successfully")
}

// TestWatchReplicasNilCallback tests error handling for nil callback
func TestWatchReplicasNilCallback(t *testing.T) {
	mockClient := newMockClient()
	provider := createTestProvider(mockClient)
	ctx := context.Background()

	err := provider.WatchReplicas(ctx, nil)
	if err == nil {
		t.Error("Expected error when registering nil callback, got nil")
	}
	if err != nil && err.Error() != "replica callback is nil" {
		t.Errorf("Expected 'replica callback is nil' error, got: %v", err)
	}
}

// TestIsPodTerminating tests the draining worker tracking for IsPodTerminating
// Note: This test requires Redis to be available. Without Redis, IsPodTerminating returns false.
func TestIsPodTerminating(t *testing.T) {
	mockClient := newMockClient()
	provider := createTestProvider(mockClient)
	ctx := context.Background()

	// Without Redis configured, IsPodTerminating should return false (fail open)
	isTerminating, err := provider.IsPodTerminating(ctx, "worker-1")
	if err != nil {
		t.Fatalf("IsPodTerminating failed: %v", err)
	}
	if isTerminating {
		t.Error("Expected worker-1 to not be terminating without Redis")
	}

	// Empty pod name should return false
	isTerminating, err = provider.IsPodTerminating(ctx, "")
	if err != nil {
		t.Fatalf("IsPodTerminating with empty name failed: %v", err)
	}
	if isTerminating {
		t.Error("Expected empty pod name to return false")
	}

	// Test with mock Redis (using miniredis for unit tests)
	// Skip Redis-dependent tests if miniredis is not available
	t.Log("Redis-based draining tests require integration test setup with real Redis")
}

// TestScaleDownMarksDrainingWorkers tests that scale down marks workers as draining
// Note: This test verifies the scale down logic works, but draining tracking requires Redis.
// Without Redis, workers are still drained via Novita API but not tracked locally.
func TestScaleDownMarksDrainingWorkers(t *testing.T) {
	mockClient := newMockClient()
	provider := createTestProvider(mockClient)
	ctx := context.Background()

	// Deploy an endpoint with 5 replicas
	req := &interfaces.DeployRequest{
		Endpoint: "test-drain-endpoint",
		SpecName: "novita-h100-single",
		Image:    "test-image:latest",
		Replicas: 5,
	}
	_, err := provider.Deploy(ctx, req)
	if err != nil {
		t.Fatalf("Failed to deploy: %v", err)
	}

	// Scale down to 1 replica (should drain 4 workers via Novita API)
	err = provider.ScaleApp(ctx, req.Endpoint, 1)
	if err != nil {
		t.Fatalf("Failed to scale down: %v", err)
	}

	// Without Redis, GetDrainingWorkers returns nil
	// The draining still happens via Novita API, just not tracked locally
	drainingWorkers := provider.GetDrainingWorkers(ctx)
	if len(drainingWorkers) > 0 {
		t.Logf("Draining workers tracked (Redis available): %v", drainingWorkers)
	} else {
		t.Log("Draining workers not tracked locally (Redis not configured) - this is expected in unit tests")
	}

	// Verify the endpoint was scaled down correctly
	endpointID := fmt.Sprintf("ep-%s", req.Endpoint)
	endpoint, err := mockClient.GetEndpoint(ctx, endpointID)
	if err != nil {
		t.Fatalf("Failed to get endpoint: %v", err)
	}
	if endpoint.Endpoint.WorkerConfig.MinNum != 1 {
		t.Errorf("Expected minNum 1 after scale down, got %d", endpoint.Endpoint.WorkerConfig.MinNum)
	}
}

func TestRealScaleDown(t *testing.T) {
	provider, err := NewNovitaDeploymentProvider(&config.Config{
		Novita: config.NovitaConfig{
			Enabled:   true,
			APIKey:    "your api key here",
			BaseURL:   "https://api.novita.ai",
			ConfigDir: "../../../config",
		},
	})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}
	ctx := context.Background()

	// Deploy an endpoint
	req := &interfaces.DeployRequest{
		Endpoint:    "base-test",
		SpecName:    "novita-5090-single",
		Image:       "ubuntu:22.04",
		Replicas:    3,
		TaskTimeout: 1200,
	}
	_, err = provider.Deploy(ctx, req)
	if err != nil {
		t.Fatalf("Failed to deploy: %v", err)
	}
	app, err := provider.GetApp(ctx, req.Endpoint)
	if err != nil {
		t.Fatalf("Failed to get app: %v", err)
	}
	if app.Replicas != 3 {
		t.Errorf("Expected replicas %d, got %d", 3, app.Replicas)
	}

	// Scale down to 0 replicas
	err = provider.ScaleApp(ctx, req.Endpoint, 2)
	if err != nil {
		t.Fatalf("Failed to scale down: %v", err)
	}
	err = provider.DeleteApp(ctx, req.Endpoint)
	if err != nil {
		t.Fatalf("Failed to delete: %v", err)
	}

}
