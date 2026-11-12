package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/legalcatalog"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
)

const (
	// legalExpiredAtKey (knowledges.metadata) is when the system first saw
	// the document fully expired: the retention period counts from there,
	// since the portal may report an expiry long after its date.
	legalExpiredAtKey       = "eff_expired_at"
	legalRetentionActor     = "system:expired-retention"
	legalRetentionTick      = time.Hour
	legalRetentionBatch     = 20
	legalRetentionRemovedAs = "retention_removed"
)

// LegalExpiredRetention removes the full text of fully expired documents
// from the public KBs once their retention period is over. Only the
// knowledge goes: the catalogue row (số hiệu, metadata, relations) stays and
// still answers "this document is no longer in force".
type LegalExpiredRetention struct {
	db       *gorm.DB
	kbs      interfaces.KnowledgeBaseService
	tenants  interfaces.TenantRepository
	settings interface {
		GetInt(ctx context.Context, key string, envName string, def int64) int64
	}
	remove func(ctx context.Context, id string) error
	now    func() time.Time
	// runs records the passes that changed something (the SuperAdmin monitor)
	runs legalRunRecorder
}

// WithRuns makes the worker record its passes.
func (w *LegalExpiredRetention) WithRuns(r legalRunRecorder) *LegalExpiredRetention {
	w.runs = r
	return w
}

// NewLegalExpiredRetention creates the worker.
func NewLegalExpiredRetention(
	db *gorm.DB, knowledge interfaces.KnowledgeService, kbs interfaces.KnowledgeBaseService,
	tenants interfaces.TenantRepository, settings interfaces.SystemSettingService,
) *LegalExpiredRetention {
	return &LegalExpiredRetention{
		db: db, kbs: kbs, tenants: tenants, settings: settings,
		remove: knowledge.DeleteKnowledge, now: time.Now,
	}
}

// StartLegalExpiredRetention runs the worker until the returned func is
// called; the first pass starts the clock of documents already expired.
func StartLegalExpiredRetention(w *LegalExpiredRetention) (stop func()) {
	noteLegalWorkerStarted(types.LegalSyncWorkerExpiredRetention, legalRetentionTick, time.Now())
	done := make(chan struct{})
	go func() {
		w.Tick(context.Background())
		ticker := time.NewTicker(legalRetentionTick)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				w.Tick(context.Background())
			case <-done:
				return
			}
		}
	}()
	return func() { close(done) }
}

// legalRetentionFrom selects the public-KB documents naming a vbpl row.
const legalRetentionFrom = ` FROM knowledges k
	JOIN knowledge_bases kb ON kb.id = k.knowledge_base_id AND kb.owner_tenant_id = 0 AND kb.deleted_at IS NULL
	JOIN legal_documents d ON d.id = k.metadata->>'external_id' AND d.source = 'vbpl'
	WHERE k.deleted_at IS NULL AND k.parse_status <> 'deleting' `

// Tick updates the clocks and removes one batch of documents past their
// period. Returns how many it removed.
func (w *LegalExpiredRetention) Tick(ctx context.Context) (removed int) {
	now := w.now()
	noteLegalWorkerPassStart(types.LegalSyncWorkerExpiredRetention, legalRetentionTick, now)
	var started, cleared, failed, kept int
	tickErr, lastErr := "", ""
	// A bare goroutine: a panic in the delete path must not take the server down.
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf(ctx, "[LegalExpiredRetention] panic: %v", r)
			tickErr = fmt.Sprintf("panic: %v", r)
		}
		beatErr := w.record(ctx, now, map[string]int{"removed": removed, "clocks_started": started, "clocks_cleared": cleared,
			"failed": failed, "kept_live_copy": kept}, lastErr, tickErr)
		noteLegalWorkerBeat(types.LegalSyncWorkerExpiredRetention, legalRetentionTick, w.now(), beatErr)
	}()
	var err error
	if started, cleared, err = w.updateClocks(ctx, now); err != nil {
		logger.Warnf(ctx, "[LegalExpiredRetention] clocks: %v", err)
		tickErr = "clocks: " + err.Error()
		return 0
	}
	days := w.settings.GetInt(ctx, types.LegalExpiredRetentionDaysSettingKey, "", types.LegalExpiredRetentionDefaultDays)
	if days < 1 { // never read as "delete at once"
		days = types.LegalExpiredRetentionDefaultDays
	}
	var rows []struct {
		ID, KnowledgeBaseID, ExternalID, DocumentNumber, NumberKey, ExpiredAt string
	}
	if err := w.db.WithContext(ctx).Raw(`SELECT k.id, k.knowledge_base_id, d.id AS external_id,
		d.document_number, d.number_key, k.metadata->>'`+legalExpiredAtKey+`' AS expired_at`+legalRetentionFrom+
		`AND d.status LIKE ? AND k.metadata->>'`+legalExpiredAtKey+`' IS NOT NULL`, LegalExpiredStatusPrefix+"%").
		Scan(&rows).Error; err != nil {
		logger.Warnf(ctx, "[LegalExpiredRetention] listing: %v", err)
		tickErr = "listing: " + err.Error()
		return 0
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ExpiredAt < rows[j].ExpiredAt })
	cutoff := now.AddDate(0, 0, -int(days))
	keys := make([]string, 0, len(rows))
	for _, r := range rows {
		keys = append(keys, r.NumberKey)
	}
	byKey, err := w.catalogueRows(ctx, keys)
	if err != nil {
		logger.Warnf(ctx, "[LegalExpiredRetention] catalogue rows: %v", err)
		tickErr = "catalogue rows: " + err.Error()
		return 0
	}
	for _, r := range rows {
		if removed == legalRetentionBatch {
			break
		}
		at, err := time.Parse(time.RFC3339, r.ExpiredAt)
		if err != nil || at.After(cutoff) {
			continue
		}
		if legalHasLiveCopy(r.ExternalID, byKey[r.NumberKey]) {
			logger.Infof(ctx, "[LegalExpiredRetention] %s (%s) kept: another row of the document is in force",
				r.ExternalID, r.DocumentNumber)
			kept++
			continue
		}
		if err := w.removeOne(ctx, r.ID, r.KnowledgeBaseID); err != nil {
			logger.Warnf(ctx, "[LegalExpiredRetention] %s (%s): %v", r.ExternalID, r.DocumentNumber, err)
			failed++
			lastErr = r.DocumentNumber + ": " + err.Error()
			continue
		}
		removed++
		WriteLegalFeedLog(ctx, w.db, LegalFeedActor{UserID: legalRetentionActor}, LegalFeedEntry{
			Action: legalRetentionRemovedAs, VBPLID: r.ExternalID, KnowledgeID: r.ID, KnowledgeBaseID: r.KnowledgeBaseID,
			DocumentNumber: r.DocumentNumber, Reason: "Hết thời hạn giữ toàn văn của văn bản hết hiệu lực toàn bộ",
		})
		logger.Infof(ctx, "[LegalExpiredRetention] %s (%s): full text removed, catalogue row kept", r.ExternalID, r.DocumentNumber)
	}
	return removed
}

// record writes a pass that removed a text, started or cleared a clock, or
// failed: a pass with nothing to do leaves only the heartbeat. (A document
// kept for a live copy is seen on every pass, so it alone is no reason.)
// Returns the error of a failed pass (the heartbeat shows it); a panic here
// is caught, not the server's.
func (w *LegalExpiredRetention) record(ctx context.Context, started time.Time, counts map[string]int, lastErr, tickErr string) (failed string) {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf(ctx, "[LegalExpiredRetention] recording panic: %v", r)
		}
	}()
	status, errText := types.LegalSyncOK, lastErr
	if tickErr != "" || (counts["failed"] > 0 && counts["removed"] == 0) {
		status = types.LegalSyncFailed
		if tickErr != "" {
			errText = tickErr
		}
		failed = errText
	}
	if w.runs == nil {
		return failed
	}
	if tickErr == "" && counts["removed"]+counts["clocks_started"]+counts["clocks_cleared"]+counts["failed"] == 0 {
		return failed
	}
	summary := map[string]any{}
	for k, n := range counts {
		summary[k] = n
	}
	if err := w.runs.Record(ctx, types.LegalSyncWorkerExpiredRetention, started, status, errText, summary); err != nil {
		logger.Warnf(ctx, "[LegalExpiredRetention] recording the pass: %v", err)
	}
	return failed
}

// catalogueRows loads the catalogue rows of the numbers, by number key.
func (w *LegalExpiredRetention) catalogueRows(ctx context.Context, keys []string) (map[string][]*types.LegalDocument, error) {
	out := map[string][]*types.LegalDocument{}
	for start := 0; start < len(keys); start += 1000 {
		var docs []*types.LegalDocument
		if err := w.db.WithContext(ctx).Model(&types.LegalDocument{}).
			Select("id", "number_key", "doc_type", "issuer", "issued_on", "status").
			Where("number_key IN ?", keys[start:min(start+1000, len(keys))]).Find(&docs).Error; err != nil {
			return nil, err
		}
		for _, d := range docs {
			out[d.NumberKey] = append(out[d.NumberKey], d)
		}
	}
	return out, nil
}

// legalHasLiveCopy reports whether another catalogue row of the same document
// is not fully expired: the portal sometimes lists one document twice and
// expires only one row, and the text must not go while the other is in force.
func legalHasLiveCopy(id string, rows []*types.LegalDocument) bool {
	var self *types.LegalDocument
	for _, d := range rows {
		if d.ID == id {
			self = d
		}
	}
	if self == nil {
		return true // the row changed under us: leave the text for the next pass
	}
	for _, d := range rows {
		if d.ID != id && !strings.HasPrefix(strings.TrimSpace(d.Status), LegalExpiredStatusPrefix) && legalcatalog.SameIssue(self, d) {
			return true
		}
	}
	return false
}

// updateClocks starts the clock of newly expired documents and clears that
// of documents the portal put back in force; it returns how many of each.
func (w *LegalExpiredRetention) updateClocks(ctx context.Context, now time.Time) (int, int, error) {
	set := "json_set(metadata, '$." + legalExpiredAtKey + "', ?)"
	clear := "json_remove(metadata, '$." + legalExpiredAtKey + "')"
	if w.db.Dialector.Name() == "postgres" {
		set = "jsonb_set(metadata::jsonb, '{" + legalExpiredAtKey + "}', to_jsonb(?::text))"
		clear = "metadata::jsonb - '" + legalExpiredAtKey + "'"
	}
	var start, stop []string
	db := w.db.WithContext(ctx)
	if err := db.Raw(`SELECT k.id`+legalRetentionFrom+`AND d.status LIKE ? AND k.metadata->>'`+legalExpiredAtKey+`' IS NULL`,
		LegalExpiredStatusPrefix+"%").Scan(&start).Error; err != nil {
		return 0, 0, err
	}
	if err := db.Raw(`SELECT k.id`+legalRetentionFrom+`AND d.status NOT LIKE ? AND k.metadata->>'`+legalExpiredAtKey+`' IS NOT NULL`,
		LegalExpiredStatusPrefix+"%").Scan(&stop).Error; err != nil {
		return 0, 0, err
	}
	stamp := now.UTC().Format(time.RFC3339)
	err := db.Transaction(func(tx *gorm.DB) error {
		for _, id := range start {
			if err := tx.Exec("UPDATE knowledges SET metadata = "+set+" WHERE id = ? AND metadata->>'"+legalExpiredAtKey+"' IS NULL",
				stamp, id).Error; err != nil {
				return err
			}
		}
		for _, id := range stop {
			if err := tx.Exec("UPDATE knowledges SET metadata = "+clear+" WHERE id = ?", id).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	return len(start), len(stop), nil
}

// removeOne deletes one knowledge with a write grant on its own KB.
func (w *LegalExpiredRetention) removeOne(ctx context.Context, id, kbID string) error {
	kb, err := w.kbs.GetKnowledgeBaseByIDOnly(ctx, kbID)
	if err != nil {
		return err
	}
	if kb == nil {
		return fmt.Errorf("knowledge base %s not found", kbID)
	}
	scoped, err := access.WithKBTaskWrite(ctx, kb, kb.TenantID)
	if err != nil {
		return err
	}
	if scoped, err = withKBWriteTenantInfo(scoped, kb, w.tenants); err != nil {
		return err
	}
	return w.remove(scoped, id)
}
