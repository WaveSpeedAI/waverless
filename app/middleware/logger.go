package middleware

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/pretty"
)

func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get caller information
		_, file, line, ok := runtime.Caller(0)
		fileInfo := "???"
		if ok {
			fileInfo = fmt.Sprintf("%s:%d", filepath.Base(file), line)
		}

		// Start time
		startTime := time.Now()

		// If it's a POST request and need to print request body
		var bodyStr string
		if c.Request.Method == "POST" {
			bodyStr = getRequestBody(c)
		}

		// Process request
		c.Next()

		// Skip logging for 404 requests
		if c.Writer.Status() == http.StatusNotFound {
			return
		}

		// End time
		endTime := time.Now()

		// Execution time
		latencyTime := endTime.Sub(startTime)

		// Request method
		reqMethod := c.Request.Method

		// Request URI
		reqUri := c.Request.RequestURI

		// Status code
		statusCode := c.Writer.Status()

		// Client IP
		clientIP := c.ClientIP()

		// Basic log format
		logMsg := fmt.Sprintf("[GIN] %v | %s | %3d | %13v | %15s | %s | %s",
			startTime.Format("2006/01/02 - 15:04:05"),
			fileInfo,
			statusCode,
			latencyTime,
			clientIP,
			reqMethod,
			reqUri,
		)

		// Add request body to log if present
		if bodyStr != "" {
			logMsg += fmt.Sprintf("\nRequest Body: %s", bodyStr)
		}

		fmt.Println(logMsg)
	}
}

// getRequestBody gets request body content
func getRequestBody(c *gin.Context) string {
	var bodyBytes []byte
	if c.Request.Body != nil {
		bodyBytes, _ = io.ReadAll(c.Request.Body)
		// Reset request body since reading it clears it
		c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	}
	return CompressBody(string(bodyBytes))
}

// CompressBody compresses JSON using pretty package
func CompressBody(body string) string {
	if len(body) == 0 {
		return ""
	}

	// Compress JSON, ugly=true means remove all whitespace
	compressed := pretty.Ugly([]byte(body))
	if len(compressed) > 1000 {
		return string(compressed[:1000]) + "..."
	}
	return string(compressed)
}
