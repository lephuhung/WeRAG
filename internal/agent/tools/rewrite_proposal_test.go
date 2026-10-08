package tools

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

const proposalP8 = "Sở Nội vụ đề nghị các đơn vị triển khai công tác cải cách hành chính năm 2026."

func TestRewriteModeDecision(t *testing.T) {
	target := &types.DocumentWorkspace{ID: "ws-1"}
	in := &types.DocumentSelection{Text: "x", DocumentID: "ws-1"}
	legacy := &types.DocumentSelection{Text: "x"} // client without document ids
	elsewhere := &types.DocumentSelection{Text: "x", DocumentID: "ws-2"}
	for _, c := range []struct {
		name, asked string
		sel         *types.DocumentSelection
		want        string
	}{
		{"selection in the target", "", in, rewriteModePropose},
		{"selection without document id", "", legacy, rewriteModePropose},
		{"no selection", "", nil, rewriteModeApply},
		{"selection elsewhere", "", elsewhere, rewriteModeApply},
		{"explicit apply", "apply", in, rewriteModeApply},
		{"explicit propose", "propose", nil, rewriteModePropose},
		{"unknown falls back", "later", in, rewriteModePropose},
	} {
		if got := rewriteMode(c.asked, c.sel, target); got != c.want {
			t.Errorf("%s: mode %q, want %q", c.name, got, c.want)
		}
	}
}

func proposalVariants(t *testing.T, res *types.ToolResult) []rewriteVariant {
	t.Helper()
	v, ok := res.Data["variants"].([]rewriteVariant)
	if !ok {
		t.Fatalf("variants = %T", res.Data["variants"])
	}
	return v
}

func TestRewriteProposeTakesNoSnapshotAndListsVariants(t *testing.T) {
	ws := newFakeWorkspace(testCongVan(t, nd30Margins))
	before := append([]byte(nil), ws.content...)
	long := "Sở Nội vụ đề nghị các cơ quan, đơn vị, địa phương khẩn trương triển khai đồng bộ, hiệu quả các nhiệm vụ cải cách hành chính năm 2026 theo kế hoạch đã được Ủy ban nhân dân tỉnh phê duyệt."
	res := runToolCtx(t, selectionCtx(proposalP8), NewRewriteParagraphsTool(ws, "s"), `{"edits":[
		{"paragraph":8,"new":"Sở Nội vụ đề nghị các đơn vị triển khai cải cách hành chính năm 2026."}],
		"variants":["`+long+`"],"labels":["Gọn hơn"]}`)
	if !res.Success {
		t.Fatalf("result: %+v", res)
	}
	if len(ws.snapshots) != 0 || !bytes.Equal(before, ws.content) {
		t.Fatalf("a proposal takes no snapshot and changes nothing: %v", ws.snapshots)
	}
	for _, key := range []string{"document_ops", "snapshot_seq"} {
		if _, ok := res.Data[key]; ok {
			t.Fatalf("a proposal carries no %s: %+v", key, res.Data)
		}
	}
	if res.Data["proposal"] != true || res.Data["document_id"] != "ws-1" || res.Data["selection_text"] != proposalP8 ||
		!strings.Contains(res.Data["document"].(string), "cong-van.docx") {
		t.Fatalf("data: %+v", res.Data)
	}
	if id, _ := res.Data["ops_batch_id"].(string); len(id) != 36 {
		t.Fatalf("ops_batch_id = %v", res.Data["ops_batch_id"])
	}
	vs := proposalVariants(t, res)
	if len(vs) != 2 || vs[0].ID != "v1" || vs[0].Label != "Gọn hơn" || vs[1].ID != "v2" || vs[1].Label != "Phương án 2" {
		t.Fatalf("variants: %+v", vs)
	}
	if vs[0].Old != proposalP8 || vs[1].New != long || len(vs[1].Ops) != 1 || vs[1].Ops[0].Op != OpReplaceParagraph ||
		vs[1].Ops[0].Anchor.Text != proposalP8 || *vs[1].Ops[0].New != long {
		t.Fatalf("variant 2: %+v", vs[1])
	}
	// each variant applies on its own to the document as it is
	for _, v := range vs {
		applyOps(t, ws.content, v.Ops)
	}
	if !strings.Contains(res.Output, long) {
		t.Fatalf("the output lists each version in full:\n%s", res.Output)
	}
	if strings.Contains(res.Output, editorAppliedNote) || !strings.Contains(res.Output, "Thay vào văn bản") ||
		!strings.Contains(res.Output, "chưa áp dụng") {
		t.Fatalf("output:\n%s", res.Output)
	}
	raw, err := json.Marshal(res.Data)
	if err != nil || !strings.Contains(string(raw), `"variants":[{"id":"v1","label":"Gọn hơn"`) {
		t.Fatalf("json: %s %v", raw, err)
	}
}

func TestRewriteProposeIsTheDefaultWithASelection(t *testing.T) {
	ws := newFakeWorkspace(testCongVan(t, nd30Margins))
	res := runToolCtx(t, selectionCtx(proposalP8), NewRewriteParagraphsTool(ws, "s"),
		`{"edits":[{"paragraph":8,"old":"năm 2026","new":"năm 2027"},{"paragraph":8,"old":"Sở Nội vụ","new":"Sở Nội vụ tỉnh"}]}`)
	if !res.Success || res.Data["proposal"] != true || len(ws.snapshots) != 0 {
		t.Fatalf("result: %+v snapshots %v", res, ws.snapshots)
	}
	vs := proposalVariants(t, res)
	// several edits make one version holding every op
	if len(vs) != 1 || len(vs[0].Ops) != 2 || vs[0].Old != "năm 2026\nSở Nội vụ" || vs[0].New != "năm 2027\nSở Nội vụ tỉnh" {
		t.Fatalf("variants: %+v", vs)
	}
	// the user explicitly asked to apply at once: today's behaviour
	res = runToolCtx(t, selectionCtx("năm 2026"), NewRewriteParagraphsTool(ws, "s"),
		`{"mode":"apply","edits":[{"paragraph":8,"old":"năm 2026","new":"năm 2027"}]}`)
	if !res.Success || len(resultOps(t, res)) != 1 || len(ws.snapshots) != 1 || !strings.Contains(res.Output, editorAppliedNote) {
		t.Fatalf("apply: %+v", res)
	}
	if _, ok := res.Data["proposal"]; ok {
		t.Fatal("an applied rewrite is no proposal")
	}
	// no selection: refused as before, whatever the mode
	res = runTool(t, NewRewriteParagraphsTool(ws, "s"), `{"mode":"propose","edits":[{"paragraph":8,"new":"x"}]}`)
	if res.Success || !strings.Contains(res.Error, "bôi đen") {
		t.Fatalf("no selection: %+v", res)
	}
}

func TestRewriteVariantsValidation(t *testing.T) {
	ws := newFakeWorkspace(testCongVan(t, nd30Margins))
	ctx := selectionCtx(proposalP8)
	for name, args := range map[string]string{
		"two edits":      `{"edits":[{"paragraph":8,"old":"năm 2026","new":"a"},{"paragraph":8,"old":"Sở","new":"b"}],"variants":["c"]}`,
		"four versions":  `{"edits":[{"paragraph":8,"new":"a"}],"variants":["b","c","d"]}`,
		"empty variant":  `{"edits":[{"paragraph":8,"new":"a"}],"variants":[" "]}`,
		"with apply":     `{"mode":"apply","edits":[{"paragraph":8,"new":"a"}],"variants":["b"]}`,
		"unknown mode":   `{"mode":"later","edits":[{"paragraph":8,"new":"a"}]}`,
		"outside select": `{"edits":[{"paragraph":12,"new":"Trần Văn B"}],"variants":["Trần Văn C"]}`,
	} {
		if res := runToolCtx(t, ctx, NewRewriteParagraphsTool(ws, "s"), args); res.Success {
			t.Errorf("%s: accepted: %+v", name, res)
		}
	}
	if len(ws.snapshots) != 0 {
		t.Fatalf("snapshots: %v", ws.snapshots)
	}
	// variants[0] repeating edits[0].new is version 1, not a second one;
	// three versions in all are fine
	res := runToolCtx(t, ctx, NewRewriteParagraphsTool(ws, "s"),
		`{"edits":[{"paragraph":8,"old":"năm 2026","new":"năm 2027"}],"variants":["năm 2027","năm 2028","trong năm 2027"]}`)
	if !res.Success {
		t.Fatalf("result: %+v", res)
	}
	vs := proposalVariants(t, res)
	if len(vs) != 3 || vs[0].New != "năm 2027" || vs[1].New != "năm 2028" || vs[2].Label != "Phương án 3" || *vs[2].Ops[0].Old != "năm 2026" {
		t.Fatalf("variants: %+v", vs)
	}
	// a version equal to the current text is no version
	res = runToolCtx(t, ctx, NewRewriteParagraphsTool(ws, "s"),
		`{"edits":[{"paragraph":8,"old":"năm 2026","new":"năm 2026"}],"variants":["năm 2027"]}`)
	if vs := proposalVariants(t, res); !res.Success || len(vs) != 1 || vs[0].ID != "v1" || vs[0].New != "năm 2027" {
		t.Fatalf("unchanged version: %+v", res)
	}
}
