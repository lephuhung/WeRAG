package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// legalReadableCopies lists, per catalogue document, the copies the caller
// may read across the knowledge bases they can see: the approved review
// links, and the imports whose metadata (source + external_id) is vouched
// for — a platform public KB (only a SuperAdmin or the platform API key
// writes there) or the caller's own organization. Another organization's
// published or granted KB counts only through an approved link. Best
// effort: a failing lookup logs and yields fewer copies, never an error.
func legalReadableCopies(
	ctx context.Context, repo interfaces.LegalCatalogRepository, reviews interfaces.LegalReviewRepository,
	kbs interfaces.KnowledgeBaseRepository, ids []string,
) map[string][]types.LegalKnowledgeRef {
	out := map[string][]types.LegalKnowledgeRef{}
	if len(ids) == 0 || kbs == nil {
		return out
	}
	caller := types.CallerFromContext(ctx)
	list, err := kbs.ListReadableKnowledgeBases(ctx, access.KBReaderFor(ctx, caller))
	if err != nil {
		logger.Warnf(ctx, "[LegalLookup] readable knowledge bases unavailable: %v", err)
		return out
	}
	readable := map[string]bool{}
	var vouchedKBIDs []string
	for _, kb := range list {
		if kb == nil || !access.InDefaultRetrievalScope(caller, kb) || readable[kb.ID] {
			continue
		}
		readable[kb.ID] = true
		if access.IsPlatformPublicKB(kb) || (caller.TenantID != 0 && kb.OwnerTenantID == caller.TenantID) {
			vouchedKBIDs = append(vouchedKBIDs, kb.ID)
		}
	}
	if len(readable) == 0 {
		return out
	}
	seen := map[string]bool{}
	add := func(docID string, r types.LegalKnowledgeRef) {
		if readable[r.KnowledgeBaseID] && !seen[docID+"\x00"+r.KnowledgeID] {
			seen[docID+"\x00"+r.KnowledgeID] = true
			out[docID] = append(out[docID], r)
		}
	}
	if reviews != nil {
		if linked, err := reviews.KnowledgeForDocuments(ctx, ids); err != nil {
			logger.Warnf(ctx, "[LegalLookup] linked copies unavailable: %v", err)
		} else {
			for docID, refs := range linked {
				for _, r := range refs {
					add(docID, r)
				}
			}
		}
	}
	if repo != nil {
		if imported, err := repo.ExternalCopies(ctx, ids, vouchedKBIDs); err != nil {
			logger.Warnf(ctx, "[LegalLookup] imported copies unavailable: %v", err)
		} else {
			for docID, refs := range imported {
				for _, r := range refs {
					add(docID, r)
				}
			}
		}
	}
	return out
}
