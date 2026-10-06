package docformat

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	vl "github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// LLM-assisted component labeling. The positional heuristic breaks on
// unusual layouts, and a mislabeled paragraph makes every size/font check
// of its component wrong. An LLM assigns each text unit to an NĐ30
// component; measurement and rule evaluation stay deterministic.
//
// A unit is one paragraph, or one side of a tab-split paragraph (id "12L" /
// "12R"). Units the reply leaves out keep the heuristic label, so a partial
// answer degrades gracefully.

// ComponentDef describes one NĐ30 component for the model.
type ComponentDef struct {
	Key, Desc string
}

// Components are the NĐ30/2020 Phụ lục I parts, in reading order. The
// descriptions are written for the model: what the part is and where it
// normally sits.
var Components = []ComponentDef{
	{"quoc_hieu", "Quốc hiệu: 'CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM', đầu trang, cột phải."},
	{"tieu_ngu", "Tiêu ngữ: 'Độc lập - Tự do - Hạnh phúc', ngay dưới quốc hiệu."},
	{"co_quan_chu_quan", "Tên cơ quan chủ quản trực tiếp — một cơ quan KHÁC, cấp trên của cơ quan ban hành — cột trái đầu trang, phía trên tên cơ quan ban hành. VD: 'UBND TỈNH THỪA THIÊN HUẾ' phía trên 'SỞ Y TẾ'. Văn bản của UBND, HĐND, Bộ, cơ quan trung ương thường KHÔNG có cơ quan chủ quản."},
	{"co_quan_ban_hanh", "Tên cơ quan, tổ chức ban hành văn bản (in đậm), cột trái đầu trang. Tên dài xuống dòng thì MỌI dòng đều là co_quan_ban_hanh: 'ỦY BAN NHÂN DÂN' + 'THÀNH PHỐ HUẾ' là MỘT tên (UBND thành phố Huế), không phải chủ quản + ban hành; tương tự 'HỘI ĐỒNG NHÂN DÂN' + 'TỈNH ...'."},
	{"so_ky_hieu", "Số, ký hiệu văn bản: 'Số: 12/QĐ-UBND', cột trái dưới tên cơ quan."},
	{"dia_danh_ngay_thang", "Địa danh và thời gian ban hành: 'Huế, ngày 05 tháng 10 năm 2026', cột phải dưới tiêu ngữ."},
	{"do_mat", "Dấu chỉ độ mật (MẬT, TỐI MẬT, TUYỆT MẬT)."},
	{"do_khan", "Dấu chỉ mức độ khẩn (KHẨN, THƯỢNG KHẨN, HỎA TỐC)."},
	{"trich_yeu", "Tên loại văn bản và trích yếu nội dung: dòng tên loại ('QUYẾT ĐỊNH', 'BÁO CÁO', 'KẾ HOẠCH'...) và (các) dòng trích yếu ngay dưới; với công văn là dòng 'V/v ...' dưới số ký hiệu (kể cả dòng xuống hàng)."},
	{"tham_quyen_ban_hanh", "Thẩm quyền ban hành của QĐ/NQ, căn giữa sau trích yếu, VD 'CHỦ TỊCH ỦY BAN NHÂN DÂN TỈNH ...', 'GIÁM ĐỐC SỞ Y TẾ'."},
	{"can_cu", "Các dòng 'Căn cứ ...' / 'Xét đề nghị ...' / 'Theo đề nghị ...' trước phần nội dung chính."},
	{"kinh_gui", "Dòng 'Kính gửi: ...' và danh sách nơi gửi đi kèm (công văn, tờ trình)."},
	{"noi_dung", "Nội dung văn bản: toàn bộ thân văn bản, kể cả đề mục (I., 1., Điều 1.), dòng 'QUYẾT ĐỊNH:' giữa văn bản, bảng số liệu, câu kết 'Trên đây là ...'."},
	{"chuc_danh", "Quyền hạn, chức vụ người ký trong khối ký (thường ở cột phải cuối văn bản, có khi chỉ căn giữa/thụt lề): 'TM. ỦY BAN NHÂN DÂN', 'KT. GIÁM ĐỐC', 'PHÓ GIÁM ĐỐC', 'CHỦ TỊCH'. Biên bản, văn bản liên tịch có NHIỀU người ký cạnh nhau (VD trái 'THƯ KÝ', phải 'CHỦ TRÌ') — mọi khối ký đều là chuc_danh/nguoi_ky, không phải noi_dung."},
	{"nguoi_ky", "Họ và tên người ký, dòng cuối của mỗi khối ký: 'Nguyễn Văn A'. Không gồm '(Đã ký)', '(Ký, đóng dấu)'."},
	{"noi_nhan", "Nơi nhận: dòng 'Nơi nhận:' và các dòng '- ...;' bên dưới, cột trái cuối văn bản."},
	{"phu_luc", "Phụ lục / văn bản kèm theo sau phần nơi nhận."},
	{"khac", "Không thuộc thành phần nào ở trên: dòng kẻ, ghi chú '(Đã ký)', dấu, chú thích, header/footer lạc vào."},
}

var componentKeys = func() map[string]bool {
	m := map[string]bool{}
	for _, c := range Components {
		m[c.Key] = true
	}
	return m
}()

// DocTypeSlugs are the types the model may answer: the registry's 29 NĐ30
// types and the QPPL types, then "unknown".
func DocTypeSlugs() []string {
	out := make([]string, 0, len(vl.DocTypes)+1)
	for _, d := range vl.DocTypes {
		out = append(out, d.Slug)
	}
	return append(out, "unknown")
}

const (
	// units kept verbatim at each end; the middle of a long body is elided
	// (labelled noi_dung) unless the heuristic flags something there
	keepHead = 45
	keepTail = 45
	textMax  = 160
)

// Unit is one labelable text fragment as shown to the model.
type Unit struct {
	ID    string   `json:"id"`
	Para  int      `json:"para"`
	Text  string   `json:"text"`
	Zone  string   `json:"zone"`
	Align string   `json:"align"`
	Size  *float64 `json:"size"`
	B     bool     `json:"b"`
	I     bool     `json:"i"`
	Upper bool     `json:"upper"`
	Tbl   string   `json:"tbl,omitempty"`
	Hint  string   `json:"hint,omitempty"`
}

func isUpperLetters(text string) bool {
	letters, lower := 0, false
	for _, r := range text {
		if unicode.IsLetter(r) {
			letters++
			if unicode.IsLower(r) {
				lower = true
			}
		}
	}
	return letters >= 2 && !lower
}

func intOrNone(p *int) string {
	if p == nil {
		return "None"
	}
	return strconv.Itoa(*p)
}

// BuildUnits lists the units with the layout cues the model needs; hints
// come from the heuristic segmentation when given.
func BuildUnits(l *Layout, heuristic *Segmentation) []Unit {
	var units []Unit
	for _, el := range elements(l) {
		p := el.para
		id := strconv.Itoa(p.Index)
		size, bold, italic := p.SizePt, p.Bold, p.Italic
		upper := p.TextIsUpper
		if p.Zone == ZoneSplit {
			if el.zone == ZoneLeft {
				id += "L"
			} else {
				id += "R"
			}
			side := p.LeftProps
			if el.zone != ZoneLeft {
				side = p.RightProps
			}
			if side.Present {
				size, bold, italic = side.SizePt, side.Bold, side.Italic
			}
			upper = isUpperLetters(el.text)
		}
		text := el.text
		if runeLen(text) > textMax {
			text = truncateRunes(text, textMax) + "…"
		}
		u := Unit{ID: id, Para: p.Index, Text: text, Zone: el.zone, Align: p.Alignment,
			Size: size, B: isTrue(bold), I: isTrue(italic), Upper: upper}
		if p.TableIndex != nil {
			u.Tbl = fmt.Sprintf("%d:%s/%s:%s", *p.TableIndex, p.TableLayout, intOrNone(p.Row), intOrNone(p.Col))
		}
		if heuristic != nil {
			u.Hint = heuristicLabel(heuristic, p.Index, el.zone)
		}
		units = append(units, u)
	}
	return units
}

// heuristicLabel is the label a segmentation gave a unit (zone-aware for
// split paragraphs).
func heuristicLabel(seg *Segmentation, para int, zone string) string {
	found := ""
	for _, key := range seg.order {
		if key == "signature" {
			continue
		}
		c := seg.Components[key]
		for i, idx := range c.Paras {
			if idx != para {
				continue
			}
			z := ""
			if i < len(c.Zones) {
				z = c.Zones[i]
			}
			if z == zone || z == "" || zone == ZoneFull {
				return key
			}
			if found == "" {
				found = key
			}
		}
	}
	return found
}

// LabelOf returns the component a segmentation assigned to a unit ("khac"
// when none).
func LabelOf(seg *Segmentation, u Unit) string {
	if l := heuristicLabel(seg, u.Para, u.Zone); l != "" {
		return l
	}
	return "khac"
}

func elide(units []Unit) ([]Unit, map[string]bool) {
	n := len(units)
	elided := map[string]bool{}
	if n <= keepHead+keepTail+10 {
		return units, elided
	}
	var sent []Unit
	for i, u := range units {
		if i < keepHead || i >= n-keepTail || (u.Hint != "" && u.Hint != "noi_dung") {
			sent = append(sent, u)
		} else {
			elided[u.ID] = true
		}
	}
	return sent, elided
}

// SystemPrompt instructs the model how to label.
const SystemPrompt = `Bạn là chuyên viên văn thư, nắm vững thể thức văn bản hành chính theo Nghị định 30/2020/NĐ-CP (Phụ lục I). Nhiệm vụ: gán MỖI đơn vị văn bản (unit) vào đúng một thành phần thể thức. Việc chấm cỡ chữ/font phụ thuộc hoàn toàn vào nhãn bạn gán, nên hãy gán theo VAI TRÒ của dòng trong văn bản, không theo định dạng của nó (văn bản cần kiểm tra có thể định dạng sai).

Gợi ý vị trí: đầu văn bản là khối 2 cột (trái: cơ quan, số ký hiệu, có thể cả 'V/v'; phải: quốc hiệu, tiêu ngữ, địa danh-ngày tháng). Sau đó là tên loại + trích yếu căn giữa, nội dung, rồi cuối văn bản là khối 2 cột (phải: chức danh + họ tên người ký; trái: nơi nhận).

Mỗi unit có: id, text, zone (left/right/full = cột trái/phải/toàn trang), align, size (pt), b (đậm), i (nghiêng), upper (in hoa), tbl ('bảng:loại/hàng:cột', loại 'columns' = bảng bố cục không viền, 'data' = bảng số liệu), hint (nhãn đoán bằng heuristic — có thể SAI).

document_type là TÊN LOẠI văn bản in ở dòng tên loại (QUYẾT ĐỊNH, BÁO CÁO, ...); văn bản không có dòng tên loại mà có 'V/v ...' dưới số ký hiệu là cong_van — đừng suy loại từ nội dung trích yếu ('V/v thông báo ...' vẫn là cong_van).

Trả về DUY NHẤT một JSON object:
{"document_type": "<một loại>", "labels": {"<id>": "<thành phần>", ...}, "notes": "<ngắn, tuỳ chọn>"}
- labels phải có mọi id được gửi; chỉ dùng tên thành phần trong danh sách.
- Các unit ở giữa thân văn bản đã được lược bớt sẽ tự gán noi_dung.`

// Task is everything a model needs to label one document.
type Task struct {
	Units       []Unit
	All         []Unit
	Elided      map[string]bool
	ElidedCount int
}

// PrepareTask builds the labeling task from the layout and its heuristic
// segmentation.
func PrepareTask(l *Layout, heuristic *Segmentation) *Task {
	all := BuildUnits(l, heuristic)
	sent, elided := elide(all)
	return &Task{Units: sent, All: all, Elided: elided, ElidedCount: len(elided)}
}

// UserMessage renders the task for the model.
func (t *Task) UserMessage() string {
	var b strings.Builder
	b.WriteString("Thành phần:\n")
	for i, c := range Components {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "- %s: %s", c.Key, c.Desc)
	}
	fmt.Fprintf(&b, "\n\nLoại văn bản: %s\n\nUnits (%d, đã lược %d unit giữa thân văn bản):\n",
		strings.Join(DocTypeSlugs(), ", "), len(t.Units), t.ElidedCount)
	for i, u := range t.Units {
		if i > 0 {
			b.WriteByte('\n')
		}
		line, _ := json.Marshal(u)
		b.Write(line)
	}
	return b.String()
}

// Reply is the model's (or a caller's) labeling answer.
type Reply struct {
	DocumentType string            `json:"document_type"`
	Labels       map[string]string `json:"labels"`
	Notes        string            `json:"notes,omitempty"`
}

var fenceRe = regexp.MustCompile("^```(?:json)?\\s*|\\s*```$")

// ParseReply extracts the JSON object from a model reply, tolerating code
// fences and surrounding prose. Labels may also come as a list of
// {"id", "component"|"label"}.
func ParseReply(text string) (*Reply, error) {
	t := strings.TrimSpace(text)
	t = fenceRe.ReplaceAllString(t, "")
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(t), &raw); err != nil {
		start, end := strings.Index(t, "{"), strings.LastIndex(t, "}")
		if start < 0 || end <= start {
			return nil, fmt.Errorf("no JSON object in model reply")
		}
		if err := json.Unmarshal([]byte(t[start:end+1]), &raw); err != nil {
			return nil, fmt.Errorf("model reply is not valid JSON: %w", err)
		}
	}
	r := &Reply{Labels: map[string]string{}}
	if v, ok := raw["document_type"]; ok {
		_ = json.Unmarshal(v, &r.DocumentType)
	}
	if v, ok := raw["notes"]; ok {
		_ = json.Unmarshal(v, &r.Notes)
	}
	if v, ok := raw["labels"]; ok {
		var m map[string]any
		if json.Unmarshal(v, &m) == nil {
			for k, x := range m {
				s, _ := x.(string)
				r.Labels[k] = strings.TrimSpace(s)
			}
		} else {
			var list []map[string]any
			if json.Unmarshal(v, &list) == nil {
				for _, x := range list {
					id := fmt.Sprint(x["id"])
					s, _ := x["component"].(string)
					if s == "" {
						s, _ = x["label"].(string)
					}
					r.Labels[id] = strings.TrimSpace(s)
				}
			}
		}
	}
	r.DocumentType = strings.TrimSpace(r.DocumentType)
	return r, nil
}

// LabelDiagnostics audits a labeling answer.
type LabelDiagnostics struct {
	Invalid       []map[string]string `json:"invalid"`
	Missing       []string            `json:"missing"`
	Disagreements []map[string]string `json:"disagreements"`
	UnknownIDs    []string            `json:"unknown_ids"`
	DocumentType  map[string]string   `json:"document_type,omitempty"`
}

func effectiveZone(l *Layout, u Unit) string {
	if u.Zone != ZoneFull {
		return u.Zone
	}
	p := l.Paragraphs[u.Para]
	if pRightish(p) {
		return ZoneRight
	}
	if p.Alignment == "left" {
		return ZoneLeft
	}
	return ZoneFull
}

func unitText(l *Layout, u Unit) string {
	p := l.Paragraphs[u.Para]
	switch {
	case strings.HasSuffix(u.ID, "L"):
		return p.LeftText
	case strings.HasSuffix(u.ID, "R"):
		return p.RightText
	}
	return strings.TrimSpace(p.Text)
}

// ApplyLabels builds a Segmentation from a labeling answer. Units without
// a (valid) label keep the heuristic one; elided units are nội dung.
func ApplyLabels(l *Layout, reply *Reply, task *Task) (*Segmentation, *LabelDiagnostics) {
	diag := &LabelDiagnostics{Invalid: []map[string]string{}, Missing: []string{},
		Disagreements: []map[string]string{}, UnknownIDs: []string{}}
	known := map[string]bool{}
	for _, u := range task.All {
		known[u.ID] = true
	}
	for id := range reply.Labels {
		if !known[id] {
			diag.UnknownIDs = append(diag.UnknownIDs, id)
		}
	}
	sort.Strings(diag.UnknownIDs)

	seg := newSegmentation(l)
	for pos, u := range task.All {
		lab, ok := reply.Labels[u.ID]
		switch {
		case !ok && task.Elided[u.ID]:
			lab = "noi_dung"
		case !ok:
			diag.Missing = append(diag.Missing, u.ID)
			lab = u.Hint
		case !componentKeys[lab]:
			diag.Invalid = append(diag.Invalid, map[string]string{"id": u.ID, "label": lab})
			lab = u.Hint
		}
		if lab == "" {
			lab = "noi_dung"
		}
		if u.Hint != "" && lab != u.Hint && !task.Elided[u.ID] {
			diag.Disagreements = append(diag.Disagreements, map[string]string{
				"id": u.ID, "text": truncateRunes(u.Text, 80), "llm": lab, "heuristic": u.Hint})
		}
		if lab == "khac" {
			continue
		}
		seg.add(lab, u.Para, unitText(l, u), effectiveZone(l, u), pos)
	}
	seg.addSignature()

	// Loại văn bản is structural once the trích yếu is labelled (type
	// heading, "V/v", ký hiệu in the số); the model's guess only fills in
	// when those are absent — models read the type off the subject.
	dt := reply.DocumentType
	structural := DetectType(seg.Comp("trich_yeu").Text, "", seg.Comp("so_ky_hieu").Text)
	switch {
	case structural != "unknown":
		seg.DetectedType = structural
	case validDocType(dt):
		seg.DetectedType = dt
	default:
		seg.DetectedType = "unknown"
	}
	if dt != "" && dt != seg.DetectedType {
		diag.DocumentType = map[string]string{"llm": dt, "used": seg.DetectedType}
	}
	return seg, diag
}

func validDocType(s string) bool {
	if s == "unknown" {
		return true
	}
	return vl.DocTypeBySlug(s) != nil
}
