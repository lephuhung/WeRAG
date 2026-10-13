package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

const (
	// legalExpansionMaxResults caps the chunks added from related documents.
	legalExpansionMaxResults = 4
	// legalExpansionMaxNotes caps the catalogue-only lines.
	legalExpansionMaxNotes = 10
	// legalExpiredStatus is the portal status of a document no longer in force.
	legalExpiredStatus = "Hết hiệu lực toàn bộ"
)

// WithLegalCatalog enables legal expansion: hits on catalogued legal
// documents pull in the documents that guide or amend them.
func (t *SearchKnowledgeTool) WithLegalCatalog(svc interfaces.LegalCatalogService) *SearchKnowledgeTool {
	t.legalCatalog = svc
	return t
}

// expandLegalRelations follows the legal catalogue from the hits on
// catalogued documents to the documents replacing, guiding (two hops) or
// amending them. A related document in force, held by a KB the search covers
// whole, is searched with the same query and its best chunk is added after
// the hits.
// The others — expired, not held, or outside the search scope — are only
// named in the returned note: their content is not current law here.
func (t *SearchKnowledgeTool) expandLegalRelations(
	ctx context.Context, query, mode string, kbModes map[string]kbSearchMode,
	kbList []*types.KnowledgeBase, kbTypeMap map[string]string, searchTargets types.SearchTargets,
	final []*searchResultWithMeta, topK int, vectorThreshold, keywordThreshold float64,
) ([]*searchResultWithMeta, string) {
	if t.legalCatalog == nil || len(final) == 0 {
		return final, ""
	}
	held := make(map[string]bool, len(final))
	hitIDs := make([]string, 0, len(final))
	for _, r := range final {
		if r.KnowledgeID != "" && !held[r.KnowledgeID] {
			held[r.KnowledgeID] = true
			hitIDs = append(hitIDs, r.KnowledgeID)
		}
	}
	expansions, err := t.legalCatalog.ExpandRelated(ctx, hitIDs)
	if err != nil {
		logger.Warnf(ctx, "[Tool][SearchKnowledge] Legal expansion failed: %v", err)
		return final, ""
	}
	if len(expansions) == 0 {
		return final, ""
	}
	// Only whole-KB targets: a search the user narrowed to files or tags
	// must not reach other documents of the KB.
	wholeKB := map[string]*types.SearchTarget{}
	for _, st := range searchTargets {
		if st != nil && st.Type == types.SearchTargetTypeKnowledgeBase && len(st.TagIDs) == 0 {
			wholeKB[st.KnowledgeBaseID] = st
		}
	}
	byKB := map[string][]string{}
	relatedOf := map[string]types.LegalExpansion{}
	var notes []string
	for _, e := range expansions {
		r := e.Related
		var reason string
		switch {
		case r.Document.Status == legalExpiredStatus:
			reason = "đã hết hiệu lực toàn bộ, không dùng nội dung"
		case len(r.Knowledge) == 0:
			reason = "chưa có trong kho tài liệu"
		default:
			ref, hit := searchableLegalCopy(r.Knowledge, held, wholeKB)
			switch {
			case hit:
				continue // already among the hits
			case ref == nil:
				reason = "ngoài phạm vi tìm kiếm"
			default:
				byKB[ref.KnowledgeBaseID] = append(byKB[ref.KnowledgeBaseID], ref.KnowledgeID)
				relatedOf[ref.KnowledgeID] = e
				continue
			}
		}
		if len(notes) < legalExpansionMaxNotes {
			notes = append(notes, fmt.Sprintf("- %s (%s) — %s: %s",
				legalDocLabel(r.Document), legalOr(r.Document.Status, "chưa rõ tình trạng"),
				legalExpansionRelation(e), reason))
		}
	}

	if len(byKB) > 0 {
		targets := make(types.SearchTargets, 0, len(byKB))
		for kbID, ids := range byKB {
			targets = append(targets, &types.SearchTarget{
				Type: types.SearchTargetTypeKnowledge, KnowledgeBaseID: kbID,
				TenantID: wholeKB[kbID].TenantID, KnowledgeIDs: ids,
			})
		}
		found := t.concurrentSearchByTargets(ctx, query, mode, kbModes, targets, kbList,
			topK, vectorThreshold, keywordThreshold, kbTypeMap)
		sort.SliceStable(found, func(i, j int) bool { return found[i].Score > found[j].Score })
		added := 0
		for _, r := range found {
			e, ok := relatedOf[r.KnowledgeID]
			if !ok || held[r.KnowledgeID] || added == legalExpansionMaxResults {
				continue
			}
			held[r.KnowledgeID] = true // best chunk per related document
			note := "Văn bản liên quan: " + legalExpansionRelation(e)
			if r.KnowledgeCustomMetadata != "" {
				note += "\n" + r.KnowledgeCustomMetadata
			}
			r.KnowledgeCustomMetadata = note
			final = append(final, r)
			added++
		}
	}
	if len(notes) == 0 {
		return final, ""
	}
	return final, "Văn bản liên quan theo quan hệ pháp lý (chỉ thông tin danh mục, không có nội dung):\n" +
		strings.Join(notes, "\n")
}

// searchableLegalCopy picks the copy of a related document to search: one
// in a KB the search covers whole. hit reports a copy already among the hits.
func searchableLegalCopy(
	refs []types.LegalKnowledgeRef, held map[string]bool, wholeKB map[string]*types.SearchTarget,
) (ref *types.LegalKnowledgeRef, hit bool) {
	for i := range refs {
		if held[refs[i].KnowledgeID] {
			return nil, true
		}
	}
	for i := range refs {
		if wholeKB[refs[i].KnowledgeBaseID] != nil {
			return &refs[i], false
		}
	}
	return nil, false
}

// legalExpansionRelation words how a related document stands to the hit.
func legalExpansionRelation(e types.LegalExpansion) string {
	r := e.Related
	verb := legalRelatedLabels[r.Relation][1]
	if !r.Incoming {
		verb = legalRelatedLabels[r.Relation][0]
	}
	anchor := legalDocLabel(e.Anchor)
	if r.Depth > 1 {
		return verb + " văn bản hướng dẫn " + anchor
	}
	return verb + " " + anchor
}
