package types

import "strings"

// MCPEndpointToolGroup partitions the endpoint tool catalog by the kind of
// access a tool needs. Groups map onto tenant API key capabilities so the
// same route/scope checks that guard the REST API also bound MCP tools.
type MCPEndpointToolGroup string

// Tool groups in display order.
const (
	MCPEndpointToolGroupRetrieve MCPEndpointToolGroup = "retrieve"
	MCPEndpointToolGroupChat     MCPEndpointToolGroup = "chat"
	MCPEndpointToolGroupWiki     MCPEndpointToolGroup = "wiki"
	MCPEndpointToolGroupIngest   MCPEndpointToolGroup = "ingest"
	// MCPEndpointToolGroupLegal reads the platform-wide legal catalogue
	// (validity, relations) and audits the public legal KBs. Read-only; it
	// maps onto the retrieve capability like the other read groups.
	MCPEndpointToolGroupLegal MCPEndpointToolGroup = "legal"
	// MCPEndpointToolGroupLegalWrite feeds the public legal knowledge bases
	// (ingest / remove vbpl.vn documents, review scan, wiki rebuild). Every
	// tool is destructive and needs the ingest capability; writes are
	// logged in legal_feed_log.
	MCPEndpointToolGroupLegalWrite MCPEndpointToolGroup = "legal_write"
)

// MCP endpoint tool names. These are the names external MCP clients see.
// They intentionally differ from the agent-internal tool names where the
// external contract is simpler (one query, server-side session handling).
const (
	MCPEndpointToolListKnowledgeBases = "list_knowledge_bases"
	MCPEndpointToolSearchKnowledge    = "search_knowledge"
	MCPEndpointToolGrepChunks         = "grep_chunks"
	MCPEndpointToolListDocuments      = "list_documents"
	MCPEndpointToolReadDocument       = "read_document"
	MCPEndpointToolAsk                = "ask"
	MCPEndpointToolWikiSearch         = "wiki_search"
	MCPEndpointToolWikiReadPage       = "wiki_read_page"
	MCPEndpointToolWikiIndex          = "wiki_index"
	MCPEndpointToolAddDocument        = "add_document"
	MCPEndpointToolUpdateDocument     = "update_document"
	MCPEndpointToolDeleteDocument     = "delete_document"
	MCPEndpointToolLegalCheckStatus   = "legal_check_status"
	MCPEndpointToolLegalFindRelated   = "legal_find_related"
	MCPEndpointToolLegalCatalogSearch = "legal_catalog_search"
	MCPEndpointToolLegalListIngested  = "legal_list_ingested"
	MCPEndpointToolLegalExpiryAudit   = "legal_expiry_audit"
	MCPEndpointToolLegalSyncStatus    = "legal_catalog_sync_status"
	MCPEndpointToolLegalListMissing   = "legal_list_missing"
	MCPEndpointToolLegalListRequests  = "legal_list_requests"
	MCPEndpointToolLegalIngest        = "legal_ingest_document"
	MCPEndpointToolLegalRemove        = "legal_remove_document"
	MCPEndpointToolLegalApplyExpiry   = "legal_apply_expiry_audit"
	MCPEndpointToolLegalReviewScan    = "legal_review_scan"
	MCPEndpointToolLegalWikiRebuild   = "legal_wiki_rebuild"
)

// MCPEndpointToolDefinition is the settings-facing description of one tool.
// Label and Description are i18n keys resolved by the frontend.
type MCPEndpointToolDefinition struct {
	Name  string               `json:"name"`
	Group MCPEndpointToolGroup `json:"group"`
	// Destructive marks tools that mutate or delete organization content; the UI
	// leaves them unchecked by default and the server requires the ingest
	// capability for them.
	Destructive bool `json:"destructive"`
}

// mcpEndpointToolCatalog is the ordered catalog. Order is the display order.
var mcpEndpointToolCatalog = []MCPEndpointToolDefinition{
	{Name: MCPEndpointToolListKnowledgeBases, Group: MCPEndpointToolGroupRetrieve},
	{Name: MCPEndpointToolSearchKnowledge, Group: MCPEndpointToolGroupRetrieve},
	{Name: MCPEndpointToolGrepChunks, Group: MCPEndpointToolGroupRetrieve},
	{Name: MCPEndpointToolListDocuments, Group: MCPEndpointToolGroupRetrieve},
	{Name: MCPEndpointToolReadDocument, Group: MCPEndpointToolGroupRetrieve},
	{Name: MCPEndpointToolAsk, Group: MCPEndpointToolGroupChat},
	{Name: MCPEndpointToolWikiSearch, Group: MCPEndpointToolGroupWiki},
	{Name: MCPEndpointToolWikiReadPage, Group: MCPEndpointToolGroupWiki},
	{Name: MCPEndpointToolWikiIndex, Group: MCPEndpointToolGroupWiki},
	{Name: MCPEndpointToolAddDocument, Group: MCPEndpointToolGroupIngest, Destructive: true},
	{Name: MCPEndpointToolUpdateDocument, Group: MCPEndpointToolGroupIngest, Destructive: true},
	{Name: MCPEndpointToolDeleteDocument, Group: MCPEndpointToolGroupIngest, Destructive: true},
	{Name: MCPEndpointToolLegalCheckStatus, Group: MCPEndpointToolGroupLegal},
	{Name: MCPEndpointToolLegalFindRelated, Group: MCPEndpointToolGroupLegal},
	{Name: MCPEndpointToolLegalCatalogSearch, Group: MCPEndpointToolGroupLegal},
	{Name: MCPEndpointToolLegalListIngested, Group: MCPEndpointToolGroupLegal},
	{Name: MCPEndpointToolLegalExpiryAudit, Group: MCPEndpointToolGroupLegal},
	{Name: MCPEndpointToolLegalSyncStatus, Group: MCPEndpointToolGroupLegal},
	{Name: MCPEndpointToolLegalListMissing, Group: MCPEndpointToolGroupLegal},
	{Name: MCPEndpointToolLegalListRequests, Group: MCPEndpointToolGroupLegal},
	{Name: MCPEndpointToolLegalIngest, Group: MCPEndpointToolGroupLegalWrite, Destructive: true},
	{Name: MCPEndpointToolLegalRemove, Group: MCPEndpointToolGroupLegalWrite, Destructive: true},
	{Name: MCPEndpointToolLegalApplyExpiry, Group: MCPEndpointToolGroupLegalWrite, Destructive: true},
	{Name: MCPEndpointToolLegalReviewScan, Group: MCPEndpointToolGroupLegalWrite, Destructive: true},
	{Name: MCPEndpointToolLegalWikiRebuild, Group: MCPEndpointToolGroupLegalWrite, Destructive: true},
}

// MCPEndpointToolCatalog returns a copy of the ordered tool catalog.
func MCPEndpointToolCatalog() []MCPEndpointToolDefinition {
	out := make([]MCPEndpointToolDefinition, len(mcpEndpointToolCatalog))
	copy(out, mcpEndpointToolCatalog)
	return out
}

// MCPEndpointToolGroups returns the groups in display order.
func MCPEndpointToolGroups() []MCPEndpointToolGroup {
	return []MCPEndpointToolGroup{
		MCPEndpointToolGroupRetrieve,
		MCPEndpointToolGroupChat,
		MCPEndpointToolGroupWiki,
		MCPEndpointToolGroupIngest,
		MCPEndpointToolGroupLegal,
		MCPEndpointToolGroupLegalWrite,
	}
}

// DefaultMCPEndpointTools is the allowlist a new endpoint starts with: every
// read-only tool, nothing that writes.
func DefaultMCPEndpointTools() []string {
	out := make([]string, 0, len(mcpEndpointToolCatalog))
	for _, def := range mcpEndpointToolCatalog {
		if !def.Destructive {
			out = append(out, def.Name)
		}
	}
	return out
}

// LookupMCPEndpointTool returns the catalog entry for name.
func LookupMCPEndpointTool(name string) (MCPEndpointToolDefinition, bool) {
	name = strings.TrimSpace(name)
	for _, def := range mcpEndpointToolCatalog {
		if def.Name == name {
			return def, true
		}
	}
	return MCPEndpointToolDefinition{}, false
}

// NormalizeMCPEndpointTools drops unknown and duplicate names and returns
// the survivors in catalog order so the stored allowlist is canonical.
func NormalizeMCPEndpointTools(in []string) []string {
	wanted := map[string]struct{}{}
	for _, name := range in {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		wanted[name] = struct{}{}
	}
	out := make([]string, 0, len(wanted))
	for _, def := range mcpEndpointToolCatalog {
		if _, ok := wanted[def.Name]; ok {
			out = append(out, def.Name)
		}
	}
	return out
}

// MCPEndpointCapabilitiesForTools derives the tenant API key capabilities an
// endpoint needs so its calls pass the same scope checks as a scoped API key.
func MCPEndpointCapabilitiesForTools(tools []string) []string {
	groups := map[MCPEndpointToolGroup]bool{}
	for _, name := range tools {
		if def, ok := LookupMCPEndpointTool(name); ok {
			groups[def.Group] = true
		}
	}
	caps := []string{}
	if groups[MCPEndpointToolGroupRetrieve] || groups[MCPEndpointToolGroupWiki] || groups[MCPEndpointToolGroupChat] ||
		groups[MCPEndpointToolGroupLegal] {
		caps = append(caps, string(APIKeyCapabilityRetrieve))
	}
	if groups[MCPEndpointToolGroupChat] {
		caps = append(caps, string(APIKeyCapabilityChat), string(APIKeyCapabilityReadAgents))
	}
	if groups[MCPEndpointToolGroupIngest] || groups[MCPEndpointToolGroupLegalWrite] {
		caps = append(caps, string(APIKeyCapabilityIngest))
	}
	return caps
}

// MCPEndpointScope builds the API-key style scope that the MCP server
// attaches to every call on this endpoint, so downstream services apply the
// existing knowledge-base and capability checks unchanged.
func MCPEndpointScope(ep *MCPEndpoint) TenantAPIKeyScope {
	if ep == nil {
		return TenantAPIKeyScope{}
	}
	kbIDs := StringArray{}
	if len(ep.KnowledgeBaseIDs) > 0 {
		kbIDs = append(kbIDs, ep.KnowledgeBaseIDs...)
	}
	return TenantAPIKeyScope{
		ScopeType:        APIKeyScopeTenant,
		FullAccess:       false,
		KnowledgeBaseIDs: kbIDs,
		Capabilities:     StringArray(MCPEndpointCapabilitiesForTools(ep.Tools)),
	}
}

// MCPEndpointAgentAllowed reports whether an endpoint of tenantID may run
// agent for the ask tool. Tenant-owned agents qualify; builtins qualify only
// when they are user-facing (listed by GetBuiltinAgentIDs). Internal builtins
// such as the wiki fixer and the skill installer stay unreachable because
// they ship write and shell tools that the endpoint allowlist never covers.
func MCPEndpointAgentAllowed(agent *CustomAgent, tenantID uint64) bool {
	if agent == nil {
		return false
	}
	if agent.IsBuiltin || IsBuiltinAgentID(agent.ID) {
		for _, id := range GetBuiltinAgentIDs() {
			if id == agent.ID {
				return true
			}
		}
		return false
	}
	return agent.TenantID == tenantID
}
