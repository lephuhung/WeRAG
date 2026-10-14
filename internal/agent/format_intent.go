package agent

import (
	"context"
	"regexp"
	"strings"
	"unicode"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"golang.org/x/text/unicode/norm"
)

// A request to check the thể thức of the open document ("Kiểm tra thể
// thức văn bản này theo Nghị định 30/2020/NĐ-CP") names a decree, so the
// knowledge-lookup classifier reads it as a question beyond the document
// and the turn starts with search_knowledge under the grounding gate. The
// model then sometimes answers from the search hits and never calls
// check_document_format (seen 2026-10-14 in the compliance suite). When
// the request is a format check and the tool is offered, the turn starts
// with check_document_format instead and is not held to the grounding gate:
// the tool's report is the evidence.

// toolCheckDocumentFormat is agenttools.ToolCheckDocumentFormat (the tools
// package is imported elsewhere in this package; the literal keeps this
// file free of it).
const toolCheckDocumentFormat = "check_document_format"

var (
	// formatTopicRe: the request is about the thể thức / presentation.
	formatTopicRe = regexp.MustCompile(`THE THUC|TRINH BAY|CAN LE|LE TRANG|KHO GIAY|CO CHU|PHONG CHU|FONT|` +
		`NGHI DINH (SO )?30\b|N[ĐD] ?30\b|30/2020`)
	// formatCheckVerbRe: the user wants it checked, not explained.
	formatCheckVerbRe = regexp.MustCompile(`KIEM TRA|RA SOAT|SOAT( LOI)?|DANH GIA|CHAM|XEM (LAI|GIUP|HO)|` +
		`(DUNG|CHUAN|PHU HOP|DAT)\b.*\b(CHUA|KHONG|CHU)\b|CO (DUNG|CHUAN|SAI|LOI)`)
	// formatDecreeQuestionRe: a question about the decree itself.
	formatDecreeQuestionRe = regexp.MustCompile(`QUY DINH (GI|NHU THE NAO|RA SAO|THE NAO)|NOI DUNG (CUA )?NGHI DINH|` +
		`NGHI DINH 30 (LA|CO|GOM|NOI)|DIEU \d+|KHOAN \d+|HIEU LUC|BAN HANH (NGAY|NAM|KHI)`)
)

// foldQuery upper-cases and strips diacritics (Đ → D) like docformat.fold.
func foldQuery(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(s) {
		switch {
		case unicode.Is(unicode.Mn, r):
		case r == 'đ' || r == 'Đ':
			b.WriteRune('D')
		default:
			b.WriteRune(r)
		}
	}
	return strings.ToUpper(strings.Join(strings.Fields(b.String()), " "))
}

// isFormatCheckRequest reports whether query asks to check the format of
// the open document rather than ask about the decree.
func isFormatCheckRequest(query string) bool {
	f := foldQuery(query)
	if f == "" || !formatTopicRe.MatchString(f) || !formatCheckVerbRe.MatchString(f) {
		return false
	}
	return !formatDecreeQuestionRe.MatchString(f)
}

// formatCheckFirstTool is check_document_format when the turn's request is
// a format check and the tool is offered, "" otherwise.
func formatCheckFirstTool(ctx context.Context, tools []chat.Tool) string {
	if !hasChatTool(tools, toolCheckDocumentFormat) || !isFormatCheckRequest(types.UserQueryFromContext(ctx)) {
		return ""
	}
	return toolCheckDocumentFormat
}
