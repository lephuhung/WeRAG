package service

import (
	"regexp"
	"strings"

	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// Code checks of an edge against A (plan §4.5). An edge failing one is
// kept as tier 2 with the reason — Unresolved when the provision it names
// may still be touched, Rejected when it is no operation on A at all — and
// never dropped silently. The check that needs the version of A right
// before B (a phrase's From) runs in the resolver, which knows it.

// LegalDocStructure lists the provisions document A has, as keys of
// LegalProvisionRef ("3||", "3|1|", "3|1|a"), read from the legal
// metadata of its chunks.
type LegalDocStructure map[string]bool

// legalStructureFrom collects the provisions of A's chunks.
func legalStructureFrom(metas []*vietnamese_legal.LegalMetadata) LegalDocStructure {
	s := LegalDocStructure{}
	for _, m := range metas {
		if m == nil {
			continue
		}
		for _, a := range m.ArticleNos {
			a = strings.ToLower(a)
			s[LegalProvisionRef{Article: a}.key()] = true
			if len(m.ArticleNos) != 1 {
				continue
			}
			for _, ref := range m.SubdivisionRefs {
				if p, ok := parseSubdivisionRef(a, ref); ok {
					s[p.key()] = true
					if p.Diem != "" {
						s[LegalProvisionRef{Article: a, Khoan: p.Khoan}.key()] = true
					}
				}
			}
			for _, k := range m.KhoanNos {
				s[LegalProvisionRef{Article: a, Khoan: strings.ToLower(k)}.key()] = true
			}
		}
	}
	return s
}

// parseSubdivisionRef reads "khoan:2" / "khoan:2/diem:a" of Điều article.
func parseSubdivisionRef(article, ref string) (LegalProvisionRef, bool) {
	p := LegalProvisionRef{Article: article}
	for _, part := range strings.Split(ref, "/") {
		kv := strings.SplitN(part, ":", 2)
		if len(kv) != 2 {
			return p, false
		}
		switch kv[0] {
		case "khoan":
			p.Khoan = strings.ToLower(kv[1])
		case "diem":
			p.Diem = strings.ToLower(kv[1])
		default:
			return p, false
		}
	}
	return p, p.Khoan != ""
}

func (s LegalDocStructure) has(p LegalProvisionRef) bool {
	return s[p.key()]
}

// legalLeadInVerbs maps the verbs of a lead-in to the operations they can
// introduce; a lead-in naming none is not checked.
var legalLeadInVerbs = []struct {
	re  *regexp.Regexp
	ops []LegalAmendOp
}{
	{regexp.MustCompile(`(?i)(?:bãi bỏ|bỏ)\s+(?:các\s+)?(?:cụm\s+từ|từ\s+ngữ|từ)`), []LegalAmendOp{LegalOpBaiBoCumTu}},
	{regexp.MustCompile(`(?i)(?:sửa đổi|thay thế|thay đổi|thay)\s+(?:các\s+)?(?:cụm\s+từ|từ\s+ngữ|từ)`), []LegalAmendOp{LegalOpThayCumTu, LegalOpBaiBoCumTu}},
	{regexp.MustCompile(`(?i)sửa đổi|được sửa|điều chỉnh`), []LegalAmendOp{LegalOpSuaDoi, LegalOpBoSung}},
	{regexp.MustCompile(`(?i)bổ sung`), []LegalAmendOp{LegalOpBoSung}},
	{regexp.MustCompile(`(?i)bãi bỏ`), []LegalAmendOp{LegalOpBaiBo, LegalOpBaiBoVanBan, LegalOpBaiBoCumTu}},
	{regexp.MustCompile(`(?i)thay thế`), []LegalAmendOp{LegalOpThayTheVanBan, LegalOpSuaDoi, LegalOpThayCumTu}},
	{regexp.MustCompile(`(?i)không\s+(?:được\s+)?áp dụng`), []LegalAmendOp{LegalOpKhongApDung}},
	{regexp.MustCompile(`(?i)(?:tạm\s+)?ngưng\s+hiệu\s+lực|tạm\s+ngưng`), []LegalAmendOp{LegalOpTamNgung}},
	{regexp.MustCompile(`(?i)đình chỉ`), []LegalAmendOp{LegalOpDinhChi}},
	{regexp.MustCompile(`(?i)hết\s+hiệu\s+lực`), []LegalAmendOp{LegalOpHetHieuLuc}},
	{regexp.MustCompile(`(?i)tiếp\s+tục\s+(?:được\s+)?(?:áp dụng|thực hiện)`), []LegalAmendOp{LegalOpTiepTucApDung}},
}

// legalLeadInAllows reports whether the lead-in's verbs can introduce op;
// known is false when the lead-in has no such verb.
func legalLeadInAllows(leadIn string, op LegalAmendOp) (ok, known bool) {
	if op == LegalOpKhac {
		return true, true
	}
	for _, v := range legalLeadInVerbs {
		if !v.re.MatchString(leadIn) {
			continue
		}
		known = true
		for _, o := range v.ops {
			if o == op {
				return true, true
			}
		}
	}
	return !known, known
}

// legalSelfScopeRe: B speaking of itself — "Nghị định này không áp dụng
// đối với …", "Thông tư này không điều chỉnh …" — sets B's own scope; the
// documents it cites are referred to, not operated on (plan §4.3).
var legalSelfScopeRe = regexp.MustCompile(`(?i)^\s*(?:\d+[a-zđ]?\.\s*|[a-zđ]\)\s*)?(?:Quy định\s+(?:tại|của)\s+)?(?:` +
	`Bộ luật|Luật|Pháp lệnh|Nghị định|Nghị quyết|Thông tư liên tịch|Thông tư|Quyết định|Chỉ thị|văn bản|quy chế|quy định)\s+này\b[^.;:]*?\bkhông\s+(?:được\s+)?(?:áp dụng|điều chỉnh)`)

// legalSelfScope reports whether a sentence states B's own scope.
func legalSelfScope(text string) bool {
	return legalSelfScopeRe.MatchString(text)
}

// ValidateLegalAmendEdge runs the static checks on e: B's own scope is no
// operation; the lead-in's verb fits the operation; the target exists in
// A (bo_sung: the new label does not yet); the quoted text starts with the
// target's label. structure may be nil (A's provisions unknown): the
// target is then not checked. A failing edge drops to tier 2.
func ValidateLegalAmendEdge(e *LegalAmendEdge, structure LegalDocStructure) {
	fail := func(reason string, reject bool) {
		if reject {
			if e.Rejected == "" {
				e.Rejected = reason
			}
		} else if e.Unresolved == "" {
			e.Unresolved = reason
		}
		e.Tier = LegalTierUnverified
	}
	if legalSelfScope(e.LeadInText) {
		fail("câu dẫn nói về phạm vi của chính văn bản sửa đổi (“… này không áp dụng”), không phải thao tác lên văn bản này", true)
		return
	}
	if legalRankInverted(e.Amender.Type, e.Target.Type) {
		fail(e.Amender.Type+" không thể sửa đổi "+e.Target.Type+" (văn bản có hiệu lực pháp lý thấp hơn): đích có thể bị đọc nhầm", true)
		return
	}
	if ok, known := legalLeadInAllows(e.LeadInText, e.Op); known && !ok {
		fail("động từ của câu dẫn không khớp thao tác "+string(e.Op), false)
	}
	p := e.Provision
	// a phrase change is checked against the words themselves (resolver);
	// chunk metadata may miss a khoản the text has
	if structure != nil && p.Article != "" && len(structure) > 0 && !e.phrase() {
		if e.Op == LegalOpBoSung && e.NewLabel != "" {
			added := legalAddedProvision(p, e.NewLabel)
			switch {
			case !structure.has(p):
				fail("không thấy "+p.String()+" (nơi bổ sung) trong văn bản", false)
			case structure.has(added):
				fail(added.String()+" đã có trong văn bản trước khi bổ sung", false)
			}
		} else if !structure.has(p) {
			fail("không thấy "+p.String()+" trong văn bản", false)
		}
	}
	if (e.Op == LegalOpSuaDoi || e.Op == LegalOpBoSung) && e.QuoteText != "" {
		want := p
		if e.Op == LegalOpBoSung && e.NewLabel != "" {
			want = legalAddedProvision(p, e.NewLabel)
		}
		switch got := quoteLabel(e.QuoteText); {
		case got == "":
			fail("lời văn trích không mở đầu bằng nhãn "+want.label(), false)
		case got != want.label():
			fail("lời văn trích mở đầu bằng “"+got+"”, không khớp "+want.String(), false)
		}
	}
}

// legalTypeRank orders document types by legal force (smaller = higher);
// only the clear tiers: Nghị quyết (Quốc hội, Chính phủ or HĐND) and
// types not listed are not ranked.
var legalTypeRank = map[string]int{
	"hiến pháp":          0,
	"luật":               1,
	"bộ luật":            1,
	"pháp lệnh":          2,
	"nghị định":          3,
	"quyết định":         4,
	"thông tư":           4,
	"thông tư liên tịch": 4,
}

// legalRankInverted reports an amender of lower force than its target
// ("Thông tư" amending a "Nghị định", "Nghị định" a "Luật"): such an
// operation cannot exist, the parser read the target wrong.
func legalRankInverted(amenderType, targetType string) bool {
	b, okB := legalTypeRank[strings.ToLower(strings.TrimSpace(amenderType))]
	a, okA := legalTypeRank[strings.ToLower(strings.TrimSpace(targetType))]
	return okA && okB && b > a
}

// legalAddedProvision is the provision a bo_sung of label under p creates.
func legalAddedProvision(p LegalProvisionRef, label string) LegalProvisionRef {
	label = strings.ToLower(label)
	switch {
	case p.Article == "":
		return LegalProvisionRef{Article: label}
	case p.Khoan == "":
		return LegalProvisionRef{Article: p.Article, Khoan: label}
	}
	return LegalProvisionRef{Article: p.Article, Khoan: p.Khoan, Diem: label}
}
