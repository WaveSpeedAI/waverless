package endpoint

import (
	"context"
	"fmt"

	"waverless/pkg/config"
	"waverless/pkg/image"
	"waverless/pkg/interfaces"
	"waverless/pkg/logger"
	"waverless/pkg/store/mysql"
)

// DeploymentManager wraps all runtime deployment operations.
type DeploymentManager struct {
	provider       interfaces.DeploymentProvider
	metadata       *MetadataManager
	endpointRepo   *mysql.EndpointRepository
	imageValidator *image.ImageValidator
	imageConfig    *config.ImageValidationConfig
}

// NewDeploymentManager creates a deployment manager.
func NewDeploymentManager(provider interfaces.DeploymentProvider, metadata *MetadataManager, endpointRepo *mysql.EndpointRepository) *DeploymentManager {
	// Get image validation config from global config
	var imgConfig *config.ImageValidationConfig
	if config.GlobalConfig != nil {
		imgConfig = &config.GlobalConfig.ImageValidation
	} else {
		// Use default config if global config is not available
		defaultConfig := config.DefaultImageValidationConfig()
		imgConfig = &defaultConfig
	}

	// Create image validator with config
	validatorConfig := &image.ImageValidationConfig{
		Enabled:       imgConfig.Enabled,
		Timeout:       imgConfig.Timeout,
		CacheDuration: imgConfig.CacheDuration,
		SkipOnTimeout: imgConfig.SkipOnTimeout,
	}

	return &DeploymentManager{
		provider:       provider,
		metadata:       metadata,
		endpointRepo:   endpointRepo,
		imageValidator: image.NewImageValidator(validatorConfig),
		imageConfig:    imgConfig,
	}
}

// Deploy provisions runtime resources and persists metadata on success.
// Before deploying, it validates the image format and optionally checks if the image exists.
//
// Image validation flow:
// 1. Always validate image format using ValidateImageFormat
// 2. If format is invalid, return error immediately
// 3. If image validation is enabled in config, check image existence using CheckImageExists
// 4. If image doesn't exist, return error with suggestion
// 5. If validation times out and SkipOnTimeout=true, log warning and proceed
// 6. If validation times out and SkipOnTimeout=false, return error
//
// Validates: Requirements 1.1, 2.1, 2.2
func (m *DeploymentManager) Deploy(ctx context.Context, req *interfaces.DeployRequest, metadata *interfaces.EndpointMetadata) (*interfaces.DeployResponse, error) {
	if m.provider == nil {
		return nil, fmt.Errorf("deployment provider not configured")
	}
	if req == nil {
		return nil, fmt.Errorf("deploy request is nil")
	}

	// Step 1: Always validate image format (Requirements 1.1, 1.2)
	if m.imageValidator != nil && req.Image != "" {
		if err := m.imageValidator.ValidateImageFormat(req.Image); err != nil {
			logger.WarnCtx(ctx, "Image format validation failed for endpoint %s: %v", req.Endpoint, err)
			return nil, fmt.Errorf("image format validation failed: %w", err)
		}
		logger.InfoCtx(ctx, "Image format validation passed for endpoint %s, image: %s", req.Endpoint, req.Image)
	}

	// Step 2: Check image existence if validation is enabled (Requirements 2.1, 2.2, 2.3)
	// Use request-level validateImage if provided, otherwise use config-level setting
	shouldValidateImage := m.imageConfig != nil && m.imageConfig.Enabled
	if req.ValidateImage != nil {
		shouldValidateImage = *req.ValidateImage
	}

	logger.InfoCtx(ctx, "Image validation config: validator=%v, config=%v, configEnabled=%v, requestValidateImage=%v, shouldValidate=%v, image=%s",
		m.imageValidator != nil, m.imageConfig != nil, m.imageConfig != nil && m.imageConfig.Enabled,
		req.ValidateImage, shouldValidateImage, req.Image)

	if m.imageValidator != nil && shouldValidateImage && req.Image != "" {
		logger.InfoCtx(ctx, "Checking image existence for endpoint %s, image: %s", req.Endpoint, req.Image)

		// Convert registry credential if provided
		var cred *interfaces.RegistryCredential
		if req.RegistryCredential != nil {
			cred = &interfaces.RegistryCredential{
				Registry: req.RegistryCredential.Registry,
				Username: req.RegistryCredential.Username,
				Password: req.RegistryCredential.Password,
			}
			logger.InfoCtx(ctx, "Using registry credential for endpoint %s, registry: %s, username: %s",
				req.Endpoint, cred.Registry, cred.Username)
		} else {
			logger.InfoCtx(ctx, "No registry credential provided for endpoint %s", req.Endpoint)
		}

		result, err := m.imageValidator.CheckImageExists(ctx, req.Image, cred)
		logger.InfoCtx(ctx, "Image validation result for endpoint %s: valid=%v, exists=%v, accessible=%v, error=%s, warning=%s",
			req.Endpoint, result != nil && result.Valid, result != nil && result.Exists, result != nil && result.Accessible,
			func() string {
				if result != nil {
					return result.Error
				} else {
					return ""
				}
			}(),
			func() string {
				if result != nil {
					return result.Warning
				} else {
					return ""
				}
			}())

		if err != nil {
			// Unexpected error during validation
			logger.ErrorCtx(ctx, "Image existence check failed for endpoint %s: %v", req.Endpoint, err)
			if !m.imageConfig.SkipOnTimeout {
				return nil, fmt.Errorf("image validation failed: %w", err)
			}
			// SkipOnTimeout is true, proceed with warning
			logger.WarnCtx(ctx, "Image validation error for endpoint %s, proceeding with warning: %v", req.Endpoint, err)
		} else if result != nil {
			// Handle validation result
			if !result.Valid {
				// Format is invalid (should not happen as we validated above, but handle anyway)
				logger.WarnCtx(ctx, "Image validation returned invalid for endpoint %s: %s", req.Endpoint, result.Error)
				return nil, fmt.Errorf("image validation failed: %s", result.Error)
			}

			if result.Error != "" && !result.Exists {
				// Image does not exist
				logger.WarnCtx(ctx, "Image does not exist for endpoint %s: %s", req.Endpoint, result.Error)
				return nil, fmt.Errorf("image not found or inaccessible: %s. Please check the image name or verify you have access permissions.", result.Error)
			}

			if result.Error != "" && result.Exists && !result.Accessible {
				// Image exists but not accessible (auth issue)
				logger.WarnCtx(ctx, "Image not accessible for endpoint %s: %s", req.Endpoint, result.Error)
				return nil, fmt.Errorf("image not accessible: %s. Please check your registry credentials.", result.Error)
			}

			if result.Warning != "" {
				// Validation completed with warning (e.g., timeout with SkipOnTimeout=true)
				logger.WarnCtx(ctx, "Image validation warning for endpoint %s: %s", req.Endpoint, result.Warning)
				// Proceed with deployment
			}

			if result.Exists && result.Accessible {
				logger.InfoCtx(ctx, "Image existence check passed for endpoint %s, image: %s", req.Endpoint, req.Image)
			}
		}
	} else {
		logger.InfoCtx(ctx, "Skipping image existence check for endpoint %s (validation disabled or no image)", req.Endpoint)
	}

	// Step 3: Proceed with deployment
	resp, err := m.provider.Deploy(ctx, req)
	if err != nil {
		return nil, err
	}

	if metadata != nil && m.metadata != nil {
		if metadata.Name == "" {
			metadata.Name = req.Endpoint
		}
		if metadata.SpecName == "" {
			metadata.SpecName = req.SpecName
		}
		if metadata.Image == "" {
			metadata.Image = req.Image
		}
		if metadata.Replicas == 0 {
			metadata.Replicas = req.Replicas
		}
		if metadata.TaskTimeout == 0 {
			metadata.TaskTimeout = req.TaskTimeout
		}
		if err := m.metadata.Save(ctx, metadata); err != nil {
			return resp, fmt.Errorf("deployment succeeded but failed to persist metadata: %w", err)
		}
	}

	return resp, nil
}

// Update orchestrates deployment updates and metadata synchronization.
// If the endpoint is UNHEALTHY due to image issues and the update is trying to scale up
// without changing the image, the update will be blocked.
// When the image is changed, the health status is reset to HEALTHY to allow new deployment.
// Validates: Requirements 5.5
func (m *DeploymentManager) Update(ctx context.Context, req *interfaces.UpdateDeploymentRequest) (*interfaces.DeployResponse, error) {
	if m.provider == nil {
		return nil, fmt.Errorf("deployment provider not configured")
	}
	if req == nil {
		return nil, fmt.Errorf("update request is nil")
	}

	// Check if this is a scale-up operation without image change
	// If so, check if endpoint is blocked due to image failure
	// Property 8: Failed Endpoint Prevents New Pods
	if m.endpointRepo != nil && req.Replicas != nil && req.Image == "" {
		// Get current endpoint to check if this is a scale-up
		currentEndpoint, err := m.endpointRepo.Get(ctx, req.Endpoint)
		if err == nil && currentEndpoint != nil {
			// Only check if we're scaling up (increasing replicas)
			if *req.Replicas > currentEndpoint.Replicas {
				blocked, reason, err := m.endpointRepo.IsBlockedDueToImageFailure(ctx, req.Endpoint)
				if err == nil && blocked {
					return nil, fmt.Errorf("%w: %s - please update the image configuration before scaling up",
						ErrEndpointBlockedDueToImageFailure, reason)
				}
			}
		}
	}

	// If image is being changed, reset health status to HEALTHY
	// This allows the endpoint to be redeployed with the new image
	if m.endpointRepo != nil && req.Image != "" {
		logger.InfoCtx(ctx, "Image changed for endpoint %s, resetting health status to HEALTHY", req.Endpoint)
		if err := m.endpointRepo.UpdateHealthStatus(ctx, req.Endpoint, "HEALTHY", ""); err != nil {
			logger.WarnCtx(ctx, "Failed to reset health status for endpoint %s: %v", req.Endpoint, err)
			// Don't fail the update, just log the warning
		}
	}

	resp, err := m.provider.UpdateDeployment(ctx, req)
	if err != nil {
		return nil, err
	}

	if m.metadata != nil {
		meta, err := m.metadata.Get(ctx, req.Endpoint)
		if err == nil && meta != nil {
			if req.SpecName != "" {
				meta.SpecName = req.SpecName
			}
			if req.Image != "" {
				meta.Image = req.Image
			}
			if req.Replicas != nil {
				meta.Replicas = *req.Replicas
				// Update status based on replicas
				if *req.Replicas == 0 {
					meta.Status = "Stopped"
				} else if meta.Status == "Stopped" || meta.Status == "Deploying" {
					meta.Status = "Pending"
				}
			}
			if req.TaskTimeout != nil {
				meta.TaskTimeout = *req.TaskTimeout
			}
			if req.EnablePtrace != nil {
				meta.EnablePtrace = *req.EnablePtrace
			}
			if req.Env != nil {
				meta.Env = *req.Env
			}
			if err := m.metadata.Save(ctx, meta); err != nil {
				return resp, fmt.Errorf("deployment updated but failed to persist metadata: %w", err)
			}
		}
	}

	return resp, nil
}

// Delete destroys runtime resources and metadata.
func (m *DeploymentManager) Delete(ctx context.Context, name string) error {
	if m.provider == nil {
		return fmt.Errorf("deployment provider not configured")
	}
	if err := m.provider.DeleteApp(ctx, name); err != nil {
		return err
	}
	if m.metadata != nil {
		if err := m.metadata.Delete(ctx, name); err != nil {
			return err
		}
	}
	return nil
}
