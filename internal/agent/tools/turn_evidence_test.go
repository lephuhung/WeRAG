package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

// The turn of hkd-1 (2026-11-03): Thông tư 40/2021 (100 triệu) and Nghị
// định 68/2026 as the model reads it (01 tỷ, the old words in a note), and
// Luật 109/2025 whose old 500 triệu sits only in a history note.
func hkdTurn() (context.Context, LegalDocIdentityFunc) {
	search := &types.ToolResult{Success: true, Data: map[string]interface{}{
		"results": []map[string]interface{}{
			{"knowledge_id": "k40", "content": "2. Hộ kinh doanh, cá nhân kinh doanh có doanh thu năm dương lịch từ 100 triệu đồng trở xuống thì thuộc trường hợp không phải nộp thuế GTGT và không phải nộp thuế TNCN."},
			{"knowledge_id": "k68", "content": "1. … từ 500 triệu đồng trở xuống thuộc đối tượng không chịu thuế giá trị gia tăng.",
				"model_content": "1. Hộ kinh doanh có mức doanh thu năm từ 01 tỷ đồng [thay cho “500 triệu đồng” theo Nghị định 141/2026/NĐ-CP, từ 01/01/2026] trở xuống thuộc đối tượng không chịu thuế giá trị gia tăng."},
			{"knowledge_id": "k109", "model_content": "[Lời văn hiện hành theo Luật 09/2026/QH16, từ 24/04/2026]\n\n1. Cá nhân cư trú có doanh thu năm từ mức quy định của Chính phủ trở xuống không phải nộp thuế thu nhập cá nhân.\n\n[trước 24/04/2026: 1. Cá nhân cư trú có mức doanh thu năm từ 500 triệu đồng trở xuống không phải nộp thuế thu nhập cá nhân.]"},
		},
	}}
	read := &types.ToolResult{Success: true, Data: map[string]interface{}{
		"chunks": []map[string]interface{}{{"knowledge_id": "k141", "content": "Sửa đổi cụm từ “500 triệu đồng” thành “01 tỷ đồng” tại Điều 3, Điều 4."}},
	}}
	steps := []types.AgentStep{{ToolCalls: []types.ToolCall{{Name: ToolSearchKnowledge, Result: search}}}, {ToolCalls: []types.ToolCall{{Name: ToolReadDocument, Result: read}}}}
	ids := map[string]LegalDocIdentity{
		"k40":  {Number: "40/2021/TT-BTC", Type: "Thông tư", IssueDate: "2021-06-01", Status: "Hết hiệu lực một phần"},
		"k68":  {Number: "68/2026/NĐ-CP", Type: "Nghị định", IssueDate: "2026-03-05", Status: "Hết hiệu lực một phần"},
		"k109": {Number: "109/2025/QH15", Type: "Luật", IssueDate: "2025-12-10", Status: "Còn hiệu lực"},
		"k141": {Number: "141/2026/NĐ-CP", Type: "Nghị định", IssueDate: "2026-04-29", Status: "Còn hiệu lực"},
	}
	identity := func(_ context.Context, kids []string) map[string]LegalDocIdentity {
		out := map[string]LegalDocIdentity{}
		for _, k := range kids {
			if d, ok := ids[k]; ok {
				out[k] = d
			}
		}
		return out
	}
	return WithTurnSteps(context.Background(), steps), identity
}

func TestRevenueThresholds(t *testing.T) {
	require.Equal(t, []float64{1e9}, revenueThresholds("mức doanh thu năm từ 01 tỷ đồng [thay cho “500 triệu đồng” theo …] trở xuống thuộc đối tượng không chịu thuế"))
	require.Equal(t, []float64{1e8}, revenueThresholds("doanh thu năm dương lịch từ 100 triệu đồng trở xuống thì không phải nộp thuế GTGT"))
	require.Empty(t, revenueThresholds("[trước 24/04/2026: doanh thu năm từ 500 triệu đồng trở xuống không phải nộp thuế]"), "history note")
	require.Empty(t, revenueThresholds("doanh thu năm từ 500 triệu đồng trở xuống thì thông báo doanh thu"), "not an exemption")
	require.Equal(t, []float64{1e9}, revenueThresholds("doanh thu từ 1.000.000.000 đồng trở xuống không phải nộp thuế"))
}

func TestCheckThresholdBasis(t *testing.T) {
	ctx, identity := hkdTurn()
	check := func(basis string, v float64) string {
		if err := checkThresholdBasis(ctx, identity, "can_cu_nguong", "ngưỡng doanh thu", basis, v); err != nil {
			return err.Error()
		}
		return ""
	}
	require.Empty(t, check("68/2026/NĐ-CP", 1e9), "the current decree")
	require.Empty(t, check("68/2026/ND-CP", 1e9), "number written without Đ")
	// the hkd-1 failure: the old circular's 100 triệu
	msg := check("40/2021/TT-BTC", 1e8)
	require.Contains(t, msg, "68/2026/NĐ-CP")
	require.Contains(t, msg, "1.000.000.000 đ")
	require.Contains(t, msg, "mới hơn")
	// the decree cited with the old circular's figure
	require.Contains(t, check("68/2026/NĐ-CP", 1e8), "ghi ngưỡng doanh thu là 1.000.000.000 đ")
	// the old 500 triệu of Luật 109 is only in a note: no figure, no block
	require.Empty(t, check("141/2026/NĐ-CP", 1e9), "an amending decree that states no threshold sentence")
	require.Contains(t, check("12/2099/NĐ-CP", 1e9), "không có trong kết quả tra cứu của lượt này")
	// outside a turn (no steps) or without a lookup the check is skipped
	require.NoError(t, checkThresholdBasis(context.Background(), identity, "can_cu_nguong", "ngưỡng", "40/2021/TT-BTC", 1e8))
	require.NoError(t, checkThresholdBasis(ctx, nil, "can_cu_nguong", "ngưỡng", "40/2021/TT-BTC", 1e8))
}

// The tool refuses the old circular's threshold and says what to use.
func TestCalculateHouseholdTaxBasisRefused(t *testing.T) {
	ctx, identity := hkdTurn()
	tool := NewCalculateHouseholdTaxTool().WithLegalDocIdentity(identity)
	res, err := tool.Execute(ctx, json.RawMessage(`{"doanh_thu": 2400000000, "nguong_doanh_thu": 100000000, "can_cu_nguong": "40/2021/TT-BTC", "ty_le_gtgt": 1, "ty_le_tncn": 0.5, "tran_doanh_thu_theo_doanh_thu": 1000000000}`))
	require.Error(t, err)
	require.False(t, res.Success)
	require.True(t, strings.Contains(res.Error, "68/2026/NĐ-CP"), res.Error)
	res, err = tool.Execute(ctx, json.RawMessage(`{"doanh_thu": 2400000000, "nguong_doanh_thu": 1000000000, "can_cu_nguong": "68/2026/NĐ-CP", "ty_le_gtgt": 1, "ty_le_tncn": 0.5, "tran_doanh_thu_theo_doanh_thu": 3000000000}`))
	require.NoError(t, err)
	require.True(t, res.Success)
	res, _ = tool.Execute(ctx, json.RawMessage(`{"doanh_thu": 2400000000, "nguong_doanh_thu": 1000000000, "ty_le_gtgt": 1, "ty_le_tncn": 0.5, "tran_doanh_thu_theo_doanh_thu": 3000000000}`))
	require.Contains(t, res.Error, "can_cu_nguong")
}
