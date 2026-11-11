package domainpacks

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/unicode/norm"
)

// Of the pack's 30 eval prompts, only the trốn-nợ question, the refusals
// and the out-of-scope ones may get a scope notice.
func TestOpcScopeMatchesOnlyTheIntendedEvalPrompts(t *testing.T) {
	src := opcSource(t)
	f, err := os.Open("../../../domain-packs/opc-vn-business-law/evals/cases.jsonl")
	require.NoError(t, err)
	defer f.Close()
	want := map[string]bool{"orig-6": true, "refusal-1": true, "refusal-2": true, "refusal-3": true,
		"oos-1": true, "oos-2": true, "oos-3": true}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	n := 0
	for scanner.Scan() {
		var c struct{ ID, Prompt string }
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &c))
		n++
		assert.Equal(t, want[c.ID], src.ScopeNotice(c.Prompt) != "", "%s: %q", c.ID, c.Prompt)
		assert.Equal(t, want[c.ID], src.ScopeBrief(c.Prompt) != "", "brief %s", c.ID)
	}
	assert.Equal(t, 32, n)
}

func TestScopeNoticeText(t *testing.T) {
	src := opcSource(t)
	got := src.ScopeNotice("Tôi muốn làm HAI SỔ KẾ TOÁN, và công ty đang bị kiện.")
	assert.Equal(t, ScopeNoticeMarker+
		"- Nội dung này nằm ngoài phạm vi cẩm nang (tranh chấp, tố tụng); hãy hỏi luật sư hoặc người có chứng chỉ hành nghề trước khi làm hồ sơ.\n"+
		"- Hệ thống không hỗ trợ lập hai sổ kế toán hoặc trốn thuế; hành vi này có thể bị xử lý hành chính hoặc hình sự. "+
		"Phương án hợp pháp: kê khai đủ doanh thu, dùng chi phí hợp lệ có chứng từ và chọn phương pháp tính thuế phù hợp. "+
		"Nên hỏi luật sư hoặc kế toán hành nghề.\n", got)
	assert.NotEmpty(t, src.ScopeNotice(norm.NFD.String("Công ty sắp PHÁ SẢN")), "NFD and case")
	assert.Empty(t, src.ScopeNotice("Thủ tục phá sảnx"), "word boundary")
	assert.Empty(t, src.ScopeNotice(""))
	// everyday questions that must not match
	for _, q := range []string{
		"Tôi có hai sổ tiết kiệm ở hai ngân hàng, có cần khai không?",
		"Nhà tôi có hai sổ đỏ, chuyển một sổ vào công ty thế nào?",
		"Công ty sáp nhập phòng ban thì có phải đăng ký thay đổi không?",
		"Thủ tục góp vốn bằng tiền vào công ty TNHH một thành viên?",
		"Tôi muốn mở sổ kế toán cho công ty mới.",
		"Kiện toàn bộ máy kế toán thế nào cho doanh nghiệp siêu nhỏ?",
		"Đối tác hỏi về thanh toán chậm, tôi trả lời sao?",
	} {
		assert.Empty(t, src.ScopeNotice(q), q)
	}
	assert.Equal(t, "x", StripFooter("x"+ScopeNoticeMarker+"- a\n"+types.ToolAnswerBlockMarker+"b"))
}

func TestParseScopeRules(t *testing.T) {
	_, errs := parseScope([]byte(`exclusions:
  - name: a
    phrases: [abc, phá sản]
    alternative: x
refusals:
  - name: b
    phrases: [Phá Sản]
  - name: ""
    phrases: []
`))
	msg := ""
	for _, e := range errs {
		msg += e.Error() + "\n"
	}
	for _, want := range []string{`"abc" is shorter than 4`, "alternative is only for refusals",
		`phrase "Phá Sản" is also in exclusions[0]`, "alternative is required for a refusal", "name is required",
		"phrases are required"} {
		assert.Contains(t, msg, want)
	}
	_, errs = parseScope([]byte("exclusion: []\n"))
	require.NotEmpty(t, errs)
	assert.True(t, strings.Contains(errs[0].Error(), "field exclusion not found"))
}

// The brief matches exactly what the notice matches and tells the model the
// matched topics before it answers.
func TestScopeBrief(t *testing.T) {
	src := opcSource(t)
	assert.Empty(t, src.ScopeBrief("Thủ tục góp vốn bằng tiền vào công ty TNHH một thành viên?"))
	assert.Empty(t, src.ScopeBrief(""))

	got := src.ScopeBrief("Tôi muốn làm HAI SỔ KẾ TOÁN, và công ty đang bị kiện.")
	assert.True(t, strings.HasPrefix(got, ScopeBriefMarker), got)
	assert.Contains(t, got, "ngoài phạm vi cẩm nang: tranh chấp, tố tụng. Nếu đúng vậy")
	assert.Contains(t, got, "Nếu câu hỏi đề nghị giúp lập hai sổ kế toán hoặc trốn thuế thì từ chối")
	assert.Contains(t, got, "(kê khai đủ doanh thu, dùng chi phí hợp lệ có chứng từ và chọn phương pháp tính thuế phù hợp)")
	assert.Contains(t, got, "nếu chỉ hỏi cách làm đúng luật thì trả lời bình thường")
	assert.Contains(t, got, "Với phần ngoài phạm vi hoặc bị từ chối, chỉ tra cứu 1–2 lần")
	assert.Contains(t, got, "phần còn lại của câu hỏi (nếu có) vẫn tra cứu và trả lời như thường")

	only := src.ScopeBrief("Công ty tôi mất khả năng thanh toán, có nên nộp đơn phá sản không?")
	assert.Contains(t, only, "ngoài phạm vi cẩm nang: phá sản")
	assert.NotContains(t, only, "thì từ chối")
}
