package middleware

import (
	"net/http"

	"waverless/pkg/config"
	"waverless/pkg/logger"

	"github.com/gin-gonic/gin"
)

// AuthMiddleware simple token authentication middleware
func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Read expected API key from config
		expectedAPIKey := config.GlobalConfig.Server.APIKey

		// Skip authentication if API key is not configured
		if expectedAPIKey == "" {
			logger.DebugCtx(c.Request.Context(), "API key not configured, skipping auth")
			c.Next()
			return
		}

		// Get token from Authorization header
		authHeader := c.GetHeader("Authorization")

		// Validate token
		if authHeader != expectedAPIKey {
			logger.WarnCtx(c.Request.Context(), "unauthorized request, invalid API key")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			c.Abort()
			return
		}

		c.Next()
	}
}
