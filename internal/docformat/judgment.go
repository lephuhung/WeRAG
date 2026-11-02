package docformat

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	vl "github.com/Tencent/WeKnora/internal/vietnamese_legal"
	"golang.org/x/text/unicode/norm"
)

// Code judgment of the Bảng kiểm of the-thuc-chung (skills/the-thuc-chung):
//
//  1. thành phần bắt buộc   2. ký thay   3. báo cáo người đứng đầu
//  4. nơi nhận   5. số ký hiệu, địa danh - ngày tháng   6. yêu cầu riêng
//
// The rules are data: a "judgment" list in the rule JSON (rules/*.json),
// one entry per rule, { "item": 1..6, "id", "desc", "kind", ...params,
// "severity" }. A type file's entry with the same id is merged field by
// field over the _base.json one (a list field is replaced whole), new ids
// are appended. Go implements only the kinds:
//
//   - required_components (item 1): the measured component.*.present checks.
//   - ky_thay (item 2): the chức vụ lines of each signature block (⏎ splits
//     a paragraph): a "PHÓ …" line needs "KT. <same title>" right above;
//     bad abbreviations (K/T, T/M, "Kt", "KT" without the dot) fail; a
//     collective authority (collective_regex on thẩm quyền ban hành, else
//     cơ quan ban hành) needs a "TM." line unless tm_exempt_regex matches
//     a line (TL./TUQ.).
//   - head_in_noi_nhan (item 3): only when a PHÓ line signs: a Nơi nhận
//     line naming the head — the title after KT./PHÓ or one of its
//     head_aliases, not preceded by "Phó" — with report_regex ("để báo
//     cáo", "để b/c", "b/c"); head without it → warn.
//   - noi_nhan_format (item 4): label "Nơi nhận:", every entry "- …" ending
//     in ";" or ".", a "- Lưu: VT, …" line.
//   - so_ky_hieu_date (item 5): "Số: NN/<ký hiệu>" (two digits at least,
//     empty số = chưa có số), the type code of the ký hiệu
//     (symbol_mode "type_code": one of the type's Phụ lục III codes;
//     "no_type_code": công văn, no type code nor forbidden_codes); the
//     địa danh line "<Địa danh>, ngày DD tháng M năm YYYY" (day < 10 and
//     tháng 1, 2 with a leading zero; "tháng 07" is a warning; an
//     impossible date fails).
//   - type_requirements (item 6): a list of requirement checks over the
//     anchors and components (see Requirement).
//   - dang_tham_quyen, dang_chuc_vu, dang_noi_nhan, dang_so_ky_hieu_date
//     (items 2–5 of a văn bản của Đảng, Hướng dẫn 36-HD/VPTW):
//     judgment_party.go.
//
// A rule's "title" names its Bảng kiểm item (the party items differ).
//
// Every rule ends pass / warn / fail, "unknown" (the data cannot answer and
// the skill prescribes "không xác định được từ dữ liệu" — a model given
// the same brief could not answer either) or "undetermined" (code could
// not decide what a reader could: only these go to the model).

// judgmentCodeVersion changes whenever the judgment kinds or the report
// they write change, so cached evaluations are dropped (SkillsDigest).
const judgmentCodeVersion = "judgment-rules-v1"

// Judgment statuses.
const (
	JudgePass         = "pass"
	JudgeWarn         = "warn"
	JudgeFail         = "fail"
	JudgeUnknown      = "unknown"      // final: không xác định được từ dữ liệu
	JudgeUndetermined = "undetermined" // code could not decide: ask the model
)

// Judgment rule kinds.
const (
	KindRequiredComponents = "required_components"
	KindKyThay             = "ky_thay"
	KindHeadInNoiNhan      = "head_in_noi_nhan"
	KindNoiNhanFormat      = "noi_nhan_format"
	KindSoKyHieuDate       = "so_ky_hieu_date"
	KindTypeRequirements   = "type_requirements"
	KindAttachedHeading    = "attached_heading"
	KindDangThamQuyen      = "dang_tham_quyen"
	KindDangChucVu         = "dang_chuc_vu"
	KindDangNoiNhan        = "dang_noi_nhan"
	KindDangSoKyHieuDate   = "dang_so_ky_hieu_date"
)

// JudgmentRule is one judgment rule as written in the rule JSON.
type JudgmentRule struct {
	Item int    `json:"item"`
	ID   string `json:"id"`
	// Title names the rule's Bảng kiểm item; "" keeps checklistTitles.
	Title    string `json:"title,omitempty"`
	Desc     string `json:"desc,omitempty"`
	Kind     string `json:"kind"`
	Severity string `json:"severity,omitempty"`
	// Scope lists the check scopes the rule runs in (scope.go); none
	// means the full scope only.
	Scope []string `json:"scope,omitempty"`

	// ky_thay
	CollectiveRegex string `json:"collective_regex,omitempty"`
	// dang_tham_quyen: a collective body whose deputy signs without "T/M"
	DeputyExemptRegex string `json:"deputy_exempt_regex,omitempty"`
	TMExemptRegex     string `json:"tm_exempt_regex,omitempty"`
	TMSeverity        string `json:"tm_severity,omitempty"`
	BadAbbrevRegex    string `json:"bad_abbrev_regex,omitempty"`
	// head_in_noi_nhan
	HeadAliases map[string][]string `json:"head_aliases,omitempty"`
	ReportRegex string              `json:"report_regex,omitempty"`
	// noi_nhan_format
	LabelRegex string `json:"label_regex,omitempty"`
	ItemRegex  string `json:"item_regex,omitempty"`
	EndRegex   string `json:"end_regex,omitempty"`
	LuuRegex   string `json:"luu_regex,omitempty"`
	LuuVTRegex string `json:"luu_vt_regex,omitempty"`
	// dang_noi_nhan: the last entry's ending; a colon after "Lưu"
	LastEndRegex  string `json:"last_end_regex,omitempty"`
	LuuColonRegex string `json:"luu_colon_regex,omitempty"`
	// dang_chuc_vu: a cấp uỷ name in a chức vụ line, an "ỦY VIÊN" line, a
	// học hàm / học vị / quân hàm before the họ tên
	AgencyRegex    string `json:"agency_regex,omitempty"`
	MemberRegex    string `json:"member_regex,omitempty"`
	HonorificRegex string `json:"honorific_regex,omitempty"`
	// so_ky_hieu_date
	SymbolMode     string   `json:"symbol_mode,omitempty"`
	ForbiddenCodes []string `json:"forbidden_codes,omitempty"`
	// type_requirements
	Requirements []Requirement `json:"requirements,omitempty"`
	// attached_heading (scope.go)
	HeadingRegex string `json:"heading_regex,omitempty"`
	NumberRegex  string `json:"number_regex,omitempty"`
	DateRegex    string `json:"date_regex,omitempty"`
	IssuerRegex  string `json:"issuer_regex,omitempty"`
}

// Requirement is one check of a type_requirements rule. Checks:
//
//   - anchor_present: a body anchor line picked by one of Anchors (and
//     matching Regex when set) exists. With Regex and no line of those
//     anchors at all, the result is WhenAbsent (default: Severity).
//   - anchor_regex: the last Last (default 1) lines of Anchors[0], plus
//     the lines of Also, must include one matching Regex. No line of
//     Anchors[0] → WhenAbsent (default unknown); a cut line that could
//     hide the match → undetermined.
//   - component_present / component_absent: the Component is found / not.
//   - line_ends: every line of Component but the last matches Regex, the
//     last matches LastRegex (căn cứ "…;" then "…,").
//   - component_regex: the lines of Component (after Skip lines) against
//     Regex in Mode first (the first line must match; WarnRegex matching
//     it instead → warn), any (one line must match) or none (no line may
//     match). A missing component → WhenAbsent (default pass).
//
// When names a component that must be present for the check to apply.
// Regexes run on fold()ed text (upper case, no diacritics, Đ → D).
type Requirement struct {
	Check      string   `json:"check"`
	Desc       string   `json:"desc"`
	Severity   string   `json:"severity,omitempty"`
	Fix        string   `json:"fix,omitempty"`
	Anchor     string   `json:"anchor,omitempty"`
	Anchors    []string `json:"anchors,omitempty"`
	Also       []string `json:"also,omitempty"`
	Last       int      `json:"last,omitempty"`
	Component  string   `json:"component,omitempty"`
	Regex      string   `json:"regex,omitempty"`
	WarnRegex  string   `json:"warn_regex,omitempty"`
	LastRegex  string   `json:"last_regex,omitempty"`
	Mode       string   `json:"mode,omitempty"`
	Skip       int      `json:"skip,omitempty"`
	When       string   `json:"when,omitempty"`
	WhenAbsent string   `json:"when_absent,omitempty"`
}

// mergeJudgment merges a type's judgment entries over the base ones by
// id, field by field.
func mergeJudgment(baseRaw, specRaw json.RawMessage) ([]JudgmentRule, error) {
	var base, spec []map[string]json.RawMessage
	if len(baseRaw) > 0 {
		if err := json.Unmarshal(baseRaw, &base); err != nil {
			return nil, fmt.Errorf("judgment: %w", err)
		}
	}
	if len(specRaw) > 0 {
		if err := json.Unmarshal(specRaw, &spec); err != nil {
			return nil, fmt.Errorf("judgment: %w", err)
		}
	}
	idOf := func(m map[string]json.RawMessage) string {
		var id string
		_ = json.Unmarshal(m["id"], &id)
		return id
	}
	merged := make([]map[string]json.RawMessage, 0, len(base)+len(spec))
	index := map[string]int{}
	for _, m := range base {
		index[idOf(m)] = len(merged)
		merged = append(merged, m)
	}
	for _, m := range spec {
		if i, ok := index[idOf(m)]; ok {
			out := map[string]json.RawMessage{}
			for k, v := range merged[i] {
				out[k] = v
			}
			for k, v := range m {
				out[k] = v
			}
			merged[i] = out
			continue
		}
		index[idOf(m)] = len(merged)
		merged = append(merged, m)
	}
	raw, err := json.Marshal(merged)
	if err != nil {
		return nil, err
	}
	var out []JudgmentRule
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("judgment: %w", err)
	}
	return out, nil
}

// JudgmentFinding is one problem a judgment rule found.
type JudgmentFinding struct {
	Severity  string `json:"severity"` // error | warn
	Component string `json:"component,omitempty"`
	Rule      string `json:"rule"`           // quy định
	Line      string `json:"line,omitempty"` // dòng vi phạm, nguyên văn
	Fix       string `json:"fix,omitempty"`  // cách sửa
	Summary   string `json:"summary"`        // the Bảng kiểm line's text
}

// JudgmentItem is one Bảng kiểm item.
type JudgmentItem struct {
	Item     int               `json:"item"`
	Title    string            `json:"title"`
	Status   string            `json:"status"`
	Note     string            `json:"note,omitempty"`   // "không áp dụng", "chưa có số"
	Reason   string            `json:"reason,omitempty"` // why unknown / undetermined
	By       string            `json:"by"`               // rules | llm
	Findings []JudgmentFinding `json:"findings,omitempty"`
	// Line is the item's Bảng kiểm line as written by the model, when the
	// model judged it.
	Line string `json:"line,omitempty"`
}

// JudgmentInfo records how the evaluation was made.
type JudgmentInfo struct {
	Method       string         `json:"method"` // none (scoped, no Bảng kiểm) | rules | rules+llm | llm
	Items        []JudgmentItem `json:"items,omitempty"`
	Undetermined []int          `json:"undetermined,omitempty"` // items sent to the model
	Seconds      float64        `json:"seconds"`
	LLMSeconds   float64        `json:"llm_seconds,omitempty"`
	Error        string         `json:"error,omitempty"`
}

// checklistTitles are the Bảng kiểm item names.
var checklistTitles = map[int]string{
	1: "Thành phần bắt buộc",
	2: "Ký thay",
	3: "Báo cáo người đứng đầu",
	4: "Nơi nhận",
	5: "Số ký hiệu, địa danh - ngày tháng",
	6: "Yêu cầu riêng theo loại",
}

// ruleOut is what one rule decided.
type ruleOut struct {
	findings     []JudgmentFinding
	note         string
	unknown      string // reason
	undetermined string // reason
}

func (o *ruleOut) add(sev, comp, rule, line, fix, summary string) {
	if sev != "warn" {
		sev = "error"
	}
	o.findings = append(o.findings, JudgmentFinding{Severity: sev, Component: comp, Rule: rule, Line: line, Fix: fix, Summary: summary})
}

// Judge decides the Bảng kiểm items from the report with the judgment
// rules of its rule set.
func Judge(r *Report) []JudgmentItem {
	rs, err := RuleSetForReport(r)
	if err != nil {
		rs = &RuleSet{}
	}
	jd := newJudgeData(r)
	outs := map[int][]ruleOut{}
	titles := map[int]string{}
	for _, jr := range rs.Judgment {
		if jr.Item < 1 || jr.Item > 6 {
			continue
		}
		if jr.Title != "" && titles[jr.Item] == "" {
			titles[jr.Item] = jr.Title
		}
		var o ruleOut
		switch jr.Kind {
		case KindRequiredComponents:
			o = judgeRequired(r)
		case KindKyThay:
			o = jd.kyThay(jr)
		case KindHeadInNoiNhan:
			o = jd.headInNoiNhan(jr)
		case KindNoiNhanFormat:
			o = jd.noiNhanFormat(jr)
		case KindSoKyHieuDate:
			o = jd.soKyHieuDate(jr)
		case KindTypeRequirements:
			o = jd.typeRequirements(jr)
		case KindAttachedHeading:
			o = jd.attachedHeading(jr)
		case KindDangThamQuyen:
			o = jd.dangThamQuyen(jr)
		case KindDangChucVu:
			o = jd.dangChucVu(jr)
		case KindDangNoiNhan:
			o = jd.dangNoiNhan(jr)
		case KindDangSoKyHieuDate:
			o = jd.dangSoKyHieuDate(jr)
		default:
			o.undetermined = fmt.Sprintf("loại quy tắc %q chưa được hỗ trợ", jr.Kind)
		}
		outs[jr.Item] = append(outs[jr.Item], o)
	}
	items := make([]JudgmentItem, 0, 6)
	for n := 1; n <= 6; n++ {
		it := JudgmentItem{Item: n, Title: checklistTitles[n], By: "rules"}
		if t := titles[n]; t != "" {
			it.Title = t
		}
		rules := outs[n]
		if len(rules) == 0 {
			// no rule for this item: the model judges it
			it.Status, it.Reason = JudgeUndetermined, "không có quy tắc cho mục này"
			items = append(items, it)
			continue
		}
		var unknown, undet, notes []string
		for _, o := range rules {
			it.Findings = append(it.Findings, o.findings...)
			if o.unknown != "" {
				unknown = append(unknown, o.unknown)
			}
			if o.undetermined != "" {
				undet = append(undet, o.undetermined)
			}
			if o.note != "" {
				notes = append(notes, o.note)
			}
		}
		hasErr, hasWarn := false, false
		for _, f := range it.Findings {
			if f.Severity == "error" {
				hasErr = true
			} else {
				hasWarn = true
			}
		}
		switch {
		case hasErr:
			it.Status = JudgeFail
		case len(undet) > 0:
			it.Status, it.Reason = JudgeUndetermined, strings.Join(undet, "; ")
		case hasWarn:
			it.Status = JudgeWarn
		case len(unknown) > 0:
			it.Status, it.Reason = JudgeUnknown, strings.Join(unknown, "; ")
		default:
			it.Status, it.Note = JudgePass, strings.Join(notes, "; ")
		}
		items = append(items, it)
	}
	return items
}

// ---------------------------------------------------------------------------
// data access

type jline struct {
	raw, f string // raw text (NFC, trimmed) and fold()ed
	zone   string
}

type judgeData struct {
	r      *Report
	format map[string]ComponentFormat
}

func newJudgeData(r *Report) *judgeData {
	jd := &judgeData{r: r, format: map[string]ComponentFormat{}}
	for _, cf := range r.Format {
		jd.format[cf.Key] = cf
	}
	return jd
}

func (jd *judgeData) found(key string) bool {
	c, ok := jd.r.Components[key]
	return ok && c.Found
}

// lines returns a component's lines, a paragraph's ⏎ splitting it.
func (jd *judgeData) lines(key string) []jline {
	var out []jline
	for _, lf := range jd.format[key].Lines {
		for _, part := range strings.Split(lf.Text, "\n") {
			raw := strings.TrimSpace(norm.NFC.String(part))
			if raw == "" {
				continue
			}
			out = append(out, jline{raw: raw, f: fold(raw), zone: lf.Zone})
		}
	}
	return out
}

func (jd *judgeData) joined(key string) string {
	var parts []string
	for _, l := range jd.lines(key) {
		parts = append(parts, l.f)
	}
	return strings.Join(parts, " ")
}

// jre compiles a rule regex once; an invalid one is nil.
func jre(pattern string) *regexp.Regexp {
	if pattern == "" {
		return nil
	}
	return anchorRegexp(pattern)
}

func quote(s string) string { return "\"" + s + "\"" }

// titleAfter returns the words after the first one ("PHÓ GIÁM ĐỐC" →
// "GIÁM ĐỐC", "KT. GIÁM ĐỐC" → "GIÁM ĐỐC").
func titleAfter(raw string) string {
	raw = strings.TrimSpace(raw)
	if i := strings.Index(raw, "."); i >= 0 && i <= 4 {
		return strings.TrimSpace(raw[i+1:])
	}
	if f := strings.Fields(raw); len(f) > 1 {
		return strings.Join(f[1:], " ")
	}
	return ""
}

// sameTitle: equal, or one a word-prefix of the other ("CHU TICH" /
// "CHU TICH THUONG TRUC").
func sameTitle(a, b string) bool {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	return a == b || strings.HasPrefix(a, b+" ") || strings.HasPrefix(b, a+" ")
}

// ---------------------------------------------------------------------------
// item 1

func judgeRequired(r *Report) ruleOut {
	var o ruleOut
	for _, c := range r.Checks {
		if !strings.HasPrefix(c.ID, "component.") || !strings.HasSuffix(c.ID, ".present") ||
			c.Severity != "error" || c.Status != StatusFail {
			continue
		}
		key := strings.TrimSuffix(strings.TrimPrefix(c.ID, "component."), ".present")
		name := ComponentName(key)
		src := "NĐ30 Phụ lục I"
		if r.Party() {
			src = PartyGuide + ", Phụ lục 1"
		}
		o.add("error", key, "Văn bản loại này phải có thành phần "+name+" ("+src+")", "",
			"Bổ sung "+name+" đúng vị trí theo "+ruleSource(r)+".", "thiếu "+name)
	}
	return o
}

// ---------------------------------------------------------------------------
// item 2

var (
	phoLineRe    = regexp.MustCompile(`^PHO\s+\S`)
	ktLineRe     = regexp.MustCompile(`^KT\s*\.\s*\S`)
	tmLineRe     = regexp.MustCompile(`^TM\s*\.\s*\S`)
	abbrevLineRe = regexp.MustCompile(`^(KT|TM|TL|TUQ|Q)(\s*\.)?\s+\S`)
	midPhoRe     = regexp.MustCompile(`^(KT|TM)\s*\..*\sPHO\s`)
)

// signatureBlocks groups the chức vụ lines by column.
func (jd *judgeData) signatureBlocks() [][]jline {
	var zones []string
	by := map[string][]jline{}
	for _, l := range jd.lines("chuc_danh") {
		if _, ok := by[l.zone]; !ok {
			zones = append(zones, l.zone)
		}
		by[l.zone] = append(by[l.zone], l)
	}
	out := make([][]jline, 0, len(zones))
	for _, z := range zones {
		out = append(out, by[z])
	}
	return out
}

// phoLines are the chức vụ lines of a deputy.
func (jd *judgeData) phoLines() []jline {
	var out []jline
	for _, b := range jd.signatureBlocks() {
		for _, l := range b {
			if phoLineRe.MatchString(l.f) {
				out = append(out, l)
			}
		}
	}
	return out
}

func (jd *judgeData) kyThay(jr JudgmentRule) ruleOut {
	var o ruleOut
	blocks := jd.signatureBlocks()
	if len(blocks) == 0 {
		if jd.found("nguoi_ky") {
			o.undetermined = "khối ký không tách được dòng chức vụ"
		} else {
			o.unknown = "không có khối ký (xem mục 1)"
		}
		return o
	}
	const comp = "chuc_danh"
	bad := jre(jr.BadAbbrevRegex)
	for _, b := range blocks {
		for i, l := range b {
			if bad != nil && bad.MatchString(l.f) {
				o.add("error", comp, "Viết tắt thẩm quyền ký phải là KT., TM., TL., TUQ., Q. (in hoa, có dấu chấm) — NĐ30 Điều 13",
					l.raw, "Sửa thành dạng viết tắt chuẩn (KT., TM., TL., TUQ., Q.).", quote(l.raw)+" viết tắt sai")
				continue
			}
			if m := abbrevLineRe.FindStringSubmatch(l.f); m != nil {
				prefix := []rune(l.raw)[:len([]rune(m[1]))]
				if m[2] == "" || strings.ToUpper(string(prefix)) != string(prefix) {
					o.add("error", comp, "Viết tắt thẩm quyền ký phải in hoa, có dấu chấm (KT., TM., TL., TUQ., Q.)",
						l.raw, "Sửa thành "+quote(m[1]+". "+titleAfter(l.raw))+".", quote(l.raw)+" viết tắt sai")
				}
			}
			if midPhoRe.MatchString(l.f) {
				o.undetermined = "dòng chức vụ " + quote(l.raw) + " gộp nhiều chức vụ trên một dòng"
				continue
			}
			if !phoLineRe.MatchString(l.f) {
				continue
			}
			title := titleAfter(l.raw)
			if i == 0 || !ktLineRe.MatchString(b[i-1].f) {
				above := ""
				if i > 0 {
					above = " (dòng trên là " + quote(b[i-1].raw) + ")"
				}
				o.add("error", comp, "Cấp phó ký thay người đứng đầu phải ghi \"KT. <chức vụ người đứng đầu>\" ngay trên chức vụ cấp phó — NĐ30 Điều 13",
					l.raw, "Thêm/sửa dòng ngay trên thành "+quote("KT. "+title)+".",
					quote(l.raw)+" không có dòng "+quote("KT. "+title)+" phía trên"+above)
				continue
			}
			ktTitle := titleAfter(b[i-1].raw)
			if !sameTitle(fold(ktTitle), fold(title)) {
				o.add("error", comp, "Chức vụ sau \"KT.\" phải trùng chức vụ người đứng đầu mà cấp phó ký thay",
					b[i-1].raw+" / "+l.raw, "Sửa thành "+quote("KT. "+title)+".",
					quote(b[i-1].raw)+" không khớp "+quote(l.raw))
			}
		}
	}
	// collective bodies sign "TM."
	if cre := jre(jr.CollectiveRegex); cre != nil {
		authority := jd.joined("tham_quyen_ban_hanh")
		if authority == "" {
			authority = jd.joined("co_quan_ban_hanh")
		}
		if authority != "" && cre.MatchString(authority) {
			hasTM, exempt := false, false
			ex := jre(jr.TMExemptRegex)
			var first jline
			for _, b := range blocks {
				for _, l := range b {
					if first.raw == "" {
						first = l
					}
					if tmLineRe.MatchString(l.f) {
						hasTM = true
					}
					if ex != nil && ex.MatchString(l.f) {
						exempt = true
					}
				}
			}
			if !hasTM && !exempt {
				sev := jr.TMSeverity
				if sev == "" {
					sev = "error"
				}
				o.add(sev, comp, "Văn bản của cơ quan làm việc theo chế độ tập thể ký thay mặt tập thể: \"TM. <tên tập thể>\" — NĐ30 Điều 13",
					first.raw, "Thêm dòng \"TM. <tên tập thể>\" (ví dụ \"TM. ỦY BAN NHÂN DÂN\") trên chức vụ người ký.",
					"thiếu \"TM.\" ("+quote(first.raw)+")")
			}
		}
	}
	return o
}

// ---------------------------------------------------------------------------
// item 3

var dashRe = regexp.MustCompile(`^[-–—]\s*`)
var dcRe = regexp.MustCompile(`^(D/C|DC|DONG CHI)\s+`)

func (jd *judgeData) headInNoiNhan(jr JudgmentRule) ruleOut {
	var o ruleOut
	pho := jd.phoLines()
	if len(pho) == 0 {
		o.note = "không áp dụng"
		return o
	}
	// the head: the title after KT. and after PHÓ, with its aliases
	titles := map[string]bool{}
	for _, b := range jd.signatureBlocks() {
		for _, l := range b {
			if ktLineRe.MatchString(l.f) || phoLineRe.MatchString(l.f) {
				if t := fold(titleAfter(l.raw)); t != "" {
					titles[t] = true
				}
			}
		}
	}
	for t := range titles {
		for _, a := range jr.HeadAliases[t] {
			titles[fold(a)] = true
		}
	}
	var alts []string
	for t := range titles {
		alts = append(alts, regexp.QuoteMeta(t))
	}
	sort.Slice(alts, func(i, j int) bool { return len(alts[i]) > len(alts[j]) })
	headRe := regexp.MustCompile(`\b(` + strings.Join(alts, "|") + `)\b`)
	reportRe := jre(jr.ReportRegex)
	const comp = "noi_nhan"
	headTitle := fold(titleAfter(pho[0].raw))
	rule := "Văn bản do cấp phó ký thay phải gửi người đứng đầu để báo cáo, ví dụ \"- " +
		sentenceCase(titleAfter(pho[0].raw)) + " (để b/c);\""
	lines := jd.lines("noi_nhan")
	if len(lines) == 0 {
		o.add("error", comp, rule, "", "Thêm Nơi nhận có dòng người đứng đầu \"(để báo cáo)\".",
			"không có Nơi nhận để báo cáo người đứng đầu")
		return o
	}
	var warnLine, maybe string
	for i, l := range lines {
		f := l.f
		if i == 0 && strings.HasPrefix(f, "NOI NHAN") {
			continue
		}
		f = dcRe.ReplaceAllString(dashRe.ReplaceAllString(f, ""), "")
		if strings.HasPrefix(f, "LUU") {
			continue
		}
		hasReport := reportRe != nil && reportRe.MatchString(f)
		head := false
		for _, loc := range headRe.FindAllStringIndex(f, -1) {
			before := strings.TrimSpace(f[:loc[0]])
			if !strings.HasSuffix(" "+before, " PHO") {
				head = true
				break
			}
		}
		switch {
		case head && hasReport:
			return o
		case head && warnLine == "":
			warnLine = l.raw
		case !head && hasReport && maybe == "" && !strings.HasPrefix(f, "PHO ") &&
			!strings.Contains(" "+f, " PHO "+headTitle) && !orgKeywordRe.MatchString(f):
			maybe = l.raw
		}
	}
	switch {
	case warnLine != "":
		o.add("warn", comp, rule, warnLine, "Ghi thêm \"(để báo cáo)\" hoặc \"(để b/c)\" sau người đứng đầu.",
			quote(warnLine)+" chưa ghi \"để báo cáo\"")
	case maybe != "":
		o.undetermined = "dòng " + quote(maybe) + " gửi để báo cáo nhưng hệ thống không nhận ra có phải người đứng đầu không"
	default:
		o.add("error", comp, rule, "", "Thêm dòng \"- "+sentenceCase(titleAfter(pho[0].raw))+" (để b/c);\" vào Nơi nhận.",
			"Nơi nhận không có người đứng đầu (để báo cáo) dù "+quote(pho[0].raw)+" ký")
	}
	return o
}

// ---------------------------------------------------------------------------
// item 4

func (jd *judgeData) noiNhanFormat(jr JudgmentRule) ruleOut {
	var o ruleOut
	const comp = "noi_nhan"
	lines := jd.lines(comp)
	if len(lines) == 0 {
		o.unknown = "không có Nơi nhận (xem mục 1)"
		return o
	}
	labelRe, itemRe, endRe := jre(jr.LabelRegex), jre(jr.ItemRegex), jre(jr.EndRegex)
	luuRe, vtRe := jre(jr.LuuRegex), jre(jr.LuuVTRegex)
	label := lines[0]
	body := lines
	if strings.HasPrefix(label.f, "NOI NHAN") {
		body = lines[1:]
		if labelRe != nil && !labelRe.MatchString(label.f) {
			o.add("error", comp, "Dòng đầu nơi nhận ghi \"Nơi nhận:\" (có dấu hai chấm)", label.raw,
				"Sửa thành \"Nơi nhận:\".", quote(label.raw)+" sai nhãn")
		}
	} else {
		o.add("error", comp, "Nơi nhận bắt đầu bằng dòng \"Nơi nhận:\"", label.raw,
			"Thêm dòng \"Nơi nhận:\" phía trên danh sách.", "thiếu nhãn \"Nơi nhận:\"")
	}
	// entries: a line without a dash after an unfinished one continues it
	type entry struct{ raw, f string }
	var entries []entry
	var noDash []string
	for _, l := range body {
		dashed := itemRe == nil || itemRe.MatchString(l.f)
		if !dashed && len(entries) > 0 && (endRe == nil || !endRe.MatchString(entries[len(entries)-1].f)) {
			e := &entries[len(entries)-1]
			e.raw += " " + l.raw
			e.f += " " + l.f
			continue
		}
		if !dashed {
			noDash = append(noDash, l.raw)
		}
		entries = append(entries, entry{l.raw, l.f})
	}
	if len(noDash) > 0 {
		o.add("error", comp, "Mỗi nơi nhận một dòng, bắt đầu bằng gạch đầu dòng và dấu cách \"- \"",
			strings.Join(noDash, " | "), "Thêm \"- \" ở đầu dòng.", quote(noDash[0])+" không bắt đầu bằng \"- \"")
	}
	var noEnd []string
	for _, e := range entries {
		if endRe != nil && !endRe.MatchString(e.f) {
			noEnd = append(noEnd, e.raw)
		}
	}
	if len(noEnd) > 0 {
		o.add("error", comp, "Mỗi dòng nơi nhận kết thúc bằng \";\", dòng cuối bằng \".\"",
			strings.Join(noEnd, " | "), "Thêm dấu \";\" (dòng cuối \".\").", quote(noEnd[0])+" thiếu dấu kết thúc")
	}
	luu := -1
	for i, e := range entries {
		if luuRe != nil && luuRe.MatchString(e.f) {
			luu = i
		}
	}
	switch {
	case luu < 0:
		o.add("error", comp, "Dòng cuối nơi nhận là \"- Lưu: VT, <viết tắt đơn vị soạn thảo>.\"", "",
			"Thêm dòng \"- Lưu: VT, <đơn vị soạn thảo>.\" ở cuối nơi nhận.", "thiếu dòng \"- Lưu: VT, …\"")
	case vtRe != nil && !vtRe.MatchString(entries[luu].f):
		o.add("error", comp, "Dòng lưu ghi \"Lưu: VT, …\" — VT (Văn thư) ngay sau \"Lưu:\"", entries[luu].raw,
			"Sửa thành \"- Lưu: VT, "+strings.TrimSpace(strings.TrimRight(lastAfterColon(entries[luu].raw), ".;"))+".\"",
			quote(entries[luu].raw)+" thiếu VT")
	}
	return o
}

func lastAfterColon(s string) string {
	if i := strings.Index(s, ":"); i >= 0 {
		return s[i+1:]
	}
	// "- Lưu Phòng." (no colon): what follows the "Lưu" label
	t := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(s), "-–• "))
	if rest, ok := cutPrefixFold(t, "lưu"); ok {
		return rest
	}
	return s
}

// ---------------------------------------------------------------------------
// item 5

var (
	soLineRe     = regexp.MustCompile(`^SO\s*(:)?\s*([^/]*?)\s*/\s*(.*)$`)
	soPlaceRe    = regexp.MustCompile(`^[\s._…\-]*$`)
	kyHieuRe     = regexp.MustCompile(`^(?:\d{4}\s*/\s*)?([A-ZĐ]+)(\d*)`)
	dateLineRe   = regexp.MustCompile(`^(.*?)(,?)\s*NGAY\b\s*(.*?)\s*\bTHANG\b\s*(.*?)\s*\bNAM\b\s*(.*?)\s*\.?$`)
	digitsOnlyRe = regexp.MustCompile(`^\d+$`)
)

func (jd *judgeData) soKyHieuDate(jr JudgmentRule) ruleOut {
	var o ruleOut
	var notes []string
	if lines := jd.lines("so_ky_hieu"); len(lines) > 0 {
		jd.judgeSo(jr, lines[0], &o, &notes)
	}
	if lines := jd.lines("dia_danh_ngay_thang"); len(lines) > 0 {
		jd.judgeDate(lines[0], &o, &notes)
	}
	if !jd.found("so_ky_hieu") && !jd.found("dia_danh_ngay_thang") {
		notes = append(notes, "không có số ký hiệu, ngày tháng")
	}
	o.note = strings.Join(notes, "; ")
	return o
}

func (jd *judgeData) judgeSo(jr JudgmentRule, l jline, o *ruleOut, notes *[]string) {
	const comp = "so_ky_hieu"
	m := soLineRe.FindStringSubmatch(l.f)
	if m == nil {
		if !strings.Contains(l.f, "/") {
			o.undetermined = "không đọc được số, ký hiệu " + quote(l.raw)
			return
		}
		o.add("error", comp, "Số, ký hiệu ghi dạng \"Số: <số>/<ký hiệu>\"", l.raw,
			"Sửa thành \"Số: <số>/<ký hiệu>\".", quote(l.raw)+" sai dạng \"Số: …/…\"")
		return
	}
	if m[1] == "" {
		o.add("error", comp, "Sau chữ \"Số\" có dấu hai chấm: \"Số: <số>/<ký hiệu>\"", l.raw,
			"Thêm dấu hai chấm sau chữ \"Số\".", quote(l.raw)+" thiếu dấu hai chấm sau \"Số\"")
	}
	num := strings.TrimSpace(m[2])
	switch {
	case soPlaceRe.MatchString(num):
		*notes = append(*notes, "chưa có số")
	case !digitsOnlyRe.MatchString(num):
		o.undetermined = "không đọc được số " + quote(l.raw)
		return
	case len(num) < 2:
		o.add("error", comp, "Số dưới 10 ghi thêm số 0 phía trước", l.raw,
			"Sửa thành \"Số: 0"+num+"/…\".", quote(l.raw)+" thiếu số 0 trước số "+num)
	}
	// the ký hiệu, from the raw text so Đ stays Đ
	raw := strings.ToUpper(norm.NFC.String(l.raw))
	i := strings.Index(raw, "/")
	if i < 0 {
		return
	}
	km := kyHieuRe.FindStringSubmatch(strings.TrimSpace(raw[i+1:]))
	if km == nil {
		o.undetermined = "không đọc được ký hiệu " + quote(l.raw)
		return
	}
	code, unitDigits := km[1], km[2] != ""
	docType := ""
	if jd.r.DocumentType != nil {
		docType = jd.r.DocumentType.Used
	}
	switch jr.SymbolMode {
	case "no_type_code":
		forbidden := false
		for _, c := range jr.ForbiddenCodes {
			if strings.EqualFold(c, code) {
				forbidden = true
			}
		}
		if !unitDigits && (forbidden || vl.DocTypeBySymbol(code) != nil) {
			o.add("error", comp, "Ký hiệu công văn là <viết tắt cơ quan>-<viết tắt đơn vị soạn thảo>, không có mã loại văn bản", l.raw,
				"Bỏ mã \""+code+"-\", ghi \"<viết tắt cơ quan>-<đơn vị soạn thảo>\" (ví dụ \"UBND-VP\").",
				quote(l.raw)+" có mã loại \""+code+"\"")
		}
	default: // type_code
		def := vl.DocTypeBySlug(docType)
		if def == nil || len(def.Symbols) == 0 {
			return
		}
		for _, s := range def.Symbols {
			if strings.EqualFold(s, code) && !unitDigits {
				return
			}
		}
		want := def.Symbols[0]
		if other := vl.DocTypeBySymbol(code); other != nil && !unitDigits {
			o.add("error", comp, "Ký hiệu loại văn bản phải khớp tên loại ("+def.Name+": "+want+"-)", l.raw,
				"Sửa mã loại thành \""+want+"-\".", "ký hiệu \""+code+"-\" ("+other.Name+") không khớp "+def.Name)
		} else {
			o.add("error", comp, "Ký hiệu gồm <viết tắt tên loại>-<viết tắt cơ quan> ("+def.Name+": "+want+"-)", l.raw,
				"Ghi mã loại \""+want+"-\" trước viết tắt cơ quan.", quote(l.raw)+" thiếu mã loại \""+want+"\"")
		}
	}
}

func (jd *judgeData) judgeDate(l jline, o *ruleOut, notes *[]string) {
	const comp = "dia_danh_ngay_thang"
	m := dateLineRe.FindStringSubmatch(l.f)
	if m == nil {
		o.undetermined = "không đọc được địa danh, ngày tháng " + quote(l.raw)
		return
	}
	place, comma := strings.TrimSpace(m[1]), m[2]
	d, mo, y := strings.TrimSpace(m[3]), strings.TrimSpace(m[4]), strings.TrimSpace(m[5])
	if place == "" {
		o.add("error", comp, "Ghi \"<Địa danh>, ngày … tháng … năm …\"; địa danh là tên đơn vị hành chính nơi cơ quan đóng trụ sở",
			l.raw, "Thêm địa danh trước \"ngày\".", quote(l.raw)+" thiếu địa danh")
	} else if comma == "" {
		o.add("warn", comp, "Sau địa danh có dấu phẩy: \"<Địa danh>, ngày …\"", l.raw,
			"Thêm dấu phẩy sau địa danh.", quote(l.raw)+" thiếu dấu phẩy sau địa danh")
	}
	blank := func(s string) bool { return soPlaceRe.MatchString(s) }
	if blank(d) || blank(mo) {
		*notes = append(*notes, "chưa ghi ngày tháng")
		return
	}
	if !digitsOnlyRe.MatchString(d) || !digitsOnlyRe.MatchString(mo) || !digitsOnlyRe.MatchString(y) {
		o.undetermined = "không đọc được ngày tháng " + quote(l.raw)
		return
	}
	dn, _ := strconv.Atoi(d)
	mn, _ := strconv.Atoi(mo)
	yn, _ := strconv.Atoi(y)
	if mn < 1 || mn > 12 || dn < 1 || len(y) != 4 ||
		time.Date(yn, time.Month(mn), dn, 0, 0, 0, 0, time.UTC).Day() != dn {
		o.add("error", comp, "Ngày tháng phải là ngày hợp lệ", l.raw, "Sửa lại ngày tháng.",
			"ngày "+d+" tháng "+mo+" năm "+y+" không hợp lệ")
		return
	}
	if dn < 10 && len(d) < 2 {
		o.add("error", comp, "Ngày dưới 10 ghi thêm số 0 phía trước", l.raw,
			"Sửa thành \"ngày 0"+d+"\".", "\"ngày "+d+"\" thiếu số 0")
	}
	if mn <= 2 && len(mo) < 2 {
		o.add("error", comp, "Tháng 1, 2 ghi thêm số 0 phía trước", l.raw,
			"Sửa thành \"tháng 0"+mo+"\".", "\"tháng "+mo+"\" thiếu số 0")
	}
	if mn >= 3 && strings.HasPrefix(mo, "0") {
		o.add("warn", comp, "Tháng 3 đến 12 thường viết không có số 0 phía trước", l.raw,
			"Nên ghi \"tháng "+strconv.Itoa(mn)+"\".", "\"tháng "+mo+"\" nên ghi \"tháng "+strconv.Itoa(mn)+"\"")
	}
}

// ---------------------------------------------------------------------------
// item 6

func (jd *judgeData) anchorLines(ids ...string) []AnchorLine {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var out []AnchorLine
	for _, a := range jd.r.Anchors {
		ok := want[a.Kind]
		for _, id := range a.IDs {
			ok = ok || want[id]
		}
		if ok {
			out = append(out, a)
		}
	}
	return out
}

func anchorText(a AnchorLine) (string, bool) {
	if a.Full != "" {
		return a.Full, false
	}
	return a.Text, strings.HasSuffix(a.Text, "…")
}

// absentStatus applies a requirement's when_absent.
func absentStatus(req Requirement, def string, o *ruleOut, comp, reason string) {
	st := req.WhenAbsent
	if st == "" {
		st = def
	}
	switch st {
	case "pass", "skip":
	case JudgeUnknown:
		o.unknown = reason
	case JudgeUndetermined:
		o.undetermined = reason
	case "warn":
		o.add("warn", comp, req.Desc, "", req.Fix, reason)
	default:
		o.add("error", comp, req.Desc, "", req.Fix, reason)
	}
}

func (jd *judgeData) typeRequirements(jr JudgmentRule) ruleOut {
	var o ruleOut
	if len(jr.Requirements) == 0 {
		o.note = "không có yêu cầu riêng cho loại này"
		return o
	}
	for _, req := range jr.Requirements {
		if req.When != "" && !jd.found(req.When) {
			continue
		}
		sev := req.Severity
		if sev == "" {
			sev = "error"
		}
		anchors := req.Anchors
		if req.Anchor != "" {
			anchors = append([]string{req.Anchor}, anchors...)
		}
		re := jre(req.Regex)
		var sub ruleOut
		switch req.Check {
		case "anchor_present":
			lines := jd.anchorLines(anchors...)
			if re != nil && len(lines) == 0 {
				absentStatus(req, sev, &sub, "noi_dung", "không có dòng mốc để xét: "+req.Desc)
				break
			}
			ok := false
			for _, a := range lines {
				t, _ := anchorText(a)
				if re == nil || re.MatchString(fold(t)) {
					ok = true
				}
			}
			if !ok {
				sub.add(sev, "noi_dung", req.Desc, "", req.Fix, "không thấy: "+lowerFirst(req.Desc))
			}
		case "anchor_regex":
			if len(anchors) == 0 || re == nil {
				sub.undetermined = "quy tắc anchor_regex thiếu anchor/regex"
				break
			}
			main := jd.anchorLines(anchors[0])
			if len(main) == 0 {
				absentStatus(req, JudgeUnknown, &sub, "noi_dung", "không có dòng mốc để xét: "+lowerFirst(req.Desc))
				break
			}
			n := req.Last
			if n <= 0 {
				n = 1
			}
			if len(main) > n {
				main = main[len(main)-n:]
			}
			check := append(append([]AnchorLine(nil), main...), jd.anchorLines(req.Also...)...)
			ok, cut := false, false
			for _, a := range check {
				t, truncated := anchorText(a)
				if re.MatchString(fold(t)) {
					ok = true
				}
				cut = cut || truncated
			}
			switch {
			case ok:
			case cut:
				sub.undetermined = "dòng mốc bị lược, không thấy hết: " + lowerFirst(req.Desc)
			default:
				last := main[len(main)-1]
				t, _ := anchorText(last)
				sub.add(sev, "noi_dung", req.Desc, t, req.Fix, "không thấy: "+lowerFirst(req.Desc))
			}
		case "line_ends":
			lines := jd.lines(req.Component)
			if len(lines) == 0 {
				absentStatus(req, "pass", &sub, req.Component, "không có "+ComponentName(req.Component))
				break
			}
			last := jre(req.LastRegex)
			var bad []string
			for i, l := range lines {
				want := re
				if i == len(lines)-1 {
					want = last
				}
				if want != nil && !want.MatchString(l.f) {
					bad = append(bad, l.raw)
				}
			}
			if len(bad) > 0 {
				sub.add(sev, req.Component, req.Desc, strings.Join(bad, " | "), req.Fix, quote(bad[0])+": "+lowerFirst(req.Desc))
			}
		case "component_present":
			if !jd.found(req.Component) {
				sub.add(sev, req.Component, req.Desc, "", req.Fix, "thiếu "+ComponentName(req.Component))
			}
		case "component_absent":
			if jd.found(req.Component) {
				lines := jd.lines(req.Component)
				line := ""
				if len(lines) > 0 {
					line = lines[0].raw
				}
				sub.add(sev, req.Component, req.Desc, line, req.Fix, "có "+ComponentName(req.Component)+" ("+quote(line)+")")
			}
		case "component_regex":
			lines := jd.lines(req.Component)
			if req.Skip > 0 {
				if req.Skip >= len(lines) {
					lines = nil
				} else {
					lines = lines[req.Skip:]
				}
			}
			if len(lines) == 0 || re == nil {
				absentStatus(req, "pass", &sub, req.Component, "không có "+ComponentName(req.Component))
				break
			}
			switch req.Mode {
			case "none":
				for _, l := range lines {
					if re.MatchString(l.f) {
						sub.add(sev, req.Component, req.Desc, l.raw, req.Fix, quote(l.raw)+": "+lowerFirst(req.Desc))
						break
					}
				}
			case "any":
				ok := false
				for _, l := range lines {
					ok = ok || re.MatchString(l.f)
				}
				if !ok {
					sub.add(sev, req.Component, req.Desc, lines[0].raw, req.Fix, "không thấy: "+lowerFirst(req.Desc))
				}
			default: // first
				l := lines[0]
				switch {
				case re.MatchString(l.f):
				case req.WarnRegex != "" && jre(req.WarnRegex) != nil && jre(req.WarnRegex).MatchString(l.f):
					sub.add("warn", req.Component, req.Desc, l.raw, req.Fix, quote(l.raw)+": "+lowerFirst(req.Desc))
				default:
					sub.add(sev, req.Component, req.Desc, l.raw, req.Fix, quote(l.raw)+": "+lowerFirst(req.Desc))
				}
			}
		default:
			sub.undetermined = fmt.Sprintf("yêu cầu %q chưa được hỗ trợ", req.Check)
		}
		o.findings = append(o.findings, sub.findings...)
		if sub.undetermined != "" {
			o.undetermined = joinReason(o.undetermined, sub.undetermined)
		}
		if sub.unknown != "" {
			o.unknown = joinReason(o.unknown, sub.unknown)
		}
	}
	return o
}

func joinReason(a, b string) string {
	if a == "" || strings.Contains(a, b) {
		if a == "" {
			return b
		}
		return a
	}
	return a + "; " + b
}

// sentenceCase: "GIÁM ĐỐC" → "Giám đốc".
func sentenceCase(s string) string {
	r := []rune(strings.ToLower(s))
	if len(r) > 0 {
		r[0] = unicode.ToUpper(r[0])
	}
	return string(r)
}

func lowerFirst(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	if len(r) > 1 && unicode.IsUpper(r[1]) {
		return s // an acronym
	}
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

// cutPrefixFold cuts prefix from s ignoring case.
func cutPrefixFold(s, prefix string) (string, bool) {
	if len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix) {
		return s[len(prefix):], true
	}
	return s, false
}
