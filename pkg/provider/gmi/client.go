package gmi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"waverless/pkg/logger"
)

// doRequest executes an HTTP request with Bearer token auth
func (p *GMIDeploymentProvider) doRequest(ctx context.Context, method, url string, body interface{}) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request: %w", err)
		}
		logger.Debugf("GMI API request: %s %s body=%s", method, url, string(jsonData))
		reqBody = bytes.NewBuffer(jsonData)
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// gmiless uses "Authorization: Bearer <token>" for /api/v1 routes
	httpReq.Header.Set("Authorization", "Bearer "+p.token)
	if body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	httpReq.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to call GMI API: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GMI API returned error status %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// getEndpointID looks up the gmiless endpoint ID by name (with cache)
func (p *GMIDeploymentProvider) getEndpointID(ctx context.Context, endpoint string) (string, error) {
	// Check cache
	if id, ok := p.endpointCache.Load(endpoint); ok {
		return id.(string), nil
	}

	// Not cached, query list API to populate cache
	_, err := p.ListApps(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to list endpoints: %w", err)
	}

	if id, ok := p.endpointCache.Load(endpoint); ok {
		return id.(string), nil
	}

	return "", fmt.Errorf("endpoint %s not found in GMI", endpoint)
}
