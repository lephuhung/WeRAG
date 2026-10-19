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
	}
	assert.Equal(t, 30, n)
}

func TestScopeNoticeText(t *testing.T) {
	src := opcSource(t)
	got := src.ScopeNotice("Tôi muốn làm HAI SỔ kế toán, và công ty đang bị kiện.")
	assert.Equal(t, ScopeNoticeMarker+
		"- Nội dung này nằm ngoài phạm vi cẩm nang (tranh chấp, tố tụng); hãy hỏi luật sư hoặc người có chứng chỉ hành nghề trước khi làm hồ sơ.\n"+
		"- Hệ thống không hỗ trợ lập hai sổ kế toán hoặc trốn thuế; hành vi này có thể bị xử lý hành chính hoặc hình sự. "+
		"Phương án hợp pháp: kê khai đủ doanh thu, dùng chi phí hợp lệ có chứng từ và chọn phương pháp tính thuế phù hợp. "+
		"Nên hỏi luật sư hoặc kế toán hành nghề.\n", got)
	assert.NotEmpty(t, src.ScopeNotice(norm.NFD.String("Công ty sắp PHÁ SẢN")), "NFD and case")
	assert.Empty(t, src.ScopeNotice("Thủ tục phá sảnx"), "word boundary")
	assert.Empty(t, src.ScopeNotice(""))
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
