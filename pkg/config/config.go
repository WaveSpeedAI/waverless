package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

var GlobalConfig *Config

// Config global configuration
type Config struct {
	Server     ServerConfig      `yaml:"server"`
	Redis      RedisConfig       `yaml:"redis"`
	MySQL      MySQLConfig       `yaml:"mysql"`
	Queue      QueueConfig       `yaml:"queue"`
	Worker     WorkerConfig      `yaml:"worker"`
	Logger     LoggerConfig      `yaml:"logger"`
	K8s        K8sConfig         `yaml:"k8s"`
	AutoScaler AutoScalerConfig  `yaml:"autoscaler"`
	Providers  *ProvidersConfig  `yaml:"providers,omitempty"` // Providers configuration (optional)
}

// ServerConfig server configuration
type ServerConfig struct {
	Port   int    `yaml:"port"`
	Mode   string `yaml:"mode"`    // debug, release
	APIKey string `yaml:"api_key"` // API key for worker authentication (optional, if empty, auth is disabled)
}

// RedisConfig Redis configuration
type RedisConfig struct {
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

// MySQLConfig MySQL configuration
type MySQLConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Database string `yaml:"database"`
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
	Path string `yaml:"path"`
}

// K8sConfig K8s configuration
type K8sConfig struct {
	Enabled    bool   `yaml:"enabled"`     // whether to enable K8s features
	Namespace  string `yaml:"namespace"`   // K8s namespace
	Platform   string `yaml:"platform"`    // Platform type: generic, aliyun-ack, aws-eks
	ConfigDir  string `yaml:"config_dir"`  // Configuration directory (specs.yaml and templates)
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

	GlobalConfig = &cfg
	return nil
}
