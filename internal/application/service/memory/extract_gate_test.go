package memory

import (
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestGateDropsOneOffDocumentRequests(t *testing.T) {
	for _, source := range []string{
		"Soát lỗi chính tả đoạn này",
		"Vẽ sơ đồ cho nội dung này",
		"Tóm tắt mục II (kết quả công tác văn thư) của văn bản",
		"Văn bản đang mở có mấy đoạn, đoạn đầu là gì? Trả lời ngắn.",
		"Viết lại mục 2 trong @ke_hoach_nguon.docx cho rõ nghĩa hơn",
		"Gửi văn bản này qua email cho Giám đốc Sở ngay bây giờ",
		"Viết lại đoạn này cho trang trọng hơn",
	} {
		for _, kind := range types.MemoryKinds[:4] {
			_, verdict := gateDecision(extractionDecision{Kind: kind, Content: "x"}, source)
			require.True(t, verdict.dropped(), "%s / %s -> %s", kind, source, verdict)
		}
	}
}

func TestGateKeepsStandingStatements(t *testing.T) {
	cases := []struct {
		kind, source string
	}{
		{types.MemoryKindProfile, "Tôi là chuyên viên văn phòng UBND xã, chủ yếu soạn công văn và tờ trình"},
		{types.MemoryKindPreference, "Từ giờ trả lời thẳng vào kết luận, đừng dẫn dắt dài dòng"},
		{types.MemoryKindPreference, "Luôn viết lại văn bản theo phong cách trang trọng"},
		{types.MemoryKindFact, "Bên tôi vừa chuyển từ phần mềm quản lý văn bản cũ sang hệ thống eOffice"},
		{types.MemoryKindPreference, "Đừng dùng từ Hán Việt khó hiểu"},
		// A statement about oneself stays durable even when the sentence goes
		// on to ask for something, or is phrased as a question.
		{types.MemoryKindProfile, "Tôi đang công tác tại Sở Tư pháp, đơn vị Phòng Hành chính, cần xem văn bản 30/2020"},
		{types.MemoryKindProfile, "Tôi đang công tác tại Sở Tư pháp, đơn vị Phòng Hành chính, cần xem văn bản C ở đâu?"},
		{types.MemoryKindProfile, "Em làm ở UBND phường Phú Hội, cho em hỏi thời hiệu xử phạt là bao lâu"},
	}
	for _, c := range cases {
		got, verdict := gateDecision(extractionDecision{Kind: c.kind, Content: "x", ExpiresAt: "null"}, c.source)
		require.Equal(t, gateKept, verdict, c.source)
		require.Equal(t, c.kind, got.Kind)
		require.False(t, got.Inferred)
	}
}

func TestGateTurnsQuestionsIntoGuessesOrCurrentWork(t *testing.T) {
	got, verdict := gateDecision(extractionDecision{Kind: types.MemoryKindProfile, Content: "Làm việc tại UBND xã"},
		"phaps luật về bhxh cho cán bộ ubnd xã quy định gì")
	require.Equal(t, gateMarkedInferred, verdict)
	require.True(t, got.Inferred)

	got, verdict = gateDecision(extractionDecision{Kind: types.MemoryKindFact, Content: "Đang tìm hiểu NĐ 53"},
		"Nghị định 53/2022/NĐ-CP quy định gì?")
	require.Equal(t, gateDemotedToTask, verdict)
	require.Equal(t, types.MemoryKindTask, got.Kind)
	require.NotNil(t, parseExpiry(got.ExpiresAt))

	_, verdict = gateDecision(extractionDecision{Kind: types.MemoryKindPreference, Content: "Quan tâm iPhone 18 Pro"},
		"iPhone 18 Pro ra mắt khi nào và giá bao nhiêu?")
	require.Equal(t, gateDropPreference, verdict)
}

func TestGateDemotesCommandsAndExpiresTasks(t *testing.T) {
	got, verdict := gateDecision(extractionDecision{Kind: types.MemoryKindFact, Content: "Tra cứu hồ sơ ưu tiên"},
		"tra cứu số lượng hồ sơ các loại ưu tiên")
	require.Equal(t, gateDemotedToTask, verdict)
	require.Equal(t, types.MemoryKindTask, got.Kind)

	_, verdict = gateDecision(extractionDecision{Kind: types.MemoryKindPreference, Content: "Kiểm tra chính tả"},
		"Kiểm tra lỗi chính tả trong văn bản được cung cấp")
	require.True(t, verdict.dropped())

	_, verdict = gateDecision(extractionDecision{Kind: types.MemoryKindTask, Content: "Soạn công văn gửi Sở Nội vụ"},
		"Soạn giúp tôi công văn gửi Sở Nội vụ về kế hoạch tập huấn")
	require.Equal(t, gateDropDocument, verdict, "drafting one document is this turn's work")

	got, verdict = gateDecision(extractionDecision{Kind: types.MemoryKindTask, Content: "Chuẩn bị hồ sơ thi đua"},
		"Chuẩn bị hồ sơ thi đua khen thưởng cuối năm cho đơn vị")
	require.Equal(t, gateTaskGivenExpiry, verdict)
	expiry := parseExpiry(got.ExpiresAt)
	require.NotNil(t, expiry)
	require.WithinDuration(t, time.Now().Add(gateTaskTTL), *expiry, 36*time.Hour)

	got, verdict = gateDecision(extractionDecision{Kind: types.MemoryKindTask, Content: "x", ExpiresAt: "2099-01-01"},
		"Tuần này phải xong báo cáo tổng kết công tác cải cách hành chính")
	require.Equal(t, gateKept, verdict)
	require.Equal(t, "2099-01-01", got.ExpiresAt)
}

func TestTopicsNameDifferentNumbers(t *testing.T) {
	require.True(t, types.TopicsNameDifferentNumbers("nội dung nghị định 53/2022/NĐ-CP", "nội dung nghị định 30/2020"))
	require.True(t, types.TopicsNameDifferentNumbers("luật bí mật nhà nước 2025", "luật bí mật nhà nước 2018"))
	require.False(t, types.TopicsNameDifferentNumbers("nghị định 30/2020", "sửa đổi nghị định 30"))
	require.False(t, types.TopicsNameDifferentNumbers("kiểm tra hiệu lực thông tư 01", "kiểm tra hiệu lực văn bản"))
	require.False(t, types.TopicsNameDifferentNumbers("soạn thảo văn bản", "viết lại văn bản"))
}

func TestTopicRecursNeedsSessionsAndDays(t *testing.T) {
	stat := &types.MemoryTopicStat{Hits: 40, SessionHits: 1, Days: []string{"2026-10-16"}}
	require.False(t, types.TopicRecurs(stat, 3))
	stat.SessionHits = 3
	require.False(t, types.TopicRecurs(stat, 3), "three sessions in one afternoon")
	stat.Days = append(stat.Days, "2026-10-17")
	require.True(t, types.TopicRecurs(stat, 3))
}

func TestActionLabelsAreNotSubjects(t *testing.T) {
	for _, label := range []string{
		"tóm tắt văn bản", "kiểm tra lỗi chính tả", "hỏi nội dung ảnh", "vẽ sơ đồ quy trình",
		"viết lại câu ngắn gọn", "chèn nội dung quy trình", "gửi văn bản cho Giám đốc",
		"soạn thảo văn bản", "kiểm tra thể thức văn bản", "tra cứu nghị định 127/2018", "Tóm tắt",
	} {
		require.True(t, topicIsAnAction(label), label)
	}
	for _, label := range []string{
		"quy định BHXH cán bộ xã", "luật đất đai", "an toàn thực phẩm", "phân bổ nhà ở xã hội",
		"đối tượng ưu tiên nhà ở", "thể thức văn bản hành chính", "xử phạt vi phạm giao thông",
		"hiệu lực văn bản quy phạm pháp luật", "cấp phép xây dựng", "khiếu nại quyết định hành chính",
	} {
		require.False(t, topicIsAnAction(label), label)
	}
}
