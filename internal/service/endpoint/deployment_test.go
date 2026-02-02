package endpoint

import (
	"context"
	"errors"
	"testing"
	"time"

	"waverless/pkg/config"
	"waverless/pkg/interfaces"
)

// mockDeploymentProvider is a mock implementation of interfaces.DeploymentProvider
type mockDeploymentProvider struct {
	deployFunc           func(ctx context.Context, req *interfaces.DeployRequest) (*interfaces.DeployResponse, error)
	updateDeploymentFunc func(ctx context.Context, req *interfaces.UpdateDeploymentRequest) (*interfaces.DeployResponse, error)
	deleteAppFunc        func(ctx context.Context, name string) error
}

func (m *mockDeploymentProvider) Deploy(ctx context.Context, req *interfaces.DeployRequest) (*interfaces.DeployResponse, error) {
	if m.deployFunc != nil {
		return m.deployFunc(ctx, req)
	}
	return &interfaces.DeployResponse{
		Endpoint:  req.Endpoint,
		Message:   "deployed",
		CreatedAt: time.Now().Format(time.RFC3339),
	}, nil
}

func (m *mockDeploymentProvider) UpdateDeployment(ctx context.Context, req *interfaces.UpdateDeploymentRequest) (*interfaces.DeployResponse, error) {
	if m.updateDeploymentFunc != nil {
		return m.updateDeploymentFunc(ctx, req)
	}
	return &interfaces.DeployResponse{
		Endpoint: req.Endpoint,
		Message:  "updated",
	}, nil
}

func (m *mockDeploymentProvider) DeleteApp(ctx context.Context, name string) error {
	if m.deleteAppFunc != nil {
		return m.deleteAppFunc(ctx, name)
	}
	return nil
}

// Implement other required methods with no-op implementations
func (m *mockDeploymentProvider) GetApp(ctx context.Context, name string) (*interfaces.AppInfo, error) {
	return nil, nil
}
func (m *mockDeploymentProvider) ListApps(ctx context.Context) ([]*interfaces.AppInfo, error) {
	return nil, nil
}
func (m *mockDeploymentProvider) GetAppLogs(ctx context.Context, name string, lines int, podNames ...string) (string, error) {
	return "", nil
}
func (m *mockDeploymentProvider) ListSpecs(ctx context.Context) ([]*interfaces.SpecInfo, error) {
	return nil, nil
}
func (m *mockDeploymentProvider) GetSpec(ctx context.Context, name string) (*interfaces.SpecInfo, error) {
	return nil, nil
}
func (m *mockDeploymentProvider) PreviewDeploymentYAML(ctx context.Context, req *interfaces.DeployRequest) (string, error) {
	return "", nil
}
func (m *mockDeploymentProvider) ListPVCs(ctx context.Context) ([]*interfaces.PVCInfo, error) {
	return nil, nil
}
func (m *mockDeploymentProvider) GetDefaultEnv(ctx context.Context) (map[string]string, error) {
	return nil, nil
}
func (m *mockDeploymentProvider) ScaleApp(ctx context.Context, endpoint string, replicas int) error {
	return nil
}
func (m *mockDeploymentProvider) GetAppStatus(ctx context.Context, endpoint string) (*interfaces.AppStatus, error) {
	return nil, nil
}
func (m *mockDeploymentProvider) WatchReplicas(ctx context.Context, callback interfaces.ReplicaCallback) error {
	return nil
}
func (m *mockDeploymentProvider) GetPods(ctx context.Context, endpoint string) ([]*interfaces.PodInfo, error) {
	return nil, nil
}
func (m *mockDeploymentProvider) DescribePod(ctx context.Context, endpoint string, podName string) (*interfaces.PodDetail, error) {
	return nil, nil
}
func (m *mockDeploymentProvider) GetPodYAML(ctx context.Context, endpoint string, podName string) (string, error) {
	return "", nil
}
func (m *mockDeploymentProvider) IsPodTerminating(ctx context.Context, podName string) (bool, error) {
	return false, nil
}

// TestDeploymentManager_Deploy_ImageFormatValidation tests that image format validation
// is performed before deployment.
// Validates: Requirements 1.1, 1.2
func TestDeploymentManager_Deploy_ImageFormatValidation(t *testing.T) {
	tests := []struct {
		name        string
		image       string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid simple image",
			image:       "nginx",
			expectError: false,
		},
		{
			name:        "valid image with tag",
			image:       "nginx:latest",
			expectError: false,
		},
		{
			name:        "valid image with registry",
			image:       "gcr.io/project/image:v1.0",
			expectError: false,
		},
		{
			name:        "invalid image - empty",
			image:       "",
			expectError: false, // Empty image is allowed (skips validation)
		},
		{
			name:        "invalid image - uppercase",
			image:       "NGINX",
			expectError: true,
			errorMsg:    "image format validation failed",
		},
		{
			name:        "invalid image - special characters",
			image:       "nginx$invalid",
			expectError: true,
			errorMsg:    "image format validation failed",
		},
		{
			name:        "invalid image - whitespace",
			image:       " nginx ",
			expectError: true,
			errorMsg:    "image format validation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup: Create deployment manager with disabled image existence check
			// to isolate format validation testing
			config.GlobalConfig = &config.Config{
				ImageValidation: config.ImageValidationConfig{
					Enabled:       false, // Disable existence check
					Timeout:       30 * time.Second,
					CacheDuration: 1 * time.Hour,
					SkipOnTimeout: true,
				},
			}

			provider := &mockDeploymentProvider{}
			dm := NewDeploymentManager(provider, nil, nil)

			req := &interfaces.DeployRequest{
				Endpoint: "test-endpoint",
				Image:    tt.image,
				Replicas: 1,
			}

			_, err := dm.Deploy(context.Background(), req, nil)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				} else if tt.errorMsg != "" && !containsString(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// TestDeploymentManager_Deploy_NilProvider tests that Deploy returns error when provider is nil
func TestDeploymentManager_Deploy_NilProvider(t *testing.T) {
	dm := &DeploymentManager{
		provider: nil,
	}

	req := &interfaces.DeployRequest{
		Endpoint: "test-endpoint",
		Image:    "nginx",
	}

	_, err := dm.Deploy(context.Background(), req, nil)
	if err == nil {
		t.Error("expected error for nil provider")
	}
	if !containsString(err.Error(), "deployment provider not configured") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestDeploymentManager_Deploy_NilRequest tests that Deploy returns error when request is nil
func TestDeploymentManager_Deploy_NilRequest(t *testing.T) {
	provider := &mockDeploymentProvider{}
	dm := NewDeploymentManager(provider, nil, nil)

	_, err := dm.Deploy(context.Background(), nil, nil)
	if err == nil {
		t.Error("expected error for nil request")
	}
	if !containsString(err.Error(), "deploy request is nil") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestDeploymentManager_Deploy_ProviderError tests that provider errors are propagated
func TestDeploymentManager_Deploy_ProviderError(t *testing.T) {
	config.GlobalConfig = &config.Config{
		ImageValidation: config.ImageValidationConfig{
			Enabled: false,
		},
	}

	expectedErr := errors.New("provider deployment failed")
	provider := &mockDeploymentProvider{
		deployFunc: func(ctx context.Context, req *interfaces.DeployRequest) (*interfaces.DeployResponse, error) {
			return nil, expectedErr
		},
	}

	dm := NewDeploymentManager(provider, nil, nil)

	req := &interfaces.DeployRequest{
		Endpoint: "test-endpoint",
		Image:    "nginx",
	}

	_, err := dm.Deploy(context.Background(), req, nil)
	if err == nil {
		t.Error("expected error from provider")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected provider error, got: %v", err)
	}
}

// TestDeploymentManager_Deploy_Success tests successful deployment
func TestDeploymentManager_Deploy_Success(t *testing.T) {
	config.GlobalConfig = &config.Config{
		ImageValidation: config.ImageValidationConfig{
			Enabled: false,
		},
	}

	provider := &mockDeploymentProvider{
		deployFunc: func(ctx context.Context, req *interfaces.DeployRequest) (*interfaces.DeployResponse, error) {
			return &interfaces.DeployResponse{
				Endpoint:  req.Endpoint,
				Message:   "deployed successfully",
				CreatedAt: time.Now().Format(time.RFC3339),
			}, nil
		},
	}

	dm := NewDeploymentManager(provider, nil, nil)

	req := &interfaces.DeployRequest{
		Endpoint: "test-endpoint",
		Image:    "nginx:latest",
		Replicas: 2,
	}

	resp, err := dm.Deploy(context.Background(), req, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Error("expected response but got nil")
	}
	if resp.Endpoint != "test-endpoint" {
		t.Errorf("expected endpoint 'test-endpoint', got %q", resp.Endpoint)
	}
}

// TestDeploymentManager_NewDeploymentManager_DefaultConfig tests that default config is used
// when GlobalConfig is nil
func TestDeploymentManager_NewDeploymentManager_DefaultConfig(t *testing.T) {
	// Save and restore global config
	savedConfig := config.GlobalConfig
	defer func() { config.GlobalConfig = savedConfig }()

	config.GlobalConfig = nil

	provider := &mockDeploymentProvider{}
	dm := NewDeploymentManager(provider, nil, nil)

	if dm.imageValidator == nil {
		t.Error("expected imageValidator to be initialized")
	}
	if dm.imageConfig == nil {
		t.Error("expected imageConfig to be initialized")
	}

	// Verify default values
	defaults := config.DefaultImageValidationConfig()
	if dm.imageConfig.Enabled != defaults.Enabled {
		t.Errorf("expected Enabled=%v, got %v", defaults.Enabled, dm.imageConfig.Enabled)
	}
	if dm.imageConfig.Timeout != defaults.Timeout {
		t.Errorf("expected Timeout=%v, got %v", defaults.Timeout, dm.imageConfig.Timeout)
	}
}

// TestDeploymentManager_NewDeploymentManager_WithConfig tests that config is properly used
func TestDeploymentManager_NewDeploymentManager_WithConfig(t *testing.T) {
	config.GlobalConfig = &config.Config{
		ImageValidation: config.ImageValidationConfig{
			Enabled:       true,
			Timeout:       60 * time.Second,
			CacheDuration: 2 * time.Hour,
			SkipOnTimeout: false,
		},
	}

	provider := &mockDeploymentProvider{}
	dm := NewDeploymentManager(provider, nil, nil)

	if dm.imageConfig.Enabled != true {
		t.Error("expected Enabled=true")
	}
	if dm.imageConfig.Timeout != 60*time.Second {
		t.Errorf("expected Timeout=60s, got %v", dm.imageConfig.Timeout)
	}
	if dm.imageConfig.CacheDuration != 2*time.Hour {
		t.Errorf("expected CacheDuration=2h, got %v", dm.imageConfig.CacheDuration)
	}
	if dm.imageConfig.SkipOnTimeout != false {
		t.Error("expected SkipOnTimeout=false")
	}
}

// containsString checks if s contains substr
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
