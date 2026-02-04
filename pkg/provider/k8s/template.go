package k8s

import (
	"bytes"
	"fmt"
	"os"
	"text/template"
)

// TemplateRenderer renders deployment templates
type TemplateRenderer struct {
	templateDir string
}

// NewTemplateRenderer creates a new template renderer
func NewTemplateRenderer(templateDir string) *TemplateRenderer {
	return &TemplateRenderer{
		templateDir: templateDir,
	}
}

// RenderContext defines the rendering context (simplified, only essential fields)
type RenderContext struct {
	// Core variables
	Endpoint      string `json:"endpoint"`      // Endpoint name (used for app name, labels, environment variables)
	Namespace     string `json:"namespace"`     // K8s namespace
	Image         string `json:"image"`         // Docker image
	Replicas      int    `json:"replicas"`      // Replica count
	ContainerName string `json:"containerName"` // Container name
	ContainerPort int32  `json:"containerPort"` // Container port
	ProxyPort     int32  `json:"proxyPort"`     // Proxy port

	// Resource configuration (from Spec)
	IsGpu         bool   `json:"isGpu"`
	GpuCount      int    `json:"gpuCount"`
	CpuLimit      string `json:"cpuLimit"`
	MemoryRequest string `json:"memoryRequest"`

	// K8s scheduling configuration (from Spec)
	NodeSelector map[string]string `json:"nodeSelector"`
	Tolerations  []Toleration      `json:"tolerations"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`

	// Storage configuration
	Volumes      []VolumeInfo      `json:"volumes,omitempty"`
	VolumeMounts []VolumeMountInfo `json:"volumeMounts,omitempty"`
	ShmSize      string            `json:"shmSize,omitempty"` // Shared memory size (e.g., "1Gi", "512Mi")

	// Security configuration
	EnablePtrace bool `json:"enablePtrace,omitempty"` // Enable SYS_PTRACE capability for debugging

	// Environment variable configuration
	Env map[string]string `json:"env,omitempty"` // Custom environment variables

	// Image pull secret for private registries
	ImagePullSecret string `json:"imagePullSecret,omitempty"` // Additional image pull secret name

	// Platform configuration tracking (for recording in Deployment annotations)
	PlatformLabelsJSON      string `json:"platformLabelsJSON,omitempty"`      // Platform labels JSON record
	PlatformAnnotationsJSON string `json:"platformAnnotationsJSON,omitempty"` // Platform annotations JSON record

	// Graceful shutdown configuration
	TaskTimeout                   int   `json:"taskTimeout"`                   // Task timeout (seconds), used to calculate terminationGracePeriodSeconds
	TerminationGracePeriodSeconds int64 `json:"terminationGracePeriodSeconds"` // Pod graceful shutdown time (seconds)
}

// VolumeInfo PVC volume info for template rendering
type VolumeInfo struct {
	Name    string `json:"name"`
	PVCName string `json:"pvcName"`
}

// VolumeMountInfo volume mount info for template rendering
type VolumeMountInfo struct {
	Name      string `json:"name"`
	MountPath string `json:"mountPath"`
}

// Render renders a template
func (r *TemplateRenderer) Render(templateName string, ctx *RenderContext) (string, error) {
	templatePath := fmt.Sprintf("%s/%s", r.templateDir, templateName)

	// Read template file
	templateContent, err := os.ReadFile(templatePath)
	if err != nil {
		return "", fmt.Errorf("failed to read template file: %v", err)
	}

	// Create template
	tmpl, err := template.New(templateName).Parse(string(templateContent))
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %v", err)
	}

	// Render template
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, ctx); err != nil {
		return "", fmt.Errorf("failed to execute template: %v", err)
	}

	return buf.String(), nil
}
