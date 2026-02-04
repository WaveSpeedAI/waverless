// Package response defines unified HTTP response format
package response

import (
	"net/http"

	apperrors "waverless/pkg/common/errors"

	"github.com/gin-gonic/gin"
)

// Response represents the unified response structure
type Response struct {
	Code    int    `json:"code"`
	Message string `json:"message,omitempty"`
	Data    any    `json:"data,omitempty"`
}

// Success returns a success response
func Success(c *gin.Context, data any) {
	c.JSON(http.StatusOK, data)
}

// SuccessWithMessage returns a success response with message
func SuccessWithMessage(c *gin.Context, message string, data any) {
	c.JSON(http.StatusOK, gin.H{
		"message": message,
		"data":    data,
	})
}

// Created returns a created response
func Created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, data)
}

// NoContent returns a no content response
func NoContent(c *gin.Context) {
	c.Status(http.StatusNoContent)
}

// Error returns an error response
func Error(c *gin.Context, err error) {
	if appErr, ok := err.(*apperrors.AppError); ok {
		c.JSON(appErr.HTTPCode(), gin.H{
			"error": appErr.Message,
		})
		return
	}

	c.JSON(http.StatusInternalServerError, gin.H{
		"error": err.Error(),
	})
}

// ErrorWithCode returns an error response with specified status code
func ErrorWithCode(c *gin.Context, code int, message string) {
	c.JSON(code, gin.H{
		"error": message,
	})
}

// BadRequest returns a 400 error
func BadRequest(c *gin.Context, message string) {
	ErrorWithCode(c, http.StatusBadRequest, message)
}

// Unauthorized returns a 401 error
func Unauthorized(c *gin.Context, message string) {
	if message == "" {
		message = "unauthorized"
	}
	ErrorWithCode(c, http.StatusUnauthorized, message)
}

// Forbidden returns a 403 error
func Forbidden(c *gin.Context, message string) {
	if message == "" {
		message = "forbidden"
	}
	ErrorWithCode(c, http.StatusForbidden, message)
}

// NotFound returns a 404 error
func NotFound(c *gin.Context, message string) {
	if message == "" {
		message = "not found"
	}
	ErrorWithCode(c, http.StatusNotFound, message)
}

// Conflict returns a 409 error
func Conflict(c *gin.Context, message string) {
	ErrorWithCode(c, http.StatusConflict, message)
}

// InternalError returns a 500 error
func InternalError(c *gin.Context, message string) {
	if message == "" {
		message = "internal server error"
	}
	ErrorWithCode(c, http.StatusInternalServerError, message)
}

// ServiceUnavailable returns a 503 error
func ServiceUnavailable(c *gin.Context, message string) {
	if message == "" {
		message = "service unavailable"
	}
	ErrorWithCode(c, http.StatusServiceUnavailable, message)
}

// List returns a list response
func List(c *gin.Context, items any, total int64, limit, offset int) {
	c.JSON(http.StatusOK, gin.H{
		"items":  items,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// Page returns a paginated response
func Page(c *gin.Context, items any, total int64, page, pageSize int) {
	c.JSON(http.StatusOK, gin.H{
		"items":    items,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
	})
}
