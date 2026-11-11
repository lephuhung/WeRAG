package tools

import "testing"

func TestIsLegalBasisCheckRequest(t *testing.T) {
	yes := []string{
		"Rà soát căn cứ pháp lý",
		"rà soát căn cứ",
		"Rà soát căn cứ pháp lý giúp tôi nhé",
		"Hãy rà soát căn cứ pháp lý của văn bản này",
		"Kiểm tra căn cứ pháp lý của dự thảo",
		"Soát lại căn cứ trong vb2",
		"Kiểm tra giúp tôi các căn cứ",
		"kiểm tra văn bản này còn hiệu lực không",
		"Kiểm tra văn bản này còn hiệu lực không?",
		"Các văn bản viện dẫn còn hiệu lực không?",
		"các văn bản được viện dẫn có còn hiệu lực không",
		"Kiểm tra hiệu lực các văn bản được viện dẫn",
		"Có văn bản nào được viện dẫn đã hết hiệu lực không?",
		"Văn bản được dẫn chiếu trong tờ trình còn hiệu lực?",
		"Các căn cứ của dự thảo còn hiệu lực không",
		"Các căn cứ pháp lý còn hiệu lực không?",
		"Tra căn cứ pháp lý được dẫn trong đoạn này và kiểm tra còn hiệu lực không",
		"Thông tư 01/2011/TT-BNV được viện dẫn trong văn bản này còn hiệu lực không?",
		"Đối chiếu căn cứ ban hành",
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
	}
	for _, q := range yes {
		if !IsLegalBasisCheckRequest(q) {
			t.Errorf("not recognised: %q", q)
		}
	}
	for _, q := range no {
		if IsLegalBasisCheckRequest(q) {
			t.Errorf("wrongly recognised: %q", q)
		}
	}
}
