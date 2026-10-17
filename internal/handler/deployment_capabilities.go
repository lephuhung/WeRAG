package handler

import (
	"net/http"

	"github.com/Tencent/WeKnora/internal/sandbox"
	"github.com/gin-gonic/gin"
)

// DeploymentCapabilityKeys is the canonical capability key list shared with
// frontend/src/config/deploymentCapabilities.ts — keep both in sync.
var DeploymentCapabilityKeys = []string{
	"organizations",
	"agents",
	"integrations.im",
	"integrations.embed",
	"integrations.api",
	"integrations.mcpserver",
	"settings.mcp",
	"settings.websearch",
	"settings.vectorstore",
	"settings.storage",
	"settings.sandbox",
	"settings.sandbox.docker",
}

// DeploymentCapability describes whether a deployment exposes a feature route.
type DeploymentCapability struct {
	Supported bool   `json:"supported"`
	Reason    string `json:"reason,omitempty"`
}

// DeploymentCapabilitiesData is returned by GET /system/capabilities.
type DeploymentCapabilitiesData struct {
	Edition      string                          `json:"edition"`
	Capabilities map[string]DeploymentCapability `json:"capabilities"`
}

// DeploymentFeatureAvailability mirrors injected backend handlers/services.
type DeploymentFeatureAvailability struct {
	Agents        bool
	IM            bool
	Embed         bool
	API           bool
	MCPServer     bool
	MCP           bool
	WebSearch     bool
	VectorStore   bool
	Storage       bool
	Sandbox       bool
	SandboxDocker bool
}

func supportedDeploymentCapability(supported bool) DeploymentCapability {
	if supported {
		return DeploymentCapability{Supported: true}
	}
	return DeploymentCapability{Supported: false, Reason: "route_not_registered"}
}

// BuildDeploymentCapabilities derives the deployment capability snapshot.
func BuildDeploymentCapabilities(
	edition string,
	available DeploymentFeatureAvailability,
) DeploymentCapabilitiesData {
	// Organizations were removed in favour of direct tenant-to-tenant KB
	// access grants — report the key as permanently unsupported so legacy
	// frontends hide the org UI instead of showing a broken screen.
	organizations := DeploymentCapability{Supported: false, Reason: "feature_removed"}

	sandboxDocker := DeploymentCapability{
		Supported: available.Sandbox && available.SandboxDocker,
	}
	if available.Sandbox && !available.SandboxDocker {
		sandboxDocker.Reason = "docker_backend_disabled"
	} else if !available.Sandbox {
		sandboxDocker.Reason = "route_not_registered"
	}

	return DeploymentCapabilitiesData{
		Edition: edition,
		Capabilities: map[string]DeploymentCapability{
			"organizations":           organizations,
			"agents":                  supportedDeploymentCapability(available.Agents),
			"integrations.im":         supportedDeploymentCapability(available.IM),
			"integrations.embed":      supportedDeploymentCapability(available.Embed),
			"integrations.api":        supportedDeploymentCapability(available.API),
			"integrations.mcpserver":  supportedDeploymentCapability(available.MCPServer),
			"settings.mcp":            supportedDeploymentCapability(available.MCP),
			"settings.websearch":      supportedDeploymentCapability(available.WebSearch),
			"settings.vectorstore":    supportedDeploymentCapability(available.VectorStore),
			"settings.storage":        supportedDeploymentCapability(available.Storage),
			"settings.sandbox":        supportedDeploymentCapability(available.Sandbox),
			"settings.sandbox.docker": sandboxDocker,
		},
	}
}

// BindDeploymentCapabilities stores the startup snapshot used by GetDeploymentCapabilities.
func (h *SystemHandler) BindDeploymentCapabilities(data DeploymentCapabilitiesData) {
	h.deploymentCapabilities = data
}

// GetDeploymentCapabilities godoc
// @Summary      Lấy danh sách năng lực của bản triển khai
// @Description  Trả về phiên bản triển khai hiện tại và các năng lực tương ứng với route backend đã đăng ký thật; chỉ supported=false mới có nghĩa là nên ẩn lối vào
// @Tags         Hệ thống
// @Produce      json
// @Success      200  {object}  map[string]interface{}  "Gói chuẩn code/msg/data, data là DeploymentCapabilitiesData"
// @Router       /system/capabilities [get]
func (h *SystemHandler) GetDeploymentCapabilities(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "success",
		"data": overlayLiveDockerSandboxCapability(h.deploymentCapabilities),
	})
}

// overlayLiveDockerSandboxCapability replaces the startup snapshot's Docker
// flag with the live 3-tier value so a System Settings toggle is visible
// without restarting the process.
func overlayLiveDockerSandboxCapability(data DeploymentCapabilitiesData) DeploymentCapabilitiesData {
	caps := make(map[string]DeploymentCapability, len(data.Capabilities))
	for key, capability := range data.Capabilities {
		caps[key] = capability
	}
	sandboxCap := caps["settings.sandbox"]
	docker := DeploymentCapability{
		Supported: sandboxCap.Supported && sandbox.DockerBackendEnabled(),
	}
	if sandboxCap.Supported && !docker.Supported {
		docker.Reason = "docker_backend_disabled"
	} else if !sandboxCap.Supported {
		docker.Reason = "route_not_registered"
	}
	caps["settings.sandbox.docker"] = docker
	data.Capabilities = caps
	return data
}
