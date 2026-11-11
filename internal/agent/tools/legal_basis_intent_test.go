package tools

import "testing"

func TestClassifyLegalBasisRequest(t *testing.T) {
	pure := []string{
		"Rà soát căn cứ pháp lý",
		"rà soát căn cứ",
		"Rà soát căn cứ pháp lý giúp tôi nhé",
		"rà soát giúp tôi căn cứ pháp lý",
		"Hãy rà soát căn cứ pháp lý của văn bản này",
		"Kiểm tra căn cứ pháp lý của dự thảo này",
		"Soát lại căn cứ trong vb2",
		"Kiểm tra giúp tôi các căn cứ",
		"kiểm tra văn bản này còn hiệu lực không",
		"Kiểm tra văn bản này còn hiệu lực không?",
		"Các văn bản viện dẫn còn hiệu lực không?",
		"các văn bản được viện dẫn có còn hiệu lực không",
		"Kiểm tra hiệu lực các văn bản được viện dẫn",
		"Có văn bản nào được viện dẫn đã hết hiệu lực không?",
		"Văn bản được dẫn chiếu trong tờ trình còn hiệu lực?",
		"Các căn cứ pháp lý còn hiệu lực không?",
		"Tra căn cứ pháp lý được dẫn trong đoạn này và kiểm tra còn hiệu lực không",
		"Đối chiếu căn cứ ban hành",
		"có căn cứ nào hết hiệu lực không",
		"phần căn cứ có văn bản nào đã hết hiệu lực chưa",
		"các văn bản trong phần căn cứ còn hiệu lực không",
		"rà soát hiệu lực các văn bản được nêu",
		// review round 2: pure unless another question is asked
		"các nghị định được nêu trong văn bản còn hiệu lực không",
		"kiểm tra xem các luật, nghị định trong tờ trình còn hiệu lực không",
		"thông tư trong văn bản này còn hiệu lực không",
		"cho tôi biết các văn bản viện dẫn còn hiệu lực không",
		"Nhờ bạn kiểm tra các căn cứ pháp lý trong văn bản",
		"kiểm tra tình trạng hiệu lực các căn cứ pháp lý",
		"rà soát căn cứ pháp lý của văn bản này xem có văn bản nào đã bị thay thế, sửa đổi",
		"gạch chân các văn bản hết hiệu lực",
		"căn cứ pháp lý của quyết định này",
		"căn cứ pháp lý của công văn này còn hiệu lực không",
		"căn cứ pháp lý của hợp đồng này",
		"rà soát căn cứ pháp lý và liệt kê văn bản hết hiệu lực",
		"kiểm tra căn cứ pháp lý xem còn hiệu lực không, liệt kê giúp tôi",
		"kiểm tra hiệu lực văn bản này",
		"rà soát hiệu lực văn bản",
		// review round 3: whatever follows "căn cứ" unless it is a căn cứ
		// of something else; a clause of thanks keeps it pure
		"rà soát căn cứ pháp lý cho tôi",
		"rà soát căn cứ pháp lý hộ mình",
		"rà soát căn cứ pháp lý ạ",
		"rà soát căn cứ pháp lý văn bản này",
		"rà soát căn cứ pháp lý tờ trình này",
		"rà soát căn cứ pháp lý file này",
		"rà soát căn cứ pháp lý dự thảo này",
		"căn cứ pháp lý văn bản này còn hiệu lực không",
		"kiểm tra căn cứ, viện dẫn trong văn bản này",
		"các căn cứ pháp lý bạn kiểm tra giúp",
		"rà soát căn cứ pháp lý toàn bộ văn bản",
		"rà soát căn cứ pháp lý tất cả các văn bản đang mở",
		"Kiểm tra căn cứ pháp lý: văn bản nào hết hiệu lực?",
		"kiểm tra căn cứ pháp lý. Cảm ơn",
		"kiểm tra căn cứ pháp lý, cảm ơn",
		"Các văn bản viện dẫn còn hiệu lực không? Cảm ơn bạn nhiều!",
	}
	mixed := []string{
		// the check runs, the rest needs a search (grounding gate stays)
		"rà soát căn cứ pháp lý của văn bản này và cho biết mức phạt nồng độ cồn",
		"Thông tư 01/2011/TT-BNV được viện dẫn trong văn bản này còn hiệu lực không?",
		"tóm tắt dự thảo và kiểm tra căn cứ",
		"kiểm tra căn cứ pháp lý của văn bản này rồi sửa lỗi chính tả",
		"Các văn bản viện dẫn còn hiệu lực không? Văn bản nào thay thế Luật Đất đai?",
		// review round 3: a later clause with a task or a question
		"rà soát căn cứ pháp lý, sau đó tóm tắt văn bản",
		"rà soát căn cứ pháp lý, đồng thời phân tích rủi ro",
		"rà soát căn cứ pháp lý. Thuế GTGT hiện nay là bao nhiêu?",
		"Rà soát căn cứ pháp lý. Thông tư 80 hướng dẫn những nội dung chính nào?",
	}
	no := []string{
		"",
		"Nghị định 30/2020/NĐ-CP còn hiệu lực không?",
		"Luật Đất đai 2013 còn hiệu lực không",
		"Nghị định 68 còn hiệu lực không",
		"Luật này còn hiệu lực không?",
		"Căn cứ pháp lý để thành lập hội là gì?",
		"Kiểm tra căn cứ pháp lý để xử phạt hành vi lấn chiếm đất",
		"Kiểm tra căn cứ pháp lý về thẩm quyền xử phạt",
		"Căn cứ pháp lý của văn bản này là gì?",
		"Kiểm tra thể thức văn bản này theo Nghị định 30",
		"Rà soát thể thức và căn cứ pháp lý",
		"Kiểm tra chính tả văn bản này",
		"Văn bản này có hiệu lực từ ngày nào?",
		"Văn bản này còn hiệu lực đến khi nào?",
		"Tóm tắt văn bản này",
		"Mức phạt vi phạm hành chính về xây dựng là bao nhiêu?",
		"Hãy viện dẫn thêm căn cứ pháp lý cho tờ trình",
		"Viện dẫn Điều 5 Luật Đất đai vào đoạn này",
		"Thông tư 01/2011/TT-BNV hết hiệu lực chưa, văn bản thay thế là gì?",
		"Quy định về hiệu lực thi hành của luật",
		// review findings (Fable, 2026-11-11)
		"viện dẫn là gì, hiệu lực hồi tố là gì",
		"cách viện dẫn văn bản hết hiệu lực trong tờ trình như thế nào",
		"quy định về dẫn chiếu văn bản hết hiệu lực",
		"các căn cứ để xử phạt hành chính còn hiệu lực là gì",
		"các văn bản viện dẫn trong Nghị định 15/2020/NĐ-CP còn hiệu lực không",
		"Thông tư 01 viện dẫn nghị định nào đã hết hiệu lực",
		"Dự thảo Luật Đất đai còn hiệu lực không",
		"dự thảo luật đất đai sửa đổi quy định gì về thu hồi đất, còn hiệu lực không",
		"trong dự thảo, điều 5 còn hiệu lực không",
		"kiểm tra chính tả trong phần căn cứ của văn bản này",
		"đánh giá căn cứ khoa học của dự thảo",
		"kiểm tra căn cứ tính thuế trong file này",
		// review round 2
		"kiểm tra các căn cứ xử phạt vi phạm giao thông",
		"các căn cứ ly hôn còn hiệu lực không",
		"đánh giá các căn cứ ly hôn theo luật hôn nhân gia đình",
		"kiểm tra các căn cứ chấm dứt hợp đồng lao động",
		"kiểm tra các căn cứ miễn trách nhiệm hình sự",
		"viện dẫn văn bản đã hết hiệu lực có vi phạm không",
		"khi văn bản được dẫn chiếu hết hiệu lực thì áp dụng văn bản nào",
		"văn bản viện dẫn văn bản hết hiệu lực thì xử lý sao",
		"kiểm tra hợp đồng trong file này còn hiệu lực không",
		"kiểm tra giấy phép trong file này còn hiệu lực không",
		"Quyết định này còn hiệu lực không?",
		// review round 3
		"các văn bản hết hiệu lực có được viện dẫn không",
		"có nên viện dẫn văn bản đã hết hiệu lực",
		"viện dẫn sai văn bản hết hiệu lực, trách nhiệm thuộc về ai",
		"tài liệu này nói gì về văn bản hết hiệu lực",
		"văn bản này làm hết hiệu lực những văn bản nào",
		"kiểm tra các căn cứ khởi kiện vụ án dân sự",
		"rà soát căn cứ cấp giấy phép xây dựng",
	}
	for _, q := range pure {
		if got := ClassifyLegalBasisRequest(q, 0); !got.Check || !got.Pure {
			t.Errorf("pure check %q: %+v", q, got)
		}
	}
	for _, q := range mixed {
		if got := ClassifyLegalBasisRequest(q, 0); !got.Check || got.Pure {
			t.Errorf("mixed check %q: %+v", q, got)
		}
	}
	for _, q := range no {
		if got := ClassifyLegalBasisRequest(q, 0); got.Check {
			t.Errorf("wrongly recognised: %q", q)
		}
	}
	// an @-mention points at a document as "văn bản này" does
	if !ClassifyLegalBasisRequest("@Tờ trình.docx còn hiệu lực không", 1).Check {
		t.Errorf("mention not read as the document")
	}
	if ClassifyLegalBasisRequest("Gửi @Tờ trình.docx, còn hiệu lực không", 0).Check {
		t.Errorf("a bare @ without a mention counted")
	}
}
