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
	soKyHieuRe = regexp.MustCompile(`^SO\s*[:.]|^SO\s+\d|^SO\s*/|^SO\s+\S*/\s*[A-Z]|^\d{1,5}[A-Z]?\s*/\s*(\d{4}\s*/\s*)?[A-Z]{1,8}\b`)
	// "Huế, ngày 05 tháng 10 năm 2026", drafts with blank day/month.
	// Anchored so a title "Báo cáo ... năm 2025" does not match.
	diaDanhRe   = regexp.MustCompile(`^([^,\d]{2,60},\s*)?NGAY\s*[\d.…_ ]{0,8}\s*THANG\s*[\d.…_ ]{0,8}\s*NAM\b`)
	matRe       = regexp.MustCompile(`^(DO\s+)?MAT(\s*[:.]|\s*$)|^DO\s+MAT\b`)
	khanRe      = regexp.MustCompile(`^(DO\s+KHAN\s*[:.]?\s*)?(KHAN|HOA\s*TOC|THUONG\s*KHAN)\b`)
	noiNhanRe   = regexp.MustCompile(`^NOI\s*NHAN\s*[:.]?`)
	kinhGuiRe   = regexp.MustCompile(`^KINH\s+GUI\s*[:.]?`)
	canCuRe     = regexp.MustCompile(`^(CAN\s+CU|THEO\s+DE\s+NGHI|XET\s+DE\s+NGHI|XET\s+TO\s+TRINH)\b`)
	phuLucRe    = regexp.MustCompile(`^PHU\s+LUC\b`)
	trichVVRe   = regexp.MustCompile(`^V\s*/\s*V\b|^VE\s+VIEC\b`)
	ruleLineRe  = regexp.MustCompile(`^[\s\p{Z}_\-–—=~.*·•]+$`)
	thamQuyenRe = regexp.MustCompile(`^(CHU\s+TICH|BO\s+TRUONG|GIAM\s+DOC|THU\s+TUONG|TONG\s+GIAM\s+DOC|` +
		`CHANH\s+AN|VIEN\s+TRUONG|UY\s+BAN\s+NHAN\s+DAN|HOI\s+DONG\s+NHAN\s+DAN|` +
		`HOI\s+DONG\s+QUAN\s+TRI|THU\s+TRUONG|CUC\s+TRUONG|GIAM\s+DOC|` +
		`TRUONG\s+BAN|CHINH\s+PHU|BAN\s+THUONG\s+VU|BAN\s+CHAP\s+HANH)\b`)
	sigOpenRe = regexp.MustCompile(`^(TM|KT|TL|TUQ|Q)\s*\.|` +
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
	genericOrgRe  = regexp.MustCompile(`^(UY\s+BAN\s+NHAN\s+DAN|HOI\s+DONG\s+NHAN\s+DAN|UY\s+BAN\s+MAT\s+TRAN\s+TO\s+QUOC(\s+VIET\s+NAM)?|BAN\s+CHAP\s+HANH|BAN\s+THUONG\s+VU|DANG\s+UY|DANG\s+BO)$`)
	wrapEndRe     = regexp.MustCompile(`[,\-–&]$|\s(VA|CUA)$`)
	daKyNameRe    = regexp.MustCompile(`^\(?\s*(DA\s+KY|KY\s+SO|DAU)`)
	daKyRe        = regexp.MustCompile(`^\(\s*(DA\s+KY|KY|DAU)`)
	symbolCodeRe  = regexp.MustCompile(`/\s*(?:\d{4}\s*/\s*)?([A-ZĐ]+)`)
	docTypeLineRe []docTypePattern
)

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
}

func elements(l *Layout) []element {
	var els []element
	for _, p := range l.Paragraphs {
		if p.Zone == ZoneSplit {
			if p.LeftText != "" && !ruleLineRe.MatchString(p.LeftText) {
				els = append(els, element{p, ZoneLeft, p.LeftText})
			}
			if p.RightText != "" && !ruleLineRe.MatchString(p.RightText) {
				els = append(els, element{p, ZoneRight, p.RightText})
			}
			continue
		}
		text := strings.TrimSpace(p.Text)
		if text == "" || ruleLineRe.MatchString(text) {
			continue
		}
		els = append(els, element{p, p.Zone, text})
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
}

// Segmentation is the component assignment of a document.
type Segmentation struct {
	order        []string // insertion order of component keys
	Components   map[string]*Component
	ParaLabels   map[int]string
	DetectedType string
	ParaTable    map[int]*int
}

func newSegmentation(l *Layout) *Segmentation {
	s := &Segmentation{Components: map[string]*Component{}, ParaLabels: map[int]string{},
		DetectedType: "unknown", ParaTable: map[int]*int{}}
	for _, p := range l.Paragraphs {
		s.ParaTable[p.Index] = p.TableIndex
	}
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

func (s *Segmentation) add(key string, para int, text, zone string, pos int) {
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
	s.Components["signature"] = &Component{Found: true, Paras: paras, Zones: zones,
		Text: strings.Join(texts, "\n"), Zone: ZoneRight, Pos: cd.Pos}
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

// typeFromSymbol: "Số: 12/QĐ-UBND" → quyet_dinh; "Số: 12/UBND-VP" (no type
// code) → cong_van; "24/2018/QH14", "05/VBHN-BCT" → "".
func typeFromSymbol(soKyHieu string) string {
	t := strings.ToUpper(norm.NFC.String(soKyHieu))
	m := symbolCodeRe.FindStringSubmatch(t)
	if m == nil {
		return ""
	}
	code := m[1]
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
	if k < n {
		return k
	}
	k = 1
	for k < n {
		fp := fold(strings.TrimSpace(lines[n-k-1].el.text))
		if genericOrgRe.MatchString(fp) || wrapEndRe.MatchString(fp) {
			k++
			continue
		}
		break
	}
	return k
}

func isHeaderLine(f string) bool {
	return quocHieuRe.MatchString(f) || tieuNguRe.MatchString(f) ||
		diaDanhRe.MatchString(f) || soKyHieuRe.MatchString(f)
}

func isTrue(b *bool) bool { return b != nil && *b }

// isHeading: a full-width line that opens the trích yếu block.
func isHeading(el element, f string) bool {
	p := el.para
	if el.zone != ZoneFull || (p.InTable && p.TableLayout == "columns") {
		return false
	}
	if quocHieuRe.MatchString(f) || tieuNguRe.MatchString(f) || diaDanhRe.MatchString(f) {
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

	// ---- header: left column (cơ quan, số), right column
	var left []leftLine
	trichOpened := false
	for hi, el := range els[:hEnd] {
		f := fold(el.text)
		if f == "" {
			continue
		}
		idx := el.para.Index
		if matRe.MatchString(f) {
			seg.add("do_mat", idx, el.text, el.zone, hi)
			continue
		}
		if khanRe.MatchString(f) && el.zone != ZoneFull && runeLen(f) < 30 {
			seg.add("do_khan", idx, el.text, el.zone, hi)
			continue
		}
		switch {
		case tieuNguRe.MatchString(f):
			seg.add("tieu_ngu", idx, el.text, el.zone, hi)
		case quocHieuRe.MatchString(f):
			seg.add("quoc_hieu", idx, el.text, el.zone, hi)
		case diaDanhRe.MatchString(f):
			seg.add("dia_danh_ngay_thang", idx, el.text, el.zone, hi)
		case soKyHieuRe.MatchString(f) && el.zone != ZoneRight:
			seg.add("so_ky_hieu", idx, el.text, ZoneLeft, hi)
		case trichVVRe.MatchString(f):
			// "V/v ..." under the số ký hiệu, usually in the left cell
			seg.add("trich_yeu", idx, el.text, el.zone, hi)
			trichOpened = true
		case el.zone == ZoneLeft || (el.zone == ZoneFull && pLeftish(el.para)) ||
			(el.zone == ZoneFull && !seg.Comp("quoc_hieu").Found):
			switch {
			case trichOpened:
				seg.add("trich_yeu", idx, el.text, ZoneLeft, hi)
			case seg.Comp("so_ky_hieu").Found && !looksAgency(el.text, f):
				seg.add("header_left_other", idx, el.text, ZoneLeft, hi)
			default:
				left = append(left, leftLine{hi, el})
			}
		case el.zone == ZoneRight || (el.zone == ZoneFull && pRightish(el.para)):
			seg.add("header_right_other", idx, el.text, ZoneRight, hi)
		default:
			seg.add("header_other", idx, el.text, el.zone, hi)
		}
	}
	if len(left) > 0 {
		nBH := banHanhLineCount(left)
		for _, ll := range left[:len(left)-nBH] {
			seg.add("co_quan_chu_quan", ll.el.para.Index, ll.el.text, ZoneLeft, ll.pos)
		}
		for _, ll := range left[len(left)-nBH:] {
			seg.add("co_quan_ban_hanh", ll.el.para.Index, ll.el.text, ZoneLeft, ll.pos)
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
		seg.add("trich_yeu", els[trichStart].para.Index, els[trichStart].text, ZoneFull, trichStart)
		j := trichStart + 1
		for j < len(els) {
			el := els[j]
			p := el.para
			f := fold(el.text)
			if el.zone != ZoneFull || p.Alignment != "center" || canCuRe.MatchString(f) || kinhGuiRe.MatchString(f) {
				break
			}
			upper := p.TextIsUpper || isTrue(p.Caps)
			if thamQuyenRe.MatchString(f) && strings.TrimSpace(el.text) != "" && upper {
				seg.add("tham_quyen_ban_hanh", p.Index, el.text, ZoneFull, j)
				j++
				continue
			}
			if seg.Comp("tham_quyen_ban_hanh").Found {
				if upper { // "KHÓA VIII, KỲ HỌP THỨ 10"
					seg.add("tham_quyen_ban_hanh", p.Index, el.text, ZoneFull, j)
					j++
					continue
				}
				break
			}
			seg.add("trich_yeu", p.Index, el.text, ZoneFull, j)
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
	tail := els[bodyStart:]
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
		case inNoiNhan && !isRight(el):
			seg.add("noi_nhan", p.Index, el.text, ZoneLeft, pos)
		case phuLucRe.MatchString(f) && k >= noiNhanEnd-1:
			seg.add("phu_luc", p.Index, el.text, el.zone, pos)
		case kinhGuiRe.MatchString(f):
			seg.add("kinh_gui", p.Index, el.text, el.zone, pos)
		case canCuRe.MatchString(f) && !seg.Comp("noi_dung").Found:
			seg.add("can_cu", p.Index, el.text, ZoneFull, pos)
		case k >= noiNhanEnd:
			seg.add("phu_luc", p.Index, el.text, el.zone, pos)
		default:
			seg.add("noi_dung", p.Index, el.text, el.zone, pos)
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
			seg.add("chuc_danh", pe.el.para.Index, pe.el.text, ZoneRight, pe.pos)
		}
		if nameLike && len(sigEls) > 1 {
			seg.add("nguoi_ky", last.el.para.Index, last.el.text, ZoneRight, last.pos)
		}
	}
	seg.addSignature()
	return seg
}
