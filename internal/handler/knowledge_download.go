package handler

import (
	"archive/zip"
	"context"
	stderrors "errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
	"unicode"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/filetransport"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

const (
	maxBatchDownloadFiles             = 200
	maxBatchDownloadBytes       int64 = 512 * 1024 * 1024
	maxConcurrentBatchDownloads       = 4
)

var batchDownloadSlots = make(chan struct{}, maxConcurrentBatchDownloads)

// BatchDownloadKnowledgeRequest chỉ định các tài liệu cần tải về trong cùng một kho tri thức.
type BatchDownloadKnowledgeRequest struct {
	IDs []string `json:"ids" binding:"required,min=1,max=200,dive,required,max=128"`
}

type knowledgeDownloadEntry struct {
	ID         string
	FolderPath string
}

// BatchDownloadKnowledge godoc
// @Summary Tải về hàng loạt tệp tài liệu
// @Description Đóng gói tối đa 200 tài liệu của cùng một kho tri thức thành ZIP, tổng nội dung gốc không quá 512 MiB. Mục không có tệp gốc sẽ bị bỏ qua; khi không có quyền, khác kho hoặc đọc lỗi thì không trả về gói nén dở dang.
// @Tags Tài liệu
// @Accept json
// @Produce application/zip
// @Param id path string true "Knowledge BaseID"
// @Param request body BatchDownloadKnowledgeRequest true "Danh sách ID tài liệu"
// @Success 200 {file} file "Gói nén ZIP"
// @Failure 400 {object} errors.AppError
// @Failure 401 {object} errors.AppError
// @Failure 403 {object} errors.AppError
// @Failure 404 {object} errors.AppError
// @Failure 429 {object} errors.AppError
// @Failure 500 {object} errors.AppError
// @Security Bearer
// @Security ApiKeyAuth
// @Router /knowledge-bases/{id}/knowledge/batch-download [post]
func (h *KnowledgeHandler) BatchDownloadKnowledge(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64*1024)
	var req BatchDownloadKnowledgeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(errors.NewBadRequestError("chọn từ 1 đến 200 tài liệu để tải về hàng loạt"))
		return
	}

	kbID := c.Param("id")
	grant, err := resolveHandlerKBDownloadAccessFor(
		c, kbID, h.kbService,
	)
	if err != nil {
		_ = c.Error(err)
		return
	}
	tenantID := grant.EffectiveTenantID
	ctx := types.WithExecutionTenant(c.Request.Context(), tenantID)
	ids := uniqueKnowledgeDownloadIDs(req.IDs)
	if len(ids) == 0 {
		_ = c.Error(errors.NewBadRequestError("ID tài liệu không được để trống"))
		return
	}

	items, err := h.kgService.GetKnowledgeBatch(ctx, tenantID, ids)
	if err != nil {
		_ = c.Error(errors.NewInternalServerError("không lấy được danh sách tài liệu cần tải, vui lòng thử lại sau"))
		return
	}
	byID := make(map[string]*types.Knowledge, len(items))
	for _, item := range items {
		if item != nil && item.KnowledgeBaseID == kbID && item.TenantID == tenantID {
			byID[item.ID] = item
		}
	}
	entries := make([]knowledgeDownloadEntry, 0, len(ids))
	// Kiểm tra cả lô tài liệu trước khi đọc bất kỳ tệp nào, tránh lẫn ID của kho tri thức hoặc đơn vị khác.
	for _, id := range ids {
		item := byID[id]
		if item == nil {
			_ = c.Error(errors.NewNotFoundError("một số tài liệu không tồn tại hoặc không thuộc kho tri thức hiện tại, hãy tải lại danh sách rồi thử lại"))
			return
		}
		if !item.IsManual() && item.FilePath == "" {
			continue
		}
		entries = append(entries, knowledgeDownloadEntry{ID: id, FolderPath: item.FolderPath})
	}
	if len(entries) == 0 {
		_ = c.Error(errors.NewBadRequestError("các tài liệu đã chọn không có tệp gốc để tải, hãy bỏ chọn rồi thử lại"))
		return
	}

	if !tryAcquireBatchDownloadSlot() {
		_ = c.Error(errors.NewTooManyRequestsError("đang có quá nhiều lượt tải hàng loạt, vui lòng thử lại sau"))
		return
	}
	defer releaseBatchDownloadSlot()

	// Tạo gói nén hoàn chỉnh trong tệp tạm trước, tránh trả về ZIP dở dang cho người dùng khi đọc lỗi.
	archive, err := os.CreateTemp("", "weknora-download-*.zip")
	if err != nil {
		_ = c.Error(errors.NewInternalServerError("không tạo được gói nén tải về, vui lòng thử lại sau"))
		return
	}
	archivePath := archive.Name()
	defer func() {
		_ = archive.Close()
		_ = os.Remove(archivePath)
	}()
	if err := writeKnowledgeDownloadArchive(
		ctx, archive, entries, h.kgService.GetKnowledgeFile, maxBatchDownloadBytes,
	); err != nil {
		mapped := mapKnowledgeDownloadError(err)
		if appErr, ok := errors.IsAppError(mapped); !ok || appErr.HTTPCode >= 500 {
			logger.ErrorWithFields(ctx, err, nil)
		}
		_ = c.Error(mapped)
		return
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		_ = c.Error(errors.NewInternalServerError("không đọc được gói nén tải về, vui lòng thử lại sau"))
		return
	}
	filename := "knowledge-files-" + time.Now().Format("20060102-150405") + ".zip"
	if err := filetransport.Serve(c.Writer, c.Request, archive, filetransport.Options{
		Filename: filename, Download: true, ContentType: "application/zip", CacheControl: "private, no-store",
	}); err != nil {
		logger.Errorf(ctx, "Failed to send knowledge archive: %v", err)
	}
}

func tryAcquireBatchDownloadSlot() bool {
	select {
	case batchDownloadSlots <- struct{}{}:
		return true
	default:
		return false
	}
}

func releaseBatchDownloadSlot() {
	<-batchDownloadSlots
}

func uniqueKnowledgeDownloadIDs(input []string) []string {
	ids := make([]string, 0, len(input))
	seen := make(map[string]bool, len(input))
	for _, id := range input {
		id = strings.TrimSpace(id)
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

func isKnowledgeDownloadCanceled(err error) bool {
	return err != nil && (stderrors.Is(err, context.Canceled) || stderrors.Is(err, context.DeadlineExceeded))
}

func mapKnowledgeDownloadError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := errors.IsAppError(err); ok {
		return err
	}
	if isKnowledgeDownloadCanceled(err) {
		return errors.NewBadRequestError("đã huỷ tải về")
	}
	return err
}

type knowledgeDownloadOpener func(context.Context, string) (io.ReadCloser, string, error)

func writeKnowledgeDownloadArchive(
	ctx context.Context,
	output io.Writer,
	entries []knowledgeDownloadEntry,
	open knowledgeDownloadOpener,
	limit int64,
) error {
	writer := zip.NewWriter(output)
	usedNames := make(map[string]bool, len(entries))
	var total int64
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		file, filename, err := open(ctx, entry.ID)
		if err != nil {
			if isKnowledgeDownloadCanceled(err) {
				return err
			}
			return errors.NewInternalServerError("một số tệp không đọc được, chưa tạo gói nén, hãy kiểm tra tài liệu rồi thử lại").WithDetails(
				gin.H{"knowledge_id": entry.ID},
			)
		}
		name := uniqueKnowledgeDownloadZipPath(entry.FolderPath, filename, usedNames)
		// Lưu nguyên dạng để tránh tốn CPU nén lại các tệp đã nén sẵn như PDF, Office.
		zipEntry, err := writer.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		if err != nil {
			_ = file.Close()
			return errors.NewInternalServerError("không ghi được gói nén tải về, vui lòng thử lại sau")
		}
		n, copyErr := io.Copy(zipEntry, io.LimitReader(
			&knowledgeDownloadReader{ctx: ctx, reader: file}, limit-total+1,
		))
		closeErr := file.Close()
		total += n
		if total > limit {
			return errors.NewBadRequestError("tổng dung lượng tệp đã chọn vượt 512 MiB, hãy tải theo từng đợt")
		}
		if isKnowledgeDownloadCanceled(copyErr) {
			return copyErr
		}
		if copyErr != nil || closeErr != nil {
			return errors.NewInternalServerError("đọc một số tệp thất bại, chưa tạo gói nén, vui lòng thử lại sau").WithDetails(
				gin.H{"knowledge_id": entry.ID},
			)
		}
	}
	if err := writer.Close(); err != nil {
		return errors.NewInternalServerError("không hoàn tất được gói nén tải về, vui lòng thử lại sau")
	}
	return nil
}

// Kiểm tra tín hiệu huỷ trước mỗi lần đọc; người dùng rời trang thì dừng đóng gói.
type knowledgeDownloadReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *knowledgeDownloadReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func uniqueKnowledgeDownloadName(filename string, used map[string]bool) string {
	return uniqueKnowledgeDownloadZipPath("", filename, used)
}

func uniqueKnowledgeDownloadZipPath(folderPath, filename string, used map[string]bool) string {
	base := sanitizeDownloadFileName(filename)
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	dir := sanitizeDownloadFolderPath(folderPath)
	name := base
	if dir != "" {
		name = dir + "/" + base
	}
	for index := 2; used[strings.ToLower(name)]; index++ {
		suffixed := fmt.Sprintf("%s (%d)%s", stem, index, ext)
		if dir != "" {
			name = dir + "/" + suffixed
		} else {
			name = suffixed
		}
	}
	used[strings.ToLower(name)] = true
	return name
}

func sanitizeDownloadFileName(filename string) string {
	filename = path.Base(strings.ReplaceAll(filename, "\\", "/"))
	filename = sanitizeDownloadRunes(filename)
	filename = strings.Trim(filename, " .")
	if filename == "" {
		filename = "document"
	}
	ext := path.Ext(filename)
	if len(ext) > 20 {
		ext = ""
	}
	stem := strings.TrimSuffix(filename, ext)
	stem = shortenDownloadName(stem, 180)
	stem = strings.TrimRight(stem, " .")
	if stem == "" {
		stem = "document"
	}
	return escapeWindowsReservedName(stem) + ext
}

func sanitizeDownloadFolderPath(folderPath string) string {
	folderPath = strings.ReplaceAll(folderPath, "\\", "/")
	var parts []string
	var total int
	for _, part := range strings.Split(folderPath, "/") {
		part = sanitizeDownloadRunes(part)
		part = strings.Trim(part, " .")
		if part == "" || part == "." || part == ".." {
			continue
		}
		part = shortenDownloadName(part, 80)
		part = strings.TrimRight(part, " .")
		if part == "" {
			continue
		}
		part = escapeWindowsReservedName(part)
		if total+len(part)+1 > 160 {
			break
		}
		parts = append(parts, part)
		total += len(part) + 1
	}
	return strings.Join(parts, "/")
}

func sanitizeDownloadRunes(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, value)
}

func shortenDownloadName(value string, limit int) string {
	var shortened strings.Builder
	for _, r := range value {
		if shortened.Len()+len(string(r)) > limit {
			break
		}
		shortened.WriteRune(r)
	}
	return shortened.String()
}

func escapeWindowsReservedName(stem string) string {
	reserved := strings.ToUpper(strings.Split(stem, ".")[0])
	reservedRunes := []rune(reserved)
	if reserved == "CON" || reserved == "PRN" || reserved == "AUX" || reserved == "NUL" ||
		(len(reservedRunes) == 4 && (strings.HasPrefix(reserved, "COM") || strings.HasPrefix(reserved, "LPT")) &&
			strings.ContainsRune("123456789¹²³", reservedRunes[3])) {
		return "_" + stem
	}
	return stem
}
