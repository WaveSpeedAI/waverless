package main

import (
	"os"
	"os/signal"
	"syscall"
	"time"

	"waverless/pkg/logger"
)

func main() {
	// Create application instance
	app := NewApplication()

	// Initialize all components
	if err := app.Initialize(); err != nil {
		logger.FatalCtx(nil, "Application initialization failed: %v", err)
	}

	// Start all components
	if err := app.Start(); err != nil {
		logger.FatalCtx(app.ctx, "Application startup failed: %v", err)
	}

	// Wait for exit signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	logger.InfoCtx(app.ctx, "Received exit signal: %v", sig)

	// Graceful shutdown (30 seconds timeout)
	if err := app.Shutdown(30 * time.Second); err != nil {
		logger.ErrorCtx(app.ctx, "Application shutdown failed: %v", err)
		os.Exit(1)
	}

	logger.InfoCtx(app.ctx, "Application safely exited")
}

