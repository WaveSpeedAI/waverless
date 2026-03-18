package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"waverless/pkg/config"

	"github.com/gin-gonic/gin"
)

func TestPortalAuthMiddleware_RequiresClusterID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	config.GlobalConfig = &config.Config{
		Server: config.ServerConfig{
			APIKey: "test-key",
		},
	}

	r := gin.New()
	r.Use(PortalAuthMiddleware())
	r.GET("/portal/v1/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/portal/v1/test", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestPortalAuthMiddleware_SetsContextValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	config.GlobalConfig = &config.Config{
		Server: config.ServerConfig{
			APIKey: "test-key",
		},
	}

	r := gin.New()
	r.Use(PortalAuthMiddleware())
	r.GET("/portal/v1/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"cluster_id": c.GetString("portal_cluster_id"),
			"request_id": c.GetString("portal_request_id"),
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/portal/v1/test", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	req.Header.Set("X-Cluster-Id", "cluster-a")
	req.Header.Set("X-Request-Id", "req-123")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
	}

	body := rec.Body.String()
	if body == "" || !strings.Contains(body, "cluster-a") || !strings.Contains(body, "req-123") {
		t.Fatalf("unexpected response body: %s", body)
	}
}
