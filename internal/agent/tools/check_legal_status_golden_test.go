package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

// checkLegalStatusGolden pins the whole report text and derived data of
// check_legal_status, generated before the status reconciliation moved to
// types (44e49ae8): the move must not change a byte the model reads.
const checkLegalStatusGolden = "3 documents match; ask the user which one they mean if it is unclear.\n\n## Luật 29/2018/QH14 — Luật Bảo vệ bí mật nhà nước\n- Cơ quan ban hành: Quốc hội\n- Ngày ban hành: 15/11/2018; có hiệu lực: 01/07/2020; hết hiệu lực: —\n- Tình trạng hiệu lực: Còn hiệu lực (theo vbpl, cập nhật 23/07/2026) — LƯU Ý: đã bị thay thế bởi Luật 117/2025/QH15 có hiệu lực từ 01/03/2026, nên văn bản này hết hiệu lực từ ngày đó (suy ra từ quan hệ thay thế; vbpl chưa cập nhật trạng thái)\n- Bị thay thế bởi:\n  - Luật 9/2027/QH16 — Luật tương lai (ban hành 01/09/2026; chưa rõ tình trạng)\n  - LUẬT BẢO VỆ BÍ MẬT NHÀ NƯỚC SỐ 117/2025/QH15 (ban hành 10/12/2025; Còn hiệu lực)\n- Bị làm hết hiệu lực bởi:\n  - Nghị quyết 5/2026/NQ-HĐND — Nghị quyết làm hết hiệu lực (ban hành 02/01/2026; Còn hiệu lực)\n- Được sửa đổi, bổ sung bởi:\n  - Luật 43/2024/QH15 — Luật sửa đổi (ban hành 29/06/2024; Còn hiệu lực)\n- Được quy định chi tiết, hướng dẫn bởi:\n  - Thông tư 9/2025/TT-BCA — Thông tư hướng dẫn (ban hành 01/09/2025; chưa rõ tình trạng)\n  - Thông tư 8/2025/TT-BCA — Thông tư hướng dẫn (ban hành 01/08/2025; chưa rõ tình trạng)\n  - Thông tư 7/2025/TT-BCA — Thông tư hướng dẫn (ban hành 01/07/2025; chưa rõ tình trạng)\n  - Thông tư 6/2025/TT-BCA — Thông tư hướng dẫn (ban hành 01/06/2025; chưa rõ tình trạng)\n  - Thông tư 5/2025/TT-BCA — Thông tư hướng dẫn (ban hành 01/05/2025; chưa rõ tình trạng)\n  - Thông tư 4/2025/TT-BCA — Thông tư hướng dẫn (ban hành 01/04/2025; chưa rõ tình trạng)\n  - Thông tư 3/2025/TT-BCA — Thông tư hướng dẫn (ban hành 01/03/2025; chưa rõ tình trạng)\n  - Thông tư 2/2025/TT-BCA — Thông tư hướng dẫn (ban hành 01/02/2025; chưa rõ tình trạng)\n  - … và 2 văn bản khác\n- Căn cứ:\n  - Luật 43/2024/QH15 — Luật sửa đổi (ban hành 29/06/2024; Còn hiệu lực)\n\n## Luật 30/2018/QH14 — Luật Bảo vệ bí mật nhà nước\n- Cơ quan ban hành: Quốc hội\n- Ngày ban hành: 15/11/2018; có hiệu lực: 01/07/2020; hết hiệu lực: —\n- Tình trạng hiệu lực: Hết hiệu lực một phần (theo vbpl) — LƯU Ý: có thể đã hết hiệu lực toàn bộ — đã bị thay thế bởi Luật 117/2025/QH15 có hiệu lực từ 01/03/2026; kiểm tra điều khoản chuyển tiếp của Luật 117/2025/QH15 trước khi kết luận\n- Bị thay thế bởi:\n  - Luật 9/2027/QH16 — Luật tương lai (ban hành 01/09/2026; chưa rõ tình trạng)\n  - LUẬT BẢO VỆ BÍ MẬT NHÀ NƯỚC SỐ 117/2025/QH15 (ban hành 10/12/2025; Còn hiệu lực)\n- Bị làm hết hiệu lực bởi:\n  - Nghị quyết 5/2026/NQ-HĐND — Nghị quyết làm hết hiệu lực (ban hành 02/01/2026; Còn hiệu lực)\n- Được sửa đổi, bổ sung bởi:\n  - Luật 43/2024/QH15 — Luật sửa đổi (ban hành 29/06/2024; Còn hiệu lực)\n- Được quy định chi tiết, hướng dẫn bởi:\n  - Thông tư 9/2025/TT-BCA — Thông tư hướng dẫn (ban hành 01/09/2025; chưa rõ tình trạng)\n  - Thông tư 8/2025/TT-BCA — Thông tư hướng dẫn (ban hành 01/08/2025; chưa rõ tình trạng)\n  - Thông tư 7/2025/TT-BCA — Thông tư hướng dẫn (ban hành 01/07/2025; chưa rõ tình trạng)\n  - Thông tư 6/2025/TT-BCA — Thông tư hướng dẫn (ban hành 01/06/2025; chưa rõ tình trạng)\n  - Thông tư 5/2025/TT-BCA — Thông tư hướng dẫn (ban hành 01/05/2025; chưa rõ tình trạng)\n  - Thông tư 4/2025/TT-BCA — Thông tư hướng dẫn (ban hành 01/04/2025; chưa rõ tình trạng)\n  - Thông tư 3/2025/TT-BCA — Thông tư hướng dẫn (ban hành 01/03/2025; chưa rõ tình trạng)\n  - Thông tư 2/2025/TT-BCA — Thông tư hướng dẫn (ban hành 01/02/2025; chưa rõ tình trạng)\n  - … và 2 văn bản khác\n- Căn cứ:\n  - Luật 43/2024/QH15 — Luật sửa đổi (ban hành 29/06/2024; Còn hiệu lực)\n\n## Nghị định 99/2026/NĐ-CP về thử nghiệm\n- Ngày ban hành: 01/10/2026; có hiệu lực: —; hết hiệu lực: —\n- Tình trạng hiệu lực: chưa xác định (theo thuvienphapluat.vn)\n- LƯU Ý: dòng danh mục lấy từ thuvienphapluat.vn, chưa có trên vbpl.vn và chưa được SuperAdmin đối chiếu: trạng thái, ngày tháng chỉ để tham khảo\n\n--- derived ---\n[{\"derived_expired_on\":\"2026-03-01\",\"derived_from\":\"117/2025/QH15\",\"derived_status\":\"Hết hiệu lực toàn bộ\",\"document_id\":\"old\",\"document_number\":\"29/2018/QH14\"},{\"derived_expired_on\":\"2026-03-01\",\"derived_from\":\"117/2025/QH15\",\"derived_status\":\"Có thể đã hết hiệu lực toàn bộ\",\"document_id\":\"partly\",\"document_number\":\"30/2018/QH14\"}]"

func TestCheckLegalStatusGoldenOutput(t *testing.T) {
	day := func(s string) *time.Time { d, _ := time.Parse("2006-01-02", s); return &d }
	defer func(prev func() time.Time) { legalNow = prev }(legalNow)
	legalNow = func() time.Time { return *day("2026-10-16") }

	newLaw := &types.LegalDocument{ID: "new", Source: "vbpl", DocType: "Luật", DocumentNumber: "117/2025/QH15",
		Title: "LUẬT BẢO VỆ BÍ MẬT NHÀ NƯỚC SỐ 117/2025/QH15", IssuedOn: day("2025-12-10"), EffectiveOn: day("2026-03-01"),
		Status: "Còn hiệu lực"}
	later := &types.LegalDocument{ID: "later", Source: "vbpl", DocType: "Nghị quyết", DocumentNumber: "5/2026/NQ-HĐND",
		Title: "Nghị quyết làm hết hiệu lực", IssuedOn: day("2026-01-02"), EffectiveOn: day("2026-04-01"), Status: "Còn hiệu lực"}
	future := &types.LegalDocument{ID: "future", Source: "vbpl", DocType: "Luật", DocumentNumber: "9/2027/QH16",
		Title: "Luật tương lai", IssuedOn: day("2026-09-01"), EffectiveOn: day("2027-01-01")}
	amend := &types.LegalDocument{ID: "amend", Source: "vbpl", DocType: "Luật", DocumentNumber: "43/2024/QH15",
		Title: "Luật sửa đổi", IssuedOn: day("2024-06-29"), Status: "Còn hiệu lực"}
	links := []types.LegalRelationLink{
		{Relation: types.LegalRelationReplaces, Incoming: true, Other: future},
		{Relation: types.LegalRelationExpires, Incoming: true, Other: later},
		{Relation: types.LegalRelationReplaces, Incoming: true, Other: newLaw},
		{Relation: types.LegalRelationAmends, Incoming: true, Other: amend},
		{Relation: types.LegalRelationBasedOn, Incoming: false, Other: amend},
	}
	for i := 0; i < 10; i++ {
		links = append(links, types.LegalRelationLink{Relation: types.LegalRelationGuides, Incoming: true,
			Other: &types.LegalDocument{ID: fmt.Sprintf("g%d", i), DocType: "Thông tư",
				DocumentNumber: fmt.Sprintf("%d/2025/TT-BCA", i+1), Title: "Thông tư hướng dẫn",
				IssuedOn: day(fmt.Sprintf("2025-0%d-01", i%9+1))}})
	}
	inForce := &types.LegalDocument{ID: "old", Source: "vbpl", DocType: "Luật", DocumentNumber: "29/2018/QH14",
		Title: "Luật Bảo vệ bí mật nhà nước", Issuer: "Quốc hội", IssuedOn: day("2018-11-15"),
		EffectiveOn: day("2020-07-01"), Status: "Còn hiệu lực", SourceUpdatedAt: day("2026-07-23")}
	partly := *inForce
	partly.ID, partly.DocumentNumber, partly.Status, partly.SourceUpdatedAt = "partly", "30/2018/QH14", "Hết hiệu lực một phần", nil
	feed := &types.LegalDocument{ID: "feed", Source: "tvpl", DocType: "Nghị định", DocumentNumber: "99/2026/NĐ-CP",
		Title: "Nghị định 99/2026/NĐ-CP về thử nghiệm", IssuedOn: day("2026-10-01")}
	svc := &fakeLegalCatalog{reports: []*types.LegalStatusReport{
		{Document: inForce, Links: links},
		{Document: &partly, Links: links},
		{Document: feed},
	}}
	res, err := NewCheckLegalStatusTool(svc).Execute(context.Background(), json.RawMessage(`{"document_number":"29/2018/QH14"}`))
	if err != nil || !res.Success {
		t.Fatalf("execute: %v %+v", err, res)
	}
	derived, _ := json.Marshal(res.Data["derived_status"])
	got := res.Output + "\n--- derived ---\n" + string(derived)
	if os.Getenv("PRINT_GOLDEN") != "" {
		fmt.Printf("%q\n", got)
	}
	if got != checkLegalStatusGolden {
		t.Fatalf("report changed:\n%s", got)
	}
}
