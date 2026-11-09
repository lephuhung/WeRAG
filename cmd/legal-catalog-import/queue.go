package main

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/legalcatalog"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
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

// sameIssue reports whether two catalogue rows of one số hiệu are the same
// document: same type class, same issue day, issuers that agree. The title
// is left out on purpose: the portal's second copy often has a bare title
// ("Thông tư 25/2026/TT-BGDĐT"), which legalcatalog.SameDocument cannot match.
func sameIssue(a, b *types.LegalDocument) bool {
	return a.IssuedOn != nil && b.IssuedOn != nil &&
		a.IssuedOn.Format("2006-01-02") == b.IssuedOn.Format("2006-01-02") &&
		vietnamese_legal.DocTypeClass(a.DocType) == vietnamese_legal.DocTypeClass(b.DocType) &&
		legalcatalog.IssuersAgree(a.Issuer, b.Issuer)
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
// knowledge's external_id or an approved link.
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
			JOIN knowledges k ON k.id = l.knowledge_id AND k.deleted_at IS NULL
			JOIN knowledge_bases kb ON kb.id = k.knowledge_base_id AND kb.owner_tenant_id = 0 AND kb.deleted_at IS NULL
			WHERE l.review_status = 'approved' AND l.legal_document_id IN ?`, part).Scan(&linked).Error; err != nil {
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
	candidates := noFullTextCandidates(docs, noFullText)
	if len(candidates) == 0 {
		return 0, nil
	}
	wanted := map[string]bool{}
	ids, keys := make([]string, 0, len(candidates)), make([]string, 0, len(candidates))
	for _, d := range candidates {
		ids, keys = append(ids, d.ID), append(keys, d.NumberKey)
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
		return 0, err
	}
	requested := map[string]bool{}
	if err := inChunks(keys, func(part []string) error {
		var existing []struct {
			NumberKey       string
			LegalDocumentID string
		}
		if err := db.WithContext(ctx).Model(&types.LegalDocumentRequest{}).Select("number_key", "legal_document_id").
			Where("number_key IN ?", part).Scan(&existing).Error; err != nil {
			return fmt.Errorf("existing requests: %w", err)
		}
		for _, r := range existing {
			requested[r.NumberKey+"\x00"+r.LegalDocumentID] = true
		}
		return nil
	}); err != nil {
		return 0, err
	}
	var fresh []*types.LegalDocument
	for _, d := range candidates {
		if held[d.ID] || requested[d.NumberKey+"\x00"+d.ID] || requested[d.NumberKey+"\x00"] {
			continue
		}
		copyHeld := false
		for _, s := range siblings[d.NumberKey] {
			copyHeld = copyHeld || (s.ID != d.ID && held[s.ID] && sameIssue(d, s))
		}
		if !copyHeld {
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
	added := 0
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, d := range fresh {
			// Raw: gorm would turn the zero request_count into its default 1.
			res := tx.Exec(`INSERT INTO legal_document_requests
				(id, number_key, document_number, doc_type, title, legal_document_id, source, status,
				 request_count, first_requested_at, last_requested_at, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?)
				ON CONFLICT (number_key, legal_document_id) DO NOTHING`,
				uuid.NewString(), d.NumberKey, d.DocumentNumber, d.DocType, d.Title, d.ID,
				types.LegalRequestSourceNoFullText, types.LegalRequestRequested, now, now, now, now)
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
