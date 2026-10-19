package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/agent/domaintools/opcvn"
	"github.com/Tencent/WeKnora/internal/types"
)

// ToolCalculateHouseholdTax computes VAT and PIT of a household business
// (hộ kinh doanh, cá nhân kinh doanh). Fixed-formula tool of the
// opc-vn-business-law domain pack, like compare_tax_methods: selectable per
// agent, never in the default allowlist.
const ToolCalculateHouseholdTax = opcvn.ToolHouseholdTax

var calculateHouseholdTaxTool = BaseTool{
	name:        ToolCalculateHouseholdTax,
	description: `Tính thuế GTGT và thuế TNCN của hộ kinh doanh, cá nhân kinh doanh: GTGT = doanh thu năm × tỷ lệ % GTGT; TNCN theo % doanh thu = (doanh thu năm − ngưỡng) × tỷ lệ % TNCN; tùy chọn TNCN trên lợi nhuận, lợi nhuận hòa vốn, mức giảm thuế TNCN, GTGT của một kỳ. Trả về bảng kết quả và khối "Giả định" (hệ thống tự chèn vào câu trả lời). Doanh thu năm (và chi phí, doanh thu kỳ) lấy từ người dùng; ngưỡng doanh thu, các tỷ lệ, thuế suất, mức giảm lấy từ văn bản thuế hiện hành tìm được trong kho tri thức (văn bản sửa đổi có thể đã thay ngưỡng) — thiếu thì hỏi hoặc tìm trước, không tự đoán. Dùng thay cho việc tự nhân chia.`,
	schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "doanh_thu": {
      "type": "number",
      "description": "Doanh thu CẢ NĂM, đơn vị đồng, lớn hơn 0 (ví dụ 2,4 tỷ = 2400000000)"
    },
    "nguong_doanh_thu": {
      "type": "number",
      "description": "Ngưỡng doanh thu năm không phải nộp GTGT, TNCN theo văn bản hiện hành, đơn vị đồng (ví dụ 1 tỷ = 1000000000)"
    },
    "ty_le_gtgt": {
      "type": "number",
      "description": "Tỷ lệ % GTGT trên doanh thu theo ngành nghề, ghi theo PHẦN TRĂM: 1 nghĩa là 1%"
    },
    "ty_le_tncn": {
      "type": "number",
      "description": "Tỷ lệ % TNCN trên doanh thu theo ngành nghề, ghi theo PHẦN TRĂM: 0.5 nghĩa là 0,5%"
    },
    "chi_phi": {
      "type": "number",
      "description": "Tùy chọn: chi phí được trừ cả năm, đơn vị đồng, để so sánh với TNCN trên lợi nhuận (cần thue_suat_tncn)"
    },
    "thue_suat_tncn": {
      "type": "number",
      "description": "Tùy chọn: thuế suất TNCN trên thu nhập tính thuế theo mức doanh thu năm, dạng thập phân (0.15 cho 15%)"
    },
    "ty_le_giam_tncn": {
      "type": "number",
      "description": "Tùy chọn: mức giảm số thuế TNCN phải nộp đang áp dụng, ghi theo PHẦN TRĂM (30 cho 30%)"
    },
    "doanh_thu_ky": {
      "type": "number",
      "description": "Tùy chọn: doanh thu của một quý hoặc tháng, đơn vị đồng, để tính GTGT của kỳ đó"
    }
  },
  "required": ["doanh_thu", "nguong_doanh_thu", "ty_le_gtgt", "ty_le_tncn"]
}`),
}

// CalculateHouseholdTaxTool wraps opcvn.CalculateHouseholdTax for the agent.
type CalculateHouseholdTaxTool struct {
	BaseTool
	// formulaBlock is the "Công thức áp dụng" text of formulas.yaml.
	formulaBlock string
}

// NewCalculateHouseholdTaxTool creates the tool; it needs no service.
func NewCalculateHouseholdTaxTool() *CalculateHouseholdTaxTool {
	return &CalculateHouseholdTaxTool{BaseTool: calculateHouseholdTaxTool}
}

// WithFormulaBlock sets the formulas text appended to every result.
func (t *CalculateHouseholdTaxTool) WithFormulaBlock(block string) *CalculateHouseholdTaxTool {
	t.formulaBlock = strings.TrimSpace(block)
	return t
}

const householdTaxAnswerNote = "Hệ thống tự chèn bảng kết quả này, khối Giả định và Công thức áp dụng vào cuối câu trả lời. " +
	"Trong câu trả lời nêu kết luận theo dòng \"Đọc kết quả\" và căn cứ đã tra; không tự tính lại các số.\n"

var (
	householdTaxRequired = []string{"doanh_thu", "nguong_doanh_thu", "ty_le_gtgt", "ty_le_tncn"}
	householdTaxOptional = []string{"chi_phi", "thue_suat_tncn", "ty_le_giam_tncn", "doanh_thu_ky"}
)

func (t *CalculateHouseholdTaxTool) fail(msg string) (*types.ToolResult, error) {
	return &types.ToolResult{Success: false, Error: msg}, fmt.Errorf("%s", msg)
}

// Execute checks the figures, then calculates. A missing figure is never
// filled in.
func (t *CalculateHouseholdTaxTool) Execute(_ context.Context, args json.RawMessage) (*types.ToolResult, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(args, &raw); err != nil {
		return t.fail(fmt.Sprintf("Không đọc được tham số: %v", err))
	}
	values := map[string]float64{}
	var missing, invalid []string
	for _, name := range append(append([]string{}, householdTaxRequired...), householdTaxOptional...) {
		value, present, ok := parseTaxNumber(raw[name])
		switch {
		case !present:
			if contains(householdTaxRequired, name) {
				missing = append(missing, name)
			}
		case !ok:
			invalid = append(invalid, name)
		default:
			values[name] = value
		}
	}
	if len(missing) > 0 {
		return t.fail("Thiếu tham số bắt buộc: " + strings.Join(missing, ", ") +
			". Không tự giả định: hỏi người dùng (doanh thu năm, ngành nghề) hoặc tìm ngưỡng và tỷ lệ trong văn bản thuế của kho tri thức.")
	}
	if len(invalid) > 0 {
		return t.fail("Tham số phải là số (đơn vị đồng cho số tiền): " + strings.Join(invalid, ", "))
	}
	opt := func(name string) *float64 {
		if v, ok := values[name]; ok {
			return &v
		}
		return nil
	}
	result, err := opcvn.CalculateHouseholdTax(opcvn.HouseholdInput{
		Revenue:       values["doanh_thu"],
		Threshold:     values["nguong_doanh_thu"],
		VATRatio:      values["ty_le_gtgt"],
		PITRatio:      values["ty_le_tncn"],
		Cost:          opt("chi_phi"),
		ProfitRate:    opt("thue_suat_tncn"),
		Reduction:     opt("ty_le_giam_tncn"),
		PeriodRevenue: opt("doanh_thu_ky"),
	})
	if err != nil {
		return t.fail("Lỗi: " + err.Error())
	}
	block := strings.TrimSpace(result.Render())
	if t.formulaBlock != "" {
		block += "\n\n" + t.formulaBlock
	}
	data := map[string]interface{}{
		"doanh_thu":               result.In.Revenue,
		"nguong_doanh_thu":        result.In.Threshold,
		"phai_nop":                result.Liable,
		"thue_gtgt":               result.VAT,
		"thue_tncn_doanh_thu":     result.PITRevenue,
		"thue_tncn_doanh_thu_net": result.PITRevenueNet,
		"gia_dinh":                result.Assumptions,
		types.ToolDataAnswerBlock: block,
	}
	if result.HasProfit {
		data["thue_tncn_loi_nhuan"] = result.PITProfit
		data["thue_tncn_loi_nhuan_net"] = result.PITProfitNet
	}
	if result.HasBreakEven {
		data["loi_nhuan_hoa_von"] = result.BreakEvenProfit
	}
	return &types.ToolResult{Success: true, Output: block + "\n\n" + householdTaxAnswerNote, Data: data}, nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
