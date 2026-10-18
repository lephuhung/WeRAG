package mcpserver

import (
	"context"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type fakeLegalRequests struct {
	interfaces.LegalRequestRepository
	status string
	limit  int
}

func (f *fakeLegalRequests) List(_ context.Context, status string, _, limit int) ([]*types.LegalDocumentRequest, int64, error) {
	f.status, f.limit = status, limit
	at := time.Date(2026, 10, 18, 14, 5, 0, 0, time.UTC)
	return []*types.LegalDocumentRequest{
		{ID: "r1", LegalDocumentID: "155366", DocumentNumber: "53/2022/NĐ-CP", Title: "T", DocType: "Nghị định",
			Status: types.LegalRequestRequested, RequestCount: 3, LastRequestedAt: at},
		{ID: "r2", DocumentNumber: "12/2024/QĐ-UBND", Status: types.LegalRequestRequested, RequestCount: 1, LastRequestedAt: at},
	}, 2, nil
}

func newLegalRequestsEngine(t *testing.T, ep *types.MCPEndpoint, repo interfaces.LegalRequestRepository) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	srv := NewServer(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, &recordingEndpointRepo{touched: make(chan string, 8)},
		nil, nil, nil, nil, nil, nil, nil)
	srv.SetLegalFeed(nil, repo)
	r := gin.New()
	r.POST("/mcp/:endpoint_id", func(c *gin.Context) {
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), types.MCPEndpointContextKey, ep))
		c.Next()
	}, gin.WrapH(srv.Handler()))
	return r
}

func TestLegalListRequests(t *testing.T) {
	ep := &types.MCPEndpoint{ID: "ep-1", TenantID: 1, Enabled: true, RateLimitPerMinute: 100,
		Tools: types.StringArray{types.MCPEndpointToolLegalListRequests}}
	repo := &fakeLegalRequests{}
	r := newLegalRequestsEngine(t, ep, repo)
	if names := toolNames(t, rpc(t, r, "tools/list", map[string]any{})); len(names) != 1 || names[0] != types.MCPEndpointToolLegalListRequests {
		t.Fatalf("tools = %v", names)
	}
	resp := rpc(t, r, "tools/call", map[string]any{"name": types.MCPEndpointToolLegalListRequests,
		"arguments": map[string]any{"limit": 9999}})
	result, _ := resp["result"].(map[string]any)
	if isErr, _ := result["isError"].(bool); isErr {
		t.Fatalf("error result: %v", result)
	}
	if repo.status != types.LegalRequestRequested || repo.limit != maxLegalRequestsLimit {
		t.Fatalf("status %q limit %d", repo.status, repo.limit)
	}
	structured, _ := result["structuredContent"].(map[string]any)
	rows, _ := structured["requests"].([]any)
	if len(rows) != 2 {
		t.Fatalf("rows = %v", structured)
	}
	first, _ := rows[0].(map[string]any)
	second, _ := rows[1].(map[string]any)
	if first["vbpl_id"] != "155366" || first["url"] != "https://vbpl.vn/van-ban/chi-tiet/155366" || first["request_count"] != float64(3) {
		t.Fatalf("first = %v", first)
	}
	if second["vbpl_id"] != "" || second["url"] != nil {
		t.Fatalf("uncatalogued row = %v", second)
	}

	rpc(t, r, "tools/call", map[string]any{"name": types.MCPEndpointToolLegalListRequests, "arguments": map[string]any{"status": "all"}})
	if repo.status != "" {
		t.Fatalf("all → %q", repo.status)
	}
	resp = rpc(t, r, "tools/call", map[string]any{"name": types.MCPEndpointToolLegalListRequests, "arguments": map[string]any{"status": "x"}})
	if result, _ := resp["result"].(map[string]any); result["isError"] != true {
		t.Fatalf("bad status accepted: %v", result)
	}
	// without the queue wired the tool reports it
	resp = rpc(t, newLegalRequestsEngine(t, ep, nil), "tools/call", map[string]any{"name": types.MCPEndpointToolLegalListRequests, "arguments": map[string]any{}})
	if result, _ := resp["result"].(map[string]any); result["isError"] != true {
		t.Fatalf("no queue: %v", result)
	}
	caps := types.MCPEndpointCapabilitiesForTools([]string{types.MCPEndpointToolLegalListRequests})
	if len(caps) != 1 || caps[0] != string(types.APIKeyCapabilityRetrieve) {
		t.Fatalf("capabilities = %v", caps)
	}
}
