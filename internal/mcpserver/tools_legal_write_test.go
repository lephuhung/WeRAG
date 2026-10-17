package mcpserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestVBPLFileNameAndMetadataMatchIngestScript(t *testing.T) {
	d := &vbplDetail{
		ID: "179941", DocNum: "30/2020/NĐ-CP", Title: "  Nghị định 30/2020/NĐ-CP  về công tác\nvăn thư ",
		DocType: vbplNamed{Name: "Nghị định"}, EffStatus: vbplNamed{Name: "Còn hiệu lực"},
		IssueDate: "2020-03-05T00:00:00Z", EffFrom: "2020-03-05T00:00:00Z",
		Related: []vbplRelated{
			{RelatedType: "APPENDIX", FileName: "phuluc1.doc", FileTitle: " Phụ lục  I "},
			{RelatedType: "MAIN", FileName: "main.doc"},
		},
	}
	// the number is inside the title, so no "num - " prefix; "/" → "-"
	if got := vbplFileName(d); got != "Nghị định 30-2020-NĐ-CP về công tác văn thư.html" {
		t.Fatalf("file name = %q", got)
	}
	d2 := &vbplDetail{ID: "1", DocNum: "01/2026/TT-BCA", Title: "Thông tư về x"}
	if got := vbplFileName(d2); got != "01-2026-TT-BCA - Thông tư về x.html" {
		t.Fatalf("file name = %q", got)
	}
	meta := vbplMetadata(d)
	for k, want := range map[string]string{
		"external_id": "179941", "source": "vbpl", "doc_number": "30/2020/NĐ-CP", "doc_type": "Nghị định",
		"issue_date": "2020-03-05", "eff_from": "2020-03-05", "eff_status": "Còn hiệu lực",
		"vbpl_url": "https://vbpl.vn/van-ban/chi-tiet/179941",
	} {
		if meta[k] != want {
			t.Fatalf("metadata[%s] = %q, want %q", k, meta[k], want)
		}
	}
	var files []map[string]string
	if err := json.Unmarshal([]byte(meta["vbpl_appendix_files"]), &files); err != nil || len(files) != 1 ||
		files[0]["title"] != "Phụ lục I" || files[0]["file_name"] != "phuluc1.doc" {
		t.Fatalf("appendix files = %q (%v)", meta["vbpl_appendix_files"], err)
	}
	if _, ok := vbplMetadata(&vbplDetail{ID: "2"})["doc_number"]; ok {
		t.Fatal("empty values must be dropped")
	}
}

func TestMemoryFileHeaderRoundTrip(t *testing.T) {
	content := []byte("<html><p class=\"prov-article\">Điều 1</p></html>")
	fh, err := memoryFileHeader("file", `a "b".html`, content)
	if err != nil {
		t.Fatal(err)
	}
	if fh.Filename != "a 'b'.html" || fh.Size != int64(len(content)) {
		t.Fatalf("header = %+v", fh)
	}
	f, err := fh.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, _ := io.ReadAll(f)
	if string(got) != string(content) {
		t.Fatalf("content = %q", got)
	}
}

func TestVBPLFetchDetail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("User-Agent"), "Mozilla") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/179941"):
			_, _ = io.WriteString(w, `{"success":true,"data":{"id":"179941","docNum":"30/2020/NĐ-CP","title":"T",
				"docType":{"name":"Nghị định"},"effStatus":{"name":"Còn hiệu lực"},
				"documentContent":{"content":"<p class=\"prov-article\">Điều 1</p>"}}}`)
		case strings.HasSuffix(r.URL.Path, "/missing"):
			_, _ = io.WriteString(w, `{"success":true,"data":null}`)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()
	client := srv.Client()
	client.Transport = rewriteHost{base: srv.URL, next: client.Transport}

	d, err := vbplFetchDetail(context.Background(), client, "179941")
	if err != nil {
		t.Fatal(err)
	}
	if d.DocNum != "30/2020/NĐ-CP" || !strings.Contains(d.Content, "prov-article") || d.RawContent != nil {
		t.Fatalf("detail = %+v", d)
	}
	if _, err := vbplFetchDetail(context.Background(), client, "missing"); err == nil || !strings.Contains(err.Error(), "no document") {
		t.Fatalf("missing: err = %v", err)
	}
	if _, err := vbplFetchDetail(context.Background(), client, "boom"); err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("500: err = %v", err)
	}
	if _, err := vbplFetchDetail(context.Background(), client, "../x"); err == nil {
		t.Fatal("bad id must be refused before any request")
	}
}

// rewriteHost sends every request to the test server.
type rewriteHost struct {
	base string
	next http.RoundTripper
}

func (r rewriteHost) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = "http"
	clone.URL.Host = strings.TrimPrefix(r.base, "http://")
	return r.next.RoundTrip(clone)
}

func TestLegalWriteGroupIsDestructiveAndNeedsIngest(t *testing.T) {
	caps := types.MCPEndpointCapabilitiesForTools([]string{types.MCPEndpointToolLegalIngest})
	if len(caps) != 1 || caps[0] != string(types.APIKeyCapabilityIngest) {
		t.Fatalf("capabilities = %v", caps)
	}
	for _, name := range types.DefaultMCPEndpointTools() {
		if strings.HasPrefix(name, "legal_") {
			def, _ := types.LookupMCPEndpointTool(name)
			if def.Group == types.MCPEndpointToolGroupLegalWrite {
				t.Fatalf("%s is a write tool and must not be a default tool", name)
			}
		}
	}
}

func TestLegalWriteToolsRefuseWithoutServices(t *testing.T) {
	ep := &types.MCPEndpoint{ID: "ep-1", TenantID: 1, Enabled: true, RateLimitPerMinute: 100,
		Tools: types.StringArray{types.MCPEndpointToolLegalIngest, types.MCPEndpointToolLegalRemove,
			types.MCPEndpointToolLegalReviewScan, types.MCPEndpointToolLegalWikiRebuild}}
	r := newLegalTestEngine(t, ep, nil)
	for name, args := range map[string]map[string]any{
		types.MCPEndpointToolLegalIngest:      {"vbpl_id": "1", "knowledge_base_id": "kb"},
		types.MCPEndpointToolLegalRemove:      {"vbpl_id": "1", "reason": "test"},
		types.MCPEndpointToolLegalReviewScan:  {},
		types.MCPEndpointToolLegalWikiRebuild: {"knowledge_base_id": "kb"},
	} {
		resp := rpc(t, r, "tools/call", map[string]any{"name": name, "arguments": args})
		result, _ := resp["result"].(map[string]any)
		if isErr, _ := result["isError"].(bool); !isErr {
			t.Fatalf("%s: expected an error result without services, got %v", name, result)
		}
	}
}
