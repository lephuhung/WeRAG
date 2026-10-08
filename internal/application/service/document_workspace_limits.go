package service

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
)

// docxMediaBytes sums the stored (compressed) size of the pictures and other
// embedded files of a .docx: everything under word/media/ and
// word/embeddings/. Pictures are stored almost uncompressed, so this is
// close to what they add to the file.
func docxMediaBytes(data []byte) (int64, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return 0, err
	}
	var total int64
	for _, f := range zr.File {
		name := strings.ToLower(f.Name)
		if strings.HasPrefix(name, "word/media/") || strings.HasPrefix(name, "word/embeddings/") {
			total += int64(f.CompressedSize64)
		}
	}
	return total, nil
}

func formatMB(n int64) string {
	return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
}

// checkDocumentWorkspaceSize refuses a document the editor should not hold:
// over MaxDocumentWorkspaceFileBytes, or carrying more than
// MaxDocumentWorkspaceMediaBytes of embedded pictures. data is the .docx
// (a .doc is checked after conversion). The messages are shown to the user.
func checkDocumentWorkspaceSize(fileName string, data []byte) error {
	if int64(len(data)) > types.MaxDocumentWorkspaceFileBytes {
		return apperrors.NewBadRequestError(fmt.Sprintf(
			"Văn bản %s nặng %s, vượt giới hạn %s của trình soạn thảo.",
			fileName, formatMB(int64(len(data))), formatMB(types.MaxDocumentWorkspaceFileBytes)))
	}
	media, err := docxMediaBytes(data)
	if err != nil {
		return apperrors.NewBadRequestError(fmt.Sprintf("Không đọc được %s: tệp Word bị hỏng hoặc không đúng định dạng.", fileName))
	}
	if media > types.MaxDocumentWorkspaceMediaBytes {
		return apperrors.NewBadRequestError(fmt.Sprintf(
			"Văn bản %s chứa %s hình ảnh, vượt giới hạn %s. Hãy nén ảnh (Word: Định dạng ảnh → Nén ảnh) hoặc gỡ bớt ảnh rồi tải lên lại.",
			fileName, formatMB(media), formatMB(types.MaxDocumentWorkspaceMediaBytes)))
	}
	return nil
}
