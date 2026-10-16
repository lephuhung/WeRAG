package service

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/docmeasure"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// inputGate refuses an upload whose page count exceeds the cap of its
// context, before the file is stored or parsed. The caps are the
// limits.* system settings; a nil settings service keeps the defaults.
type inputGate struct {
	settings interfaces.SystemSettingService
}

func newInputGate(settings interfaces.SystemSettingService) *inputGate {
	return &inputGate{settings: settings}
}

func (g *inputGate) limit(ctx context.Context, key string, def int) int {
	if g == nil || g.settings == nil {
		return def
	}
	return int(g.settings.GetInt(ctx, key, "", int64(def)))
}

// pageLimit returns the cap for a measured file: its kind (pdf_text,
// pdf_scan, docx) and the pages allowed in the given context.
func (g *inputGate) pageLimit(ctx context.Context, gate types.InputGateContext, ext string, m docmeasure.Measure) (kind string, limit int) {
	chat := gate == types.InputGateChat
	switch strings.ToLower(strings.TrimPrefix(ext, ".")) {
	case "pdf":
		if m.Scanned {
			if chat {
				return "pdf_scan", g.limit(ctx, types.SettingLimitsChatPDFScanPages, types.DefaultLimitsChatPDFScanPages)
			}
			return "pdf_scan", g.limit(ctx, types.SettingLimitsKBPDFScanPages, types.DefaultLimitsKBPDFScanPages)
		}
		if chat {
			return "pdf_text", g.limit(ctx, types.SettingLimitsChatPDFTextPages, types.DefaultLimitsChatPDFTextPages)
		}
		return "pdf_text", g.limit(ctx, types.SettingLimitsKBPDFTextPages, types.DefaultLimitsKBPDFTextPages)
	case "docx":
		if chat {
			return "docx", g.limit(ctx, types.SettingLimitsChatDocxPages, types.DefaultLimitsChatDocxPages)
		}
		return "docx", g.limit(ctx, types.SettingLimitsKBDocxPages, types.DefaultLimitsKBDocxPages)
	}
	return "", 0
}

// CheckPages measures the file and returns the 400 the user sees when it
// has more pages than its context allows; nil lets the upload go on. A
// file that cannot be measured passes: the parser reports it.
func (g *inputGate) CheckPages(ctx context.Context, gate types.InputGateContext, fileName, ext string, data []byte) error {
	m := docmeasure.MeasureFile(ext, data)
	if m.Pages <= 0 {
		return nil
	}
	kind, limit := g.pageLimit(ctx, gate, ext, m)
	if kind == "" || limit <= 0 || m.Pages <= limit {
		return nil
	}
	logger.Infof(ctx, "[InputGate] %s refused: %s has %d %s pages, cap %d (%s)", gate, fileName, m.Pages, kind, limit, ext)
	return apperrors.NewBadRequestError(pageLimitMessage(gate, fileName, kind, m, limit)).
		WithDetails(types.InputGateDetails{
			Reason: types.InputGateReasonTooManyPages, Kind: kind,
			Pages: m.Pages, Limit: limit, Estimated: m.Estimated,
		})
}

func pageLimitMessage(gate types.InputGateContext, fileName, kind string, m docmeasure.Measure, limit int) string {
	what := "Tệp"
	switch kind {
	case "pdf_scan":
		what = "PDF scan (không có lớp chữ, phải OCR từng trang)"
	case "pdf_text":
		what = "PDF"
	case "docx":
		what = "Tệp Word"
	}
	count := fmt.Sprintf("%d trang", m.Pages)
	if m.Estimated {
		count = fmt.Sprintf("khoảng %d trang", m.Pages)
	}
	where := "kho tri thức"
	if gate == types.InputGateChat {
		where = "cuộc hội thoại"
	}
	return fmt.Sprintf("%s %s có %s, vượt giới hạn %d trang của %s. Hãy tách tệp hoặc chỉ gửi phần cần dùng.",
		what, fileName, count, limit, where)
}

// ParsedTextTooThin tells whether a parsed upload yielded too little text
// to be worth keeping (see types.MinParsedTextRunes). Image references are
// not text.
func ParsedTextTooThin(content string) bool {
	return approxTextContentRunes(content) < types.MinParsedTextRunes
}

// noTextMessage is the failure a user sees when nothing readable came out
// of the upload. ocrSkipped (see metadataOCRSkipped) names an agent setting
// that kept the OCR from running, which the user can act on by switching
// agents; otherwise the file itself is the cause.
func noTextMessage(ext string, ocrSkipped string) string {
	switch ocrSkipped {
	case "ocr_error":
		return "Dịch vụ đọc ảnh (OCR) đang không hoạt động nên chưa đọc được tệp. Hãy thử lại sau hoặc gửi tệp có lớp chữ."
	case "no_vlm":
		return "Trợ lý này không đọc được ảnh (chưa cấu hình mô hình nhìn ảnh). Hãy chọn trợ lý khác hoặc gửi tệp văn bản."
	case "scan_disabled":
		return "Tệp PDF là bản scan không có lớp chữ và trợ lý này không OCR bản scan. Hãy gửi tệp có lớp chữ hoặc chọn trợ lý có bật đọc tài liệu scan."
	}
	switch strings.ToLower(strings.TrimPrefix(ext, ".")) {
	case "png", "jpg", "jpeg", "gif", "webp", "bmp", "tiff", "tif":
		return "Không đọc được chữ trong ảnh. Hãy chụp rõ hơn, thẳng góc, đủ sáng, hoặc gửi tệp văn bản thay cho ảnh."
	case "pdf":
		return "Không đọc được chữ trong tệp PDF: bản scan quá mờ hoặc tệp không có nội dung. Hãy gửi bản scan rõ hơn hoặc tệp có lớp chữ."
	case "mp3", "wav", "m4a", "flac", "ogg":
		return "Không nhận dạng được lời nói trong tệp âm thanh."
	}
	return "Tệp không có nội dung chữ đọc được."
}

// QuestionTooLong returns the 400 a question longer than the chat cap
// gets; nil otherwise. The cap is limits.chat.question_max_runes.
func (g *inputGate) QuestionTooLong(ctx context.Context, query string) error {
	limit := g.limit(ctx, types.SettingLimitsChatQuestionMaxRunes, types.DefaultLimitsChatQuestionMaxRunes)
	n := utf8.RuneCountInString(query)
	if limit <= 0 || n <= limit {
		return nil
	}
	return apperrors.NewBadRequestError(fmt.Sprintf(
		"Câu hỏi dài %d ký tự, vượt giới hạn %d ký tự. Hãy rút gọn câu hỏi hoặc gửi nội dung dài dưới dạng tệp đính kèm.", n, limit)).
		WithDetails(types.QuestionLengthDetails{Reason: types.InputGateReasonQuestionTooLong, Runes: n, Limit: limit})
}

// BroadQuestionGateEnabled tells whether short generic questions are
// answered with the narrowing card (limits.chat.broad_question_gate).
func (g *inputGate) BroadQuestionGateEnabled(ctx context.Context) bool {
	if g == nil || g.settings == nil {
		return true
	}
	return g.settings.GetBool(ctx, types.SettingLimitsChatBroadQuestionGate, "", true)
}

// ParseConcurrencyLimit is how many uploads of one user may be parsed at
// once (limits.parse.max_concurrent_per_user); 0 means no limit.
func (g *inputGate) ParseConcurrencyLimit(ctx context.Context) int {
	return g.limit(ctx, types.SettingLimitsParseMaxConcurrentPerUser, types.DefaultLimitsParseMaxConcurrentPerUser)
}

// NewInputGate exposes the gate to packages outside the service (the
// session handler checks question length before the stream starts).
func NewInputGate(settings interfaces.SystemSettingService) InputGate {
	return newInputGate(settings)
}

// InputGate is the part of the gate other packages use.
type InputGate interface {
	QuestionTooLong(ctx context.Context, query string) error
}

// The per-user parse queue: an upload whose owner already has the allowed
// number of documents being parsed is put back for parseSlotDelay(waits)
// and tried again, at most parseSlotMaxWaits times (then it goes ahead so
// a stuck counter never holds a document forever). A document counted as
// "processing" for longer than parseSlotStaleAfter is ignored: its worker
// died and the row was never finalized.
const (
	parseSlotMaxWaits   = 60
	parseSlotStaleAfter = 30 * time.Minute
)

func parseSlotDelay(waits int) time.Duration {
	if waits > 6 {
		waits = 6
	}
	if waits < 1 {
		waits = 1
	}
	return time.Duration(waits) * 10 * time.Second
}

// parseSlotFree tells whether an upload may start parsing now, given how
// many of its owner's documents are being parsed (count) and how often it
// already waited.
func parseSlotFree(limit int, count int64, waits int) bool {
	return limit <= 0 || count < int64(limit) || waits >= parseSlotMaxWaits
}
