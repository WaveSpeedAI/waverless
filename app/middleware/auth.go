package middleware

import (
	"net/http"
	"strings"

	"waverless/pkg/config"
	"waverless/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// AuthMiddleware simple token authentication middleware
func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !validateBearerAPIKey(c) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			c.Abort()
			return
		}

		c.Next()
	}
}

// PortalAuthMiddleware authenticates portal southbound requests and captures routing headers.
func PortalAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !validateBearerAPIKey(c) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			c.Abort()
			return
		}

		clusterID := c.GetHeader("X-Cluster-Id")
		if clusterID == "" {
			logger.WarnCtx(c.Request.Context(), "portal request missing X-Cluster-Id header")
			c.JSON(http.StatusBadRequest, gin.H{"error": "missing X-Cluster-Id"})
			c.Abort()
			return
		}

		requestID := c.GetHeader("X-Request-Id")
		if requestID == "" {
			requestID = c.GetHeader("X-Request-ID")
		}
		if requestID == "" {
			requestID = c.Request.Header.Get("X-Request-Id")
		}
		if requestID == "" {
			requestID = c.Request.Header.Get("X-Request-ID")
		}
		if requestID == "" {
			requestID = strings.TrimSpace(c.Query("request_id"))
		}
		if requestID == "" {
			requestID = uuid.NewString()
		}

		c.Set("portal_cluster_id", clusterID)
		c.Set("portal_request_id", requestID)
		c.Next()
	}
}

func validateBearerAPIKey(c *gin.Context) bool {
	// Read expected API key from config
	expectedAPIKey := config.GlobalConfig.Server.APIKey

	// Skip authentication if API key is not configured
	if expectedAPIKey == "" {
		logger.DebugCtx(c.Request.Context(), "API key not configured, skipping auth")
		return true
	}

	// Get token from Authorization header
	authHeader := c.GetHeader("Authorization")
	authHeader = strings.TrimPrefix(authHeader, "Bearer ")

	// Validate token
	if authHeader != expectedAPIKey {
		logger.WarnCtx(c.Request.Context(), "unauthorized request, invalid API key")
		return false
	}

	return true
}
