package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// fakeLegalBasisCatalog is an in-memory catalogue: rows and incoming edges
// (to id → links).
type fakeLegalBasisCatalog struct {
	docs  []*types.LegalDocument
	links map[string][]types.LegalRelationLink
	calls int
}

func (f *fakeLegalBasisCatalog) FindByNumberKey(_ context.Context, key string, limit int) ([]*types.LegalDocument, error) {
	f.calls++
	var out []*types.LegalDocument
	for _, d := range f.docs {
		if d.NumberKey == key {
			out = append(out, d)
		}
	}
	if len(out) > 0 {
		return out, nil
	}
	var prefixed []*types.LegalDocument
	for _, d := range f.docs {
		if strings.HasPrefix(d.NumberKey, key+"/") {
			prefixed = append(prefixed, d)
		}
	}
	return prefixed, nil
}

func (f *fakeLegalBasisCatalog) FindByNumberPrefixTypes(_ context.Context, key string, docTypes []string, limit int) ([]*types.LegalDocument, error) {
	var out []*types.LegalDocument
	for _, d := range f.docs {
		if !strings.HasPrefix(d.NumberKey, key+"/") {
			continue
		}
		for _, t := range docTypes {
			if strings.EqualFold(t, d.DocType) {
				out = append(out, d)
				break
			}
		}
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

func (f *fakeLegalBasisCatalog) ListLinks(_ context.Context, id string, _, incoming []types.LegalRelation) ([]types.LegalRelationLink, error) {
	var out []types.LegalRelationLink
	for _, l := range f.links[id] {
		for _, r := range incoming {
			if l.Relation == r && l.Incoming {
				out = append(out, l)
			}
		}
	}
	return out, nil
}

func lbDay(s string) *time.Time {
	d, _ := time.Parse("2006-01-02", s)
	return &d
}

func lbDoc(id, number, docType, status string, issued string) *types.LegalDocument {
	return &types.LegalDocument{
		ID: id, DocumentNumber: number, NumberKey: vietnamese_legal.CatalogNumberKey(number), DocType: docType,
		Title: docType + " " + number, Status: status, IssuedOn: lbDay(issued), Source: types.LegalDocumentSourceVBPL,
	}
}

func newLegalBasisFixture() *fakeLegalBasisCatalog {
	nd30 := lbDoc("nd30", "30/2020/NĐ-CP", "Nghị định", "Còn hiệu lực", "2020-03-05")
	tt01 := lbDoc("tt01", "01/2011/TT-BNV", "Thông tư", "Hết hiệu lực toàn bộ", "2011-01-19")
	tt01.ExpiredOn = lbDay("2020-03-05")
	nd123 := lbDoc("nd123", "123/2020/NĐ-CP", "Nghị định", "Còn hiệu lực", "2020-10-19")
	nd70 := lbDoc("nd70", "70/2025/NĐ-CP", "Nghị định", "Còn hiệu lực", "2025-03-20")
	nd70.EffectiveOn = lbDay("2025-06-01")
	// a stale status: replaced by a law already in force
	l24 := lbDoc("l24", "24/2018/QH14", "Luật", "Còn hiệu lực", "2018-06-12")
	l116 := lbDoc("l116", "116/2025/QH15", "Luật", "Còn hiệu lực", "2025-12-10")
	l116.EffectiveOn = lbDay("2026-07-01")
	// two provinces' decisions share a number
	qdA := lbDoc("qdA", "12/2024/QĐ-UBND", "Quyết định", "Còn hiệu lực", "2024-03-01")
	qdB := lbDoc("qdB", "12/2024/QĐ-UBND", "Quyết định", "Hết hiệu lực toàn bộ", "2024-05-01")
	future := lbDoc("nq9", "09/2026/NQ-CP", "Nghị quyết", "Chưa có hiệu lực", "2026-11-01")
	future.EffectiveOn = lbDay("2027-01-01")
	partial := lbDoc("nd15", "15/2018/NĐ-CP", "Nghị định", "Hết hiệu lực một phần", "2018-02-02")
	unknown := lbDoc("tt7", "07/2019/TT-BTC", "Thông tư", "Chưa xác định", "2019-02-01")
	nd68a := lbDoc("nd68a", "68/2019/NĐ-CP", "Nghị định", "Còn hiệu lực", "2019-08-14")
	nd68b := lbDoc("nd68b", "68/2026/NĐ-CP", "Nghị định", "Còn hiệu lực", "2026-03-01")
	return &fakeLegalBasisCatalog{
		docs: []*types.LegalDocument{nd30, tt01, nd123, nd70, l24, l116, qdA, qdB, future, partial, unknown, nd68a, nd68b},
		links: map[string][]types.LegalRelationLink{
			"tt01":  {{Relation: types.LegalRelationReplaces, Incoming: true, Other: nd30}},
			"nd123": {{Relation: types.LegalRelationAmends, Incoming: true, Other: nd70}},
			"l24":   {{Relation: types.LegalRelationReplaces, Incoming: true, Other: l116}},
		},
	}
}

func findingFor(t *testing.T, r *LegalBasisReport, citation string) LegalBasisFinding {
	t.Helper()
	for _, f := range r.Findings {
		if f.Citation == citation {
			return f
		}
	}
	var got []string
	for _, f := range r.Findings {
		got = append(got, f.Citation)
	}
	t.Fatalf("no finding %q in %q", citation, got)
	return LegalBasisFinding{}
}

func TestCheckLegalBasisStatuses(t *testing.T) {
	paras := []string{
		"ỦY BAN NHÂN DÂN TỈNH X",
		"Số: 15/2026/QĐ-UBND",
		"Căn cứ Nghị định số 30/2020/NĐ-CP ngày 05/3/2020 của Chính phủ về công tác văn thư;",
		"Căn cứ Thông tư 01/2011/TT-BNV hướng dẫn thể thức;",
		"Căn cứ Nghị định 123/2020/NĐ-CP; Luật An ninh mạng 24/2018/QH14;",
		"Căn cứ Quyết định 12/2024/QĐ-UBND; Nghị quyết 09/2026/NQ-CP; Nghị định 15/2018/NĐ-CP; Thông tư 07/2019/TT-BTC;",
		"Căn cứ Nghị định 99/2025/NĐ-CP; Kế hoạch 45/KH-UBND; Nghị định 68; Luật Đất đai;",
		"Theo Nghị định 30/2020/NĐ-CP và Nghị định 30/2020/NĐ-CP.",
	}
	now := time.Date(2026, 11, 11, 9, 0, 0, 0, lbVN)
	r, err := CheckLegalBasis(context.Background(), paras, newLegalBasisFixture(), nil, now)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]LegalBasisStatus{
		"30/2020/NĐ-CP":   LegalBasisInForce,
		"01/2011/TT-BNV":  LegalBasisExpired,
		"123/2020/NĐ-CP":  LegalBasisAmended,
		"24/2018/QH14":    LegalBasisExpired,
		"12/2024/QĐ-UBND": LegalBasisAmbiguous,
		"09/2026/NQ-CP":   LegalBasisNotYetInForce,
		"15/2018/NĐ-CP":   LegalBasisPartlyExpired,
		"07/2019/TT-BTC":  LegalBasisUnknown,
		"99/2025/NĐ-CP":   LegalBasisNotFound,
		"Nghị định 68":    LegalBasisAmbiguous,
	}
	for c, want := range cases {
		if f := findingFor(t, r, c); f.Status != want {
			t.Errorf("%s: status %s, want %s (%s)", c, f.Status, want, f.Note)
		}
	}
	if len(r.Findings) != len(cases) {
		t.Errorf("%d findings, want %d", len(r.Findings), len(cases))
	}
	// the own number and paperwork are left out (names need a resolver)
	if r.Skipped.OwnNumber != 1 || r.Skipped.Paperwork != 1 || r.Skipped.NamesWithoutYear != 0 {
		t.Errorf("skipped %+v", r.Skipped)
	}
	// expired first, in force last
	if r.Findings[0].Status != LegalBasisExpired || r.Findings[len(r.Findings)-1].Status != LegalBasisInForce {
		t.Errorf("order: first %s, last %s", r.Findings[0].Status, r.Findings[len(r.Findings)-1].Status)
	}

	tt := findingFor(t, r, "01/2011/TT-BNV")
	if len(tt.Related) != 1 || tt.Related[0].Relation != LegalBasisReplacedBy || tt.Related[0].DocumentNumber != "30/2020/NĐ-CP" ||
		tt.Related[0].Status != LegalBasisInForce {
		t.Errorf("replaced by: %+v", tt.Related)
	}
	if !strings.Contains(tt.Note, "đã hết hiệu lực từ ngày 05/03/2020") || !strings.Contains(tt.Note, "Nghị định 30/2020/NĐ-CP") {
		t.Errorf("note: %s", tt.Note)
	}
	l24 := findingFor(t, r, "24/2018/QH14")
	if l24.DerivedFrom != "116/2025/QH15" || l24.DerivedExpiredOn != "2026-07-01" || l24.Document.CatalogStatus != "Còn hiệu lực" {
		t.Errorf("derived expiry: %+v", l24)
	}
	amended := findingFor(t, r, "123/2020/NĐ-CP")
	if len(amended.Related) != 1 || amended.Related[0].Relation != LegalBasisAmendedBy {
		t.Errorf("amended by: %+v", amended.Related)
	}
	amb := findingFor(t, r, "12/2024/QĐ-UBND")
	if amb.Document != nil || amb.CandidatesTotal != 2 || len(amb.Candidates) != 2 ||
		amb.Candidates[0].ID != "qdB" || amb.Candidates[0].Status != LegalBasisExpired || amb.Candidates[1].Status != LegalBasisInForce {
		t.Errorf("ambiguous: %+v", amb)
	}
	nf := findingFor(t, r, "99/2025/NĐ-CP")
	if strings.Contains(nf.Note, "hết hiệu lực") || !strings.Contains(nf.Note, "chưa rõ") {
		t.Errorf("not found must never read as expired: %s", nf.Note)
	}
	bare := findingFor(t, r, "Nghị định 68")
	if !bare.Incomplete || bare.CandidatesTotal != 2 {
		t.Errorf("bare number: %+v", bare)
	}

	// one finding for the three places of 30/2020/NĐ-CP, each addressed
	nd30 := findingFor(t, r, "30/2020/NĐ-CP")
	if len(nd30.Positions) != 3 {
		t.Fatalf("positions: %+v", nd30.Positions)
	}
	p := nd30.Positions[2]
	if p.Paragraph != 7 || p.TextOccurrence != 2 || p.Text != "30/2020/NĐ-CP" || p.Anchor == nil || p.Anchor.Text != paras[7] {
		t.Errorf("third position: %+v", p)
	}
	if got := string([]rune(paras[p.Paragraph])[p.Start:p.End]); got != p.Text {
		t.Errorf("offsets give %q", got)
	}

	// marks: every place of the findings whose validity is in question,
	// never an edit; ambiguous / unknown / not found only on request
	marks := 0
	for _, f := range r.Findings {
		if LegalBasisMarkedByDefault(f.Status) {
			marks += len(f.Positions)
		}
	}
	if marks != 4 { // 01/2011, 24/2018, 09/2026, 15/2018
		t.Errorf("%d default marks", marks)
	}
	if len(r.DocumentOps) != marks {
		t.Errorf("%d ops, want %d", len(r.DocumentOps), marks)
	}
	for _, op := range r.DocumentOps {
		if op.Op != OpMark || op.Style != "underline" || op.New != nil || op.Old != nil || op.Text == "" {
			t.Errorf("not a mark: %+v", op)
		}
	}
	if r.Summary[LegalBasisExpired] != 2 || r.Summary[LegalBasisAmbiguous] != 2 {
		t.Errorf("summary %+v", r.Summary)
	}
	if _, err := json.Marshal(r); err != nil {
		t.Fatal(err)
	}
}

func TestCheckLegalBasisPaperworkAndNames(t *testing.T) {
	cat := newLegalBasisFixture()
	l13 := lbDoc("l13", "45/2013/QH13", "Luật", "Hết hiệu lực toàn bộ", "2013-11-29")
	l13.Title = "Luật Đất đai"
	l13.ExpiredOn = lbDay("2024-08-01")
	cat.docs = append(cat.docs, l13)
	var asked []string
	names := func(_ context.Context, name, year string) (LegalNameResolution, error) {
		asked = append(asked, name+"|"+year)
		if name == "Luật Đất đai" && year == "2013" {
			return LegalNameResolution{Chosen: l13}, nil
		}
		return LegalNameResolution{}, nil
	}
	paras := []string{
		"Căn cứ Luật Đất đai năm 2013 và Luật Đất đai số 45/2013/QH13;",
		"Căn cứ Luật Đất đai; Kế hoạch số 45/KH-UBND; Tờ trình 12/2025/TTr-STP; Luật Thủ đô 2024.",
	}
	r, err := CheckLegalBasis(context.Background(), paras, cat, names, time.Date(2026, 11, 11, 0, 0, 0, 0, lbVN))
	if err != nil {
		t.Fatal(err)
	}
	// the name and the number of the same law are one finding
	f := findingFor(t, r, "Luật Đất đai 2013")
	if f.Status != LegalBasisExpired || len(f.Positions) != 2 {
		t.Errorf("Luật Đất đai 2013: %+v", f)
	}
	if nf := findingFor(t, r, "Luật Thủ đô 2024"); nf.Status != LegalBasisNotFound {
		t.Errorf("Luật Thủ đô 2024: %s", nf.Status)
	}
	if r.Skipped.NamesWithoutYear != 1 || r.Skipped.Paperwork != 2 {
		t.Errorf("skipped %+v", r.Skipped)
	}
	if len(r.Findings) != 2 {
		t.Errorf("findings %+v", r.Findings)
	}
	if strings.Join(asked, ",") != "Luật Đất đai|2013,Luật Thủ đô|2024" {
		t.Errorf("names asked %q", asked)
	}
}

func TestClassifyLegalDocumentDates(t *testing.T) {
	now := time.Date(2026, 11, 11, 23, 30, 0, 0, lbVN)
	d := lbDoc("x", "1/2020/NĐ-CP", "Nghị định", "Còn hiệu lực", "2020-01-01")
	d.ExpiredOn = lbDay("2026-11-11") // expires today: no longer in force
	if s := ClassifyLegalDocument(&types.LegalStatusReport{Document: d}, now); s != LegalBasisExpired {
		t.Errorf("expired today: %s", s)
	}
	d.ExpiredOn = lbDay("2026-11-12")
	if s := ClassifyLegalDocument(&types.LegalStatusReport{Document: d}, now); s != LegalBasisInForce {
		t.Errorf("expires tomorrow: %s", s)
	}
	d.ExpiredOn, d.Status = nil, "Tạm ngưng hiệu lực"
	if s := ClassifyLegalDocument(&types.LegalStatusReport{Document: d}, now); s != LegalBasisSuspended {
		t.Errorf("suspended: %s", s)
	}
	// a replacement not yet in force leaves the document in force
	d.Status = "Còn hiệu lực"
	next := lbDoc("y", "2/2026/NĐ-CP", "Nghị định", "Còn hiệu lực", "2026-10-01")
	next.EffectiveOn = lbDay("2027-01-01")
	rep := &types.LegalStatusReport{Document: d, Links: []types.LegalRelationLink{{Relation: types.LegalRelationReplaces, Incoming: true, Other: next}}}
	if s := ClassifyLegalDocument(rep, now); s != LegalBasisInForce {
		t.Errorf("pending replacement: %s", s)
	}
}
