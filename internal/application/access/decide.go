package access

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// KBAction is what a caller wants to do with one knowledge base.
type KBAction int

const (
	// ActionView reads and searches the KB (documents, chunks, previews).
	ActionView KBAction = iota + 1
	// ActionDownloadOriginal fetches original uploaded bytes.
	ActionDownloadOriginal
	// ActionContribute uploads documents and edits/deletes the caller's own
	// documents (Knowledge.CreatedBy). Route role floors and the
	// creator-or-admin document guards still apply on top.
	ActionContribute
	// ActionManage is every right on the KB: settings, sharing, deletion
	// and other people's documents.
	ActionManage
)

// String names the action for logs and tests.
func (a KBAction) String() string {
	switch a {
	case ActionView:
		return "view"
	case ActionDownloadOriginal:
		return "download_original"
	case ActionContribute:
		return "contribute"
	case ActionManage:
		return "manage"
	default:
		return "unknown"
	}
}

// RequiredPermission maps the action to the KB permission it needs.
// DownloadOriginal needs Viewer plus Decision.CanDownloadOriginal.
func (a KBAction) RequiredPermission() types.KBPermission {
	switch a {
	case ActionContribute:
		return types.KBPermissionEditor
	case ActionManage:
		return types.KBPermissionAdmin
	default:
		return types.KBPermissionViewer
	}
}

// ActionForPermission maps a legacy required permission to an action:
// Admin -> Manage, Editor -> Contribute, anything else -> View.
func ActionForPermission(required types.KBPermission) KBAction {
	switch required {
	case types.KBPermissionAdmin:
		return ActionManage
	case types.KBPermissionEditor:
		return ActionContribute
	default:
		return ActionView
	}
}

// AccessSource names the rule that granted a decision.
type AccessSource string

const (
	SourceSuperAdmin  AccessSource = "superadmin"
	SourceTenantAdmin AccessSource = "tenant_admin"
	SourceTenantScope AccessSource = "tenant_scope"
	SourceACLUser     AccessSource = "acl_user"
	SourceACLTenant   AccessSource = "acl_tenant"
	SourcePublic      AccessSource = "public"
	SourcePublished   AccessSource = "published"
	SourceNone        AccessSource = ""
)

// Decision is the caller's effective right on one KB. Permission is the
// highest permission the caller holds (not merely the one the action
// needed), so the same decision also answers "what may I do here".
type Decision struct {
	Permission          types.KBPermission
	CanDownloadOriginal bool
	Source              AccessSource
	// EffectiveTenantID is the KB's data-scope tenant; content runs there
	// after authorization. It is never the caller's identity.
	EffectiveTenantID uint64
}

// Allows reports whether the decision satisfies action.
func (d *Decision) Allows(action KBAction) bool {
	if d == nil || !d.Permission.IsValid() {
		return false
	}
	if !d.Permission.HasPermission(action.RequiredPermission()) {
		return false
	}
	return action != ActionDownloadOriginal || d.CanDownloadOriginal
}

// MembershipLookup confirms an active tenant membership. It is optional:
// HTTP requests already carry a role resolved from an active membership
// on this request, so guards pass nil. Long-lived contexts (workers, MCP
// sessions, job acceptance) pass one to re-check revocation.
type MembershipLookup interface {
	IsActiveMember(ctx context.Context, tenantID uint64, userID string) (bool, error)
}

// KBLookups are the data sources DecideKB may consult. Any nil field
// disables that source (fail closed): no tenant grant, no invite, no
// membership re-check.
type KBLookups struct {
	Grants  KBGrantLookup
	Invites KBInviteLookup
	Members MembershipLookup
}

// DecideKB is the single KB permission decision. Order:
//
//  1. Missing KB -> ErrNotFound. API-key scope is applied first; machine
//     principals (API keys, embed/IM/MCP principals, synthetic or absent
//     users) keep the pre-DecideKB rule: the owner tenant holds every
//     right, nobody else holds any. A key never becomes human.
//  2. Explicit human SuperAdmin -> Admin + download on every KB.
//  3. Active Tenant Admin of the owner tenant -> Admin + download.
//  4. Active member of the owner tenant on a 'tenant' or 'published' KB
//     -> kb.MemberDefault (Viewer or Editor) + download.
//  5. 'public' (platform-owned) or 'published' -> every authenticated
//     human, tenantless included: Viewer + download.
//  6. ACL (restricted KBs, and tenant KBs read from outside the owner
//     tenant): a live invite issued by the current owner whose recipient
//     membership is still active, or an approved grant to the caller's
//     tenant -> Viewer, no download. Guests (no tenant role) get no ACL.
//  7. Otherwise ErrUnauthorized when the caller has no tenant, else
//     ErrForbidden. A decision that does not satisfy action maps the same
//     way.
//
// There is no explicit deny. Lookup errors fail closed.
func DecideKB(ctx context.Context, caller types.Caller, kb *types.KnowledgeBase, action KBAction,
	l KBLookups,
) (*Decision, error) {
	if kb == nil || kb.ID == "" {
		return nil, ErrNotFound
	}
	if err := checkAPIKeyScope(ctx, kb.ID); err != nil {
		return nil, err
	}
	caller = caller.Normalize()
	if callerMismatch(ctx, caller) {
		return nil, ErrForbidden
	}
	var decision *Decision
	if IsExplicitHumanSuperAdmin(ctx, caller) || IsAuthenticatedHuman(ctx, caller) {
		decision = decideHumanKB(ctx, caller, kb, action, l)
	} else {
		decision = decideMachineKB(caller, kb)
	}
	if decision.Allows(action) {
		return decision, nil
	}
	if caller.TenantID == 0 && !IsExplicitHumanSuperAdmin(ctx, caller) {
		return nil, ErrUnauthorized
	}
	return nil, ErrForbidden
}

// decideMachineKB keeps the machine-principal rule that predates DecideKB:
// a principal of the owner tenant holds Admin (API-key capabilities and
// route gates narrow it), anything else holds nothing — no public,
// published or ACL reads for keys.
func decideMachineKB(caller types.Caller, kb *types.KnowledgeBase) *Decision {
	if kb.OwnerTenantID != 0 && kb.OwnerTenantID == caller.TenantID {
		return grantDecision(kb, types.KBPermissionAdmin, true, SourceTenantScope)
	}
	return nil
}

func decideHumanKB(ctx context.Context, caller types.Caller, kb *types.KnowledgeBase, action KBAction,
	l KBLookups,
) *Decision {
	if IsExplicitHumanSuperAdmin(ctx, caller) {
		return grantDecision(kb, types.KBPermissionAdmin, true, SourceSuperAdmin)
	}
	ownerMember := isActiveOwnerMember(ctx, caller, kb, l.Members)
	if ownerMember && caller.Role.IsTenantAdmin() {
		return grantDecision(kb, types.KBPermissionAdmin, true, SourceTenantAdmin)
	}
	if ownerMember && (kb.Visibility == types.KBVisibilityTenant || kb.Visibility == types.KBVisibilityPublished) {
		return grantDecision(kb, kb.MemberDefault.Permission(), true, SourceTenantScope)
	}
	if IsPlatformPublicKB(kb) {
		return grantDecision(kb, types.KBPermissionViewer, true, SourcePublic)
	}
	if kb.IsTenantPublished() {
		return grantDecision(kb, types.KBPermissionViewer, true, SourcePublished)
	}
	// ACL confers Viewer without download: skip the lookups when the
	// action needs more, so writes never cost an ACL query.
	if action != ActionView || kb.OwnerTenantID == 0 {
		return nil
	}
	if kb.Visibility != types.KBVisibilityRestricted && kb.Visibility != types.KBVisibilityTenant {
		return nil
	}
	if hasLiveInvite(ctx, caller, kb, l.Invites) {
		return grantDecision(kb, types.KBPermissionViewer, false, SourceACLUser)
	}
	if caller.HasTenantRole() && HasTenantGrant(ctx, caller, kb.ID, kb.OwnerTenantID, l.Grants) {
		return grantDecision(kb, types.KBPermissionViewer, false, SourceACLTenant)
	}
	return nil
}

func grantDecision(kb *types.KnowledgeBase, permission types.KBPermission, download bool,
	source AccessSource,
) *Decision {
	return &Decision{
		Permission: permission, CanDownloadOriginal: download,
		Source: source, EffectiveTenantID: kb.TenantID,
	}
}

// isActiveOwnerMember reports whether caller acts in the KB's owner tenant
// with an active membership there (a tenant role on this request, plus
// the optional membership re-check).
func isActiveOwnerMember(ctx context.Context, caller types.Caller, kb *types.KnowledgeBase,
	members MembershipLookup,
) bool {
	if kb.OwnerTenantID == 0 || caller.TenantID != kb.OwnerTenantID || !caller.HasTenantRole() {
		return false
	}
	if members == nil {
		return true
	}
	ok, err := members.IsActiveMember(ctx, kb.OwnerTenantID, caller.UserID)
	return err == nil && ok
}

// hasLiveInvite reports whether caller (a real human, never a key) holds a
// live invite on kb. The lookup checks the current owner, expiry and the
// recipient's active membership in recipient_tenant_id, so a Guest (no
// active membership) never passes. Errors fail closed.
func hasLiveInvite(ctx context.Context, caller types.Caller, kb *types.KnowledgeBase, invites KBInviteLookup) bool {
	if invites == nil || !IsHumanCaller(caller) {
		return false
	}
	if _, isKey := types.TenantAPIKeyScopeFromContext(ctx); isKey {
		return false
	}
	ok, err := invites.HasLiveInvite(ctx, kb.ID, caller.UserID)
	return err == nil && ok
}

// accessFromDecision projects a decision onto the legacy KBAccess grant.
// operation caps the grant carried in context (a read never mints a
// reusable write grant).
func accessFromDecision(kb *types.KnowledgeBase, caller types.Caller, d *Decision,
	operation types.KBPermission,
) *KBAccess {
	return &KBAccess{
		KnowledgeBase: kb, Caller: caller.Normalize(),
		EffectiveTenantID: d.EffectiveTenantID, Permission: d.Permission,
		Source: d.Source, CanDownloadOriginal: d.CanDownloadOriginal,
		operationPermission: operation,
	}
}
