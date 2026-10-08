package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/docformat"
	"github.com/Tencent/WeKnora/internal/types"
)

// routeWorkspace holds two targets: vb1 a kế hoạch on cải cách hành chính,
// vb2 a báo cáo on the budget, each with two Điều.
func routeWorkspace(t *testing.T) *fakeWorkspace {
	t.Helper()
	p := func(text string, bold bool) string { return testPara(text, "left", "Times New Roman", 14, bold, false) }
	plan := strings.Join([]string{
		p("KẾ HOẠCH", true),
		p("Điều 1. Mục tiêu cải cách hành chính", true),
		p("Đẩy mạnh cải cách hành chính, nâng cao chỉ số hài lòng của người dân.", false),
		p("Điều 2. Tổ chức thực hiện", true),
		p("Sở Nội vụ chủ trì, phối hợp các sở theo dõi việc thực hiện.", false),
	}, "")
	report := strings.Join([]string{
		p("BÁO CÁO", true),
		p("Điều 1. Thu ngân sách", true),
		p("Tổng thu ngân sách năm 2025 đạt 1.250 tỷ đồng.", false),
		p("Điều 2. Chi đầu tư", true),
		p("Chi đầu tư phát triển 430 tỷ đồng, giao Phòng PA05 theo dõi.", false),
	}, "")
	ws := newFakeWorkspace(buildTestDocx(t, plan, [4]int{20, 15, 30, 20}))
	ws.addDocument("ws-2", "bao-cao.docx", buildTestDocx(t, report, [4]int{20, 15, 30, 20}))
	return ws
}

type fakeRouter struct {
	reply string
	err   error
	calls int
	seen  string
}

func (f *fakeRouter) Complete(_ context.Context, msgs []docformat.Message) (string, error) {
	f.calls++
	f.seen = msgs[len(msgs)-1].Content
	return f.reply, f.err
}

func TestRouteTier1PicksTheClearDocumentAndSection(t *testing.T) {
	freshDocProfiles(t)
	ws := routeWorkspace(t)
	model := &fakeRouter{}

	ctx, scope := ApplyDocumentScope(toolCtx(), ws, 7, "s-route-1", DocumentRouteInput{Query: "tổng thu ngân sách năm 2025 đạt bao nhiêu", Model: model})
	if scope == nil || scope.DocumentIDs[0] != "ws-2" || scope.SetBy != types.DocumentScopeSetByRouter || model.calls != 0 {
		t.Fatalf("tier 1 must pick vb2 without the model: %+v (calls %d)", scope, model.calls)
	}
	if len(scope.Sections) != 1 || scope.Sections[0].Title != "Điều 1. Thu ngân sách" {
		t.Fatalf("the matching section: %+v", scope.Sections)
	}
	if types.DocumentScopeFromContext(ctx) != scope {
		t.Fatal("the scope rides on ctx")
	}
	if st := SessionDocumentScope(toolCtx(), "s-route-1"); st == nil || st.DocumentIDs[0] != "ws-2" || st.Query == "" {
		t.Fatalf("the router scope is stored: %+v", st)
	}

	// a code alone decides too, and the task is read from the words
	_, scope = ApplyDocumentScope(toolCtx(), ws, 7, "s-route-2", DocumentRouteInput{Query: "đối chiếu nhiệm vụ của PA05", Model: model})
	if scope == nil || scope.DocumentIDs[0] != "ws-2" || scope.Task != types.DocumentScopeTaskCompare {
		t.Fatalf("PA05: %+v", scope)
	}
	_, scope = ApplyDocumentScope(toolCtx(), ws, 7, "s-route-3", DocumentRouteInput{Query: "Sở Nội vụ chủ trì việc gì trong cải cách hành chính"})
	if scope == nil || scope.DocumentIDs[0] != "ws-1" {
		t.Fatalf("vb1: %+v", scope)
	}
}

func TestRouteTier2AsksTheModelWhenTier1IsUnclear(t *testing.T) {
	freshDocProfiles(t)
	ws := routeWorkspace(t)
	model := &fakeRouter{reply: "```json\n{\"documents\":[{\"handle\":\"vb1\",\"sections\":[\"Điều 2. Tổ chức thực hiện\",\"Điều 9\"]}],\"task\":\"summary\",\"confidence\":0.8}\n```"}
	history := []DocumentRouteTurn{
		{Role: "user", Content: "kế hoạch này giao cho ai"}, {Role: "tool", Content: "x"},
		{Role: "assistant", Content: strings.Repeat("Sở Nội vụ chủ trì. ", 40)},
	}
	_, scope := ApplyDocumentScope(toolCtx(), ws, 7, "s-route-4", DocumentRouteInput{Query: "tóm tắt giúp phần còn lại", History: history, Model: model})
	if model.calls != 1 || scope == nil || scope.DocumentIDs[0] != "ws-1" || scope.Task != types.DocumentScopeTaskSummary ||
		len(scope.Sections) != 1 || scope.Sections[0].Title != "Điều 2. Tổ chức thực hiện" {
		t.Fatalf("tier 2 scope: %+v (calls %d)", scope, model.calls)
	}
	for _, want := range []string{"Câu hỏi: tóm tắt giúp phần còn lại", "user: kế hoạch này giao cho ai", "- vb2 · bao-cao.docx (văn bản làm việc)"} {
		if !strings.Contains(model.seen, want) {
			t.Fatalf("router input lacks %q:\n%s", want, model.seen)
		}
	}
	if strings.Contains(model.seen, "tool: x") || strings.Count(model.seen, "Sở Nội vụ chủ trì.") > 20 {
		t.Fatalf("history is user/assistant only, trimmed:\n%s", model.seen)
	}

	// with a router scope stored, an unclear follow-up keeps it
	model.calls = 0
	_, kept := ApplyDocumentScope(toolCtx(), ws, 7, "s-route-4", DocumentRouteInput{Query: "còn gì nữa", Model: model})
	if model.calls != 0 || kept == nil || kept.DocumentIDs[0] != "ws-1" {
		t.Fatalf("the stored router scope is kept: %+v (calls %d)", kept, model.calls)
	}

	for name, m := range map[string]*fakeRouter{
		"low confidence": {reply: `{"documents":[{"handle":"vb1"}],"confidence":0.4}`},
		"unknown handle": {reply: `{"documents":[{"handle":"vb7"}],"confidence":0.9}`},
		"not json":       {reply: "vb1"},
		"error":          {err: errors.New("down")},
	} {
		if _, scope := ApplyDocumentScope(toolCtx(), ws, 7, "s-route-5-"+name, DocumentRouteInput{Query: "tóm tắt giúp phần còn lại", Model: m}); scope != nil || m.calls != 1 {
			t.Fatalf("%s: no scope expected, got %+v", name, scope)
		}
	}
	// a plain code lookup never asks the model
	m := &fakeRouter{reply: `{"documents":[{"handle":"vb1"}],"confidence":0.9}`}
	if _, scope := ApplyDocumentScope(toolCtx(), ws, 7, "s-route-6", DocumentRouteInput{Query: "45/KH-UBND", Model: m}); scope != nil || m.calls != 0 {
		t.Fatalf("code lookup: %+v (calls %d)", scope, m.calls)
	}
}

func TestRoutePrecedence(t *testing.T) {
	freshDocProfiles(t)
	ws := routeWorkspace(t)
	model := &fakeRouter{reply: `{"documents":[{"handle":"vb2"}],"confidence":0.9}`}

	// a user scope wins over the router
	user := &types.DocumentScope{DocumentIDs: []string{"ws-1"}, SetBy: types.DocumentScopeSetByUser}
	SetSessionDocumentScope(toolCtx(), "s-prec-1", user)
	_, scope := ApplyDocumentScope(toolCtx(), ws, 7, "s-prec-1", DocumentRouteInput{Query: "tổng thu ngân sách đạt bao nhiêu", Model: model})
	if scope == nil || scope.DocumentIDs[0] != "ws-1" || scope.SetBy != types.DocumentScopeSetByUser || model.calls != 0 {
		t.Fatalf("user scope: %+v", scope)
	}
	// an @ overrides it for the turn without clearing it
	named := types.WithMentionedDocuments(toolCtx(), []string{"ws-2"})
	if ctx, scope := ApplyDocumentScope(named, ws, 7, "s-prec-1", DocumentRouteInput{Query: "x"}); scope != nil || types.DocumentScopeFromContext(ctx) != nil {
		t.Fatalf("@ overrides the scope: %+v", scope)
	}
	if SessionDocumentScope(toolCtx(), "s-prec-1") == nil {
		t.Fatal("a user scope survives an @")
	}

	// a selection clears a stored router scope
	SetSessionDocumentScope(toolCtx(), "s-prec-2", &types.DocumentScope{DocumentIDs: []string{"ws-2"}, SetBy: types.DocumentScopeSetByRouter})
	sel := types.WithDocumentSelection(toolCtx(), &types.DocumentSelection{Text: "Sở Nội vụ chủ trì", DocumentID: "ws-1"})
	if _, scope := ApplyDocumentScope(sel, ws, 7, "s-prec-2", DocumentRouteInput{Query: "sửa đoạn này"}); scope != nil {
		t.Fatalf("selection: %+v", scope)
	}
	if SessionDocumentScope(toolCtx(), "s-prec-2") != nil {
		t.Fatal("a selection clears the router scope")
	}

	// a scope naming a closed document drops out
	SetSessionDocumentScope(toolCtx(), "s-prec-3", &types.DocumentScope{DocumentIDs: []string{"ws-gone"}, SetBy: types.DocumentScopeSetByUser})
	if _, scope := ApplyDocumentScope(toolCtx(), ws, 7, "s-prec-3", DocumentRouteInput{Query: "còn gì nữa"}); scope != nil {
		t.Fatalf("stale scope: %+v", scope)
	}
}

func TestDocumentScopeStoreRoundTrip(t *testing.T) {
	s := &types.DocumentScope{DocumentIDs: []string{"a"}, Task: types.DocumentScopeTaskLookup, SetBy: types.DocumentScopeSetByUser}
	SetSessionDocumentScope(toolCtx(), "s-store", s)
	got := SessionDocumentScope(toolCtx(), "s-store")
	if got == nil || got == s || got.Task != types.DocumentScopeTaskLookup {
		t.Fatalf("round trip: %+v", got)
	}
	got.Task = "x"
	if SessionDocumentScope(toolCtx(), "s-store").Task != types.DocumentScopeTaskLookup {
		t.Fatal("the stored scope is a copy")
	}
	ClearSessionDocumentScope(toolCtx(), "s-store")
	if SessionDocumentScope(toolCtx(), "s-store") != nil {
		t.Fatal("cleared")
	}
}

func TestBuildOpenDocumentPromptInjectsTheScope(t *testing.T) {
	freshDocProfiles(t)
	ws := routeWorkspace(t)
	_, scope := ApplyDocumentScope(toolCtx(), ws, 7, "s-scope-1", DocumentRouteInput{Query: "chi đầu tư phát triển bao nhiêu, đối chiếu"})
	if scope == nil || len(scope.Sections) != 1 {
		t.Fatalf("scope: %+v", scope)
	}
	ctx := types.WithDocumentScope(context.Background(), scope)
	got := BuildOpenDocumentPrompt(ctx, ws, 7, "s-scope-1", "chi đầu tư phát triển bao nhiêu")
	for _, want := range []string{
		"Phạm vi hiện tại: vb2 (Điều 2. Chi đầu tư [3–4]) · đối chiếu (do bộ định tuyến chọn)\n",
		`<document_sections handle="vb2" name="bao-cao.docx" role="văn bản làm việc" unit="đoạn">`,
		"### Điều 2. Chi đầu tư [3–4]\n[3] Điều 2. Chi đầu tư\n[4] Chi đầu tư phát triển 430 tỷ đồng",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "1.250 tỷ") || strings.Contains(got, "<relevant_passages handle") || strings.Contains(got, "<open_document handle") {
		t.Fatalf("only the scoped section is injected:\n%s", got)
	}

	// a scoped target without sections is injected whole
	whole := &types.DocumentScope{DocumentIDs: []string{"ws-1"}, SetBy: types.DocumentScopeSetByUser}
	got = BuildOpenDocumentPrompt(types.WithDocumentScope(context.Background(), whole), ws, 7, "s-scope-1", "")
	if !strings.Contains(got, `<open_document handle="vb1"`) || strings.Contains(got, `handle="vb2"`) ||
		!strings.Contains(got, "Phạm vi hiện tại: vb1 (do người dùng chọn)") {
		t.Fatalf("whole scoped target:\n%s", got)
	}
	// an @ wins over the scope
	named := types.WithMentionedDocuments(types.WithDocumentScope(context.Background(), whole), []string{"ws-2"})
	if got = BuildOpenDocumentPrompt(named, ws, 7, "s-scope-1", ""); strings.Contains(got, "Phạm vi hiện tại") || !strings.Contains(got, `<open_document handle="vb2"`) {
		t.Fatalf("@ over scope:\n%s", got)
	}
}
