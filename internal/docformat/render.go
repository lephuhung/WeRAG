package docformat

import (
	"encoding/json"
	"fmt"
	"strings"
)

// componentNames are the Vietnamese names of the components, for reports.
var componentNames = map[string]string{
	"quoc_hieu":           "Quốc hiệu",
	"tieu_ngu":            "Tiêu ngữ",
	"co_quan_chu_quan":    "Cơ quan chủ quản",
	"co_quan_ban_hanh":    "Cơ quan ban hành",
	"so_ky_hieu":          "Số, ký hiệu",
	"dia_danh_ngay_thang": "Địa danh, ngày tháng",
	"do_mat":              "Độ mật",
	"do_khan":             "Độ khẩn",
	"trich_yeu":           "Tên loại, trích yếu",
	"tham_quyen_ban_hanh": "Thẩm quyền ban hành",
	"can_cu":              "Căn cứ",
	"kinh_gui":            "Kính gửi",
	"noi_dung":            "Nội dung",
	"chuc_danh":           "Chức vụ người ký",
	"nguoi_ky":            "Họ tên người ký",
	"signature":           "Khối chữ ký",
	"noi_nhan":            "Nơi nhận",
	"phu_luc":             "Phụ lục",
}

// ComponentName returns the Vietnamese name of a component key.
func ComponentName(key string) string {
	if n, ok := componentNames[key]; ok {
		return n
	}
	return key
}

func compactJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// RenderText renders a report as compact text for an agent to explain to
// the user: findings first (fail, then warn), each with the rule, the
// measured value and the offending lines; then what could not be measured
// and the components found.
func RenderText(r *Report) string {
	var b strings.Builder
	if !r.OK {
		fmt.Fprintf(&b, "Không kiểm tra được %s: %s\n", r.Source, r.Error)
		return b.String()
	}
	dt := r.DocumentType
	fmt.Fprintf(&b, "Văn bản: %s\nLoại văn bản: %s — bộ luật: %s\n", r.Source, dt.Detected, dt.RuleSet)
	if s := r.Segmentation; s != nil {
		fmt.Fprintf(&b, "Gán thành phần: %s", s.Method)
		if s.Model != "" {
			fmt.Fprintf(&b, " (%s)", s.Model)
		}
		if s.Error != "" {
			fmt.Fprintf(&b, " — %s", s.Error)
		}
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "Kết quả: %d đạt, %d sai, %d cảnh báo, %d không đo được\n",
		r.Summary.Pass, r.Summary.Fail, r.Summary.Warn, r.Summary.Skip)

	for _, status := range []string{StatusFail, StatusWarn} {
		title := map[string]string{StatusFail: "\n## Lỗi (sai thể thức)\n", StatusWarn: "\n## Cảnh báo (nên sửa)\n"}[status]
		wrote := false
		for _, c := range r.Checks {
			if c.Status != status {
				continue
			}
			if !wrote {
				b.WriteString(title)
				wrote = true
			}
			fmt.Fprintf(&b, "- [%s] %s", c.ID, c.Desc)
			if c.Actual != nil {
				fmt.Fprintf(&b, " — thực tế: %s", compactJSON(c.Actual))
			}
			b.WriteByte('\n')
			for _, ev := range c.Evidence {
				if txt, ok := ev["text"]; ok {
					fmt.Fprintf(&b, "    · đoạn %v %q: %s\n", ev["para"], txt, compactJSON(ev["actual"]))
				} else {
					fmt.Fprintf(&b, "    · %s\n", compactJSON(ev))
				}
			}
		}
	}
	var skipped []string
	for _, c := range r.Checks {
		if c.Status == StatusSkip && c.Severity == "error" && c.Note != "" &&
			!strings.HasPrefix(c.Note, "thành phần không bắt buộc") {
			skipped = append(skipped, c.ID)
		}
	}
	if len(skipped) > 0 {
		fmt.Fprintf(&b, "\nKhông đo được (thiếu thành phần): %s\n", strings.Join(skipped, ", "))
	}
	b.WriteString("\n## Thành phần bóc tách được\n")
	for _, c := range Components {
		comp, ok := r.Components[c.Key]
		if !ok || !comp.Found || c.Key == "noi_dung" {
			continue
		}
		fmt.Fprintf(&b, "- %s: %s\n", ComponentName(c.Key), truncateRunes(strings.ReplaceAll(comp.Text, "\n", " / "), 120))
	}
	b.WriteString("\nLưu ý: kiểm tra tự động theo NĐ30/2020 Phụ lục I trên file .docx; con dấu, chữ ký, đánh số trang cần rà soát thủ công.\n")
	return b.String()
}
