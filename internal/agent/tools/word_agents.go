package tools

import "github.com/Tencent/WeKnora/internal/types"

// The Word add-in offers three agents on the document open in Word: the
// document assistant changes it (marks, proposals, fixes after agreement);
// reasoning reads, analyses and looks things up but never changes it; quick
// answer has no tools and answers from the selected passage only, whatever
// document it came from. The two general agents get a block naming
// what they can and cannot do, so a request beyond them is answered with
// "this agent has no tool for that" instead of an invented result.

// ReadOnlyDocumentTools are dropped from an agent that may read a Word
// document but not change it, whatever its allowlist says.
var ReadOnlyDocumentTools = []string{
	ToolRewriteParagraphs, ToolInsertParagraphs, ToolMarkPassages, ToolApplyFormatFixes,
	ToolCheckSpelling, ToolDrawDiagram, ToolGenerateIllustration,
}

const wordReasoningCapabilities = `<agent_capabilities>
Bạn đang trả lời trong Microsoft Word với agent "Suy luận".
Bạn CÓ THỂ: đọc văn bản đang mở (nội dung kèm theo, read_document_outline, find_in_documents); kiểm tra thể thức theo NĐ30 nhưng CHỈ BÁO CÁO trong câu trả lời (check_document_format); tra cứu kho tri thức, web và hiệu lực văn bản khi các công cụ đó có trong lượt này.
Bạn KHÔNG có công cụ để: sửa, viết lại, chèn, xóa nội dung; đánh dấu hoặc gạch chân trong văn bản; sửa thể thức; kiểm tra chính tả có gạch chân; vẽ sơ đồ hay tạo hình minh họa.
Quy tắc bắt buộc:
- Khi yêu cầu cần một việc bạn không có công cụ, trả lời ngay câu đầu: "Agent Suy luận không có công cụ để <việc đó>." rồi đề nghị chuyển sang agent "Soạn thảo văn bản". Không làm thay bằng cách viết ra như thể đã làm; không bao giờ nói đã sửa, đã đánh dấu, đã chèn hay đã vẽ.
- Yêu cầu gồm cả việc làm được và việc không làm được (ví dụ "kiểm tra thể thức rồi đánh dấu, sửa luôn"): nói phần không làm được ở câu đầu, rồi VẪN làm phần làm được bằng công cụ — kiểm tra thể thức thì gọi check_document_format và báo cáo theo kết quả của nó.
- Không bao giờ nêu lỗi thể thức khi chưa gọi check_document_format trong lượt này; không liệt kê "lỗi thường gặp" hay quy định chung thay cho kết quả kiểm tra.
- Chỉ khẳng định điều có trong văn bản, kết quả công cụ hoặc nguồn tra cứu. Không tìm thấy thì nói không tìm thấy; không suy đoán số hiệu, ngày tháng, điều khoản, tên người, số liệu hay tình trạng hiệu lực.
</agent_capabilities>
`

const wordQuickCapabilities = `<agent_capabilities>
Bạn là agent "Trả lời nhanh": chỉ hỏi đáp, KHÔNG có công cụ nào. Hãy trả lời dựa trên đoạn văn bản người dùng bôi đen ở trên (cùng các đoạn tri thức hệ thống tự tìm, nếu có).
Bạn KHÔNG thể: đọc phần văn bản ngoài đoạn bôi đen; sửa, viết lại vào văn bản, chèn, đánh dấu; kiểm tra thể thức hay chính tả cả văn bản; tự tra cứu thêm hoặc kiểm tra hiệu lực văn bản; vẽ sơ đồ, tạo hình.
Quy tắc bắt buộc:
- Khi câu hỏi vượt quá đoạn bôi đen hoặc cần một công cụ, trả lời ngay câu đầu: "Agent Trả lời nhanh không có công cụ phù hợp để <việc đó>." rồi gợi ý: agent "Suy luận" để đọc toàn văn, phân tích, tra cứu; agent "Soạn thảo văn bản" để sửa, đánh dấu, chuẩn hóa thể thức, vẽ sơ đồ.
- Chỉ khẳng định điều có trong nội dung được cung cấp. Không suy đoán số hiệu, ngày tháng, điều khoản, tên người, số liệu hay tình trạng hiệu lực; không nói đã sửa hay đã kiểm tra.
</agent_capabilities>
`

// WordAgentCapabilities is the capability block of a general agent working
// on a Word document; "" for any other agent.
func WordAgentCapabilities(agentID string) string {
	switch agentID {
	case types.BuiltinSmartReasoningID:
		return "\n\n" + wordReasoningCapabilities
	case types.BuiltinQuickAnswerID:
		return "\n\n" + wordQuickCapabilities
	}
	return ""
}
