package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// A session's document scope lives in Redis (with the format-check client,
// see UseFormatCheckRedis) for a day, else in process memory.
const (
	docScopeKeyPrefix = "werag:docscope:"
	docScopeTTL       = 24 * time.Hour
)

type storedScope struct {
	scope   *types.DocumentScope
	expires time.Time
}

var docScopes sync.Map // session ID → storedScope

// SessionDocumentScope returns the stored scope of a session, or nil.
func SessionDocumentScope(ctx context.Context, sessionID string) *types.DocumentScope {
	if sessionID == "" {
		return nil
	}
	if formatChecks.rdb != nil {
		var s types.DocumentScope
		if !formatChecks.redisGet(ctx, docScopeKeyPrefix+sessionID, &s) {
			return nil
		}
		return &s
	}
	v, ok := docScopes.Load(sessionID)
	if !ok {
		return nil
	}
	st := v.(storedScope)
	if time.Now().After(st.expires) {
		docScopes.Delete(sessionID)
		return nil
	}
	c := *st.scope
	return &c
}

// SetSessionDocumentScope stores the scope of a session (replacing any).
func SetSessionDocumentScope(ctx context.Context, sessionID string, scope *types.DocumentScope) {
	if sessionID == "" || scope == nil {
		return
	}
	c := *scope
	if formatChecks.rdb == nil {
		docScopes.Store(sessionID, storedScope{scope: &c, expires: time.Now().Add(docScopeTTL)})
		return
	}
	raw, err := json.Marshal(&c)
	if err != nil {
		return
	}
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), formatCheckRedisTimeout)
	defer cancel()
	if err := formatChecks.rdb.Set(rctx, docScopeKeyPrefix+sessionID, raw, docScopeTTL).Err(); err != nil {
		logger.Warnf(ctx, "[DocumentScope] redis set %s: %v", sessionID, err)
	}
}

// ClearSessionDocumentScope removes the scope of a session.
func ClearSessionDocumentScope(ctx context.Context, sessionID string) {
	docScopes.Delete(sessionID)
	if formatChecks.rdb == nil || sessionID == "" {
		return
	}
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), formatCheckRedisTimeout)
	defer cancel()
	if err := formatChecks.rdb.Del(rctx, docScopeKeyPrefix+sessionID).Err(); err != nil {
		logger.Warnf(ctx, "[DocumentScope] redis del %s: %v", sessionID, err)
	}
}

// liveScope keeps the part of scope that still names documents of the
// session (a closed tab drops out); nil when none is left.
func liveScope(scope *types.DocumentScope, docs []*types.DocumentWorkspace) *types.DocumentScope {
	if scope == nil {
		return nil
	}
	have := map[string]bool{}
	for _, d := range docs {
		have[d.ID] = true
	}
	out := *scope
	out.DocumentIDs = nil
	out.Sections = nil
	for _, id := range scope.DocumentIDs {
		if have[id] {
			out.DocumentIDs = append(out.DocumentIDs, id)
		}
	}
	for _, s := range scope.Sections {
		if have[s.DocumentID] {
			out.Sections = append(out.Sections, s)
		}
	}
	if len(out.DocumentIDs) == 0 {
		return nil
	}
	return &out
}

var scopeTaskVI = map[string]string{
	types.DocumentScopeTaskFormat:   "thể thức",
	types.DocumentScopeTaskSpelling: "chính tả",
	types.DocumentScopeTaskSummary:  "tóm tắt",
	types.DocumentScopeTaskLookup:   "tra cứu",
	types.DocumentScopeTaskCompare:  "đối chiếu",
	types.DocumentScopeTaskEdit:     "sửa",
}

// scopeLine is the scope's line in <session_documents>, e.g. "Phạm vi
// hiện tại: vb2 (Điều 3. Tổ chức [12–20]) · đối chiếu (do bộ định tuyến
// chọn)".
func scopeLine(scope *types.DocumentScope, docs []*types.DocumentWorkspace) string {
	if scope == nil {
		return ""
	}
	byID := map[string]*types.DocumentWorkspace{}
	for _, d := range docs {
		byID[d.ID] = d
	}
	var parts []string
	for _, id := range scope.DocumentIDs {
		d := byID[id]
		if d == nil {
			continue
		}
		part := d.Handle()
		var secs []string
		for _, s := range scope.SectionsOf(id) {
			title := clipRunes(s.Title, 40)
			if title == "" {
				title = "mục"
			}
			secs = append(secs, fmt.Sprintf("%s [%d–%d]", title, s.From, s.To))
		}
		if len(secs) > 0 {
			part += " (" + strings.Join(secs, "; ") + ")"
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return ""
	}
	line := "Phạm vi hiện tại: " + strings.Join(parts, ", ")
	if t := scopeTaskVI[scope.Task]; t != "" {
		line += " · " + t
	}
	if scope.SetBy == types.DocumentScopeSetByUser {
		line += " (do người dùng chọn)"
	} else {
		line += " (do bộ định tuyến chọn)"
	}
	return line
}
