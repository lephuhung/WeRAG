package docformat

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	vl "github.com/Tencent/WeKnora/internal/vietnamese_legal"
	"golang.org/x/text/unicode/norm"
)

// Heuristic segmentation of a Layout into the components of a Vietnamese
// administrative document (NĐ30/2020 Phụ lục I). It is positional: a
// two-column header (cơ quan left; quốc hiệu, tiêu ngữ, địa danh right) as a
// borderless table or tab stops, a centered trích yếu, the body, a
// right-column signature and a bottom-left "Nơi nhận". It is also the
// fallback when no LLM labels are available, and supplies the hints the
// LLM sees.

// fold upper-cases, strips diacritics (Đ → D) and collapses whitespace so
// keywords match regardless of casing or sloppy accents.
func fold(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(s) {
		switch {
		case unicode.Is(unicode.Mn, r):
		case r == 'đ' || r == 'Đ':
			b.WriteRune('D')
		default:
			b.WriteRune(r)
		}
	}
	return strings.ToUpper(strings.Join(strings.Fields(b.String()), " "))
}

func runeLen(s string) int { return utf8.RuneCountInString(s) }

func truncateRunes(s string, n int) string {
	if runeLen(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

var (
	quocHieuRe = regexp.MustCompile(`^(CONG\s*HOA?\s*XA\s*HOI|.*XA\s*HOI\s*CHU\s*NGHIA\s*VIET\s*NAM|CONG\s*HOA\s*XA\s*HOI).*$`)
	tieuNguRe  = regexp.MustCompile(`^DOC\s*LAP\s*[-–—]\s*TU\s*DO\s*[-–—]\s*HANH\s*PHUC\.?$`)
	// "Số: 12/QĐ-UBND", "Số:    /BC-SYT" (draft), "Số 12/UBND-VP",
	// "12/2025/NĐ-CP" — but never an agency name starting with "SỞ".
	// A văn bản quy phạm pháp luật names its type before the number:
	// "Nghị quyết số: 162/2024/QH15", "Luật số: 64/2025/QH15" — the whole
	// line, so a body sentence citing "Quyết định số 12/…" does not match.
	soKyHieuRe = regexp.MustCompile(`^SO\s*[:.]|^SO\s+\d|^SO\s*/|^SO\s+\S*/\s*[A-Z]|^\d{1,5}[A-Z]?\s*/\s*(\d{4}\s*/\s*)?[A-Z]{1,8}\b|` +
		`^` + typedSoPrefix + `\s+SO\s*[:.]?\s*[\d.…_ ]*/\s*[0-9A-Z/.\-]+$`)
	// "Huế, ngày 05 tháng 10 năm 2026", drafts with blank day/month.
	// Anchored so a title "Báo cáo ... năm 2025" does not match.
	diaDanhRe = regexp.MustCompile(`^([^,\d]{2,60},\s*)?NGAY\s*[\d.…_ ]{0,8}\s*THANG\s*[\d.…_ ]{0,8}\s*NAM\b`)
	matRe     = regexp.MustCompile(`^(DO\s+)?MAT(\s*[:.]|\s*$)|^DO\s+MAT\b`)
	khanRe    = regexp.MustCompile(`^(DO\s+KHAN\s*[:.]?\s*)?(KHAN|HOA\s*TOC|THUONG\s*KHAN)\b`)
	noiNhanRe = regexp.MustCompile(`^NOI\s*NHAN\s*[:.]?`)
	kinhGuiRe = regexp.MustCompile(`^KINH\s+GUI\s*[:.]?`)
	canCuRe   = regexp.MustCompile(`^(CAN\s+CU|THEO\s+DE\s+NGHI|XET\s+DE\s+NGHI|XET\s+TO\s+TRINH)\b`)
	phuLucRe  = regexp.MustCompile(`^PHU\s+LUC\b`)
	// ô 11 — chỉ dẫn về phạm vi lưu hành, a whole line on its own
	chiDanRe = regexp.MustCompile(`^(XEM\s+XONG\s+TRA\s+LAI|LUU\s+HANH\s+NOI\s+BO)\.?$`)
	// ô 12 — ký hiệu người soạn thảo + số bản ("NVA.15", "TTr/2b"), raw text
	kyHieuSTRe = regexp.MustCompile(`^\pL{1,8}\s?[./]\s?\d{1,3}\pL?$`)
	// ô 13 — địa chỉ, điện thoại, fax, email, website of the cơ quan
	diaChiStartRe = regexp.MustCompile(`^(DIA\s+CHI|DT|DIEN\s+THOAI|FAX|E-?MAIL|WEBSITE)\b`)
	diaChiAnyRe   = regexp.MustCompile(`\b(DIA\s+CHI|DT|DIEN\s+THOAI|FAX|E-?MAIL|WEBSITE)\b`)
	trichVVRe     = regexp.MustCompile(`^V\s*/\s*V\b|^VE\s+VIEC\b`)
	ruleLineRe    = regexp.MustCompile(`^[\s\p{Z}_\-–—=~.*·•]+$`)
	thamQuyenRe   = regexp.MustCompile(`^(CHU\s+TICH|BO\s+TRUONG|GIAM\s+DOC|THU\s+TUONG|TONG\s+GIAM\s+DOC|` +
		`CHANH\s+AN|VIEN\s+TRUONG|UY\s+BAN\s+NHAN\s+DAN|HOI\s+DONG\s+NHAN\s+DAN|` +
		`HOI\s+DONG\s+QUAN\s+TRI|THU\s+TRUONG|CUC\s+TRUONG|GIAM\s+DOC|` +
		`TRUONG\s+BAN|CHINH\s+PHU|BAN\s+THUONG\s+VU|BAN\s+CHAP\s+HANH)\b`)
	// "T/M", "K/T", "T/L" open a party khối ký (Hướng dẫn 36-HD/VPTW)
	sigOpenRe = regexp.MustCompile(`^(TM|KT|TL|TUQ|Q)\s*\.|^(T/M|K/T|T/L)\s|` +
		`^(KY\s+DUYET|QUYEN|KIEM\s+NGHIEM|CHU\s+TICH|PHO\s+CHU\s+TICH|` +
		`THU\s+TUONG|PHO\s+THU\s+TUONG|BO\s+TRUONG|PHO\s+BO\s+TRUONG|` +
		`GIAM\s+DOC|PHO\s+GIAM\s+DOC|THU\s+TRUONG|PHO\s+THU\s+TRUONG|` +
		`TRUONG\s+(PHONG|BAN|DOAN|KHOA|CHI\s+NHANH)|PHO\s+TRUONG|` +
		`TO\s+TRUONG|CHU\s+NHAN|TONG\s+GIAM\s+DOC|PHO\s+TONG|` +
		`VIEN\s+TRUONG|PHO\s+VIEN|CHANH\s+VAN\s+PHONG|PHO\s+CHANH|` +
		`N[/.]?D|U\.?B\.?N\.?D\.?|TONG\s+BIEN\s+TAP|BI\s+THU|PHO\s+BI\s+THU|` +
		`TRUONG\s+BAN|PHO\s+TRUONG\s+BAN|PHAT\s+NGON\s+VIEN|` +
		`DAI\s+DIEN|NGUOI\s+(KY|LAP|DUYET|VIET|GHI)|THU\s+KY|CHU\s+TOA)\b`)
	orgKeywordRe = regexp.MustCompile(`^(UBND|UY\s+BAN|BO\s|SO\s|CUC|VU\s|TONG\s+CUC|CONG\s+TY|` +
		`TAP\s+DOAN|TRUONG\s+(DAI\s+HOC|HOC)|VIEN|BAN\s|PHONG|` +
		`DAI\s+SU\s+QUAN|LANH\s+SU|QUAN|HUYEN|PHUONG|XA|THI\s+XA|` +
		`THANH\s+PHO|TINH|CHINH\s+PHU|QUOC\s+HOI|DANG|MAT\s+TRAN|` +
		`CONG\s+DOAN|DOAN\s+TN|HOI\s|TRUNG\s+TAM|SO\s+HUU|NHA\s+NUOC)`)
	// a collective-body name continued by its locality on the next line:
	// "ỦY BAN NHÂN DÂN / TỈNH THỪA THIÊN HUẾ"
	genericOrgRe = regexp.MustCompile(`^(UY\s+BAN\s+NHAN\s+DAN|HOI\s+DONG\s+NHAN\s+DAN|UY\s+BAN\s+MAT\s+TRAN\s+TO\s+QUOC(\s+VIET\s+NAM)?|BAN\s+CHAP\s+HANH|BAN\s+THUONG\s+VU|DANG\s+UY|DANG\s+BO)$`)
	wrapEndRe    = regexp.MustCompile(`[,\-–&]$|\s(VA|CUA)$`)
	// a line continuing the one above: "SỞ TÀI NGUYÊN" / "VÀ MÔI TRƯỜNG"
	wrapStartRe   = regexp.MustCompile(`^(VA|CUA)\s`)
	daKyNameRe    = regexp.MustCompile(`^\(?\s*(DA\s+KY|KY\s+SO|DAU)`)
	daKyRe        = regexp.MustCompile(`^\(\s*(DA\s+KY|KY|DAU)`)
	symbolCodeRe  = regexp.MustCompile(`/\s*(?:\d{4}\s*/\s*)?([A-ZĐ]+)(\d*)`)
	docTypeLineRe []docTypePattern
)

// typedSoPrefix: the type names a QPPL writes before "số:".
const typedSoPrefix = `(LUAT|BO\s+LUAT|PHAP\s+LENH|LENH|NGHI\s+QUYET(\s+LIEN\s+TICH)?|NGHI\s+DINH|QUYET\s+DINH|THONG\s+TU(\s+LIEN\s+TICH)?|CHI\s+THI)`

type docTypePattern struct {
	slug string
	re   *regexp.Regexp
}

// nonTypeSymbols are ký hiệu that are not a document type ("QH14" Luật/NQ
// of Quốc hội, "L-CTN" Lệnh, "VBHN" văn bản hợp nhất): never công văn.
var nonTypeSymbols = map[string]bool{"QH": true, "UBTVQH": true, "CTN": true, "L": true, "VBHN": true}

func init() {
	// tên loại line patterns, longest name first ("THONG TU LIEN TICH"
	// before "THONG TU"), anchored at the start of the line
	defs := make([]vl.DocTypeDef, len(vl.DocTypes))
	copy(defs, vl.DocTypes)
	sort.SliceStable(defs, func(i, j int) bool { return runeLen(defs[i].Name) > runeLen(defs[j].Name) })
	for _, d := range defs {
		pat := `^` + strings.Join(strings.Fields(fold(d.Name)), `\s+`) + `\b`
		docTypeLineRe = append(docTypeLineRe, docTypePattern{d.Slug, regexp.MustCompile(pat)})
	}
}

// Element is a positioned text fragment: one per paragraph, or one per side
// of a tab-split paragraph.
type element struct {
	para *Para
	zone string
	text string
	// sub is the line of a paragraph read line by line (para is then its
	// LineView), -1 for a whole paragraph
	sub int
}

// quocHieuLineRe is a line holding the quốc hiệu alone — not a sentence
// quoting "nước Cộng hòa xã hội chủ nghĩa Việt Nam".
var quocHieuLineRe = regexp.MustCompile(`^CONG\s*HOA?\s*XA\s*HOI\s*CHU\s*NGHIA\s*VIET\s*NAM\.?$`)

// lineSplit returns the content lines (indexes into p.Lines()) of a
// paragraph to read line by line, nil to read it whole. A web export puts
// "CỘNG HÒA …⏎Độc lập …⏎-----", "KT. THỦ TƯỚNG⏎PHÓ THỦ TƯỚNG⏎⏎Tên" or
// "Nơi nhận:⏎- …;⏎- …" in one paragraph: it is split when a line is the
// quốc hiệu or tiêu ngữ, when it opens with "Nơi nhận", or when it opens a
// khối ký and is short, unjustified lines. Soft breaks inside body prose
// keep the paragraph whole.
func lineSplit(p *Para) []int {
	if p.Zone == ZoneSplit || !strings.Contains(p.Text, "\n") {
		return nil
	}
	lines := p.Lines()
	var content []int
	short := true
	for k, ln := range lines {
		t := strings.TrimSpace(ln)
		if t == "" || ruleLineRe.MatchString(t) {
			continue
		}
		content = append(content, k)
		if runeLen(t) > 80 {
			short = false
		}
	}
	if len(content) < 2 {
		return nil
	}
	for _, k := range content {
		if f := fold(lines[k]); quocHieuLineRe.MatchString(f) || tieuNguRe.MatchString(f) || partyTitleRe.MatchString(f) {
			return content
		}
	}
	first := fold(lines[content[0]])
	if noiNhanRe.MatchString(first) {
		return content
	}
	if short && p.Alignment != "justify" && p.Alignment != "both" && sigOpenRe.MatchString(first) {
		return content
	}
	return nil
}

func elements(l *Layout) []element {
	var els []element
	for _, p := range l.Paragraphs {
		if p.Zone == ZoneSplit {
			if p.LeftText != "" && !ruleLineRe.MatchString(p.LeftText) {
				els = append(els, element{p, ZoneLeft, p.LeftText, -1})
			}
			if p.RightText != "" && !ruleLineRe.MatchString(p.RightText) {
				els = append(els, element{p, ZoneRight, p.RightText, -1})
			}
			continue
		}
		if subs := lineSplit(p); subs != nil {
			lines := p.Lines()
			for _, k := range subs {
				els = append(els, element{p.LineView(k), p.Zone, strings.TrimSpace(lines[k]), k})
			}
			continue
		}
		text := strings.TrimSpace(p.Text)
		if text == "" || ruleLineRe.MatchString(text) {
			continue
		}
		els = append(els, element{p, p.Zone, text, -1})
	}
	return els
}

// Component is one NĐ30 part found in the document.
type Component struct {
	Found bool     `json:"found"`
	Paras []int    `json:"paras"`
	Text  string   `json:"text"`
	Zone  string   `json:"zone"`
	Zones []string `json:"-"` // aligned with Paras
	Texts []string `json:"-"`
	Pos   *int     `json:"-"` // first element position, for the order check
	// FooterParas are indexes into Layout.FooterParas for a component read
	// from the page footer (ô 13); such a component has no body paragraph.
	FooterParas []int `json:"footer_paras,omitempty"`
	// Subs: for a paragraph read line by line (lineSplit), the lines of it
	// (Para.LineView indexes) this component holds; a paragraph absent
	// here belongs to it whole.
	Subs map[int][]int `json:"-"`
}

func hasInt(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// ParaViews are the component's paragraphs as the rules measure them, in
// order: a paragraph it holds whole, or each of its lines (Para.LineView)
// of a paragraph read line by line; zones are the matching Zones entries.
func (c *Component) ParaViews(l *Layout) ([]*Para, []string) {
	var out []*Para
	var zones []string
	for i, idx := range c.Paras {
		if idx < 0 || idx >= len(l.Paragraphs) {
			continue
		}
		z := ""
		if i < len(c.Zones) {
			z = c.Zones[i]
		}
		p := l.Paragraphs[idx]
		if subs := c.Subs[idx]; len(subs) > 0 {
			for _, k := range subs {
				out = append(out, p.LineView(k))
				zones = append(zones, z)
			}
			continue
		}
		out = append(out, p)
		zones = append(zones, z)
	}
	return out, zones
}

// Segmentation is the component assignment of a document.
type Segmentation struct {
	order        []string // insertion order of component keys
	Components   map[string]*Component
	ParaLabels   map[int]string
	DetectedType string
	ParaTable    map[int]*int
	// Bounds are where the heuristic found the head and the tail blocks
	// (Segment only).
	Bounds Bounds
	// agencyGuessed: the cơ quan lines share one weight and were split into
	// chủ quản / ban hành by wording alone.
	agencyGuessed bool
	// tailBody: a nội dung line sits inside the tail block (a second
	// signature column, a signature set as body text).
	tailBody bool
	bodyPos  []int
	// QuocHieuLines counts the lines holding the quốc hiệu alone, anywhere
	// in the document: two when a second header opens an attached
	// document, more in a collection of forms (scope.go).
	QuocHieuLines int
	// FormMarker is the "Mẫu số … theo Thông tư …" line at the top of a
	// form issued by a specialised act (scope.go), "" when there is none.
	FormMarker string
	// ModelNotND30 is the labeling model's reason for calling the document
	// no văn bản hành chính under NĐ30, "" when it did not (or no model
	// labelled it); ScopeOf weighs it only when code cannot tell.
	ModelNotND30 string
}

// Bounds locate the NĐ30 blocks in element (= labeling unit) indexes. All
// components but nội dung sit in the head (ô 1–5, 9a, thẩm quyền, căn cứ,
// 10a/10b, 11) or the tail (ô 7, 9b, 12); ô 13 sits in the page footer.
type Bounds struct {
	// HeadEnd is one past the head block: header, trích yếu, thẩm quyền,
	// căn cứ, Kính gửi — the first nội dung element.
	HeadEnd int
	// TailStart is the first element of the signature / Nơi nhận region:
	// after the last body paragraph, moved back over the short, unjustified
	// lines just above it (a signature the heuristic read as body).
	TailStart int
	// PhuLucStart is the first phụ lục element (len(elements) when none).
	PhuLucStart int
	// HeadOK / TailOK: the head (a header anchor or a trích yếu) and the
	// tail (a Nơi nhận or a signature) were found.
	HeadOK, TailOK bool
}

func newSegmentation(l *Layout) *Segmentation {
	s := &Segmentation{Components: map[string]*Component{}, ParaLabels: map[int]string{},
		DetectedType: "unknown", ParaTable: map[int]*int{}}
	for _, p := range l.Paragraphs {
		s.ParaTable[p.Index] = p.TableIndex
	}
	els := elements(l)
	s.QuocHieuLines = len(quocHieuElements(els))
	s.FormMarker = formMarker(els)
	return s
}

var emptyComponent = &Component{Paras: []int{}}

// Comp returns the component or an empty, not-found one.
func (s *Segmentation) Comp(key string) *Component {
	if c, ok := s.Components[key]; ok {
		return c
	}
	return emptyComponent
}

// Keys returns component keys in the order they were first found.
func (s *Segmentation) Keys() []string { return append([]string(nil), s.order...) }

// add records a unit of component key: paragraph para, or line sub of it
// (sub >= 0, a paragraph read line by line — Para.LineView).
func (s *Segmentation) add(key string, para int, text, zone string, pos, sub int) {
	c, ok := s.Components[key]
	if !ok {
		c = &Component{Paras: []int{}, Zone: zone}
		s.Components[key] = c
		s.order = append(s.order, key)
	}
	c.Found = true
	if c.Pos == nil || pos < *c.Pos {
		c.Pos = ptr(pos)
	}
	seen := false
	for _, p := range c.Paras {
		if p == para {
			seen = true
			break
		}
	}
	if !seen {
		c.Paras = append(c.Paras, para)
		c.Zones = append(c.Zones, zone)
	}
	if sub >= 0 {
		if c.Subs == nil {
			c.Subs = map[int][]int{}
		}
		if !hasInt(c.Subs[para], sub) {
			c.Subs[para] = append(c.Subs[para], sub)
		}
	}
	if text != "" {
		c.Texts = append(c.Texts, text)
		c.Text = strings.Join(c.Texts, "\n")
	}
	if _, ok := s.ParaLabels[para]; !ok {
		s.ParaLabels[para] = key
	}
}

// addSignature records the whole signature block once chức danh / người ký
// are known.
func (s *Segmentation) addSignature() {
	cd, nk := s.Comp("chuc_danh"), s.Comp("nguoi_ky")
	if !cd.Found && !nk.Found {
		return
	}
	set := map[int]bool{}
	for _, p := range append(append([]int{}, cd.Paras...), nk.Paras...) {
		set[p] = true
	}
	paras := make([]int, 0, len(set))
	for p := range set {
		paras = append(paras, p)
	}
	sort.Ints(paras)
	zones := make([]string, len(paras))
	for i := range zones {
		zones[i] = ZoneRight
	}
	var texts []string
	for _, t := range []string{cd.Text, nk.Text} {
		if t != "" {
			texts = append(texts, t)
		}
	}
	if _, ok := s.Components["signature"]; !ok {
		s.order = append(s.order, "signature")
	}
	var subs map[int][]int
	for _, c := range []*Component{cd, nk} {
		for p, ks := range c.Subs {
			if subs == nil {
				subs = map[int][]int{}
			}
			for _, k := range ks {
				if !hasInt(subs[p], k) {
					subs[p] = append(subs[p], k)
				}
			}
		}
	}
	for _, ks := range subs {
		sort.Ints(ks)
	}
	s.Components["signature"] = &Component{Found: true, Paras: paras, Zones: zones,
		Text: strings.Join(texts, "\n"), Zone: ZoneRight, Pos: cd.Pos, Subs: subs}
}

func looksAgency(text, folded string) bool {
	if folded == "" {
		return false
	}
	if soKyHieuRe.MatchString(folded) || matRe.MatchString(folded) || khanRe.MatchString(folded) {
		return false
	}
	if orgKeywordRe.MatchString(folded) {
		return true
	}
	letters, upper := 0, 0
	for _, r := range text {
		if unicode.IsLetter(r) {
			letters++
			if unicode.IsUpper(r) {
				upper++
			}
		}
	}
	return letters >= 4 && float64(upper)/float64(letters) > 0.8
}

func typeFromLine(line string) string {
	f := fold(line)
	for _, d := range docTypeLineRe {
		if d.re.MatchString(f) {
			return d.slug
		}
	}
	return ""
}

// typeFromSymbol: "Số: 12/QĐ-UBND" → quyet_dinh; "Số: 12/UBND-VP" and
// "Số: 45/PA05-Đ4" (no type code, unit codes) → cong_van; "24/2018/QH14",
// "05/VBHN-BCT" → "".
func typeFromSymbol(soKyHieu string) string {
	t := strings.ToUpper(norm.NFC.String(soKyHieu))
	m := symbolCodeRe.FindStringSubmatch(t)
	if m == nil {
		return ""
	}
	code := m[1]
	if m[2] != "" {
		// letters run straight into digits: an issuing unit's code ("PA05",
		// "PV01" of Công an units, "QH14"), never a type code — the type
		// code stands alone before "-" ("QĐ-", "BC-")
		if nonTypeSymbols[code] {
			return ""
		}
		return "cong_van"
	}
	if d := vl.DocTypeBySymbol(code); d != nil {
		return d.Slug
	}
	if nonTypeSymbols[code] {
		return ""
	}
	return "cong_van"
}

// DetectType reads the loại văn bản: the type heading opening the trích yếu
// wins ("BÁO CÁO / Kết quả thực hiện Nghị quyết…" is a báo cáo), then the
// ký hiệu in the số, then "V/v" (công văn), then a short heading line.
func DetectType(trichYeu, fallbackScan, soKyHieu string) string {
	var lines []string
	for _, ln := range strings.Split(trichYeu, "\n") {
		if strings.TrimSpace(ln) != "" {
			lines = append(lines, ln)
		}
	}
	if len(lines) > 0 {
		if t := typeFromLine(lines[0]); t != "" {
			return t
		}
		if trichVVRe.MatchString(fold(lines[0])) {
			return "cong_van"
		}
	}
	if soKyHieu != "" {
		// "Nghị quyết số: 162/2024/QH15" names its type
		if t := typeFromLine(soKyHieu); t != "" {
			return t
		}
		if t := typeFromSymbol(soKyHieu); t != "" {
			return t
		}
	}
	if fallbackScan != "" {
		scan := strings.Split(fallbackScan, "\n")
		if len(scan) > 60 {
			scan = scan[:60]
		}
		for _, ln := range scan {
			if runeLen(fold(ln)) <= 40 {
				if t := typeFromLine(ln); t != "" {
					return t
				}
			}
		}
	}
	return "unknown"
}

type leftLine struct {
	pos int
	el  element
}

// banHanhLineCount: how many trailing agency lines form the cơ quan ban
// hành. NĐ30 prints chủ quản regular and ban hành bold, so the trailing run
// sharing the last line's weight is ban hành when the lines above differ;
// with uniform weight a generic collective name or a line broken after a
// comma/connector joins the line below it.
func banHanhLineCount(lines []leftLine) int {
	n := len(lines)
	isBold := func(i int) bool { b := lines[i].el.para.Bold; return b != nil && *b }
	last := isBold(n - 1)
	k := 1
	for k < n && isBold(n-k-1) == last {
		k++
	}
	if k == n {
		k = 1
		for k < n {
			fp := fold(strings.TrimSpace(lines[n-k-1].el.text))
			if genericOrgRe.MatchString(fp) || wrapEndRe.MatchString(fp) {
				k++
				continue
			}
			break
		}
	}
	// a block opening with "VÀ ..." continues the line above it
	for k < n && wrapStartRe.MatchString(fold(strings.TrimSpace(lines[n-k].el.text))) {
		k++
	}
	return k
}

func isHeaderLine(f string) bool {
	return quocHieuRe.MatchString(f) || tieuNguRe.MatchString(f) || partyTitleRe.MatchString(f) ||
		diaDanhRe.MatchString(f) || soKyHieuRe.MatchString(f)
}

func isTrue(b *bool) bool { return b != nil && *b }

// floatingBox: a text-box line of the header region that carries no header
// pattern — a form number ("Mẫu số: 140BH …"), a stamp — which floats over
// the header and is not part of its columns or of the trích yếu.
func floatingBox(el element, f string) bool {
	return el.para.Source == "textbox" && !isHeaderLine(f) && !chiDanRe.MatchString(f)
}

// isHeading: a full-width line that opens the trích yếu block.
func isHeading(el element, f string) bool {
	p := el.para
	if el.zone != ZoneFull || (p.InTable && p.TableLayout == "columns") {
		return false
	}
	if quocHieuRe.MatchString(f) || tieuNguRe.MatchString(f) || diaDanhRe.MatchString(f) || partyTitleRe.MatchString(f) {
		return false
	}
	if trichVVRe.MatchString(f) {
		return true
	}
	if p.Alignment != "center" {
		return false
	}
	return isTrue(p.Bold) || p.TextIsUpper || isTrue(p.Caps) || typeFromLine(el.text) != ""
}

// headerEnd is the element index one past the two-column header: the
// leading run of side-by-side content plus full-width lines carrying a
// header pattern or an agency name (kept pending until header content
// follows them), possibly over several tables.
func headerEnd(els []element) int {
	seenAnchor := false
	end, pending := 0, 0
	limit := len(els)
	if limit > 40 {
		limit = 40
	}
	for i := 0; i < limit; i++ {
		el := els[i]
		f := fold(el.text)
		side := el.zone == ZoneLeft || el.zone == ZoneRight || el.para.Zone == ZoneSplit
		if isHeaderLine(f) || matRe.MatchString(f) || side {
			seenAnchor = seenAnchor || !side || isHeaderLine(f)
			end, pending = i+1, 0
			continue
		}
		if floatingBox(el, f) {
			// a text box ("Mẫu số …", a stamp) floats over the header; it
			// neither extends nor ends it
			continue
		}
		if chiDanRe.MatchString(f) {
			// ô 11 sits by the header (with độ mật / độ khẩn)
			end, pending = i+1, 0
			continue
		}
		if isHeading(el, f) && !trichVVRe.MatchString(f) {
			break
		}
		if trichVVRe.MatchString(f) || khanRe.MatchString(f) {
			end, pending = i+1, 0
			continue
		}
		if looksAgency(el.text, f) && pending < 3 {
			pending++
			if !seenAnchor {
				end = i + 1
			}
			continue
		}
		break
	}
	if seenAnchor {
		return end
	}
	return 0
}

// isDiaChi: an ô 13 line — it opens with, or holds two of, Địa chỉ / ĐT /
// Điện thoại / Fax / Email / Website.
func isDiaChi(folded string) bool {
	return diaChiStartRe.MatchString(folded) || len(diaChiAnyRe.FindAllStringIndex(folded, -1)) >= 2
}

func pLeftish(p *Para) bool {
	ind := 0.0
	if p.IndentLeftMM != nil {
		ind = *p.IndentLeftMM
	}
	return (p.Alignment == "left" || p.Alignment == "justify") && ind < 40
}

func pRightish(p *Para) bool {
	if p.Alignment == "right" {
		return true
	}
	return p.Alignment == "center" && p.IndentLeftMM != nil && *p.IndentLeftMM > 60
}

// Segment assigns components with the positional heuristic.
func Segment(l *Layout) *Segmentation {
	seg := newSegmentation(l)
	els := elements(l)
	if len(els) == 0 {
		return seg
	}
	hEnd := headerEnd(els)
	// a văn bản của Đảng (party.go): tiêu đề instead of quốc hiệu, the
	// trích yếu of a công văn under the số without "V/v"
	party := partyHeader(els[:hEnd])

	// ---- header: left column (cơ quan, số), right column
	var left []leftLine
	trichOpened := false
	for hi, el := range els[:hEnd] {
		f := fold(el.text)
		if f == "" {
			continue
		}
		idx := el.para.Index
		if chiDanRe.MatchString(f) || (party && partyChiDanRe.MatchString(f)) {
			seg.add("chi_dan_luu_hanh", idx, el.text, el.zone, hi, el.sub)
			continue
		}
		if floatingBox(el, f) && !matRe.MatchString(f) && !khanRe.MatchString(f) {
			continue
		}
		if matRe.MatchString(f) {
			seg.add("do_mat", idx, el.text, el.zone, hi, el.sub)
			continue
		}
		if khanRe.MatchString(f) && el.zone != ZoneFull && runeLen(f) < 30 {
			seg.add("do_khan", idx, el.text, el.zone, hi, el.sub)
			continue
		}
		switch {
		case partyTitleRe.MatchString(f):
			seg.add("tieu_de_dang", idx, el.text, el.zone, hi, el.sub)
		case tieuNguRe.MatchString(f):
			seg.add("tieu_ngu", idx, el.text, el.zone, hi, el.sub)
		case quocHieuRe.MatchString(f):
			seg.add("quoc_hieu", idx, el.text, el.zone, hi, el.sub)
		case diaDanhRe.MatchString(f):
			seg.add("dia_danh_ngay_thang", idx, el.text, el.zone, hi, el.sub)
		case soKyHieuRe.MatchString(f) && el.zone != ZoneRight:
			seg.add("so_ky_hieu", idx, el.text, ZoneLeft, hi, el.sub)
		case trichVVRe.MatchString(f):
			// "V/v ..." under the số ký hiệu, usually in the left cell
			seg.add("trich_yeu", idx, el.text, el.zone, hi, el.sub)
			trichOpened = true
		case el.zone == ZoneLeft || (el.zone == ZoneFull && pLeftish(el.para)) ||
			(el.zone == ZoneFull && !seg.Comp("quoc_hieu").Found && !seg.Comp("tieu_de_dang").Found):
			switch {
			case trichOpened:
				seg.add("trich_yeu", idx, el.text, ZoneLeft, hi, el.sub)
			case party && seg.Comp("so_ky_hieu").Found && !looksAgency(el.text, f) && partyCongVanLine(seg, el):
				// HD36 ô 5b: a party công văn's trích yếu, no "V/v"
				seg.add("trich_yeu", idx, el.text, ZoneLeft, hi, el.sub)
				trichOpened = true
			case seg.Comp("so_ky_hieu").Found && !looksAgency(el.text, f):
				seg.add("header_left_other", idx, el.text, ZoneLeft, hi, el.sub)
			default:
				left = append(left, leftLine{hi, el})
			}
		case el.zone == ZoneRight || (el.zone == ZoneFull && pRightish(el.para)):
			seg.add("header_right_other", idx, el.text, ZoneRight, hi, el.sub)
		default:
			seg.add("header_other", idx, el.text, el.zone, hi, el.sub)
		}
	}
	if len(left) > 0 {
		nBH := banHanhLineCount(left)
		// same weight throughout: the split rests on wording; trusted when
		// the ban hành block opens with an agency keyword ("SỞ ...")
		seg.agencyGuessed = nBH < len(left) && uniformWeight(left) &&
			!orgKeywordRe.MatchString(fold(left[len(left)-nBH].el.text))
		for _, ll := range left[:len(left)-nBH] {
			seg.add("co_quan_chu_quan", ll.el.para.Index, ll.el.text, ZoneLeft, ll.pos, ll.el.sub)
		}
		for _, ll := range left[len(left)-nBH:] {
			seg.add("co_quan_ban_hanh", ll.el.para.Index, ll.el.text, ZoneLeft, ll.pos, ll.el.sub)
		}
	}

	// ---- trích yếu: the full-width heading after the header
	trichStart := -1
	if !trichOpened {
		scanTo := hEnd + 8
		if scanTo > len(els) {
			scanTo = len(els)
		}
		for i := hEnd; i < scanTo; i++ {
			if chiDanRe.MatchString(fold(els[i].text)) {
				continue
			}
			if isHeading(els[i], fold(els[i].text)) {
				trichStart = i
				break
			}
			if els[i].zone == ZoneFull && runeLen(els[i].text) > 120 {
				break // body paragraph reached — no heading
			}
		}
	}
	bodyStart := hEnd
	if trichStart >= 0 {
		seg.add("trich_yeu", els[trichStart].para.Index, els[trichStart].text, ZoneFull, trichStart, els[trichStart].sub)
		j := trichStart + 1
		for j < len(els) {
			el := els[j]
			p := el.para
			f := fold(el.text)
			if el.zone != ZoneFull || p.Alignment != "center" || canCuRe.MatchString(f) || kinhGuiRe.MatchString(f) ||
				chiDanRe.MatchString(f) {
				break
			}
			upper := p.TextIsUpper || isTrue(p.Caps)
			if thamQuyenRe.MatchString(f) && strings.TrimSpace(el.text) != "" && upper {
				seg.add("tham_quyen_ban_hanh", p.Index, el.text, ZoneFull, j, el.sub)
				j++
				continue
			}
			if seg.Comp("tham_quyen_ban_hanh").Found {
				if upper { // "KHÓA VIII, KỲ HỌP THỨ 10"
					seg.add("tham_quyen_ban_hanh", p.Index, el.text, ZoneFull, j, el.sub)
					j++
					continue
				}
				break
			}
			seg.add("trich_yeu", p.Index, el.text, ZoneFull, j, el.sub)
			j++
		}
		bodyStart = j
	}

	scanEnd := len(els)
	if scanEnd > 60 {
		scanEnd = 60
	}
	scan := make([]string, 0, scanEnd)
	for _, e := range els[:scanEnd] {
		scan = append(scan, e.text)
	}
	seg.DetectedType = DetectType(seg.Comp("trich_yeu").Text, strings.Join(scan, "\n"), seg.Comp("so_ky_hieu").Text)

	// ---- tail: nơi nhận + signature, split by zone (often one table)
	// a second header (quốc hiệu) after the body opens a document attached
	// to this one (the Quy chế a Quyết định issues): the main document's
	// tail ends there, the rest is its phụ lục
	attach := attachedStart(els, hEnd, bodyStart)
	tail := els[bodyStart:attach]
	isRight := func(el element) bool {
		return el.zone == ZoneRight || (el.zone == ZoneFull && pRightish(el.para))
	}
	noiNhan := -1
	for k, el := range tail {
		if noiNhanRe.MatchString(fold(el.text)) {
			noiNhan = k // last wins: a phụ lục may quote "Nơi nhận"
		}
	}
	noiNhanEnd := len(tail)
	if noiNhan >= 0 {
		for k := noiNhan + 1; k < len(tail); k++ {
			el := tail[k]
			if phuLucRe.MatchString(fold(el.text)) ||
				(el.zone == ZoneFull && el.para.Alignment == "center" && (isTrue(el.para.Bold) || el.para.TextIsUpper)) {
				noiNhanEnd = k
				break
			}
		}
	}
	// signature = the right-zone run after the last body paragraph; it
	// opens at the first authority / upper-case line
	lastBody := -1
	for k, el := range tail[:noiNhanEnd] {
		if noiNhan >= 0 && k >= noiNhan {
			break
		}
		if !isRight(el) && el.zone != ZoneLeft {
			lastBody = k
		}
	}
	sigOpen := -1
	var cands []int
	for k := lastBody + 1; k < noiNhanEnd; k++ {
		if isRight(tail[k]) {
			cands = append(cands, k)
		}
	}
	for _, k := range cands {
		if sigOpenRe.MatchString(fold(tail[k].text)) || tail[k].para.TextIsUpper {
			sigOpen = k
			break
		}
	}
	if sigOpen < 0 && len(cands) > 0 {
		sigOpen = cands[0]
	}

	// ô 12/13 sit under nơi nhận (or under the signature when there is no
	// nơi nhận): only lines past that point are considered
	bottomFrom := noiNhan
	if bottomFrom < 0 {
		bottomFrom = sigOpen
	}
	kyHieuK := -1
	if noiNhan >= 0 {
		for k := noiNhanEnd - 1; k > noiNhan; k-- {
			el := tail[k]
			if isRight(el) || isDiaChi(fold(el.text)) {
				continue
			}
			if t := strings.TrimSpace(el.text); runeLen(t) <= 12 && (kyHieuSTRe.MatchString(t) || (party && partyKyHieuSTRe.MatchString(t))) {
				kyHieuK = k
			}
			break // only the last left line of the nơi nhận block
		}
	}

	type posEl struct {
		pos int
		el  element
	}
	var sigEls []posEl
	for k, el := range tail {
		f := fold(el.text)
		p := el.para
		pos := bodyStart + k
		inNoiNhan := noiNhan >= 0 && noiNhan <= k && k < noiNhanEnd
		inSig := sigOpen >= 0 && sigOpen <= k && k < noiNhanEnd
		switch {
		case inSig && isRight(el):
			sigEls = append(sigEls, posEl{pos, el})
		case chiDanRe.MatchString(f) && !seg.Comp("noi_dung").Found:
			// ô 11 belongs to the head block; in the body it is body text
			seg.add("chi_dan_luu_hanh", p.Index, el.text, el.zone, pos, el.sub)
		case bottomFrom >= 0 && bottomFrom < k && k < noiNhanEnd && isDiaChi(f):
			seg.add("dia_chi_co_quan", p.Index, el.text, el.zone, pos, el.sub)
		case k == kyHieuK:
			seg.add("ky_hieu_soan_thao", p.Index, el.text, ZoneLeft, pos, el.sub)
		case inNoiNhan && !isRight(el):
			seg.add("noi_nhan", p.Index, el.text, ZoneLeft, pos, el.sub)
		case phuLucRe.MatchString(f) && k >= noiNhanEnd-1:
			seg.add("phu_luc", p.Index, el.text, el.zone, pos, el.sub)
		case kinhGuiRe.MatchString(f):
			seg.add("kinh_gui", p.Index, el.text, el.zone, pos, el.sub)
		case canCuRe.MatchString(f) && !seg.Comp("noi_dung").Found:
			seg.add("can_cu", p.Index, el.text, ZoneFull, pos, el.sub)
		case party && partyCanCu(seg, el, f):
			// HD36: "- Căn cứ …;" lines, upright
			seg.add("can_cu", p.Index, el.text, ZoneFull, pos, el.sub)
		case canCuMore(seg, el, f):
			// "Nhằm …;", "Theo đề nghị của …;" continue the căn cứ block
			seg.add("can_cu", p.Index, el.text, ZoneFull, pos, el.sub)
		case k >= noiNhanEnd:
			seg.add("phu_luc", p.Index, el.text, el.zone, pos, el.sub)
		default:
			seg.add("noi_dung", p.Index, el.text, el.zone, pos, el.sub)
			seg.bodyPos = append(seg.bodyPos, pos)
		}
	}

	// chức danh = authority lines; người ký = the last right-zone line when
	// it reads as a name (not all caps, not "(Đã ký)")
	if len(sigEls) > 0 {
		last := sigEls[len(sigEls)-1]
		lf := fold(last.el.text)
		nameLike := !last.el.para.TextIsUpper && !sigOpenRe.MatchString(lf) && !daKyNameRe.MatchString(lf)
		head := sigEls
		if nameLike && len(sigEls) > 1 {
			head = sigEls[:len(sigEls)-1]
		}
		for _, pe := range head {
			if daKyRe.MatchString(fold(pe.el.text)) {
				continue // "(Đã ký)" / "(Ký, đóng dấu)" placeholders
			}
			seg.add("chuc_danh", pe.el.para.Index, pe.el.text, ZoneRight, pe.pos, pe.el.sub)
		}
		if nameLike && len(sigEls) > 1 {
			seg.add("nguoi_ky", last.el.para.Index, last.el.text, ZoneRight, last.pos, last.el.sub)
		}
	}
	for k, el := range els[attach:] {
		seg.add("phu_luc", el.para.Index, el.text, el.zone, attach+k, el.sub)
	}
	seg.addSignature()
	seg.addFooterDiaChi(l)
	seg.Bounds = blockBounds(els, seg, hEnd, bodyStart, lastBody, noiNhan, sigOpen,
		hEnd > 0 || trichOpened || trichStart >= 0)
	markParty(l, seg)
	return seg
}

// quocHieuElements are the elements holding the quốc hiệu alone.
func quocHieuElements(els []element) []int {
	var out []int
	for i, el := range els {
		if quocHieuLineRe.MatchString(fold(el.text)) {
			out = append(out, i)
		}
	}
	return out
}

// formMarkerScan is how many leading elements formMarker reads: the marker
// sits in the top corner, before or beside the quốc hiệu.
const formMarkerScan = 12

var (
	// a line opening "Mẫu số: 140BH", "Mẫu số 01/BB", "Mẫu biên bản số 02"
	formMarkerOpenRe = regexp.MustCompile(`^\(?\s*MAU(\s+[A-Z]+){0,3}\s+SO\s*:?\s*[0-9]`)
	// "… (ban hành kèm) theo Thông tư số 128/2025/TT-BCA …"
	formMarkerActRe = regexp.MustCompile(`\b(KEM\s+)?THEO\s+(THONG TU( LIEN TICH)?|TTLT|TT|QUYET DINH|QD|NGHI DINH|ND)\b(\s+SO)?\s*:?\s*([0-9]+\s*/\s*[0-9]{4}\s*/\s*[A-Z0-9-]+)?`)
	// NĐ30's own appendix templates ("Mẫu 1.1 …" copied from Nghị định
	// 30/2020/NĐ-CP) are văn bản hành chính, not a specialised form
	formMarkerND30Re = regexp.MustCompile(`\b30\s*/\s*2020\s*/\s*ND-CP\b`)
)

// formMarker returns the form-number line of a form issued by a
// specialised act — "Mẫu số: 140BH theo TT số 128/2025/TT-BCA ngày
// 19/12/2025" (tố tụng, xử lý vi phạm hành chính, nghiệp vụ forms) — read
// from the first elements, text boxes included. A marker line may be split
// over several text-box paragraphs, so the lines after the opening one are
// joined until the act reference shows up. "" when there is none or the
// act is NĐ30 itself.
func formMarker(els []element) string {
	n := len(els)
	if n > formMarkerScan {
		n = formMarkerScan
	}
	for i := 0; i < n; i++ {
		if !formMarkerOpenRe.MatchString(fold(els[i].text)) {
			continue
		}
		line := strings.TrimSpace(els[i].text)
		for j := i; j < n && j < i+3; j++ {
			if j > i {
				line += " " + strings.TrimSpace(els[j].text)
			}
			f := fold(line)
			if formMarkerND30Re.MatchString(f) {
				return ""
			}
			if formMarkerActRe.MatchString(f) {
				return truncateRunes(line, 160)
			}
		}
	}
	return ""
}

// formCollectionMin quốc hiệu lines make a collection of forms (biểu mẫu),
// each with its own header (scope.go).
const formCollectionMin = 3

// attachedStart is the element where an attached document begins — the
// header block (same paragraph or same table) of the second quốc hiệu, when
// the first one heads the document and the second comes after the body —
// or len(els). Forms attached after it (more quốc hiệu) stay in the
// attached part.
func attachedStart(els []element, hEnd, bodyStart int) int {
	qh := quocHieuElements(els)
	if len(qh) < 2 || qh[0] >= hEnd || qh[1] <= bodyStart {
		return len(els)
	}
	a := qh[1]
	sameBlock := func(x, y element) bool {
		if x.para.Index == y.para.Index {
			return true
		}
		return x.para.TableIndex != nil && y.para.TableIndex != nil && *x.para.TableIndex == *y.para.TableIndex
	}
	for a > bodyStart && sameBlock(els[a-1], els[a]) {
		a--
	}
	return a
}

var (
	// the resolution line ("QUYẾT ĐỊNH:", "Thủ tướng Chính phủ ban hành
	// Quyết định …") and the first article / chapter end the căn cứ block
	resolutionRe = regexp.MustCompile(`^(QUYET\s+DINH|QUYET\s+NGHI|NGHI\s+QUYET|CHI\s+THI)\s*:|` +
		`\bBAN\s+HANH\s+(QUYET\s+DINH|NGHI\s+DINH|THONG\s+TU|NGHI\s+QUYET|CHI\s+THI|LUAT|PHAP\s+LENH)\b`)
	articleRe = regexp.MustCompile(`^(DIEU\s+\d|CHUONG\s+[IVXLC\d]|PHAN\s+[IVXLC\d]|MUC\s+\d)`)
)

// canCuMore: an italic line ending in ";" or "." right after the căn cứ
// lines ("Nhằm triển khai …;", "Theo đề nghị của …;") still belongs to the
// căn cứ block — until the resolution line, the first Điều, or a
// non-italic paragraph.
func canCuMore(seg *Segmentation, el element, f string) bool {
	if !seg.Comp("can_cu").Found || seg.Comp("noi_dung").Found || !isTrue(el.para.Italic) {
		return false
	}
	t := strings.TrimSpace(el.text)
	if !strings.HasSuffix(t, ";") && !strings.HasSuffix(t, ".") {
		return false
	}
	return !resolutionRe.MatchString(f) && !articleRe.MatchString(f)
}

// partyCongVanLine: a left-column line under a party số ký hiệu opens a
// công văn's trích yếu (ô 5b) when the ký hiệu is a công văn's (CV) or has
// no code, or when the line is set like one (italic, 12 pt or less); a
// nghị quyết's stray line there leaves its heading alone.
func partyCongVanLine(seg *Segmentation, el element) bool {
	switch partyTypeFromSymbol(seg.Comp("so_ky_hieu").Text) {
	case "", "dang_cong_van":
		return true
	}
	p := el.para
	return isTrue(p.Italic) || (p.SizePt != nil && *p.SizePt <= 12)
}

// partyCanCu: a căn cứ line of a party document — "- Căn cứ …;" opening
// the block (HD36 prints căn cứ upright, often as a dashed list), or a
// line ending in ";" or "," right after it, until the first Điều or the
// resolution line.
func partyCanCu(seg *Segmentation, el element, f string) bool {
	if seg.Comp("noi_dung").Found {
		return false
	}
	bare := strings.TrimLeft(f, "-–—+ ")
	if canCuRe.MatchString(bare) {
		return true
	}
	if !seg.Comp("can_cu").Found {
		return false
	}
	t := strings.TrimSpace(el.text)
	if !strings.HasSuffix(t, ";") && !strings.HasSuffix(t, ",") {
		return false
	}
	return !resolutionRe.MatchString(bare) && !articleRe.MatchString(bare)
}

// addFooterDiaChi reads ô 13 from the page-1 footer when the body has none:
// the footer lines that read as an address / phone / email line.
func (s *Segmentation) addFooterDiaChi(l *Layout) {
	if s.Comp("dia_chi_co_quan").Found {
		return
	}
	var idx []int
	var texts []string
	for _, p := range l.FooterParas {
		t := strings.TrimSpace(p.Text)
		if t != "" && isDiaChi(fold(t)) {
			idx = append(idx, p.Index)
			texts = append(texts, t)
		}
	}
	if len(idx) == 0 {
		return
	}
	s.Components["dia_chi_co_quan"] = &Component{Found: true, Paras: []int{}, Text: strings.Join(texts, "\n"),
		Zone: ZoneFooter, Texts: texts, FooterParas: idx}
	s.order = append(s.order, "dia_chi_co_quan")
}

// sigLike: a line that may belong to a signature block — short and not
// justified like body prose.
func sigLike(el element) bool {
	return el.para.Alignment != "justify" && el.para.Alignment != "both" && runeLen(el.text) <= 80
}

func blockBounds(els []element, seg *Segmentation, hEnd, bodyStart, lastBody, noiNhan, sigOpen int, headOK bool) Bounds {
	n := len(els)
	b := Bounds{HeadEnd: bodyStart, TailStart: n, PhuLucStart: n, HeadOK: headOK,
		TailOK: noiNhan >= 0 || sigOpen >= 0}
	first := func(key string) int {
		if c := seg.Comp(key); c.Found && c.Pos != nil {
			return *c.Pos
		}
		return -1
	}
	if p := first("phu_luc"); p >= 0 {
		b.PhuLucStart = p
	}
	b.TailStart = bodyStart + lastBody + 1
	if b.TailStart > b.PhuLucStart {
		b.TailStart = b.PhuLucStart
	}
	if p := first("noi_dung"); p >= 0 && p < b.TailStart {
		b.HeadEnd = p
	} else {
		b.HeadEnd = b.TailStart
	}
	if b.HeadEnd < hEnd {
		b.HeadEnd = hEnd
	}
	// a signature set as body text (centred / tab-indented, not in the right
	// column) ends the body in the heuristic's eyes: walk back over it
	for k := 0; k < 8 && b.TailStart-1 > b.HeadEnd && sigLike(els[b.TailStart-1]); k++ {
		b.TailStart--
	}
	for _, p := range seg.bodyPos {
		if p >= b.TailStart && p < b.PhuLucStart {
			seg.tailBody = true
		}
	}
	return b
}

// uniformWeight: every cơ quan line has the same bold flag.
func uniformWeight(lines []leftLine) bool {
	for _, ll := range lines[1:] {
		if isTrue(ll.el.para.Bold) != isTrue(lines[0].el.para.Bold) {
			return false
		}
	}
	return true
}
