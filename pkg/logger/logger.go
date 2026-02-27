package logger

import (
	"context"
	"fmt"
	"os"

	"waverless/pkg/config"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

var Log *zap.Logger
var sugar *zap.SugaredLogger
var clsCore *CLSCore // CLS core 用于关闭时清理

const (
	defaultTraceID = "0"
)

func init() {
	// Create default development environment configuration
	defaultConfig := zap.NewDevelopmentConfig()
	defaultConfig.EncoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
	defaultConfig.EncoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout("2006-01-02 15:04:05.000")

	// Create default logger
	defaultLogger, _ := defaultConfig.Build(zap.AddCallerSkip(1))

	// Initialize global variables
	Log = defaultLogger
	sugar = defaultLogger.Sugar()
}

// Init initializes logger
func Init() error {
	cfg := config.GlobalConfig.Logger

	// Set log level
	atomicLevel := zap.NewAtomicLevel()
	switch cfg.Level {
	case "debug":
		atomicLevel.SetLevel(zapcore.DebugLevel)
	case "info":
		atomicLevel.SetLevel(zapcore.InfoLevel)
	case "warn":
		atomicLevel.SetLevel(zapcore.WarnLevel)
	case "error":
		atomicLevel.SetLevel(zapcore.ErrorLevel)
	default:
		atomicLevel.SetLevel(zapcore.InfoLevel)
	}

	// Configure encoder
	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalLevelEncoder,
		EncodeTime:     zapcore.TimeEncoderOfLayout("2006-01-02 15:04:05.000"),
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	// Configure output
	var syncer zapcore.WriteSyncer
	switch cfg.Output {
	case "file":
		syncer = zapcore.AddSync(getLogWriter(cfg))
	case "both":
		syncer = zapcore.NewMultiWriteSyncer(
			zapcore.AddSync(os.Stdout),
			zapcore.AddSync(getLogWriter(cfg)),
		)
	default: // console
		syncer = zapcore.AddSync(os.Stdout)
	}

	// 基础 core（控制台/文件）- 使用 JSON 格式
	baseCore := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderConfig),
		syncer,
		atomicLevel,
	)

	// 尝试初始化 CLS core（优先环境变量，其次配置文件）
	var cores []zapcore.Core
	cores = append(cores, baseCore)

	if clsCfg := GetCLSConfig(&cfg.CLS); clsCfg != nil {
		if cc, err := NewCLSCore(*clsCfg, atomicLevel.Level(), zapcore.NewJSONEncoder(encoderConfig)); err == nil {
			cores = append(cores, cc)
			clsCore = cc
			// 使用标准库 log 输出，避免循环依赖
			println("[INFO] CLS logging enabled, topic:", clsCfg.TopicID)
		} else {
			println("[WARN] CLS init failed:", err.Error())
		}
	}

	// Create logger with tee core
	Log = zap.New(zapcore.NewTee(cores...), zap.AddCaller(), zap.AddCallerSkip(1))
	sugar = Log.Sugar()

	return nil
}

// getLogWriter creates log writer with rotation support
func getLogWriter(cfg config.LoggerConfig) zapcore.WriteSyncer {
	maxSize := cfg.File.MaxSize
	if maxSize == 0 {
		maxSize = 100
	}
	maxBackups := cfg.File.MaxBackups
	if maxBackups == 0 {
		maxBackups = 3
	}
	maxAge := cfg.File.MaxAge
	if maxAge == 0 {
		maxAge = 7
	}

	return zapcore.AddSync(&lumberjack.Logger{
		Filename:   cfg.File.Path,
		MaxSize:    maxSize,
		MaxBackups: maxBackups,
		MaxAge:     maxAge,
		Compress:   cfg.File.Compress,
	})
}

// defaultFields returns default field list, including trace_id
func defaultFields() []zap.Field {
	return []zap.Field{zap.String("trace_id", defaultTraceID)}
}

// withDefaultFields adds default fields to existing field list
func withDefaultFields(fields ...zap.Field) []zap.Field {
	return append(defaultFields(), fields...)
}

// Debug level
func Debug(msg string, fields ...zap.Field) {
	Log.Debug(msg, withDefaultFields(fields...)...)
}

// Info level
func Info(msg string, fields ...zap.Field) {
	Log.Info(msg, withDefaultFields(fields...)...)
}

// Warn level
func Warn(msg string, fields ...zap.Field) {
	Log.Warn(msg, withDefaultFields(fields...)...)
}

// Error level
func Error(msg string, fields ...zap.Field) {
	Log.Error(msg, withDefaultFields(fields...)...)
}

// Fatal level
func Fatal(msg string, fields ...zap.Field) {
	Log.Fatal(msg, withDefaultFields(fields...)...)
}

// defaultPrefix returns default log prefix
func defaultPrefix() string {
	return fmt.Sprintf("%s\t", defaultTraceID)
}

// Debugf formats Debug log
func Debugf(format string, args ...interface{}) {
	sugar.Debugf(defaultPrefix()+format, args...)
}

// Infof formats Info log
func Infof(format string, args ...interface{}) {
	sugar.Infof(defaultPrefix()+format, args...)
}

// Warnf formats Warn log
func Warnf(format string, args ...interface{}) {
	sugar.Warnf(defaultPrefix()+format, args...)
}

// Errorf formats Error log
func Errorf(format string, args ...interface{}) {
	sugar.Errorf(defaultPrefix()+format, args...)
}

// Fatalf formats Fatal log
func Fatalf(format string, args ...interface{}) {
	sugar.Fatalf(defaultPrefix()+format, args...)
}

// getTraceFields retrieves trace-related fields
func getTraceFields(ctx context.Context) string {
	if ctx == nil {
		return "0"
	}
	// waverless doesn't support trace yet, use default value
	return "0"
}

func DebugCtx(ctx context.Context, format string, args ...interface{}) {
	tracePrefix := fmt.Sprintf("%s\t", getTraceFields(ctx))
	sugar.Debugf(tracePrefix+format, args...)
}

func InfoCtx(ctx context.Context, format string, args ...interface{}) {
	tracePrefix := fmt.Sprintf("%s\t", getTraceFields(ctx))
	sugar.Infof(tracePrefix+format, args...)
}

func WarnCtx(ctx context.Context, format string, args ...interface{}) {
	tracePrefix := fmt.Sprintf("%s\t", getTraceFields(ctx))
	sugar.Warnf(tracePrefix+format, args...)
}

func ErrorCtx(ctx context.Context, format string, args ...interface{}) {
	tracePrefix := fmt.Sprintf("%s\t", getTraceFields(ctx))
	sugar.Errorf(tracePrefix+format, args...)
}

func FatalCtx(ctx context.Context, format string, args ...interface{}) {
	tracePrefix := fmt.Sprintf("%s\t", getTraceFields(ctx))
	sugar.Fatalf(tracePrefix+format, args...)
}

// Sync flushes any buffered log entries
func Sync() error {
	return Log.Sync()
}

// Close 关闭 logger，包括 CLS producer
func Close() {
	if clsCore != nil {
		clsCore.Close()
	}
	Log.Sync()
}
