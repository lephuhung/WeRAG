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
		`{"doanh_thu": 2400000000, "nguong_doanh_thu": "1000000000", "ty_le_gtgt": 1, "ty_le_tncn": 0.5, "ty_le_giam_tncn": 30}`))
	if err != nil || !res.Success {
		t.Fatalf("%v %+v", err, res)
	}
	if res.Data["thue_gtgt"] != 24_000_000.0 || res.Data["thue_tncn_doanh_thu_net"] != 4_900_000.0 {
		t.Fatalf("data: %+v", res.Data)
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
	res, err = tool.Execute(context.Background(), json.RawMessage(`{"doanh_thu": 2400000000, "nguong_doanh_thu": 1000000000, "ty_le_gtgt": 1, "ty_le_tncn": "0,5"}`))
	if err == nil || !strings.Contains(res.Error, "ty_le_tncn") {
		t.Fatalf("%v %+v", err, res)
	}
}
