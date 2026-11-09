package main

import (
	"context"
	"encoding/json"
	"fmt"

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
