package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// A replacement keeps the old public copy of a legal document readable until
// the new copy is parsed (IngestHTML). The new copy carries
// types.LegalReplacesKnowledgeKey; the swap (FinishReplacement) runs when its
// chunks are written (the post-parse legal hook) and, restart-safe, from the
// auto-ingest worker's sweep (SweepIngests), which also cleans copies whose
// parse failed and puts their request back, at most
// types.LegalRequestParseFailureMax times.

const (
	// legalReplaceStartedAction / legalReplacedAction are the legal_feed_log
	// rows of a replacement: accepted (the old copy stays), then done (the
	// old copy deleted; same detail plus the ids).
	legalReplaceStartedAction = "ingest_replace_started"
	legalReplacedAction       = "ingest_replaced"
	// legalReplaceFailedAction: the new copy of a replacement failed to parse
	// and was deleted, the old one stays; legalIngestFailedAction: the same
	// for a first copy a request was closed with.
	legalReplaceFailedAction = "replace_failed"
	legalIngestFailedAction  = "ingest_failed"
	legalReplaceActor        = "system:legal-replace"
	// legalReplaceRefusedAction: a marker named a copy of another document
	// or KB: not deleted (the marker is still cleared)
	legalReplaceRefusedAction = "replace_refused"
	// legalReplaceStuckAfter: a replacement copy parsing for longer than this
	// is given up like a failed one.
	legalReplaceStuckAfter = 6 * time.Hour
	// legalReplaceSweepGrace: the sweep leaves a parsed copy's swap to the
	// parse's own hook for this long.
	legalReplaceSweepGrace = 10 * time.Minute
)

// legalCopyRow is one public-KB knowledge the swap and the sweep work on.
type legalCopyRow struct {
	ID, KnowledgeBaseID, ExternalID, ParseStatus, EnableStatus, Replaces, ErrorMessage string
}

func (r *legalCopyRow) readable() bool { return types.LegalCopyReadable(r.ParseStatus, r.EnableStatus) }

const legalCopySelect = `SELECT k.id, k.knowledge_base_id, COALESCE(k.metadata->>'external_id', '') AS external_id,
	COALESCE(k.parse_status, '') AS parse_status, COALESCE(k.enable_status, '') AS enable_status,
	COALESCE(k.metadata->>'` + types.LegalReplacesKnowledgeKey + `', '') AS replaces,
	COALESCE(k.error_message, '') AS error_message
	FROM knowledges k
	JOIN knowledge_bases kb ON kb.id = k.knowledge_base_id AND kb.owner_tenant_id = 0 AND kb.deleted_at IS NULL
	WHERE k.deleted_at IS NULL`

func (s *LegalFeedService) copyRow(ctx context.Context, id string) (*legalCopyRow, error) {
	var rows []legalCopyRow
	if err := s.db.WithContext(ctx).Raw(legalCopySelect+` AND k.id = ?`, id).Scan(&rows).Error; err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

// taskCtx is ctx with the write grant on kbID the system workers use (no
// caller): the swap runs in the parse task or the sweep.
func (s *LegalFeedService) taskCtx(ctx context.Context, kbID string) (context.Context, error) {
	if s.KBs == nil {
		return ctx, nil // tests: the knowledge service fake checks no grant
	}
	kb, err := s.KBs.GetKnowledgeBaseByIDOnly(ctx, kbID)
	if err != nil {
		return nil, err
	}
	if kb == nil {
		return nil, fmt.Errorf("knowledge base %s not found", kbID)
	}
	scoped, err := access.WithKBTaskWrite(ctx, kb, kb.TenantID)
	if err != nil {
		return nil, err
	}
	return withKBWriteTenantInfo(scoped, kb, s.Tenants)
}

// FinishReplacement swaps a parsed replacement in: for each old copy it names
// (still there), a reviewer's decision on the old link carries over to the
// new link, the old link is dropped once the new copy has one
// (retireReplacedLink), the requests pointing at the old copy point at the
// new one, and the old copy is deleted; then the marker is cleared and
// "ingest_replaced" logged (by whoever cleared it). Idempotent: a copy without the marker,
// not readable yet, or whose old copies are gone already, is fine. Returns
// whether a swap was done.
func (s *LegalFeedService) FinishReplacement(ctx context.Context, newID string) (bool, error) {
	if s == nil || s.db == nil || s.knowledge == nil || newID == "" {
		return false, nil
	}
	c, err := s.copyRow(ctx, newID)
	if err != nil || c == nil || c.Replaces == "" || !c.readable() {
		return false, err
	}
	dctx, err := s.taskCtx(ctx, c.KnowledgeBaseID)
	if err != nil {
		return false, fmt.Errorf("write grant on %s: %w", c.KnowledgeBaseID, err)
	}
	olds := strings.Split(c.Replaces, ",")
	for _, old := range olds {
		old = strings.TrimSpace(old)
		if old == "" || old == newID {
			continue
		}
		o, err := s.copyRow(ctx, old)
		if err != nil {
			return false, err
		}
		if o == nil {
			continue // gone already (an earlier swap, retention, a person)
		}
		if o.ExternalID == "" || o.ExternalID != c.ExternalID || o.KnowledgeBaseID != c.KnowledgeBaseID {
			// a marker never deletes another document's copy
			logger.Warnf(ctx, "[LegalFeed] replacement %s (%s in %s) names %s (%s in %s): not deleted",
				newID, c.ExternalID, c.KnowledgeBaseID, old, o.ExternalID, o.KnowledgeBaseID)
			WriteLegalFeedLog(ctx, s.db, LegalFeedActor{UserID: legalReplaceActor}, LegalFeedEntry{
				Action: legalReplaceRefusedAction, VBPLID: c.ExternalID, KnowledgeID: newID, KnowledgeBaseID: c.KnowledgeBaseID,
				Reason: "Bản thay thế ghi một bản khác văn bản hoặc khác kho: không xoá",
				Detail: map[string]any{"new_knowledge_id": newID, "new_external_id": c.ExternalID, "new_knowledge_base_id": c.KnowledgeBaseID,
					"old_knowledge_id": old, "old_external_id": o.ExternalID, "old_knowledge_base_id": o.KnowledgeBaseID},
			})
			continue
		}
		s.carryReview(ctx, old, newID)
		if err := s.retireReplacedLink(ctx, old, newID); err != nil {
			return false, err
		}
		if err := s.db.WithContext(ctx).Exec(`UPDATE legal_document_requests SET knowledge_id = ?, updated_at = ? WHERE knowledge_id = ?`,
			newID, time.Now(), old).Error; err != nil {
			return false, fmt.Errorf("requests of %s: %w", old, err)
		}
		if err := s.knowledge.DeleteKnowledge(dctx, old); err != nil {
			return false, fmt.Errorf("delete the replaced copy %s: %w", old, err)
		}
	}
	res := s.db.WithContext(ctx).Exec(`UPDATE knowledges SET metadata = `+s.withoutMetadataKey(types.LegalReplacesKnowledgeKey)+
		` WHERE id = ? AND metadata->>'`+types.LegalReplacesKnowledgeKey+`' IS NOT NULL`, newID)
	if res.Error != nil {
		return false, fmt.Errorf("clear the replacement marker of %s: %w", newID, res.Error)
	}
	if res.RowsAffected == 0 {
		return false, nil // another run swapped it meanwhile
	}
	started := s.replaceStarted(ctx, newID)
	detail := started.detail
	if detail == nil {
		detail = map[string]any{"replaced_knowledge_id": olds[0]}
	}
	detail["old_knowledge_ids"], detail["new_knowledge_id"] = olds, newID
	WriteLegalFeedLog(ctx, s.db, LegalFeedActor{UserID: legalReplaceActor}, LegalFeedEntry{
		Action: legalReplacedAction, VBPLID: c.ExternalID, KnowledgeID: newID, KnowledgeBaseID: c.KnowledgeBaseID,
		DocumentNumber: started.documentNumber, Reason: started.reason, Detail: detail,
	})
	logger.Infof(ctx, "[LegalFeed] %s: replacement %s swapped in, %v deleted", c.ExternalID, newID, olds)
	return true, nil
}

// withoutMetadataKey is metadata with key removed (both dialects).
func (s *LegalFeedService) withoutMetadataKey(key string) string {
	if s.db.Dialector.Name() == "postgres" {
		return "(metadata::jsonb - '" + key + "')"
	}
	return "json_remove(metadata, '$." + key + "')"
}

type legalReplaceStart struct {
	detail                 map[string]any
	documentNumber, reason string
}

// replaceStarted is the "ingest_replace_started" row of newID (the detail
// the swap's row repeats).
func (s *LegalFeedService) replaceStarted(ctx context.Context, newID string) legalReplaceStart {
	var rows []struct {
		DocumentNumber, Reason string
		Detail                 string
	}
	if err := s.db.WithContext(ctx).Raw(`SELECT document_number, reason, CAST(detail AS TEXT) AS detail FROM legal_feed_log
		WHERE action = ? AND knowledge_id = ? ORDER BY id DESC LIMIT 1`, legalReplaceStartedAction, newID).Scan(&rows).Error; err != nil ||
		len(rows) == 0 {
		return legalReplaceStart{}
	}
	out := legalReplaceStart{documentNumber: rows[0].DocumentNumber, reason: rows[0].Reason}
	_ = json.Unmarshal([]byte(rows[0].Detail), &out.detail)
	return out
}

// carryReview copies a reviewer's decision on the old copy's link to the
// new copy's (made after the replacement started, or missing).
func (s *LegalFeedService) carryReview(ctx context.Context, oldID, newID string) {
	if s.Reviews == nil {
		return
	}
	old, err := s.Reviews.GetLink(ctx, oldID)
	if err != nil || !legalHumanReview(old) {
		return
	}
	cur, err := s.Reviews.GetLink(ctx, newID)
	if err != nil || legalHumanReview(cur) {
		return
	}
	link := *old
	if cur != nil {
		if cur.LegalDocumentID != old.LegalDocumentID {
			return // the new text was identified otherwise: its own review stands
		}
		link = *cur
		link.ReviewStatus, link.ReviewedBy, link.ReviewedAt = old.ReviewStatus, old.ReviewedBy, old.ReviewedAt
	}
	link.KnowledgeID = newID
	if err := s.Reviews.SaveLink(ctx, &link); err != nil {
		logger.Warnf(ctx, "[LegalFeed] carrying the review of %s to %s: %v", oldID, newID, err)
	}
}

// retireReplacedLink drops the legal_document_links row of a replaced copy,
// which would otherwise point at a deleted knowledge after the swap. The new
// copy keeps the document linked: it normally has its own link from the
// ingest (saveReviewLink) carrying the reviewer's decision (carryReview runs
// first); when it has none, the old link is copied to it. The delete itself
// only runs while the new copy has a link, so an approved document is never
// left without one. A failure stops the swap before the old copy is
// deleted, so the sweep retries it.
func (s *LegalFeedService) retireReplacedLink(ctx context.Context, oldID, newID string) error {
	if s.Reviews != nil {
		cur, err := s.Reviews.GetLink(ctx, newID)
		if err != nil {
			return fmt.Errorf("link of %s: %w", newID, err)
		}
		if cur == nil {
			old, err := s.Reviews.GetLink(ctx, oldID)
			if err != nil {
				return fmt.Errorf("link of %s: %w", oldID, err)
			}
			if old != nil {
				link := *old
				link.KnowledgeID, link.CreatedAt, link.UpdatedAt = newID, time.Time{}, time.Time{}
				if err := s.Reviews.SaveLink(ctx, &link); err != nil {
					return fmt.Errorf("link of %s from %s: %w", newID, oldID, err)
				}
			}
		}
	}
	if err := s.db.WithContext(ctx).Exec(`DELETE FROM legal_document_links WHERE knowledge_id = ?
		AND EXISTS (SELECT 1 FROM legal_document_links n WHERE n.knowledge_id = ?)`, oldID, newID).Error; err != nil {
		return fmt.Errorf("drop the link of the replaced copy %s: %w", oldID, err)
	}
	return nil
}

// cleanFailedCopy deletes a copy whose parse failed (or a replacement stuck
// parsing) with its own legal_document_links row (the ingest linked it): the
// old copy of a replacement stays readable and linked. It is logged
// (replace_failed / ingest_failed, with the parse error) and each request
// closed with it goes back to its queue: the worker's (auto_ingest) for a
// vbpl.vn document, the operators' (requested) for another; after
// types.LegalRequestParseFailureMax failures the worker gives up on it
// (auto_ingest_skip, shown in the SuperAdmin queue).
func (s *LegalFeedService) cleanFailedCopy(ctx context.Context, c *legalCopyRow, why string) error {
	dctx, err := s.taskCtx(ctx, c.KnowledgeBaseID)
	if err != nil {
		return fmt.Errorf("write grant on %s: %w", c.KnowledgeBaseID, err)
	}
	var reqs []struct {
		ID, LegalDocumentID, Source string
		Detail                      string
	}
	if err := s.db.WithContext(ctx).Raw(`SELECT r.id, r.legal_document_id, COALESCE(d.source, '') AS source, CAST(r.detail AS TEXT) AS detail
		FROM legal_document_requests r LEFT JOIN legal_documents d ON d.id = r.legal_document_id
		WHERE r.knowledge_id = ?`, c.ID).Scan(&reqs).Error; err != nil {
		return fmt.Errorf("requests of %s: %w", c.ID, err)
	}
	// before the knowledge: a failure here leaves the copy for the sweep to retry
	if err := s.db.WithContext(ctx).Exec(`DELETE FROM legal_document_links WHERE knowledge_id = ?`, c.ID).Error; err != nil {
		return fmt.Errorf("drop the link of the failed copy %s: %w", c.ID, err)
	}
	if err := s.knowledge.DeleteKnowledge(dctx, c.ID); err != nil {
		return fmt.Errorf("delete the failed copy %s: %w", c.ID, err)
	}
	action := legalIngestFailedAction
	detail := map[string]any{"parse_status": c.ParseStatus, "error": why}
	if c.Replaces != "" {
		action = legalReplaceFailedAction
		detail["old_knowledge_ids"] = strings.Split(c.Replaces, ",")
	}
	ids := make([]string, 0, len(reqs))
	now := time.Now()
	for _, r := range reqs {
		ids = append(ids, r.ID)
		failures := 1
		var d map[string]any
		if json.Unmarshal([]byte(r.Detail), &d) == nil {
			if n, ok := d[types.LegalRequestDetailParseFailures].(float64); ok {
				failures = int(n) + 1
			}
		}
		status := types.LegalRequestAutoIngest
		if r.Source != types.LegalDocumentSourceVBPL {
			status = types.LegalRequestRequested // a feed's text: the operators fetch it again
		}
		if err := s.db.WithContext(ctx).Exec(`UPDATE legal_document_requests SET status = ?, knowledge_id = '', decided_by = '',
			decided_at = NULL, decision_note = '', claimed_by = '', claimed_at = NULL, updated_at = ? WHERE id = ? AND knowledge_id = ?`,
			status, now, r.ID, c.ID).Error; err != nil {
			return fmt.Errorf("reopen request %s: %w", r.ID, err)
		}
		note := map[string]any{types.LegalRequestDetailParseFailures: failures,
			types.LegalRequestDetailAutoIngestError: "phân tích toàn văn thất bại: " + why}
		if failures >= types.LegalRequestParseFailureMax {
			note[types.LegalRequestDetailAutoIngestSkip] = fmt.Sprintf("phân tích toàn văn thất bại %d lần: %s", failures, why)
		}
		if s.requests != nil {
			if _, err := s.requests.UpdateDetail(ctx, r.ID, note); err != nil {
				logger.Warnf(ctx, "[LegalFeed] noting the failed parse on request %s: %v", r.ID, err)
			}
		}
	}
	if len(ids) > 0 {
		detail["requests"] = ids
	}
	WriteLegalFeedLog(ctx, s.db, LegalFeedActor{UserID: legalReplaceActor}, LegalFeedEntry{
		Action: action, VBPLID: c.ExternalID, KnowledgeID: c.ID, KnowledgeBaseID: c.KnowledgeBaseID,
		Reason: "Phân tích toàn văn thất bại: " + why, Detail: detail,
	})
	logger.Warnf(ctx, "[LegalFeed] %s: copy %s (%s) cleaned, %d request(s) back in the queue: %s",
		c.ExternalID, c.ID, c.ParseStatus, len(reqs), why)
	return nil
}

// SweepIngests finishes, at most budget each, the replacements parsed
// without their swap (a restart between the parse and the swap, or a swap
// that failed) and cleans the copies whose parse failed: a replacement's new
// copy failed, cancelled or parsing for over legalReplaceStuckAfter, and a
// first copy a request was closed with whose parse failed. Data-driven
// (the marker, the parse status): safe after a restart. Returns the counts
// and the last error.
func (s *LegalFeedService) SweepIngests(ctx context.Context, now time.Time, budget int) (finished, cleaned int, lastErr string) {
	if s == nil || s.db == nil || s.knowledge == nil || budget <= 0 {
		return 0, 0, ""
	}
	marked := `COALESCE(k.metadata->>'` + types.LegalReplacesKnowledgeKey + `', '') <> ''`
	readable := `(k.parse_status IN ('` + types.ParseStatusCompleted + `', '` + types.ParseStatusFinalizing + `') OR k.enable_status = 'enabled')`
	failed := `k.parse_status IN ('` + types.ParseStatusFailed + `', '` + types.ParseStatusCancelled + `')`
	var parsed []legalCopyRow
	if err := s.db.WithContext(ctx).Raw(legalCopySelect+` AND `+marked+` AND `+readable+` AND k.updated_at < ?
		ORDER BY k.updated_at LIMIT ?`, now.Add(-legalReplaceSweepGrace), budget).Scan(&parsed).Error; err != nil {
		return 0, 0, "replacements: " + err.Error()
	}
	for _, c := range parsed {
		if ok, err := s.FinishReplacement(ctx, c.ID); err != nil {
			lastErr = c.ExternalID + ": " + err.Error()
			logger.Warnf(ctx, "[LegalFeed] sweep: swap of %s: %v", c.ID, err)
		} else if ok {
			finished++
		}
	}
	var broken []legalCopyRow
	if err := s.db.WithContext(ctx).Raw(legalCopySelect+` AND (
		(`+marked+` AND (`+failed+` OR (NOT `+readable+` AND k.parse_status <> '`+types.ParseStatusDeleting+`' AND k.created_at < ?)))
		OR (`+failed+` AND EXISTS (SELECT 1 FROM legal_document_requests r WHERE r.knowledge_id = k.id)))
		ORDER BY k.created_at LIMIT ?`, now.Add(-legalReplaceStuckAfter), budget).Scan(&broken).Error; err != nil {
		return finished, 0, "failed copies: " + err.Error()
	}
	for i := range broken {
		c := &broken[i]
		why := strings.TrimSpace(c.ErrorMessage)
		switch {
		case c.ParseStatus != types.ParseStatusFailed && c.ParseStatus != types.ParseStatusCancelled:
			why = fmt.Sprintf("chưa phân tích xong sau %s", legalReplaceStuckAfter)
		case why == "":
			why = c.ParseStatus
		}
		if err := s.cleanFailedCopy(ctx, c, why); err != nil {
			lastErr = c.ExternalID + ": " + err.Error()
			logger.Warnf(ctx, "[LegalFeed] sweep: cleaning %s: %v", c.ID, err)
			continue
		}
		cleaned++
	}
	return finished, cleaned, lastErr
}

// LegalReplaceAfterParse is the first legal follow-up of a parse
// (knowledgeService.legalAfterParse): a replacement whose chunks are written
// is swapped in. Best effort: the sweep retries a failed swap.
func (s *LegalFeedService) LegalReplaceAfterParse(ctx context.Context, knowledgeID string) {
	if _, err := s.FinishReplacement(ctx, knowledgeID); err != nil {
		logger.Warnf(ctx, "[LegalFeed] swap of %s after parsing: %v (the auto-ingest sweep retries)", knowledgeID, err)
	}
}
