package service

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// Snapshot timeline of a document workspace. AI edits are applied inside the
// editor by the assistant plugin, so the backend's job is to capture
// restorable versions: before an AI edit plan (source "ai"), on demand from
// the UI ("manual"), when the last editor closes ("close") and around a
// restore ("restore").

const (
	onlyOfficeUserdataPrefix = "werag:"

	documentRevisionLabelMaxRunes = 512
	// documentRevisionTagMaxRunes bounds the label carried in the forcesave
	// userdata; the stored label is the full one recorded by Snapshot.
	documentRevisionTagMaxRunes = 200

	documentRevisionLabelClose         = "Đóng tài liệu"
	documentRevisionLabelBeforeRestore = "Trước khi khôi phục"
	documentRevisionLabelManual        = "Lưu thủ công"
	documentRevisionLabelAI            = "Trước khi AI chỉnh sửa"
)

// encodeSnapshotTag is the userdata suffix of a labelled force-save:
// "<source>:<label>" for ai/restore/close, the bare label for manual.
func encodeSnapshotTag(label, source string) string {
	label = capRunes(strings.ReplaceAll(label, "|", "/"), documentRevisionTagMaxRunes)
	if source == types.DocumentRevisionSourceManual {
		// A manual label that happens to start with "ai:" must not be
		// mistaken for an AI snapshot.
		if _, _, ok := splitSourcePrefix(label); ok {
			return types.DocumentRevisionSourceManual + ":" + label
		}
		return label
	}
	return source + ":" + label
}

// parseSnapshotUserdata decodes "werag:<workspaceID>|<tag>" from a callback.
func parseSnapshotUserdata(workspaceID, userdata string) (label, source string, ok bool) {
	rest, found := strings.CutPrefix(userdata, onlyOfficeUserdataPrefix+workspaceID+"|")
	if !found || strings.TrimSpace(rest) == "" {
		return "", "", false
	}
	if src, lbl, ok := splitSourcePrefix(rest); ok {
		return lbl, src, true
	}
	return rest, types.DocumentRevisionSourceManual, true
}

func splitSourcePrefix(tag string) (source, label string, ok bool) {
	src, lbl, found := strings.Cut(tag, ":")
	if !found || !types.IsValidDocumentRevisionSource(src) {
		return "", "", false
	}
	return src, lbl, true
}

func capRunes(s string, max int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return strings.TrimSpace(string([]rune(s)[:max]))
}

func defaultRevisionLabel(source string) string {
	switch source {
	case types.DocumentRevisionSourceAI:
		return documentRevisionLabelAI
	case types.DocumentRevisionSourceClose:
		return documentRevisionLabelClose
	case types.DocumentRevisionSourceRestore:
		return documentRevisionLabelBeforeRestore
	default:
		return documentRevisionLabelManual
	}
}

func (s *documentWorkspaceService) Snapshot(
	ctx context.Context, tenantID uint64, sessionID, documentID, label, source string, wait time.Duration,
) (*types.DocumentRevision, error) {
	if s.revisions == nil {
		return nil, apperrors.NewServiceUnavailableError("document revisions are not configured")
	}
	if source == "" {
		source = types.DocumentRevisionSourceManual
	}
	if !types.IsValidDocumentRevisionSource(source) {
		return nil, apperrors.NewBadRequestError("invalid revision source")
	}
	label = capRunes(label, documentRevisionLabelMaxRunes)
	if label == "" {
		label = defaultRevisionLabel(source)
	}
	ws, err := s.getTarget(ctx, tenantID, sessionID, documentID)
	if err != nil {
		return nil, err
	}
	ws, err = s.flushEditor(ctx, ws, encodeSnapshotTag(label, source), wait)
	if err != nil {
		return nil, err
	}
	// When the save landed, the callback has already recorded this snapshot
	// and recordRevision returns that row.
	rev, err := s.recordRevision(ctx, ws, label, source, true)
	if err == nil && ws.IsWordAddin() && source == types.DocumentRevisionSourceAI {
		// an AI edit is about to be applied in Word: the next flush waits
		// for the taskpane to upload the result
		s.client.expect(ws.ID)
	}
	return rev, err
}

func (s *documentWorkspaceService) ListRevisions(
	ctx context.Context, tenantID uint64, sessionID, documentID string,
) ([]*types.DocumentRevision, error) {
	if s.revisions == nil {
		return nil, nil
	}
	ws, err := s.getTarget(ctx, tenantID, sessionID, documentID)
	if err != nil {
		return nil, err
	}
	return s.revisions.ListByWorkspace(ctx, ws.ID)
}

func (s *documentWorkspaceService) Restore(
	ctx context.Context, tenantID uint64, sessionID, documentID string, seq int,
) (*types.DocumentWorkspace, error) {
	if s.revisions == nil {
		return nil, apperrors.NewServiceUnavailableError("document revisions are not configured")
	}
	ws, err := s.getTarget(ctx, tenantID, sessionID, documentID)
	if err != nil {
		return nil, err
	}
	target, err := s.revisions.GetBySeq(ctx, ws.ID, seq)
	if err != nil {
		return nil, err
	}
	if target == nil {
		return nil, apperrors.NewNotFoundError("Revision not found")
	}
	// Keep the state being replaced restorable, including unsaved editor
	// changes.
	if _, err := s.Snapshot(ctx, tenantID, sessionID, ws.ID, documentRevisionLabelBeforeRestore,
		types.DocumentRevisionSourceRestore, 0); err != nil {
		return nil, fmt.Errorf("snapshot before restore: %w", err)
	}
	ws, err = s.Get(ctx, tenantID, sessionID, ws.ID)
	if err != nil {
		return nil, err
	}
	expected := ws.Revision
	ws.CurrentRef = target.Ref
	ws.FileSize = target.FileSize
	// A new key makes the open editor reload the restored file; saves still
	// under the old key are then ignored as stale.
	ws.Revision = expected + 1
	ok, err := s.repo.UpdateIfRevision(ctx, ws, expected)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, apperrors.NewConflictError("document changed during restore; try again")
	}
	s.bind(ctx, target.Ref, ws.ID, types.ResourceRelationArtifact)
	if ws.IsWordAddin() {
		// the taskpane writes the restored file into Word and uploads it
		s.client.forget(ws.ID)
	}
	if _, err := s.recordRevision(ctx, ws, fmt.Sprintf("Khôi phục bản #%d", seq),
		types.DocumentRevisionSourceRestore, false); err != nil {
		logger.Warnf(ctx, "[DocumentWorkspace] record restore revision failed: workspace=%s err=%v", ws.ID, err)
	}
	logger.Infof(ctx, "[DocumentWorkspace] restored revision #%d: workspace=%s revision=%d", seq, ws.ID, ws.Revision)
	return ws, nil
}

// recordRevision adds a timeline row pointing at ws.CurrentRef. With dedupe,
// an existing latest row for the same file is returned instead.
func (s *documentWorkspaceService) recordRevision(
	ctx context.Context, ws *types.DocumentWorkspace, label, source string, dedupe bool,
) (*types.DocumentRevision, error) {
	if dedupe {
		latest, err := s.revisions.Latest(ctx, ws.ID)
		if err != nil {
			return nil, err
		}
		if latest != nil && latest.Ref == ws.CurrentRef {
			return latest, nil
		}
	}
	rev := &types.DocumentRevision{
		WorkspaceID: ws.ID, TenantID: ws.TenantID, Ref: ws.CurrentRef,
		Label: capRunes(label, documentRevisionLabelMaxRunes), Source: source, FileSize: ws.FileSize,
	}
	if err := s.revisions.Create(ctx, rev); err != nil {
		return nil, err
	}
	return rev, nil
}

// recordRevisionBestEffort is recordRevision (deduplicated) for the callback
// path, where a failure must not fail the save that already succeeded.
func (s *documentWorkspaceService) recordRevisionBestEffort(
	ctx context.Context, ws *types.DocumentWorkspace, label, source string,
) {
	if s.revisions == nil {
		return
	}
	if _, err := s.recordRevision(ctx, ws, label, source, true); err != nil {
		logger.Warnf(ctx, "[DocumentWorkspace] record revision failed: workspace=%s source=%s err=%v", ws.ID, source, err)
	}
}
