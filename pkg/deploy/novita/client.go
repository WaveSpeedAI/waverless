package novita

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"waverless/pkg/config"
	"waverless/pkg/logger"
)

// Client is the Novita API client
type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new Novita API client
func NewClient(cfg *config.NovitaConfig) *Client {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.novita.ai"
	}

	return &Client{
		apiKey:  cfg.APIKey,
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// CreateEndpoint creates a new endpoint
func (c *Client) CreateEndpoint(ctx context.Context, req *CreateEndpointRequest) (*CreateEndpointResponse, error) {
	url := c.baseURL + "/gpu-instance/openapi/v1/endpoint/create"

	respData, err := c.doRequest(ctx, "POST", url, req)
	if err != nil {
		return nil, err
	}

	var resp CreateEndpointResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse create endpoint response: %w", err)
	}

	return &resp, nil
}

// GetEndpoint gets endpoint details
func (c *Client) GetEndpoint(ctx context.Context, endpointID string) (*GetEndpointResponse, error) {
	url := fmt.Sprintf("%s/gpu-instance/openapi/v1/endpoint?id=%s", c.baseURL, endpointID)

	respData, err := c.doRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	var resp GetEndpointResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse get endpoint response: %w", err)
	}

	return &resp, nil
}

// ListEndpoints lists all endpoints
func (c *Client) ListEndpoints(ctx context.Context) (*ListEndpointsResponse, error) {
	url := c.baseURL + "/gpu-instance/openapi/v1/endpoints"

	respData, err := c.doRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	var resp ListEndpointsResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse list endpoints response: %w", err)
	}

	return &resp, nil
}

// UpdateEndpoint updates an existing endpoint
func (c *Client) UpdateEndpoint(ctx context.Context, req *UpdateEndpointRequest) error {
	url := c.baseURL + "/gpu-instance/openapi/v1/endpoint/update"

	_, err := c.doRequest(ctx, "POST", url, req)
	return err
}

// DeleteEndpoint deletes an endpoint
func (c *Client) DeleteEndpoint(ctx context.Context, endpointID string) error {
	url := c.baseURL + "/gpu-instance/openapi/v1/endpoint/delete"

	req := &DeleteEndpointRequest{
		ID: endpointID,
	}

	_, err := c.doRequest(ctx, "POST", url, req)
	return err
}

// CreateRegistryAuth creates a new container registry authentication
func (c *Client) CreateRegistryAuth(ctx context.Context, req *CreateRegistryAuthRequest) (*CreateRegistryAuthResponse, error) {
	url := c.baseURL + "/gpu-instance/openapi/v1/repository/auth/save"

	respData, err := c.doRequest(ctx, "POST", url, req)
	if err != nil {
		return nil, err
	}

	var resp CreateRegistryAuthResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse create registry auth response: %w", err)
	}

	return &resp, nil
}

// ListRegistryAuths lists all container registry authentications
func (c *Client) ListRegistryAuths(ctx context.Context) (*ListRegistryAuthsResponse, error) {
	url := c.baseURL + "/gpu-instance/openapi/v1/repository/auths"

	respData, err := c.doRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	var resp ListRegistryAuthsResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse list registry auths response: %w", err)
	}

	return &resp, nil
}

// DeleteRegistryAuth deletes a container registry authentication
func (c *Client) DeleteRegistryAuth(ctx context.Context, authID string) error {
	url := c.baseURL + "/gpu-instance/openapi/v1/repository/auth/delete"

	req := &DeleteRegistryAuthRequest{
		ID: authID,
	}

	_, err := c.doRequest(ctx, "POST", url, req)
	return err
}

// DrainWorker drains or undrains a worker
// When drain=true, the worker stops accepting new tasks and will be removed after current tasks complete
func (c *Client) DrainWorker(ctx context.Context, req *DrainWorkerRequest) error {
	url := c.baseURL + "/gpu-instance/openapi/v1/endpoint/worker/drain"

	_, err := c.doRequest(ctx, "POST", url, req)
	return err
}

// doRequest performs an HTTP request with proper authentication
func (c *Client) doRequest(ctx context.Context, method, url string, body interface{}) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(jsonData)

		// Log request for debugging
		logger.Debugf("Novita API Request: %s %s, Body: %s", method, url, string(jsonData))
	} else {
		logger.Debugf("Novita API Request: %s %s", method, url)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	// Execute request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute HTTP request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Log response for debugging
	// logger.Debugf("Novita API Response: Status %d, Body: %s", resp.StatusCode, string(respData))

	// Check for HTTP errors
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var errResp ErrorResponse
		if err := json.Unmarshal(respData, &errResp); err == nil && errResp.Message != "" {
			return nil, fmt.Errorf("novita API error (status %d): %s", resp.StatusCode, errResp.Message)
		}
		return nil, fmt.Errorf("novita API error (status %d): %s", resp.StatusCode, string(respData))
	}

	return respData, nil
}
