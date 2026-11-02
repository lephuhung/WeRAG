package docformat

import (
	"regexp"
	"sort"
	"strings"
)

// Văn bản của Đảng: the thể thức of Hướng dẫn 36-HD/VPTW (03/4/2018) of
// the Văn phòng Trung ương Đảng, not Nghị định 30/2020. The tiêu đề
// "ĐẢNG CỘNG SẢN VIỆT NAM" (ô 1, top right, no quốc hiệu / tiêu ngữ) marks
// a party document; the cơ quan ban hành has a "*" line under it, the số
// reads "Số 15-HD/TU" (no colon, "-" before the type code, "/" before the
// agency code), the trích yếu is closed by "-----", a khối ký signs
// "T/M", "K/T", "T/L" and the nơi nhận list ends its lines with ",".
//
// A party document gets its own types (dang_<loại>, or "dang" when the
// loại is unclear) and rule sets: rules/_dang_base.json, merged under a
// rules/dang_<loại>.json when the type has one (rules.go). It is never
// judged by the NĐ30 rules: the scope (scope.go), the judgment kinds
// (judgment_party.go) and the skill (skills/the-thuc-dang) are its own.

// PartyGuide names the instrument party documents are checked against.
const PartyGuide = "Hướng dẫn 36-HD/VPTW"

// PartyGeneric is the type of a party document whose loại is unclear.
const PartyGeneric = "dang"

// partyBaseRules is the shared rule set of party documents.
const partyBaseRules = "_dang_base"

// PartySkill is the skill that applies to every party document.
const PartySkill = "the-thuc-dang"

// partyType is one loại văn bản của Đảng: its slug, name, ký hiệu codes
// (fold()ed: QĐi → QDI) and tên loại (fold()ed).
type partyType struct {
	Slug, Name string
	Codes      []string
	Line       string
}

// partyTypes are the loại văn bản of Quy định 66-QĐ/TW that HD36 lays out;
// HD36 1.3.1 fixes the codes QĐ, QĐi, CT, CTr, TT (thông tri), TTr.
var partyTypes = []partyType{
	{"dang_nghi_quyet", "Nghị quyết", []string{"NQ"}, "NGHI QUYET"},
	{"dang_quyet_dinh", "Quyết định", []string{"QD"}, "QUYET DINH"},
	{"dang_quy_dinh", "Quy định", []string{"QDI"}, "QUY DINH"},
	{"dang_chi_thi", "Chỉ thị", []string{"CT"}, "CHI THI"},
	{"dang_ket_luan", "Kết luận", []string{"KL"}, "KET LUAN"},
	{"dang_quy_che", "Quy chế", []string{"QC"}, "QUY CHE"},
	{"dang_thong_tri", "Thông tri", []string{"TT"}, "THONG TRI"},
	{"dang_huong_dan", "Hướng dẫn", []string{"HD"}, "HUONG DAN"},
	{"dang_bao_cao", "Báo cáo", []string{"BC"}, "BAO CAO"},
	{"dang_ke_hoach", "Kế hoạch", []string{"KH"}, "KE HOACH"},
	{"dang_chuong_trinh", "Chương trình", []string{"CTR"}, "CHUONG TRINH"},
	{"dang_thong_bao", "Thông báo", []string{"TB"}, "THONG BAO"},
	{"dang_to_trinh", "Tờ trình", []string{"TTR"}, "TO TRINH"},
	{"dang_cong_van", "Công văn", []string{"CV"}, "CONG VAN"},
	{"dang_bien_ban", "Biên bản", []string{"BB"}, "BIEN BAN"},
}

var (
	// partyTitleRe: the tiêu đề alone on its line (fold()ed), possibly
	// followed by its rule typed as underscores.
	partyTitleRe = regexp.MustCompile(`^DANG\s+CONG\s+SAN\s+VIET\s+NAM\.?[\s_\-–—]*$`)
	// partySoRe: a party số ký hiệu "Số 05-NQ/TU" (also with the wrong
	// colon) — group 1 the số, group 2 the type code.
	partySoRe = regexp.MustCompile(`^SO\s*[:.]?\s*([\d._… ]*?)\s*-\s*([A-Z]+)\s*/\s*\S`)
	// partyChiDanRe: ô 11a/11b of a party document — a phạm vi lưu hành
	// ("XONG HỘI NGHỊ TRẢ LẠI", "TÀI LIỆU THU HỒI") or "Dự thảo lần 2".
	// "(Dự thảo lần 2)", "Tài liệu phục vụ Hội nghị", in parentheses or not.
	partyChiDanRe = regexp.MustCompile(`^\(?\s*(XONG\s+HOI\s+NGHI\s+TRA\s+LAI|XEM\s+XONG\s+TRA\s+LAI|` +
		`TAI\s+LIEU\s+(THU\s+HOI|HOI\s+NGHI|PHUC\s+VU\s+HOI\s+NGHI)|` +
		`LUU\s+HANH\s+NOI\s+BO|DU\s+THAO(\s+LAN\s+(THU\s+)?\d+)?)\s*\.?\s*\)?\.?$`)
	// partyKyHieuSTRe: ô 12 of a party document, "ABC-268" as well
	partyKyHieuSTRe = regexp.MustCompile(`^\pL{1,8}\s?[./-]\s?\d{1,4}\pL?$`)
)

// IsPartyType reports whether docType is a type of văn bản của Đảng.
func IsPartyType(docType string) bool {
	return docType == PartyGeneric || strings.HasPrefix(docType, "dang_")
}

// PartyTypes lists the party document types (without the generic one).
func PartyTypes() []string {
	out := make([]string, 0, len(partyTypes))
	for _, t := range partyTypes {
		out = append(out, t.Slug)
	}
	sort.Strings(out)
	return out
}

func partyTypeBySlug(slug string) *partyType {
	for i := range partyTypes {
		if partyTypes[i].Slug == slug {
			return &partyTypes[i]
		}
	}
	return nil
}

// PartyTypeName names a party type ("dang_bao_cao" → "Báo cáo").
func PartyTypeName(slug string) string {
	if t := partyTypeBySlug(slug); t != nil {
		return t.Name
	}
	return "chưa rõ loại"
}

// partyLabel is the rule set label of a party type without its own file.
func partyLabel(slug string) string {
	if t := partyTypeBySlug(slug); t != nil {
		return t.Name + " của Đảng (" + PartyGuide + ")"
	}
	return "Thể thức chung văn bản của Đảng (" + PartyGuide + ")"
}

// PartySlugFor maps a type to its party counterpart: "bao_cao" →
// "dang_bao_cao"; a party type stays; one HD36 has not → "dang".
func PartySlugFor(docType string) string {
	if IsPartyType(docType) {
		return docType
	}
	if t := partyTypeBySlug("dang_" + docType); t != nil {
		return t.Slug
	}
	return PartyGeneric
}

// partyTypeFromSymbol reads the type code before "/" of a party số ký
// hiệu: "Số 15-HD/TU" → dang_huong_dan, "Số 05-QĐi/TU" → dang_quy_dinh.
func partyTypeFromSymbol(soKyHieu string) string {
	m := partySoRe.FindStringSubmatch(fold(strings.TrimSpace(soKyHieu)))
	if m == nil {
		return ""
	}
	for _, t := range partyTypes {
		for _, c := range t.Codes {
			if c == m[2] {
				return t.Slug
			}
		}
	}
	return ""
}

// partyTypeFromLine reads a tên loại line ("HƯỚNG DẪN", "KẾT LUẬN CỦA BAN
// THƯỜNG VỤ …").
func partyTypeFromLine(line string) string {
	f := fold(line)
	for _, t := range partyTypes {
		if f == t.Line || strings.HasPrefix(f, t.Line+" ") {
			return t.Slug
		}
	}
	return ""
}

// DetectPartyType reads the loại of a party document: a trích yếu in the
// left column (under the số, ô 5b) is a công văn's, read by its ký hiệu;
// otherwise the tên loại line wins, then the ký hiệu, then the NĐ30 type
// the heuristic detected, mapped to its party counterpart.
func DetectPartyType(seg *Segmentation) string {
	ty := seg.Comp("trich_yeu")
	bySym := partyTypeFromSymbol(seg.Comp("so_ky_hieu").Text)
	if ty.Found && ty.Zone == ZoneLeft {
		if bySym != "" {
			return bySym
		}
		return "dang_cong_van"
	}
	if ty.Found {
		first, _, _ := strings.Cut(strings.TrimSpace(ty.Text), "\n")
		if t := partyTypeFromLine(first); t != "" {
			return t
		}
	}
	if bySym != "" {
		return bySym
	}
	return PartySlugFor(seg.DetectedType)
}

// partyHeader reports whether the header elements hold the tiêu đề Đảng.
func partyHeader(els []element) bool {
	for _, el := range els {
		if partyTitleRe.MatchString(fold(el.text)) {
			return true
		}
	}
	return false
}

// partyTitleScan bounds how many leading elements markParty reads for the
// tiêu đề: it heads the document.
const partyTitleScan = 12

// markParty makes seg a party document's when it carries the tiêu đề
// "ĐẢNG CỘNG SẢN VIỆT NAM" and no quốc hiệu: the tiêu đề becomes its own
// component (a model may have labelled it quốc hiệu, or left it out) and
// the type a party type. A document quoting "Đảng Cộng sản Việt Nam" in a
// sentence, or carrying a quốc hiệu, is left alone. Idempotent.
func markParty(l *Layout, seg *Segmentation) {
	if q, ok := seg.Components["quoc_hieu"]; ok && q.Found && !seg.Comp("tieu_de_dang").Found &&
		partyTitleRe.MatchString(fold(q.Text)) {
		seg.renameComponent("quoc_hieu", "tieu_de_dang")
	}
	if !seg.Comp("tieu_de_dang").Found {
		if seg.Comp("quoc_hieu").Found || seg.QuocHieuLines > 0 {
			return
		}
		els := elements(l)
		if len(els) > partyTitleScan {
			els = els[:partyTitleScan]
		}
		for i, el := range els {
			if partyTitleRe.MatchString(fold(el.text)) {
				seg.dropPara(el.para.Index, el.sub, el.text)
				seg.add("tieu_de_dang", el.para.Index, el.text, el.zone, i, el.sub)
				break
			}
		}
		if !seg.Comp("tieu_de_dang").Found {
			return
		}
	}
	seg.DetectedType = DetectPartyType(seg)
}

// renameComponent moves component from to key to (to absent).
func (s *Segmentation) renameComponent(from, to string) {
	c, ok := s.Components[from]
	if !ok {
		return
	}
	delete(s.Components, from)
	s.Components[to] = c
	for i, k := range s.order {
		if k == from {
			s.order[i] = to
		}
	}
	for p, k := range s.ParaLabels {
		if k == from {
			s.ParaLabels[p] = to
		}
	}
}

// dropPara removes the unit (paragraph para, or its line sub when sub >=
// 0, a paragraph read line by line) and its text from every component
// holding it. A component holding other lines of the paragraph keeps the
// paragraph and those lines; one holding the paragraph whole loses only
// the text; a component left empty is removed.
func (s *Segmentation) dropPara(para, sub int, text string) {
	for key, c := range s.Components {
		at := -1
		for i, p := range c.Paras {
			if p == para {
				at = i
			}
		}
		if at < 0 {
			continue
		}
		if sub >= 0 {
			subs := c.Subs[para]
			if len(subs) == 0 || !hasInt(subs, sub) {
				// the title line is not this component's: at most its text
				if len(subs) == 0 {
					c.Texts = dropText(c.Texts, text)
					c.Text = strings.Join(c.Texts, "\n")
				}
				continue
			}
			var keep []int
			for _, k := range subs {
				if k != sub {
					keep = append(keep, k)
				}
			}
			c.Texts = dropText(c.Texts, text)
			c.Text = strings.Join(c.Texts, "\n")
			if len(keep) > 0 {
				c.Subs[para] = keep
				continue
			}
			delete(c.Subs, para)
		}
		c.Paras = append(c.Paras[:at:at], c.Paras[at+1:]...)
		if at < len(c.Zones) {
			c.Zones = append(c.Zones[:at:at], c.Zones[at+1:]...)
		}
		c.Texts = dropText(c.Texts, text)
		c.Text = strings.Join(c.Texts, "\n")
		delete(c.Subs, para)
		if len(c.Paras) == 0 {
			delete(s.Components, key)
			for i, k := range s.order {
				if k == key {
					s.order = append(s.order[:i:i], s.order[i+1:]...)
					break
				}
			}
		}
		if s.ParaLabels[para] == key {
			delete(s.ParaLabels, para)
		}
	}
}

// dropText removes the entries equal to text (trimmed).
func dropText(texts []string, text string) []string {
	var out []string
	for _, t := range texts {
		if strings.TrimSpace(t) != strings.TrimSpace(text) {
			out = append(out, t)
		}
	}
	return out
}

// Party reports whether r was checked as a văn bản của Đảng.
func (r *Report) Party() bool {
	return r != nil && r.DocumentType != nil && IsPartyType(r.DocumentType.Used)
}

// ruleSource names, in a finding, what the rules come from: "NĐ30" or
// the party guide.
func ruleSource(r *Report) string {
	if r.Party() {
		return PartyGuide
	}
	return "NĐ30"
}

// StandardName names the instrument a report was checked against, for the
// user: "Nghị định 30/2020/NĐ-CP" or "Hướng dẫn 36-HD/VPTW".
func (r *Report) StandardName() string {
	if r.Party() {
		return PartyGuide
	}
	return "Nghị định 30/2020/NĐ-CP"
}
