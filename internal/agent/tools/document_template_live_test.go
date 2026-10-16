//go:build live

package tools

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

// Live document templates against a real model through WeKnora's chat
// stack. Opt-in:
//
//	TEMPLATE_LIVE_BASE_URL=http://10.10.0.240:8000/v1 \
//	TEMPLATE_LIVE_MODEL=Qwen/Qwen3.6-35B-A3B-FP8 TEMPLATE_LIVE_PROVIDER=generic \
//	go test -tags live -run LiveDocumentTemplate -v ./internal/agent/tools/
func TestLiveDocumentTemplate(t *testing.T) {
	base, name := os.Getenv("TEMPLATE_LIVE_BASE_URL"), os.Getenv("TEMPLATE_LIVE_MODEL")
	if base == "" || name == "" {
		t.Skip("TEMPLATE_LIVE_BASE_URL / TEMPLATE_LIVE_MODEL not set")
	}
	model, err := chat.NewChat(&chat.ChatConfig{
		Source: types.ModelSourceRemote, BaseURL: base, ModelName: name,
		ModelID: "live", Provider: os.Getenv("TEMPLATE_LIVE_PROVIDER"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	profile := &types.TenantAgencyProfile{
		ParentAgency: "UBND tỉnh Hà Tĩnh", AgencyName: "Sở Nội vụ", AgencyAbbr: "SNV", Locality: "Hà Tĩnh",
		HeadTitle: "Giám đốc", DefaultRecipients: types.StringArray{"- UBND tỉnh (để b/c);"},
	}
	run := func(id string, user TemplateUserContext, turns ...string) *DocumentTemplateOutcome {
		in := DocumentTemplateInput{
			Request: &types.DocumentTemplateRequest{SchemaVersion: 1, RequestID: "live-" + id, TemplateID: id, DocumentID: "doc"},
			Profile: profile, User: user, Today: time.Now(), Model: model,
		}
		var out *DocumentTemplateOutcome
		for _, turn := range turns {
			in.Inputs = append(in.Inputs, turn)
			start := time.Now()
			out, err = PrepareDocumentTemplate(context.Background(), in)
			if err != nil {
				t.Fatalf("%s: %v", id, err)
			}
			in.Asked = out.Asked
			if out.Learned != nil {
				in.User = *out.Learned
			}
			raw, _ := json.MarshalIndent(out.Data, "", "  ")
			t.Logf("== %s, turn %q (%s)\n%s\nanswer: %s\nlearned: %+v", id, turn, time.Since(start).Round(time.Millisecond), raw, out.Answer, out.Learned)
		}
		return out
	}

	unit := TemplateUserContext{Unit: "Văn phòng", UnitAbbr: "VP", Position: "Chuyên viên"}
	if out := run("ke-hoach", unit,
		"Tạo cho tôi văn bản kế hoạch theo NĐ 30, thông tin tôi cần là: tổ chức đào tạo kỹ năng ứng dụng trí tuệ nhân tạo cho cán bộ, công chức của Sở trong quý IV năm 2026; 2 lớp, mỗi lớp 40 học viên; Văn phòng Sở chủ trì, các phòng cử người tham gia. Giám đốc Trần Văn Bình ký trực tiếp."); !out.Ready {
		t.Errorf("ke-hoach not ready")
	}
	if out := run("cong-van", TemplateUserContext{},
		"Tạo cho tôi văn bản công văn theo NĐ 30, thông tin tôi cần là: đề nghị các phòng thuộc Sở báo cáo kết quả cải cách hành chính 9 tháng đầu năm, gửi về Văn phòng Sở trước ngày 20/10/2026",
		"Tôi là chuyên viên Phòng Tổ chức cán bộ, viết tắt TCCB. Phó Giám đốc Lê Thị Hoa ký thay Giám đốc."); !out.Ready {
		t.Errorf("cong-van not ready after the follow-up")
	}
	run("quyet-dinh", unit,
		"Tạo cho tôi văn bản quyết định cá biệt theo NĐ 30, thông tin tôi cần là: cử bà Nguyễn Thị Lan, chuyên viên Văn phòng, tham dự lớp bồi dưỡng quản lý nhà nước ngạch chuyên viên chính từ ngày 02/11/2026 đến ngày 30/11/2026 tại Hà Nội. Căn cứ Quyết định số 12/2025/QĐ-UBND ngày 10/3/2025 của UBND tỉnh quy định chức năng, nhiệm vụ của Sở Nội vụ. Theo đề nghị của Chánh Văn phòng Sở. Giám đốc Trần Văn Bình ký trực tiếp.")
}
