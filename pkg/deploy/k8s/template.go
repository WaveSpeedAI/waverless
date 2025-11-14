package k8s

import (
	"bytes"
	"fmt"
	"os"
	"text/template"
)

// TemplateRenderer 模板渲染器
type TemplateRenderer struct {
	templateDir string
}

// NewTemplateRenderer 创建模板渲染器
func NewTemplateRenderer(templateDir string) *TemplateRenderer {
	return &TemplateRenderer{
		templateDir: templateDir,
	}
}

// RenderContext 渲染上下文（简化版，只保留必要字段）
type RenderContext struct {
	// Core variables
	Endpoint      string `json:"endpoint"`      // Endpoint name (used for app name, labels, environment variables)
	Namespace     string `json:"namespace"`     // K8s namespace
	Image         string `json:"image"`         // Docker 镜像
	Replicas      int    `json:"replicas"`      // 副本数
	ContainerName string `json:"containerName"` // Container name
	ContainerPort int32  `json:"containerPort"` // Container port
	ProxyPort     int32  `json:"proxyPort"`     // Proxy port

	// 资源配置（从 Spec 中来）
	IsGpu         bool   `json:"isGpu"`
	GpuCount      int    `json:"gpuCount"`
	CpuLimit      string `json:"cpuLimit"`
	MemoryRequest string `json:"memoryRequest"`

	// K8s 调度配置（从 Spec 中来）
	NodeSelector map[string]string `json:"nodeSelector"`
	Tolerations  []Toleration      `json:"tolerations"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`

	// 平台配置追踪（用于记录到 Deployment annotations）
	PlatformLabelsJSON      string `json:"platformLabelsJSON,omitempty"`      // 平台labels的JSON记录
	PlatformAnnotationsJSON string `json:"platformAnnotationsJSON,omitempty"` // 平台annotations的JSON记录

	// 优雅关闭配置
	TaskTimeout                    int   `json:"taskTimeout"`                    // 任务超时时间（秒），用于计算terminationGracePeriodSeconds
	TerminationGracePeriodSeconds int64 `json:"terminationGracePeriodSeconds"` // Pod优雅关闭时间（秒）
}

// Render 渲染模板
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
