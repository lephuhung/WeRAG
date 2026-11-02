package tools

import (
	"strings"
	"testing"
)

func TestWantsWordFile(t *testing.T) {
	for _, q := range []string{
		"Giờ tạo cho tôi file Word kế hoạch này",
		"Đưa nội dung trên vào file Word giúp tôi",
		"Tạo cho tôi file Word công văn gửi các phòng về lịch tập huấn chuyển đổi số ngày 25/10/2026",
		"xuất ra Word nhé",
		"Lưu thành .docx chứ",
		"soạn giúp tôi tệp văn bản thông báo này",
		"Bạn có thể tạo file word không?",
		"Cho tôi file Word kế hoạch này",
		"Gửi tôi bản Word nhé",
		"Tải về dạng Word giúp tôi",
		"In ra Word",
	} {
		if !WantsWordFile(q) {
			t.Errorf("%q: want a Word file", q)
		}
	}
	for _, q := range []string{
		"Word là gì, khác gì Google Docs?",
		"Cách đánh số trang trong Word như thế nào?",
		"Hướng dẫn tôi cách tạo file Word",
		"Công văn 12/QT-SNV quy định gì?",
		"Soạn giúp tôi nội dung công văn gửi các phòng",
		"Chuyển file Word này sang PDF",
		"Văn bản Word này nói gì?",
		"Tạo kế hoạch tập huấn", // no file asked: the agent answers in chat
		"keyword nào hay dùng",  // "word" inside another word
		"Tôi soạn trong Word bị lỗi font",
		"Viết hoa chữ đầu dòng trong Word",
		"Lưu ý khi dùng Word để soạn công văn",
		"Tạo kế hoạch tập huấn rồi gửi các phòng, ai không mở được Word thì báo",
	} {
		if WantsWordFile(q) {
			t.Errorf("%q: not a request for a Word file", q)
		}
	}
}

func TestHandoffTemplateID(t *testing.T) {
	if got := HandoffTemplateID("Giờ tạo cho tôi file Word kế hoạch này", nil); got != "ke-hoach" {
		t.Errorf("from the message: %q", got)
	}
	earlier := []string{"Chốt như sau: 3 lớp, 40 học viên", "Tôi cần tổ chức tập huấn, kế hoạch nên có gì?"}
	if got := HandoffTemplateID("Tạo file Word giúp tôi", earlier); got != "ke-hoach" {
		t.Errorf("from the conversation: %q", got)
	}
	if got := HandoffTemplateID("Đưa vào file Word công văn, theo kế hoạch đã duyệt", nil); got != "ke-hoach" {
		t.Errorf("last named: %q", got)
	}
	if got := HandoffTemplateID("Tạo file Word giúp tôi", []string{"xin chào"}); got != "" {
		t.Errorf("none named: %q", got)
	}
	if got := HandoffTemplateID("Tạo file Word giúp tôi", []string{"Lãnh đạo đã quyết định tổ chức 3 lớp"}); got != "" {
		t.Errorf("a verb taken for the type: %q", got)
	}
	if got := HandoffTemplateID("Tạo file Word giúp tôi", []string{"Soạn giúp bản báo cáo tháng 10"}); got != "bao-cao" {
		t.Errorf("bản báo cáo: %q", got)
	}
	// documents a legal chat cites are no type to draft…
	for _, cited := range []string{"Nghị quyết 18-NQ/TW còn hiệu lực không?", "Công điện 05/CĐ-TTg nói gì?", "Khi nào lập biên bản vi phạm hành chính?"} {
		if got := HandoffTemplateID("Tạo file Word giúp tôi", []string{cited}); got != "" {
			t.Errorf("%q taken for the type: %q", cited, got)
		}
	}
	// …unless named as one
	if got := HandoffTemplateID("Tạo file Word giúp tôi", []string{"Viết dự thảo nghị quyết về chuyển đổi số"}); got != "nghi-quyet" {
		t.Errorf("dự thảo nghị quyết: %q", got)
	}
	if got := HandoffTemplateID("Đưa giấy mời này vào file Word", nil); got != "giay-moi" {
		t.Errorf("giấy mời: %q", got)
	}
}

func TestAgentHandoffCard(t *testing.T) {
	c := &AgentHandoffCard{TemplateID: "ke-hoach"}
	d := c.Data()
	if d["display_type"] != AgentHandoffType || d["target_agent_id"] != "builtin-document-assistant" ||
		d["resend"] != "Soạn kế hoạch từ nội dung đã trao đổi" || d["from_conversation"] != true {
		t.Fatalf("card %v", d)
	}
	if !strings.Contains(c.Message(), "Soạn thảo văn bản") || !strings.Contains(c.Message(), "soạn kế hoạch") {
		t.Fatalf("message %q", c.Message())
	}
	if opts, _ := d["options"].([]map[string]interface{}); len(opts) != len(handoffTemplateWords) || opts[0]["template_id"] != "ke-hoach" {
		t.Fatalf("the guess first among all types: %v", d["options"])
	}
	d = (&AgentHandoffCard{}).Data()
	opts, _ := d["options"].([]map[string]interface{})
	if len(opts) != len(handoffTemplateWords) || d["resend"] != nil || opts[0]["resend"] != "Soạn công văn từ nội dung đã trao đổi" {
		t.Fatalf("no type: %v", d)
	}
}
