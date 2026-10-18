package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// gateCatalog answers Lookup from a fixed table keyed by number key.
type gateCatalog struct {
	interfaces.LegalCatalogService
	reports map[string][]*types.LegalStatusReport
}

func (c *gateCatalog) Lookup(_ context.Context, number, _ string, _ int) ([]*types.LegalStatusReport, error) {
	key := strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(number, "đ", "d"), "Đ", "D"))
	return c.reports[key], nil
}

// gateRequests keeps requests in memory; held answers KnowledgeNamed.
type gateRequests struct {
	interfaces.LegalRequestRepository
	held     bool
	ingested map[string]bool
	needles  []string
	recorded []*types.LegalDocumentRequest
	stored   map[string]*types.LegalDocumentRequest
}

func (r *gateRequests) KnowledgeNamed(_ context.Context, _ []string, needles []string) (bool, error) {
	r.needles = needles
	return r.held, nil
}

func (r *gateRequests) PublicKnowledgeByExternalIDs(_ context.Context, ids []string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, id := range ids {
		if r.ingested[id] {
			out[id] = true
		}
	}
	return out, nil
}

func (r *gateRequests) Record(_ context.Context, req *types.LegalDocumentRequest) (*types.LegalDocumentRequest, error) {
	r.recorded = append(r.recorded, req)
	if r.stored == nil {
		r.stored = map[string]*types.LegalDocumentRequest{}
	}
	k := req.NumberKey + "|" + req.LegalDocumentID
	if row, ok := r.stored[k]; ok {
		if row.Status == types.LegalRequestRequested && row.LastSessionID != req.LastSessionID {
			row.RequestCount++
			row.LastSessionID = req.LastSessionID
		}
		return row, nil
	}
	row := *req
	row.ID, row.Status, row.RequestCount = "r"+req.LegalDocumentID, types.LegalRequestRequested, 1
	r.stored[k] = &row
	return &row, nil
}

type gateReviews struct {
	interfaces.LegalReviewRepository
	approved map[string]bool
}

func (r *gateReviews) ApprovedDocumentIDs(_ context.Context, ids []string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, id := range ids {
		if r.approved[id] {
			out[id] = true
		}
	}
	return out, nil
}

type gateOutcome struct {
	handled bool
	ctx     context.Context
	answer  string
	data    map[string]interface{}
}

func runGate(t *testing.T, s *sessionService, query, session string) gateOutcome {
	t.Helper()
	bus := event.NewEventBus()
	var out gateOutcome
	bus.On(event.EventAgentFinalAnswer, func(_ context.Context, e event.Event) error {
		if d, ok := e.Data.(event.AgentFinalAnswerData); ok && d.Content != "" {
			out.answer = d.Content
		}
		return nil
	})
	bus.On(event.EventAgentToolResult, func(_ context.Context, e event.Event) error {
		if d, ok := e.Data.(event.AgentToolResultData); ok {
			out.data = d.Data
		}
		return nil
	})
	req := &types.QARequest{Query: query, Session: &types.Session{ID: session, TenantID: 10003},
		AssistantMessageID: "m-" + session, CustomAgent: &types.CustomAgent{ID: types.BuiltinSmartReasoningID}}
	ctx, handled, err := s.legalMissingDocumentGate(context.Background(), req, bus, []string{"kb-own"})
	if err != nil {
		t.Fatalf("gate: %v", err)
	}
	out.handled, out.ctx = handled, ctx
	return out
}

func TestLegalMissingDocumentGate(t *testing.T) {
	day := func(s string) *time.Time { d, _ := time.Parse("2006-01-02", s); return &d }
	nd53 := &types.LegalDocument{ID: "155366", DocumentNumber: "53/2022/NĐ-CP", DocType: "Nghị định",
		Title: "Quy định chi tiết một số điều của Luật An ninh mạng", Status: "Còn hiệu lực", IssuedOn: day("2022-08-15")}
	old := &types.LegalDocument{ID: "1001", DocumentNumber: "27/2015/NĐ-CP", DocType: "Nghị định",
		Title: "Quy định về bầu thi đua", Status: "Hết hiệu lực toàn bộ", ExpiredOn: day("2023-01-01")}
	newer := &types.LegalDocument{ID: "2002", DocumentNumber: "98/2023/NĐ-CP", DocType: "Nghị định",
		Title: "Quy định chi tiết Luật Thi đua, khen thưởng", Status: "Còn hiệu lực"}
	tt := &types.LegalDocument{ID: "3003", DocumentNumber: "01/2024/TT-BCA", DocType: "Thông tư", Title: "Thông tư X", Status: "Còn hiệu lực"}
	catalog := &gateCatalog{reports: map[string][]*types.LegalStatusReport{
		"53/2022/ND-CP":  {{Document: nd53}},
		"27/2015/ND-CP":  {{Document: old, Links: []types.LegalRelationLink{{Relation: types.LegalRelationReplaces, Incoming: true, Other: newer}}}},
		"01/2024/TT-BCA": {{Document: tt}},
	}}
	newSvc := func() (*sessionService, *gateRequests, *gateReviews) {
		req, rev := &gateRequests{}, &gateReviews{approved: map[string]bool{}}
		s := &sessionService{legalCatalog: catalog}
		s.SetLegalRequests(req, rev)
		return s, req, rev
	}

	t.Run("catalogue hit not held: requested card", func(t *testing.T) {
		s, reqs, _ := newSvc()
		out := runGate(t, s, "Nghị định 53/2022/NĐ-CP quy định những gì về lưu trữ dữ liệu?", "s1")
		if !out.handled || out.data["display_type"] != tools.LegalRequestType || out.data["mode"] != tools.LegalRequestModeRequested {
			t.Fatalf("outcome = %+v", out)
		}
		if len(reqs.recorded) != 1 || reqs.recorded[0].LegalDocumentID != "155366" || reqs.recorded[0].Source != types.LegalRequestSourceCatalogue ||
			reqs.recorded[0].LastTenantID != 10003 || reqs.recorded[0].DocType != "Nghị định" {
			t.Fatalf("recorded = %+v", reqs.recorded)
		}
		if !strings.Contains(out.answer, "chưa có toàn văn Nghị định 53/2022/NĐ-CP") || strings.Contains(out.answer, "lần thứ") {
			t.Fatalf("answer = %q", out.answer)
		}
		docs, _ := out.data["documents"].([]map[string]interface{})
		if len(docs) != 1 || docs[0]["url"] != "https://vbpl.vn/van-ban/chi-tiet/155366" || docs[0]["issued_on"] != "2022-08-15" {
			t.Fatalf("documents = %v", out.data["documents"])
		}
		// spellings a title or file name may carry
		joined := strings.Join(reqs.needles, " ")
		for _, want := range []string{"53/2022/nđ-cp", "53/2022/nd-cp", "53-2022-nđ-cp", "53-2022-nd-cp"} {
			if !strings.Contains(joined, want) {
				t.Fatalf("needles %v lack %q", reqs.needles, want)
			}
		}
		// another session asks: second request
		out = runGate(t, s, "số 53/2022/NĐ-CP nói gì", "s2")
		if !out.handled || out.data["request_count"] != 2 || !strings.Contains(out.answer, "(lần thứ 2)") {
			t.Fatalf("second = %+v", out)
		}
	})

	t.Run("approved public copy: no card", func(t *testing.T) {
		s, reqs, rev := newSvc()
		rev.approved["155366"] = true
		if out := runGate(t, s, "Nghị định 53/2022/NĐ-CP quy định gì?", "s1"); out.handled || len(reqs.recorded) != 0 {
			t.Fatalf("outcome = %+v recorded %v", out, reqs.recorded)
		}
	})

	t.Run("ingested into a public KB, not linked yet: no card", func(t *testing.T) {
		s, reqs, _ := newSvc()
		reqs.ingested = map[string]bool{"155366": true}
		if out := runGate(t, s, "Nghị định 53/2022/NĐ-CP quy định gì?", "s1"); out.handled || len(reqs.recorded) != 0 {
			t.Fatalf("outcome = %+v recorded %v", out, reqs.recorded)
		}
	})

	t.Run("drafting text that cites it: recorded, no card", func(t *testing.T) {
		s, reqs, _ := newSvc()
		for i, q := range []string{
			"Soạn tờ trình căn cứ Nghị định 53/2022/NĐ-CP về bảo vệ dữ liệu",
			"Hãy viết công văn trả lời Thông tư 01/2024/TT-BCA",
			"giúp tôi kiểm tra chính tả văn bản theo Nghị định 53/2022/NĐ-CP",
			"Làm giúp tôi một báo cáo thực hiện Nghị định 27/2015/NĐ-CP",
		} {
			out := runGate(t, s, q, "s"+string(rune('a'+i)))
			if out.handled {
				t.Fatalf("%q handled with a card", q)
			}
			if i < 2 && types.LegalRequestRecordedFromContext(out.ctx) == "" {
				t.Fatalf("%q: ctx not flagged", q)
			}
		}
		// the expired one is not recorded; the other three are
		if len(reqs.recorded) != 3 {
			t.Fatalf("recorded = %+v", reqs.recorded)
		}
		// a content question still gets the card
		if out := runGate(t, s, "Nghị định 53/2022/NĐ-CP quy định gì", "s9"); !out.handled {
			t.Fatalf("content question not handled: %+v", out)
		}
	})

	t.Run("own paperwork number unknown to the catalogue: nothing recorded", func(t *testing.T) {
		s, reqs, _ := newSvc()
		for _, q := range []string{"Tờ trình 45/2025/TTr-CAT nói gì?", "Công văn 123/2025/CV-PV01 yêu cầu gì?",
			"Kế hoạch 10/2025/KH-UBND gồm những gì?"} {
			out := runGate(t, s, q, "s1")
			if out.handled || types.LegalRequestRecordedFromContext(out.ctx) != "" {
				t.Fatalf("%q: %+v", q, out)
			}
		}
		if len(reqs.recorded) != 0 {
			t.Fatalf("recorded = %+v", reqs.recorded)
		}
	})

	t.Run("own KB holds it: no card", func(t *testing.T) {
		s, reqs, _ := newSvc()
		reqs.held = true
		if out := runGate(t, s, "Thông tư 01/2024/TT-BCA quy định gì?", "s1"); out.handled || len(reqs.recorded) != 0 {
			t.Fatalf("outcome = %+v", out)
		}
	})

	t.Run("expired: expired card naming the replacement", func(t *testing.T) {
		s, reqs, _ := newSvc()
		out := runGate(t, s, "Nghị định 27/2015/NĐ-CP quy định gì về thi đua?", "s1")
		if !out.handled || out.data["mode"] != tools.LegalRequestModeExpired || len(reqs.recorded) != 0 {
			t.Fatalf("outcome = %+v recorded %v", out, reqs.recorded)
		}
		repl, _ := out.data["replacement"].(map[string]interface{})
		if repl["document_number"] != "98/2023/NĐ-CP" || !strings.Contains(out.answer, "98/2023/NĐ-CP") ||
			!strings.Contains(out.answer, "từ ngày 01/01/2023") {
			t.Fatalf("answer = %q replacement %v", out.answer, repl)
		}
	})

	t.Run("validity question: no card", func(t *testing.T) {
		s, reqs, _ := newSvc()
		for _, q := range []string{"Nghị định 53/2022/NĐ-CP còn hiệu lực không?", "Ai ký Nghị định 53/2022/NĐ-CP?",
			"Nghị định 53/2022/NĐ-CP ban hành ngày nào?"} {
			if out := runGate(t, s, q, "s1"); out.handled {
				t.Fatalf("%q handled", q)
			}
		}
		if len(reqs.recorded) != 0 {
			t.Fatalf("recorded = %v", reqs.recorded)
		}
	})

	t.Run("rejected request: rejected card", func(t *testing.T) {
		s, reqs, _ := newSvc()
		reqs.stored = map[string]*types.LegalDocumentRequest{"01/2024/TT-BCA|3003": {ID: "r1", Status: types.LegalRequestRejected,
			RequestCount: 4, LastSessionID: "old", DecisionNote: "Văn bản mật, không công bố"}}
		out := runGate(t, s, "Thông tư số 01/2024/TT-BCA quy định gì", "s1")
		if !out.handled || out.data["mode"] != tools.LegalRequestModeRejected || out.data["decision_note"] != "Văn bản mật, không công bố" ||
			!strings.Contains(out.answer, "Văn bản mật, không công bố.") || reqs.stored["01/2024/TT-BCA|3003"].RequestCount != 4 {
			t.Fatalf("outcome = %+v", out)
		}
	})

	t.Run("catalogue miss: request recorded, turn runs, ctx flagged", func(t *testing.T) {
		s, reqs, _ := newSvc()
		out := runGate(t, s, "Quyết định 12/2024/QĐ-UBND tỉnh Huế quy định gì?", "s1")
		if out.handled || len(reqs.recorded) != 1 || reqs.recorded[0].LegalDocumentID != "" || reqs.recorded[0].Source != "" ||
			reqs.recorded[0].NumberKey != "12/2024/QD-UBND" || reqs.recorded[0].DocType != "Quyết định" {
			t.Fatalf("outcome = %+v recorded %+v", out, reqs.recorded)
		}
		if got := types.LegalRequestRecordedFromContext(out.ctx); got != "12/2024/QĐ-UBND" {
			t.Fatalf("ctx flag = %q", got)
		}
	})

	t.Run("not applicable", func(t *testing.T) {
		s, reqs, _ := newSvc()
		bus := event.NewEventBus()
		base := func() *types.QARequest {
			return &types.QARequest{Query: "Nghị định 53/2022/NĐ-CP quy định gì?", Session: &types.Session{ID: "s"},
				CustomAgent: &types.CustomAgent{ID: types.BuiltinSmartReasoningID}}
		}
		withSel := base()
		withSel.DocumentSelection = &types.DocumentSelection{Text: "đoạn văn"}
		withAtt := base()
		withAtt.Attachments = types.MessageAttachments{{ID: "a", FileName: "nd53.pdf"}}
		for name, c := range map[string]struct {
			req *types.QARequest
			kbs []string
		}{
			"no KB": {base(), nil}, "selection": {withSel, []string{"kb"}}, "attachment": {withAtt, []string{"kb"}},
			"bare number": {&types.QARequest{Query: "Nghị định 53 quy định gì?", Session: &types.Session{ID: "s"},
				CustomAgent: &types.CustomAgent{ID: "x"}}, []string{"kb"}},
		} {
			if _, handled, _ := s.legalMissingDocumentGate(context.Background(), c.req, bus, c.kbs); handled {
				t.Fatalf("%s: handled", name)
			}
		}
		if len(reqs.recorded) != 0 {
			t.Fatalf("recorded = %v", reqs.recorded)
		}
	})
}

func TestLegalDocTypeBefore(t *testing.T) {
	for prefix, want := range map[string]string{
		"Hỏi về Nghị định số ": "Nghị định",
		"thông tư liên tịch ":  "Thông tư liên tịch",
		"Theo Thông tư ":       "Thông tư",
		"Văn bản ":             "",
		"QUYẾT ĐỊNH SỐ ":       "Quyết định",
	} {
		if got := tools.LegalDocTypeBefore(prefix); got != want {
			t.Errorf("LegalDocTypeBefore(%q) = %q, want %q", prefix, got, want)
		}
	}
}
