package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/Tencent/WeKnora/internal/agent/domaintools/opcvn"
	"github.com/Tencent/WeKnora/internal/types"
)

// ToolCompareTaxMethods compares corporate income tax on a ratio of revenue
// with tax declared on profit. It is the fixed-formula tool of the
// opc-vn-business-law domain pack (docs/domain-pack-standard.md §4.1):
// selectable per agent, never in the default allowlist.
const ToolCompareTaxMethods = "compare_tax_methods"

var compareTaxMethodsTool = BaseTool{
	name:        ToolCompareTaxMethods,
	description: `So sánh thuế thu nhập doanh nghiệp (TNDN) theo hai cách: nộp theo tỷ lệ % trên doanh thu, và kê khai trên lợi nhuận × thuế suất. Trả về thuế mỗi cách, biên lợi nhuận, biên lợi nhuận hòa vốn, chênh lệch, khuyến nghị sơ bộ và khối "Giả định" (đưa khối này vào câu trả lời). Cả bốn tham số đều bắt buộc và không có giá trị mặc định: doanh thu và lợi nhuận lấy từ người dùng; tỷ lệ % doanh thu và thuế suất lấy từ văn bản thuế tìm được trong kho tri thức hoặc do người dùng nêu — thiếu thì hỏi người dùng hoặc tìm trước, không tự đoán. Chỉ tính TNDN: chưa tính GTGT, lệ phí môn bài, ưu đãi, chi phí không được trừ.`,
	schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "doanh_thu": {
      "type": "number",
      "description": "Doanh thu năm, đơn vị đồng, lớn hơn 0 (ví dụ 8 tỷ = 8000000000)"
    },
    "loi_nhuan": {
      "type": "number",
      "description": "Lợi nhuận trước thuế, đơn vị đồng; 0 nếu lỗ; không lớn hơn doanh thu"
    },
    "ty_le_doanh_thu": {
      "type": "number",
      "description": "Tỷ lệ thuế TNDN tính trên doanh thu, ghi theo PHẦN TRĂM, lớn hơn 0 và nhỏ hơn 100: 1.5 nghĩa là 1,5%; 0.5 nghĩa là 0,5%"
    },
    "thue_suat": {
      "type": "number",
      "description": "Thuế suất TNDN dạng thập phân trong khoảng (0, 1], ví dụ 0.2 cho 20%"
    }
  },
  "required": ["doanh_thu", "loi_nhuan", "ty_le_doanh_thu", "thue_suat"]
}`),
}

// CompareTaxMethodsTool wraps opcvn.CompareTaxMethods for the agent.
type CompareTaxMethodsTool struct {
	BaseTool
}

// NewCompareTaxMethodsTool creates the tool; it needs no service.
func NewCompareTaxMethodsTool() *CompareTaxMethodsTool {
	return &CompareTaxMethodsTool{BaseTool: compareTaxMethodsTool}
}

// compareTaxArgs are the four parameters in schema order.
var compareTaxArgs = []string{"doanh_thu", "loi_nhuan", "ty_le_doanh_thu", "thue_suat"}

func (t *CompareTaxMethodsTool) fail(msg string) (*types.ToolResult, error) {
	return &types.ToolResult{Success: false, Error: msg}, fmt.Errorf("%s", msg)
}

// Execute checks that all four figures are present and numeric, then runs
// the comparison. A missing figure is never filled in.
func (t *CompareTaxMethodsTool) Execute(_ context.Context, args json.RawMessage) (*types.ToolResult, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(args, &raw); err != nil {
		return t.fail(fmt.Sprintf("Không đọc được tham số: %v", err))
	}
	values := map[string]float64{}
	var missing, invalid []string
	for _, name := range compareTaxArgs {
		value, present, ok := parseTaxNumber(raw[name])
		switch {
		case !present:
			missing = append(missing, name)
		case !ok:
			invalid = append(invalid, name)
		default:
			values[name] = value
		}
	}
	if len(missing) > 0 {
		return t.fail("Thiếu tham số bắt buộc: " + strings.Join(missing, ", ") +
			". Không tự giả định: hỏi người dùng (doanh thu, lợi nhuận) hoặc tìm tỷ lệ và thuế suất trong văn bản thuế của kho tri thức.")
	}
	if len(invalid) > 0 {
		return t.fail("Tham số phải là số (đơn vị đồng cho số tiền): " + strings.Join(invalid, ", "))
	}

	result, err := opcvn.CompareTaxMethods(opcvn.TaxInput{
		Revenue:      values["doanh_thu"],
		Profit:       values["loi_nhuan"],
		RevenueRatio: values["ty_le_doanh_thu"],
		TaxRate:      values["thue_suat"],
	})
	if err != nil {
		return t.fail("Lỗi: " + err.Error())
	}
	return &types.ToolResult{
		Success: true,
		Output:  result.Render(),
		Data: map[string]interface{}{
			"doanh_thu":           result.Revenue,
			"loi_nhuan":           result.Profit,
			"ty_le_doanh_thu":     result.RevenueRatio,
			"thue_suat":           result.TaxRate,
			"thue_theo_doanh_thu": result.TaxOnRevenue,
			"thue_theo_loi_nhuan": result.TaxOnProfit,
			"bien_loi_nhuan":      result.Margin,
			"bien_hoa_von":        result.BreakEven,
			"chenh_lech":          result.Difference,
			"khuyen_nghi":         result.Recommendation,
			"gia_dinh":            result.Assumptions,
		},
	}, nil
}

// parseTaxNumber reads a JSON number, or a string holding a plain number
// (models sometimes quote numbers). "8 tỷ" or "1.200.000" are refused rather
// than guessed: the dot could be a decimal point or a thousands separator.
func parseTaxNumber(raw json.RawMessage) (value float64, present, ok bool) {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return 0, false, false
	}
	if strings.HasPrefix(text, `"`) {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return 0, true, false
		}
		s = strings.TrimSpace(s)
		if s == "" {
			return 0, false, false
		}
		if strings.Count(s, ".") > 1 || strings.ContainsAny(s, ", ") {
			return 0, true, false
		}
		text = s
	}
	v, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0, true, false
	}
	return v, true, true
}
