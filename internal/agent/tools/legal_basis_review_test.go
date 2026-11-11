package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"golang.org/x/text/unicode/norm"

	"github.com/Tencent/WeKnora/internal/types"
)

// Cases from the Fable review of F1 (2026-11-11) and its probe.

var lbNow = time.Date(2026, 11, 11, 9, 0, 0, 0, time.Local)

func lbCheck(t *testing.T, cat *fakeLegalBasisCatalog, names LegalNameResolver, paras ...string) *LegalBasisReport {
	t.Helper()
	r, err := CheckLegalBasis(context.Background(), paras, cat, names, lbNow)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func lbMarked(r *LegalBasisReport, text string) bool {
	for _, op := range r.DocumentOps {
		if op.Text == text {
			return true
		}
	}
	return false
}

// H1: a year-less number restarts every year — a lone catalogue row of the
// number is the cited document only when the issue date written after it
// is that row's.
func TestLegalBasisYearlessNumbersNeedTheirIssueDate(t *testing.T) {
	cat := newLegalBasisFixture()
	qd := lbDoc("qd1461", "1461/QĐ-TTg", "Quyết định", "Hết hiệu lực toàn bộ", "2015-08-01")
	nq := lbDoc("nq18", "18-NQ/TW", "Nghị quyết", "Còn hiệu lực", "2022-06-16")
	cat.docs = append(cat.docs, qd, nq)

	r := lbCheck(t, cat, nil, "Căn cứ Quyết định số 1461/QĐ-TTg ngày 20/11/2023 của Thủ tướng Chính phủ;")
	f := findingFor(t, r, "1461/QĐ-TTg ngày 20/11/2023")
	if f.Status != LegalBasisAmbiguous || f.Ambiguity != LegalBasisAmbiguityDateMismatch || f.Document != nil ||
		len(f.Candidates) != 1 || f.Candidates[0].ID != "qd1461" || f.IssuedOn != "2023-11-20" {
		t.Fatalf("2023 decision must not be the 2015 row: %+v", f)
	}
	if strings.Contains(f.Note, "hết hiệu lực") || lbMarked(r, "1461/QĐ-TTg") {
		t.Errorf("never called expired nor underlined by default: %s", f.Note)
	}

	for _, written := range []string{"ngày 01/8/2015", "ngày 1-8-2015", "ngày 01 tháng 8 năm 2015", ", ban hành ngày 1/08/2015"} {
		r = lbCheck(t, cat, nil, "Căn cứ Quyết định số 1461/QĐ-TTg "+written+";")
		if len(r.Findings) != 1 || r.Findings[0].Status != LegalBasisExpired || r.Findings[0].Document.ID != "qd1461" {
			t.Errorf("%q: %+v", written, r.Findings)
		}
	}

	// no date: ambiguous, incomplete, even with one candidate
	r = lbCheck(t, cat, nil, "Thực hiện Quyết định 1461/QĐ-TTg của Thủ tướng.")
	f = r.Findings[0]
	if f.Status != LegalBasisAmbiguous || f.Ambiguity != LegalBasisAmbiguityMissingDate || !f.Incomplete || f.CandidatesTotal != 1 {
		t.Errorf("undated: %+v", f)
	}
	if !strings.Contains(f.Note, "ngày ban hành") {
		t.Errorf("note: %s", f.Note)
	}

	// Party numbers likewise; a later undated mention is the dated one
	r = lbCheck(t, cat, nil,
		"Căn cứ Nghị quyết số 18-NQ/TW ngày 16/6/2022 của Hội nghị Trung ương;",
		"Quán triệt Nghị quyết 18-NQ/TW trong toàn ngành.")
	if len(r.Findings) != 1 {
		t.Fatalf("one finding: %+v", r.Findings)
	}
	f = r.Findings[0]
	if f.Kind != "party" || f.Status != LegalBasisInForce || len(f.Positions) != 2 || f.Document.ID != "nq18" {
		t.Errorf("party: %+v", f)
	}
	// an undated mention before any dated one stays apart (ambiguous)
	r = lbCheck(t, cat, nil, "Nghị quyết 18-NQ/TW.", "Nghị quyết số 18-NQ/TW ngày 16/6/2022.")
	if len(r.Findings) != 2 || r.Summary[LegalBasisAmbiguous] != 1 || r.Summary[LegalBasisInForce] != 1 {
		t.Errorf("undated first: %+v", r.Summary)
	}
	// a Party document the catalogue lacks: "chưa có trong danh mục"
	r = lbCheck(t, cat, nil, "Kết luận 21-KL/TW ngày 25/10/2021.")
	if len(r.Findings) != 1 || r.Findings[0].Status != LegalBasisNotFound {
		t.Errorf("party not found: %+v", r.Findings)
	}
}

// M2, M4, L1 and the other classifier gaps.
func TestLegalBasisClassifierReviewCases(t *testing.T) {
	cat := newLegalBasisFixture()
	repealer := lbDoc("rper", "6/2020/NĐ-CP", "Nghị định", "Còn hiệu lực", "2020-01-01")
	repealer.EffectiveOn = lbDay("2020-02-01")
	pending := lbDoc("pend", "60/2026/NĐ-CP", "Nghị định", "Chưa có hiệu lực", "2026-10-01")
	pending.EffectiveOn = lbDay("2027-01-01")

	repealed := lbDoc("rp", "5/2010/NĐ-CP", "Nghị định", "Còn hiệu lực", "2010-01-01")
	laterRepeal := lbDoc("rp2", "4/2010/NĐ-CP", "Nghị định", "Còn hiệu lực", "2010-01-01")
	partExp := lbDoc("pe", "7/2012/NĐ-CP", "Nghị định", "Hết hiệu lực một phần", "2012-01-01")
	partExp.ExpiredOn = lbDay("2021-01-01")
	partSusp := lbDoc("sp", "8/2012/NĐ-CP", "Nghị định", "Ngưng hiệu lực một phần", "2012-01-01")
	staleNotYet := lbDoc("st", "9/2012/NĐ-CP", "Nghị định", "Chưa có hiệu lực", "2012-01-01")
	staleNotYet.EffectiveOn = lbDay("2012-03-01")
	suspendedReplaced := lbDoc("sus", "10/2012/NĐ-CP", "Nghị định", "Ngưng hiệu lực", "2012-01-01")
	blankReplaced := lbDoc("blank", "11/2012/NĐ-CP", "Nghị định", "", "2012-01-01")
	blankRepealed := lbDoc("blank2", "12/2012/NĐ-CP", "Nghị định", "", "2012-01-01")
	partMaybe := lbDoc("pm", "13/2012/NĐ-CP", "Nghị định", "Hết hiệu lực một phần", "2012-01-01")
	soonReplaced := lbDoc("soon", "14/2012/NĐ-CP", "Nghị định", "Còn hiệu lực", "2012-01-01")
	cat.docs = append(cat.docs, repealer, pending, repealed, laterRepeal, partExp, partSusp, staleNotYet, suspendedReplaced,
		blankReplaced, blankRepealed, partMaybe, soonReplaced)
	in := func(rel types.LegalRelation, o *types.LegalDocument) []types.LegalRelationLink {
		return []types.LegalRelationLink{{Relation: rel, Incoming: true, Other: o}}
	}
	cat.links["rp"] = in(types.LegalRelationRepeals, repealer)
	cat.links["rp2"] = in(types.LegalRelationRepeals, pending)
	cat.links["sus"] = in(types.LegalRelationReplaces, repealer)
	cat.links["blank"] = in(types.LegalRelationReplaces, repealer)
	cat.links["blank2"] = in(types.LegalRelationPartiallyExpires, repealer)
	cat.links["pm"] = in(types.LegalRelationReplaces, repealer)
	cat.links["soon"] = in(types.LegalRelationReplaces, pending)

	r := lbCheck(t, cat, nil,
		"Nghị định 5/2010/NĐ-CP; Nghị định 4/2010/NĐ-CP; Nghị định 7/2012/NĐ-CP; Nghị định 8/2012/NĐ-CP; Nghị định 9/2012/NĐ-CP;",
		"Nghị định 10/2012/NĐ-CP; Nghị định 11/2012/NĐ-CP; Nghị định 12/2012/NĐ-CP; Nghị định 13/2012/NĐ-CP; Nghị định 14/2012/NĐ-CP.")
	want := map[string]LegalBasisStatus{
		"5/2010/NĐ-CP":  LegalBasisNeedsReview,   // M4: repealed by a document in force
		"4/2010/NĐ-CP":  LegalBasisInForce,       // the repealer is not in force yet
		"7/2012/NĐ-CP":  LegalBasisPartlyExpired, // M2: partial status wins over expired_on
		"8/2012/NĐ-CP":  LegalBasisNeedsReview,   // M2: partial suspension is not suspended
		"9/2012/NĐ-CP":  LegalBasisInForce,       // L1: stale "Chưa có hiệu lực"
		"10/2012/NĐ-CP": LegalBasisSuspended,     // status kept, replacer worded (M3)
		"11/2012/NĐ-CP": LegalBasisExpired,       // no status + replacer in force
		"12/2012/NĐ-CP": LegalBasisNeedsReview,   // no status + partial expiry in force
		"13/2012/NĐ-CP": LegalBasisPartlyExpired, // + replacer in force: maybe wholly
		"14/2012/NĐ-CP": LegalBasisInForce,       // replacer pending
	}
	for c, s := range want {
		if f := findingFor(t, r, c); f.Status != s {
			t.Errorf("%s: %s, want %s (%s)", c, f.Status, s, f.Note)
		}
	}
	f := findingFor(t, r, "5/2010/NĐ-CP")
	if len(f.ReviewBy) != 1 || !strings.Contains(f.Note, "Nghị định 6/2020/NĐ-CP") || !lbMarked(r, "5/2010/NĐ-CP") {
		t.Errorf("needs review names the repealer and is underlined: %+v", f)
	}
	if f := findingFor(t, r, "8/2012/NĐ-CP"); !f.PartialSuspension || !strings.Contains(f.Note, "ngưng hiệu lực một phần") {
		t.Errorf("partial suspension: %+v", f)
	}
	if f := findingFor(t, r, "9/2012/NĐ-CP"); !f.StaleStatus || !strings.Contains(f.Note, "Chưa có hiệu lực") {
		t.Errorf("stale status: %+v", f)
	}
	// M3: a replacer already in force is not "chưa có hiệu lực"
	if f := findingFor(t, r, "10/2012/NĐ-CP"); strings.Contains(f.Note, "chưa có hiệu lực") ||
		!strings.Contains(f.Note, "đã có hiệu lực từ ngày 01/02/2020") {
		t.Errorf("suspended + replacer in force: %s", f.Note)
	}
	if f := findingFor(t, r, "14/2012/NĐ-CP"); !strings.Contains(f.Note, "Sắp bị thay thế bởi Nghị định 60/2026/NĐ-CP (có hiệu lực từ ngày 01/01/2027)") {
		t.Errorf("pending replacer: %s", f.Note)
	}
	if f := findingFor(t, r, "12/2012/NĐ-CP"); strings.Contains(f.Note, "ghi còn hiệu lực") {
		t.Errorf("no status is not \"in force\": %s", f.Note)
	}
	if f := findingFor(t, r, "11/2012/NĐ-CP"); f.DerivedFrom != "6/2020/NĐ-CP" {
		t.Errorf("derived: %+v", f)
	}
	if f := findingFor(t, r, "13/2012/NĐ-CP"); !f.MaybeExpired || !strings.Contains(f.Note, "có thể đã hết hiệu lực toàn bộ") {
		t.Errorf("maybe expired: %+v", f)
	}
	// M5: the report's underlines are the questioned statuses only
	for _, op := range r.DocumentOps {
		for _, f := range r.Findings {
			for _, p := range f.Positions {
				if p.Text == op.Text && !LegalBasisMarkedByDefault(f.Status) {
					t.Errorf("%s (%s) underlined by default", p.Text, f.Status)
				}
			}
		}
	}

	// "Chưa có hiệu lực" without a date stays not yet effective
	d := lbDoc("x", "1/2026/NĐ-CP", "Nghị định", "Chưa có hiệu lực", "2026-11-01")
	if s := ClassifyLegalDocument(&types.LegalStatusReport{Document: d}, lbNow); s != LegalBasisNotYetEffective {
		t.Errorf("no effective date: %s", s)
	}
}

// L3: a law name the catalogue matches only by a shorter title.
func TestLegalBasisNameGuess(t *testing.T) {
	cat := newLegalBasisFixture()
	gd := lbDoc("gd", "43/2019/QH14", "Luật", "Còn hiệu lực", "2019-06-14")
	names := func(context.Context, string, string) (LegalNameResolution, error) {
		return LegalNameResolution{Candidates: []*types.LegalDocument{gd}, Guess: true}, nil
	}
	r := lbCheck(t, cat, names, "Căn cứ Luật Giáo dục nghề nghiệp mới 2019;")
	f := r.Findings[0]
	if f.Status != LegalBasisAmbiguous || f.Ambiguity != LegalBasisAmbiguityNameGuess || len(f.Candidates) != 1 ||
		strings.Contains(f.Note, "cùng số hiệu") {
		t.Errorf("name guess: %+v", f)
	}
}

// L2: a bare number matches only documents of its type, and more rows
// than the scan are said.
func TestLegalBasisPartialNumberTypeAndScan(t *testing.T) {
	cat := &fakeLegalBasisCatalog{links: map[string][]types.LegalRelationLink{}}
	cat.docs = append(cat.docs,
		lbDoc("nd", "68/2026/NĐ-CP", "Nghị định", "Còn hiệu lực", "2026-03-01"),
		lbDoc("tt", "68/2026/TT-BTC", "Thông tư", "Còn hiệu lực", "2026-04-01"))
	for i := 0; i < legalBasisPrefixScan+5; i++ {
		cat.docs = append(cat.docs, lbDoc(fmt.Sprintf("qd%d", i), fmt.Sprintf("7/%d/QĐ-UBND", 1800+i), "Quyết định", "Còn hiệu lực", "2020-01-01"))
	}
	r := lbCheck(t, cat, nil, "Theo NĐ 68/2026 và Quyết định 7.")
	if f := findingFor(t, r, "Nghị định 68/2026"); f.Status != LegalBasisInForce || f.Document.ID != "nd" || !f.Incomplete {
		t.Errorf("NĐ 68/2026: %+v", f)
	}
	f := findingFor(t, r, "Quyết định 7")
	if f.Status != LegalBasisAmbiguous || !f.CandidatesMore || f.CandidatesTotal != legalBasisPrefixScan || !strings.Contains(f.Note, "hơn") {
		t.Errorf("Quyết định 7: total %d more %v note %s", f.CandidatesTotal, f.CandidatesMore, f.Note)
	}
}

// L5 and the finding cap: too many places and too many citations say so.
func TestLegalBasisCaps(t *testing.T) {
	cat := newLegalBasisFixture()
	var paras []string
	for i := 0; i < legalBasisMaxMarks+10; i++ {
		paras = append(paras, fmt.Sprintf("Đoạn %d theo Thông tư 01/2011/TT-BNV.", i))
	}
	r := lbCheck(t, cat, nil, paras...)
	if len(r.DocumentOps) != legalBasisMaxMarks || !r.MarksTruncated || r.Truncated {
		t.Errorf("marks %d truncated %v/%v", len(r.DocumentOps), r.MarksTruncated, r.Truncated)
	}
	paras = nil
	for i := 0; i < legalBasisMaxFindings+5; i++ {
		paras = append(paras, fmt.Sprintf("Nghị định %d/2001/NĐ-CP.", 100+i))
	}
	r = lbCheck(t, cat, nil, paras...)
	if len(r.Findings) != legalBasisMaxFindings || !r.Truncated {
		t.Errorf("findings %d truncated %v", len(r.Findings), r.Truncated)
	}
}

// Positions on decomposed text, a no-break space, repeated paragraphs and
// a number inside a longer one: what the editor's search will find.
func TestLegalBasisPositionsOnOddText(t *testing.T) {
	cat := newLegalBasisFixture()
	nfd := norm.NFD.String("Căn cứ Nghị định số 30/2020/NĐ-CP;")
	paras := []string{nfd, "Theo Nghị định 30/2020/NĐ-CP.", "Theo Nghị định 30/2020/NĐ-CP.",
		"Nghị định 168/2020/NĐ-CP và Nghị định 68/2020/NĐ-CP", "Số 12/2025/TT-BTC"}
	r := lbCheck(t, cat, nil, paras...)
	f := findingFor(t, r, "30/2020/NĐ-CP")
	if len(f.Positions) != 3 {
		t.Fatalf("positions %+v", f.Positions)
	}
	p0 := f.Positions[0]
	if got := string([]rune(nfd)[p0.Start:p0.End]); got != p0.Text {
		t.Errorf("NFD offsets give %q, raw %q", got, p0.Text)
	}
	if p0.Anchor.Text != anchorText(nfd) || strings.Contains(p0.Anchor.Text, " ") {
		t.Errorf("anchor %q", p0.Anchor.Text)
	}
	if f.Positions[1].Anchor.Occurrence != 1 || f.Positions[2].Anchor.Occurrence != 2 {
		t.Errorf("repeated paragraphs: %d, %d", f.Positions[1].Anchor.Occurrence, f.Positions[2].Anchor.Occurrence)
	}
	// the editor's search finds "68/2020/NĐ-CP" inside "168/2020/NĐ-CP" first
	if p := findingFor(t, r, "68/2020/NĐ-CP").Positions[0]; p.TextOccurrence != 2 {
		t.Errorf("68 inside 168: occurrence %d", p.TextOccurrence)
	}
	if r.Skipped.OwnNumber != 1 {
		t.Errorf("own number %+v", r.Skipped)
	}
}

// The classifier reads each catalogue document's links once.
func TestLegalBasisClassifyCache(t *testing.T) {
	cat := &countingLinks{fakeLegalBasisCatalog: newLegalBasisFixture()}
	// 30/2020 cited and also the replacer of 01/2011 (not re-read: related
	// documents use their row), 12/2024 twice through its candidates
	r, err := CheckLegalBasis(context.Background(), []string{
		"Nghị định 30/2020/NĐ-CP; Thông tư 01/2011/TT-BNV; Quyết định 12/2024/QĐ-UBND; Luật Đất đai số 31/2024/QH15.",
		"Nghị định 30/2020/NĐ-CP.",
	}, cat, nil, lbNow)
	if err != nil {
		t.Fatal(err)
	}
	for id, n := range cat.byID {
		if n > 1 {
			t.Errorf("%s links read %d times", id, n)
		}
	}
	_ = r
}

type countingLinks struct {
	*fakeLegalBasisCatalog
	byID map[string]int
}

func (c *countingLinks) ListLinks(ctx context.Context, id string, out, in []types.LegalRelation) ([]types.LegalRelationLink, error) {
	if c.byID == nil {
		c.byID = map[string]int{}
	}
	c.byID[id]++
	return c.fakeLegalBasisCatalog.ListLinks(ctx, id, out, in)
}
