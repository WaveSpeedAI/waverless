package gmi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"waverless/pkg/logger"
)

// doRequestRaw executes an HTTP request and returns raw response bytes.
// Used by GetAppLogs for plain text responses that don't use the BFF JSON envelope.
func (p *GMIDeploymentProvider) doRequestRaw(ctx context.Context, method, url string, body interface{}) ([]byte, error) {
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

	httpReq.Header.Set("Authorization", "Bearer "+p.token)
	if body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}

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

// doRequestBFF executes an HTTP request against the BFF API.
// It unwraps the BFF response envelope {"msg":"success","data":{...}}
// and unmarshals the data field into result (if non-nil).
// Handles 204 No Content (Delete) gracefully.
func (p *GMIDeploymentProvider) doRequestBFF(ctx context.Context, method, path string, body interface{}, result interface{}) error {
	url := p.baseURL + path

	var reqBody io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request: %w", err)
		}
		logger.Debugf("BFF API request: %s %s body=%s", method, path, string(jsonData))
		reqBody = bytes.NewBuffer(jsonData)
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+p.token)
	if body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	httpReq.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to call BFF API: %w", err)
	}
	defer resp.Body.Close()

	// 204 No Content — Delete returns empty body
	if resp.StatusCode == http.StatusNoContent {
		return nil
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	// Error responses (4xx/5xx)
	if resp.StatusCode >= 400 {
		var errResp bffResponse
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Msg != "" {
			return fmt.Errorf("BFF API error %d: %s", resp.StatusCode, errResp.Msg)
		}
		return fmt.Errorf("BFF API error %d: %s", resp.StatusCode, string(respBody))
	}

	// Unwrap BFF envelope: {"msg":"success","data":{...}}
	var bffResp bffResponse
	if err := json.Unmarshal(respBody, &bffResp); err != nil {
		return fmt.Errorf("failed to parse BFF response: %w", err)
	}

	if result != nil && len(bffResp.Data) > 0 {
		if err := json.Unmarshal(bffResp.Data, result); err != nil {
			return fmt.Errorf("failed to parse BFF response data: %w", err)
		}
	}

	return nil
}

// doRequestSSELogs makes an HTTP GET to a BFF SSE endpoint, parses the first
// SSE event, and extracts the "logs" field. Used by GetAppLogs.
func (p *GMIDeploymentProvider) doRequestSSELogs(ctx context.Context, method, url string) (string, error) {
	httpReq, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create HTTP request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.token)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("failed to call GMI API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("GMI API returned error status %d: %s", resp.StatusCode, string(body))
	}

	// Parse SSE events. Gin's c.SSEvent writes:
	//   event: <name>\ndata: <json>\n\n
	scanner := bufio.NewScanner(resp.Body)
	var eventType, dataStr string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event:") {
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			dataStr = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		} else if line == "" && dataStr != "" {
			// Empty line = end of SSE event
			break
		}
	}

	if dataStr == "" {
		return "", fmt.Errorf("no SSE event received from BFF")
	}

	var event struct {
		Logs  string `json:"logs"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(dataStr), &event); err != nil {
		return "", fmt.Errorf("failed to parse SSE data: %w (raw: %s)", err, dataStr)
	}

	if eventType == "error" || event.Error != "" {
		return "", fmt.Errorf("agent log error: %s", event.Error)
	}

	return event.Logs, nil
}

