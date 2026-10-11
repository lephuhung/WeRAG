package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestCheckMermaid(t *testing.T) {
	for _, c := range []struct {
		name, src, wantErr string
	}{
		{"flowchart", "flowchart TD\n  A[\"Tiếp nhận\"] --> B[\"Thẩm định\"]", ""},
		{"fenced", "```mermaid\nmindmap\n  root((\"Cải cách\"))\n    \"Thể chế\"\n```", ""},
		{"comment first", "%% sơ đồ\ntimeline\n  2025 : \"Ban hành\"\n  2026 : \"Thực hiện\"", ""},
		{"nothing to draw", "mindmap\n  root((\"Một ý\"))", "không đủ để vẽ"},
		{"empty", "  ", "trống"},
		{"unknown type", "erDiagram\n A ||--o{ B : has", "dòng đầu"},
		{"init directive", "%%{init: {'securityLevel':'loose'}}%%\nflowchart TD\n A-->B", "init"},
		{"click", "flowchart TD\n A-->B\n click A \"https://x\"", "click"},
		{"script", "flowchart TD\n A[\"<script>x</script>\"]", "HTML"},
		{"too long", "flowchart TD\n" + strings.Repeat("A-->B\n", 1200), "quá dài"},
	} {
		src, msg := checkMermaid(c.src)
		if c.wantErr == "" {
			if msg != "" || src == "" || strings.Contains(src, "```") {
				t.Errorf("%s: src=%q msg=%q", c.name, src, msg)
			}
			continue
		}
		if !strings.Contains(msg, c.wantErr) {
			t.Errorf("%s: msg=%q, want %q", c.name, msg, c.wantErr)
		}
	}
	// seen live: a model escaped the edge-label quotes twice
	src, msg := checkMermaid("flowchart TD\n C{\"Xét nghiệm\"} -->|\\\"Dương tính\\\"| D[\"Xử lý\"]")
	if msg != "" || strings.Contains(src, `\"`) || !strings.Contains(src, `|"Dương tính"|`) {
		t.Fatalf("double-escaped quotes: src=%q msg=%q", src, msg)
	}
}

func TestDrawDiagramAfterMatchBeatsIndex(t *testing.T) {
	ws := newFakeWorkspace(testCongVan(t, nd30Margins))
	res := runToolCtx(t, toolCtx(), NewDrawDiagramTool(ws, "s"),
		`{"title":"Quy trình","mermaid":"flowchart TD\n A[\"Tiếp nhận\"] --> B[\"Xử lý\"]","after":3,"after_match":"`+string([]rune(proposalP8)[:20])+`"}`)
	if !res.Success {
		t.Fatalf("result: %+v", res)
	}
	if ill := illustrationOf(t, res); ill["after"] != 8 {
		t.Fatalf("after_match wins over a miscounted index: %+v", ill)
	}
}

func illustrationOf(t *testing.T, res *types.ToolResult) map[string]interface{} {
	t.Helper()
	ill, ok := res.Data["illustration"].(map[string]interface{})
	if !ok {
		t.Fatalf("illustration = %T (%+v)", res.Data["illustration"], res)
	}
	return ill
}

func TestDrawDiagramProposesAfterTheSelection(t *testing.T) {
	ws := newFakeWorkspace(testCongVan(t, nd30Margins))
	before := string(ws.content)
	res := runToolCtx(t, selectionCtx(proposalP8), NewDrawDiagramTool(ws, "s"),
		`{"title":"Quy trình","mermaid":"flowchart TD\n A[\"Tiếp nhận\"] --> B[\"Xử lý\"]","caption":"Hình 1"}`)
	if !res.Success {
		t.Fatalf("result: %+v", res)
	}
	if len(ws.snapshots) != 0 || string(ws.content) != before {
		t.Fatal("a diagram proposal takes no snapshot and changes nothing")
	}
	ill := illustrationOf(t, res)
	anchor, _ := ill["anchor"].(*OpAnchor)
	if ill["kind"] != IllustrationDiagram || anchor == nil || anchor.Text != proposalP8 || ill["after"] != 8 ||
		ill["caption"] != "Hình 1" || !strings.HasPrefix(ill["mermaid"].(string), "flowchart TD") {
		t.Fatalf("illustration: %+v", ill)
	}
	if res.Data["document_id"] != "ws-1" || len(res.Data["ops_batch_id"].(string)) != 36 {
		t.Fatalf("data: %+v", res.Data)
	}
	if _, ok := res.Data["document_ops"]; ok {
		t.Fatal("a proposal carries no document_ops")
	}
	if !strings.Contains(res.Output, "CHƯA thay đổi") {
		t.Fatalf("output: %s", res.Output)
	}
}

func TestDrawDiagramWithoutPlaceStillProposes(t *testing.T) {
	ws := newFakeWorkspace(testCongVan(t, nd30Margins))
	res := runToolCtx(t, toolCtx(), NewDrawDiagramTool(ws, "s"), `{"title":"Sơ đồ","mermaid":"pie\n \"A\" : 1\n \"B\" : 2"}`)
	if !res.Success {
		t.Fatalf("result: %+v", res)
	}
	if _, ok := illustrationOf(t, res)["anchor"]; ok {
		t.Fatal("no selection, no after: no anchor")
	}
	if !strings.Contains(res.Output, "Chưa xác định được vị trí chèn") {
		t.Fatalf("output: %s", res.Output)
	}
	// no document at all: shown and downloadable
	res = runToolCtx(t, toolCtx(), NewDrawDiagramTool(nil, "s"), `{"title":"Sơ đồ","mermaid":"pie\n \"A\" : 1\n \"B\" : 2"}`)
	if !res.Success || res.Data["document_id"] != nil {
		t.Fatalf("result: %+v", res)
	}
	res = runToolCtx(t, toolCtx(), NewDrawDiagramTool(ws, "s"), `{"title":"x","mermaid":"erDiagram"}`)
	if res.Success {
		t.Fatal("an unknown diagram type fails")
	}
}

type fakeImages struct {
	args json.RawMessage
	res  *types.ToolResult
}

func (f *fakeImages) Execute(_ context.Context, args json.RawMessage) (*types.ToolResult, error) {
	f.args = args
	return f.res, nil
}

func TestGenerateIllustration(t *testing.T) {
	ws := newFakeWorkspace(testCongVan(t, nd30Margins))
	png := []byte("\x89PNG fake")
	images := &fakeImages{res: &types.ToolResult{Success: true, GeneratedImages: [][]byte{png}, Images: []string{"data:image/png;base64,x"}}}
	res := runToolCtx(t, selectionCtx(proposalP8), NewGenerateIllustrationTool(images, "Qwen Image", ws, "s"),
		`{"title":"Ảnh bìa","prompt":"A clean flat illustration of a public service office, no text","aspect":"wide","after":3}`)
	if !res.Success {
		t.Fatalf("result: %+v", res)
	}
	var call map[string]interface{}
	_ = json.Unmarshal(images.args, &call)
	if call["width"] != float64(1664) || call["height"] != float64(928) || !strings.Contains(call["prompt"].(string), "no text") {
		t.Fatalf("image call: %s", images.args)
	}
	sum := sha256.Sum256(png)
	ill := illustrationOf(t, res)
	if ill["kind"] != IllustrationImage || ill["image_sha256"] != hex.EncodeToString(sum[:]) || ill["after"] != 3 {
		t.Fatalf("illustration: %+v", ill)
	}
	if len(res.GeneratedImages) != 1 || len(res.Images) != 0 {
		t.Fatalf("the picture is persisted, not shown to the model: %d %d", len(res.GeneratedImages), len(res.Images))
	}

	images.res = &types.ToolResult{Success: false, Error: "connection refused"}
	res = runToolCtx(t, toolCtx(), NewGenerateIllustrationTool(images, "Qwen Image", ws, "s"), `{"title":"x","prompt":"a tree"}`)
	if res.Success || !strings.Contains(res.Error, "connection refused") || !strings.Contains(res.Error, "KHÔNG thử lại") {
		t.Fatalf("failure: %+v", res)
	}
	res = runToolCtx(t, toolCtx(), NewGenerateIllustrationTool(images, "Qwen Image", ws, "s"), `{"title":"x","prompt":"a tree","aspect":"tall"}`)
	if res.Success {
		t.Fatal("unknown aspect fails")
	}
}

func TestDrawDiagramRefusesThinData(t *testing.T) {
	res := runToolCtx(t, toolCtx(), NewDrawDiagramTool(nil, "s"), `{"title":"x","mermaid":"flowchart TD\n A[\"Một ý\"]"}`)
	if res.Success || !strings.Contains(res.Error, "không đủ để vẽ") || strings.Contains(res.Error, "gọi lại") {
		t.Fatalf("thin diagram: %+v", res)
	}
}

func TestGenerateIllustrationRefusesVietnameseAndReportsLostService(t *testing.T) {
	images := &fakeImages{res: &types.ToolResult{Success: true, GeneratedImages: [][]byte{[]byte("png")}}}
	tool := NewGenerateIllustrationTool(images, "Qwen Image", nil, "s")
	res := runToolCtx(t, toolCtx(), tool, `{"title":"x","prompt":"A banner with the words Chào mừng năm mới"}`)
	if res.Success || !strings.Contains(res.Error, "KHÔNG viết được tiếng Việt") || images.args != nil {
		t.Fatalf("vietnamese prompt: %+v", res)
	}
	if hasVietnameseLetters("A café in Paris, no text") {
		t.Fatal("French accents are not Vietnamese")
	}

	images.res = &types.ToolResult{Success: false, Error: "dial tcp 10.10.0.241:8101: connect: connection refused"}
	res = runToolCtx(t, toolCtx(), tool, `{"title":"x","prompt":"a tree"}`)
	if res.Success || !strings.Contains(res.Error, "mất kết nối") || !strings.Contains(res.Error, "hỏi họ") {
		t.Fatalf("lost service: %+v", res)
	}
	images.res = &types.ToolResult{Success: false, Error: "width/height must be within 512-2752"}
	res = runToolCtx(t, toolCtx(), tool, `{"title":"x","prompt":"a tree"}`)
	if res.Success || strings.Contains(res.Error, "mất kết nối") || !strings.Contains(res.Error, "điều chỉnh") {
		t.Fatalf("refused request: %+v", res)
	}
	// FastMCP hides the cause (here the model server was OOM-killed).
	images.res = &types.ToolResult{Success: false, Error: "Error executing tool generate_image"}
	res = runToolCtx(t, toolCtx(), tool, `{"title":"x","prompt":"a tree"}`)
	if res.Success || !strings.Contains(res.Error, "lỗi ở phía dịch vụ") || strings.Contains(res.Error, "điều chỉnh") {
		t.Fatalf("service fault: %+v", res)
	}
}
