package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/docformat"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"
)

// The format check takes about a minute (segmentation, then a reasoning
// evaluation), so its finished evaluations are kept per file content: the
// document assistant runs it in the background when a conversation's
// document is opened, and the user's "thể thức thế nào" is then answered
// from the cache. With Redis (UseFormatCheckRedis) results and session
// states survive a server restart; without it they live in this process.
const (
	formatCheckCacheTTL     = 2 * time.Hour
	formatCheckCacheEntries = 32
	// formatCheckRunTimeout bounds one shared run, which outlives the turn
	// that started it. A "running" state older than this belongs to a run
	// that died with its process and no longer counts.
	formatCheckRunTimeout = 6 * time.Minute

	formatCheckResultKeyPrefix = "werag:docformat:result:"
	formatCheckStateKeyPrefix  = "werag:docformat:state:"
	formatCheckRedisTimeout    = 2 * time.Second
)

// formatCheckResult is a finished, evaluated check of one file content.
type formatCheckResult struct {
	Output       string                      `json:"output"`
	FileName     string                      `json:"file_name"`
	DocumentType *docformat.DocumentTypeInfo `json:"document_type,omitempty"`
	Summary      *docformat.Summary          `json:"summary,omitempty"`
	Method       string                      `json:"method"`
	Skills       []string                    `json:"skills"`
	Evaluated    bool                        `json:"evaluated"`
	// Evaluation is the model's judgment alone (Output wraps it for the
	// agent); empty on results cached before it was kept.
	Evaluation string    `json:"evaluation,omitempty"`
	At         time.Time `json:"at"`
	// Flags are the measured findings that name their paragraphs, marked
	// in the editor when the user asks for the check (format_marks.go);
	// empty on results cached before they were kept.
	Flags []formatFlag `json:"flags,omitempty"`
}

// evaluation is the judgment to show the user, without the agent framing.
func (r *formatCheckResult) evaluation() string {
	if r.Evaluation != "" {
		return r.Evaluation
	}
	out := r.Output
	if i := strings.Index(out, "\n\n"); i >= 0 && strings.HasPrefix(out, "# ") {
		out = out[i+2:]
	}
	if i := strings.LastIndex(out, "\n\n---\n"); i >= 0 {
		out = out[:i]
	}
	return strings.TrimSpace(out)
}

// data is the tool result data of the check.
func (r *formatCheckResult) data() map[string]interface{} {
	return map[string]interface{}{
		"file_name":     r.FileName,
		"document_type": r.DocumentType,
		"summary":       r.Summary,
		"method":        r.Method,
		"skills":        r.Skills,
		"evaluated":     r.Evaluated,
	}
}

type formatCheckCache struct {
	mu      sync.Mutex
	entries map[string]*formatCheckResult
	group   singleflight.Group
	// prewarmed holds the sessions this process already checked in the
	// background, so each conversation costs at most one unasked run.
	prewarmed sync.Map
	// states holds each prewarmed session's *types.DocumentFormatCheck
	// when Redis is not configured.
	states sync.Map
	rdb    *redis.Client
}

var formatChecks = &formatCheckCache{entries: map[string]*formatCheckResult{}}

// UseFormatCheckRedis keeps format-check results and session states in
// Redis, shared by restarts; nil keeps them in process memory.
func UseFormatCheckRedis(rdb *redis.Client) {
	formatChecks.rdb = rdb
}

// formatCheckKey identifies a check: the file bytes, the model that labels
// and judges it, and the requested rule set ("" = detected).
func formatCheckKey(content []byte, model, docType string) string {
	return formatCheckIdentity(content) + "|" + model + "|" + strings.ToLower(strings.TrimSpace(docType))
}

func (c *formatCheckCache) redisGet(ctx context.Context, key string, into interface{}) bool {
	if c.rdb == nil {
		return false
	}
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), formatCheckRedisTimeout)
	defer cancel()
	raw, err := c.rdb.Get(rctx, key).Bytes()
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			logger.Warnf(ctx, "[FormatCheck] redis get %s: %v", key, err)
		}
		return false
	}
	return json.Unmarshal(raw, into) == nil
}

func (c *formatCheckCache) redisSet(ctx context.Context, key string, v interface{}) {
	if c.rdb == nil {
		return
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return
	}
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), formatCheckRedisTimeout)
	defer cancel()
	if err := c.rdb.Set(rctx, key, raw, formatCheckCacheTTL).Err(); err != nil {
		logger.Warnf(ctx, "[FormatCheck] redis set %s: %v", key, err)
	}
}

func (c *formatCheckCache) get(ctx context.Context, key string) *formatCheckResult {
	c.mu.Lock()
	r := c.entries[key]
	if r != nil && time.Since(r.At) > formatCheckCacheTTL {
		delete(c.entries, key)
		r = nil
	}
	c.mu.Unlock()
	if r != nil {
		return r
	}
	var stored formatCheckResult
	if !c.redisGet(ctx, formatCheckResultKeyPrefix+key, &stored) {
		return nil
	}
	c.remember(&stored, key)
	return &stored
}

// remember keeps r in the process cache under keys.
func (c *formatCheckCache) remember(r *formatCheckResult, keys ...string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, k := range keys {
		c.entries[k] = r
	}
	for len(c.entries) > formatCheckCacheEntries {
		oldest, oldestAt := "", time.Time{}
		for k, e := range c.entries {
			if oldest == "" || e.At.Before(oldestAt) {
				oldest, oldestAt = k, e.At
			}
		}
		delete(c.entries, oldest)
	}
}

func (c *formatCheckCache) put(ctx context.Context, r *formatCheckResult, keys ...string) {
	c.remember(r, keys...)
	for _, k := range keys {
		c.redisSet(ctx, formatCheckResultKeyPrefix+k, r)
	}
}

func (c *formatCheckCache) storeState(ctx context.Context, documentID string, st *types.DocumentFormatCheck) {
	if c.rdb == nil {
		c.states.Store(documentID, st)
		return
	}
	c.redisSet(ctx, formatCheckStateKeyPrefix+documentID, st)
}

// SessionFormatCheck returns the background format check of an editable
// document (workspace ID), or nil when none was started or the run that
// started it died.
func SessionFormatCheck(ctx context.Context, documentID string) *types.DocumentFormatCheck {
	var st types.DocumentFormatCheck
	if formatChecks.rdb != nil {
		if !formatChecks.redisGet(ctx, formatCheckStateKeyPrefix+documentID, &st) {
			return nil
		}
	} else {
		v, ok := formatChecks.states.Load(documentID)
		if !ok {
			return nil
		}
		st = *v.(*types.DocumentFormatCheck)
	}
	if st.InProgress() && time.Since(st.StartedAt) > formatCheckRunTimeout {
		return nil
	}
	return &st
}

// formatCheckDocumentKey holds a document's latest background result
// (workspace ID), so it can be shown without the file bytes.
func formatCheckDocumentKey(documentID string) string {
	return "doc:" + documentID
}

// SessionFormatCheckReport returns the evaluation of a document's finished
// background check (workspace ID), or nil when none is kept.
func SessionFormatCheckReport(ctx context.Context, documentID string) *types.DocumentFormatReport {
	r := formatChecks.get(ctx, formatCheckDocumentKey(documentID))
	if r == nil || !r.Evaluated {
		return nil
	}
	report := &types.DocumentFormatReport{FileName: r.FileName, Evaluation: r.evaluation(), CheckedAt: r.At}
	if info := r.DocumentType; info != nil {
		report.DocumentType = info.Used
		report.DocumentTypeLabel = documentTypeLabel(info.RuleSet)
	}
	if s := r.Summary; s != nil {
		report.Summary = &types.DocumentFormatSummary{Pass: s.Pass, Fail: s.Fail, Warn: s.Warn, Skip: s.Skip}
	}
	return report
}

// documentTypeLabel turns a rule set label such as
// "Quy chế (NĐ30/2020, Phụ lục I)" into the type name "Quy chế".
func documentTypeLabel(ruleSet string) string {
	if i := strings.Index(ruleSet, " ("); i > 0 {
		return ruleSet[:i]
	}
	return ruleSet
}

// formatCheckOutcome is what one run returns to every caller waiting on it.
type formatCheckOutcome struct {
	result  *formatCheckResult // nil when the check failed
	failure string             // tool error text when result is nil
}

// do returns the cached check for key or runs it once, shared by every
// concurrent caller of the same key. The run is detached from ctx: a turn
// that stops waiting does not cancel it for the others or for the cache.
func (c *formatCheckCache) do(ctx context.Context, key string, run func(context.Context) formatCheckOutcome) formatCheckOutcome {
	if r := c.get(ctx, key); r != nil {
		return formatCheckOutcome{result: r}
	}
	ch := c.group.DoChan(key, func() (interface{}, error) {
		if r := c.get(ctx, key); r != nil {
			return formatCheckOutcome{result: r}, nil
		}
		runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), formatCheckRunTimeout)
		defer cancel()
		return run(runCtx), nil
	})
	select {
	case res := <-ch:
		return res.Val.(formatCheckOutcome)
	case <-ctx.Done():
		return formatCheckOutcome{failure: "format check interrupted: " + ctx.Err().Error()}
	}
}
