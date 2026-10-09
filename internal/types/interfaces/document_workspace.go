package interfaces

import (
	"context"
	"io"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

// DocumentWorkspaceRepository persists DocumentWorkspace rows.
type DocumentWorkspaceRepository interface {
	Create(ctx context.Context, ws *types.DocumentWorkspace) error
	// GetBySession returns the session's active target (last activated,
	// else latest opened), or (nil, nil). Sources are never active.
	GetBySession(ctx context.Context, tenantID uint64, sessionID string) (*types.DocumentWorkspace, error)
	// ListBySession returns the session's documents of both roles in
	// handle order (position).
	ListBySession(ctx context.Context, tenantID uint64, sessionID string) ([]*types.DocumentWorkspace, error)
	// NextPosition is the position of the next document opened in the
	// session (positions are never reused).
	NextPosition(ctx context.Context, tenantID uint64, sessionID string) (int, error)
	// SetActive makes the document the session's active one.
	SetActive(ctx context.Context, id string, at time.Time) error
	// DeleteByID soft-deletes one document of the session.
	DeleteByID(ctx context.Context, tenantID uint64, sessionID, id string) error
	GetByID(ctx context.Context, id string) (*types.DocumentWorkspace, error)
	// Update saves every column of ws.
	Update(ctx context.Context, ws *types.DocumentWorkspace) error
	// UpdateIfRevision saves ws only while the stored revision still equals
	// expectedRevision; it returns (false, nil) when another writer won.
	UpdateIfRevision(ctx context.Context, ws *types.DocumentWorkspace, expectedRevision int) (bool, error)
	Delete(ctx context.Context, tenantID uint64, sessionID string) error
	// SaveText stores or replaces the text of a source document.
	SaveText(ctx context.Context, text *types.DocumentWorkspaceText) error
	// GetText returns a source's stored text, or (nil, nil).
	GetText(ctx context.Context, workspaceID string) (*types.DocumentWorkspaceText, error)
}

// DocumentWorkspaceService owns the editable documents of a chat session and
// the bridge to the ONLYOFFICE Document Server.
//
// Write protocol for anything that edits the file outside the editor (the
// agent's docx tools): call PrepareExternalWrite, modify the returned bytes,
// then CommitExternalWrite with the revision PrepareExternalWrite returned.
// Prepare force-saves the editor first so unsaved human edits are not lost;
// Commit stores the new bytes, bumps Revision (which rotates the editor key)
// and fails with an ErrConflict-coded error (errors.NewConflictError) when the revision moved.
type DocumentWorkspaceService interface {
	// Enabled reports whether the ONLYOFFICE integration is configured: the
	// embedded editor, its saves and .doc conversion need it.
	Enabled() bool

	// DocumentsEnabled reports whether session documents are on: sources,
	// Word add-in targets, the format check and the document context in the
	// prompt. They need no Document Server, so it does not depend on Enabled.
	DocumentsEnabled() bool

	// CreateFromAttachment copies a session's temporary attachment (.docx or
	// .doc; .doc is converted to .docx) into a new workspace and makes it the
	// active one. Opening an upload that is already open returns its
	// workspace. A session holds at most MaxDocumentWorkspacesPerSession
	// documents, and a file over the size or embedded-picture limits is
	// refused (see checkDocumentWorkspaceSize).
	CreateFromAttachment(ctx context.Context, tenantID uint64, sessionID, userID, attachmentID string) (*types.DocumentWorkspace, error)

	// CreateFromAttachmentFor is CreateFromAttachment for a given editor
	// kind (types.DocumentEditorKind*). A Word add-in target needs no
	// Document Server and takes .docx only.
	CreateFromAttachmentFor(ctx context.Context, tenantID uint64, sessionID, userID, attachmentID, editorKind string) (*types.DocumentWorkspace, error)

	// StoreClientSave stores the file the Word add-in uploaded as a Word
	// add-in target's latest version and releases the tools waiting for it.
	// baseRevision is the revision the taskpane last saw; a restore since
	// then answers a conflict.
	StoreClientSave(ctx context.Context, tenantID uint64, sessionID, documentID string, baseRevision int, data []byte) (*types.DocumentWorkspace, error)

	// CreateSourceFromAttachment records a chat upload of the session as a
	// source document: a copy of the file now, its parsed text once the
	// upload is parsed (TextStatus processing until then). An upload that
	// already has a document returns it. A session holds at most
	// MaxDocumentSourcesPerSession sources (409 beyond); image uploads are
	// refused (they stay plain attachments).
	CreateSourceFromAttachment(ctx context.Context, tenantID uint64, sessionID, userID, attachmentID string) (*types.DocumentWorkspace, error)

	// SetRole switches a document's role. To target: a Word source gets an
	// editor file (a .doc is converted) and keeps its handle; refused for a
	// non-Word file or at MaxDocumentWorkspacesPerSession targets. To
	// source: the target is snapshotted, its paragraphs become its stored
	// text and it leaves the tab strip.
	SetRole(ctx context.Context, tenantID uint64, sessionID, documentID, role string) (*types.DocumentWorkspace, error)

	// SourceText returns a source's stored text (copied from the upload
	// first when its parsing finished since), with the row. A target is a
	// bad request; a source still being parsed, or whose parsing failed, a
	// conflict.
	SourceText(ctx context.Context, tenantID uint64, sessionID, documentID string) (*types.DocumentWorkspaceText, *types.DocumentWorkspace, error)

	// GetBySession returns the session's active target or an
	// ErrNotFound-coded error.
	GetBySession(ctx context.Context, tenantID uint64, sessionID string) (*types.DocumentWorkspace, error)

	// Get returns one workspace of the session (documentID "" = the active
	// one) or an ErrNotFound-coded error. Every method below that takes a
	// documentID resolves it the same way.
	Get(ctx context.Context, tenantID uint64, sessionID, documentID string) (*types.DocumentWorkspace, error)

	// List returns the session's documents of both roles in handle order.
	List(ctx context.Context, tenantID uint64, sessionID string) ([]*types.DocumentWorkspace, error)

	// Activate makes the target the session's active one (the tab the
	// user is looking at). The editor-only methods below (Activate, View's
	// editor config, ForceSave, PrepareExternalWrite, CommitExternalWrite,
	// Snapshot, ListRevisions, Restore) answer a source with a 409.
	Activate(ctx context.Context, tenantID uint64, sessionID, documentID string) (*types.DocumentWorkspace, error)

	// Remove closes a document's tab: it snapshots the current state and
	// soft-deletes the workspace.
	Remove(ctx context.Context, tenantID uint64, sessionID, documentID string) error

	// View builds the API view including the signed editor config for user.
	View(ctx context.Context, ws *types.DocumentWorkspace, userID, userName, lang string) (*types.DocumentWorkspaceView, error)

	// OpenCurrent streams the latest version of the document.
	OpenCurrent(ctx context.Context, tenantID uint64, sessionID, documentID string) (io.ReadCloser, *types.DocumentWorkspace, error)

	// ForceSave asks the Document Server to flush unsaved editor changes. It
	// returns nil when a save was triggered or when there was nothing to save.
	ForceSave(ctx context.Context, tenantID uint64, sessionID, documentID string) error

	// PrepareExternalWrite force-saves, waits up to wait for the save callback
	// (returning early when there was nothing to save), and returns the latest
	// bytes with the workspace revision they belong to.
	PrepareExternalWrite(ctx context.Context, tenantID uint64, sessionID, documentID string, wait time.Duration) (*types.DocumentWorkspace, []byte, error)

	// CommitExternalWrite stores data as the new current version.
	CommitExternalWrite(ctx context.Context, tenantID uint64, sessionID, documentID string, expectedRevision int, data []byte) (*types.DocumentWorkspace, error)

	// HandleCallback processes a Document Server callback. ticket is the path
	// segment the callback URL was issued with; authorization is the raw
	// Authorization header value (Document Server JWT).
	HandleCallback(ctx context.Context, ticket string, authorization string, payload *types.OnlyOfficeCallback) error

	// Snapshot force-saves the editor, waits up to wait for the save callback
	// (no wait when there was nothing to save) and records a revision that
	// points at the resulting CurrentRef. source is one of the
	// types.DocumentRevisionSource* values. When the latest revision already
	// points at the same file, that revision is returned instead of a new one.
	Snapshot(ctx context.Context, tenantID uint64, sessionID, documentID, label, source string, wait time.Duration) (*types.DocumentRevision, error)

	// ListRevisions returns the workspace timeline, newest first.
	ListRevisions(ctx context.Context, tenantID uint64, sessionID, documentID string) ([]*types.DocumentRevision, error)

	// Restore makes revision seq the current version: it snapshots the
	// current state first, then points CurrentRef at the revision's file and
	// bumps Revision (rotating the editor key so the editor reloads).
	Restore(ctx context.Context, tenantID uint64, sessionID, documentID string, seq int) (*types.DocumentWorkspace, error)
}

// DocumentRevisionRepository persists the snapshot timeline of a workspace.
type DocumentRevisionRepository interface {
	// Create assigns rev.Seq (one more than the workspace's latest) and
	// inserts the row.
	Create(ctx context.Context, rev *types.DocumentRevision) error
	// ListByWorkspace returns every revision, newest (highest seq) first.
	ListByWorkspace(ctx context.Context, workspaceID string) ([]*types.DocumentRevision, error)
	// GetBySeq returns (nil, nil) when the workspace has no such revision.
	GetBySeq(ctx context.Context, workspaceID string, seq int) (*types.DocumentRevision, error)
	// Latest returns the highest-seq revision, or (nil, nil) when none.
	Latest(ctx context.Context, workspaceID string) (*types.DocumentRevision, error)
}
