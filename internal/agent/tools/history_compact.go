package tools

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/types"
)

// The document tools' outputs (a format report, an outline listing up to 200
// paragraphs, spelling findings, edit plans) serve the turn that ran them:
// the engine reads them live and the answer restates what matters. Replayed
// verbatim they would stay in every later turn's context, so a past turn
// replays a short summary built from the result's Data instead. This is for
// the history replay only (service.LoadAgentHistory): what is stored
// (SanitizeAgentStepsForStorage) and streamed (StreamContentForToolResult)
// is unchanged.

// historyCompactMaxRunes caps one summarized tool message.
const historyCompactMaxRunes = 800

// historyCompactFallbackRunes is how much of the raw output a summary keeps
// when Data lacks what its rule reads.
const historyCompactFallbackRunes = 600

// historyListItems is how many findings or changes a summary lists.
const historyListItems = 5

// historyCompactRule summarizes one tool's successful result from its Data,
// or from the head of its output when Data lacks what the rule reads; ""
// keeps the output as it is.
type historyCompactRule func(data map[string]interface{}, args map[string]interface{}, output string) string

// documentToolHistoryRules are the tools whose output a past turn replays as
// a summary. find_in_documents is not here: its stored output is already one
// line (compactToolSummary, display_type document_search).
var documentToolHistoryRules = map[string]historyCompactRule{
	ToolCheckDocumentFormat: compactFormatCheckHistory,
	ToolReadDocumentOutline: compactOutlineHistory,
	ToolCheckSpelling:       compactSpellingHistory,
	ToolApplyFormatFixes:    compactFormatFixesHistory,
	ToolRewriteParagraphs:   compactRewriteHistory,
	ToolInsertParagraphs:    compactInsertHistory,
	ToolMarkPassages:        compactMarkHistory,
}

// HistoryToolOutput is the tool message a past turn replays: the stored
// output as CompactToolOutputForHistory rebuilds it, and for the document
// tools a short summary of it (documentToolHistoryRules). A failure, or a
// summary no shorter than the output, keeps the output.
func HistoryToolOutput(toolName string, args map[string]interface{}, result *types.ToolResult) string {
	out := CompactToolOutputForHistory(toolName, result)
	rule, ok := documentToolHistoryRules[toolName]
	if !ok || result == nil || !result.Success {
		return out
	}
	summary := strings.TrimSpace(rule(genericData(result.Data), args, result.Output))
	if summary == "" {
		return out
	}
	summary = clipRunes(summary, historyCompactMaxRunes)
	if utf8.RuneCountInString(summary) >= utf8.RuneCountInString(out) {
		return out
	}
	return summary
}

// genericData is data as it reads back from storage (JSON numbers, maps and
// slices), so a rule reads the live result and a stored one alike.
func genericData(data map[string]interface{}) map[string]interface{} {
	if data == nil {
		return nil
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return nil
	}
	var out map[string]interface{}
	if json.Unmarshal(raw, &out) != nil {
		return nil
	}
	return out
}

func listField(data map[string]interface{}, key string) []map[string]interface{} {
	items, _ := data[key].([]interface{})
	out := make([]map[string]interface{}, 0, len(items))
	for _, it := range items {
		if m, ok := it.(map[string]interface{}); ok {
			out = append(out, m)
		}
	}
	return out
}

func hasField(data map[string]interface{}, key string) bool {
	_, ok := data[key]
	return ok
}

var documentHandlePattern = regexp.MustCompile(`^vb\d+$`)

// historyDocumentName names the document a result is about: its handle (from
// the "document" label of an edit result, else the call's document
// argument) and its file name.
func historyDocumentName(data, args map[string]interface{}) string {
	handle := ""
	if label := stringField(data, "document"); label != "" {
		handle, _, _ = strings.Cut(label, " · ")
	}
	if handle == "" {
		if arg := strings.ToLower(stringField(args, "document")); documentHandlePattern.MatchString(arg) {
			handle = arg
		}
	}
	name := stringField(data, "file_name")
	switch {
	case handle != "" && name != "":
		return handle + " · " + name
	case handle != "":
		return handle
	case name != "":
		return name
	}
	return "tài liệu"
}

// historyHead is the start of output, cut at a line end when one is near.
func historyHead(output string, n int) string {
	output = strings.TrimSpace(output)
	if utf8.RuneCountInString(output) <= n {
		return output
	}
	head := string([]rune(output)[:n])
	if i := strings.LastIndex(head, "\n"); i > len(head)/2 {
		head = head[:i]
	}
	return strings.TrimSpace(head) + "\n…"
}

// firstLine is the first non-empty line of output.
func firstLine(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

func compactFormatCheckHistory(data, args map[string]interface{}, output string) string {
	const note = "\n(báo cáo đầy đủ không lưu trong lịch sử; gọi lại check_document_format khi cần, kết quả đã lưu đệm)"
	summary, _ := data["summary"].(map[string]interface{})
	if summary == nil {
		return "Kết quả kiểm tra thể thức (rút gọn):\n" + historyHead(output, historyCompactFallbackRunes) + note
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Đã kiểm tra thể thức %s", historyDocumentName(data, args))
	if dt, _ := data["document_type"].(map[string]interface{}); dt != nil {
		if used := stringField(dt, "used"); used != "" {
			fmt.Fprintf(&b, " (loại %s)", used)
		}
	}
	fmt.Fprintf(&b, ": %d phát hiện không đạt, %d cảnh báo, %d đạt",
		intField(summary, "fail"), intField(summary, "warn"), intField(summary, "pass"))
	if evaluated, ok := data["evaluated"].(bool); ok && !evaluated {
		b.WriteString(" (chưa thẩm định)")
	}
	b.WriteString(".")
	if head := firstLine(output); head != "" {
		b.WriteString("\n" + clipRunes(head, 200))
	}
	b.WriteString(note)
	return b.String()
}

func compactOutlineHistory(data, args map[string]interface{}, output string) string {
	if !hasField(data, "from") || !hasField(data, "to") {
		return historyHead(output, historyCompactFallbackRunes)
	}
	from, to := intField(data, "from"), intField(data, "to")
	name := historyDocumentName(data, args)
	if stringField(data, "role") == types.DocumentWorkspaceRoleSource {
		unit := stringField(data, "unit")
		if unit == "" {
			unit = "phần"
		}
		if to <= from {
			return fmt.Sprintf("Đã đọc tài liệu nguồn %s từ %s %d: không có nội dung.", name, unit, from)
		}
		return fmt.Sprintf("Đã đọc %s %d–%d (%d %s) của tài liệu nguồn %s; tài liệu có %d %s "+
			"(nội dung không lưu trong lịch sử; đọc lại bằng read_document_outline khi cần).",
			unit, from, to-1, to-from, unit, name, intField(data, "part_count"), unit)
	}
	if to <= from {
		return fmt.Sprintf("Đã đọc dàn ý %s từ đoạn %d: không có đoạn nào.", name, from)
	}
	return fmt.Sprintf("Đã đọc đoạn %d–%d (%d đoạn) của %s; tài liệu có %d đoạn "+
		"(nội dung không lưu trong lịch sử; đọc lại bằng read_document_outline khi cần).",
		from, to-1, to-from, name, intField(data, "paragraph_count"))
}

func compactSpellingHistory(data, args map[string]interface{}, output string) string {
	if !hasField(data, "found") {
		return historyHead(output, historyCompactFallbackRunes)
	}
	found := intField(data, "found")
	name := historyDocumentName(data, args)
	if found == 0 {
		return fmt.Sprintf("Đã kiểm tra chính tả %s: không phát hiện lỗi.", name)
	}
	marked := 0
	if m, _ := data["marked"].(bool); m {
		marked = len(listField(data, "document_ops"))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Đã kiểm tra chính tả %s: %d lỗi chính tả, đã đánh dấu %d", name, found, marked)
	findings := listField(data, "findings")
	for i, f := range findings {
		if i == historyListItems {
			fmt.Fprintf(&b, "\n… và %d lỗi khác", len(findings)-i)
			break
		}
		fmt.Fprintf(&b, "\n- Đoạn [%d]: “%s” → “%s”", intField(f, "paragraph"),
			clipRunes(stringField(f, "wrong"), 60), clipRunes(stringField(f, "correct"), 60))
	}
	if hasField(data, "next_from") {
		fmt.Fprintf(&b, "\nCòn các đoạn sau, từ from=%d.", intField(data, "next_from"))
	}
	b.WriteString("\n(chi tiết không lưu trong lịch sử)")
	return b.String()
}

// compactEditsHistory is the summary of an edit tool: the changes applied to
// the document, one line each up to historyListItems.
func compactEditsHistory(data, args map[string]interface{}, n int, lines []string, failed int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Đã áp dụng %d thay đổi trên %s", n, historyDocumentName(data, args))
	if failed > 0 {
		fmt.Fprintf(&b, " (%d chỗ không thực hiện được)", failed)
	}
	for i, line := range lines {
		if i == historyListItems {
			fmt.Fprintf(&b, "\n… và %d thay đổi khác", len(lines)-i)
			break
		}
		b.WriteString("\n- " + line)
	}
	b.WriteString("\n(chi tiết không lưu trong lịch sử)")
	return b.String()
}

func compactFormatFixesHistory(data, args map[string]interface{}, output string) string {
	if !hasField(data, "applied") {
		return historyHead(output, historyCompactFallbackRunes)
	}
	applied := listField(data, "applied")
	lines := make([]string, 0, len(applied))
	for _, a := range applied {
		lines = append(lines, fmt.Sprintf("%s: %s", stringField(a, "check_id"), clipRunes(stringField(a, "change"), 100)))
	}
	dryRun, _ := data["dry_run"].(bool)
	blocked, _ := data["blocked"].(bool)
	if dryRun || blocked {
		why := "chạy thử"
		if blocked {
			why = "cấu trúc văn bản chưa nhận diện chắc chắn"
		}
		s := compactEditsHistory(data, args, len(applied), lines, 0)
		return strings.Replace(s, "Đã áp dụng", fmt.Sprintf("Kế hoạch (chưa áp dụng, %s):", why), 1)
	}
	s := compactEditsHistory(data, args, len(applied), lines, 0)
	if manual := len(listField(data, "manual")); manual > 0 {
		s += fmt.Sprintf("\n%d mục cần sửa tay.", manual)
	}
	return s
}

func compactRewriteHistory(data, args map[string]interface{}, output string) string {
	if !hasField(data, "changes") {
		return historyHead(output, historyCompactFallbackRunes)
	}
	var lines []string
	for _, c := range listField(data, "changes") {
		if stringField(c, "status") == "planned" {
			lines = append(lines, fmt.Sprintf("Đoạn [%d]: “%s” → “%s”", intField(c, "paragraph"),
				clipRunes(stringField(c, "old"), 60), clipRunes(stringField(c, "new"), 60)))
		}
	}
	return compactEditsHistory(data, args, intField(data, "planned"), lines, intField(data, "failed"))
}

func compactInsertHistory(data, args map[string]interface{}, output string) string {
	if !hasField(data, "changes") {
		return historyHead(output, historyCompactFallbackRunes)
	}
	var lines []string
	for _, c := range listField(data, "changes") {
		if stringField(c, "status") != "planned" {
			continue
		}
		where := fmt.Sprintf("sau đoạn [%d]", intField(c, "after"))
		if intField(c, "after") < 0 {
			where = "ở đầu văn bản"
		}
		lines = append(lines, fmt.Sprintf("Chèn %s: “%s”", where,
			clipRunes(strings.ReplaceAll(stringField(c, "text"), "\n", " ↵ "), 80)))
	}
	return compactEditsHistory(data, args, intField(data, "planned"), lines, intField(data, "failed"))
}

func compactMarkHistory(data, args map[string]interface{}, output string) string {
	if !hasField(data, "marks") {
		return historyHead(output, historyCompactFallbackRunes)
	}
	var lines []string
	for _, m := range listField(data, "marks") {
		if stringField(m, "status") != "planned" {
			continue
		}
		line := fmt.Sprintf("Đánh dấu đoạn [%d]", intField(m, "paragraph"))
		if text := stringField(m, "text"); text != "" {
			line += fmt.Sprintf(" “%s”", clipRunes(text, 60))
		}
		lines = append(lines, line+": "+clipRunes(stringField(m, "reason"), 80))
	}
	return compactEditsHistory(data, args, intField(data, "planned"), lines, intField(data, "failed"))
}
