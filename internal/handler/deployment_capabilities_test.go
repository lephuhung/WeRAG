package handler

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/sandbox"
)

func TestBuildDeploymentCapabilitiesIncludesAllKeys(t *testing.T) {
	result := BuildDeploymentCapabilities("standard", DeploymentFeatureAvailability{
		Agents:      true,
		IM:          true,
		Embed:       true,
		API:         true,
		MCP:         true,
		WebSearch:   true,
		VectorStore: true,
		Storage:     true,
		Sandbox:     true,
	})

	for _, key := range DeploymentCapabilityKeys {
		if _, ok := result.Capabilities[key]; !ok {
			t.Fatalf("missing capability key %q", key)
		}
	}
}

func TestOverlayLiveDockerSandboxCapabilityIgnoresStartupSnapshot(t *testing.T) {
	sandbox.ClearDockerBackendEnabledOverride()
	t.Cleanup(sandbox.ClearDockerBackendEnabledOverride)
	t.Setenv(sandbox.DockerBackendEnabledEnv, "")

	snapshot := BuildDeploymentCapabilities("standard", DeploymentFeatureAvailability{
		Sandbox:       true,
		SandboxDocker: true,
	})
	live := overlayLiveDockerSandboxCapability(snapshot)
	docker := live.Capabilities["settings.sandbox.docker"]
	if docker.Supported {
		t.Fatal("live env off must hide docker even if the startup snapshot was on")
	}
	if docker.Reason != "docker_backend_disabled" {
		t.Fatalf("reason = %q, want docker_backend_disabled", docker.Reason)
	}

	t.Setenv(sandbox.DockerBackendEnabledEnv, "true")
	enabled := overlayLiveDockerSandboxCapability(snapshot)
	if !enabled.Capabilities["settings.sandbox.docker"].Supported {
		t.Fatal("live env true must expose docker")
	}
}
