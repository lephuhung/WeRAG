package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/legalcatalog"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// portalOverwriteAction is the legal_feed_log action of a reviewed row the
// portal updated.
const portalOverwriteAction = "portal_overwrite"

// portalOverwrite is one reviewed vbpl row whose curated values the portal
// replaces; Before holds the values it had, by JSON field name.
type portalOverwrite struct {
	ID, DocumentNumber string
	Before             map[string]string
}

// reviewedOutcome says what an import does with the reviewed rows.
type reviewedOutcome struct {
	fresh       []*types.LegalDocument
	overwritten []portalOverwrite
	// pending: row id → new source_pending (nil clears it).
	pending map[string]types.JSON
	parked  int
}

// reconcileReviewed decides the fate of imported documents. A row nobody
// reviewed is upserted. A reviewed vbpl row is upserted too, because vbpl.vn
// is the standard, except that a field the portal left blank keeps its value.
// A reviewed row from any other source stays as it is, the portal's values
// parked in source_pending.
func reconcileReviewed(docs []*types.LegalDocument, reviewed map[string]*types.LegalDocument) (*reviewedOutcome, error) {
	out := &reviewedOutcome{pending: map[string]types.JSON{}}
	for _, d := range docs {
		cur := reviewed[d.ID]
		switch {
		case cur == nil:
			out.fresh = append(out.fresh, d)
		case cur.Source == types.LegalDocumentSourceVBPL:
			legalcatalog.KeepReviewedWhereBlank(d, cur)
			if before := legalcatalog.ReviewDiff(d, cur); len(before) > 0 {
				out.overwritten = append(out.overwritten, portalOverwrite{ID: d.ID, DocumentNumber: d.DocumentNumber, Before: before})
			}
			if len(cur.SourcePending) > 0 {
				out.pending[d.ID] = nil
			}
			out.fresh = append(out.fresh, d)
		default:
			var pending types.JSON
			if diff := legalcatalog.ReviewDiff(cur, d); len(diff) > 0 {
				raw, err := json.Marshal(diff)
				if err != nil {
					return nil, fmt.Errorf("encode pending values of %s: %w", d.ID, err)
				}
				pending = types.JSON(raw)
				out.parked++
			}
			out.pending[d.ID] = pending
		}
	}
	return out, nil
}

// logPortalOverwrites records the values the import is about to replace.
func logPortalOverwrites(ctx context.Context, db *gorm.DB, rows []portalOverwrite) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, r := range rows {
			detail, err := json.Marshal(map[string]any{"before": r.Before})
			if err != nil {
				return err
			}
			if err := tx.Exec(`INSERT INTO legal_feed_log (action, vbpl_id, document_number, reason, detail)
				VALUES (?, ?, ?, ?, ?)`, portalOverwriteAction, r.ID, r.DocumentNumber,
				"vbpl.vn là chuẩn: giá trị trên cổng thay giá trị đã duyệt", string(detail)).Error; err != nil {
				return fmt.Errorf("%s: %w", r.ID, err)
			}
		}
		return nil
	})
}

// linkPortalKnowledge approves the catalogue link of every public-KB document
// that names its vbpl row (metadata external_id): its identity is known, so
// it needs neither a reviewer nor the manual scan. Parsing must have stored
// the chunks, like the scan requires. Returns the links added.
func linkPortalKnowledge(ctx context.Context, db *gorm.DB) (int64, error) {
	res := db.WithContext(ctx).Exec(`INSERT INTO legal_document_links
		(knowledge_id, knowledge_base_id, legal_document_id, review_status, suggestion, reviewed_by, reviewed_at, created_at, updated_at)
		SELECT k.id, k.knowledge_base_id, d.id, ?, '{}', ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
		FROM knowledges k
		JOIN knowledge_bases kb ON kb.id = k.knowledge_base_id AND kb.owner_tenant_id = 0 AND kb.deleted_at IS NULL
		JOIN legal_documents d ON d.id = k.metadata->>'external_id' AND d.source = ?
		WHERE k.deleted_at IS NULL AND k.parse_status IN (?, ?)
		  AND NOT EXISTS (SELECT 1 FROM legal_document_links l WHERE l.knowledge_id = k.id)
		ON CONFLICT DO NOTHING`,
		types.LegalReviewApproved, types.LegalReviewerVBPL,
		types.LegalDocumentSourceVBPL, types.ParseStatusCompleted, types.ParseStatusFinalizing)
	return res.RowsAffected, res.Error
}

// listValidity is a list-only row's validity from the portal, for a document
// already catalogued: nothing else of the list row replaces the catalogue's.
func listValidity(d, cur *types.LegalDocument) map[string]any {
	changes := map[string]any{}
	if s := strings.TrimSpace(d.Status); s != "" && s != strings.TrimSpace(cur.Status) {
		changes["status"] = s
	}
	day := func(column string, in, have *time.Time) {
		if in != nil && (have == nil || in.Format("2006-01-02") != have.Format("2006-01-02")) {
			changes[column] = *in
		}
	}
	day("effective_on", d.EffectiveOn, cur.EffectiveOn)
	day("expired_on", d.ExpiredOn, cur.ExpiredOn)
	return changes
}

// updateListOnlyValidity applies the portal's validity to catalogued vbpl
// rows the crawl knows from its list only. Returns the rows changed.
func updateListOnlyValidity(
	ctx context.Context, db *gorm.DB, listRows, catalogued []*types.LegalDocument,
) (int, error) {
	byID := make(map[string]*types.LegalDocument, len(catalogued))
	for _, c := range catalogued {
		byID[c.ID] = c
	}
	var overwrites []portalOverwrite
	type update struct {
		id      string
		changes map[string]any
	}
	var updates []update
	for _, d := range listRows {
		cur := byID[d.ID]
		if cur == nil || cur.Source != types.LegalDocumentSourceVBPL {
			continue
		}
		changes := listValidity(d, cur)
		if len(changes) == 0 {
			continue
		}
		if cur.ReviewedAt != nil {
			after := *cur
			if s, ok := changes["status"].(string); ok {
				after.Status = s
			}
			if t, ok := changes["effective_on"].(time.Time); ok {
				after.EffectiveOn = &t
			}
			if t, ok := changes["expired_on"].(time.Time); ok {
				after.ExpiredOn = &t
			}
			overwrites = append(overwrites, portalOverwrite{ID: d.ID, DocumentNumber: cur.DocumentNumber,
				Before: legalcatalog.ReviewDiff(&after, cur)})
		}
		updates = append(updates, update{d.ID, changes})
	}
	if err := logPortalOverwrites(ctx, db, overwrites); err != nil {
		return 0, err
	}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, u := range updates {
			u.changes["updated_at"] = time.Now()
			if err := tx.Model(&types.LegalDocument{}).Where("id = ?", u.id).Updates(u.changes).Error; err != nil {
				return fmt.Errorf("%s: %w", u.id, err)
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return len(updates), nil
}

// syncKnowledgeValidity copies the catalogue's status onto the public-KB
// documents naming a vbpl row (metadata eff_status), so what a document's
// card says follows the portal. Returns the documents changed.
func syncKnowledgeValidity(ctx context.Context, db *gorm.DB) (int, error) {
	var rows []struct {
		ID     string
		Status string
	}
	if err := db.WithContext(ctx).Raw(`SELECT k.id, d.status
		FROM knowledges k
		JOIN knowledge_bases kb ON kb.id = k.knowledge_base_id AND kb.owner_tenant_id = 0 AND kb.deleted_at IS NULL
		JOIN legal_documents d ON d.id = k.metadata->>'external_id' AND d.source = ?
		WHERE k.deleted_at IS NULL AND d.status <> '' AND COALESCE(k.metadata->>'eff_status', '') <> d.status`,
		types.LegalDocumentSourceVBPL).Scan(&rows).Error; err != nil {
		return 0, err
	}
	set := "json_set(metadata, '$.eff_status', ?)"
	if db.Dialector.Name() == "postgres" {
		set = "jsonb_set(metadata::jsonb, '{eff_status}', to_jsonb(?::text))"
	}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, r := range rows {
			if err := tx.Exec("UPDATE knowledges SET metadata = "+set+", updated_at = CURRENT_TIMESTAMP WHERE id = ?", r.Status, r.ID).Error; err != nil {
				return fmt.Errorf("%s: %w", r.ID, err)
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return len(rows), nil
}
