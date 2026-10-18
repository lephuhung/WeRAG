package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func runCompareTax(t *testing.T, args string) (bool, string, map[string]interface{}) {
	t.Helper()
	res, err := NewCompareTaxMethodsTool().Execute(context.Background(), json.RawMessage(args))
	if res == nil {
		t.Fatalf("nil result (err %v)", err)
	}
	if res.Success != (err == nil) {
		t.Fatalf("success %v but err %v", res.Success, err)
	}
	if !res.Success {
		return false, res.Error, nil
	}
	return true, res.Output, res.Data
}

func TestCompareTaxMethodsToolComputes(t *testing.T) {
	ok, out, data := runCompareTax(t, `{"doanh_thu":8000000000,"loi_nhuan":1200000000,"ty_le_doanh_thu":1.5,"thue_suat":0.2}`)
	if !ok {
		t.Fatal(out)
	}
	for _, want := range []string{"120.000.000 đ", "240.000.000 đ", "7,50%", "Giả định:",
		"Chưa tính GTGT, lệ phí môn bài, ưu đãi, chi phí không được trừ."} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if data["thue_theo_doanh_thu"].(float64) != 120_000_000 || data["bien_hoa_von"].(float64) != 0.075 {
		t.Errorf("data %v", data)
	}
}

func TestCompareTaxMethodsToolAcceptsQuotedNumbers(t *testing.T) {
	ok, out, _ := runCompareTax(t, `{"doanh_thu":"8000000000","loi_nhuan":"1.2e9","ty_le_doanh_thu":"1.5","thue_suat":"0.2"}`)
	if !ok {
		t.Fatal(out)
	}
}

func TestCompareTaxMethodsToolHasNoDefaults(t *testing.T) {
	ok, msg, _ := runCompareTax(t, `{"doanh_thu":8000000000,"loi_nhuan":1200000000}`)
	if ok {
		t.Fatal("missing ratio and rate must fail")
	}
	if !strings.Contains(msg, "ty_le_doanh_thu") || !strings.Contains(msg, "thue_suat") {
		t.Errorf("message must name the missing parameters: %s", msg)
	}
	ok, msg, _ = runCompareTax(t, `{"doanh_thu":8000000000,"loi_nhuan":1200000000,"ty_le_doanh_thu":1.5,"thue_suat":null}`)
	if ok || !strings.Contains(msg, "thue_suat") {
		t.Errorf("null rate must fail: %s", msg)
	}
}

func TestCompareTaxMethodsToolRejectsBadValues(t *testing.T) {
	for _, args := range []string{
		`{"doanh_thu":"8 tỷ","loi_nhuan":1,"ty_le_doanh_thu":1.5,"thue_suat":0.2}`,
		`{"doanh_thu":"1.200.000","loi_nhuan":1,"ty_le_doanh_thu":1.5,"thue_suat":0.2}`,
		`{"doanh_thu":100,"loi_nhuan":200,"ty_le_doanh_thu":1.5,"thue_suat":0.2}`,
		`{"doanh_thu":100,"loi_nhuan":10,"ty_le_doanh_thu":1.5,"thue_suat":20}`,
		`{"doanh_thu":100,"loi_nhuan":10,"ty_le_doanh_thu":100,"thue_suat":0.2}`,
		`not json`,
	} {
		if ok, _, _ := runCompareTax(t, args); ok {
			t.Errorf("%s: want failure", args)
		}
	}
}

func TestCompareTaxMethodsIsSelectableButNotDefault(t *testing.T) {
	found := false
	for _, def := range AvailableToolDefinitions() {
		if def.Name == ToolCompareTaxMethods {
			found = true
		}
	}
	if !found {
		t.Error("compare_tax_methods must be in AvailableToolDefinitions")
	}
	for _, name := range DefaultAllowedTools() {
		if name == ToolCompareTaxMethods {
			t.Error("compare_tax_methods must not be allowed by default")
		}
	}
	var schema struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(NewCompareTaxMethodsTool().Parameters(), &schema); err != nil {
		t.Fatal(err)
	}
	if len(schema.Required) != 4 {
		t.Errorf("all four parameters are required, got %v", schema.Required)
	}
}

func TestCompareTaxMethodsToolAppendsFormulaBlock(t *testing.T) {
	block := "Công thức áp dụng:\n- Thuế kê khai: Thuế = Lợi nhuận × Thuế suất (căn cứ: 67/2025/QH15)"
	res, err := NewCompareTaxMethodsTool().WithFormulaBlock(block).Execute(context.Background(),
		json.RawMessage(`{"doanh_thu":8000000000,"loi_nhuan":1200000000,"ty_le_doanh_thu":1.5,"thue_suat":0.2}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Output, block) || !strings.HasSuffix(res.Output, compareTaxAnswerNote) {
		t.Errorf("formula block and the note to the model expected:\n%s", res.Output)
	}
	if answer, _ := res.Data[types.ToolDataAnswerBlock].(string); strings.Contains(answer, "Hệ thống tự chèn") {
		t.Error("the note to the model is not part of the user-facing block")
	}
	if strings.Index(res.Output, "Giả định:") > strings.Index(res.Output, "Công thức áp dụng:") {
		t.Error("the assumptions come before the formulas")
	}
}

func TestCompareTaxMethodsToolLeavesAnAnswerBlock(t *testing.T) {
	block := "Công thức áp dụng:\n- X: a = b (căn cứ: 67/2025/QH15)"
	res, err := NewCompareTaxMethodsTool().WithFormulaBlock(block).Execute(context.Background(),
		json.RawMessage(`{"doanh_thu":5000000000,"loi_nhuan":300000000,"ty_le_doanh_thu":1,"thue_suat":0.17}`))
	if err != nil {
		t.Fatal(err)
	}
	answer, _ := res.Data[types.ToolDataAnswerBlock].(string)
	for _, want := range []string{"50.000.000 đ", "51.000.000 đ", "Khuyến nghị sơ bộ:", "Giả định:", block} {
		if !strings.Contains(answer, want) {
			t.Errorf("answer block lacks %q:\n%s", want, answer)
		}
	}
}
