package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// Word add-in targets. Microsoft Word holds the live document, so the
// backend cannot force-save it the way it asks the Document Server: the
// taskpane uploads the file instead (StoreClientSave) before every chat
// turn and after it applies an edit plan. An AI snapshot (taken right
// before a tool returns its plan) marks the workspace as expecting such an
// upload; the next flush waits for it, so a second editing tool in the same
// turn reads the document with the first tool's edits applied.

// clientSaveMaxWait caps the wait for the taskpane's upload: a tool that
// snapshotted and then returned no plan leaves the mark behind, and only
// the next upload clears it.
const clientSaveMaxWait = 10 * time.Second

// clientSaves is the in-memory state of Word add-in uploads, per workspace.
// It is per process: after a restart the first upload is stored even when
// unchanged, and nothing is expected.
type clientSaves struct {
	mu       sync.Mutex
	hash     map[string]string
	expected map[string]bool
}

func newClientSaves() clientSaves {
	return clientSaves{hash: map[string]string{}, expected: map[string]bool{}}
}

func contentHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// stored records the bytes now in CurrentRef and clears the expectation.
func (c *clientSaves) stored(id string, data []byte) {
	c.mu.Lock()
	c.hash[id] = contentHash(data)
	delete(c.expected, id)
	c.mu.Unlock()
}

// unchanged reports whether data is what CurrentRef already holds.
func (c *clientSaves) unchanged(id string, data []byte) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	h, ok := c.hash[id]
	return ok && h == contentHash(data)
}

func (c *clientSaves) expect(id string) {
	c.mu.Lock()
	c.expected[id] = true
	c.mu.Unlock()
}

func (c *clientSaves) isExpected(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.expected[id]
}

func (c *clientSaves) forget(id string) {
	c.mu.Lock()
	delete(c.hash, id)
	delete(c.expected, id)
	c.mu.Unlock()
}

// awaitClientSave is flushEditor for a Word add-in target: it waits for the
// upload an AI snapshot announced, if one is pending, at most wait (capped
// by clientSaveMaxWait). Like the ONLYOFFICE flush, only a cancelled ctx is
// an error; a late upload means the caller works on the last stored version.
func (s *documentWorkspaceService) awaitClientSave(
	ctx context.Context, ws *types.DocumentWorkspace, wait time.Duration,
) (*types.DocumentWorkspace, error) {
	if !s.client.isExpected(ws.ID) {
		return ws, nil
	}
	waiter := s.waiterFor(ws.ID)
	// the upload may have landed between the check and the registration
	if !s.client.isExpected(ws.ID) {
		s.dropWaiter(ws.ID, waiter)
	} else {
		if wait <= 0 || wait > clientSaveMaxWait {
			wait = clientSaveMaxWait
		}
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-waiter.ch:
		case <-timer.C:
			s.dropWaiter(ws.ID, waiter)
			logger.Warnf(ctx, "[DocumentWorkspace] Word add-in upload did not arrive within %s, proceeding: workspace=%s", wait, ws.ID)
		case <-ctx.Done():
			s.dropWaiter(ws.ID, waiter)
			return nil, ctx.Err()
		}
	}
	if fresh, err := s.repo.GetByID(ctx, ws.ID); err == nil && fresh != nil {
		return fresh, nil
	}
	return ws, nil
}

// StoreClientSave stores the file the Word add-in uploaded as the target's
// latest version. baseRevision is the revision the taskpane last saw: a
// restore since then (which the taskpane has not written into Word yet)
// makes the upload stale, answered with a conflict. An upload identical to
// the stored file only releases the waiting tools.
func (s *documentWorkspaceService) StoreClientSave(
	ctx context.Context, tenantID uint64, sessionID, documentID string, baseRevision int, data []byte,
) (*types.DocumentWorkspace, error) {
	if len(data) == 0 {
		return nil, apperrors.NewBadRequestError("document is empty")
	}
	ws, err := s.getTarget(ctx, tenantID, sessionID, documentID)
	if err != nil {
		return nil, err
	}
	if !ws.IsWordAddin() {
		return nil, apperrors.NewBadRequestError("this document is edited in the embedded editor, not in Word")
	}
	if baseRevision != ws.Revision {
		return nil, apperrors.NewConflictError("document was restored to another version; reload it in Word")
	}
	if err := checkDocumentWorkspaceSize(ws.FileName, data); err != nil {
		return nil, err
	}
	if s.client.unchanged(ws.ID, data) {
		s.client.stored(ws.ID, data)
		s.signal(ws.ID, nil)
		return ws, nil
	}
	ref, err := s.files.SaveBytes(ctx, data, tenantID, documentWorkspaceStorageName(ws.ID), false)
	if err != nil {
		return nil, fmt.Errorf("store uploaded document: %w", err)
	}
	s.bind(ctx, ref, ws.ID, types.ResourceRelationArtifact)
	now := time.Now()
	ws.CurrentRef = ref
	ws.FileSize = int64(len(data))
	ws.SaveCount++
	ws.LastSavedAt = &now
	ws.Status = types.DocumentWorkspaceStatusOpen
	ws.ClosedAt = nil
	ok, err := s.repo.UpdateIfRevision(ctx, ws, baseRevision)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, apperrors.NewConflictError("document was restored to another version; reload it in Word")
	}
	s.client.stored(ws.ID, data)
	s.signal(ws.ID, nil)
	logger.Infof(ctx, "[DocumentWorkspace] Word add-in save stored: workspace=%s size=%d save_count=%d",
		ws.ID, ws.FileSize, ws.SaveCount)
	return ws, nil
}
