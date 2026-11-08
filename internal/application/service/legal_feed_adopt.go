package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/legalcatalog"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// adoptFeedTwin runs before a portal document (vbplID) is ingested: a
// legal feed's row of the same document (same số hiệu and
// legalcatalog.SameDocument) hands its references to the portal row first
// (repository.AdoptLegalDocument), so the feed's KB copy is replaced by the
// portal's instead of standing next to it. A push of the feed row itself,
// and a document no feed row matches, pass through. A feed copy in another
// public KB than kbID (the ingest's) is refused before anything moves: a
// document lives in one KB.
func (s *LegalFeedService) adoptFeedTwin(ctx context.Context, vbplID, documentNumber, kbID string) error {
	db := s.db.WithContext(ctx)
	var row types.LegalDocument
	if err := db.Where("id = ?", vbplID).Limit(1).Find(&row).Error; err != nil {
		return fmt.Errorf("catalogue lookup: %w", err)
	}
	if row.IsFeedRow() {
		return nil
	}
	key := row.NumberKey
	if key == "" {
		key = vietnamese_legal.CatalogNumberKey(documentNumber)
	}
	if key == "" {
		return nil
	}
	var rows []*types.LegalDocument
	if err := db.Where("number_key = ? AND id <> ? AND source NOT IN ?", key, vbplID,
		[]string{types.LegalDocumentSourceVBPL, types.LegalDocumentSourceManual}).Find(&rows).Error; err != nil {
		return fmt.Errorf("feed rows of %s: %w", key, err)
	}
	var feed []*types.LegalDocument
	for _, f := range rows {
		if f.IsFeedRow() {
			feed = append(feed, f)
		}
	}
	if len(feed) == 0 {
		return nil
	}
	if row.ID == "" {
		// without the portal's catalogue row the feed row cannot be told
		// to be the same document; one held in a KB would end up twice
		for _, f := range feed {
			held, err := s.heldPublic(ctx, f.ID)
			if err != nil {
				return err
			}
			if len(held) > 0 {
				return refuse("văn bản %s (%s) đã có bản toàn văn từ nguồn %s (dòng %s) nhưng danh mục chưa có dòng vbpl %s: "+
					"chạy nhập danh mục vbpl trước để gộp hai dòng", vbplID, documentNumber, f.Source, f.ID, vbplID)
			}
		}
		return nil
	}
	var same []*types.LegalDocument
	for _, f := range feed {
		if legalcatalog.SameDocument(f, &row) {
			same = append(same, f)
		}
	}
	switch len(same) {
	case 0:
		return nil
	case 1:
	default:
		ids := make([]string, 0, len(same))
		for _, f := range same {
			ids = append(ids, f.ID)
		}
		return refuse("nhiều dòng danh mục từ nguồn ngoài (%s) cùng là văn bản %s: SuperAdmin chọn dòng giữ lại trước khi nạp",
			strings.Join(ids, ", "), vbplID)
	}
	f := same[0]
	heldPortal, err := s.heldPublic(ctx, vbplID)
	if err != nil {
		return err
	}
	heldFeed, err := s.heldPublic(ctx, f.ID)
	if err != nil {
		return err
	}
	if len(heldPortal) > 0 && len(heldFeed) > 0 {
		return refuse("văn bản %s đã có hai bản toàn văn (vbpl %s và nguồn %s %s): SuperAdmin xoá một bản trước khi nạp",
			row.DocumentNumber, vbplID, f.Source, f.ID)
	}
	for _, other := range heldFeed {
		if other != kbID {
			return refuse("văn bản %s đã có bản toàn văn từ nguồn %s (dòng %s) ở kho %s, khác kho %s đang nạp: "+
				"một văn bản chỉ nằm ở một kho — xoá bản đó hoặc nạp vào kho %s", row.DocumentNumber, f.Source, f.ID, other, kbID, other)
		}
	}
	res, err := repository.AdoptLegalDocument(ctx, s.db, f.ID, vbplID)
	if err != nil {
		return fmt.Errorf("adopt feed row %s: %w", f.ID, err)
	}
	logger.Infof(ctx, "[LegalFeed] feed row %s adopted by %s before ingest: %d knowledges, %d provision links, %d candidates; "+
		"rebuild the catalogue wiki of KBs %s after the ingest", f.ID, vbplID, len(res.KnowledgeIDs), res.ProvisionLinks,
		res.Candidates, strings.Join(res.KnowledgeBaseIDs, ","))
	return nil
}

// heldPublic lists the public KBs holding the catalogue document id.
func (s *LegalFeedService) heldPublic(ctx context.Context, id string) ([]string, error) {
	var kbs []string
	ext := legalJSONText(s.db, "k.metadata", "external_id")
	err := s.db.WithContext(ctx).Raw(`SELECT DISTINCT k.knowledge_base_id FROM knowledges k
		JOIN knowledge_bases kb ON kb.id = k.knowledge_base_id AND kb.owner_tenant_id = 0 AND kb.deleted_at IS NULL
		WHERE k.deleted_at IS NULL AND `+ext+` = ?`, id).Scan(&kbs).Error
	if err != nil {
		return nil, fmt.Errorf("held document lookup: %w", err)
	}
	return kbs, nil
}
