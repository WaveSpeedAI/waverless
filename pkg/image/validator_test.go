package image

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"waverless/pkg/interfaces"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidateImageFormat_ValidFormats tests that valid image formats are accepted.
// Validates: Requirements 1.1, 1.3
func TestValidateImageFormat_ValidFormats(t *testing.T) {
	validator := NewImageValidator(nil)

	testCases := []struct {
		name  string
		image string
	}{
		// Docker Hub - simple names
		{"simple name", "nginx"},
		{"simple name with tag", "nginx:latest"},
		{"simple name with version tag", "nginx:1.0"},
		{"simple name with complex tag", "nginx:1.21.6-alpine"},

		// Docker Hub - with namespace
		{"library namespace", "library/nginx"},
		{"library namespace with tag", "library/nginx:1.0"},
		{"user namespace", "wavespeed/model-deploy"},
		{"user namespace with tag", "wavespeed/model-deploy:v1.0.0"},
		{"user namespace with timestamp tag", "wavespeed/model-deploy:wan_i2v-default-202511051642"},

		// Private registries
		{"private registry", "registry.example.com/myimage"},
		{"private registry with tag", "registry.example.com/myimage:latest"},
		{"private registry with port", "registry.example.com:5000/myimage"},
		{"private registry with port and tag", "registry.example.com:5000/myimage:v1"},
		{"private registry with namespace", "registry.example.com/namespace/myimage:tag"},

		// Cloud registries - GCR
		{"gcr.io", "gcr.io/project/image"},
		{"gcr.io with tag", "gcr.io/project/image:latest"},
		{"gcr.io with nested path", "gcr.io/project/subdir/image:v1"},

		// Cloud registries - AWS ECR
		{"aws ecr", "123456789012.dkr.ecr.us-east-1.amazonaws.com/myrepo"},
		{"aws ecr with tag", "123456789012.dkr.ecr.us-east-1.amazonaws.com/myrepo:latest"},

		// Cloud registries - GitHub Container Registry
		{"ghcr.io", "ghcr.io/owner/image"},
		{"ghcr.io with tag", "ghcr.io/owner/image:sha-abc123"},

		// Cloud registries - Azure Container Registry
		{"azure acr", "myregistry.azurecr.io/myimage"},
		{"azure acr with tag", "myregistry.azurecr.io/myimage:v1.0"},

		// With digest
		{"with digest", "nginx@sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abc1"},
		{"with tag and digest", "nginx:latest@sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abc1"},

		// Special valid cases
		{"underscore in name", "my_image"},
		{"double underscore", "my__image"},
		{"period in name", "my.image"},
		{"hyphen in name", "my-image"},
		{"localhost registry", "localhost/myimage"},
		{"localhost with port", "localhost:5000/myimage"},
		{"ip address registry", "192.168.1.1/myimage"},
		{"ip address with port", "192.168.1.1:5000/myimage:tag"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := validator.ValidateImageFormat(tc.image)
			assert.NoError(t, err, "Expected valid image format: %s", tc.image)
		})
	}
}

// TestValidateImageFormat_InvalidFormats tests that invalid image formats are rejected.
// Validates: Requirements 1.1, 1.2
func TestValidateImageFormat_InvalidFormats(t *testing.T) {
	validator := NewImageValidator(nil)

	testCases := []struct {
		name        string
		image       string
		errContains string
	}{
		// Empty and whitespace
		{"empty string", "", "cannot be empty"},
		{"whitespace only", "   ", "leading or trailing whitespace"},
		{"leading whitespace", " nginx", "leading or trailing whitespace"},
		{"trailing whitespace", "nginx ", "leading or trailing whitespace"},

		// Invalid characters
		{"invalid char $", "nginx$latest", "invalid character"},
		{"invalid char !", "nginx!latest", "invalid character"},
		{"invalid char #", "nginx#latest", "invalid character"},
		{"invalid char %", "nginx%latest", "invalid character"},
		{"invalid char &", "nginx&latest", "invalid character"},
		{"invalid char *", "nginx*latest", "invalid character"},

		// Invalid tag format
		{"empty tag", "nginx:", "tag cannot be empty"},
		{"tag starting with hyphen", "nginx:-latest", "must start with"},
		{"tag starting with period", "nginx:.latest", "must start with"},

		// Invalid digest format
		{"empty digest", "nginx@", "digest cannot be empty"},
		{"digest without colon", "nginx@sha256abc123", "algorithm:hex"},
		{"digest with short hash", "nginx@sha256:abc", "at least 32"},
		{"digest with invalid hex", "nginx@sha256:xyz123xyz123xyz123xyz123xyz123xyz123", "invalid character"},

		// Invalid repository format
		{"uppercase in repo", "MyImage", "must be lowercase"},
		{"uppercase in namespace", "MyNamespace/image", "must be lowercase"},
		{"starting with hyphen", "-nginx", "invalid character"},
		{"starting with period", ".nginx", "invalid character"},
		{"ending with hyphen", "nginx-", "must end with"},
		{"ending with period", "nginx.", "invalid registry address format"},
		{"double slash", "registry//image", "empty path component"},
		{"triple period", "my...image", "invalid registry address format"},

		// Invalid registry format
		{"invalid registry port", "registry.example.com:abc/image", "invalid registry port"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := validator.ValidateImageFormat(tc.image)
			require.Error(t, err, "Expected invalid image format: %s", tc.image)
			assert.Contains(t, err.Error(), tc.errContains, "Error message should contain: %s", tc.errContains)
		})
	}
}

// TestValidateImageFormat_ErrorMessages tests that error messages are descriptive and in English.
// Validates: Requirement 1.2
func TestValidateImageFormat_ErrorMessages(t *testing.T) {
	validator := NewImageValidator(nil)

	testCases := []struct {
		name        string
		image       string
		errContains string
	}{
		{"empty image", "", "image name cannot be empty"},
		{"whitespace", " nginx", "image name cannot contain leading or trailing whitespace"},
		{"invalid char", "nginx$", "image name contains invalid character"},
		{"empty tag", "nginx:", "image tag cannot be empty"},
		{"uppercase", "Nginx", "image repository path must be lowercase"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := validator.ValidateImageFormat(tc.image)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.errContains)
		})
	}
}

// TestNewImageValidator tests the ImageValidator constructor.
func TestNewImageValidator(t *testing.T) {
	t.Run("with nil config uses defaults", func(t *testing.T) {
		validator := NewImageValidator(nil)
		require.NotNil(t, validator)
		assert.NotNil(t, validator.httpClient)
		assert.NotNil(t, validator.cache)
		assert.NotNil(t, validator.config)
		assert.True(t, validator.config.Enabled)
	})

	t.Run("with custom config", func(t *testing.T) {
		config := &ImageValidationConfig{
			Enabled:       false,
			Timeout:       10,
			CacheDuration: 30,
			SkipOnTimeout: false,
		}
		validator := NewImageValidator(config)
		require.NotNil(t, validator)
		assert.False(t, validator.config.Enabled)
	})
}

// TestDefaultImageValidationConfig tests the default configuration.
func TestDefaultImageValidationConfig(t *testing.T) {
	config := DefaultImageValidationConfig()
	require.NotNil(t, config)
	assert.True(t, config.Enabled)
	assert.Equal(t, 30*1000000000, int(config.Timeout))         // 30 seconds in nanoseconds
	assert.Equal(t, 3600*1000000000, int(config.CacheDuration)) // 1 hour in nanoseconds
	assert.True(t, config.SkipOnTimeout)
}

// TestCheckImageExists_InvalidFormat tests that invalid image formats return validation errors.
// Validates: Requirements 2.1
func TestCheckImageExists_InvalidFormat(t *testing.T) {
	validator := NewImageValidator(nil)
	ctx := context.Background()

	testCases := []struct {
		name  string
		image string
	}{
		{"empty string", ""},
		{"uppercase", "MyImage"},
		{"invalid char", "nginx$latest"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := validator.CheckImageExists(ctx, tc.image, nil)
			require.NoError(t, err)
			assert.False(t, result.Valid)
			assert.False(t, result.Exists)
			assert.NotEmpty(t, result.Error)
		})
	}
}

// TestCheckImageExists_CacheHit tests that cached results are returned without making HTTP requests.
// Validates: Requirements 2.5
func TestCheckImageExists_CacheHit(t *testing.T) {
	validator := NewImageValidator(nil)
	ctx := context.Background()

	// Pre-populate cache
	cachedResult := &interfaces.ImageValidationResult{
		Valid:      true,
		Exists:     true,
		Accessible: true,
		CheckedAt:  time.Now(),
	}
	validator.cache.Set("nginx:latest", cachedResult, time.Hour)

	// Check should return cached result
	result, err := validator.CheckImageExists(ctx, "nginx:latest", nil)
	require.NoError(t, err)
	assert.True(t, result.Valid)
	assert.True(t, result.Exists)
	assert.True(t, result.Accessible)
}

// TestParseImageReference tests the image reference parsing logic.
func TestParseImageReference(t *testing.T) {
	testCases := []struct {
		name        string
		image       string
		registry    string
		repository  string
		tag         string
		isDockerHub bool
	}{
		{
			name:        "simple name",
			image:       "nginx",
			registry:    "registry-1.docker.io",
			repository:  "library/nginx",
			tag:         "latest",
			isDockerHub: true,
		},
		{
			name:        "simple name with tag",
			image:       "nginx:1.21",
			registry:    "registry-1.docker.io",
			repository:  "library/nginx",
			tag:         "1.21",
			isDockerHub: true,
		},
		{
			name:        "user namespace",
			image:       "wavespeed/model",
			registry:    "registry-1.docker.io",
			repository:  "wavespeed/model",
			tag:         "latest",
			isDockerHub: true,
		},
		{
			name:        "gcr.io",
			image:       "gcr.io/project/image:v1",
			registry:    "gcr.io",
			repository:  "project/image",
			tag:         "v1",
			isDockerHub: false,
		},
		{
			name:        "private registry",
			image:       "registry.example.com/myimage:latest",
			registry:    "registry.example.com",
			repository:  "myimage",
			tag:         "latest",
			isDockerHub: false,
		},
		{
			name:        "private registry with port",
			image:       "registry.example.com:5000/myimage:v1",
			registry:    "registry.example.com:5000",
			repository:  "myimage",
			tag:         "v1",
			isDockerHub: false,
		},
		{
			name:        "aws ecr",
			image:       "123456789.dkr.ecr.us-east-1.amazonaws.com/myrepo:latest",
			registry:    "123456789.dkr.ecr.us-east-1.amazonaws.com",
			repository:  "myrepo",
			tag:         "latest",
			isDockerHub: false,
		},
		{
			name:        "docker.io explicit",
			image:       "docker.io/library/nginx:latest",
			registry:    "registry-1.docker.io",
			repository:  "library/nginx",
			tag:         "latest",
			isDockerHub: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ref, err := parseImageReference(tc.image)
			require.NoError(t, err)
			assert.Equal(t, tc.registry, ref.Registry)
			assert.Equal(t, tc.repository, ref.Repository)
			assert.Equal(t, tc.tag, ref.Tag)
			assert.Equal(t, tc.isDockerHub, ref.IsDockerHub)
		})
	}
}

// TestParseWWWAuthenticate tests the WWW-Authenticate header parsing.
func TestParseWWWAuthenticate(t *testing.T) {
	testCases := []struct {
		name    string
		header  string
		realm   string
		service string
		scope   string
		wantErr bool
	}{
		{
			name:    "docker hub format",
			header:  `Bearer realm="https://auth.docker.io/token",service="registry.docker.io",scope="repository:library/nginx:pull"`,
			realm:   "https://auth.docker.io/token",
			service: "registry.docker.io",
			scope:   "repository:library/nginx:pull",
			wantErr: false,
		},
		{
			name:    "gcr format",
			header:  `Bearer realm="https://gcr.io/v2/token",service="gcr.io"`,
			realm:   "https://gcr.io/v2/token",
			service: "gcr.io",
			scope:   "",
			wantErr: false,
		},
		{
			name:    "lowercase bearer",
			header:  `bearer realm="https://auth.example.com/token"`,
			realm:   "https://auth.example.com/token",
			service: "",
			scope:   "",
			wantErr: false,
		},
		{
			name:    "basic auth not supported",
			header:  `Basic realm="Registry"`,
			wantErr: true,
		},
		{
			name:    "missing realm",
			header:  `Bearer service="registry.docker.io"`,
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			info, err := parseWWWAuthenticate(tc.header)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.realm, info.Realm)
			assert.Equal(t, tc.service, info.Service)
			assert.Equal(t, tc.scope, info.Scope)
		})
	}
}

// TestBuildManifestURL tests the manifest URL building logic.
func TestBuildManifestURL(t *testing.T) {
	testCases := []struct {
		name     string
		ref      *imageReference
		expected string
	}{
		{
			name: "docker hub",
			ref: &imageReference{
				Registry:   "registry-1.docker.io",
				Repository: "library/nginx",
				Tag:        "latest",
			},
			expected: "https://registry-1.docker.io/v2/library/nginx/manifests/latest",
		},
		{
			name: "with digest",
			ref: &imageReference{
				Registry:   "registry-1.docker.io",
				Repository: "library/nginx",
				Tag:        "latest",
				Digest:     "sha256:abc123",
			},
			expected: "https://registry-1.docker.io/v2/library/nginx/manifests/sha256:abc123",
		},
		{
			name: "private registry",
			ref: &imageReference{
				Registry:   "registry.example.com:5000",
				Repository: "myimage",
				Tag:        "v1.0",
			},
			expected: "https://registry.example.com:5000/v2/myimage/manifests/v1.0",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			url := buildManifestURL(tc.ref)
			assert.Equal(t, tc.expected, url)
		})
	}
}

// TestIsRegistry tests the registry detection logic.
func TestIsRegistry(t *testing.T) {
	testCases := []struct {
		input    string
		expected bool
	}{
		{"gcr.io", true},
		{"registry.example.com", true},
		{"localhost", true},
		{"localhost:5000", true},
		{"192.168.1.1", true},
		{"192.168.1.1:5000", true},
		{"wavespeed", false},
		{"library", false},
		{"nginx", false},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result := isRegistry(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// TestCheckImageExists_MockServer tests CheckImageExists with a mock HTTP server.
// This tests the actual HTTP flow without hitting real registries.
func TestCheckImageExists_MockServer(t *testing.T) {
	t.Run("image exists - 200 OK", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify request
			assert.Contains(t, r.URL.Path, "/v2/")
			assert.Contains(t, r.URL.Path, "/manifests/")
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		validator := NewImageValidator(nil)
		ctx := context.Background()

		// Parse server URL to get host:port
		serverURL := strings.TrimPrefix(server.URL, "http://")
		image := serverURL + "/myimage:latest"

		result, err := validator.CheckImageExists(ctx, image, nil)
		require.NoError(t, err)
		assert.True(t, result.Valid)
		assert.True(t, result.Exists)
		assert.True(t, result.Accessible)
	})

	t.Run("image not found - 404", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		validator := NewImageValidator(nil)
		ctx := context.Background()

		serverURL := strings.TrimPrefix(server.URL, "http://")
		image := serverURL + "/nonexistent:latest"

		result, err := validator.CheckImageExists(ctx, image, nil)
		require.NoError(t, err)
		assert.True(t, result.Valid)
		assert.False(t, result.Exists)
		assert.Contains(t, result.Error, "Image not found")
	})

	t.Run("forbidden - 403", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))
		defer server.Close()

		validator := NewImageValidator(nil)
		ctx := context.Background()

		serverURL := strings.TrimPrefix(server.URL, "http://")
		image := serverURL + "/private:latest"

		result, err := validator.CheckImageExists(ctx, image, nil)
		require.NoError(t, err)
		assert.True(t, result.Valid)
		assert.True(t, result.Exists)
		assert.False(t, result.Accessible)
		assert.Contains(t, result.Error, "Access denied")
	})

	t.Run("unauthorized without credentials - 401", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			// No WWW-Authenticate header
		}))
		defer server.Close()

		validator := NewImageValidator(nil)
		ctx := context.Background()

		serverURL := strings.TrimPrefix(server.URL, "http://")
		image := serverURL + "/private:latest"

		result, err := validator.CheckImageExists(ctx, image, nil)
		require.NoError(t, err)
		assert.True(t, result.Valid)
		assert.True(t, result.Exists)
		assert.False(t, result.Accessible)
		assert.Contains(t, result.Error, "authentication")
	})

	t.Run("unauthorized with auth flow - success", func(t *testing.T) {
		tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Token endpoint
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"token": "test-token-123"}`))
		}))
		defer tokenServer.Close()

		requestCount := 0
		registryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCount++
			if requestCount == 1 {
				// First request - return 401 with WWW-Authenticate
				w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s",service="test-registry"`, tokenServer.URL))
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			// Second request - verify token and return success
			auth := r.Header.Get("Authorization")
			if auth == "Bearer test-token-123" {
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer registryServer.Close()

		validator := NewImageValidator(nil)
		ctx := context.Background()

		serverURL := strings.TrimPrefix(registryServer.URL, "http://")
		image := serverURL + "/myimage:latest"

		result, err := validator.CheckImageExists(ctx, image, nil)
		require.NoError(t, err)
		assert.True(t, result.Valid)
		assert.True(t, result.Exists)
		assert.True(t, result.Accessible)
	})

	t.Run("unauthorized with credentials - auth failure", func(t *testing.T) {
		tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Token endpoint - return error for bad credentials
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer tokenServer.Close()

		registryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s",service="test-registry"`, tokenServer.URL))
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer registryServer.Close()

		validator := NewImageValidator(nil)
		ctx := context.Background()

		serverURL := strings.TrimPrefix(registryServer.URL, "http://")
		image := serverURL + "/myimage:latest"

		cred := &interfaces.RegistryCredential{
			Username: "baduser",
			Password: "badpass",
		}

		result, err := validator.CheckImageExists(ctx, image, cred)
		require.NoError(t, err)
		assert.True(t, result.Valid)
		assert.True(t, result.Exists)
		assert.False(t, result.Accessible)
		assert.Contains(t, result.Error, "Authentication failed")
	})
}

// TestCheckImageExists_Timeout tests timeout handling.
func TestCheckImageExists_Timeout(t *testing.T) {
	t.Run("timeout with skip enabled", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Simulate slow response
			time.Sleep(200 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		config := &ImageValidationConfig{
			Enabled:       true,
			Timeout:       50 * time.Millisecond, // Very short timeout
			CacheDuration: time.Hour,
			SkipOnTimeout: true,
		}
		validator := NewImageValidator(config)
		ctx := context.Background()

		serverURL := strings.TrimPrefix(server.URL, "http://")
		image := serverURL + "/myimage:latest"

		result, err := validator.CheckImageExists(ctx, image, nil)
		require.NoError(t, err)
		assert.True(t, result.Valid)
		// Should have warning about timeout or connection error
		assert.True(t, result.Warning != "" || result.Error != "")
	})
}
