package access

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// KBReaderFor classifies caller for a readable-KB listing with the same
// predicates DecideKB uses: explicit human SuperAdmin, authenticated human
// (never an API key or other machine principal), and the tenant role held
// on this request. The repository turns the result into one SQL query, so
// listings and per-KB decisions stay in step.
func KBReaderFor(ctx context.Context, caller types.Caller) types.KBReader {
	caller = caller.Normalize()
	reader := types.KBReader{TenantID: caller.TenantID, UserID: caller.UserID}
	if IsExplicitHumanSuperAdmin(ctx, caller) {
		reader.SuperAdmin = true
		reader.Human = true
		return reader
	}
	reader.Human = IsAuthenticatedHuman(ctx, caller)
	if caller.TenantID != 0 && caller.HasTenantRole() {
		reader.TenantMember = true
		reader.TenantAdmin = caller.Role.IsTenantAdmin()
	}
	return reader
}

// InDefaultRetrievalScope reports whether a KB from a readable listing
// belongs to the caller's default retrieval scope ("all knowledge bases"
// agents, workspace-wide document search). A SuperAdmin may view every KB,
// but that platform-wide reach is not a default scope: SuperAdmin rows are
// kept only for the caller's own tenant and platform public KBs. Rows the
// caller holds through any other source stay in scope.
func InDefaultRetrievalScope(caller types.Caller, kb *types.KnowledgeBase) bool {
	if kb == nil {
		return false
	}
	if kb.AccessSource != string(SourceSuperAdmin) {
		return true
	}
	return IsPlatformPublicKB(kb) || (caller.TenantID != 0 && kb.OwnerTenantID == caller.TenantID)
}
