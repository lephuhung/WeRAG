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
	GetBySession(ctx context.Context, tenantID uint64, sessionID string) (*types.DocumentWorkspace, error)
	GetByID(ctx context.Context, id string) (*types.DocumentWorkspace, error)
	// Update saves every column of ws.
	Update(ctx context.Context, ws *types.DocumentWorkspace) error
	// UpdateIfRevision saves ws only while the stored revision still equals
	// expectedRevision; it returns (false, nil) when another writer won.
	UpdateIfRevision(ctx context.Context, ws *types.DocumentWorkspace, expectedRevision int) (bool, error)
	Delete(ctx context.Context, tenantID uint64, sessionID string) error
}

// DocumentWorkspaceService owns the editable document of a chat session and
// the bridge to the ONLYOFFICE Document Server.
//
// Write protocol for anything that edits the file outside the editor (the
// agent's docx tools): call PrepareExternalWrite, modify the returned bytes,
// then CommitExternalWrite with the revision PrepareExternalWrite returned.
// Prepare force-saves the editor first so unsaved human edits are not lost;
// Commit stores the new bytes, bumps Revision (which rotates the editor key)
// and fails with an ErrConflict-coded error (errors.NewConflictError) when the revision moved.
type DocumentWorkspaceService interface {
	// Enabled reports whether the ONLYOFFICE integration is configured.
	Enabled() bool

	// CreateFromAttachment copies a session's temporary attachment (.docx or
	// .doc; .doc is converted to .docx) into a new workspace. A session has at
	// most one workspace; creating a second returns an error.
	CreateFromAttachment(ctx context.Context, tenantID uint64, sessionID, userID, attachmentID string) (*types.DocumentWorkspace, error)

	// GetBySession returns the workspace or an ErrNotFound-coded error.
	GetBySession(ctx context.Context, tenantID uint64, sessionID string) (*types.DocumentWorkspace, error)

	// View builds the API view including the signed editor config for user.
	View(ctx context.Context, ws *types.DocumentWorkspace, userID, userName, lang string) (*types.DocumentWorkspaceView, error)

	// OpenCurrent streams the latest version of the document.
	OpenCurrent(ctx context.Context, tenantID uint64, sessionID string) (io.ReadCloser, *types.DocumentWorkspace, error)

	// ForceSave asks the Document Server to flush unsaved editor changes. It
	// returns nil when a save was triggered or when there was nothing to save.
	ForceSave(ctx context.Context, tenantID uint64, sessionID string) error

	// PrepareExternalWrite force-saves, waits up to wait for the save callback
	// (returning early when there was nothing to save), and returns the latest
	// bytes with the workspace revision they belong to.
	PrepareExternalWrite(ctx context.Context, tenantID uint64, sessionID string, wait time.Duration) (*types.DocumentWorkspace, []byte, error)

	// CommitExternalWrite stores data as the new current version.
	CommitExternalWrite(ctx context.Context, tenantID uint64, sessionID string, expectedRevision int, data []byte) (*types.DocumentWorkspace, error)

	// HandleCallback processes a Document Server callback. ticket is the path
	// segment the callback URL was issued with; authorization is the raw
	// Authorization header value (Document Server JWT).
	HandleCallback(ctx context.Context, ticket string, authorization string, payload *types.OnlyOfficeCallback) error
}
