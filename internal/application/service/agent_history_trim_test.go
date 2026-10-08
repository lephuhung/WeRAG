package service

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	agenttoken "github.com/Tencent/WeKnora/internal/agent/token"
	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// attachmentTurns is three completed turns: u1 and u3 carry an attachment
// with content, u2 none.
func attachmentTurns() []*types.Message {
	rows := storedTurns(3)
	rows[0].Attachments = types.MessageAttachments{{
		ID: "att-1", FileName: "bao-cao.pdf", FileType: ".pdf", FileSize: 4096,
		ContentMode: "selected_chunks", Content: "NỘI DUNG CŨ " + strings.Repeat("dữ liệu ", 200),
	}}
	rows[4].Attachments = types.MessageAttachments{{
		ID: "att-3", FileName: "to-trinh.docx", FileType: ".docx", FileSize: 2048,
		ContentMode: "full", Content: "NỘI DUNG MỚI",
	}}
	return rows
}

func userContents(msgs []chat.Message) []string {
	var out []string
	for _, m := range msgs {
		if m.Role == "user" {
			out = append(out, m.Content)
		}
	}
	return out
}

func TestHistoryReplaysAttachmentContentOnlyOnTheNewestAttachmentTurn(t *testing.T) {
	rows := attachmentTurns()
	repo := &historyRepo{rows: rows}
	got, _, err := LoadAgentHistory(context.Background(), repo, "s1", unlimitedBudget, false)
	require.NoError(t, err)
	users := userContents(got)
	require.Len(t, users, 3)

	older := users[0]
	assert.NotContains(t, older, "NỘI DUNG CŨ")
	assert.Contains(t, older, `name="bao-cao.pdf"`)
	assert.Contains(t, older, "<type>.pdf</type>")
	assert.Contains(t, older, "<size_kb>4.00</size_kb>")
	assert.Contains(t, older, "<content_mode>selected_chunks</content_mode>")
	assert.Contains(t, older, "<status>omitted_in_history</status>")
	assert.Contains(t, older, "Nội dung tệp đã được hiển thị ở lượt trước và không lặp lại")
	assert.NotContains(t, older, "extraction failed")

	assert.Contains(t, users[2], "NỘI DUNG MỚI")
	assert.NotContains(t, users[2], "omitted_in_history")

	// what is stored is unchanged
	assert.Contains(t, rows[0].Attachments[0].Content, "NỘI DUNG CŨ")
	assert.Nil(t, rows[0].Attachments[0].HistoryOmission)
}

func TestHistoryKeepsTheOnlyAttachmentTurnsContent(t *testing.T) {
	rows := attachmentTurns()
	rows[4].Attachments = nil // the newest attachment turn is now turn 1
	got, _, err := LoadAgentHistory(context.Background(), &historyRepo{rows: rows}, "s1", unlimitedBudget, false)
	require.NoError(t, err)
	assert.Contains(t, userContents(got)[0], "NỘI DUNG CŨ")
}

func TestHistoryKeepsAnOlderAttachmentsFailureNote(t *testing.T) {
	rows := attachmentTurns()
	rows[0].Attachments = append(rows[0].Attachments,
		types.MessageAttachment{ID: "att-2", FileName: "scan.png", ParseError: "OCR timed out"},
		types.MessageAttachment{ID: "att-x", FileName: "empty.txt"})
	got, _, err := LoadAgentHistory(context.Background(), &historyRepo{rows: rows}, "s1", unlimitedBudget, false)
	require.NoError(t, err)
	older := userContents(got)[0]
	assert.Contains(t, older, "<error>OCR timed out</error>")
	// an empty attachment was never shown: its note stays its own
	assert.Equal(t, 1, strings.Count(older, "omitted_in_history"))
	assert.Contains(t, older, "extraction failed")
}

func TestHistoryNamesTheSessionDocumentOfAnOlderAttachment(t *testing.T) {
	rows := attachmentTurns()
	docs := []*types.DocumentWorkspace{
		{ID: "w-1", AttachmentID: "att-1", Position: 2, Role: types.DocumentWorkspaceRoleSource},
	}
	got, _, err := LoadAgentHistory(context.Background(), &historyRepo{rows: rows}, "s1", unlimitedBudget, false,
		WithHistoryDocuments(docs))
	require.NoError(t, err)
	older := userContents(got)[0]
	assert.Contains(t, older, "tài liệu nguồn vb2")
	assert.Contains(t, older, "find_in_documents (document=vb2)")
	assert.NotContains(t, older, "NỘI DUNG CŨ")
}

func TestHistoryNeverReplaysAnAttachmentOpenAsATab(t *testing.T) {
	rows := attachmentTurns()
	docs := []*types.DocumentWorkspace{
		{ID: "w-3", AttachmentID: "att-3", Position: 1, Role: types.DocumentWorkspaceRoleTarget},
	}
	got, _, err := LoadAgentHistory(context.Background(), &historyRepo{rows: rows}, "s1", unlimitedBudget, false,
		WithHistoryDocuments(docs))
	require.NoError(t, err)
	users := userContents(got)
	// the tab brings its own text; the newest upload with content is turn 1
	assert.NotContains(t, users[2], "NỘI DUNG MỚI")
	assert.Contains(t, users[2], "tab soạn thảo vb1")
	assert.Contains(t, users[0], "NỘI DUNG CŨ")
}

func TestHistoryTrimLetsAnOlderAttachmentTurnFit(t *testing.T) {
	rows := attachmentTurns()
	rows[0].Attachments[0].Content = strings.Repeat("dữ liệu dài ", 4000)
	// the budget fits all three turns only without turn 1's content
	got, _, err := LoadAgentHistory(context.Background(), &historyRepo{rows: rows}, "s1", 1500, false)
	require.NoError(t, err)
	assert.Len(t, userContents(got), 3)
}

// outlineStep is a past read_document_outline call with a long listing.
func outlineStep() types.AgentStep {
	return types.AgentStep{ToolCalls: []types.ToolCall{{
		ID: "call-1", Name: agenttools.ToolReadDocumentOutline, Args: map[string]interface{}{"document": "vb1"},
		Result: &types.ToolResult{
			Success: true,
			Output:  "# Dàn ý tài liệu A.docx\n" + strings.Repeat("[1] đoạn văn bản dài | nội dung\n", 200),
			Data: map[string]interface{}{"file_name": "A.docx", "paragraph_count": 300.0,
				"from": 0.0, "to": 200.0},
		},
	}}}
}

func TestHistoryReplaysAPastOutlineAsASummary(t *testing.T) {
	rows := storedTurns(2)
	rows[1].AgentSteps = []types.AgentStep{outlineStep()}
	got, _, err := LoadAgentHistory(context.Background(), &historyRepo{rows: rows}, "s1", unlimitedBudget, false)
	require.NoError(t, err)
	var tool string
	for _, m := range got {
		if m.Role == "tool" {
			tool = m.Content
		}
	}
	assert.Equal(t, "Đã đọc đoạn 0–199 (200 đoạn) của vb1 · A.docx; tài liệu có 300 đoạn "+
		"(nội dung không lưu trong lịch sử; đọc lại bằng read_document_outline khi cần).", tool)
	// the stored step keeps the listing
	assert.Contains(t, rows[1].AgentSteps[0].ToolCalls[0].Result.Output, "[1] đoạn văn bản dài")
}

func TestHistoryTrimTotalsCountWhatTheReplayLeftOut(t *testing.T) {
	rows := attachmentTurns()
	rows[3].AgentSteps = []types.AgentStep{outlineStep()} // turn 2's answer
	est, err := agenttoken.NewEstimator()
	require.NoError(t, err)
	replay := newHistoryReplay(est, unlimitedBudget, false)
	slim := make([]*types.Message, 0, len(rows))
	for _, m := range rows {
		slim = append(slim, replay.track(m))
	}
	turns, _ := replay.newestWithin(completeHistoryTurns(slim, true))
	totals := replay.trimTotals(turns)

	assert.Equal(t, 1, totals.attachmentTurns)
	full := utf8.RuneCountInString(rows[0].Attachments.BuildPrompt())
	assert.Greater(t, totals.attachmentRunes, full/2)
	assert.Less(t, totals.attachmentRunes, full)
	listing := utf8.RuneCountInString(rows[3].AgentSteps[0].ToolCalls[0].Result.Output)
	assert.Greater(t, totals.toolRunes, listing*9/10)
	assert.Less(t, totals.toolRunes, listing)
}
