package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Tencent/WeKnora/internal/legalcatalog"
	"github.com/Tencent/WeKnora/internal/types"
)

// LegalAdoption is what AdoptLegalDocument moved.
type LegalAdoption struct {
	FromID, ToID string
	// KnowledgeIDs are the public-KB knowledges that held FromID; their
	// KnowledgeBaseIDs may want a wiki catalogue rebuild.
	KnowledgeIDs     []string
	KnowledgeBaseIDs []string
	Chunks           int64
	ReviewLinks      int64
	Requests         int64
	RequestsDropped  int64
	Drafts           int64
	Relations        int
	ProvisionLinks   int
	LinksMerged      int
	Candidates       int
	ReviewInherited  bool
}

// AdoptLegalDocument hands every reference to the catalogue row fromID — a
// legal feed's provisional row ("tvpl-…") — to toID, the vbpl.vn row of the
// same document, and deletes fromID: the KB copy (knowledge external_id and
// the chunks' legal.source_doc_id), the legal review link, the requests and
// drafts, the catalogue relations, the provision links and the feed's link
// candidates, rewriting the synthetic provision ids and mark keys that
// embed the document id. A SuperAdmin's review of fromID passes to toID:
// its curated values stay, toID's portal values that differ are parked in
// source_pending. toID keeps its source. One transaction; fromID must be a
// feed row (types.LegalDocument.IsFeedRow) and toID must exist.
func AdoptLegalDocument(ctx context.Context, db *gorm.DB, fromID, toID string) (*LegalAdoption, error) {
	fromID, toID = strings.TrimSpace(fromID), strings.TrimSpace(toID)
	if fromID == "" || toID == "" || fromID == toID {
		return nil, fmt.Errorf("adopt legal document: need two different ids (%q, %q)", fromID, toID)
	}
	out := &LegalAdoption{FromID: fromID, ToID: toID}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		pg := tx.Dialector.Name() == "postgres"
		lock := tx
		if pg {
			// a push of the feed row waits until the row has moved
			lock = tx.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		var from, to types.LegalDocument
		if err := lock.Where("id = ?", fromID).Limit(1).Find(&from).Error; err != nil {
			return err
		}
		if from.ID == "" {
			return fmt.Errorf("adopt legal document: %s is not in the catalogue", fromID)
		}
		if !from.IsFeedRow() {
			return fmt.Errorf("adopt legal document: %s (source %q) is not a feed row", fromID, from.Source)
		}
		if err := tx.Where("id = ?", toID).Limit(1).Find(&to).Error; err != nil {
			return err
		}
		if to.ID == "" {
			return fmt.Errorf("adopt legal document: %s is not in the catalogue", toID)
		}

		if err := adoptKnowledges(tx, pg, out); err != nil {
			return err
		}
		res := tx.Model(&types.LegalDocumentLink{}).Where("legal_document_id = ?", fromID).
			Update("legal_document_id", toID)
		if res.Error != nil {
			return res.Error
		}
		out.ReviewLinks = res.RowsAffected
		if err := adoptRequests(tx, out); err != nil {
			return err
		}
		res = tx.Exec(`UPDATE legal_document_drafts SET legal_document_id = ? WHERE legal_document_id = ?
			AND NOT EXISTS (SELECT 1 FROM legal_document_drafts d WHERE d.legal_document_id = ?)`, toID, fromID, toID)
		if res.Error != nil {
			return res.Error
		}
		out.Drafts = res.RowsAffected
		if err := adoptRelations(tx, out); err != nil {
			return err
		}
		linkMoved, err := adoptProvisionLinks(tx, out)
		if err != nil {
			return err
		}
		if err := adoptCandidates(tx, out, linkMoved); err != nil {
			return err
		}
		if from.ReviewedAt != nil && to.ReviewedAt == nil {
			if err := inheritReview(tx, &from, &to); err != nil {
				return err
			}
			out.ReviewInherited = true
		}
		return tx.Where("id = ?", fromID).Delete(&types.LegalDocument{}).Error
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// adoptKnowledges moves the public-KB copy: knowledge external_id and the
// chunks' legal.source_doc_id.
func adoptKnowledges(tx *gorm.DB, pg bool, out *LegalAdoption) error {
	ext := "json_extract(CAST(k.metadata AS TEXT), '$.external_id')"
	if pg {
		ext = "k.metadata->>'external_id'"
	}
	var rows []struct{ ID, KnowledgeBaseID string }
	if err := tx.Raw(`SELECT k.id, k.knowledge_base_id FROM knowledges k
		JOIN knowledge_bases kb ON kb.id = k.knowledge_base_id AND kb.owner_tenant_id = 0
		WHERE `+ext+` = ?`, out.FromID).Scan(&rows).Error; err != nil {
		return fmt.Errorf("knowledges of %s: %w", out.FromID, err)
	}
	if len(rows) == 0 {
		return nil
	}
	seenKB := map[string]bool{}
	for _, r := range rows {
		out.KnowledgeIDs = append(out.KnowledgeIDs, r.ID)
		if !seenKB[r.KnowledgeBaseID] {
			seenKB[r.KnowledgeBaseID] = true
			out.KnowledgeBaseIDs = append(out.KnowledgeBaseIDs, r.KnowledgeBaseID)
		}
	}
	setExt := "json_set(metadata, '$.external_id', ?)"
	setDoc := "json_set(metadata, '$.legal.source_doc_id', ?)"
	hasLegal := "json_extract(CAST(metadata AS TEXT), '$.legal') IS NOT NULL"
	if pg {
		setExt = "jsonb_set(metadata::jsonb, '{external_id}', to_jsonb(?::text))"
		setDoc = "jsonb_set(metadata::jsonb, '{legal,source_doc_id}', to_jsonb(?::text))"
		hasLegal = "(metadata::jsonb)->'legal' IS NOT NULL"
	}
	if err := tx.Exec(`UPDATE knowledges SET metadata = `+setExt+` WHERE id IN ?`, out.ToID, out.KnowledgeIDs).Error; err != nil {
		return fmt.Errorf("knowledge external_id: %w", err)
	}
	res := tx.Exec(`UPDATE chunks SET metadata = `+setDoc+` WHERE knowledge_id IN ? AND `+hasLegal, out.ToID, out.KnowledgeIDs)
	if res.Error != nil {
		return fmt.Errorf("chunk source_doc_id: %w", res.Error)
	}
	out.Chunks = res.RowsAffected
	return nil
}

// adoptRequests moves the requests; one of a number already filed for toID
// (unique number_key + legal_document_id) is dropped, toID's is kept.
func adoptRequests(tx *gorm.DB, out *LegalAdoption) error {
	res := tx.Exec(`DELETE FROM legal_document_requests WHERE legal_document_id = ? AND number_key IN
		(SELECT r.number_key FROM legal_document_requests r WHERE r.legal_document_id = ?)`, out.FromID, out.ToID)
	if res.Error != nil {
		return res.Error
	}
	out.RequestsDropped = res.RowsAffected
	res = tx.Model(&types.LegalDocumentRequest{}).Where("legal_document_id = ?", out.FromID).
		Updates(map[string]any{"legal_document_id": out.ToID, "updated_at": time.Now()})
	out.Requests = res.RowsAffected
	return res.Error
}

// adoptRelations moves the catalogue edges, dropping self edges and repeats.
func adoptRelations(tx *gorm.DB, out *LegalAdoption) error {
	var old []types.LegalDocumentRelation
	if err := tx.Where("from_id = ? OR to_id = ?", out.FromID, out.FromID).Find(&old).Error; err != nil {
		return err
	}
	if len(old) == 0 {
		return nil
	}
	if err := tx.Where("from_id = ? OR to_id = ?", out.FromID, out.FromID).Delete(&types.LegalDocumentRelation{}).Error; err != nil {
		return err
	}
	move := func(id string) string {
		if id == out.FromID {
			return out.ToID
		}
		return id
	}
	var moved []types.LegalDocumentRelation
	for _, r := range old {
		r.FromID, r.ToID = move(r.FromID), move(r.ToID)
		if r.FromID == r.ToID {
			continue
		}
		moved = append(moved, r)
	}
	out.Relations = len(moved)
	if len(moved) == 0 {
		return nil
	}
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&moved).Error
}

// movedProvisionID is a provision id after the document moved: a synthetic
// id of (fromID, path) becomes the one of (toID, path); any other id stays.
func movedProvisionID(id, fromID, toID, path string) string {
	if path != "" && id == legalcatalog.SyntheticProvisionID(fromID, path) {
		return legalcatalog.SyntheticProvisionID(toID, path)
	}
	return id
}

// adoptProvisionLinks moves the provision links at either end. A link that
// becomes the same edge as one already stored (unique target provision,
// code, source provision) is merged into it. linkMoved maps a merged
// link's id to the survivor's, for the candidates' link_id.
func adoptProvisionLinks(tx *gorm.DB, out *LegalAdoption) (map[int64]int64, error) {
	var links []types.LegalProvisionLink
	if err := tx.Where("target_doc_id = ? OR source_doc_id = ?", out.FromID, out.FromID).Order("id").Find(&links).Error; err != nil {
		return nil, err
	}
	linkMoved := map[int64]int64{}
	for _, lk := range links {
		upd := map[string]any{}
		target, source := lk.TargetProvisionID, lk.SourceProvisionID
		if lk.TargetDocID == out.FromID {
			upd["target_doc_id"] = out.ToID
			target = movedProvisionID(target, out.FromID, out.ToID, lk.TargetPath)
		}
		if lk.SourceDocID == out.FromID {
			upd["source_doc_id"] = out.ToID
			source = movedProvisionID(source, out.FromID, out.ToID, lk.SourcePath)
		}
		upd["target_provision_id"], upd["source_provision_id"] = target, source
		var twin types.LegalProvisionLink
		if err := tx.Where("target_provision_id = ? AND relation_code = ? AND source_provision_id = ? AND id <> ?",
			target, lk.RelationCode, source, lk.ID).Limit(1).Find(&twin).Error; err != nil {
			return nil, err
		}
		if twin.ID != 0 {
			if err := tx.Delete(&types.LegalProvisionLink{}, lk.ID).Error; err != nil {
				return nil, err
			}
			linkMoved[lk.ID] = twin.ID
			out.LinksMerged++
			continue
		}
		if err := tx.Model(&types.LegalProvisionLink{}).Where("id = ?", lk.ID).Updates(upd).Error; err != nil {
			return nil, err
		}
		out.ProvisionLinks++
	}
	return linkMoved, nil
}

// adoptCandidates moves the feed's link candidates at either end, their
// synthetic ids and the mark key recorded at push time (it names B's id:
// a re-push must still recognize a decided mark).
func adoptCandidates(tx *gorm.DB, out *LegalAdoption, linkMoved map[int64]int64) error {
	var list []types.LegalProvisionLinkCandidate
	if err := tx.Where("target_doc_id = ? OR amender_doc_id = ?", out.FromID, out.FromID).Order("id").Find(&list).Error; err != nil {
		return err
	}
	for _, c := range list {
		upd := map[string]any{"updated_at": time.Now()}
		if c.TargetDocID == out.FromID {
			upd["target_doc_id"] = out.ToID
			upd["target_provision_id"] = movedProvisionID(c.TargetProvisionID, out.FromID, out.ToID, c.TargetPath)
		}
		if c.AmenderDocID == out.FromID {
			upd["amender_doc_id"] = out.ToID
			upd["source_provision_id"] = movedProvisionID(c.SourceProvisionID, out.FromID, out.ToID, c.SourcePath)
		}
		if d, ok := movedMarkKey(c.Detail, out.FromID, out.ToID); ok {
			upd["detail"] = d
		}
		if c.LinkID != nil {
			if to, ok := linkMoved[*c.LinkID]; ok {
				upd["link_id"] = to
			}
		}
		if err := tx.Model(&types.LegalProvisionLinkCandidate{}).Where("id = ?", c.ID).Updates(upd).Error; err != nil {
			return err
		}
		out.Candidates++
	}
	return nil
}

// movedMarkKey rewrites the amender id field of a candidate's mark key
// ("target path|code|number key|amender id|source path").
func movedMarkKey(detail types.JSON, fromID, toID string) (types.JSON, bool) {
	if len(detail) == 0 {
		return nil, false
	}
	var m map[string]any
	if json.Unmarshal(detail, &m) != nil {
		return nil, false
	}
	key, _ := m["mark_key"].(string)
	parts := strings.Split(key, "|")
	if len(parts) != 5 || parts[3] != fromID {
		return nil, false
	}
	parts[3] = toID
	m["mark_key"] = strings.Join(parts, "|")
	b, err := json.Marshal(m)
	if err != nil {
		return nil, false
	}
	return types.JSON(b), true
}

// inheritReview passes a SuperAdmin's review of the feed row to the portal
// row: the curated values and reviewer stay, the portal's differing values
// wait in source_pending. A value the feed row never had (an empty date,
// the "Chưa xác định" status a push writes when its source gave none) is
// no curated value: the portal's stays. The portal row keeps its source.
func inheritReview(tx *gorm.DB, from, to *types.LegalDocument) error {
	curated := *from
	if curated.EffectiveOn == nil {
		curated.EffectiveOn = to.EffectiveOn
	}
	if curated.ExpiredOn == nil {
		curated.ExpiredOn = to.ExpiredOn
	}
	if s := strings.TrimSpace(curated.Status); s == "" || s == types.LegalStatusUnknown {
		curated.Status = to.Status
	}
	from = &curated
	upd := map[string]any{
		"document_number": from.DocumentNumber, "number_key": from.NumberKey, "title": from.Title,
		"doc_type": from.DocType, "issuer": from.Issuer, "issued_on": from.IssuedOn,
		"effective_on": from.EffectiveOn, "expired_on": from.ExpiredOn, "status": from.Status,
		"reviewed_by": from.ReviewedBy, "reviewed_at": from.ReviewedAt, "updated_at": time.Now(),
	}
	if diff := legalcatalog.ReviewDiff(from, to); len(diff) > 0 {
		b, err := json.Marshal(diff)
		if err != nil {
			return err
		}
		upd["source_pending"] = types.JSON(b)
	} else {
		upd["source_pending"] = nil
	}
	return tx.Model(&types.LegalDocument{}).Where("id = ?", to.ID).Updates(upd).Error
}
