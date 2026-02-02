package config

import (
	"log"
	"os"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

var GlobalConfig *Config

// Config global configuration
type Config struct {
	Server           ServerConfig           `yaml:"server"`
	Redis            RedisConfig            `yaml:"redis"`
	MySQL            MySQLConfig            `yaml:"mysql"`
	Queue            QueueConfig            `yaml:"queue"`
	Worker           WorkerConfig           `yaml:"worker"`
	Logger           LoggerConfig           `yaml:"logger"`
	K8s              K8sConfig              `yaml:"k8s"`
	AutoScaler       AutoScalerConfig       `yaml:"autoscaler"`
	Docker           DockerConfig           `yaml:"docker"`              // Docker registry authentication
	Notification     NotificationConfig     `yaml:"notification"`        // Notification configuration
	Providers        *ProvidersConfig       `yaml:"providers,omitempty"` // Providers configuration (optional)
	Novita           NovitaConfig           `yaml:"novita"`              // Novita serverless configuration
	ImageValidation  ImageValidationConfig  `yaml:"imageValidation"`     // Image validation configuration
	ResourceReleaser ResourceReleaserConfig `yaml:"resourceReleaser"`    // Resource releaser configuration
}

// ImageValidationConfig contains configuration for image validation.
// Validates: Requirements 8.1, 8.3, 8.4, 8.5
type ImageValidationConfig struct {
	// Enabled indicates whether image validation is enabled
	// Environment variable: IMAGE_VALIDATION_ENABLED
	Enabled bool `yaml:"enabled"`

	// Timeout is the timeout for validation requests (default: 30s)
	// Environment variable: IMAGE_VALIDATION_TIMEOUT (in seconds)
	Timeout time.Duration `yaml:"timeout"`

	// CacheDuration is how long to cache validation results (default: 1h)
	// Environment variable: IMAGE_VALIDATION_CACHE_DURATION (in seconds)
	CacheDuration time.Duration `yaml:"cacheDuration"`

	// SkipOnTimeout indicates whether to proceed with a warning when validation times out (default: true)
	// Environment variable: IMAGE_VALIDATION_SKIP_ON_TIMEOUT
	SkipOnTimeout bool `yaml:"skipOnTimeout"`
}

// ResourceReleaserConfig contains configuration for the ResourceReleaser.
// Validates: Requirements 8.1, 8.2, 8.5
type ResourceReleaserConfig struct {
	// ImagePullTimeout is the maximum time to wait for image pull before terminating the worker.
	// Default: 5 minutes
	// Environment variable: RESOURCE_RELEASER_IMAGE_PULL_TIMEOUT (in seconds)
	ImagePullTimeout time.Duration `yaml:"imagePullTimeout"`

	// CheckInterval is the interval between checks for stuck workers.
	// Default: 30 seconds
	// Environment variable: RESOURCE_RELEASER_CHECK_INTERVAL (in seconds)
	CheckInterval time.Duration `yaml:"checkInterval"`

	// MaxRetries is the maximum number of termination retries before giving up.
	// Default: 3
	// Environment variable: RESOURCE_RELEASER_MAX_RETRIES
	MaxRetries int `yaml:"maxRetries"`
}

// DefaultImageValidationConfig returns the default configuration for image validation.
func DefaultImageValidationConfig() ImageValidationConfig {
	return ImageValidationConfig{
		Enabled:       true,
		Timeout:       30 * time.Second,
		CacheDuration: 1 * time.Hour,
		SkipOnTimeout: true,
	}
}

// DefaultResourceReleaserConfig returns the default configuration for ResourceReleaser.
func DefaultResourceReleaserConfig() ResourceReleaserConfig {
	return ResourceReleaserConfig{
		ImagePullTimeout: 5 * time.Minute,
		CheckInterval:    30 * time.Second,
		MaxRetries:       3,
	}
}

// ServerConfig server configuration
type ServerConfig struct {
	Port    int    `yaml:"port"`
	Mode    string `yaml:"mode"`     // debug, release
	APIKey  string `yaml:"api_key"`  // API key for worker authentication (optional, if empty, auth is disabled)
	BaseURL string `yaml:"base_url"` // Base URL for the server
}

// RedisConfig Redis configuration
type RedisConfig struct {
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

// MySQLConfig MySQL configuration
type MySQLConfig struct {
	Host     string       `yaml:"host"`
	Port     int          `yaml:"port"`
	User     string       `yaml:"user"`
	Password string       `yaml:"password"`
	Database string       `yaml:"database"`
	Proxy    *ProxyConfig `yaml:"proxy,omitempty"` // Proxy configuration (optional)
}

// ProxyConfig Proxy configuration for network connections
type ProxyConfig struct {
	Enabled bool   `yaml:"enabled"` // Enable proxy
	Type    string `yaml:"type"`    // Proxy type: http, https, socks5
	Host    string `yaml:"host"`    // Proxy host
	Port    int    `yaml:"port"`    // Proxy port
}

// QueueConfig queue configuration
type QueueConfig struct {
	Concurrency int `yaml:"concurrency"`  // queue processing concurrency
	MaxRetry    int `yaml:"max_retry"`    // maximum retry count
	TaskTimeout int `yaml:"task_timeout"` // task timeout (seconds)
	// Note: Task data is persisted permanently in Redis (no TTL)
}

// WorkerConfig Worker configuration
type WorkerConfig struct {
	HeartbeatInterval  int `yaml:"heartbeat_interval"`  // Heartbeat interval (seconds)
	HeartbeatTimeout   int `yaml:"heartbeat_timeout"`   // Heartbeat timeout (seconds)
	DefaultConcurrency int `yaml:"default_concurrency"` // default concurrency
}

// LoggerConfig logger configuration
type LoggerConfig struct {
	Level  string           `yaml:"level"`  // debug, info, warn, error
	Output string           `yaml:"output"` // console, file, both
	File   LoggerFileConfig `yaml:"file"`
}

// LoggerFileConfig logger file configuration
type LoggerFileConfig struct {
	Path       string `yaml:"path"`
	MaxSize    int    `yaml:"max_size"`    // MB per file (default: 100)
	MaxBackups int    `yaml:"max_backups"` // max backup files (default: 3)
	MaxAge     int    `yaml:"max_age"`     // days to keep (default: 7)
	Compress   bool   `yaml:"compress"`    // compress rotated files (default: false)
}

// K8sConfig K8s configuration
type K8sConfig struct {
	Enabled   bool              `yaml:"enabled"`              // whether to enable K8s features
	Namespace string            `yaml:"namespace"`            // K8s namespace
	Platform  string            `yaml:"platform"`             // Platform type: generic, aliyun-ack, aws-eks
	ConfigDir string            `yaml:"config_dir"`           // Configuration directory (specs.yaml and templates)
	GlobalEnv map[string]string `yaml:"global_env,omitempty"` // Global environment variables for all deployments
	AWS       *AWSConfig        `yaml:"aws,omitempty"`        // AWS configuration (for aws-eks platform)
}

// AWSConfig AWS configuration
type AWSConfig struct {
	Region          string `yaml:"region"`            // AWS region (optional, auto-detect if empty)
	AccessKeyID     string `yaml:"access_key_id"`     // AWS Access Key ID (optional, use IAM role if empty)
	SecretAccessKey string `yaml:"secret_access_key"` // AWS Secret Access Key (optional)
}

// ProvidersConfig providers configuration
type ProvidersConfig struct {
	Deployment string `yaml:"deployment"` // Deployment provider: k8s, docker, custom
	Queue      string `yaml:"queue"`      // Queue provider: redis, mysql, postgres
	Metadata   string `yaml:"metadata"`   // Metadata storage: redis, mysql, postgres
}

// AutoScalerConfig autoscaler configuration
type AutoScalerConfig struct {
	Enabled        bool `yaml:"enabled"`         // Whether to enable autoscaling
	Interval       int  `yaml:"interval"`        // Control loop interval (seconds)
	MaxGPUCount    int  `yaml:"max_gpu_count"`   // Total cluster GPU count
	MaxCPUCores    int  `yaml:"max_cpu_cores"`   // Total cluster CPU cores
	MaxMemoryGB    int  `yaml:"max_memory_gb"`   // Total cluster memory (GB)
	StarvationTime int  `yaml:"starvation_time"` // Starvation time threshold (seconds)
}

// DockerConfig Docker registry authentication configuration
type DockerConfig struct {
	ProxyURL   string                        `yaml:"proxy_url"`  // HTTP proxy URL (e.g., "http://127.0.0.1:7890")
	Registries map[string]DockerRegistryAuth `yaml:"registries"` // Registry authentication (key: registry URL)
}

// DockerRegistryAuth Docker registry authentication info
type DockerRegistryAuth struct {
	Username string `yaml:"username"` // Registry username
	Password string `yaml:"password"` // Registry password or token
	Auth     string `yaml:"auth"`     // Base64 encoded username:password (optional)
}

// NotificationConfig Notification configuration
type NotificationConfig struct {
	FeishuWebhookURL string `yaml:"feishu_webhook_url"` // Feishu (Lark) webhook URL
}

// NovitaConfig Novita serverless configuration
type NovitaConfig struct {
	Enabled      bool   `yaml:"enabled"`       // Whether to enable Novita provider
	APIKey       string `yaml:"api_key"`       // Novita API key (Bearer token)
	BaseURL      string `yaml:"base_url"`      // API base URL, default: https://api.novita.ai
	ConfigDir    string `yaml:"config_dir"`    // Configuration directory (specs.yaml and templates)
	PollInterval int    `yaml:"poll_interval"` // Poll interval for status updates (seconds, default: 10)
}

// Init initializes configuration
func Init() error {
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "config/config.yaml"
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return err
	}

	// Apply environment variable overrides and validate configuration
	applyEnvOverrides(&cfg)
	validateAndApplyDefaults(&cfg)

	GlobalConfig = &cfg
	return nil
}

// applyEnvOverrides applies environment variable overrides to the configuration.
// Environment variables take precedence over config file values.
func applyEnvOverrides(cfg *Config) {
	// Image Validation configuration
	if v := os.Getenv("IMAGE_VALIDATION_ENABLED"); v != "" {
		if enabled, err := strconv.ParseBool(v); err == nil {
			cfg.ImageValidation.Enabled = enabled
		} else {
			log.Printf("[WARN] Invalid IMAGE_VALIDATION_ENABLED value '%s', using config file value: %v", v, err)
		}
	}

	if v := os.Getenv("IMAGE_VALIDATION_TIMEOUT"); v != "" {
		if seconds, err := strconv.Atoi(v); err == nil && seconds > 0 {
			cfg.ImageValidation.Timeout = time.Duration(seconds) * time.Second
		} else {
			log.Printf("[WARN] Invalid IMAGE_VALIDATION_TIMEOUT value '%s', using config file value: %v", v, err)
		}
	}

	if v := os.Getenv("IMAGE_VALIDATION_CACHE_DURATION"); v != "" {
		if seconds, err := strconv.Atoi(v); err == nil && seconds > 0 {
			cfg.ImageValidation.CacheDuration = time.Duration(seconds) * time.Second
		} else {
			log.Printf("[WARN] Invalid IMAGE_VALIDATION_CACHE_DURATION value '%s', using config file value: %v", v, err)
		}
	}

	if v := os.Getenv("IMAGE_VALIDATION_SKIP_ON_TIMEOUT"); v != "" {
		if skip, err := strconv.ParseBool(v); err == nil {
			cfg.ImageValidation.SkipOnTimeout = skip
		} else {
			log.Printf("[WARN] Invalid IMAGE_VALIDATION_SKIP_ON_TIMEOUT value '%s', using config file value: %v", v, err)
		}
	}

	// Resource Releaser configuration
	if v := os.Getenv("RESOURCE_RELEASER_IMAGE_PULL_TIMEOUT"); v != "" {
		if seconds, err := strconv.Atoi(v); err == nil && seconds > 0 {
			cfg.ResourceReleaser.ImagePullTimeout = time.Duration(seconds) * time.Second
		} else {
			log.Printf("[WARN] Invalid RESOURCE_RELEASER_IMAGE_PULL_TIMEOUT value '%s', using config file value: %v", v, err)
		}
	}

	if v := os.Getenv("RESOURCE_RELEASER_CHECK_INTERVAL"); v != "" {
		if seconds, err := strconv.Atoi(v); err == nil && seconds > 0 {
			cfg.ResourceReleaser.CheckInterval = time.Duration(seconds) * time.Second
		} else {
			log.Printf("[WARN] Invalid RESOURCE_RELEASER_CHECK_INTERVAL value '%s', using config file value: %v", v, err)
		}
	}

	if v := os.Getenv("RESOURCE_RELEASER_MAX_RETRIES"); v != "" {
		if retries, err := strconv.Atoi(v); err == nil && retries >= 0 {
			cfg.ResourceReleaser.MaxRetries = retries
		} else {
			log.Printf("[WARN] Invalid RESOURCE_RELEASER_MAX_RETRIES value '%s', using config file value: %v", v, err)
		}
	}
}

// validateAndApplyDefaults validates configuration values and applies defaults for invalid values.
// This implements Property 9: Configuration Fallback to Defaults from the design document.
// Validates: Requirements 8.5
func validateAndApplyDefaults(cfg *Config) {
	defaults := DefaultImageValidationConfig()
	releaserDefaults := DefaultResourceReleaserConfig()

	// Validate ImageValidation configuration
	// Note: If imageValidation section is not present in config file, all fields will be zero values.
	// We need to apply defaults for all fields, not just validate them.

	// Apply default for Enabled if the entire imageValidation section is missing
	// We detect this by checking if both Timeout and CacheDuration are zero (unlikely to be intentionally set to 0)
	if cfg.ImageValidation.Timeout == 0 && cfg.ImageValidation.CacheDuration == 0 {
		// Entire section is missing, apply all defaults
		log.Printf("[INFO] imageValidation section not found in config, using defaults (enabled=%v)",
			defaults.Enabled)
		cfg.ImageValidation.Enabled = defaults.Enabled
		cfg.ImageValidation.Timeout = defaults.Timeout
		cfg.ImageValidation.CacheDuration = defaults.CacheDuration
		cfg.ImageValidation.SkipOnTimeout = defaults.SkipOnTimeout
	} else {
		// Section exists but some values might be invalid
		if cfg.ImageValidation.Timeout <= 0 {
			log.Printf("[WARN] Invalid imageValidation.timeout value '%v', using default '%v'",
				cfg.ImageValidation.Timeout, defaults.Timeout)
			cfg.ImageValidation.Timeout = defaults.Timeout
		}

		if cfg.ImageValidation.CacheDuration <= 0 {
			log.Printf("[WARN] Invalid imageValidation.cacheDuration value '%v', using default '%v'",
				cfg.ImageValidation.CacheDuration, defaults.CacheDuration)
			cfg.ImageValidation.CacheDuration = defaults.CacheDuration
		}
	}

	// Validate ResourceReleaser configuration
	if cfg.ResourceReleaser.ImagePullTimeout <= 0 {
		log.Printf("[WARN] Invalid resourceReleaser.imagePullTimeout value '%v', using default '%v'",
			cfg.ResourceReleaser.ImagePullTimeout, releaserDefaults.ImagePullTimeout)
		cfg.ResourceReleaser.ImagePullTimeout = releaserDefaults.ImagePullTimeout
	}

	if cfg.ResourceReleaser.CheckInterval <= 0 {
		log.Printf("[WARN] Invalid resourceReleaser.checkInterval value '%v', using default '%v'",
			cfg.ResourceReleaser.CheckInterval, releaserDefaults.CheckInterval)
		cfg.ResourceReleaser.CheckInterval = releaserDefaults.CheckInterval
	}

	if cfg.ResourceReleaser.MaxRetries < 0 {
		log.Printf("[WARN] Invalid resourceReleaser.maxRetries value '%d', using default '%d'",
			cfg.ResourceReleaser.MaxRetries, releaserDefaults.MaxRetries)
		cfg.ResourceReleaser.MaxRetries = releaserDefaults.MaxRetries
	}
}
