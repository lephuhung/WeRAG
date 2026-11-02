package mcpserver

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
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
			Status: types.LegalRequestRequested, RequestCount: 3, LastRequestedAt: at,
			Detail: types.JSON(`{"portal":{"html":false,"pdf_file":"VanBanGoc_53.pdf","exists":true}}`)},
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
	if portal, _ := first["portal"].(map[string]any); portal["pdf_file"] != "VanBanGoc_53.pdf" || portal["html"] != false {
		t.Fatalf("first portal = %v", first["portal"])
	}
	if second["vbpl_id"] != "" || second["url"] != nil || second["portal"] != nil {
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

type fakeLegalDrafts struct {
	interfaces.LegalDraftService
	fetched, by string
	err         error
}

func (f *fakeLegalDrafts) FetchDocument(_ context.Context, vbplID, by string) (*types.LegalDocumentDraft, error) {
	f.fetched, f.by = vbplID, by
	if f.err != nil {
		return nil, f.err
	}
	return &types.LegalDocumentDraft{ID: "d1", LegalDocumentID: vbplID, DocumentNumber: "53/2022/NĐ-CP",
		PDFFileName: "VanBanGoc_53.pdf", Status: types.LegalDraftOCR, RequestID: "r1"}, nil
}

func (f *fakeLegalDrafts) Summaries(_ context.Context, ids []string) (map[string]*types.LegalDraftSummary, error) {
	out := map[string]*types.LegalDraftSummary{}
	for _, id := range ids {
		if id == "155366" {
			out[id] = &types.LegalDraftSummary{ID: "d1", Status: types.LegalDraftReady, OCRPages: 12}
		}
	}
	return out, nil
}

func TestLegalFetchDraft(t *testing.T) {
	ep := &types.MCPEndpoint{ID: "ep-1", TenantID: 1, Enabled: true, RateLimitPerMinute: 100,
		Tools: types.StringArray{types.MCPEndpointToolLegalListRequests, types.MCPEndpointToolLegalFetchDraft}}
	repo := &fakeLegalRequests{}
	drafts := &fakeLegalDrafts{}
	gin.SetMode(gin.TestMode)
	srv := NewServer(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, &recordingEndpointRepo{touched: make(chan string, 8)},
		nil, nil, nil, nil, nil, nil, nil)
	srv.SetLegalFeed(nil, repo)
	srv.SetLegalDrafts(drafts)
	r := gin.New()
	r.POST("/mcp/:endpoint_id", func(c *gin.Context) {
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), types.MCPEndpointContextKey, ep))
		c.Next()
	}, gin.WrapH(srv.Handler()))

	if names := toolNames(t, rpc(t, r, "tools/list", map[string]any{})); len(names) != 2 {
		t.Fatalf("tools = %v", names)
	}
	resp := rpc(t, r, "tools/call", map[string]any{"name": types.MCPEndpointToolLegalFetchDraft,
		"arguments": map[string]any{"vbpl_id": " 155366 "}})
	result, _ := resp["result"].(map[string]any)
	if isErr, _ := result["isError"].(bool); isErr {
		t.Fatalf("error result: %v", result)
	}
	structured, _ := result["structuredContent"].(map[string]any)
	if drafts.fetched != "155366" || drafts.by != "mcp:ep-1" || structured["draft_id"] != "d1" || structured["status"] != "ocr" ||
		structured["pdf_file"] != "VanBanGoc_53.pdf" {
		t.Fatalf("fetch: %v (%q %q)", structured, drafts.fetched, drafts.by)
	}
	drafts.err = apperrors.NewBadRequestError("vbpl.vn có toàn văn HTML của văn bản này")
	resp = rpc(t, r, "tools/call", map[string]any{"name": types.MCPEndpointToolLegalFetchDraft,
		"arguments": map[string]any{"vbpl_id": "155366"}})
	if result, _ := resp["result"].(map[string]any); result["isError"] != true || !strings.Contains(fmt.Sprint(result["content"]), "toàn văn HTML") {
		t.Fatalf("refusal: %v", result)
	}
	resp = rpc(t, r, "tools/call", map[string]any{"name": types.MCPEndpointToolLegalFetchDraft, "arguments": map[string]any{}})
	if result, _ := resp["result"].(map[string]any); result["isError"] != true {
		t.Fatalf("no vbpl_id accepted: %v", result)
	}

	// request rows carry their draft
	resp = rpc(t, r, "tools/call", map[string]any{"name": types.MCPEndpointToolLegalListRequests, "arguments": map[string]any{"status": "fetched"}})
	result, _ = resp["result"].(map[string]any)
	structured, _ = result["structuredContent"].(map[string]any)
	rows, _ := structured["requests"].([]any)
	if repo.status != types.LegalRequestFetched || len(rows) != 2 {
		t.Fatalf("list: %q %v", repo.status, structured)
	}
	first, _ := rows[0].(map[string]any)
	second, _ := rows[1].(map[string]any)
	if d, _ := first["draft"].(map[string]any); d["id"] != "d1" || d["status"] != "ready" || d["ocr_pages"] != float64(12) {
		t.Fatalf("draft summary = %v", first["draft"])
	}
	if second["draft"] != nil {
		t.Fatalf("uncatalogued row has a draft: %v", second)
	}
	if def, ok := types.LookupMCPEndpointTool(types.MCPEndpointToolLegalFetchDraft); !ok || !def.Destructive ||
		def.Group != types.MCPEndpointToolGroupLegalWrite {
		t.Fatalf("catalog entry = %+v", def)
	}
}
