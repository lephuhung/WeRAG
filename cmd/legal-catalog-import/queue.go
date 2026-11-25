package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/legalcatalog"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	// expiredStatusPrefix: a fully expired document stays metadata only
	// (service.LegalExpiredStatusPrefix; the import does not link the service layer).
	expiredStatusPrefix = "Hết hiệu lực toàn bộ"
	// maxNoFullTextRequests guards the queue: a portal change that empties
	// the full texts must not bury it under thousands of rows in one night.
	maxNoFullTextRequests = 500
)

// noFullTextCandidates are the imported documents worth asking another
// source for: the portal has no full text, a số hiệu to search by, and the
// document is not fully expired.
func noFullTextCandidates(docs []*types.LegalDocument, noFullText map[string]bool) []*types.LegalDocument {
	var out []*types.LegalDocument
	for _, d := range docs {
		if noFullText[d.ID] && d.NumberKey != "" && !strings.HasPrefix(d.Status, expiredStatusPrefix) {
			out = append(out, d)
		}
	}
	return out
}

// inChunks runs fn over ids in pieces small enough for one IN list.
func inChunks(ids []string, fn func([]string) error) error {
	for start := 0; start < len(ids); start += 1000 {
		if err := fn(ids[start:min(start+1000, len(ids))]); err != nil {
			return err
		}
	}
	return nil
}

// heldInPublicKB returns the ids among ids that a public KB holds, by the
// knowledge's external_id or an approved link. A copy whose parse failed (or
// that is being deleted) holds nothing: the document is queued again; one
// still parsing holds it (no second ingest).
func heldInPublicKB(ctx context.Context, db *gorm.DB, ids []string) (map[string]bool, error) {
	held := map[string]bool{}
	requests := repository.NewLegalRequestRepository(db)
	err := inChunks(ids, func(part []string) error {
		found, err := requests.PublicKnowledgeByExternalIDs(ctx, part)
		if err != nil {
			return fmt.Errorf("public KB copies: %w", err)
		}
		for id := range found {
			held[id] = true
		}
		var linked []string
		if err := db.WithContext(ctx).Raw(`SELECT DISTINCT l.legal_document_id FROM legal_document_links l
			JOIN knowledges k ON k.id = l.knowledge_id AND k.deleted_at IS NULL AND COALESCE(k.parse_status, '') NOT IN ?
			JOIN knowledge_bases kb ON kb.id = k.knowledge_base_id AND kb.owner_tenant_id = 0 AND kb.deleted_at IS NULL
			WHERE l.review_status = 'approved' AND l.legal_document_id IN ?`, types.LegalCopyNotHeldStatuses, part).Scan(&linked).Error; err != nil {
			return fmt.Errorf("approved links: %w", err)
		}
		for _, id := range linked {
			held[id] = true
		}
		return nil
	})
	return held, err
}

// enqueueNoFullText adds the candidates no public KB holds, under their own
// id or that of another catalogue row of the same document (catalogued is
// the whole catalogue), to the request queue and returns how many it added.
// A number that already has a request is left alone, whatever its state, so
// a request closed or rejected by a person is not reopened every night, and
// one a user made before the catalogue knew the document is not doubled.
// Rows carry request_count 0: repository.Record counts the first real ask.
// A batch over the limit queues its newest documents; the rest follow on
// later nights.
func enqueueNoFullText(
	ctx context.Context, db *gorm.DB, docs, catalogued []*types.LegalDocument, noFullText map[string]bool, now time.Time,
) (int, error) {
	candidates, err := withoutPublicCopy(ctx, db, noFullTextCandidates(docs, noFullText), catalogued)
	if err != nil {
		return 0, err
	}
	requested, err := loadRequests(ctx, db, candidates)
	if err != nil {
		return 0, err
	}
	var fresh []*types.LegalDocument
	for _, d := range candidates {
		if _, ok := requested[d.NumberKey+"\x00"+d.ID]; ok {
			continue
		}
		if _, ok := requested[d.NumberKey+"\x00"]; !ok {
			fresh = append(fresh, d)
		}
	}
	if len(fresh) > maxNoFullTextRequests {
		sort.SliceStable(fresh, func(i, j int) bool {
			a, b := fresh[i].IssuedOn, fresh[j].IssuedOn
			if (a == nil) != (b == nil) {
				return b == nil
			}
			if a != nil && !a.Equal(*b) {
				return a.After(*b)
			}
			return fresh[i].ID < fresh[j].ID
		})
		log.Printf("%d documents without full text are over the limit of %d a night: the oldest %d wait",
			len(fresh), maxNoFullTextRequests, len(fresh)-maxNoFullTextRequests)
		fresh = fresh[:maxNoFullTextRequests]
	}
	return insertRequests(ctx, db, fresh, nil, nil, types.LegalRequestSourceNoFullText, types.LegalRequestRequested, now)
}

// insertRequests adds rows nobody asked for (request_count 0). kbIDs names
// the public KB holding a document's current copy, if any; details a new
// row's detail JSON, if any.
func insertRequests(
	ctx context.Context, db *gorm.DB, fresh []*types.LegalDocument, kbIDs map[string]string, details map[string]types.JSON,
	source, status string, now time.Time,
) (int, error) {
	added := 0
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, d := range fresh {
			// Raw: gorm would turn the zero request_count into its default 1.
			res := tx.Exec(`INSERT INTO legal_document_requests
				(id, number_key, document_number, doc_type, title, legal_document_id, knowledge_base_id, source, status,
				 request_count, detail, first_requested_at, last_requested_at, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?, ?)
				ON CONFLICT (number_key, legal_document_id) DO NOTHING`,
				uuid.NewString(), d.NumberKey, d.DocumentNumber, d.DocType, d.Title, d.ID, kbIDs[d.ID], source, status,
				details[d.ID], now, now, now, now)
			if res.Error != nil {
				return res.Error
			}
			added += int(res.RowsAffected)
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("insert requests: %w", err)
	}
	return added, nil
}

// withoutPublicCopy drops the candidates a public KB holds, under their own
// id or that of another catalogue row of the same document (catalogued is
// the whole catalogue).
func withoutPublicCopy(
	ctx context.Context, db *gorm.DB, candidates, catalogued []*types.LegalDocument,
) ([]*types.LegalDocument, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	wanted := map[string]bool{}
	ids := make([]string, 0, len(candidates))
	for _, d := range candidates {
		ids = append(ids, d.ID)
		wanted[d.NumberKey] = true
	}
	siblings := map[string][]*types.LegalDocument{}
	for _, c := range catalogued {
		if wanted[c.NumberKey] {
			siblings[c.NumberKey] = append(siblings[c.NumberKey], c)
			ids = append(ids, c.ID)
		}
	}
	held, err := heldInPublicKB(ctx, db, ids)
	if err != nil {
		return nil, err
	}
	var out []*types.LegalDocument
	for _, d := range candidates {
		copyHeld := held[d.ID]
		for _, s := range siblings[d.NumberKey] {
			copyHeld = copyHeld || (s.ID != d.ID && held[s.ID] && legalcatalog.SameIssue(d, s))
		}
		if !copyHeld {
			out = append(out, d)
		}
	}
	return out, nil
}

// requestRow is what the import needs to know of an existing request.
type requestRow struct {
	ID              string
	NumberKey       string
	LegalDocumentID string
	DocumentNumber  string
	Source          string
	Status          string
	KnowledgeID     string
	ClaimedBy       string
	ClaimedAt       *time.Time
	DecidedBy       string
	DecidedAt       *time.Time
	DecisionNote    string
	Detail          types.JSON
}

// requestReopenedAction is the legal_feed_log action of a decided request
// (added, or rejected by a SuperAdmin) the import hands back to the worker;
// the decision it replaces is in the log row and in the request's detail
// (types.LegalRequestDetailPreviousDecision).
const requestReopenedAction = "request_reopened"

// loadRequests returns the requests of the candidates' numbers, keyed by
// number key + NUL + catalogue id.
func loadRequests(ctx context.Context, db *gorm.DB, candidates []*types.LegalDocument) (map[string]requestRow, error) {
	keys := make([]string, 0, len(candidates))
	for _, d := range candidates {
		keys = append(keys, d.NumberKey)
	}
	out := map[string]requestRow{}
	err := inChunks(keys, func(part []string) error {
		var rows []requestRow
		if err := db.WithContext(ctx).Model(&types.LegalDocumentRequest{}).
			Select("id", "number_key", "legal_document_id", "document_number", "source", "status", "knowledge_id",
				"claimed_by", "claimed_at", "decided_by", "decided_at", "decision_note", "detail").
			Where("number_key IN ?", part).Scan(&rows).Error; err != nil {
			return fmt.Errorf("existing requests: %w", err)
		}
		for _, r := range rows {
			out[r.NumberKey+"\x00"+r.LegalDocumentID] = r
		}
		return nil
	})
	return out, err
}

// autoIngestSkipped reports whether the worker gave up on a request.
func autoIngestSkipped(detail types.JSON) bool {
	var d map[string]json.RawMessage
	if len(detail) == 0 || json.Unmarshal(detail, &d) != nil {
		return false
	}
	v := d[types.LegalRequestDetailAutoIngestSkip]
	return len(v) > 0 && string(v) != "null"
}

// queueAutoIngest hands the imported documents vbpl.vn has the full text of,
// and no public KB holds, to the backend's auto-ingest worker: a new row, or
// a waiting or formerly added request of that document (nobody working on it,
// not given up on).
// Returns the rows added and the requests moved.
func queueAutoIngest(
	ctx context.Context, db *gorm.DB, docs, catalogued []*types.LegalDocument, hasHTML map[string]bool, now time.Time,
) (added, moved int, err error) {
	candidates, err := withoutPublicCopy(ctx, db, noFullTextCandidates(docs, hasHTML), catalogued)
	if err != nil {
		return 0, 0, err
	}
	requests, err := loadRequests(ctx, db, candidates)
	if err != nil {
		return 0, 0, err
	}
	var fresh []*types.LegalDocument
	for _, d := range candidates {
		r, ok := requests[d.NumberKey+"\x00"+d.ID]
		if !ok {
			if _, unknownID := requests[d.NumberKey+"\x00"]; !unknownID {
				fresh = append(fresh, d)
			}
			continue
		}
		// An added request here is one whose document no public KB holds any
		// more (its text was removed when it expired, the portal has it in
		// force again).
		n, err := moveToAutoIngest(ctx, db, r, "", false, "", now)
		if err != nil {
			return added, moved, err
		}
		moved += n
	}
	if len(fresh) > maxNoFullTextRequests {
		log.Printf("%d documents to ingest are over the limit of %d a night: the rest wait", len(fresh), maxNoFullTextRequests)
		fresh = fresh[:maxNoFullTextRequests]
	}
	added, err = insertRequests(ctx, db, fresh, nil, nil, types.LegalRequestSourceHTML, types.LegalRequestAutoIngest, now)
	return added, moved, err
}

// moveToAutoIngest hands a waiting or added request to the worker, unless a
// person is working on it or the worker gave up on it. kbID names the public
// KB holding the copy to replace ("": the worker picks one by type). A text
// update (update true) concerns a document the KB holds, so an earlier
// rejection or give-up, which was about adding it, does not stop it. The
// decision a reopened request carried (status, by, at, note) is kept in its
// detail under previous_decision and in a legal_feed_log row, so it does not
// vanish with the reset fields. changedMD5 (a text update: the crawler's
// file hash that showed the change) is stamped in the detail under
// types.LegalRequestDetailChangedHTMLMD5 by the same write that moves it.
func moveToAutoIngest(
	ctx context.Context, db *gorm.DB, r requestRow, kbID string, update bool, changedMD5 string, now time.Time,
) (int, error) {
	held := r.ClaimedBy != "" && r.ClaimedAt != nil && now.Sub(*r.ClaimedAt) < types.LegalRequestClaimLease
	added := r.Status == types.LegalRequestAdded || (update && r.Status == types.LegalRequestRejected)
	if (r.Status != types.LegalRequestRequested && !added) || held || (!update && autoIngestSkipped(r.Detail)) {
		return 0, nil
	}
	source := r.Source
	if source == types.LegalRequestSourceNoFullText {
		source = types.LegalRequestSourceHTML
	}
	values := map[string]any{"status": types.LegalRequestAutoIngest, "source": source, "knowledge_base_id": kbID, "updated_at": now}
	var previous map[string]any
	if added {
		values["knowledge_id"] = ""
		values["decided_by"], values["decided_at"], values["decision_note"] = "", nil, ""
		previous = map[string]any{"status": r.Status, "by": r.DecidedBy, "note": r.DecisionNote,
			"knowledge_id": r.KnowledgeID, "reopened_at": now.UTC().Format(time.RFC3339)}
		if r.DecidedAt != nil {
			previous["at"] = r.DecidedAt.UTC().Format(time.RFC3339)
		}
	}
	if previous != nil || (update && autoIngestSkipped(r.Detail)) || changedMD5 != "" {
		detail, err := reopenedDetail(r.Detail, previous, update, changedMD5)
		if err != nil {
			return 0, fmt.Errorf("request %s detail: %w", r.ID, err)
		}
		values["detail"] = detail
	}
	moved := 0
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&types.LegalDocumentRequest{}).
			Where("id = ? AND status = ?", r.ID, r.Status).
			Where("claimed_by = '' OR claimed_at IS NULL OR claimed_at < ?", now.Add(-types.LegalRequestClaimLease)).
			Updates(values)
		if res.Error != nil {
			return fmt.Errorf("move request %s: %w", r.ID, res.Error)
		}
		moved = int(res.RowsAffected)
		if moved == 0 || previous == nil {
			return nil
		}
		reason := "Văn bản đã có trong kho: tự nạp lại"
		if update {
			reason = "Toàn văn trên vbpl.vn đã đổi: tự nạp bản mới, quyết định trước được giữ trong nhật ký"
		}
		raw, err := json.Marshal(map[string]any{"request_id": r.ID, "previous_decision": previous})
		if err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO legal_feed_log (action, vbpl_id, document_number, reason, detail)
			VALUES (?, ?, ?, ?, ?)`, requestReopenedAction, r.LegalDocumentID, r.DocumentNumber, reason, string(raw)).Error; err != nil {
			return fmt.Errorf("log reopened request %s: %w", r.ID, err)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return moved, nil
}

// reopenedDetail is a request's detail with previous (when not nil) under
// types.LegalRequestDetailPreviousDecision, changedMD5 (when not empty)
// under types.LegalRequestDetailChangedHTMLMD5 and, for a text update, the
// worker's give-up marker removed; the other keys stay.
func reopenedDetail(detail types.JSON, previous map[string]any, update bool, changedMD5 string) (types.JSON, error) {
	d := map[string]json.RawMessage{}
	if len(detail) > 0 && string(detail) != "null" {
		if err := json.Unmarshal(detail, &d); err != nil {
			return nil, err
		}
	}
	if update {
		delete(d, types.LegalRequestDetailAutoIngestSkip)
	}
	if previous != nil {
		raw, err := json.Marshal(previous)
		if err != nil {
			return nil, err
		}
		d[types.LegalRequestDetailPreviousDecision] = raw
	}
	if changedMD5 != "" {
		raw, err := json.Marshal(changedMD5)
		if err != nil {
			return nil, err
		}
		d[types.LegalRequestDetailChangedHTMLMD5] = raw
	}
	raw, err := json.Marshal(d)
	return types.JSON(raw), err
}

// queueChangedText hands the worker the documents whose full text on the
// portal is no longer the one a public KB holds (the knowledge's file hash
// against the crawl's). Only copies from the portal count: a copy pushed from
// another source has other bytes by nature. Each request queued carries the
// crawl's hash (types.LegalRequestDetailChangedHTMLMD5): a document whose
// request already carries the current hash, whatever its status, was handed
// to the worker for this very file and decided from the live portal text,
// so it is not queued again (the portal can change a page without its
// updatedDate: the crawler keeps an older file than the copy the worker
// fetched, and would reopen the request every night); a refetched file
// (another hash) queues it again. Returns the documents queued.
func queueChangedText(
	ctx context.Context, db *gorm.DB, docs []*types.LegalDocument, htmlMD5 map[string]string, now time.Time,
) (int, error) {
	has := make(map[string]bool, len(htmlMD5))
	for id, sum := range htmlMD5 {
		has[id] = sum != ""
	}
	candidates := noFullTextCandidates(docs, has)
	ids := make([]string, 0, len(candidates))
	for _, d := range candidates {
		ids = append(ids, d.ID)
	}
	// A completed copy with other bytes is a changed text, unless a copy of
	// the portal's current bytes is already there, parsing or not (a
	// replacement in flight: the old copy stays until the new one is
	// parsed, and must not be queued again meanwhile).
	copyKB := map[string]string{}
	current := map[string]bool{}
	err := inChunks(ids, func(part []string) error {
		var rows []struct {
			ExternalID      string
			KnowledgeBaseID string
			FileHash        string
			ParseStatus     string
		}
		if err := db.WithContext(ctx).Raw(`SELECT k.metadata->>'external_id' AS external_id, k.knowledge_base_id, k.file_hash,
			COALESCE(k.parse_status, '') AS parse_status
			FROM knowledges k
			JOIN knowledge_bases kb ON kb.id = k.knowledge_base_id AND kb.owner_tenant_id = 0 AND kb.deleted_at IS NULL
			WHERE k.deleted_at IS NULL AND COALESCE(k.parse_status, '') NOT IN ? AND k.metadata->>'source' = ?
			  AND k.metadata->>'external_id' IN ?`,
			types.LegalCopyNotHeldStatuses, types.LegalDocumentSourceVBPL, part).Scan(&rows).Error; err != nil {
			return fmt.Errorf("public KB copies: %w", err)
		}
		for _, r := range rows {
			switch {
			case r.FileHash != "" && r.FileHash == htmlMD5[r.ExternalID]:
				current[r.ExternalID] = true
			case r.FileHash != "" && r.ParseStatus == types.ParseStatusCompleted:
				copyKB[r.ExternalID] = r.KnowledgeBaseID
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	var changed []*types.LegalDocument
	for _, d := range candidates {
		if _, ok := copyKB[d.ID]; ok && !current[d.ID] {
			changed = append(changed, d)
		}
	}
	requests, err := loadRequests(ctx, db, changed)
	if err != nil {
		return 0, err
	}
	var todo []*types.LegalDocument
	handed := 0
	for _, d := range changed {
		if r, ok := requests[d.NumberKey+"\x00"+d.ID]; ok && htmlMD5[d.ID] != "" && changedMD5(r.Detail) == htmlMD5[d.ID] {
			handed++
			continue
		}
		todo = append(todo, d)
	}
	if handed > 0 {
		log.Printf("%d documents with a changed text not queued again: the worker already had this crawl's file of them", handed)
	}
	if len(todo) > maxNoFullTextRequests {
		log.Printf("%d documents with a changed text are over the limit of %d a night: the rest wait", len(todo), maxNoFullTextRequests)
		todo = todo[:maxNoFullTextRequests]
	}
	var fresh []*types.LegalDocument
	details := map[string]types.JSON{}
	queued := 0
	for _, d := range todo {
		sum := htmlMD5[d.ID]
		r, ok := requests[d.NumberKey+"\x00"+d.ID]
		if !ok {
			detail, err := reopenedDetail(nil, nil, true, sum)
			if err != nil {
				return queued, err
			}
			details[d.ID] = detail
			fresh = append(fresh, d)
			continue
		}
		n, err := moveToAutoIngest(ctx, db, r, copyKB[d.ID], true, sum, now)
		if err != nil {
			return queued, err
		}
		queued += n
	}
	n, err := insertRequests(ctx, db, fresh, copyKB, details, types.LegalRequestSourceHTML, types.LegalRequestAutoIngest, now)
	return queued + n, err
}

// changedMD5 is the crawl's hash a text update was queued for
// (types.LegalRequestDetailChangedHTMLMD5), "" when none.
func changedMD5(detail types.JSON) string {
	var d map[string]json.RawMessage
	if len(detail) == 0 || json.Unmarshal(detail, &d) != nil {
		return ""
	}
	var sum string
	if json.Unmarshal(d[types.LegalRequestDetailChangedHTMLMD5], &sum) != nil {
		return ""
	}
	return sum
}
