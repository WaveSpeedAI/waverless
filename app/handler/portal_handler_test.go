package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPortalHandler_GetInstanceInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewPortalHandler(nil, nil, nil, nil)
	r := gin.New()
	r.GET("/portal/v1/instance/info", func(c *gin.Context) {
		c.Set("portal_cluster_id", "cluster-a")
		c.Set("portal_request_id", "req-123")
		h.GetInstanceInfo(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/portal/v1/instance/info", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "cluster-a") || !strings.Contains(body, "portal-v1") {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestProviderTypes_NilProvider(t *testing.T) {
	got := providerTypes(nil)
	if len(got) != 1 || got[0] != "unknown" {
		t.Fatalf("unexpected provider types: %#v", got)
	}
}
