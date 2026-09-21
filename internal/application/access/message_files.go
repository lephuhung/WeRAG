package access

import (
	"context"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// MessageFileLookup is the narrow message-service surface needed by the
// message-scoped file proxy. Keeping it small makes the authorization boundary
// independently testable.
type MessageFileLookup interface {
	GetMessage(ctx context.Context, sessionID, messageID string) (*types.Message, error)
}

// KBGrantPermissionGuard is the grant surface the message-scoped proxy
// needs for its granted-KB fallback.
type KBGrantPermissionGuard = KBGrantLookup

// KBTenantLookup resolves knowledge bases without a tenant filter, so a
// message's persisted retrieval evidence can be mapped back to the knowledge
// bases the chunks were retrieved from.
type KBTenantLookup interface {
	GetKnowledgeBasesByIDsOnly(ctx context.Context, ids []string) ([]*types.KnowledgeBase, error)
}

// KnowledgeOwnerLookup resolves a knowledge entry to its knowledge base for
// message references that predate the denormalized knowledge_base_id field.
type KnowledgeOwnerLookup interface {
	GetKnowledgeByIDOnly(ctx context.Context, id string) (*types.Knowledge, error)
}

// MessageKBGrantAuthorizer bundles the read-only lookups behind the
// message proxy's granted-KB fallback. A nil GrantGuard or KBs lookup
// disables the fallback. A live Bindings lookup is also required and can
// be supplied by the resource catalog. A nil Knowledges lookup only disables the
// pre-denormalization KnowledgeID path.
type MessageKBGrantAuthorizer struct {
	GrantGuard KBGrantPermissionGuard
	KBs        KBTenantLookup
	Knowledges KnowledgeOwnerLookup
	Bindings   interfaces.KBResourceLookup
}

// resourceAccessibleViaGrantedKB reports whether the message's persisted
// retrieval evidence proves the resource came from a KB granted to the
// caller's tenant. This is the fallback for replies whose retrieval pulled
// chunks from a knowledge base owned by another tenant via an approved
// access grant.
//
// Evidence required from one retrieval record: a canonical resource://
// handle (not a prefix of a longer token) appears in the chunk text or
// image_info, its knowledge base belongs to the resource's tenant, and
// that KB is granted to the caller's tenant with at least viewer
// permission, and an independent live resource binding confirms the file
// still belongs to it. Smart-reasoning turns persist that evidence on
// AgentSteps when KnowledgeReferences was never filled. Any lookup
// failure fails closed.
func (a MessageKBGrantAuthorizer) resourceAccessibleViaGrantedKB(
	ctx context.Context,
	message *types.Message,
	resource *types.StoredResource,
	callerTenantID uint64,
) bool {
	if a.GrantGuard == nil || a.KBs == nil || a.Bindings == nil || message == nil || resource == nil {
		return false
	}
	handle, ok := types.ParseResourcePath(types.BuildResourcePath(resource.Handle))
	if !ok {
		return false
	}

	kbIDs := a.collectSharedKBEvidenceIDs(ctx, message, handle)
	if len(kbIDs) == 0 {
		return false
	}

	kbs, err := a.KBs.GetKnowledgeBasesByIDsOnly(ctx, kbIDs)
	if err != nil {
		return false
	}
	permissions := NewKBGrantPermissions(ctx, a.GrantGuard, callerTenantID)
	for _, kb := range kbs {
		if kb == nil || kb.TenantID != resource.TenantID {
			continue
		}
		granted, err := permissions.Check(kb.ID, types.KBPermissionViewer)
		if err == nil && granted {
			bound, bindingErr := a.Bindings.IsReferencedByKnowledgeBase(
				ctx, resource.TenantID, kb.ID, types.BuildResourcePath(handle))
			if bindingErr == nil && bound {
				return true
			}
		}
	}
	return false
}

func (a MessageKBGrantAuthorizer) collectSharedKBEvidenceIDs(
	ctx context.Context,
	message *types.Message,
	handle string,
) []string {
	seenKB := make(map[string]bool)
	seenKnowledge := make(map[string]bool)
	var kbIDs, knowledgeIDs []string
	addKB := func(id string) {
		if id == "" || seenKB[id] {
			return
		}
		seenKB[id] = true
		kbIDs = append(kbIDs, id)
	}
	addKnowledge := func(id string) {
		if id == "" || seenKnowledge[id] {
			return
		}
		seenKnowledge[id] = true
		knowledgeIDs = append(knowledgeIDs, id)
	}

	for _, ref := range message.KnowledgeReferences {
		if !searchResultHasResourceHandle(ref, handle) {
			continue
		}
		if ref.KnowledgeBaseID != "" {
			addKB(ref.KnowledgeBaseID)
			continue
		}
		addKnowledge(ref.KnowledgeID)
	}
	collectKBEvidenceFromValue(message.AgentSteps, handle, "", "", addKB, addKnowledge)

	if a.Knowledges != nil {
		for _, knowledgeID := range knowledgeIDs {
			knowledge, err := a.Knowledges.GetKnowledgeByIDOnly(ctx, knowledgeID)
			if err != nil || knowledge == nil {
				continue
			}
			addKB(knowledge.KnowledgeBaseID)
		}
	}
	return kbIDs
}

func searchResultHasResourceHandle(ref *types.SearchResult, handle string) bool {
	if ref == nil {
		return false
	}
	return textHasResourceHandle(ref.Content, handle) ||
		textHasResourceHandle(ref.MatchedContent, handle) ||
		textHasResourceHandle(ref.ImageInfo, handle)
}

func textHasResourceHandle(text, handle string) bool {
	if text == "" || handle == "" {
		return false
	}
	want := types.BuildResourcePath(handle)
	for _, ref := range types.ScanResourceReferences(text) {
		if ref == want {
			return true
		}
	}
	return false
}

func collectKBEvidenceFromValue(
	v interface{},
	handle, kbID, knowledgeID string,
	addKB, addKnowledge func(string),
) {
	switch val := v.(type) {
	case string:
		if !textHasResourceHandle(val, handle) {
			return
		}
		if kbID != "" {
			addKB(kbID)
			return
		}
		addKnowledge(knowledgeID)
	case types.AgentSteps:
		for _, step := range val {
			collectKBEvidenceFromValue(step, handle, kbID, knowledgeID, addKB, addKnowledge)
		}
	case types.AgentStep:
		collectKBEvidenceFromValue(val.ToolCalls, handle, kbID, knowledgeID, addKB, addKnowledge)
	case []types.ToolCall:
		for _, call := range val {
			collectKBEvidenceFromValue(call, handle, kbID, knowledgeID, addKB, addKnowledge)
		}
	case types.ToolCall:
		if val.Result != nil {
			collectKBEvidenceFromValue(val.Result.Output, handle, kbID, knowledgeID, addKB, addKnowledge)
			collectKBEvidenceFromValue(val.Result.Data, handle, kbID, knowledgeID, addKB, addKnowledge)
		}
	case map[string]interface{}:
		nextKB := firstNonEmptyString(
			stringFromAnyMap(val, "knowledge_base_id"),
			stringFromAnyMap(val, "knowledge_base"),
			kbID,
		)
		nextKnowledge := firstNonEmptyString(stringFromAnyMap(val, "knowledge_id"), knowledgeID)
		for _, nested := range val {
			collectKBEvidenceFromValue(nested, handle, nextKB, nextKnowledge, addKB, addKnowledge)
		}
	case []interface{}:
		for _, item := range val {
			collectKBEvidenceFromValue(item, handle, kbID, knowledgeID, addKB, addKnowledge)
		}
	case []map[string]interface{}:
		for _, item := range val {
			collectKBEvidenceFromValue(item, handle, kbID, knowledgeID, addKB, addKnowledge)
		}
	}
}

func stringFromAnyMap(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	s, _ := m[key].(string)
	return strings.TrimSpace(s)
}

func firstNonEmptyString(values ...string) string {
	if len(values) == 0 {
		return ""
	}
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
