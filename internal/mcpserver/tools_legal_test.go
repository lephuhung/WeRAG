package mcpserver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

// fakeLegalCatalog answers Lookup with one replaced document; the embedded
// interface leaves the other methods nil (never called here).
type fakeLegalCatalog struct {
	interfaces.LegalCatalogService
	lastNumber string
}

func (f *fakeLegalCatalog) Lookup(_ context.Context, number, _ string, _ int) ([]*types.LegalStatusReport, error) {
	f.lastNumber = number
	issued := time.Date(2018, 11, 15, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	old := &types.LegalDocument{ID: "1", DocumentNumber: "29/2018/QH14", Title: "Luật Bảo vệ bí mật nhà nước",
		DocType: "Luật", IssuedOn: &issued, EffectiveOn: &issued, Status: "Còn hiệu lực"}
	repl := &types.LegalDocument{ID: "2", DocumentNumber: "117/2025/QH15", Title: "Luật Bảo vệ bí mật nhà nước",
		DocType: "Luật", IssuedOn: &newer, EffectiveOn: &newer, Status: "Còn hiệu lực"}
	return []*types.LegalStatusReport{{
		Document: old,
		Links:    []types.LegalRelationLink{{Relation: types.LegalRelationReplaces, Incoming: true, Other: repl}},
	}}, nil
}

func newLegalTestEngine(t *testing.T, ep *types.MCPEndpoint, catalog interfaces.LegalCatalogService) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	repo := &recordingEndpointRepo{touched: make(chan string, 8)}
	srv := NewServer(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, repo, nil, nil, nil, nil, catalog, nil, nil)
	r := gin.New()
	r.POST("/mcp/:endpoint_id", func(c *gin.Context) {
		ctx := context.WithValue(c.Request.Context(), types.MCPEndpointContextKey, ep)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}, gin.WrapH(srv.Handler()))
	return r
}

func TestLegalToolsListedWhenEnabled(t *testing.T) {
	ep := &types.MCPEndpoint{ID: "ep-1", TenantID: 1, Enabled: true, RateLimitPerMinute: 100,
		Tools: types.StringArray{types.MCPEndpointToolLegalCheckStatus, types.MCPEndpointToolLegalExpiryAudit}}
	names := toolNames(t, rpc(t, newLegalTestEngine(t, ep, &fakeLegalCatalog{}), "tools/list", map[string]any{}))
	if len(names) != 2 {
		t.Fatalf("tools = %v", names)
	}
}

func TestLegalCheckStatusReportsDerivedExpiry(t *testing.T) {
	ep := &types.MCPEndpoint{ID: "ep-1", TenantID: 1, Enabled: true, RateLimitPerMinute: 100,
		Tools: types.StringArray{types.MCPEndpointToolLegalCheckStatus}}
	catalog := &fakeLegalCatalog{}
	resp := rpc(t, newLegalTestEngine(t, ep, catalog), "tools/call", map[string]any{
		"name": types.MCPEndpointToolLegalCheckStatus, "arguments": map[string]any{"document_number": " 29/2018/QH14 "},
	})
	if catalog.lastNumber != "29/2018/QH14" {
		t.Fatalf("number passed = %q", catalog.lastNumber)
	}
	result, _ := resp["result"].(map[string]any)
	if isErr, _ := result["isError"].(bool); isErr {
		t.Fatalf("unexpected error result: %v", result)
	}
	content, _ := result["content"].([]any)
	first, _ := content[0].(map[string]any)
	text, _ := first["text"].(string)
	if !strings.Contains(text, "117/2025/QH15") || !strings.Contains(text, "hết hiệu lực") {
		t.Fatalf("report text = %q", text)
	}
	structured, _ := result["structuredContent"].(map[string]any)
	if _, ok := structured["derived_status"]; !ok {
		t.Fatalf("structured content lacks derived_status: %v", structured)
	}
}

func TestLegalCheckStatusWithoutCatalogue(t *testing.T) {
	ep := &types.MCPEndpoint{ID: "ep-1", TenantID: 1, Enabled: true, RateLimitPerMinute: 100,
		Tools: types.StringArray{types.MCPEndpointToolLegalCheckStatus}}
	resp := rpc(t, newLegalTestEngine(t, ep, nil), "tools/call", map[string]any{
		"name": types.MCPEndpointToolLegalCheckStatus, "arguments": map[string]any{"document_number": "1/2020"},
	})
	result, _ := resp["result"].(map[string]any)
	if isErr, _ := result["isError"].(bool); !isErr {
		t.Fatalf("expected an error result without a catalogue, got %v", result)
	}
}

func TestLegalGroupNeedsOnlyRetrieve(t *testing.T) {
	caps := types.MCPEndpointCapabilitiesForTools([]string{types.MCPEndpointToolLegalExpiryAudit})
	if len(caps) != 1 || caps[0] != string(types.APIKeyCapabilityRetrieve) {
		t.Fatalf("capabilities = %v", caps)
	}
	for _, name := range types.DefaultMCPEndpointTools() {
		if name == types.MCPEndpointToolLegalCheckStatus {
			return
		}
	}
	t.Fatal("legal tools should be in the default (read-only) allowlist")
}

func TestLegalTypeTargetsFromKBNames(t *testing.T) {
	targets := legalTypeTargets([]struct{ ID, Name string }{
		{"a", "VBPL – Hiến pháp, Bộ luật, Luật, Pháp lệnh"},
		{"b", "VBPL – Thông tư"},
		{"c", "VBPL – Thông tư liên tịch"},
		{"d", "Kho khác"},
	})
	if targets["Luật"].ID != "a" || targets["Pháp lệnh"].ID != "a" || targets["Thông tư"].ID != "b" ||
		targets["Thông tư liên tịch"].ID != "c" || len(targets) != 6 {
		t.Fatalf("targets = %v", targets)
	}
}
