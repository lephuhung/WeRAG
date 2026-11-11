package session

import (
	stderrors "errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/docformat/answerdocx"
	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	secutils "github.com/Tencent/WeKnora/internal/utils"
)

// AnswerExportHandler serves one chat answer as a Word file.
type AnswerExportHandler struct {
	exports *service.AnswerExportService
}

// NewAnswerExportHandler creates the handler.
func NewAnswerExportHandler(exports *service.AnswerExportService) *AnswerExportHandler {
	return &AnswerExportHandler{exports: exports}
}

// ExportAnswerDocx godoc
// @Summary      Tải câu trả lời ra .docx
// @Description  Tạo tệp Word từ câu trả lời đã lưu (không gọi mô hình): câu hỏi, thời gian, nội dung Markdown và danh sách tài liệu tham khảo. Chỉ áp dụng cho câu trả lời đã hoàn tất của trợ lý.
// @Tags         Cuộc trò chuyện
// @Produce      application/vnd.openxmlformats-officedocument.wordprocessingml.document
// @Param        session_id  path  string  true  "ID cuộc trò chuyện"
// @Param        message_id  path  string  true  "ID tin nhắn trả lời"
// @Success      200  {file}  binary
// @Failure      400  {object}  errors.AppError  "Không phải câu trả lời hoàn tất"
// @Failure      404  {object}  errors.AppError  "Không tìm thấy"
// @Security     Bearer
// @Router       /sessions/{session_id}/messages/{message_id}/export.docx [get]
func (h *AnswerExportHandler) ExportAnswerDocx(c *gin.Context) {
	ctx := c.Request.Context()
	sessionID := secutils.SanitizeForLog(paramSessionID(c))
	messageID := secutils.SanitizeForLog(c.Param("message_id"))
	if sessionID == "" || messageID == "" {
		_ = c.Error(errors.NewBadRequestError("session_id and message_id are required"))
		return
	}
	if h == nil || h.exports == nil {
		_ = c.Error(errors.NewInternalServerError("answer export unavailable"))
		return
	}
	file, err := h.exports.Export(ctx, sessionID, messageID)
	if err != nil {
		switch {
		case stderrors.Is(err, errors.ErrSessionNotFound), stderrors.Is(err, gorm.ErrRecordNotFound):
			_ = c.Error(errors.NewNotFoundError("message not found"))
		case service.IsAnswerExportRefusal(err):
			_ = c.Error(errors.NewBadRequestError(err.Error()))
		default:
			logger.Errorf(ctx, "answer export failed: session=%s message=%s err=%v", sessionID, messageID, err)
			_ = c.Error(errors.NewInternalServerError("không tạo được tệp .docx"))
		}
		return
	}
	c.Header("Content-Disposition", answerDisposition(file.FileName))
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, docxContentType, file.Data)
}

// answerDisposition is an attachment header with an ASCII filename (the
// Vietnamese name without diacritics) and the UTF-8 name per RFC 5987.
func answerDisposition(name string) string {
	return `attachment; filename="` + answerdocx.ASCIIFileName(name) + `"; filename*=UTF-8''` +
		(urlPathEscaper{}).escape(name)
}
