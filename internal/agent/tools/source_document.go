package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/types"
)

// sourceTextReader is the part of the document workspace service that reads
// a source document's stored text (interfaces.DocumentWorkspaceService
// satisfies it).
type sourceTextReader interface {
	SourceText(ctx context.Context, tenantID uint64, sessionID, documentID string) (*types.DocumentWorkspaceText, *types.DocumentWorkspace, error)
}

// Paging of a source in read_document_outline: its parts are whole chunks
// (≈1600 characters) or lines, given in full up to sourceOutlineRunes.
const (
	sourceOutlineChunkLimit = 6
	sourceOutlineChunkMax   = 20
	sourceOutlineLineLimit  = 80
	sourceOutlineLineMax    = 200
	sourceOutlineRunes      = 16000
	sourceOutlinePartRunes  = 4000
)

// sourcePart is one numbered piece of a source's text.
type sourcePart struct {
	header, text string
}

// sourceParts splits a source's stored text: its chunks when the upload was
// chunked (chunked=true), else its non-empty lines.
func sourceParts(text *types.DocumentWorkspaceText) (parts []sourcePart, chunked bool) {
	var chunks []types.TemporaryDocumentChunk
	if len(text.Chunks) > 0 {
		_ = json.Unmarshal(text.Chunks, &chunks)
	}
	if len(chunks) > 0 {
		for _, c := range chunks {
			parts = append(parts, sourcePart{header: strings.TrimSpace(c.ContextHeader), text: strings.TrimSpace(c.Content)})
		}
		return parts, true
	}
	for _, line := range strings.Split(text.Content, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			parts = append(parts, sourcePart{text: line})
		}
	}
	return parts, false
}

// outlineSource is read_document_outline for a source: its stored text as
// numbered chunks (or lines), each in full, paged by from/limit.
func outlineSource(ctx context.Context, src DocumentWorkspaceSource, sessionID string, ws *types.DocumentWorkspace, from, limitArg int, component string) *types.ToolResult {
	reader, ok := src.(sourceTextReader)
	tenantID, hasTenant := types.TenantIDFromContext(ctx)
	if !ok || !hasTenant {
		return &types.ToolResult{Success: false, Error: errors.New("không đọc được nội dung tài liệu nguồn " + DocumentLabel(ws)).Error()}
	}
	text, row, err := reader.SourceText(ctx, tenantID, sessionID, ws.ID)
	if err != nil {
		return &types.ToolResult{Success: false, Error: err.Error()}
	}
	if row != nil {
		ws = row
	}
	parts, chunked := sourceParts(text)
	unit, limit, maxLimit := "dòng", sourceOutlineLineLimit, sourceOutlineLineMax
	if chunked {
		unit, limit, maxLimit = "chunk", sourceOutlineChunkLimit, sourceOutlineChunkMax
	}
	if limitArg > 0 {
		limit = min(limitArg, maxLimit)
	}

	var body strings.Builder
	used, to := 0, from
	for i := from; i < len(parts) && i < from+limit; i++ {
		p := parts[i]
		line := clipRunes(p.text, sourceOutlinePartRunes)
		if chunked {
			label := "(chunk) "
			if p.header != "" {
				label += strings.NewReplacer("\n", " ↵ ").Replace(p.header) + " | "
			}
			line = fmt.Sprintf("[%d] %s%s\n\n", i, label, line)
		} else {
			line = fmt.Sprintf("[%d] %s\n", i, line)
		}
		n := utf8.RuneCountInString(line)
		if used > 0 && used+n > sourceOutlineRunes {
			break
		}
		body.WriteString(line)
		used += n
		to = i + 1
	}

	var out strings.Builder
	fmt.Fprintf(&out, "# Nội dung tài liệu nguồn %s (%s, %d %s)\n", DocumentLabel(ws), ws.FileType, len(parts), unit)
	out.WriteString("Tài liệu nguồn chỉ tra cứu, không sửa được; số trong [ ] là số thứ tự " + unit + ", không phải chỉ số đoạn của rewrite_paragraphs. " +
		"Khi dùng nội dung này, hãy dẫn nguồn bằng tên tệp hoặc số ký hiệu của văn bản.\n")
	if component != "" {
		out.WriteString("Tài liệu nguồn không có nhãn thành phần NĐ30; bỏ qua tham số component.\n")
	}
	out.WriteString("\n")
	if to <= from {
		out.WriteString("(không có nội dung trong phạm vi này)\n")
	} else {
		out.WriteString(body.String())
	}
	if to < len(parts) {
		fmt.Fprintf(&out, "\nCòn các %s sau: gọi lại với document=%s from=%d.\n", unit, ws.Handle(), to)
	}
	return &types.ToolResult{
		Success: true,
		Output:  out.String(),
		Data: map[string]interface{}{
			"file_name":  ws.FileName,
			"role":       types.DocumentWorkspaceRoleSource,
			"part_count": len(parts),
			"unit":       unit,
			"from":       from,
			"to":         to,
		},
	}
}
