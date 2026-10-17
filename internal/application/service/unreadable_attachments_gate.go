package service

import (
	"context"
	"strings"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// UnreadableAttachmentsToolName names the pipeline card that ends a turn
// whose files could not be read.
const UnreadableAttachmentsToolName = "unreadable_attachments"

// unreadableAttachmentsAnswer ends a chat turn whose every attached file
// failed to yield text (parse failure, unreadable scan, a photo without
// text) and whose images do not reach the model either, with a fixed
// answer naming each file and the cause. No model runs and no tool is
// called: the prompt would carry nothing of the files, and the model would
// answer from the session history instead, as it did when the history
// still held an earlier, garbled read of the same photo. Returns true when
// it answered.
func (s *sessionService) unreadableAttachmentsAnswer(ctx context.Context, req *types.QARequest, bus *event.EventBus, agentMode bool) (bool, error) {
	if req == nil || req.Session == nil || bus == nil {
		return false, nil
	}
	answer, ok := unreadableAttachmentsMessage(req)
	if !ok {
		return false, nil
	}
	logger.Infof(ctx, "every attachment of the turn is unreadable (%d): answering with the cause, no model runs", len(req.Attachments))
	data := map[string]interface{}{"files": unreadableAttachmentFiles(req.Attachments)}
	if agentMode {
		return true, emitPipelineCard(ctx, bus, req, UnreadableAttachmentsToolName, "unread-", answer, data)
	}
	return true, emitPipelineCardNormal(ctx, bus, req, UnreadableAttachmentsToolName, "unread-", answer, data)
}

// unreadableAttachmentsMessage builds the fixed answer when the turn has
// attachments and none of them is readable; ok is false otherwise (no
// attachments, at least one readable file, or an image forwarded to the
// model).
func unreadableAttachmentsMessage(req *types.QARequest) (string, bool) {
	if len(req.Attachments) == 0 || len(req.ImageURLs) > 0 || strings.TrimSpace(req.ImageDescription) != "" {
		return "", false
	}
	for _, att := range req.Attachments {
		if att.ParseError == "" {
			return "", false
		}
	}
	// One file: the cause alone, since the attachment chip already shows
	// the file's name. Several files: one line each, named.
	if len(req.Attachments) == 1 {
		return "Chưa thể trả lời vì tệp đính kèm không đọc được. " + attachmentFailureCause(req.Attachments[0].ParseError), true
	}
	var sb strings.Builder
	sb.WriteString("Chưa thể trả lời vì các tệp đính kèm đều không đọc được.\n")
	for _, att := range req.Attachments {
		name := strings.TrimSpace(att.FileName)
		if name == "" {
			name = "tệp"
		}
		sb.WriteString("- ")
		sb.WriteString(name)
		sb.WriteString(": ")
		sb.WriteString(attachmentFailureCause(att.ParseError))
		sb.WriteString("\n")
	}
	sb.WriteString("Hãy gửi lại tệp đọc được, hoặc đặt câu hỏi không kèm tệp.")
	return sb.String(), true
}

// attachmentFailureCause turns a stored parse error into the sentence shown
// to the user: the handler's English prefixes are dropped, the Vietnamese
// cause the parser wrote is kept, and an English-only error gets a generic
// sentence.
func attachmentFailureCause(parseError string) string {
	cause := strings.TrimSpace(parseError)
	for _, prefix := range []string{"parsing failed:", "failed to parse document:", "failed to read document:",
		"failed to process text file:", "failed to transcribe audio file:"} {
		if strings.HasPrefix(strings.ToLower(cause), prefix) {
			cause = strings.TrimSpace(cause[len(prefix):])
			break
		}
	}
	if cause == "" || !strings.ContainsFunc(cause, func(r rune) bool { return r > 127 }) {
		// no Vietnamese text in it: an internal error message
		return "tệp không đọc được."
	}
	return cause
}

func unreadableAttachmentFiles(atts types.MessageAttachments) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(atts))
	for _, att := range atts {
		out = append(out, map[string]interface{}{
			"id": att.ID, "file_name": att.FileName, "file_type": att.FileType, "cause": attachmentFailureCause(att.ParseError),
		})
	}
	return out
}
