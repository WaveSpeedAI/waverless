// Package image provides image validation functionality for the Waverless platform.
// It validates image reference formats and checks image existence in registries.
package image

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"waverless/pkg/interfaces"

	"github.com/go-redis/redis/v8"
)

// ImageValidator validates container image references.
// It implements the interfaces.ImageValidator interface.
type ImageValidator struct {
	httpClient *http.Client
	cache      *ImageValidationCache
	config     *ImageValidationConfig
}

// ImageValidationConfig contains configuration for image validation.
type ImageValidationConfig struct {
	// Enabled indicates whether image validation is enabled
	Enabled bool `yaml:"enabled"`
	// Timeout is the timeout for validation requests (default: 30s)
	Timeout time.Duration `yaml:"timeout"`
	// CacheDuration is how long to cache validation results (default: 1h)
	CacheDuration time.Duration `yaml:"cacheDuration"`
	// SkipOnTimeout indicates whether to proceed with a warning when validation times out (default: true)
	SkipOnTimeout bool `yaml:"skipOnTimeout"`
}

// DefaultImageValidationConfig returns the default configuration for image validation.
func DefaultImageValidationConfig() *ImageValidationConfig {
	return &ImageValidationConfig{
		Enabled:       true,
		Timeout:       30 * time.Second,
		CacheDuration: 1 * time.Hour,
		SkipOnTimeout: true,
	}
}

// NewImageValidator creates a new ImageValidator with the given configuration.
func NewImageValidator(config *ImageValidationConfig) *ImageValidator {
	if config == nil {
		config = DefaultImageValidationConfig()
	}

	cacheConfig := &CacheConfig{
		DefaultTTL: config.CacheDuration,
	}

	return &ImageValidator{
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
		cache:  NewImageValidationCacheWithConfig(cacheConfig),
		config: config,
	}
}

// WithRedisCache configures the validator to use Redis for caching.
// The in-memory cache is used as a fallback when Redis is unavailable.
// Returns the validator instance for method chaining.
//
// Example:
//
//	validator := NewImageValidator(config).WithRedisCache(redisClient)
func (v *ImageValidator) WithRedisCache(client interface{ GetClient() interface{} }) *ImageValidator {
	// Type assert to get the actual redis.Client
	// This allows accepting the RedisClient wrapper from pkg/store/redis
	if rc, ok := client.GetClient().(*redis.Client); ok {
		v.cache.WithRedis(rc)
	}
	return v
}

// WithRedisClient configures the validator to use a Redis client directly for caching.
// The in-memory cache is used as a fallback when Redis is unavailable.
// Returns the validator instance for method chaining.
func (v *ImageValidator) WithRedisClient(client *redis.Client) *ImageValidator {
	v.cache.WithRedis(client)
	return v
}

// imageReferenceRegex validates image reference format.
// Supports:
// - Simple names: nginx, ubuntu
// - With tag: nginx:latest, nginx:1.0
// - With digest: nginx@sha256:abc123...
// - With namespace: library/nginx, user/repo
// - With registry: gcr.io/project/image, registry.example.com/image:tag
// - AWS ECR: 123456789.dkr.ecr.us-east-1.amazonaws.com/repo:tag
//
// The regex is based on the Docker image reference specification:
// [registry/][namespace/]repository[:tag][@digest]
var (
	// Registry pattern: optional registry with port
	// Examples: gcr.io, registry.example.com:5000, 123456789.dkr.ecr.us-east-1.amazonaws.com
	registryPattern = `(?:(?:[a-zA-Z0-9](?:[a-zA-Z0-9-]*[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}(?::\d+)?|localhost(?::\d+)?|\d+\.\d+\.\d+\.\d+(?::\d+)?)`

	// Repository component pattern: alphanumeric with optional separators (-, _, .)
	// Must start and end with alphanumeric
	componentPattern = `[a-z0-9]+(?:(?:[._]|__|[-]*)[a-z0-9]+)*`

	// Tag pattern: alphanumeric with optional separators (-, _, .)
	// Max 128 characters
	tagPattern = `[a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}`

	// Digest pattern: algorithm:hex
	digestPattern = `[a-zA-Z][a-zA-Z0-9]*:[a-fA-F0-9]{32,}`

	// Full image reference regex
	// Format: [registry/][namespace/]repository[:tag][@digest]
	imageReferenceRegex = regexp.MustCompile(
		`^` +
			// Optional registry (with trailing /)
			`(?:` + registryPattern + `/)?` +
			// Repository path (one or more components separated by /)
			`(?:` + componentPattern + `/)*` + componentPattern +
			// Optional tag
			`(?::` + tagPattern + `)?` +
			// Optional digest
			`(?:@` + digestPattern + `)?` +
			`$`,
	)

	// Simple validation for common cases - more permissive
	// This catches obvious errors while allowing valid references
	simpleImageRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._/:@-]*$`)
)

// ValidateImageFormat validates the format of an image reference.
// It supports common registry formats including:
// - Docker Hub: nginx, nginx:latest, library/nginx:1.0, user/repo:tag
// - Private registries: registry.example.com/image:tag
// - Cloud registries: gcr.io/project/image:tag, 123456789.dkr.ecr.us-east-1.amazonaws.com/repo:tag
//
// Returns nil if the format is valid, otherwise returns an error with a descriptive message.
//
// Validates: Requirements 1.1, 1.2, 1.3
func (v *ImageValidator) ValidateImageFormat(image string) error {
	// Check for empty string
	if image == "" {
		return fmt.Errorf("image name cannot be empty")
	}

	// Check for whitespace
	if strings.TrimSpace(image) != image {
		return fmt.Errorf("image name cannot contain leading or trailing whitespace")
	}

	// Check for invalid characters
	if !simpleImageRegex.MatchString(image) {
		return fmt.Errorf("image name contains invalid characters, only letters, numbers, dots (.), colons (:), slashes (/), underscores (_), hyphens (-) and @ are allowed")
	}

	// Parse and validate the image reference
	if err := validateImageReference(image); err != nil {
		return err
	}

	return nil
}

// validateImageReference performs detailed validation of an image reference.
// It parses the reference into its components and validates each part.
func validateImageReference(image string) error {
	// Split by @ to separate digest
	var mainPart, digest string
	if idx := strings.LastIndex(image, "@"); idx != -1 {
		mainPart = image[:idx]
		digest = image[idx+1:]

		// Validate digest format
		if err := validateDigest(digest); err != nil {
			return err
		}
	} else {
		mainPart = image
	}

	// Split by : to separate tag (but be careful with registry ports)
	var namePart, tag string
	if idx := strings.LastIndex(mainPart, ":"); idx != -1 {
		// Check if this is a port number (part of registry) or a tag
		afterColon := mainPart[idx+1:]
		beforeColon := mainPart[:idx]

		// If there's a / after the colon position, it's likely a port
		// e.g., registry.example.com:5000/image
		if strings.Contains(afterColon, "/") {
			namePart = mainPart
		} else {
			// It's a tag
			namePart = beforeColon
			tag = afterColon

			// Validate tag format
			if err := validateTag(tag); err != nil {
				return err
			}
		}
	} else {
		namePart = mainPart
	}

	// Validate the name part (registry/namespace/repository)
	if err := validateNamePart(namePart); err != nil {
		return err
	}

	return nil
}

// validateDigest validates a digest string (e.g., sha256:abc123...)
func validateDigest(digest string) error {
	if digest == "" {
		return fmt.Errorf("image digest cannot be empty")
	}

	// Digest format: algorithm:hex
	parts := strings.SplitN(digest, ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid image digest format, should be algorithm:hex (e.g., sha256:abc123...)")
	}

	algorithm := parts[0]
	hex := parts[1]

	// Validate algorithm (must start with letter, alphanumeric)
	if !regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9]*$`).MatchString(algorithm) {
		return fmt.Errorf("invalid image digest algorithm: %s", algorithm)
	}

	// Validate hex (must be at least 32 hex characters)
	if len(hex) < 32 {
		return fmt.Errorf("image digest hash is too short, at least 32 characters required")
	}
	if !regexp.MustCompile(`^[a-fA-F0-9]+$`).MatchString(hex) {
		return fmt.Errorf("image digest hash contains invalid characters, only hexadecimal characters allowed")
	}

	return nil
}

// validateTag validates a tag string
func validateTag(tag string) error {
	if tag == "" {
		return fmt.Errorf("image tag cannot be empty")
	}

	// Tag max length is 128 characters
	if len(tag) > 128 {
		return fmt.Errorf("image tag is too long, maximum length is 128 characters")
	}

	// Tag must start with alphanumeric or underscore
	if !regexp.MustCompile(`^[a-zA-Z0-9_]`).MatchString(tag) {
		return fmt.Errorf("image tag must start with a letter, number, or underscore")
	}

	// Tag can only contain alphanumeric, underscore, period, and hyphen
	if !regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9._-]*$`).MatchString(tag) {
		return fmt.Errorf("image tag contains invalid characters, only letters, numbers, underscores (_), dots (.) and hyphens (-) are allowed")
	}

	return nil
}

// validateNamePart validates the name part of an image reference (registry/namespace/repository)
func validateNamePart(name string) error {
	if name == "" {
		return fmt.Errorf("image repository name cannot be empty")
	}

	// Split by / to get components
	parts := strings.Split(name, "/")

	// Check total length
	if len(name) > 255 {
		return fmt.Errorf("image name is too long, maximum length is 255 characters")
	}

	// Validate each component
	for i, part := range parts {
		if part == "" {
			return fmt.Errorf("image name contains empty path component")
		}

		// First component might be a registry (contains . or :)
		if i == 0 && (strings.Contains(part, ".") || strings.Contains(part, ":")) {
			if err := validateRegistry(part); err != nil {
				return err
			}
		} else {
			// Repository component
			if err := validateRepositoryComponent(part); err != nil {
				return err
			}
		}
	}

	return nil
}

// validateRegistry validates a registry hostname
func validateRegistry(registry string) error {
	// Remove port if present
	host := registry
	if idx := strings.LastIndex(registry, ":"); idx != -1 {
		host = registry[:idx]
		port := registry[idx+1:]

		// Validate port
		if !regexp.MustCompile(`^\d+$`).MatchString(port) {
			return fmt.Errorf("invalid registry port: %s", port)
		}
	}

	// Validate hostname
	// Can be: domain name, IP address, or localhost
	if host == "localhost" {
		return nil
	}

	// IP address
	if regexp.MustCompile(`^\d+\.\d+\.\d+\.\d+$`).MatchString(host) {
		return nil
	}

	// Domain name
	if !regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?)*$`).MatchString(host) {
		return fmt.Errorf("invalid registry address format: %s", host)
	}

	return nil
}

// validateRepositoryComponent validates a single component of a repository path
func validateRepositoryComponent(component string) error {
	if component == "" {
		return fmt.Errorf("image repository path component cannot be empty")
	}

	// Component must be lowercase
	if strings.ToLower(component) != component {
		return fmt.Errorf("image repository path must be lowercase: %s", component)
	}

	// Component must start and end with alphanumeric
	if !regexp.MustCompile(`^[a-z0-9]`).MatchString(component) {
		return fmt.Errorf("image repository path component must start with lowercase letter or number: %s", component)
	}
	if !regexp.MustCompile(`[a-z0-9]$`).MatchString(component) {
		return fmt.Errorf("image repository path component must end with lowercase letter or number: %s", component)
	}

	// Component can only contain lowercase alphanumeric, underscore, period, and hyphen
	// But separators cannot be consecutive (except __ which is allowed)
	if !regexp.MustCompile(`^[a-z0-9]+(?:(?:[._]|__|[-]+)[a-z0-9]+)*$`).MatchString(component) {
		return fmt.Errorf("invalid image repository path component format: %s", component)
	}

	return nil
}

// CheckImageExists checks if an image exists in the registry.
// It uses Docker Registry HTTP API V2 to verify image existence.
//
// Implementation:
// 1. Check cache first for previously validated images
// 2. Parse image reference to extract registry, repository, and tag
// 3. For Docker Hub: call https://registry-1.docker.io/v2/{repo}/manifests/{tag}
// 4. For private registries: call https://{registry}/v2/{repo}/manifests/{tag}
// 5. Handle authentication if needed (Bearer Token flow)
//
// Authentication flow (Docker Hub example):
// 1. Request manifest, if 401 returned, WWW-Authenticate header contains token service URL
// 2. Parse header to get token service address
// 3. Request token with user credentials
// 4. Retry manifest request with token
//
// Validates: Requirements 2.1, 2.2, 2.3
func (v *ImageValidator) CheckImageExists(ctx context.Context, image string, cred *interfaces.RegistryCredential) (*interfaces.ImageValidationResult, error) {
	// First validate the format
	if err := v.ValidateImageFormat(image); err != nil {
		return &interfaces.ImageValidationResult{
			Valid:     false,
			Exists:    false,
			Error:     err.Error(),
			CheckedAt: time.Now(),
		}, nil
	}

	// Check cache first
	if cached := v.cache.Get(image); cached != nil {
		return cached, nil
	}

	// Parse image reference
	ref, err := parseImageReference(image)
	if err != nil {
		return &interfaces.ImageValidationResult{
			Valid:     false,
			Exists:    false,
			Error:     fmt.Sprintf("invalid image name format: %s", err.Error()),
			CheckedAt: time.Now(),
		}, nil
	}

	// Build manifest URL
	manifestURL := buildManifestURL(ref)

	// Check manifest with optional authentication
	result := v.checkManifest(ctx, manifestURL, ref, cred)

	// Cache successful results
	if result.Valid && result.Exists && result.Accessible {
		v.cache.Set(image, result, v.config.CacheDuration)
	}

	return result, nil
}

// imageReference represents a parsed image reference
type imageReference struct {
	Registry    string // e.g., "registry-1.docker.io", "gcr.io"
	Repository  string // e.g., "library/nginx", "project/image"
	Tag         string // e.g., "latest", "1.0"
	Digest      string // e.g., "sha256:abc123..."
	IsDockerHub bool   // true if this is a Docker Hub image
}

// parseImageReference parses an image string into its components
func parseImageReference(image string) (*imageReference, error) {
	ref := &imageReference{
		Tag: "latest", // default tag
	}

	// Handle digest
	if idx := strings.LastIndex(image, "@"); idx != -1 {
		ref.Digest = image[idx+1:]
		image = image[:idx]
	}

	// Handle tag
	if idx := strings.LastIndex(image, ":"); idx != -1 {
		// Check if this is a port (part of registry) or a tag
		afterColon := image[idx+1:]
		if !strings.Contains(afterColon, "/") {
			ref.Tag = afterColon
			image = image[:idx]
		}
	}

	// Parse registry and repository
	parts := strings.Split(image, "/")

	// Determine if first part is a registry
	if len(parts) == 1 {
		// Simple name like "nginx" -> Docker Hub library image
		ref.Registry = "registry-1.docker.io"
		ref.Repository = "library/" + parts[0]
		ref.IsDockerHub = true
	} else if len(parts) == 2 {
		// Could be "user/repo" (Docker Hub) or "registry.io/repo"
		if isRegistry(parts[0]) {
			ref.Registry = parts[0]
			ref.Repository = parts[1]
			ref.IsDockerHub = false
		} else {
			// Docker Hub user/repo
			ref.Registry = "registry-1.docker.io"
			ref.Repository = image
			ref.IsDockerHub = true
		}
	} else {
		// Has explicit registry: "registry.io/namespace/repo"
		ref.Registry = parts[0]
		ref.Repository = strings.Join(parts[1:], "/")
		ref.IsDockerHub = parts[0] == "docker.io" || parts[0] == "index.docker.io"
		if ref.IsDockerHub {
			ref.Registry = "registry-1.docker.io"
		}
	}

	// Normalize Docker Hub registry
	if ref.Registry == "docker.io" || ref.Registry == "index.docker.io" {
		ref.Registry = "registry-1.docker.io"
		ref.IsDockerHub = true
	}

	return ref, nil
}

// isRegistry checks if a string looks like a registry hostname
func isRegistry(s string) bool {
	// Contains a dot (domain) or colon (port) or is localhost
	return strings.Contains(s, ".") || strings.Contains(s, ":") || s == "localhost"
}

// buildManifestURL builds the manifest URL for the given image reference
func buildManifestURL(ref *imageReference) string {
	// Use digest if available, otherwise use tag
	reference := ref.Tag
	if ref.Digest != "" {
		reference = ref.Digest
	}

	// Determine scheme - use HTTP for localhost/IP addresses (typically test servers)
	scheme := "https"
	host := ref.Registry
	if strings.Contains(host, ":") {
		// Remove port for localhost check
		hostWithoutPort := strings.Split(host, ":")[0]
		if hostWithoutPort == "localhost" || hostWithoutPort == "127.0.0.1" {
			scheme = "http"
		}
	} else if host == "localhost" || host == "127.0.0.1" {
		scheme = "http"
	}

	return fmt.Sprintf("%s://%s/v2/%s/manifests/%s", scheme, ref.Registry, ref.Repository, reference)
}

// checkManifest checks if the manifest exists, handling authentication
func (v *ImageValidator) checkManifest(ctx context.Context, manifestURL string, ref *imageReference, cred *interfaces.RegistryCredential) *interfaces.ImageValidationResult {
	req, err := http.NewRequestWithContext(ctx, "HEAD", manifestURL, nil)
	if err != nil {
		return &interfaces.ImageValidationResult{
			Valid:     true,
			Exists:    false,
			Error:     "Failed to create request",
			CheckedAt: time.Now(),
		}
	}

	// Set Accept headers for manifest types
	req.Header.Set("Accept", strings.Join([]string{
		"application/vnd.docker.distribution.manifest.v2+json",
		"application/vnd.docker.distribution.manifest.list.v2+json",
		"application/vnd.oci.image.manifest.v1+json",
		"application/vnd.oci.image.index.v1+json",
	}, ", "))

	resp, err := v.httpClient.Do(req)
	if err != nil {
		// Check for timeout or other network errors
		// When SkipOnTimeout is true, we also skip on network errors to avoid blocking deployment
		if ctx.Err() == context.DeadlineExceeded {
			if v.config.SkipOnTimeout {
				return &interfaces.ImageValidationResult{
					Valid:     true,
					Exists:    false,
					Warning:   "Registry connection timeout, will verify during actual pull",
					CheckedAt: time.Now(),
				}
			}
			return &interfaces.ImageValidationResult{
				Valid:     true,
				Exists:    false,
				Error:     "Registry connection timeout",
				CheckedAt: time.Now(),
			}
		}
		// For other network errors (connection refused, DNS failure, etc.)
		// If SkipOnTimeout is true, we also skip these errors to avoid blocking deployment
		if v.config.SkipOnTimeout {
			return &interfaces.ImageValidationResult{
				Valid:     true,
				Exists:    false,
				Warning:   "Cannot connect to registry, will verify during actual pull",
				CheckedAt: time.Now(),
			}
		}
		return &interfaces.ImageValidationResult{
			Valid:     true,
			Exists:    false,
			Error:     "Cannot connect to registry",
			Warning:   "Please check network connection or registry address",
			CheckedAt: time.Now(),
		}
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return &interfaces.ImageValidationResult{
			Valid:      true,
			Exists:     true,
			Accessible: true,
			CheckedAt:  time.Now(),
		}

	case http.StatusUnauthorized:
		// Need authentication
		wwwAuth := resp.Header.Get("WWW-Authenticate")
		if wwwAuth == "" {
			return &interfaces.ImageValidationResult{
				Valid:      true,
				Exists:     true,
				Accessible: false,
				Error:      "Image requires authentication to access",
				Warning:    "Please provide registry credentials",
				CheckedAt:  time.Now(),
			}
		}

		// Try with authentication
		return v.checkManifestWithAuth(ctx, manifestURL, wwwAuth, ref, cred)

	case http.StatusNotFound:
		return &interfaces.ImageValidationResult{
			Valid:     true,
			Exists:    false,
			Error:     "Image not found, please check the image name",
			CheckedAt: time.Now(),
		}

	case http.StatusForbidden:
		return &interfaces.ImageValidationResult{
			Valid:      true,
			Exists:     true,
			Accessible: false,
			Error:      "Access denied, please check account permissions",
			CheckedAt:  time.Now(),
		}

	default:
		return &interfaces.ImageValidationResult{
			Valid:     true,
			Exists:    false,
			Error:     fmt.Sprintf("Registry returned error: %d", resp.StatusCode),
			CheckedAt: time.Now(),
		}
	}
}

// checkManifestWithAuth handles authenticated manifest check
func (v *ImageValidator) checkManifestWithAuth(ctx context.Context, manifestURL, wwwAuth string, ref *imageReference, cred *interfaces.RegistryCredential) *interfaces.ImageValidationResult {
	// Parse WWW-Authenticate header
	authInfo, err := parseWWWAuthenticate(wwwAuth)
	if err != nil {
		return &interfaces.ImageValidationResult{
			Valid:      true,
			Exists:     true,
			Accessible: false,
			Error:      "Failed to parse authentication info",
			CheckedAt:  time.Now(),
		}
	}

	// Get authentication token
	token, err := v.getAuthToken(ctx, authInfo, ref, cred)
	if err != nil {
		return &interfaces.ImageValidationResult{
			Valid:      true,
			Exists:     true,
			Accessible: false,
			Error:      "Authentication failed, please check username and password",
			CheckedAt:  time.Now(),
		}
	}

	// Retry with token
	req, err := http.NewRequestWithContext(ctx, "HEAD", manifestURL, nil)
	if err != nil {
		return &interfaces.ImageValidationResult{
			Valid:     true,
			Exists:    false,
			Error:     "Failed to create request",
			CheckedAt: time.Now(),
		}
	}

	req.Header.Set("Accept", strings.Join([]string{
		"application/vnd.docker.distribution.manifest.v2+json",
		"application/vnd.docker.distribution.manifest.list.v2+json",
		"application/vnd.oci.image.manifest.v1+json",
		"application/vnd.oci.image.index.v1+json",
	}, ", "))
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return &interfaces.ImageValidationResult{
			Valid:     true,
			Exists:    false,
			Error:     "Request failed",
			CheckedAt: time.Now(),
		}
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return &interfaces.ImageValidationResult{
			Valid:      true,
			Exists:     true,
			Accessible: true,
			CheckedAt:  time.Now(),
		}

	case http.StatusUnauthorized:
		return &interfaces.ImageValidationResult{
			Valid:      true,
			Exists:     true,
			Accessible: false,
			Error:      "Authentication failed, please check username and password",
			CheckedAt:  time.Now(),
		}

	case http.StatusForbidden:
		return &interfaces.ImageValidationResult{
			Valid:      true,
			Exists:     true,
			Accessible: false,
			Error:      "Access denied, please check account permissions",
			CheckedAt:  time.Now(),
		}

	case http.StatusNotFound:
		return &interfaces.ImageValidationResult{
			Valid:     true,
			Exists:    false,
			Error:     "Image not found, please check the image name",
			CheckedAt: time.Now(),
		}

	default:
		return &interfaces.ImageValidationResult{
			Valid:     true,
			Exists:    false,
			Error:     fmt.Sprintf("Registry returned error: %d", resp.StatusCode),
			CheckedAt: time.Now(),
		}
	}
}

// authInfo contains parsed WWW-Authenticate header information
type authInfo struct {
	Realm   string // Token service URL
	Service string // Service name
	Scope   string // Scope (e.g., "repository:library/nginx:pull")
}

// parseWWWAuthenticate parses the WWW-Authenticate header
// Format: Bearer realm="https://auth.docker.io/token",service="registry.docker.io",scope="repository:library/nginx:pull"
func parseWWWAuthenticate(header string) (*authInfo, error) {
	if !strings.HasPrefix(strings.ToLower(header), "bearer ") {
		return nil, fmt.Errorf("unsupported auth type: %s", header)
	}

	info := &authInfo{}
	params := header[7:] // Remove "Bearer " prefix

	// Parse key="value" pairs
	// Use regex to handle quoted values properly
	re := regexp.MustCompile(`(\w+)="([^"]*)"`)
	matches := re.FindAllStringSubmatch(params, -1)

	for _, match := range matches {
		if len(match) == 3 {
			key := strings.ToLower(match[1])
			value := match[2]
			switch key {
			case "realm":
				info.Realm = value
			case "service":
				info.Service = value
			case "scope":
				info.Scope = value
			}
		}
	}

	if info.Realm == "" {
		return nil, fmt.Errorf("missing realm in WWW-Authenticate header")
	}

	return info, nil
}

// tokenResponse represents the response from a token service
type tokenResponse struct {
	Token       string `json:"token"`
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

// getAuthToken gets an authentication token from the token service
func (v *ImageValidator) getAuthToken(ctx context.Context, auth *authInfo, ref *imageReference, cred *interfaces.RegistryCredential) (string, error) {
	// Build token request URL
	tokenURL, err := url.Parse(auth.Realm)
	if err != nil {
		return "", fmt.Errorf("invalid realm URL: %w", err)
	}

	// Add query parameters
	q := tokenURL.Query()
	if auth.Service != "" {
		q.Set("service", auth.Service)
	}

	// Build scope if not provided
	scope := auth.Scope
	if scope == "" {
		scope = fmt.Sprintf("repository:%s:pull", ref.Repository)
	}
	q.Set("scope", scope)

	tokenURL.RawQuery = q.Encode()

	// Create request
	req, err := http.NewRequestWithContext(ctx, "GET", tokenURL.String(), nil)
	if err != nil {
		return "", fmt.Errorf("failed to create token request: %w", err)
	}

	// Add Basic Auth if credentials provided
	if cred != nil && cred.Username != "" && cred.Password != "" {
		auth := base64.StdEncoding.EncodeToString([]byte(cred.Username + ":" + cred.Password))
		req.Header.Set("Authorization", "Basic "+auth)
	}

	// Make request
	resp, err := v.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token request returned status %d", resp.StatusCode)
	}

	// Parse response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read token response: %w", err)
	}

	var tokenResp tokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("failed to parse token response: %w", err)
	}

	// Return token (prefer "token" field, fall back to "access_token")
	if tokenResp.Token != "" {
		return tokenResp.Token, nil
	}
	if tokenResp.AccessToken != "" {
		return tokenResp.AccessToken, nil
	}

	return "", fmt.Errorf("no token in response")
}

// Ensure ImageValidator implements interfaces.ImageValidator
var _ interfaces.ImageValidator = (*ImageValidator)(nil)
