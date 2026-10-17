package access

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// KBFileGate decides reads of stored resources that belong to knowledge
// base content. A resource bound to a KB document is readable only by a
// caller DecideKB admits on (at least) one of those KBs: ActionView for
// derived files (extracted images), ActionDownloadOriginal when the
// resource is a document's original upload. Same-tenant storage alone
// never grants it, so restricted KBs stay closed to members without an
// ACL and ACL viewers never fetch originals.
type KBFileGate struct {
	Bindings interfaces.MessageFileBindingLookup
	KBs      KBTenantLookup
	Lookups  KBLookups
}

// Active reports whether the gate has the lookups it needs.
func (g KBFileGate) Active() bool { return g.Bindings != nil && g.KBs != nil }

// Authorize reports whether the resource is KB content (bound) and, if so,
// whether the caller may read it (err == nil). Lookup failures are treated
// as bound and denied (fail closed). An inactive gate reports unbound.
func (g KBFileGate) Authorize(ctx context.Context, tenantID uint64, reference string) (bool, error) {
	if !g.Active() || tenantID == 0 || reference == "" {
		return false, nil
	}
	bindings, err := g.Bindings.GetMessageFileBindings(ctx, tenantID, reference, "")
	if err != nil {
		return true, ErrForbidden
	}
	if bindings == nil || len(bindings.KnowledgeBaseIDs) == 0 {
		return false, nil
	}
	original := make(map[string]bool, len(bindings.SourceFileKBIDs))
	for _, id := range bindings.SourceFileKBIDs {
		original[id] = true
	}
	kbs, err := g.KBs.GetKnowledgeBasesByIDsOnly(ctx, bindings.KnowledgeBaseIDs)
	if err != nil {
		return true, ErrForbidden
	}
	caller := types.CallerFromContext(ctx)
	for _, kb := range kbs {
		if kb == nil || kb.TenantID != tenantID {
			continue
		}
		action := ActionView
		if original[kb.ID] {
			action = ActionDownloadOriginal
		}
		if _, err := DecideKB(ctx, caller, kb, action, g.Lookups); err == nil {
			return true, nil
		}
	}
	return true, ErrForbidden
}

// NewKBFileGate builds a gate from a resource catalog, a KB loader and the
// grant service; the grant service doubles as the invite lookup when it
// implements KBInviteLookup.
func NewKBFileGate(catalog any, kbs KBTenantLookup, grants KBGrantLookup) KBFileGate {
	gate := KBFileGate{KBs: kbs, Lookups: KBLookups{Grants: grants}}
	gate.Bindings, _ = catalog.(interfaces.MessageFileBindingLookup)
	if invites, ok := grants.(KBInviteLookup); ok {
		gate.Lookups.Invites = invites
	}
	return gate
}
