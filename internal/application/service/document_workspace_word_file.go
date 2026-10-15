package service

import (
	"context"
	"path/filepath"
	"strings"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"golang.org/x/text/unicode/norm"
)

// maxWordFileIDLen bounds the ID the add-in sends (a UUID).
const maxWordFileIDLen = 64

// errWordFileMismatch answers a Word file binding to another file's
// document: the add-in then drops that conversation from the file's list.
const errWordFileMismatch = "this conversation belongs to another Word file"

// BindWordFile ties a Word add-in target to the Word file fileID. A Word
// file keeps its conversations inside the .docx, so a file once bound to
// another file's conversation (seen 2026-10-15: picked from a history that
// listed every file's chats) would upload itself over that file's
// document; the ID stops it. A document from before the ID is bound by the
// first file of the same name.
func (s *documentWorkspaceService) BindWordFile(
	ctx context.Context, tenantID uint64, sessionID, documentID, fileID, fileName string,
) (*types.DocumentWorkspace, error) {
	fileID = strings.TrimSpace(fileID)
	if fileID == "" || len(fileID) > maxWordFileIDLen {
		return nil, apperrors.NewBadRequestError("word_file_id is required (at most 64 characters)")
	}
	ws, err := s.getTarget(ctx, tenantID, sessionID, documentID)
	if err != nil {
		return nil, err
	}
	if ws.EditorKind != types.DocumentEditorKindWordAddin {
		return nil, apperrors.NewConflictError("only a Word add-in document is bound to a Word file")
	}
	switch {
	case ws.WordFileID == fileID:
		return ws, nil
	case ws.WordFileID != "" || !sameWordFileName(ws.FileName, fileName):
		logger.Infof(ctx, "[DocumentWorkspace] refused word file binding workspace=%s file=%s",
			ws.ID, secutils.SanitizeForLog(fileName))
		return nil, apperrors.NewConflictError(errWordFileMismatch)
	}
	ws.WordFileID = fileID
	if err := s.repo.Update(ctx, ws); err != nil {
		return nil, err
	}
	return ws, nil
}

// sameWordFileName compares two names of a .docx: case, Unicode form and
// the .docx/.doc extension aside (Word may give the name without it).
func sameWordFileName(a, b string) bool {
	clean := func(s string) string {
		s = norm.NFC.String(strings.TrimSpace(s))
		if ext := strings.ToLower(filepath.Ext(s)); ext == ".docx" || ext == ".doc" {
			s = s[:len(s)-len(ext)]
		}
		return strings.ToLower(strings.TrimSpace(s))
	}
	x := clean(a)
	return x != "" && x == clean(b)
}
