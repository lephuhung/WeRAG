package tools

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/docformat"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"golang.org/x/sync/singleflight"
)

// Profiles are cached like format-check results (same Redis client and
// TTL, see UseFormatCheckRedis): a finished profile per plain-text hash and
// model, and a per-document state holding the profile shown for it.
const (
	docProfileResultKeyPrefix = "werag:docprofile:result:"
	docProfileStateKeyPrefix  = "werag:docprofile:state:"
	docProfileCacheEntries    = 64
	// docProfileRetryAfter: a failed profile is tried again this long after
	// its run started (a model that was down may be back).
	docProfileRetryAfter = 10 * time.Minute
)

type docProfileStore struct {
	mu      sync.Mutex
	results map[string]*types.DocumentProfile
	// states holds each document's *types.DocumentProfile without Redis.
	states sync.Map
	// queued holds the documents with a background job in this process.
	queued sync.Map
	group  singleflight.Group
}

var docProfiles = &docProfileStore{results: map[string]*types.DocumentProfile{}}

func docProfileKey(textHash, model string) string { return textHash + "|" + model }

func (s *docProfileStore) result(ctx context.Context, key string) *types.DocumentProfile {
	s.mu.Lock()
	p := s.results[key]
	if p != nil && p.GeneratedAt != nil && time.Since(*p.GeneratedAt) > formatCheckCacheTTL {
		delete(s.results, key)
		p = nil
	}
	s.mu.Unlock()
	if p != nil {
		c := *p
		return &c
	}
	var stored types.DocumentProfile
	if !formatChecks.redisGet(ctx, docProfileResultKeyPrefix+key, &stored) {
		return nil
	}
	s.remember(key, &stored)
	return &stored
}

func (s *docProfileStore) remember(key string, p *types.DocumentProfile) {
	c := *p
	s.mu.Lock()
	defer s.mu.Unlock()
	s.results[key] = &c
	for len(s.results) > docProfileCacheEntries {
		oldest, oldestAt := "", time.Time{}
		for k, e := range s.results {
			at := time.Time{}
			if e.GeneratedAt != nil {
				at = *e.GeneratedAt
			}
			if oldest == "" || at.Before(oldestAt) {
				oldest, oldestAt = k, at
			}
		}
		delete(s.results, oldest)
	}
}

func (s *docProfileStore) putResult(ctx context.Context, key string, p *types.DocumentProfile) {
	s.remember(key, p)
	formatChecks.redisSet(ctx, docProfileResultKeyPrefix+key, p)
}

func (s *docProfileStore) storeState(ctx context.Context, documentID string, p *types.DocumentProfile) {
	c := *p
	if formatChecks.rdb == nil {
		s.states.Store(documentID, &c)
		return
	}
	formatChecks.redisSet(ctx, docProfileStateKeyPrefix+documentID, &c)
}

// SessionDocumentProfile returns the profile state of a session document
// (workspace ID): the profile shown for it, with its status. nil when none
// was made, or when the run that started one died without a profile.
func SessionDocumentProfile(ctx context.Context, documentID string) *types.DocumentProfile {
	var st types.DocumentProfile
	if formatChecks.rdb != nil {
		if !formatChecks.redisGet(ctx, docProfileStateKeyPrefix+documentID, &st) {
			return nil
		}
	} else {
		v, ok := docProfiles.states.Load(documentID)
		if !ok {
			return nil
		}
		st = *v.(*types.DocumentProfile)
	}
	if st.InProgress() && time.Since(st.StartedAt) > formatCheckRunTimeout {
		if st.TextHash == "" {
			return nil
		}
		st.Status = types.DocumentProfileReady // the last profile, still shown
	}
	return &st
}

// DocumentProfileNeed tells what ws's profile needs: start (none yet, a
// source whose text changed, a role change, a failure worth retrying) or a
// scheduled refresh (a target edited since its profile). Cheap: it reads
// only the state, so callers test it before resolving a model.
func DocumentProfileNeed(ctx context.Context, ws *types.DocumentWorkspace) (start, refresh bool) {
	if ws == nil || (ws.IsSource() && ws.TextStatus != types.DocumentSourceTextReady) {
		return false, false
	}
	st := SessionDocumentProfile(ctx, ws.ID)
	switch {
	case st == nil:
		return true, false
	case st.InProgress():
		return false, false
	case st.Role != "" && st.Role != types.DocumentRoleOf(ws):
		return true, false
	case st.Status == types.DocumentProfileFailed:
		return time.Since(st.StartedAt) > docProfileRetryAfter, false
	case st.Describes(ws):
		return false, false
	case ws.IsSource():
		return true, false
	}
	return false, true
}

// DocumentProfiler makes the profiles of one session's documents with the
// document assistant's model (nil: every profile fails with "no model").
type DocumentProfiler struct {
	workspace DocumentWorkspaceSource
	chatModel chat.Chat
	sessionID string
	identity  ProfileIdentityFunc
}

// NewDocumentProfiler builds the profiler of one session.
func NewDocumentProfiler(workspace DocumentWorkspaceSource, chatModel chat.Chat, sessionID string) *DocumentProfiler {
	return &DocumentProfiler{workspace: workspace, chatModel: chatModel, sessionID: sessionID}
}

// WithIdentity sets how the số hiệu and type are reconciled with the header
// (the knowledge-base profile's applyLegalIdentity).
func (p *DocumentProfiler) WithIdentity(fn ProfileIdentityFunc) *DocumentProfiler {
	c := *p
	c.identity = fn
	return &c
}

func (p *DocumentProfiler) modelName() string {
	if p.chatModel == nil {
		return ""
	}
	return p.chatModel.GetModelName()
}

// Start queues the profile of ws as a background job, which takes one of
// the format-check slots (formatCheckSlots), at most one job per document
// in this process. The previous profile stays shown, flagged stale, until
// the new one is ready. It returns at once.
func (p *DocumentProfiler) Start(ctx context.Context, ws *types.DocumentWorkspace) {
	if p == nil || ws == nil || p.sessionID == "" {
		return
	}
	if _, busy := docProfiles.queued.LoadOrStore(ws.ID, struct{}{}); busy {
		return
	}
	queued := p.progressState(ctx, ws, types.DocumentProfileQueued)
	docProfiles.storeState(ctx, ws.ID, queued)
	ctx = context.WithoutCancel(ctx)
	go func() {
		defer docProfiles.queued.Delete(ws.ID)
		release, ok := waitProfileSlot(ctx, ws.ID, queued)
		if !ok {
			return
		}
		defer release()
		p.run(ctx, ws.ID)
	}()
}

// ensureFirst makes the document's first profile synchronously when it has
// none ready: the background format check calls it inside its slot, so the
// profile of a document opened with its check is made before the check's
// evaluation. A run already under way for the document is joined.
func (p *DocumentProfiler) ensureFirst(ctx context.Context, documentID string) {
	if p == nil {
		return
	}
	if st := SessionDocumentProfile(ctx, documentID); st != nil && st.Status == types.DocumentProfileReady {
		return
	}
	p.run(ctx, documentID)
}

// Refresh follows the schedule after a target's saves: the profile is
// kept when the plain text did not change (a formatting-only save), and
// made again otherwise. It returns once the check is done; a new profile
// is queued (see Start).
func (p *DocumentProfiler) Refresh(ctx context.Context, documentID string) {
	tenantID, ok := types.TenantIDFromContext(ctx)
	if p == nil || !ok {
		return
	}
	ws, err := p.workspace.Get(ctx, tenantID, p.sessionID, documentID)
	if err != nil || ws == nil {
		return
	}
	in, row, err := p.input(ctx, ws)
	if err == nil {
		st := SessionDocumentProfile(ctx, documentID)
		if st != nil && st.Status == types.DocumentProfileReady && st.TextHash == in.hash() && st.Model == p.modelName() {
			kept := *st
			kept.Role, kept.Revision, kept.SaveCount = types.DocumentRoleOf(row), row.Revision, row.SaveCount
			docProfiles.storeState(ctx, documentID, &kept)
			logger.Infof(ctx, "[DocumentProfile] text of %s unchanged since its profile; kept", documentID)
			return
		}
	}
	p.Start(ctx, ws)
}

// progressState is the state shown while ws's profile is queued or runs:
// the previous profile, if any, with the new status.
func (p *DocumentProfiler) progressState(ctx context.Context, ws *types.DocumentWorkspace, status string) *types.DocumentProfile {
	st := SessionDocumentProfile(ctx, ws.ID)
	if st == nil || st.TextHash == "" {
		st = &types.DocumentProfile{Role: types.DocumentRoleOf(ws), Revision: ws.Revision, SaveCount: ws.SaveCount}
	}
	st.Status, st.Error, st.StartedAt = status, "", time.Now()
	return st
}

// input reads what ws's profile is made from, and the row it was read at.
func (p *DocumentProfiler) input(ctx context.Context, ws *types.DocumentWorkspace) (*profileInput, *types.DocumentWorkspace, error) {
	if ws.IsSource() {
		reader, ok := p.workspace.(sourceTextReader)
		tenantID, hasTenant := types.TenantIDFromContext(ctx)
		if !ok || !hasTenant {
			return nil, nil, errors.New("source text unavailable")
		}
		text, row, err := reader.SourceText(ctx, tenantID, p.sessionID, ws.ID)
		if err != nil {
			return nil, nil, err
		}
		if row == nil {
			row = ws
		}
		return sourceProfileInput(row, text), row, nil
	}
	_, layout, row, err := readWorkspaceLayout(ctx, p.workspace, p.sessionID, ws)
	if err != nil {
		return nil, nil, err
	}
	return targetProfileInput(row, layout), row, nil
}

// run makes (or reuses) the profile of one document and records it as the
// document's state. Concurrent runs of a document share one.
func (p *DocumentProfiler) run(ctx context.Context, documentID string) {
	_, _, _ = docProfiles.group.Do(documentID, func() (interface{}, error) {
		runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), formatCheckRunTimeout)
		defer cancel()
		p.runOnce(runCtx, documentID)
		return nil, nil
	})
}

func (p *DocumentProfiler) runOnce(ctx context.Context, documentID string) {
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok {
		return
	}
	ws, err := p.workspace.Get(ctx, tenantID, p.sessionID, documentID)
	if err != nil || ws == nil {
		return
	}
	fail := func(row *types.DocumentWorkspace, msg string) {
		st := p.progressState(ctx, row, types.DocumentProfileFailed)
		st.Error = msg
		docProfiles.storeState(ctx, documentID, st)
		logger.Warnf(ctx, "[DocumentProfile] profile of %s failed: %s", documentID, msg)
	}
	in, row, err := p.input(ctx, ws)
	if err != nil {
		fail(ws, err.Error())
		return
	}
	model := p.modelName()
	hash := in.hash()
	finish := func(prof *types.DocumentProfile) {
		prof.Status, prof.Error = types.DocumentProfileReady, ""
		prof.Role, prof.Revision, prof.SaveCount = types.DocumentRoleOf(row), row.Revision, row.SaveCount
		docProfiles.storeState(ctx, documentID, prof)
	}
	if st := SessionDocumentProfile(ctx, documentID); st != nil && st.TextHash == hash && st.Model == model && st.GeneratedAt != nil {
		finish(st)
		return
	}
	key := docProfileKey(hash, model)
	if cached := docProfiles.result(ctx, key); cached != nil {
		finish(cached)
		return
	}
	if p.chatModel == nil {
		fail(row, "no model")
		return
	}
	running := p.progressState(ctx, row, types.DocumentProfileRunning)
	docProfiles.storeState(ctx, documentID, running)
	started := time.Now()
	prof, err := generateDocumentProfile(ctx, docformat.ChatCompleter(p.chatModel), in, p.identity)
	if err != nil {
		fail(row, err.Error())
		return
	}
	now := time.Now()
	prof.TextHash, prof.Model, prof.GeneratedAt = hash, model, &now
	prof.Status = types.DocumentProfileReady
	docProfiles.putResult(ctx, key, prof)
	finish(prof)
	logger.Infof(ctx, "[DocumentProfile] profile of %s (%s, %d sections) ready in %s",
		documentID, row.Handle(), len(prof.Sections), now.Sub(started).Round(time.Second))
}

// waitProfileSlot takes a format-check slot, recording the queued state
// meanwhile (refreshed so SessionDocumentProfile keeps counting it).
func waitProfileSlot(ctx context.Context, documentID string, queued *types.DocumentProfile) (release func(), ok bool) {
	if release, ok := tryFormatCheckSlot(); ok {
		return release, true
	}
	slots := formatCheckSlots
	ticker := time.NewTicker(formatCheckQueuePoll)
	defer ticker.Stop()
	for {
		select {
		case slots <- struct{}{}:
			return func() { <-slots }, true
		case <-ticker.C:
			st := *queued
			st.StartedAt = time.Now()
			docProfiles.storeState(ctx, documentID, &st)
		case <-ctx.Done():
			return nil, false
		}
	}
}
