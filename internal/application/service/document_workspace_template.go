package service

import (
	"context"
	"fmt"

	"github.com/Tencent/WeKnora/internal/docformat/docxedit"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// Revision labels of a document drafted from chat (the web's NĐ30
// templates, prepare_document_template).
const (
	documentRevisionLabelBeforeTemplate = "Trước khi tạo văn bản theo NĐ 30"
	documentRevisionLabelTemplate       = "Tạo văn bản theo NĐ 30"
)

// StoreTemplate writes a document the page built from a ready
// prepare_document_template result into an embedded-editor target that is
// still empty. The editor is saved and snapshotted first (the empty state
// stays restorable), a document with any text is refused, and the new
// editor key makes the open editor load the file. baseRevision is the
// revision the page last saw; a restore or AI write since then is a
// conflict. The Word add-in writes its templates into Word and uploads
// them as usual (StoreClientSave).
func (s *documentWorkspaceService) StoreTemplate(
	ctx context.Context, tenantID uint64, sessionID, documentID string, baseRevision int, data []byte,
) (*types.DocumentWorkspace, error) {
	if len(data) == 0 {
		return nil, apperrors.NewBadRequestError("document is empty")
	}
	if s.revisions == nil {
		return nil, apperrors.NewServiceUnavailableError("document revisions are not configured")
	}
	ws, err := s.getTarget(ctx, tenantID, sessionID, documentID)
	if err != nil {
		return nil, err
	}
	if ws.IsWordAddin() {
		return nil, apperrors.NewBadRequestError("this document is edited in Word; the add-in inserts the template there")
	}
	if ws.Revision != baseRevision {
		return nil, apperrors.NewConflictError("document changed since it was opened; reload it and try again")
	}
	if err := checkDocumentWorkspaceSize(ws.FileName, data); err != nil {
		return nil, err
	}
	if _, err := docxedit.Open(data); err != nil {
		return nil, apperrors.NewBadRequestError("the template is not a valid .docx")
	}
	// what the user typed until now is saved and kept as a revision
	if _, err := s.Snapshot(ctx, tenantID, sessionID, ws.ID, documentRevisionLabelBeforeTemplate,
		types.DocumentRevisionSourceAI, 0); err != nil {
		return nil, fmt.Errorf("snapshot before template: %w", err)
	}
	ws, err = s.Get(ctx, tenantID, sessionID, ws.ID)
	if err != nil {
		return nil, err
	}
	if ws.Revision != baseRevision {
		return nil, apperrors.NewConflictError("document changed since it was opened; reload it and try again")
	}
	if err := s.refuseNonEmpty(ctx, ws); err != nil {
		return nil, err
	}
	ref, err := s.files.SaveBytes(ctx, data, tenantID, documentWorkspaceStorageName(ws.ID), false)
	if err != nil {
		return nil, fmt.Errorf("save template: %w", err)
	}
	s.bind(ctx, ref, ws.ID, types.ResourceRelationArtifact)
	ws.CurrentRef = ref
	ws.FileSize = int64(len(data))
	// a new key: the editor reloads the template, saves under the old key
	// are ignored as stale
	ws.Revision = baseRevision + 1
	ok, err := s.repo.UpdateIfRevision(ctx, ws, baseRevision)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, apperrors.NewConflictError("document changed since it was opened; reload it and try again")
	}
	if _, err := s.recordRevision(ctx, ws, documentRevisionLabelTemplate, types.DocumentRevisionSourceAI, false); err != nil {
		logger.Warnf(ctx, "[DocumentWorkspace] record template revision failed: workspace=%s err=%v", ws.ID, err)
	}
	logger.Infof(ctx, "[DocumentWorkspace] template stored: workspace=%s revision=%d size=%d", ws.ID, ws.Revision, ws.FileSize)
	return ws, nil
}

// refuseNonEmpty fails when the stored document has any text in its body:
// a template never replaces what the user wrote.
func (s *documentWorkspaceService) refuseNonEmpty(ctx context.Context, ws *types.DocumentWorkspace) error {
	reader, err := s.files.GetFile(ctx, ws.CurrentRef)
	if err != nil {
		return fmt.Errorf("open document: %w", err)
	}
	defer reader.Close()
	current, err := readCapped(reader, maxDocumentWorkspaceBytes())
	if err != nil {
		return fmt.Errorf("read document: %w", err)
	}
	doc, err := docxedit.Open(current)
	if err != nil {
		return apperrors.NewConflictError("the document could not be read; a new document is created only in an empty one")
	}
	for _, p := range doc.Paragraphs() {
		if !p.Empty {
			return apperrors.NewConflictError("the document already has content; a new document is created only in an empty one")
		}
	}
	return nil
}
