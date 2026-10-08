package service

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/docformat"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/infrastructure/docparser"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/google/uuid"
)

// temporaryDocumentReadyNotifier is implemented by the temporary document
// service: it calls fn once an upload's parsing ends (ready or failed), so a
// source copies its text while the upload still exists.
type temporaryDocumentReadyNotifier interface {
	OnDocumentParsed(fn func(ctx context.Context, tenantID uint64, sessionID, documentID string))
}

// errSourceDocument is the 409 of an editor-only action (editor config,
// save, snapshot, revisions, edits) asked for a source; the text is the
// agent tools' refusal.
func errSourceDocument(ws *types.DocumentWorkspace) error {
	return apperrors.NewConflictError(tools.SourceDocumentRefusal(ws))
}

func countRole(docs []*types.DocumentWorkspace, source bool) int {
	n := 0
	for _, d := range docs {
		if d.IsSource() == source {
			n++
		}
	}
	return n
}

func errTooManyTargets() error {
	return apperrors.NewConflictError(fmt.Sprintf(
		"Cuộc hội thoại đã mở tối đa %d văn bản; hãy đóng bớt một văn bản trước khi mở thêm.",
		types.MaxDocumentWorkspacesPerSession))
}

func errTooManySources() error {
	return apperrors.NewConflictError(fmt.Sprintf(
		"Cuộc hội thoại đã có tối đa %d tài liệu nguồn; hãy gỡ bớt một tài liệu nguồn trước khi tải thêm.",
		types.MaxDocumentSourcesPerSession))
}

// SourceCapacityError returns the 409 a new source would get in this
// session (nil when there is room), so the upload handler can refuse
// before it stores and parses the file.
func SourceCapacityError(docs []*types.DocumentWorkspace) error {
	if countRole(docs, true) >= types.MaxDocumentSourcesPerSession {
		return errTooManySources()
	}
	return nil
}

func (s *documentWorkspaceService) CreateSourceFromAttachment(
	ctx context.Context, tenantID uint64, sessionID, userID, attachmentID string,
) (*types.DocumentWorkspace, error) {
	if !s.Enabled() {
		return nil, apperrors.NewServiceUnavailableError("document editor is not configured")
	}
	sessionID = strings.TrimSpace(sessionID)
	attachmentID = strings.TrimSpace(attachmentID)
	if tenantID == 0 || sessionID == "" || attachmentID == "" {
		return nil, apperrors.NewBadRequestError("attachment_id is required")
	}
	existing, err := s.repo.ListBySession(ctx, tenantID, sessionID)
	if err != nil {
		return nil, err
	}
	for _, ws := range existing {
		if ws.AttachmentID == attachmentID {
			return ws, nil
		}
	}
	if err := SourceCapacityError(existing); err != nil {
		return nil, err
	}
	doc, err := s.attachments.Get(ctx, tenantID, sessionID, attachmentID)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, apperrors.NewNotFoundError("Attachment not found")
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(doc.FileName)), ".")
	if ext == "" {
		ext = strings.TrimPrefix(strings.ToLower(doc.FileType), ".")
	}
	if docparser.IsImageFormat(ext) {
		return nil, apperrors.NewBadRequestError("an image attachment is not a source document")
	}
	reader, fileName, err := s.attachments.OpenFile(ctx, tenantID, sessionID, attachmentID)
	if err != nil {
		return nil, err
	}
	data, err := readCapped(reader, maxDocumentWorkspaceBytes())
	_ = reader.Close()
	if err != nil {
		return nil, fmt.Errorf("read attachment: %w", err)
	}
	if strings.TrimSpace(fileName) == "" {
		fileName = doc.FileName
	}
	fileName = filepath.Base(fileName)
	position, err := s.repo.NextPosition(ctx, tenantID, sessionID)
	if err != nil {
		return nil, err
	}
	wsID := uuid.NewString()
	// A copy outside temporary storage: the upload expires after 24 hours,
	// a source lives as long as the session.
	ref, err := s.files.SaveBytes(ctx, data, tenantID, documentWorkspaceStorageNameExt(wsID, ext), false)
	if err != nil {
		return nil, fmt.Errorf("save source copy: %w", err)
	}
	ws := &types.DocumentWorkspace{
		ID: wsID, TenantID: tenantID, SessionID: sessionID, AttachmentID: attachmentID, Position: position,
		UserID: userID, OriginalRef: ref, CurrentRef: ref, FileName: fileName, FileType: ext,
		FileSize: int64(len(data)), Status: types.DocumentWorkspaceStatusOpen,
		Role: types.DocumentWorkspaceRoleSource, TextStatus: types.DocumentSourceTextProcessing,
	}
	if err := s.repo.Create(ctx, ws); err != nil {
		if isUniqueViolation(err) {
			return nil, apperrors.NewConflictError("this file is already in the conversation")
		}
		return nil, err
	}
	s.bind(ctx, ref, ws.ID, types.ResourceRelationSourceFile)
	logger.Infof(ctx, "[DocumentWorkspace] created source=%s session=%s position=%d file=%s size=%d",
		ws.ID, sessionID, ws.Position, secutils.SanitizeForLog(fileName), ws.FileSize)
	// parsing may already be over (lite mode parses inline)
	s.syncSourceText(ctx, ws, doc)
	return ws, nil
}

// onAttachmentParsed copies the parsed text of an upload into its source
// row, if the upload is one.
func (s *documentWorkspaceService) onAttachmentParsed(ctx context.Context, tenantID uint64, sessionID, attachmentID string) {
	docs, err := s.repo.ListBySession(ctx, tenantID, sessionID)
	if err != nil {
		logger.Warnf(ctx, "[DocumentWorkspace] list documents after parse of %s failed: %v", attachmentID, err)
		return
	}
	for _, ws := range docs {
		if ws.AttachmentID == attachmentID && ws.IsSource() {
			s.syncSourceText(ctx, ws, nil)
			return
		}
	}
}

// syncSourceText copies the upload's parsed text into the source's stored
// text once parsing has finished (doc nil: read the upload). Best effort: a
// failure leaves the source processing, and the next read tries again.
func (s *documentWorkspaceService) syncSourceText(ctx context.Context, ws *types.DocumentWorkspace, doc *types.TemporaryDocument) {
	if !ws.IsSource() || ws.TextStatus != types.DocumentSourceTextProcessing || ws.AttachmentID == "" {
		return
	}
	if doc == nil || (doc.Status != types.TemporaryDocumentStatusReady && doc.Status != types.TemporaryDocumentStatusFailed) {
		fresh, err := s.attachments.Get(ctx, ws.TenantID, ws.SessionID, ws.AttachmentID)
		if err != nil {
			logger.Warnf(ctx, "[DocumentWorkspace] read upload of source=%s failed: %v", ws.ID, err)
			return
		}
		doc = fresh
	}
	switch {
	case doc == nil:
		// the upload is gone (expired) before its text was copied
		ws.TextStatus = types.DocumentSourceTextFailed
	case doc.Status == types.TemporaryDocumentStatusReady:
		if err := s.repo.SaveText(ctx, &types.DocumentWorkspaceText{
			WorkspaceID: ws.ID, TenantID: ws.TenantID, Content: doc.Content, Chunks: doc.Chunks,
			TokenCount: doc.TokenCount, ChunkCount: doc.ChunkCount,
		}); err != nil {
			logger.Warnf(ctx, "[DocumentWorkspace] store text of source=%s failed: %v", ws.ID, err)
			return
		}
		ws.TextStatus = types.DocumentSourceTextReady
	case doc.Status == types.TemporaryDocumentStatusFailed:
		ws.TextStatus = types.DocumentSourceTextFailed
	default:
		return
	}
	if err := s.repo.Update(ctx, ws); err != nil {
		logger.Warnf(ctx, "[DocumentWorkspace] update text status of source=%s failed: %v", ws.ID, err)
	}
}

func (s *documentWorkspaceService) SourceText(
	ctx context.Context, tenantID uint64, sessionID, documentID string,
) (*types.DocumentWorkspaceText, *types.DocumentWorkspace, error) {
	if strings.TrimSpace(documentID) == "" {
		return nil, nil, apperrors.NewBadRequestError("document id is required")
	}
	ws, err := s.Get(ctx, tenantID, sessionID, documentID)
	if err != nil {
		return nil, nil, err
	}
	if !ws.IsSource() {
		return nil, ws, apperrors.NewBadRequestError(ws.Handle() + " is not a source document")
	}
	s.syncSourceText(ctx, ws, nil)
	switch ws.TextStatus {
	case types.DocumentSourceTextProcessing:
		return nil, ws, apperrors.NewConflictError(fmt.Sprintf("%s (%s) đang được đọc, hãy thử lại sau ít phút.", ws.Handle(), ws.FileName))
	case types.DocumentSourceTextFailed:
		return nil, ws, apperrors.NewConflictError(fmt.Sprintf("Không đọc được nội dung của %s (%s).", ws.Handle(), ws.FileName))
	}
	text, err := s.repo.GetText(ctx, ws.ID)
	if err != nil {
		return nil, ws, err
	}
	if text == nil {
		return nil, ws, apperrors.NewNotFoundError("source text not found")
	}
	return text, ws, nil
}

func (s *documentWorkspaceService) SetRole(
	ctx context.Context, tenantID uint64, sessionID, documentID, role string,
) (*types.DocumentWorkspace, error) {
	if strings.TrimSpace(documentID) == "" {
		return nil, apperrors.NewBadRequestError("document id is required")
	}
	ws, err := s.Get(ctx, tenantID, sessionID, documentID)
	if err != nil {
		return nil, err
	}
	switch role {
	case types.DocumentWorkspaceRoleTarget:
		if !ws.IsSource() {
			s.markActive(ctx, ws)
			return ws, nil
		}
		return s.promote(ctx, ws)
	case types.DocumentWorkspaceRoleSource:
		if ws.IsSource() {
			return ws, nil
		}
		return s.demote(ctx, ws)
	default:
		return nil, apperrors.NewBadRequestError("role must be target or source")
	}
}

// promote makes a Word source a target: the stored copy becomes the editor
// file (a .doc is converted), the row keeps its handle and becomes the
// active tab.
func (s *documentWorkspaceService) promote(ctx context.Context, ws *types.DocumentWorkspace) (*types.DocumentWorkspace, error) {
	if !s.Enabled() {
		return nil, apperrors.NewServiceUnavailableError("document editor is not configured")
	}
	ext := strings.ToLower(filepath.Ext(ws.FileName))
	if ext != ".docx" && ext != ".doc" {
		return nil, apperrors.NewBadRequestError(fmt.Sprintf(
			"Chỉ văn bản Word (.docx, .doc) mở được trong trình soạn thảo; %s chỉ dùng làm tài liệu nguồn.", ws.FileName))
	}
	docs, err := s.repo.ListBySession(ctx, ws.TenantID, ws.SessionID)
	if err != nil {
		return nil, err
	}
	if countRole(docs, false) >= types.MaxDocumentWorkspacesPerSession {
		return nil, errTooManyTargets()
	}
	reader, err := s.files.GetFile(ctx, ws.CurrentRef)
	if err != nil {
		return nil, fmt.Errorf("open source copy: %w", err)
	}
	data, err := readCapped(reader, maxDocumentWorkspaceBytes())
	_ = reader.Close()
	if err != nil {
		return nil, fmt.Errorf("read source copy: %w", err)
	}
	if int64(len(data)) > types.MaxDocumentWorkspaceFileBytes {
		return nil, checkDocumentWorkspaceSize(ws.FileName, data)
	}
	fileName, ref := ws.FileName, ws.CurrentRef
	if ext == ".doc" {
		data, err = s.convertDocToDocx(ctx, ws.TenantID, ws.ID, fileName, data)
		if err != nil {
			return nil, err
		}
		fileName = strings.TrimSuffix(fileName, filepath.Ext(fileName)) + ".docx"
		ref = ""
	}
	if err := checkDocumentWorkspaceSize(fileName, data); err != nil {
		return nil, err
	}
	if ref == "" {
		if ref, err = s.files.SaveBytes(ctx, data, ws.TenantID, documentWorkspaceStorageName(ws.ID), false); err != nil {
			return nil, fmt.Errorf("save document copy: %w", err)
		}
		s.bind(ctx, ref, ws.ID, types.ResourceRelationSourceFile)
	}
	s.bind(ctx, ref, ws.ID, types.ResourceRelationArtifact)
	ws.Role = types.DocumentWorkspaceRoleTarget
	ws.TextStatus = ""
	ws.FileName, ws.FileType, ws.FileSize = fileName, "docx", int64(len(data))
	ws.OriginalRef, ws.CurrentRef = ref, ref
	ws.Status, ws.ClosedAt = types.DocumentWorkspaceStatusOpen, nil
	if err := s.repo.Update(ctx, ws); err != nil {
		return nil, err
	}
	s.markActive(ctx, ws)
	logger.Infof(ctx, "[DocumentWorkspace] source=%s promoted to target (%s)", ws.ID, ws.Handle())
	return ws, nil
}

// demote turns a target into a source: its current state is snapshotted
// (like closing the tab), its paragraphs become the stored text, and it
// leaves the tab strip.
func (s *documentWorkspaceService) demote(ctx context.Context, ws *types.DocumentWorkspace) (*types.DocumentWorkspace, error) {
	docs, err := s.repo.ListBySession(ctx, ws.TenantID, ws.SessionID)
	if err != nil {
		return nil, err
	}
	if err := SourceCapacityError(docs); err != nil {
		return nil, err
	}
	if s.revisions != nil {
		if _, err := s.Snapshot(ctx, ws.TenantID, ws.SessionID, ws.ID, documentRevisionLabelClose,
			types.DocumentRevisionSourceClose, 0); err != nil {
			logger.Warnf(ctx, "[DocumentWorkspace] snapshot before demoting workspace=%s failed: %v", ws.ID, err)
		}
		if fresh, err := s.repo.GetByID(ctx, ws.ID); err == nil && fresh != nil {
			ws = fresh
		}
	}
	reader, err := s.files.GetFile(ctx, ws.CurrentRef)
	if err != nil {
		return nil, fmt.Errorf("open document: %w", err)
	}
	data, err := readCapped(reader, maxDocumentWorkspaceBytes())
	_ = reader.Close()
	if err != nil {
		return nil, fmt.Errorf("read document: %w", err)
	}
	content := docxPlainText(data)
	if err := s.repo.SaveText(ctx, &types.DocumentWorkspaceText{
		WorkspaceID: ws.ID, TenantID: ws.TenantID, Content: content,
	}); err != nil {
		return nil, err
	}
	ws.Role = types.DocumentWorkspaceRoleSource
	ws.TextStatus = types.DocumentSourceTextReady
	ws.ActiveAt = nil
	if err := s.repo.Update(ctx, ws); err != nil {
		return nil, err
	}
	logger.Infof(ctx, "[DocumentWorkspace] target=%s demoted to source (%s)", ws.ID, ws.Handle())
	return ws, nil
}

// docxPlainText is the non-empty paragraphs of a .docx, one per line.
func docxPlainText(data []byte) string {
	layout := docformat.InspectDocx(data)
	var b strings.Builder
	for _, p := range layout.Paragraphs {
		text := strings.TrimSpace(strings.NewReplacer("\n", " ", "\t", " ").Replace(p.Text))
		if text == "" {
			continue
		}
		b.WriteString(text)
		b.WriteString("\n")
	}
	return b.String()
}

func documentWorkspaceStorageNameExt(workspaceID, ext string) string {
	ext = strings.TrimPrefix(strings.ToLower(ext), ".")
	if ext == "" {
		ext = "bin"
	}
	return "document_workspace_" + workspaceID + "_" + uuid.NewString()[:8] + "." + ext
}
