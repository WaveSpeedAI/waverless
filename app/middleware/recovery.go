package middleware

import (
	"waverless/pkg/logger"
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
)

// Recovery middleware catches panic and converts it to standard error response
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				// Get stack trace
				stack := debug.Stack()

				// Log error
				logger.ErrorCtx(c.Request.Context(),
					"panic recovered: %v\nstack:\n%s",
					err,
					string(stack),
				)
				// Return stack trace in debug mode
				if gin.Mode() == gin.DebugMode {
					c.JSON(http.StatusInternalServerError, gin.H{
						"error":   err,
						"stack":   string(stack),
						"message": "Internal Server Error",
					})
				}
			}
		}()

		c.Next()
	}
}
