//go:build live

package tools

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

// The NĐ30 form types against a real model (same opt-in as
// TestLiveDocumentTemplate): each request is complete in one message, so
// each must come back ready with the facts in their lines.
func TestLiveDocumentTemplateForms(t *testing.T) {
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
	// the agency in capitals, as most profiles hold it
	profile := &types.TenantAgencyProfile{
		ParentAgency: "UBND TỈNH HÀ TĨNH", AgencyName: "SỞ NỘI VỤ", AgencyAbbr: "SNV", Locality: "Hà Tĩnh",
		HeadTitle: "Giám đốc", CollectiveName: "Ban Giám đốc", DefaultRecipients: types.StringArray{"- UBND tỉnh (để b/c);"},
	}
	cases := []struct {
		id, ask string
		want    []string
	}{
		{"giay-moi", "Soạn giấy mời trưởng các phòng thuộc Sở dự hội nghị sơ kết công tác cải cách hành chính 9 tháng năm 2026, do Giám đốc Sở chủ trì, lúc 08 giờ 00 ngày 25/10/2026 tại Hội trường tầng 3 Sở Nội vụ; đề nghị các phòng chuẩn bị ý kiến tham luận. Người ký: Nguyễn Văn An, Giám đốc, ký trực tiếp.",
			[]string{"Sở Nội vụ trân trọng kính mời", "Tới dự", "Thời gian: 08 giờ 00", "Địa điểm: Hội trường tầng 3"}},
		{"giay-gioi-thieu", "Giấy giới thiệu bà Trần Thị Bình, chuyên viên Phòng Tổ chức, đến Sở Tài chính liên hệ quyết toán kinh phí năm 2025, có giá trị đến hết ngày 31/10/2026. Người ký: Nguyễn Văn An, Giám đốc, ký trực tiếp.",
			[]string{"trân trọng giới thiệu:", "Ông (bà): Trần Thị Bình", "Được cử đến: Sở Tài chính", "Giấy này có giá trị đến hết ngày 31/10/2026./."}},
		{"giay-nghi-phep", "Cấp giấy nghỉ phép cho ông Lê Văn Cường, chuyên viên Văn phòng, theo đơn ngày 15/10/2026, nghỉ 05 ngày từ 20/10/2026 đến hết 24/10/2026 tại thành phố Đà Nẵng, tính vào phép năm 2026. Người ký: Nguyễn Văn An, Giám đốc, ký trực tiếp.",
			[]string{"Xét Đơn đề nghị nghỉ phép ngày 15/10/2026 của ông (bà) Lê Văn Cường", "kể từ ngày 20/10/2026 đến hết ngày 24/10/2026", "được tính vào thời gian"}},
		{"bien-ban", "Lập biên bản cuộc họp giao ban tháng 10 năm 2026, bắt đầu 14 giờ 00 ngày 15/10/2026 tại phòng họp số 1; thành phần: Ban Giám đốc và trưởng các phòng; chủ trì ông Nguyễn Văn An, Giám đốc Sở; thư ký bà Phạm Thị Dung, chuyên viên Văn phòng. Nội dung: các phòng báo cáo kết quả tháng 10; Giám đốc kết luận giao Văn phòng tổng hợp kế hoạch tháng 11. Cuộc họp kết thúc lúc 16 giờ 30 cùng ngày.",
			[]string{"Thời gian bắt đầu: 14 giờ 00", "Chủ trì (chủ tọa): Nguyễn Văn An", "Thư ký (người ghi biên bản): Phạm Thị Dung", "báo cáo kết quả", "kết thúc vào"}},
		{"cong-dien", "Công điện của Giám đốc Sở Nội vụ gửi trưởng các phòng thuộc Sở và thủ trưởng các đơn vị sự nghiệp trực thuộc về việc chủ động ứng phó bão số 6: trực ban 24/24 giờ, rà soát trụ sở, sẵn sàng phương án di dời hồ sơ lưu trữ. Người ký: Nguyễn Văn An, Giám đốc, ký trực tiếp.",
			nil},
		{"nghi-quyet", "Nghị quyết của Ban Giám đốc thông qua kế hoạch chuyển đổi số năm 2027 của Sở, căn cứ Luật Tổ chức chính quyền địa phương ngày 19/02/2025; Điều 1 thông qua kế hoạch, Điều 2 giao Văn phòng tổ chức thực hiện, nghị quyết có hiệu lực từ ngày ký. Người ký: Nguyễn Văn An, Giám đốc, TM. Ban Giám đốc.",
			nil},
		{"chi-thi", "Chỉ thị của Giám đốc Sở về tăng cường kỷ luật, kỷ cương hành chính: yêu cầu cán bộ chấp hành giờ làm việc, không sử dụng điện thoại cá nhân vào việc riêng trong giờ làm việc, trưởng phòng chịu trách nhiệm kiểm tra hằng tuần. Người ký: Nguyễn Văn An, Giám đốc, ký trực tiếp.",
			nil},
	}
	for _, c := range cases {
		in := DocumentTemplateInput{
			Request: &types.DocumentTemplateRequest{SchemaVersion: 1, RequestID: "live-" + c.id, TemplateID: c.id, DocumentID: "doc"},
			Profile: profile, User: TemplateUserContext{Unit: "Văn phòng", UnitAbbr: "VP"},
			Inputs: []string{c.ask}, Today: time.Now(), Model: model,
		}
		start := time.Now()
		out, err := PrepareDocumentTemplate(context.Background(), in)
		if err != nil {
			t.Fatalf("%s: %v", c.id, err)
		}
		raw, _ := json.MarshalIndent(out.Data, "", "  ")
		t.Logf("== %s (%s)\n%s\nanswer: %s", c.id, time.Since(start).Round(time.Millisecond), raw, out.Answer)
		if !out.Ready {
			t.Errorf("%s: not ready: %v", c.id, out.Data["missing_fields"])
			continue
		}
		var body []string
		for _, p := range out.Data["fields"].(map[string]interface{})["paragraphs"].([]templateParagraph) {
			body = append(body, p.Text)
		}
		text := strings.Join(body, "\n")
		for _, w := range c.want {
			if !strings.Contains(text, w) {
				t.Errorf("%s: body lacks %q", c.id, w)
			}
		}
	}
}
