package handler

import (
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/Tencent/WeKnora/internal/docformat"
	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

// maxFormatCheckUpload caps a .docx sent to the format checker.
const maxFormatCheckUpload = 30 << 20

// DocumentFormatHandler checks uploaded .docx files against the thể thức
// of Nghị định 30/2020/NĐ-CP. Components are labelled by the workspace
// default chat model, so the check follows whatever model the workspace is
// switched to.
type DocumentFormatHandler struct {
	models interfaces.ModelService
}

// NewDocumentFormatHandler creates the handler.
func NewDocumentFormatHandler(models interfaces.ModelService) *DocumentFormatHandler {
	return &DocumentFormatHandler{models: models}
}

// ListTypes godoc
// @Summary      Document types with a format rule set
// @Tags         Document format
// @Produce      json
// @Success      200  {object}  map[string]interface{}
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /document-format/types [get]
func (h *DocumentFormatHandler) ListTypes(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"document_types": docformat.AvailableTypes(), "fallback": "base",
	}})
}

// Check godoc
// @Summary      Check the thể thức of a .docx (NĐ30/2020)
// @Description  Multipart upload: file (.docx), optional document_type
// @Description  (auto by default) and segmenter (auto | heuristic). Returns
// @Description  every rule with pass/fail/warn/skip, the measured value and
// @Description  the offending lines.
// @Tags         Document format
// @Accept       multipart/form-data
// @Produce      json
// @Param        file           formData  file    true   ".docx file"
// @Param        document_type  formData  string  false  "rule set, e.g. cong_van"
// @Param        segmenter      formData  string  false  "auto | heuristic"
// @Success      200  {object}  map[string]interface{}
// @Failure      400  {object}  errors.AppError
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /document-format/check [post]
func (h *DocumentFormatHandler) Check(c *gin.Context) {
	ctx := c.Request.Context()
	limitUploadBody(c, maxFormatCheckUpload)
	fh, err := c.FormFile("file")
	if err != nil {
		if isRequestBodyTooLarge(err) {
			_ = c.Error(errors.NewBadRequestError("file cannot exceed 30 MB"))
			return
		}
		_ = c.Error(errors.NewBadRequestError("file is required"))
		return
	}
	if !strings.EqualFold(filepath.Ext(fh.Filename), ".docx") {
		_ = c.Error(errors.NewBadRequestError("only .docx files are supported"))
		return
	}
	segmenter := strings.TrimSpace(c.PostForm("segmenter"))
	switch segmenter {
	case "", docformat.SegmenterAuto, docformat.SegmenterHeuristic:
	default:
		_ = c.Error(errors.NewBadRequestError("segmenter must be auto or heuristic"))
		return
	}
	f, err := fh.Open()
	if err != nil {
		_ = c.Error(errors.NewBadRequestError("could not read the upload"))
		return
	}
	defer f.Close()
	content, err := io.ReadAll(io.LimitReader(f, maxFormatCheckUpload+1))
	if err != nil || len(content) > maxFormatCheckUpload {
		_ = c.Error(errors.NewBadRequestError("could not read the upload"))
		return
	}

	opts := docformat.Options{
		DocumentType: c.PostForm("document_type"),
		Segmenter:    segmenter,
		SourceName:   filepath.Base(fh.Filename),
	}
	if segmenter != docformat.SegmenterHeuristic {
		// no chat model or an unavailable one is not an error: the report
		// says the heuristic was used
		if models, err := h.models.ListModels(ctx); err == nil {
			if m := selectChatModel(models, ""); m != nil {
				if model, err := h.models.GetChatModel(ctx, m.ID); err == nil {
					opts.LLM = docformat.ChatCompleter(model)
					opts.ModelName = m.Name
				} else {
					logger.Warnf(ctx, "document format: chat model %s unavailable: %v", m.ID, err)
				}
			}
		}
	}
	report := docformat.Check(ctx, content, opts)
	if !report.OK {
		_ = c.Error(errors.NewBadRequestError(report.Error))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": report})
}
