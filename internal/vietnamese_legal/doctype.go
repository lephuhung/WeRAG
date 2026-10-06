// doctype.go is the single registry of Vietnamese document types. Every
// other type list in this package (ParseReference keywords, the Document
// entity prefix, the số-hiệu suffix map) is derived from it, and
// docformat/docformat/doctypes.py mirrors it for the format checker — keep
// the two in sync.
//
// Two groups:
//   - The 29 văn bản hành chính of Điều 7 Nghị định 30/2020/NĐ-CP, with the
//     ký hiệu from Phụ lục III (công văn and thư công have none).
//   - The văn bản quy phạm pháp luật types this package already recognised
//     (Luật, Nghị định, Thông tư, …). Nghị quyết / Quyết định / Chỉ thị
//     belong to both and are listed once, in the NĐ30 group.
package vietnamese_legal

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// DocTypeGroup classifies a document type.
type DocTypeGroup string

const (
	// DocTypeGroupHanhChinh is a văn bản hành chính of Điều 7 NĐ30/2020.
	DocTypeGroupHanhChinh DocTypeGroup = "hanh_chinh"
	// DocTypeGroupQPPL is a văn bản quy phạm pháp luật only.
	DocTypeGroupQPPL DocTypeGroup = "qppl"
)

// DocTypeDef describes one document type.
type DocTypeDef struct {
	Slug  string
	Name  string // display name, e.g. "Kế hoạch"
	Group DocTypeGroup
	// Symbols are the ký hiệu codes used in số ký hiệu ("KH" in
	// "45/KH-UBND"). Case and Đ are significant: HĐ ≠ HD, ĐA ≠ DA.
	Symbols []string
	// Legacy marks the types recognised before the NĐ30 registry existed.
	// They keep the old loose matching (keyword anywhere in a reference,
	// bare prefix ⇒ Document entity); the other types require an anchored
	// keyword or a số hiệu to avoid matching common words ("quy định",
	// "hướng dẫn", "dự án").
	Legacy bool
}

// DocTypes lists every known type. The first 29 entries are Điều 7
// NĐ30/2020 in the decree's order.
var DocTypes = []DocTypeDef{
	{"nghi_quyet", "Nghị quyết", DocTypeGroupHanhChinh, []string{"NQ"}, true},
	{"quyet_dinh", "Quyết định", DocTypeGroupHanhChinh, []string{"QĐ"}, true},
	{"chi_thi", "Chỉ thị", DocTypeGroupHanhChinh, []string{"CT"}, true},
	{"quy_che", "Quy chế", DocTypeGroupHanhChinh, []string{"QC"}, false},
	{"quy_dinh", "Quy định", DocTypeGroupHanhChinh, []string{"QyĐ"}, false},
	{"thong_cao", "Thông cáo", DocTypeGroupHanhChinh, []string{"TC"}, false},
	{"thong_bao", "Thông báo", DocTypeGroupHanhChinh, []string{"TB"}, false},
	{"huong_dan", "Hướng dẫn", DocTypeGroupHanhChinh, []string{"HD"}, false},
	{"chuong_trinh", "Chương trình", DocTypeGroupHanhChinh, []string{"CTr"}, false},
	{"ke_hoach", "Kế hoạch", DocTypeGroupHanhChinh, []string{"KH"}, false},
	{"phuong_an", "Phương án", DocTypeGroupHanhChinh, []string{"PA"}, false},
	{"de_an", "Đề án", DocTypeGroupHanhChinh, []string{"ĐA"}, false},
	{"du_an", "Dự án", DocTypeGroupHanhChinh, []string{"DA"}, false},
	{"bao_cao", "Báo cáo", DocTypeGroupHanhChinh, []string{"BC"}, false},
	{"bien_ban", "Biên bản", DocTypeGroupHanhChinh, []string{"BB"}, false},
	{"to_trinh", "Tờ trình", DocTypeGroupHanhChinh, []string{"TTr"}, false},
	{"hop_dong", "Hợp đồng", DocTypeGroupHanhChinh, []string{"HĐ"}, false},
	{"cong_van", "Công văn", DocTypeGroupHanhChinh, nil, false},
	{"cong_dien", "Công điện", DocTypeGroupHanhChinh, []string{"CĐ"}, false},
	{"ban_ghi_nho", "Bản ghi nhớ", DocTypeGroupHanhChinh, []string{"GN"}, false},
	{"ban_thoa_thuan", "Bản thỏa thuận", DocTypeGroupHanhChinh, []string{"TTh"}, false},
	{"giay_uy_quyen", "Giấy ủy quyền", DocTypeGroupHanhChinh, []string{"GUQ"}, false},
	{"giay_moi", "Giấy mời", DocTypeGroupHanhChinh, []string{"GM"}, false},
	{"giay_gioi_thieu", "Giấy giới thiệu", DocTypeGroupHanhChinh, []string{"GGT"}, false},
	{"giay_nghi_phep", "Giấy nghỉ phép", DocTypeGroupHanhChinh, []string{"NP"}, false},
	{"phieu_gui", "Phiếu gửi", DocTypeGroupHanhChinh, []string{"PG"}, false},
	{"phieu_chuyen", "Phiếu chuyển", DocTypeGroupHanhChinh, []string{"PC"}, false},
	{"phieu_bao", "Phiếu báo", DocTypeGroupHanhChinh, []string{"PB"}, false},
	{"thu_cong", "Thư công", DocTypeGroupHanhChinh, nil, false},

	{"hien_phap", "Hiến pháp", DocTypeGroupQPPL, nil, true},
	{"bo_luat", "Bộ luật", DocTypeGroupQPPL, nil, true},
	{"luat", "Luật", DocTypeGroupQPPL, nil, true},
	{"phap_lenh", "Pháp lệnh", DocTypeGroupQPPL, nil, true},
	{"nghi_dinh", "Nghị định", DocTypeGroupQPPL, []string{"NĐ"}, true},
	{"thong_tu_lien_tich", "Thông tư liên tịch", DocTypeGroupQPPL, []string{"TTLT"}, true},
	{"thong_tu", "Thông tư", DocTypeGroupQPPL, []string{"TT"}, true},
}

// ND30DocTypeCount is the number of văn bản hành chính in Điều 7 NĐ30/2020.
const ND30DocTypeCount = 29

var (
	docTypeBySlug   = map[string]*DocTypeDef{}
	docTypeBySymbol = map[string]*DocTypeDef{}
	// docTypeBySymbolFolded catches a ký hiệu whose Đ was lost to a bad
	// font/OCR ("361/2025/ND-CP"): the D-for-Đ form, unless that form is
	// itself another type's code (HD is hướng dẫn, not a damaged HĐ).
	docTypeBySymbolFolded = map[string]*DocTypeDef{}
	// docTypesLongestFirst orders types by name length so "Thông tư liên
	// tịch" is tried before "Thông tư" and "Bộ luật" before "Luật".
	docTypesLongestFirst []*DocTypeDef
)

func init() {
	for i := range DocTypes {
		d := &DocTypes[i]
		docTypeBySlug[d.Slug] = d
		for _, s := range d.Symbols {
			docTypeBySymbol[strings.ToUpper(s)] = d
		}
		docTypesLongestFirst = append(docTypesLongestFirst, d)
	}
	for code, d := range docTypeBySymbol {
		if f := strings.ReplaceAll(code, "Đ", "D"); docTypeBySymbol[f] == nil {
			docTypeBySymbolFolded[f] = d
		}
	}
	sortByNameLenDesc(docTypesLongestFirst)
}

func sortByNameLenDesc(ds []*DocTypeDef) {
	// insertion sort: stable, tiny input
	for i := 1; i < len(ds); i++ {
		for j := i; j > 0 && len([]rune(ds[j].Name)) > len([]rune(ds[j-1].Name)); j-- {
			ds[j], ds[j-1] = ds[j-1], ds[j]
		}
	}
}

// DocTypeBySlug returns the registry entry for slug, or nil.
func DocTypeBySlug(slug string) *DocTypeDef { return docTypeBySlug[slug] }

// DocTypeBySymbol maps a ký hiệu code ("KH", "QĐ", "TTr") to its type, or
// nil. Matching is case-insensitive but keeps Đ distinct from D.
func DocTypeBySymbol(code string) *DocTypeDef {
	code = strings.ToUpper(strings.TrimSpace(code))
	if d := docTypeBySymbol[code]; d != nil {
		return d
	}
	return docTypeBySymbolFolded[code]
}

// foldVN upper-cases, strips Vietnamese diacritics (Đ → D) and collapses
// whitespace, so OCR damage to the tone marks ("NGHI ĐỊNH", "ĐẦNG CỘNG
// SẢN") still matches. Mirrors docformat segment.fold.
func foldVN(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(s) {
		switch {
		case unicode.Is(unicode.Mn, r):
		case r == 'đ' || r == 'Đ':
			b.WriteRune('D')
		default:
			b.WriteRune(unicode.ToUpper(r))
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// DocTypeByName maps a display name ("Kế hoạch", "KẾ HOẠCH") or a slug
// ("ke_hoach") to its type, or nil. Used to canonicalise model-written
// labels.
func DocTypeByName(name string) *DocTypeDef {
	name = strings.Join(strings.Fields(name), " ")
	if d := docTypeBySlug[strings.ToLower(name)]; d != nil {
		return d
	}
	for i := range DocTypes {
		if strings.EqualFold(DocTypes[i].Name, name) {
			return &DocTypes[i]
		}
	}
	return nil
}

// ownNumberRe is the shape of a document's own số hiệu once spaces are
// removed: "45/KH-UBND", "53/2022/NĐ-CP", "24/2018/QH14".
// Party documents number type-first: "57-NQ/TW".
var ownNumberRe = regexp.MustCompile(
	`^(?:\d{1,5}[a-zA-Z]?/(?:\d{2,4}/)?\p{L}[\p{L}\p{N}_\-]*|\d{1,5}-\p{L}+/\p{L}[\p{L}\p{N}\-]*)$`)

var numberLabelRe = regexp.MustCompile(`(?i)^(?:luật\s+số|số)\s*[:：.]?\s*`)

// NormalizeOwnDocumentNumber cleans a model- or parser-supplied số hiệu
// ("Số: 45 / KH-UBND" → "45/KH-UBND") and returns "" when the result does
// not have the shape of a số hiệu.
func NormalizeOwnDocumentNumber(s string) string {
	s = numberLabelRe.ReplaceAllString(strings.TrimSpace(s), "")
	s = stripWS(strings.Trim(s, " .,;:*_|"))
	if !ownNumberRe.MatchString(s) {
		return ""
	}
	return s
}

// numberSymbolRe / partyNumberSymbolRe split a số hiệu around its type
// ký hiệu: "361/2025/" + "ND" + "-CP", "57-" + "NQ" + "/TW".
var (
	numberSymbolRe      = regexp.MustCompile(`^(\d{1,5}[a-zA-Z]?/(?:\d{2,4}/)?)(\p{L}+)(.*)$`)
	partyNumberSymbolRe = regexp.MustCompile(`^(\d{1,5}-)(\p{L}+)(/.*)$`)
)

// RestoreDocumentNumberSymbol repairs a type ký hiệu whose Đ was lost to a
// bad font or OCR: "361/2025/ND-CP" → "361/2025/NĐ-CP". Only the type
// ký hiệu is touched, never the issuer part ("-CP", "-UBND").
//
//   - With a known docTypeSlug, a ký hiệu that folds to that type's own
//     code takes the canonical spelling — this also settles the ambiguous
//     pairs (HD → HĐ for a hợp đồng, DA → ĐA for an đề án).
//   - Without one, only an unambiguous fold is restored (ND → NĐ, QD → QĐ,
//     CD → CĐ); HD and DA stay as written since they are codes themselves.
//
// A number with a correct or unknown ký hiệu is returned unchanged.
func RestoreDocumentNumberSymbol(number, docTypeSlug string) string {
	pre, code, post, ok := splitNumberSymbol(number)
	if !ok {
		return number
	}
	if d := docTypeBySlug[docTypeSlug]; d != nil {
		for _, sym := range d.Symbols {
			if code == sym {
				return number
			}
			if foldVN(code) == foldVN(sym) {
				return pre + sym + post
			}
		}
	}
	if docTypeBySymbol[code] == nil && docTypeBySymbol[strings.ToUpper(code)] == nil {
		if d := docTypeBySymbolFolded[strings.ToUpper(code)]; d != nil {
			for _, sym := range d.Symbols {
				if strings.ReplaceAll(strings.ToUpper(sym), "Đ", "D") == strings.ToUpper(code) {
					return pre + sym + post
				}
			}
		}
	}
	return number
}

func splitNumberSymbol(number string) (pre, code, post string, ok bool) {
	if m := numberSymbolRe.FindStringSubmatch(number); m != nil {
		return m[1], m[2], m[3], true
	}
	if m := partyNumberSymbolRe.FindStringSubmatch(number); m != nil {
		return m[1], m[2], m[3], true
	}
	return "", "", "", false
}

// ---------------------------------------------------------------------------
// Header detection
// ---------------------------------------------------------------------------

// DocTypeDetection is the outcome of DetectDocType.
type DocTypeDetection struct {
	Slug string
	// Source names the signal that decided: "heading" (the tên loại line),
	// "symbol" (ký hiệu in the số), "vv" (a "V/v" trích yếu ⇒ công văn)
	// or "title" (knowledge title / reference).
	Source string
}

// The tên loại line is in the header, above the "Căn cứ …" preamble. The
// cut keeps a body heading ("QUY ĐỊNH" of an attached regulation, "BÁO CÁO"
// cited in the text) from being mistaken for this document's type.
var docTypeHeaderCutRe = regexp.MustCompile(`(?i)căn\s+cứ|kính\s+gửi`)

// Markdown / table decoration around header lines.
var headerDecorRe = regexp.MustCompile(`^[\s#>*_|\-–—=:.]+|[\s*_|:.]+$`)

// "Số: 45/KH-UBND", "Số 12/2025/QĐ-UBND", "Số: 12/UBND-VP".
var soKyHieuRe = regexp.MustCompile(
	`(?im)^[\s#>*_|]*số\s*[:：.]?\s*\d{1,5}[a-z]?\s*/\s*(?:\d{4}\s*/\s*)?([\p{L}]+)(?:\s*-\s*([\p{L}\p{N}\-]+))?`,
)

// Party documents (văn bản của Đảng) number type-first: "Số 57-NQ/TW",
// "Số 12-CT/TU".
var soKyHieuPartyRe = regexp.MustCompile(
	`(?im)^[\s#>*_|]*số\s*[:：.]?\s*\d{1,5}\s*-\s*([\p{L}]+)\s*/\s*[\p{L}\p{N}]+`,
)

var vvRe = regexp.MustCompile(`(?im)^[\s#>*_|]*(?:v\s*/\s*v|về\s+việc)\b`)

// Frame markers, matched on foldVN text: quốc hiệu / tiêu ngữ (NĐ30) or
// the Party header "ĐẢNG CỘNG SẢN VIỆT NAM". A bare uppercase "BÁO CÁO"
// line in an arbitrary file is not enough to call it a văn bản hành chính.
var frameFoldedRe = regexp.MustCompile(`CONG HOA XA HOI|DOC LAP ?[-–—] ?TU DO|DANG CONG SAN VIET NAM`)

// Ký hiệu codes that are not a type of their own ("QH14" Luật/NQ of Quốc
// hội, "L-CTN" Lệnh, "VBHN" văn bản hợp nhất) — never read as công văn.
var nonTypeSymbolRe = regexp.MustCompile(`^(QH\d*|UBTVQH\d*|CTN|L|VBHN)$`)

// DetectDocType determines a document's type from its header text
// (markdown of the first chunk). Signals, strongest first:
//  1. the tên loại line ("QUYẾT ĐỊNH", "KẾ HOẠCH", "GIẤY MỜI") above the
//     preamble — only when the header carries an NĐ30 frame (quốc hiệu or
//     số ký hiệu), so a plain file titled "BÁO CÁO" is not classified;
//  2. the ký hiệu in the số ("45/KH-UBND" ⇒ kế hoạch); a số whose ký hiệu
//     is the issuing unit only ("12/UBND-VP") ⇒ công văn (NĐ30);
//  3. a "V/v …" trích yếu ⇒ công văn.
//
// Returns the zero value when nothing matched.
func DetectDocType(header string) DocTypeDetection {
	if len(header) > 2500 {
		header = header[:2500]
	}
	if cut := docTypeHeaderCutRe.FindStringIndex(header); cut != nil {
		header = header[:cut[0]]
	}

	so := soKyHieuRe.FindStringSubmatch(header)
	party := soKyHieuPartyRe.FindStringSubmatch(header)
	framed := so != nil || party != nil || frameFoldedRe.MatchString(foldVN(header))

	if framed {
		if d := typeFromHeadingLines(header); d != nil {
			return DocTypeDetection{Slug: d.Slug, Source: "heading"}
		}
	}
	if party != nil {
		// Party ký hiệu outside the NĐ30 table (KL kết luận, …) are not
		// công văn — only a known code decides.
		if d := DocTypeBySymbol(party[1]); d != nil {
			return DocTypeDetection{Slug: d.Slug, Source: "symbol"}
		}
	}
	if so != nil {
		if d := DocTypeBySymbol(so[1]); d != nil {
			return DocTypeDetection{Slug: d.Slug, Source: "symbol"}
		}
		if !nonTypeSymbolRe.MatchString(strings.ToUpper(so[1])) {
			return DocTypeDetection{Slug: "cong_van", Source: "symbol"}
		}
	}
	if framed && vvRe.MatchString(header) {
		return DocTypeDetection{Slug: "cong_van", Source: "vv"}
	}
	return DocTypeDetection{}
}

// typeFromHeadingLines returns the type of the first header line that IS a
// tên loại: the line (table cells split apart, decoration stripped) opens
// with a type name and is either all uppercase ("KẾ HOẠCH TRIỂN KHAI …") or
// nothing but the name ("Quyết định"). Names are compared accent-folded so
// OCR tone-mark damage ("NGHI ĐỊNH") still matches.
func typeFromHeadingLines(header string) *DocTypeDef {
	for _, raw := range strings.Split(header, "\n") {
		for _, cell := range strings.Split(raw, "|") {
			line := headerDecorRe.ReplaceAllString(strings.TrimSpace(cell), "")
			if line == "" {
				continue
			}
			folded := foldVN(line)
			if frameFoldedRe.MatchString(folded) {
				continue
			}
			isUpper := line == strings.ToUpper(line)
			for _, d := range docTypesLongestFirst {
				name := foldVN(d.Name)
				if !strings.HasPrefix(folded, name) {
					continue
				}
				rest := folded[len(name):]
				if !endsWord(rest) {
					continue // "LUAT" must not match "LUATSU…"
				}
				if isUpper || strings.TrimSpace(rest) == "" {
					return d
				}
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Reference / entity-name matching
// ---------------------------------------------------------------------------

// matchDocTypeKeyword finds the document type named in a free-form
// reference whose số hiệu has already been removed. A type name at the
// start of the text wins ("Báo cáo thực hiện Nghị quyết 12" is a báo cáo);
// otherwise only legacy types match anywhere (previous behaviour).
// NĐ30-only names double as ordinary words ("quy định về thuế", "hướng
// dẫn nộp hồ sơ"), so they count only when the reference carries a number.
func matchDocTypeKeyword(text string, hasNumber bool) *DocTypeDef {
	low := strings.TrimSpace(strings.ToLower(text))
	for _, d := range docTypesLongestFirst {
		if !d.Legacy && !hasNumber {
			continue
		}
		kw := strings.ToLower(d.Name)
		if strings.HasPrefix(low, kw) && endsWord(low[len(kw):]) {
			return d
		}
	}
	for _, d := range docTypesLongestFirst {
		if d.Legacy && strings.Contains(low, strings.ToLower(d.Name)) {
			return d
		}
	}
	return nil
}

// endsWord reports whether rest (the text after a keyword) starts at a word
// boundary — RE2's \b is ASCII-only and never fires after "ư" / "ị".
func endsWord(rest string) bool {
	if rest == "" {
		return true
	}
	r, _ := utf8.DecodeRuneInString(rest)
	return !unicode.IsLetter(r) && !unicode.IsDigit(r)
}

// symbolInNumberRe extracts the ký hiệu from a số hiệu: "45/KH-UBND" → KH,
// "53/2022/NĐ-CP" → NĐ, "57-NQ/TW" → NQ.
var symbolInNumberRe = regexp.MustCompile(`^\d+(?:[a-zA-Z]?/(?:\d{4}/)?|-)([\p{L}]+)`)

// DocTypeFromNumber infers the type from a số hiệu's ký hiệu, or nil.
func DocTypeFromNumber(number string) *DocTypeDef {
	m := symbolInNumberRe.FindStringSubmatch(strings.ReplaceAll(number, " ", ""))
	if m == nil {
		return nil
	}
	return DocTypeBySymbol(m[1])
}

// legacyDocPrefixRe / nd30DocPrefixRe are built from the registry. A legacy
// type name alone marks a Document entity ("Luật Đất đai"); an NĐ30-only
// type also needs a number ("Kế hoạch 45/KH-UBND", "Công văn số 12") —
// "Dự án đường cao tốc" or "Hợp đồng lao động" are not documents.
var legacyDocPrefixRe, nd30DocPrefixRe = buildDocPrefixRes()

var numberedRefRe = regexp.MustCompile(`(?i)\d+\s*/\s*[\p{L}\d]|\bsố\s*:?\s*\d`)

func buildDocPrefixRes() (*regexp.Regexp, *regexp.Regexp) {
	var legacy, nd30 []string
	for _, d := range sortedNames() {
		if d.Legacy {
			legacy = append(legacy, regexp.QuoteMeta(d.Name))
		} else {
			nd30 = append(nd30, regexp.QuoteMeta(d.Name))
		}
	}
	// Not \b: RE2 word boundaries are ASCII-only ("Thông tư", "Chỉ thị").
	const end = `(?:[^\p{L}\p{N}]|$)`
	return regexp.MustCompile(`(?i)^(?:` + strings.Join(legacy, "|") + `)` + end),
		regexp.MustCompile(`(?i)^(?:` + strings.Join(nd30, "|") + `)` + end)
}

// sortedNames returns DocTypes longest name first without relying on
// init() order (package-level vars initialise before init runs).
func sortedNames() []*DocTypeDef {
	out := make([]*DocTypeDef, len(DocTypes))
	for i := range DocTypes {
		out[i] = &DocTypes[i]
	}
	sortByNameLenDesc(out)
	return out
}

// isDocTypePrefixedName reports whether an entity name opens with a
// document-type name that makes it a Document.
func isDocTypePrefixedName(name string) bool {
	if legacyDocPrefixRe.MatchString(name) {
		return true
	}
	return nd30DocPrefixRe.MatchString(name) && numberedRefRe.MatchString(name)
}
