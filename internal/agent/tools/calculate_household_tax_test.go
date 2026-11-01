package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestCalculateHouseholdTaxTool(t *testing.T) {
	tool := NewCalculateHouseholdTaxTool().WithFormulaBlock("Công thức áp dụng:\n- x")
	res, err := tool.Execute(context.Background(), json.RawMessage(
		`{"doanh_thu": 2400000000, "nguong_doanh_thu": "1000000000", "ty_le_gtgt": 1, "ty_le_tncn": 0.5, "tran_doanh_thu_theo_doanh_thu": 3000000000, "ty_le_giam_tncn": 30, "tran_doanh_thu_giam_tncn": 3000000000, "doanh_thu_ky": 600000000}`))
	if err != nil || !res.Success {
		t.Fatalf("%v %+v", err, res)
	}
	if res.Data["thue_gtgt"] != 24_000_000.0 || res.Data["thue_tncn_doanh_thu_net"] != 4_900_000.0 {
		t.Fatalf("data: %+v", res.Data)
	}
	// every amount states its period
	if res.Data["ky"] != "năm" || res.Data["thue_gtgt_ky"] != 6_000_000.0 || res.Data["giam_thue_tncn_ap_dung"] != true {
		t.Fatalf("period data: %+v", res.Data)
	}
	if s, _ := res.Data["thue_tncn_ky"].(string); !strings.Contains(s, "không tính theo kỳ") {
		t.Fatalf("thue_tncn_ky: %v", res.Data["thue_tncn_ky"])
	}
	block, _ := res.Data[types.ToolDataAnswerBlock].(string)
	if !strings.Contains(block, "28.900.000 đ") || !strings.HasSuffix(block, "Công thức áp dụng:\n- x") {
		t.Fatalf("block: %s", block)
	}
	if _, ok := res.Data["loi_nhuan_hoa_von"]; ok {
		t.Fatal("break-even without a profit rate")
	}
}

func TestCalculateHouseholdTaxToolRefusesMissing(t *testing.T) {
	tool := NewCalculateHouseholdTaxTool()
	res, err := tool.Execute(context.Background(), json.RawMessage(`{"doanh_thu": 2400000000, "ty_le_gtgt": 1, "ty_le_tncn": "0,5"}`))
	if err == nil || res.Success || !strings.Contains(res.Error, "nguong_doanh_thu") {
		t.Fatalf("%v %+v", err, res)
	}
	res, err = tool.Execute(context.Background(), json.RawMessage(`{"doanh_thu": 2400000000, "nguong_doanh_thu": 1000000000, "ty_le_gtgt": 1, "ty_le_tncn": "0,5", "tran_doanh_thu_theo_doanh_thu": 3000000000}`))
	if err == nil || !strings.Contains(res.Error, "ty_le_tncn") {
		t.Fatalf("%v %+v", err, res)
	}
}
