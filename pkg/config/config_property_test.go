// Package config provides property-based tests for configuration fallback functionality.
// These tests verify universal properties that should hold across all valid inputs.
//
// Feature: image-validation-and-status
// Property 9: Configuration fallback to defaults
// **Validates: Requirements 8.5**
package config

import (
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// ============================================================================
// Property-based tests for Property 9: Configuration Fallback to Defaults
// ============================================================================

// TestProperty_InvalidTimeoutFallsBackToDefault tests that invalid timeout values fall back to defaults
//
// Property: For any invalid configuration value (negative timeout, invalid duration format, etc.),
// the system SHALL use the default value and log a warning, ensuring the system remains operational.
//
// Feature: image-validation-and-status, Property 9: Configuration fallback to defaults
// **Validates: Requirements 8.5**
func TestProperty_InvalidTimeoutFallsBackToDefault(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 50

	properties := gopter.NewProperties(parameters)
	defaults := DefaultImageValidationConfig()

	// Property 9a: Negative timeout values fall back to default
	properties.Property("negative timeout values fall back to default", prop.ForAll(
		func(negativeSeconds int) bool {
			// Create config with negative timeout
			cfg := &Config{
				ImageValidation: ImageValidationConfig{
					Timeout:       time.Duration(negativeSeconds) * time.Second,
					CacheDuration: defaults.CacheDuration, // Valid value
				},
			}

			// Apply validation
			validateAndApplyDefaults(cfg)

			// Verify: timeout should be the default value
			return cfg.ImageValidation.Timeout == defaults.Timeout
		},
		gen.IntRange(-1000, -1), // Negative values
	))

	// Property 9b: Zero timeout values fall back to default
	properties.Property("zero timeout values fall back to default", prop.ForAll(
		func(_ int) bool {
			// Create config with zero timeout
			cfg := &Config{
				ImageValidation: ImageValidationConfig{
					Timeout:       0,
					CacheDuration: defaults.CacheDuration, // Valid value
				},
			}

			// Apply validation
			validateAndApplyDefaults(cfg)

			// Verify: timeout should be the default value
			return cfg.ImageValidation.Timeout == defaults.Timeout
		},
		gen.Const(0),
	))

	properties.TestingRun(t)
}

// TestProperty_InvalidCacheDurationFallsBackToDefault tests that invalid cache duration values fall back to defaults
//
// Property: For any invalid cache duration value (negative or zero), the system SHALL use
// the default value.
//
// Feature: image-validation-and-status, Property 9: Configuration fallback to defaults
// **Validates: Requirements 8.5**
func TestProperty_InvalidCacheDurationFallsBackToDefault(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 50

	properties := gopter.NewProperties(parameters)
	defaults := DefaultImageValidationConfig()

	// Property 9c: Negative cache duration values fall back to default
	properties.Property("negative cache duration values fall back to default", prop.ForAll(
		func(negativeSeconds int) bool {
			// Create config with negative cache duration
			cfg := &Config{
				ImageValidation: ImageValidationConfig{
					Timeout:       defaults.Timeout, // Valid value
					CacheDuration: time.Duration(negativeSeconds) * time.Second,
				},
			}

			// Apply validation
			validateAndApplyDefaults(cfg)

			// Verify: cache duration should be the default value
			return cfg.ImageValidation.CacheDuration == defaults.CacheDuration
		},
		gen.IntRange(-1000, -1), // Negative values
	))

	// Property 9d: Zero cache duration values fall back to default
	properties.Property("zero cache duration values fall back to default", prop.ForAll(
		func(_ int) bool {
			// Create config with zero cache duration
			cfg := &Config{
				ImageValidation: ImageValidationConfig{
					Timeout:       defaults.Timeout, // Valid value
					CacheDuration: 0,
				},
			}

			// Apply validation
			validateAndApplyDefaults(cfg)

			// Verify: cache duration should be the default value
			return cfg.ImageValidation.CacheDuration == defaults.CacheDuration
		},
		gen.Const(0),
	))

	properties.TestingRun(t)
}

// TestProperty_InvalidImagePullTimeoutFallsBackToDefault tests that invalid image pull timeout values fall back to defaults
//
// Property: For any invalid image pull timeout value (negative or zero), the system SHALL use
// the default value.
//
// Feature: image-validation-and-status, Property 9: Configuration fallback to defaults
// **Validates: Requirements 8.5**
func TestProperty_InvalidImagePullTimeoutFallsBackToDefault(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 50

	properties := gopter.NewProperties(parameters)
	defaults := DefaultResourceReleaserConfig()

	// Property 9e: Negative image pull timeout values fall back to default
	properties.Property("negative image pull timeout values fall back to default", prop.ForAll(
		func(negativeSeconds int) bool {
			// Create config with negative image pull timeout
			cfg := &Config{
				ResourceReleaser: ResourceReleaserConfig{
					ImagePullTimeout: time.Duration(negativeSeconds) * time.Second,
					CheckInterval:    defaults.CheckInterval, // Valid value
					MaxRetries:       defaults.MaxRetries,    // Valid value
				},
			}

			// Apply validation
			validateAndApplyDefaults(cfg)

			// Verify: image pull timeout should be the default value
			return cfg.ResourceReleaser.ImagePullTimeout == defaults.ImagePullTimeout
		},
		gen.IntRange(-1000, -1), // Negative values
	))

	// Property 9f: Zero image pull timeout values fall back to default
	properties.Property("zero image pull timeout values fall back to default", prop.ForAll(
		func(_ int) bool {
			// Create config with zero image pull timeout
			cfg := &Config{
				ResourceReleaser: ResourceReleaserConfig{
					ImagePullTimeout: 0,
					CheckInterval:    defaults.CheckInterval, // Valid value
					MaxRetries:       defaults.MaxRetries,    // Valid value
				},
			}

			// Apply validation
			validateAndApplyDefaults(cfg)

			// Verify: image pull timeout should be the default value
			return cfg.ResourceReleaser.ImagePullTimeout == defaults.ImagePullTimeout
		},
		gen.Const(0),
	))

	properties.TestingRun(t)
}

// TestProperty_InvalidCheckIntervalFallsBackToDefault tests that invalid check interval values fall back to defaults
//
// Property: For any invalid check interval value (negative or zero), the system SHALL use
// the default value.
//
// Feature: image-validation-and-status, Property 9: Configuration fallback to defaults
// **Validates: Requirements 8.5**
func TestProperty_InvalidCheckIntervalFallsBackToDefault(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 50

	properties := gopter.NewProperties(parameters)
	defaults := DefaultResourceReleaserConfig()

	// Property 9g: Negative check interval values fall back to default
	properties.Property("negative check interval values fall back to default", prop.ForAll(
		func(negativeSeconds int) bool {
			// Create config with negative check interval
			cfg := &Config{
				ResourceReleaser: ResourceReleaserConfig{
					ImagePullTimeout: defaults.ImagePullTimeout, // Valid value
					CheckInterval:    time.Duration(negativeSeconds) * time.Second,
					MaxRetries:       defaults.MaxRetries, // Valid value
				},
			}

			// Apply validation
			validateAndApplyDefaults(cfg)

			// Verify: check interval should be the default value
			return cfg.ResourceReleaser.CheckInterval == defaults.CheckInterval
		},
		gen.IntRange(-1000, -1), // Negative values
	))

	// Property 9h: Zero check interval values fall back to default
	properties.Property("zero check interval values fall back to default", prop.ForAll(
		func(_ int) bool {
			// Create config with zero check interval
			cfg := &Config{
				ResourceReleaser: ResourceReleaserConfig{
					ImagePullTimeout: defaults.ImagePullTimeout, // Valid value
					CheckInterval:    0,
					MaxRetries:       defaults.MaxRetries, // Valid value
				},
			}

			// Apply validation
			validateAndApplyDefaults(cfg)

			// Verify: check interval should be the default value
			return cfg.ResourceReleaser.CheckInterval == defaults.CheckInterval
		},
		gen.Const(0),
	))

	properties.TestingRun(t)
}

// TestProperty_InvalidMaxRetriesFallsBackToDefault tests that invalid max retries values fall back to defaults
//
// Property: For any invalid max retries value (negative), the system SHALL use
// the default value.
//
// Feature: image-validation-and-status, Property 9: Configuration fallback to defaults
// **Validates: Requirements 8.5**
func TestProperty_InvalidMaxRetriesFallsBackToDefault(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 50

	properties := gopter.NewProperties(parameters)
	defaults := DefaultResourceReleaserConfig()

	// Property 9i: Negative max retries values fall back to default
	properties.Property("negative max retries values fall back to default", prop.ForAll(
		func(negativeRetries int) bool {
			// Create config with negative max retries
			cfg := &Config{
				ResourceReleaser: ResourceReleaserConfig{
					ImagePullTimeout: defaults.ImagePullTimeout, // Valid value
					CheckInterval:    defaults.CheckInterval,    // Valid value
					MaxRetries:       negativeRetries,
				},
			}

			// Apply validation
			validateAndApplyDefaults(cfg)

			// Verify: max retries should be the default value
			return cfg.ResourceReleaser.MaxRetries == defaults.MaxRetries
		},
		gen.IntRange(-1000, -1), // Negative values
	))

	// Property 9j: Zero max retries is valid (not negative)
	properties.Property("zero max retries is valid and preserved", prop.ForAll(
		func(_ int) bool {
			// Create config with zero max retries (which is valid - means no retries)
			cfg := &Config{
				ResourceReleaser: ResourceReleaserConfig{
					ImagePullTimeout: defaults.ImagePullTimeout, // Valid value
					CheckInterval:    defaults.CheckInterval,    // Valid value
					MaxRetries:       0,
				},
			}

			// Apply validation
			validateAndApplyDefaults(cfg)

			// Verify: zero max retries should be preserved (it's valid)
			return cfg.ResourceReleaser.MaxRetries == 0
		},
		gen.Const(0),
	))

	properties.TestingRun(t)
}

// TestProperty_ValidValuesArePreserved tests that valid configuration values are not overwritten
//
// Property: For any valid configuration value, the system SHALL preserve the value
// and NOT overwrite it with the default.
//
// Feature: image-validation-and-status, Property 9: Configuration fallback to defaults
// **Validates: Requirements 8.5**
func TestProperty_ValidValuesArePreserved(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 50

	properties := gopter.NewProperties(parameters)
	defaults := DefaultImageValidationConfig()
	releaserDefaults := DefaultResourceReleaserConfig()

	// Property 9k: Valid timeout values are preserved
	properties.Property("valid timeout values are preserved", prop.ForAll(
		func(positiveSeconds int) bool {
			timeout := time.Duration(positiveSeconds) * time.Second

			// Create config with valid timeout
			cfg := &Config{
				ImageValidation: ImageValidationConfig{
					Timeout:       timeout,
					CacheDuration: defaults.CacheDuration, // Valid value
				},
			}

			// Apply validation
			validateAndApplyDefaults(cfg)

			// Verify: timeout should be preserved (not changed to default)
			return cfg.ImageValidation.Timeout == timeout
		},
		gen.IntRange(1, 300), // Positive values (1-300 seconds)
	))

	// Property 9l: Valid cache duration values are preserved
	properties.Property("valid cache duration values are preserved", prop.ForAll(
		func(positiveSeconds int) bool {
			cacheDuration := time.Duration(positiveSeconds) * time.Second

			// Create config with valid cache duration
			cfg := &Config{
				ImageValidation: ImageValidationConfig{
					Timeout:       defaults.Timeout, // Valid value
					CacheDuration: cacheDuration,
				},
			}

			// Apply validation
			validateAndApplyDefaults(cfg)

			// Verify: cache duration should be preserved (not changed to default)
			return cfg.ImageValidation.CacheDuration == cacheDuration
		},
		gen.IntRange(1, 7200), // Positive values (1-7200 seconds = 2 hours)
	))

	// Property 9m: Valid image pull timeout values are preserved
	properties.Property("valid image pull timeout values are preserved", prop.ForAll(
		func(positiveSeconds int) bool {
			imagePullTimeout := time.Duration(positiveSeconds) * time.Second

			// Create config with valid image pull timeout
			cfg := &Config{
				ResourceReleaser: ResourceReleaserConfig{
					ImagePullTimeout: imagePullTimeout,
					CheckInterval:    releaserDefaults.CheckInterval, // Valid value
					MaxRetries:       releaserDefaults.MaxRetries,    // Valid value
				},
			}

			// Apply validation
			validateAndApplyDefaults(cfg)

			// Verify: image pull timeout should be preserved (not changed to default)
			return cfg.ResourceReleaser.ImagePullTimeout == imagePullTimeout
		},
		gen.IntRange(1, 600), // Positive values (1-600 seconds = 10 minutes)
	))

	// Property 9n: Valid check interval values are preserved
	properties.Property("valid check interval values are preserved", prop.ForAll(
		func(positiveSeconds int) bool {
			checkInterval := time.Duration(positiveSeconds) * time.Second

			// Create config with valid check interval
			cfg := &Config{
				ResourceReleaser: ResourceReleaserConfig{
					ImagePullTimeout: releaserDefaults.ImagePullTimeout, // Valid value
					CheckInterval:    checkInterval,
					MaxRetries:       releaserDefaults.MaxRetries, // Valid value
				},
			}

			// Apply validation
			validateAndApplyDefaults(cfg)

			// Verify: check interval should be preserved (not changed to default)
			return cfg.ResourceReleaser.CheckInterval == checkInterval
		},
		gen.IntRange(1, 120), // Positive values (1-120 seconds = 2 minutes)
	))

	// Property 9o: Valid max retries values are preserved
	properties.Property("valid max retries values are preserved", prop.ForAll(
		func(positiveRetries int) bool {
			// Create config with valid max retries
			cfg := &Config{
				ResourceReleaser: ResourceReleaserConfig{
					ImagePullTimeout: releaserDefaults.ImagePullTimeout, // Valid value
					CheckInterval:    releaserDefaults.CheckInterval,    // Valid value
					MaxRetries:       positiveRetries,
				},
			}

			// Apply validation
			validateAndApplyDefaults(cfg)

			// Verify: max retries should be preserved (not changed to default)
			return cfg.ResourceReleaser.MaxRetries == positiveRetries
		},
		gen.IntRange(0, 10), // Non-negative values (0-10)
	))

	properties.TestingRun(t)
}

// TestProperty_ValidationIsDeterministic tests that validation is deterministic
//
// Property: For any configuration input, applying validateAndApplyDefaults multiple times
// SHALL produce the same result.
//
// Feature: image-validation-and-status, Property 9: Configuration fallback to defaults
// **Validates: Requirements 8.5**
func TestProperty_ValidationIsDeterministic(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 50

	properties := gopter.NewProperties(parameters)

	// Property 9p: Validation is deterministic
	properties.Property("validation is deterministic", prop.ForAll(
		func(timeoutSec, cacheSec, pullTimeoutSec, checkIntervalSec, maxRetries int) bool {
			// Create two identical configs
			cfg1 := &Config{
				ImageValidation: ImageValidationConfig{
					Timeout:       time.Duration(timeoutSec) * time.Second,
					CacheDuration: time.Duration(cacheSec) * time.Second,
				},
				ResourceReleaser: ResourceReleaserConfig{
					ImagePullTimeout: time.Duration(pullTimeoutSec) * time.Second,
					CheckInterval:    time.Duration(checkIntervalSec) * time.Second,
					MaxRetries:       maxRetries,
				},
			}

			cfg2 := &Config{
				ImageValidation: ImageValidationConfig{
					Timeout:       time.Duration(timeoutSec) * time.Second,
					CacheDuration: time.Duration(cacheSec) * time.Second,
				},
				ResourceReleaser: ResourceReleaserConfig{
					ImagePullTimeout: time.Duration(pullTimeoutSec) * time.Second,
					CheckInterval:    time.Duration(checkIntervalSec) * time.Second,
					MaxRetries:       maxRetries,
				},
			}

			// Apply validation to both
			validateAndApplyDefaults(cfg1)
			validateAndApplyDefaults(cfg2)

			// Verify: both configs should have the same values
			return cfg1.ImageValidation.Timeout == cfg2.ImageValidation.Timeout &&
				cfg1.ImageValidation.CacheDuration == cfg2.ImageValidation.CacheDuration &&
				cfg1.ResourceReleaser.ImagePullTimeout == cfg2.ResourceReleaser.ImagePullTimeout &&
				cfg1.ResourceReleaser.CheckInterval == cfg2.ResourceReleaser.CheckInterval &&
				cfg1.ResourceReleaser.MaxRetries == cfg2.ResourceReleaser.MaxRetries
		},
		gen.IntRange(-100, 100), // timeout seconds
		gen.IntRange(-100, 100), // cache seconds
		gen.IntRange(-100, 100), // pull timeout seconds
		gen.IntRange(-100, 100), // check interval seconds
		gen.IntRange(-10, 10),   // max retries
	))

	properties.TestingRun(t)
}

// TestProperty_ValidationIsIdempotent tests that validation is idempotent
//
// Property: Applying validateAndApplyDefaults twice to the same config SHALL produce
// the same result as applying it once.
//
// Feature: image-validation-and-status, Property 9: Configuration fallback to defaults
// **Validates: Requirements 8.5**
func TestProperty_ValidationIsIdempotent(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 50

	properties := gopter.NewProperties(parameters)

	// Property 9q: Validation is idempotent
	properties.Property("validation is idempotent", prop.ForAll(
		func(timeoutSec, cacheSec, pullTimeoutSec, checkIntervalSec, maxRetries int) bool {
			// Create config
			cfg := &Config{
				ImageValidation: ImageValidationConfig{
					Timeout:       time.Duration(timeoutSec) * time.Second,
					CacheDuration: time.Duration(cacheSec) * time.Second,
				},
				ResourceReleaser: ResourceReleaserConfig{
					ImagePullTimeout: time.Duration(pullTimeoutSec) * time.Second,
					CheckInterval:    time.Duration(checkIntervalSec) * time.Second,
					MaxRetries:       maxRetries,
				},
			}

			// Apply validation once
			validateAndApplyDefaults(cfg)

			// Record values after first application
			timeout1 := cfg.ImageValidation.Timeout
			cache1 := cfg.ImageValidation.CacheDuration
			pullTimeout1 := cfg.ResourceReleaser.ImagePullTimeout
			checkInterval1 := cfg.ResourceReleaser.CheckInterval
			maxRetries1 := cfg.ResourceReleaser.MaxRetries

			// Apply validation again
			validateAndApplyDefaults(cfg)

			// Verify: values should be the same after second application
			return cfg.ImageValidation.Timeout == timeout1 &&
				cfg.ImageValidation.CacheDuration == cache1 &&
				cfg.ResourceReleaser.ImagePullTimeout == pullTimeout1 &&
				cfg.ResourceReleaser.CheckInterval == checkInterval1 &&
				cfg.ResourceReleaser.MaxRetries == maxRetries1
		},
		gen.IntRange(-100, 100), // timeout seconds
		gen.IntRange(-100, 100), // cache seconds
		gen.IntRange(-100, 100), // pull timeout seconds
		gen.IntRange(-100, 100), // check interval seconds
		gen.IntRange(-10, 10),   // max retries
	))

	properties.TestingRun(t)
}

// TestProperty_AllInvalidValuesFallBackToDefaults tests that all invalid values in a config fall back to defaults
//
// Property: When all configuration values are invalid, the system SHALL use all default values.
//
// Feature: image-validation-and-status, Property 9: Configuration fallback to defaults
// **Validates: Requirements 8.5**
func TestProperty_AllInvalidValuesFallBackToDefaults(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 50

	properties := gopter.NewProperties(parameters)
	defaults := DefaultImageValidationConfig()
	releaserDefaults := DefaultResourceReleaserConfig()

	// Property 9r: All invalid values fall back to defaults
	properties.Property("all invalid values fall back to defaults", prop.ForAll(
		func(negTimeout, negCache, negPullTimeout, negCheckInterval, negMaxRetries int) bool {
			// Create config with all invalid values
			cfg := &Config{
				ImageValidation: ImageValidationConfig{
					Timeout:       time.Duration(negTimeout) * time.Second,
					CacheDuration: time.Duration(negCache) * time.Second,
				},
				ResourceReleaser: ResourceReleaserConfig{
					ImagePullTimeout: time.Duration(negPullTimeout) * time.Second,
					CheckInterval:    time.Duration(negCheckInterval) * time.Second,
					MaxRetries:       negMaxRetries,
				},
			}

			// Apply validation
			validateAndApplyDefaults(cfg)

			// Verify: all values should be defaults
			return cfg.ImageValidation.Timeout == defaults.Timeout &&
				cfg.ImageValidation.CacheDuration == defaults.CacheDuration &&
				cfg.ResourceReleaser.ImagePullTimeout == releaserDefaults.ImagePullTimeout &&
				cfg.ResourceReleaser.CheckInterval == releaserDefaults.CheckInterval &&
				cfg.ResourceReleaser.MaxRetries == releaserDefaults.MaxRetries
		},
		gen.IntRange(-1000, -1), // negative timeout
		gen.IntRange(-1000, -1), // negative cache
		gen.IntRange(-1000, -1), // negative pull timeout
		gen.IntRange(-1000, -1), // negative check interval
		gen.IntRange(-1000, -1), // negative max retries
	))

	properties.TestingRun(t)
}

// TestProperty_MixedValidInvalidValues tests that mixed valid/invalid values are handled correctly
//
// Property: When some configuration values are valid and some are invalid, the system SHALL
// preserve valid values and use defaults for invalid values.
//
// Feature: image-validation-and-status, Property 9: Configuration fallback to defaults
// **Validates: Requirements 8.5**
func TestProperty_MixedValidInvalidValues(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 50

	properties := gopter.NewProperties(parameters)
	defaults := DefaultImageValidationConfig()
	releaserDefaults := DefaultResourceReleaserConfig()

	// Property 9s: Valid timeout with invalid cache duration
	properties.Property("valid timeout with invalid cache duration", prop.ForAll(
		func(validTimeout, invalidCache int) bool {
			timeout := time.Duration(validTimeout) * time.Second
			cacheDuration := time.Duration(invalidCache) * time.Second

			cfg := &Config{
				ImageValidation: ImageValidationConfig{
					Timeout:       timeout,
					CacheDuration: cacheDuration,
				},
			}

			validateAndApplyDefaults(cfg)

			// Verify: timeout preserved, cache duration is default
			return cfg.ImageValidation.Timeout == timeout &&
				cfg.ImageValidation.CacheDuration == defaults.CacheDuration
		},
		gen.IntRange(1, 300),   // valid timeout
		gen.IntRange(-1000, 0), // invalid cache duration
	))

	// Property 9t: Invalid timeout with valid cache duration
	properties.Property("invalid timeout with valid cache duration", prop.ForAll(
		func(invalidTimeout, validCache int) bool {
			timeout := time.Duration(invalidTimeout) * time.Second
			cacheDuration := time.Duration(validCache) * time.Second

			cfg := &Config{
				ImageValidation: ImageValidationConfig{
					Timeout:       timeout,
					CacheDuration: cacheDuration,
				},
			}

			validateAndApplyDefaults(cfg)

			// Verify: timeout is default, cache duration preserved
			return cfg.ImageValidation.Timeout == defaults.Timeout &&
				cfg.ImageValidation.CacheDuration == cacheDuration
		},
		gen.IntRange(-1000, 0), // invalid timeout
		gen.IntRange(1, 7200),  // valid cache duration
	))

	// Property 9u: Mixed valid/invalid ResourceReleaser values
	properties.Property("mixed valid invalid resource releaser values", prop.ForAll(
		func(validPullTimeout, invalidCheckInterval, validMaxRetries int) bool {
			pullTimeout := time.Duration(validPullTimeout) * time.Second
			checkInterval := time.Duration(invalidCheckInterval) * time.Second

			cfg := &Config{
				ResourceReleaser: ResourceReleaserConfig{
					ImagePullTimeout: pullTimeout,
					CheckInterval:    checkInterval,
					MaxRetries:       validMaxRetries,
				},
			}

			validateAndApplyDefaults(cfg)

			// Verify: pull timeout and max retries preserved, check interval is default
			return cfg.ResourceReleaser.ImagePullTimeout == pullTimeout &&
				cfg.ResourceReleaser.CheckInterval == releaserDefaults.CheckInterval &&
				cfg.ResourceReleaser.MaxRetries == validMaxRetries
		},
		gen.IntRange(1, 600),   // valid pull timeout
		gen.IntRange(-1000, 0), // invalid check interval
		gen.IntRange(0, 10),    // valid max retries
	))

	properties.TestingRun(t)
}

// TestProperty_DefaultFunctionsReturnValidValues tests that default functions return valid values
//
// Property: The DefaultImageValidationConfig and DefaultResourceReleaserConfig functions
// SHALL always return valid configuration values (positive durations, non-negative retries).
//
// Feature: image-validation-and-status, Property 9: Configuration fallback to defaults
// **Validates: Requirements 8.5**
func TestProperty_DefaultFunctionsReturnValidValues(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 50

	properties := gopter.NewProperties(parameters)

	// Property 9v: DefaultImageValidationConfig returns valid values
	properties.Property("DefaultImageValidationConfig returns valid values", prop.ForAll(
		func(_ int) bool {
			defaults := DefaultImageValidationConfig()

			// Verify: all values are valid
			return defaults.Timeout > 0 &&
				defaults.CacheDuration > 0
		},
		gen.Const(0),
	))

	// Property 9w: DefaultResourceReleaserConfig returns valid values
	properties.Property("DefaultResourceReleaserConfig returns valid values", prop.ForAll(
		func(_ int) bool {
			defaults := DefaultResourceReleaserConfig()

			// Verify: all values are valid
			return defaults.ImagePullTimeout > 0 &&
				defaults.CheckInterval > 0 &&
				defaults.MaxRetries >= 0
		},
		gen.Const(0),
	))

	properties.TestingRun(t)
}
