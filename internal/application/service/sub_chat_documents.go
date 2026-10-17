package service

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
)

// Sub-chats ("chat nhỏ") of a document-assistant chat work on the root
// chat's documents: the editor tabs (document_workspaces) and the chat
// uploads (temporary_documents) stay keyed by the root session, and each
// sub-chat only has its own messages and context. The two services below
// wrap the document services so every caller — HTTP handlers, the agent's
// document tools, the QA pipeline — passes a sub-chat's id and works on the
// root's rows without knowing about sub-chats. A root (or any ordinary
// session) resolves to itself, so resolving twice is harmless.

// chatRoots maps a session id to the session whose documents it works on.
// chat_root_id is set when a sub-chat is created and never changes, so a
// resolved value is cached for good (bounded by chatRootCacheMax).
type chatRoots struct {
	repo  interfaces.SessionRepository
	mu    sync.RWMutex
	cache map[string]string
}

const chatRootCacheMax = 50000

func newChatRoots(repo interfaces.SessionRepository) *chatRoots {
	return &chatRoots{repo: repo, cache: map[string]string{}}
}

func (r *chatRoots) resolve(ctx context.Context, tenantID uint64, sessionID string) string {
	if r == nil || r.repo == nil || sessionID == "" {
		return sessionID
	}
	key := sessionID
	r.mu.RLock()
	root, ok := r.cache[key]
	r.mu.RUnlock()
	if ok {
		return root
	}
	root, err := r.repo.ChatRootID(ctx, tenantID, sessionID)
	if err != nil {
		// not cached: a failed read degrades to the session itself
		logger.Warnf(ctx, "sub-chat root of session %s unavailable: %v", sessionID, err)
		return sessionID
	}
	r.mu.Lock()
	if len(r.cache) >= chatRootCacheMax {
		r.cache = map[string]string{}
	}
	r.cache[key] = root
	r.mu.Unlock()
	return root
}

// subChatWorkspaces is interfaces.DocumentWorkspaceService on the root
// chat's documents.
type subChatWorkspaces struct {
	interfaces.DocumentWorkspaceService
	roots *chatRoots
}

// NewSubChatWorkspaces wraps the document workspace service for sub-chats.
func NewSubChatWorkspaces(inner interfaces.DocumentWorkspaceService, sessions interfaces.SessionRepository) interfaces.DocumentWorkspaceService {
	return &subChatWorkspaces{DocumentWorkspaceService: inner, roots: newChatRoots(sessions)}
}

func (w *subChatWorkspaces) root(ctx context.Context, tenantID uint64, sessionID string) string {
	return w.roots.resolve(ctx, tenantID, sessionID)
}

// OnSourceTextReady forwards the hook DocumentFormatPrecheck looks for.
func (w *subChatWorkspaces) OnSourceTextReady(fn func(context.Context, *types.DocumentWorkspace)) {
	if n, ok := w.DocumentWorkspaceService.(interface {
		OnSourceTextReady(func(context.Context, *types.DocumentWorkspace))
	}); ok {
		n.OnSourceTextReady(fn)
	}
}

func (w *subChatWorkspaces) CreateFromAttachment(ctx context.Context, tenantID uint64, sessionID, userID, attachmentID string) (*types.DocumentWorkspace, error) {
	return w.DocumentWorkspaceService.CreateFromAttachment(ctx, tenantID, w.root(ctx, tenantID, sessionID), userID, attachmentID)
}

func (w *subChatWorkspaces) CreateFromAttachmentFor(ctx context.Context, tenantID uint64, sessionID, userID, attachmentID, editorKind string) (*types.DocumentWorkspace, error) {
	return w.DocumentWorkspaceService.CreateFromAttachmentFor(ctx, tenantID, w.root(ctx, tenantID, sessionID), userID, attachmentID, editorKind)
}

func (w *subChatWorkspaces) StoreClientSave(ctx context.Context, tenantID uint64, sessionID, documentID string, baseRevision int, data []byte) (*types.DocumentWorkspace, error) {
	return w.DocumentWorkspaceService.StoreClientSave(ctx, tenantID, w.root(ctx, tenantID, sessionID), documentID, baseRevision, data)
}

func (w *subChatWorkspaces) StoreTemplate(ctx context.Context, tenantID uint64, sessionID, documentID string, baseRevision int, data []byte) (*types.DocumentWorkspace, error) {
	return w.DocumentWorkspaceService.StoreTemplate(ctx, tenantID, w.root(ctx, tenantID, sessionID), documentID, baseRevision, data)
}

func (w *subChatWorkspaces) BindWordFile(ctx context.Context, tenantID uint64, sessionID, documentID, fileID, fileName string) (*types.DocumentWorkspace, error) {
	return w.DocumentWorkspaceService.BindWordFile(ctx, tenantID, w.root(ctx, tenantID, sessionID), documentID, fileID, fileName)
}

func (w *subChatWorkspaces) CreateSourceFromAttachment(ctx context.Context, tenantID uint64, sessionID, userID, attachmentID string) (*types.DocumentWorkspace, error) {
	return w.DocumentWorkspaceService.CreateSourceFromAttachment(ctx, tenantID, w.root(ctx, tenantID, sessionID), userID, attachmentID)
}

func (w *subChatWorkspaces) SetRole(ctx context.Context, tenantID uint64, sessionID, documentID, role string) (*types.DocumentWorkspace, error) {
	return w.DocumentWorkspaceService.SetRole(ctx, tenantID, w.root(ctx, tenantID, sessionID), documentID, role)
}

func (w *subChatWorkspaces) SourceText(ctx context.Context, tenantID uint64, sessionID, documentID string) (*types.DocumentWorkspaceText, *types.DocumentWorkspace, error) {
	return w.DocumentWorkspaceService.SourceText(ctx, tenantID, w.root(ctx, tenantID, sessionID), documentID)
}

func (w *subChatWorkspaces) GetBySession(ctx context.Context, tenantID uint64, sessionID string) (*types.DocumentWorkspace, error) {
	return w.DocumentWorkspaceService.GetBySession(ctx, tenantID, w.root(ctx, tenantID, sessionID))
}

func (w *subChatWorkspaces) Get(ctx context.Context, tenantID uint64, sessionID, documentID string) (*types.DocumentWorkspace, error) {
	return w.DocumentWorkspaceService.Get(ctx, tenantID, w.root(ctx, tenantID, sessionID), documentID)
}

func (w *subChatWorkspaces) List(ctx context.Context, tenantID uint64, sessionID string) ([]*types.DocumentWorkspace, error) {
	return w.DocumentWorkspaceService.List(ctx, tenantID, w.root(ctx, tenantID, sessionID))
}

func (w *subChatWorkspaces) Activate(ctx context.Context, tenantID uint64, sessionID, documentID string) (*types.DocumentWorkspace, error) {
	return w.DocumentWorkspaceService.Activate(ctx, tenantID, w.root(ctx, tenantID, sessionID), documentID)
}

func (w *subChatWorkspaces) Remove(ctx context.Context, tenantID uint64, sessionID, documentID string) error {
	return w.DocumentWorkspaceService.Remove(ctx, tenantID, w.root(ctx, tenantID, sessionID), documentID)
}

func (w *subChatWorkspaces) OpenCurrent(ctx context.Context, tenantID uint64, sessionID, documentID string) (io.ReadCloser, *types.DocumentWorkspace, error) {
	return w.DocumentWorkspaceService.OpenCurrent(ctx, tenantID, w.root(ctx, tenantID, sessionID), documentID)
}

func (w *subChatWorkspaces) ForceSave(ctx context.Context, tenantID uint64, sessionID, documentID string) error {
	return w.DocumentWorkspaceService.ForceSave(ctx, tenantID, w.root(ctx, tenantID, sessionID), documentID)
}

func (w *subChatWorkspaces) PrepareExternalWrite(ctx context.Context, tenantID uint64, sessionID, documentID string, wait time.Duration) (*types.DocumentWorkspace, []byte, error) {
	return w.DocumentWorkspaceService.PrepareExternalWrite(ctx, tenantID, w.root(ctx, tenantID, sessionID), documentID, wait)
}

func (w *subChatWorkspaces) CommitExternalWrite(ctx context.Context, tenantID uint64, sessionID, documentID string, expectedRevision int, data []byte) (*types.DocumentWorkspace, error) {
	return w.DocumentWorkspaceService.CommitExternalWrite(ctx, tenantID, w.root(ctx, tenantID, sessionID), documentID, expectedRevision, data)
}

func (w *subChatWorkspaces) Snapshot(ctx context.Context, tenantID uint64, sessionID, documentID, label, source string, wait time.Duration) (*types.DocumentRevision, error) {
	return w.DocumentWorkspaceService.Snapshot(ctx, tenantID, w.root(ctx, tenantID, sessionID), documentID, label, source, wait)
}

func (w *subChatWorkspaces) ListRevisions(ctx context.Context, tenantID uint64, sessionID, documentID string) ([]*types.DocumentRevision, error) {
	return w.DocumentWorkspaceService.ListRevisions(ctx, tenantID, w.root(ctx, tenantID, sessionID), documentID)
}

func (w *subChatWorkspaces) Restore(ctx context.Context, tenantID uint64, sessionID, documentID string, seq int) (*types.DocumentWorkspace, error) {
	return w.DocumentWorkspaceService.Restore(ctx, tenantID, w.root(ctx, tenantID, sessionID), documentID, seq)
}

// subChatUploads is interfaces.TemporaryDocumentService on the root chat's
// uploads: a file uploaded in any sub-chat belongs to the document chat.
type subChatUploads struct {
	inner interfaces.TemporaryDocumentService
	roots *chatRoots
}

// NewSubChatUploads wraps the chat-upload service for sub-chats.
func NewSubChatUploads(inner interfaces.TemporaryDocumentService, sessions interfaces.SessionRepository) interfaces.TemporaryDocumentService {
	return &subChatUploads{inner: inner, roots: newChatRoots(sessions)}
}

func (u *subChatUploads) root(ctx context.Context, tenantID uint64, sessionID string) string {
	return u.roots.resolve(ctx, tenantID, sessionID)
}

// OnDocumentParsed forwards the hook the workspace service looks for (a
// source copies its upload's text once parsing ends).
func (u *subChatUploads) OnDocumentParsed(fn func(ctx context.Context, tenantID uint64, sessionID, documentID string)) {
	if n, ok := u.inner.(temporaryDocumentReadyNotifier); ok {
		n.OnDocumentParsed(fn)
	}
}

func (u *subChatUploads) Create(ctx context.Context, tenantID uint64, sessionID, fileName, mimeType string, fileSize int64, reader io.Reader, options types.TemporaryDocumentCreateOptions) (*types.TemporaryDocument, error) {
	return u.inner.Create(ctx, tenantID, u.root(ctx, tenantID, sessionID), fileName, mimeType, fileSize, reader, options)
}

func (u *subChatUploads) Get(ctx context.Context, tenantID uint64, sessionID, documentID string) (*types.TemporaryDocument, error) {
	return u.inner.Get(ctx, tenantID, u.root(ctx, tenantID, sessionID), documentID)
}

func (u *subChatUploads) OpenFile(ctx context.Context, tenantID uint64, sessionID, documentID string) (io.ReadCloser, string, error) {
	return u.inner.OpenFile(ctx, tenantID, u.root(ctx, tenantID, sessionID), documentID)
}

func (u *subChatUploads) List(ctx context.Context, tenantID uint64, sessionID string) ([]*types.TemporaryDocument, error) {
	return u.inner.List(ctx, tenantID, u.root(ctx, tenantID, sessionID))
}

func (u *subChatUploads) Delete(ctx context.Context, tenantID uint64, sessionID, documentID string) error {
	return u.inner.Delete(ctx, tenantID, u.root(ctx, tenantID, sessionID), documentID)
}

func (u *subChatUploads) ResolveForPrompt(ctx context.Context, tenantID uint64, sessionID string, documentIDs []string, query string, opts types.TemporaryDocumentPromptOptions) (*types.TemporaryDocumentPromptResult, error) {
	return u.inner.ResolveForPrompt(ctx, tenantID, u.root(ctx, tenantID, sessionID), documentIDs, query, opts)
}

func (u *subChatUploads) Process(ctx context.Context, task *asynq.Task) error {
	return u.inner.Process(ctx, task)
}

func (u *subChatUploads) CleanupExpired(ctx context.Context) error {
	return u.inner.CleanupExpired(ctx)
}

var (
	_ interfaces.DocumentWorkspaceService = (*subChatWorkspaces)(nil)
	_ interfaces.TemporaryDocumentService = (*subChatUploads)(nil)
	_ temporaryDocumentReadyNotifier      = (*subChatUploads)(nil)
)
