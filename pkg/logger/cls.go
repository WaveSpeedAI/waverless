package logger

import (
	"os"
	"sync"
	"time"
	"waverless/pkg/config"

	cls "github.com/tencentcloud/tencentcloud-cls-sdk-go"
	"go.uber.org/zap/zapcore"
)

// CLSConfig CLS 日志配置
type CLSConfig struct {
	TopicID   string // CLS 日志主题 ID
	Endpoint  string // 地域接入点，如 na-siliconvalley.cls.tencentcs.com
	SecretID  string // 腾讯云 SecretId
	SecretKey string // 腾讯云 SecretKey
	Source    string // 服务标识，用于区分不同服务
}

// CLSCore 实现 zapcore.Core 接口，将日志发送到腾讯云 CLS
type CLSCore struct {
	producer *cls.AsyncProducerClient
	topicID  string
	source   string
	level    zapcore.Level
	encoder  zapcore.Encoder
	fields   []zapcore.Field
	mu       sync.Mutex
}

// clsCallback CLS 回调
type clsCallback struct{}

func (c *clsCallback) Success(result *cls.Result) {}
func (c *clsCallback) Fail(result *cls.Result)    {}

// NewCLSCore 创建 CLS Core
func NewCLSCore(cfg CLSConfig, level zapcore.Level, encoder zapcore.Encoder) (*CLSCore, error) {
	producerConfig := cls.GetDefaultAsyncProducerClientConfig()
	producerConfig.Endpoint = cfg.Endpoint
	producerConfig.AccessKeyID = cfg.SecretID
	producerConfig.AccessKeySecret = cfg.SecretKey

	producer, err := cls.NewAsyncProducerClient(producerConfig)
	if err != nil {
		return nil, err
	}
	producer.Start()

	return &CLSCore{
		producer: producer,
		topicID:  cfg.TopicID,
		source:   cfg.Source,
		level:    level,
		encoder:  encoder,
	}, nil
}

// Enabled 实现 zapcore.Core
func (c *CLSCore) Enabled(level zapcore.Level) bool {
	return level >= c.level
}

// With 实现 zapcore.Core
func (c *CLSCore) With(fields []zapcore.Field) zapcore.Core {
	clone := &CLSCore{
		producer: c.producer,
		topicID:  c.topicID,
		source:   c.source,
		level:    c.level,
		encoder:  c.encoder.Clone(),
		fields:   append(c.fields, fields...),
	}
	return clone
}

// Check 实现 zapcore.Core
func (c *CLSCore) Check(entry zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(entry.Level) {
		return ce.AddCore(entry, c)
	}
	return ce
}

// Write 实现 zapcore.Core
func (c *CLSCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	allFields := append(c.fields, fields...)

	// 构建日志内容
	content := map[string]string{
		"service":   c.source,
		"level":     entry.Level.String(),
		"message":   entry.Message,
		"logger":    entry.LoggerName,
		"caller":    entry.Caller.TrimmedPath(),
		"timestamp": entry.Time.Format("2006-01-02 15:04:05.000"),
	}

	// 添加额外字段
	for _, f := range allFields {
		content[f.Key] = fieldToString(f)
	}

	log := cls.NewCLSLog(time.Now().Unix(), content)
	return c.producer.SendLog(c.topicID, log, &clsCallback{})
}

// Sync 实现 zapcore.Core
func (c *CLSCore) Sync() error {
	return nil
}

// Close 关闭 CLS producer
func (c *CLSCore) Close() {
	if c.producer != nil {
		c.producer.Close(5000) // 5秒超时
	}
}

// fieldToString 将 zap field 转换为字符串
func fieldToString(f zapcore.Field) string {
	switch f.Type {
	case zapcore.StringType:
		return f.String
	case zapcore.Int64Type, zapcore.Int32Type, zapcore.Int16Type, zapcore.Int8Type:
		return string(rune(f.Integer))
	default:
		return f.String
	}
}

// GetCLSConfigFromEnv 从环境变量获取 CLS 配置
// 环境变量：
//   - CLS_TOPIC_ID: 日志主题 ID
//   - CLS_ENDPOINT: 地域接入点
//   - TENCENTCLOUD_SECRET_ID: 腾讯云 SecretId
//   - TENCENTCLOUD_SECRET_KEY: 腾讯云 SecretKey
//   - CLS_SOURCE: 服务标识（可选，默认 waverless）
func GetCLSConfigFromEnv() *CLSConfig {
	topicID := os.Getenv("CLS_TOPIC_ID")
	endpoint := os.Getenv("CLS_ENDPOINT")
	secretID := os.Getenv("TENCENTCLOUD_SECRET_ID")
	secretKey := os.Getenv("TENCENTCLOUD_SECRET_KEY")

	// 任一必要参数缺失则返回 nil，降级为原有日志方式
	if topicID == "" || endpoint == "" || secretID == "" || secretKey == "" {
		return nil
	}

	source := os.Getenv("CLS_SOURCE")
	if source == "" {
		source = "waverless"
	}

	return &CLSConfig{
		TopicID:   topicID,
		Endpoint:  endpoint,
		SecretID:  secretID,
		SecretKey: secretKey,
		Source:    source,
	}
}

// GetCLSConfig 获取 CLS 配置，优先级：环境变量 > 配置文件
// 返回 nil 表示不启用 CLS
func GetCLSConfig(cfgCLS *config.CLSLogConfig) *CLSConfig {
	// 优先从环境变量获取
	if envCfg := GetCLSConfigFromEnv(); envCfg != nil {
		return envCfg
	}

	// 从配置文件获取
	if cfgCLS == nil || !cfgCLS.Enabled {
		return nil
	}

	// 检查必要参数
	if cfgCLS.TopicID == "" || cfgCLS.Endpoint == "" || cfgCLS.SecretID == "" || cfgCLS.SecretKey == "" {
		return nil
	}

	source := cfgCLS.Source
	if source == "" {
		source = "waverless"
	}

	return &CLSConfig{
		TopicID:   cfgCLS.TopicID,
		Endpoint:  cfgCLS.Endpoint,
		SecretID:  cfgCLS.SecretID,
		SecretKey: cfgCLS.SecretKey,
		Source:    source,
	}
}
