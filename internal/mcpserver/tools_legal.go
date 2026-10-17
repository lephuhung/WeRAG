package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/mark3labs/mcp-go/mcp"
)

// Legal tools expose the platform-wide legal catalogue (vbpl.vn metadata and
// relations, see docs/legal-document-feed-spec.md) and an audit of the
// public legal knowledge bases to external MCP clients, so an agent that
// feeds documents can check validity without SQL or a SuperAdmin login.
// Every tool here is read-only; the catalogue is public law, the same for
// every tenant, so none of them is scoped by the endpoint's knowledge bases.

const (
	defaultLegalSearchLimit = 10
	maxLegalSearchLimit     = 30
	maxLegalListIDs         = 100
	defaultLegalAuditLimit  = 200
	maxLegalAuditLimit      = 1000
)

// legalExpiredStatusPrefix is the portal status of a document no longer in
// force as a whole (vbpl.vn wording, kept verbatim in legal_documents).
const legalExpiredStatusPrefix = "Hết hiệu lực toàn bộ"

func legalCheckStatusTool() mcp.Tool {
	return mcp.NewTool(types.MCPEndpointToolLegalCheckStatus,
		mcp.WithDescription("Check whether a Vietnamese legal document is still in force. Looks the document up in "+
			"the platform legal catalogue (vbpl.vn snapshot) by số ký hiệu or title words and returns its dates, "+
			"the portal's status with the snapshot date, the documents that replace, repeal, amend or guide it, "+
			"and a derived status when a replacing document already took effect although the portal still "+
			"lists it in force. A document missing from the catalogue is unknown, not repealed."),
		mcp.WithString("document_number", mcp.Description("Số ký hiệu, e.g. \"30/2020/NĐ-CP\"; \"30/2020\" also works")),
		mcp.WithString("title", mcp.Description("Title words when no number is known, e.g. \"Luật Đất đai\"")),
	)
}

func legalFindRelatedTool() mcp.Tool {
	return mcp.NewTool(types.MCPEndpointToolLegalFindRelated,
		mcp.WithDescription("Walk the legal catalogue's relation graph from one document: the documents that "+
			"detail or guide it (guidance), amend it (amendments), its earlier and later versions (history) or "+
			"the documents it is based on (basis). Related documents held in a public knowledge base carry "+
			"their knowledge_id, usable with read_document."),
		mcp.WithString("document_number", mcp.Description("Số ký hiệu, e.g. \"109/2025/QH15\"")),
		mcp.WithString("title", mcp.Description("Title words when no number is known")),
		mcp.WithString("mode", mcp.Description("guidance (default), amendments, history or basis"),
			mcp.Enum("guidance", "amendments", "history", "basis")),
		mcp.WithNumber("depth", mcp.Description("Hops to follow, 1-3 (default: the mode's maximum)")),
		mcp.WithBoolean("include_expired", mcp.Description("Also list documents no longer in force (history always does)")),
	)
}

func legalCatalogSearchTool() mcp.Tool {
	return mcp.NewTool(types.MCPEndpointToolLegalCatalogSearch,
		mcp.WithDescription("Find catalogue rows by số ký hiệu or title words. Returns id (the vbpl.vn id used as "+
			"external_id on ingested documents), number, title, type, issuer, dates and status."),
		mcp.WithString("q", mcp.Required(), mcp.Description("Number or title words")),
		mcp.WithNumber("limit", mcp.Description("Maximum rows, default 10, max 30")),
	)
}

func legalListIngestedTool() mcp.Tool {
	return mcp.NewTool(types.MCPEndpointToolLegalListIngested,
		mcp.WithDescription("List documents of the public legal knowledge bases together with their catalogue "+
			"row, keyed by the vbpl.vn id (metadata.external_id). Pass vbpl_ids to check whether given documents "+
			"are ingested, or a knowledge_base_id to page through one knowledge base."),
		mcp.WithArray("vbpl_ids", mcp.WithStringItems(), mcp.Description("vbpl.vn ids to look up (max 100)")),
		mcp.WithString("knowledge_base_id", mcp.Description("Restrict to one public knowledge base")),
		mcp.WithNumber("page", mcp.Description("1-based page when listing by knowledge base, default 1")),
		mcp.WithNumber("page_size", mcp.Description("Rows per page, default 50, max 200")),
	)
}

func legalExpiryAuditTool() mcp.Tool {
	return mcp.NewTool(types.MCPEndpointToolLegalExpiryAudit,
		mcp.WithDescription("Audit the public legal knowledge bases against the catalogue. Returns two lists: "+
			"remove = documents whose catalogue status says they expired as a whole (must be deleted from the "+
			"knowledge base; the catalogue row stays), review = documents the portal still lists in force but "+
			"which a replacing or ending document already superseded (a person checks transitional clauses "+
			"before removal). Read-only: nothing is deleted."),
		mcp.WithString("knowledge_base_id", mcp.Description("Restrict to one public knowledge base")),
		mcp.WithNumber("limit", mcp.Description("Maximum rows per list, default 200, max 1000")),
	)
}

func legalSyncStatusTool() mcp.Tool {
	return mcp.NewTool(types.MCPEndpointToolLegalSyncStatus,
		mcp.WithDescription("Report the state of the legal catalogue: latest source snapshot date, document "+
			"counts by status, relation count, and how many documents the public legal knowledge bases hold. "+
			"A snapshot older than two days means the nightly vbpl.vn sync is failing."),
	)
}

// --- handlers ---------------------------------------------------------

func (s *Server) handleLegalCheckStatus(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.legalCatalogService == nil {
		return mcp.NewToolResultError("legal catalogue is not available"), nil
	}
	args, _ := json.Marshal(map[string]any{
		"document_number": strings.TrimSpace(req.GetString("document_number", "")),
		"title":           strings.TrimSpace(req.GetString("title", "")),
	})
	res, execErr := tools.NewCheckLegalStatusTool(s.legalCatalogService).Execute(ctx, args)
	return toolResultFromAgentTool(res, execErr), nil
}

func (s *Server) handleLegalFindRelated(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.legalCatalogService == nil {
		return mcp.NewToolResultError("legal catalogue is not available"), nil
	}
	callArgs := map[string]any{
		"document_number": strings.TrimSpace(req.GetString("document_number", "")),
		"title":           strings.TrimSpace(req.GetString("title", "")),
		"mode":            strings.TrimSpace(req.GetString("mode", "")),
		"include_expired": req.GetBool("include_expired", false),
	}
	if depth := req.GetInt("depth", 0); depth > 0 {
		callArgs["depth"] = depth
	}
	args, _ := json.Marshal(callArgs)
	res, execErr := tools.NewFindRelatedLegalDocumentsTool(s.legalCatalogService).Execute(ctx, args)
	return toolResultFromAgentTool(res, execErr), nil
}

func (s *Server) handleLegalCatalogSearch(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.legalReviewService == nil {
		return mcp.NewToolResultError("legal catalogue is not available"), nil
	}
	q := strings.TrimSpace(req.GetString("q", ""))
	if q == "" {
		return mcp.NewToolResultError("q is required"), nil
	}
	limit := clampInt(req.GetInt("limit", defaultLegalSearchLimit), 1, maxLegalSearchLimit, defaultLegalSearchLimit)
	docs, err := s.legalReviewService.SearchCatalog(ctx, q, limit)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("catalogue search failed", err), nil
	}
	rows := make([]map[string]any, 0, len(docs))
	for _, d := range docs {
		rows = append(rows, legalDocRow(d))
	}
	text := fmt.Sprintf("%d catalogue document(s) match %q.", len(rows), q)
	return mcp.NewToolResultStructured(map[string]any{"query": q, "documents": rows}, text), nil
}

// legalIngestedRow is one public-KB document joined with its catalogue row.
type legalIngestedRow struct {
	KnowledgeID       string `json:"knowledge_id"`
	KnowledgeBaseID   string `json:"knowledge_base_id"`
	KnowledgeBaseName string `json:"knowledge_base_name"`
	Title             string `json:"title"`
	ParseStatus       string `json:"parse_status"`
	ExternalID        string `json:"external_id"`
	DocumentNumber    string `json:"document_number"`
	DocType           string `json:"doc_type"`
	Status            string `json:"status"`
	// PortalStatus is the portal's newer status parked in source_pending
	// when the catalogue row is SuperAdmin-reviewed (imports never overwrite
	// a reviewed row); empty when the row is current.
	PortalStatus string     `json:"portal_status,omitempty"`
	EffectiveOn  *time.Time `json:"effective_on"`
	ExpiredOn    *time.Time `json:"expired_on"`
	// The newest in-force document that replaced or ended this one, when
	// the catalogue has such a relation (derived expiry, see
	// check_legal_status).
	SupersededBy   string     `json:"superseded_by,omitempty"`
	SupersededByID string     `json:"superseded_by_id,omitempty"`
	SupersededOn   *time.Time `json:"superseded_on,omitempty"`
}

// legalIngestedSelect joins live public-KB knowledge with the catalogue and
// the earliest in-force document that replaces or ends it. Public KBs are
// platform-owned (owner_tenant_id = 0); the endpoint's tenant is irrelevant.
const legalIngestedSelect = `
SELECT k.id AS knowledge_id, k.knowledge_base_id, kb.name AS knowledge_base_name, k.title, k.parse_status,
       COALESCE(k.metadata->>'external_id', '') AS external_id,
       COALESCE(d.document_number, '') AS document_number, COALESCE(d.doc_type, '') AS doc_type,
       COALESCE(d.status, '') AS status, COALESCE(d.source_pending->>'status', '') AS portal_status,
       d.effective_on, d.expired_on,
       COALESCE(sup.document_number, '') AS superseded_by, COALESCE(sup.id, '') AS superseded_by_id,
       sup.effective_on AS superseded_on
FROM knowledges k
JOIN knowledge_bases kb ON kb.id = k.knowledge_base_id AND kb.owner_tenant_id = 0 AND kb.deleted_at IS NULL
LEFT JOIN legal_documents d ON d.id = k.metadata->>'external_id'
LEFT JOIN LATERAL (
    SELECT o.id, o.document_number, o.effective_on
    FROM legal_document_relations r
    JOIN legal_documents o ON o.id = r.from_id
    WHERE r.to_id = d.id AND r.relation IN ('replaces', 'expires')
      AND o.effective_on IS NOT NULL AND o.effective_on <= CURRENT_DATE
    ORDER BY o.effective_on ASC
    LIMIT 1
) sup ON TRUE
WHERE k.deleted_at IS NULL`

func (s *Server) handleLegalListIngested(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.db == nil {
		return mcp.NewToolResultError("database is not available"), nil
	}
	ids := req.GetStringSlice("vbpl_ids", nil)
	kbID := strings.TrimSpace(req.GetString("knowledge_base_id", ""))
	if len(ids) == 0 && kbID == "" {
		return mcp.NewToolResultError("vbpl_ids or knowledge_base_id is required"), nil
	}
	if len(ids) > maxLegalListIDs {
		return mcp.NewToolResultErrorf("at most %d vbpl_ids per call", maxLegalListIDs), nil
	}
	query := legalIngestedSelect
	var params []any
	if kbID != "" {
		query += " AND k.knowledge_base_id = ?"
		params = append(params, kbID)
	}
	if len(ids) > 0 {
		clean := make([]string, 0, len(ids))
		for _, id := range ids {
			if id = strings.TrimSpace(id); id != "" {
				clean = append(clean, id)
			}
		}
		query += " AND k.metadata->>'external_id' IN ?"
		params = append(params, clean)
	}
	pageSize := clampInt(req.GetInt("page_size", 50), 1, 200, 50)
	page := req.GetInt("page", 1)
	if page < 1 {
		page = 1
	}
	query += " ORDER BY k.knowledge_base_id, k.title LIMIT ? OFFSET ?"
	params = append(params, pageSize+1, (page-1)*pageSize)

	rows := []legalIngestedRow{}
	if err := s.db.WithContext(ctx).Raw(query, params...).Scan(&rows).Error; err != nil {
		return mcp.NewToolResultErrorFromErr("listing failed", err), nil
	}
	hasMore := len(rows) > pageSize
	if hasMore {
		rows = rows[:pageSize]
	}
	found := map[string]bool{}
	for _, r := range rows {
		found[r.ExternalID] = true
	}
	missing := []string{}
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" && !found[id] {
			missing = append(missing, id)
		}
	}
	text := fmt.Sprintf("%d ingested document(s)", len(rows))
	if len(missing) > 0 {
		text += fmt.Sprintf("; %d of the requested ids are not in any public knowledge base", len(missing))
	}
	return mcp.NewToolResultStructured(map[string]any{
		"documents": rows, "missing_vbpl_ids": missing, "page": page, "page_size": pageSize, "has_more": hasMore,
	}, text+"."), nil
}

func (s *Server) handleLegalExpiryAudit(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.db == nil {
		return mcp.NewToolResultError("database is not available"), nil
	}
	kbID := strings.TrimSpace(req.GetString("knowledge_base_id", ""))
	limit := clampInt(req.GetInt("limit", defaultLegalAuditLimit), 1, maxLegalAuditLimit, defaultLegalAuditLimit)

	run := func(where string, params ...any) ([]legalIngestedRow, error) {
		query := legalIngestedSelect + " AND " + where
		if kbID != "" {
			query += " AND k.knowledge_base_id = ?"
			params = append(params, kbID)
		}
		query += " ORDER BY k.knowledge_base_id, k.title LIMIT ?"
		params = append(params, limit)
		rows := []legalIngestedRow{} // never nil: clients expect a JSON array
		err := s.db.WithContext(ctx).Raw(query, params...).Scan(&rows).Error
		return rows, err
	}
	remove, err := run("(d.status LIKE ? OR d.source_pending->>'status' LIKE ?)", legalExpiredStatusPrefix+"%", legalExpiredStatusPrefix+"%")
	if err != nil {
		return mcp.NewToolResultErrorFromErr("audit failed", err), nil
	}
	// Still in force (or partly, or unknown) by the portal, no expiry date,
	// but superseded by a document already in force — same rule as
	// check_legal_status's derived status.
	review, err := run("d.expired_on IS NULL AND sup.id IS NOT NULL AND d.status IN ('', 'Chưa xác định', 'Còn hiệu lực', 'Hết hiệu lực một phần') AND COALESCE(d.source_pending->>'status', '') NOT LIKE ?", legalExpiredStatusPrefix+"%")
	if err != nil {
		return mcp.NewToolResultErrorFromErr("audit failed", err), nil
	}
	text := fmt.Sprintf("remove: %d document(s) expired as a whole (by the catalogue row or by a newer portal status "+
		"parked on a reviewed row); review: %d document(s) superseded by an in-force document while the portal "+
		"still lists them in force. Nothing was changed.", len(remove), len(review))
	return mcp.NewToolResultStructured(map[string]any{
		"remove": remove, "review": review, "limit": limit, "knowledge_base_id": kbID,
	}, text), nil
}

func (s *Server) handleLegalSyncStatus(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.db == nil {
		return mcp.NewToolResultError("database is not available"), nil
	}
	db := s.db.WithContext(ctx)
	var byStatus []struct {
		Status string `json:"status"`
		Count  int64  `json:"count"`
	}
	if err := db.Raw("SELECT status, COUNT(*) AS count FROM legal_documents GROUP BY status ORDER BY count DESC").
		Scan(&byStatus).Error; err != nil {
		return mcp.NewToolResultErrorFromErr("status query failed", err), nil
	}
	var totals struct {
		Documents   int64      `json:"documents"`
		Reviewed    int64      `json:"reviewed"`
		ReviewedPen int64      `json:"reviewed_pending_diffs"`
		Manual      int64      `json:"manual"`
		LatestSnap  *time.Time `json:"latest_snapshot"`
		MetadataOnl int64      `json:"-"`
	}
	if err := db.Raw(`SELECT COUNT(*) AS documents,
		COUNT(*) FILTER (WHERE reviewed_by <> '') AS reviewed,
		COUNT(*) FILTER (WHERE reviewed_by <> '' AND source_pending IS NOT NULL) AS reviewed_pen,
		COUNT(*) FILTER (WHERE source = 'manual') AS manual,
		MAX(source_updated_at) AS latest_snap FROM legal_documents`).Scan(&totals).Error; err != nil {
		return mcp.NewToolResultErrorFromErr("totals query failed", err), nil
	}
	var relations int64
	if err := db.Raw("SELECT COUNT(*) FROM legal_document_relations").Scan(&relations).Error; err != nil {
		return mcp.NewToolResultErrorFromErr("relations query failed", err), nil
	}
	var kbs []struct {
		KnowledgeBaseID string `json:"knowledge_base_id"`
		Name            string `json:"name"`
		Documents       int64  `json:"documents"`
		Unlinked        int64  `json:"unlinked"`
	}
	if err := db.Raw(`SELECT kb.id AS knowledge_base_id, kb.name,
		COUNT(k.id) AS documents,
		COUNT(k.id) FILTER (WHERE l.knowledge_id IS NULL) AS unlinked
		FROM knowledge_bases kb
		LEFT JOIN knowledges k ON k.knowledge_base_id = kb.id AND k.deleted_at IS NULL
		LEFT JOIN legal_document_links l ON l.knowledge_id = k.id
		WHERE kb.owner_tenant_id = 0 AND kb.deleted_at IS NULL
		GROUP BY kb.id, kb.name ORDER BY kb.name`).Scan(&kbs).Error; err != nil {
		return mcp.NewToolResultErrorFromErr("knowledge base query failed", err), nil
	}
	snapshot := "unknown"
	stale := true
	if totals.LatestSnap != nil {
		snapshot = totals.LatestSnap.Format("2006-01-02")
		stale = time.Since(*totals.LatestSnap) > 48*time.Hour
	}
	text := fmt.Sprintf("Catalogue snapshot %s (%d documents, %d relations)", snapshot, totals.Documents, relations)
	if stale {
		text += " — STALE: the nightly vbpl.vn sync has not succeeded for more than two days"
	}
	if totals.ReviewedPen > 0 {
		text += fmt.Sprintf("; %d reviewed catalogue row(s) hold newer portal values in source_pending that a "+
			"SuperAdmin has not applied (their status may be outdated)", totals.ReviewedPen)
	}
	text += "."
	return mcp.NewToolResultStructured(map[string]any{
		"latest_snapshot":        snapshot,
		"stale":                  stale,
		"documents":              totals.Documents,
		"reviewed":               totals.Reviewed,
		"reviewed_pending_diffs": totals.ReviewedPen,
		"manual":                 totals.Manual,
		"relations":              relations,
		"by_status":              byStatus,
		"public_knowledge_bases": kbs,
	}, text), nil
}

// legalDocRow is the compact JSON form of a catalogue row for clients.
func legalDocRow(d *types.LegalDocument) map[string]any {
	if d == nil {
		return nil
	}
	return map[string]any{
		"id":              d.ID,
		"source":          d.Source,
		"document_number": d.DocumentNumber,
		"title":           d.Title,
		"doc_type":        d.DocType,
		"issuer":          d.Issuer,
		"field":           d.Field,
		"issued_on":       legalDate(d.IssuedOn),
		"effective_on":    legalDate(d.EffectiveOn),
		"expired_on":      legalDate(d.ExpiredOn),
		"status":          d.Status,
		"snapshot":        legalDate(d.SourceUpdatedAt),
		"url":             "https://vbpl.vn/van-ban/chi-tiet/" + d.ID,
	}
}

func legalDate(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("2006-01-02")
}

func clampInt(v, lo, hi, def int) int {
	if v == 0 {
		return def
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// --- legal_list_missing --------------------------------------------------

const (
	defaultLegalMissingLimit = 100
	maxLegalMissingLimit     = 500
	defaultLegalMinYear      = 2010
	// legalKBNamePrefix: the public legal KBs are named "VBPL – <type>, <type>…"
	// (docs/legal-document-feed-spec.md §1); the types after the prefix say
	// which document types each one holds.
	legalKBNamePrefix = "VBPL – "
)

// legalLocalIssuer mirrors LOCAL_ISSUER of crawl_vbpl.py: issuers of
// province-level documents, which the feed does not take yet.
var legalLocalIssuer = regexp.MustCompile(`UBND|HĐND|Ủy ban nhân dân|Uỷ ban nhân dân|Hội đồng nhân dân|tỉnh|Tỉnh|thành phố|Thành phố|TP\.`)

func legalListMissingTool() mcp.Tool {
	return mcp.NewTool(types.MCPEndpointToolLegalListMissing,
		mcp.WithDescription("List catalogue documents that belong in the public legal knowledge bases but are not "+
			"there yet: a document type one of the bases holds (read from the base names \"VBPL – <types>\"), issued "+
			"in or after min_year, central issuer, not fully expired, and with full catalogue metadata (the portal "+
			"list alone is skipped unless include_metadata_only). Each row names the knowledge base to feed it into; "+
			"legal_ingest_document then reports when the portal has no full text for it."),
		mcp.WithString("doc_type", mcp.Description("Only this document type, e.g. \"Thông tư\"")),
		mcp.WithNumber("min_year", mcp.Description("Earliest issue year, default 2010")),
		mcp.WithString("issued_after", mcp.Description("Only documents issued after this date (YYYY-MM-DD), e.g. the last feed")),
		mcp.WithBoolean("include_metadata_only", mcp.Description("Also list documents the catalogue knows from the portal list only")),
		mcp.WithNumber("limit", mcp.Description("Maximum rows, default 100, max 500; newest first")),
	)
}

// legalMissingRow is one catalogue document without a public-KB copy.
type legalMissingRow struct {
	VBPLID         string     `json:"vbpl_id"`
	DocumentNumber string     `json:"document_number"`
	Title          string     `json:"title"`
	DocType        string     `json:"doc_type"`
	Issuer         string     `json:"issuer"`
	IssuedOn       *time.Time `json:"issued_on"`
	EffectiveOn    *time.Time `json:"effective_on"`
	Status         string     `json:"status"`
	MetadataOnly   bool       `json:"metadata_only"`
	// Target knowledge base for the document's type.
	KnowledgeBaseID   string `json:"knowledge_base_id"`
	KnowledgeBaseName string `json:"knowledge_base_name"`
	URL               string `json:"url"`
}

// legalTypeTargets maps each document type to the public KB named for it.
// "VBPL – Hiến pháp, Bộ luật, Luật, Pháp lệnh" → four types. A type listed
// by two bases keeps the first (name order).
func legalTypeTargets(kbs []struct{ ID, Name string }) map[string]struct{ ID, Name string } {
	out := map[string]struct{ ID, Name string }{}
	for _, kb := range kbs {
		if !strings.HasPrefix(kb.Name, legalKBNamePrefix) {
			continue
		}
		for _, t := range strings.Split(strings.TrimPrefix(kb.Name, legalKBNamePrefix), ",") {
			t = strings.TrimSpace(t)
			if _, dup := out[t]; t != "" && !dup {
				out[t] = kb
			}
		}
	}
	return out
}

func (s *Server) handleLegalListMissing(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.db == nil {
		return mcp.NewToolResultError("database is not available"), nil
	}
	db := s.db.WithContext(ctx)
	var kbs []struct{ ID, Name string }
	if err := db.Raw(`SELECT id, name FROM knowledge_bases WHERE owner_tenant_id = 0 AND deleted_at IS NULL
		AND name LIKE ? ORDER BY name`, legalKBNamePrefix+"%").Scan(&kbs).Error; err != nil {
		return mcp.NewToolResultErrorFromErr("knowledge base query failed", err), nil
	}
	targets := legalTypeTargets(kbs)
	if len(targets) == 0 {
		return mcp.NewToolResultError("no public knowledge base named \"" + legalKBNamePrefix + "<type>\" exists"), nil
	}
	wantType := strings.TrimSpace(req.GetString("doc_type", ""))
	var typeList []string
	for t := range targets {
		if wantType == "" || strings.EqualFold(t, wantType) {
			typeList = append(typeList, t)
		}
	}
	if len(typeList) == 0 {
		return mcp.NewToolResultErrorf("no public knowledge base holds document type %q", wantType), nil
	}
	minYear := req.GetInt("min_year", defaultLegalMinYear)
	if minYear < 1900 {
		minYear = defaultLegalMinYear
	}
	limit := clampInt(req.GetInt("limit", defaultLegalMissingLimit), 1, maxLegalMissingLimit, defaultLegalMissingLimit)

	query := `SELECT d.id AS vbpl_id, d.document_number, d.title, d.doc_type, d.issuer, d.issued_on, d.effective_on,
		d.status, (d.signer = '' AND d.field = '') AS metadata_only
		FROM legal_documents d
		WHERE d.source = 'vbpl' AND d.doc_type IN ? AND d.issued_on >= ?
		  AND d.status NOT LIKE ? AND COALESCE(d.source_pending->>'status', '') NOT LIKE ?
		  AND NOT EXISTS (SELECT 1 FROM knowledges k
		      JOIN knowledge_bases kb ON kb.id = k.knowledge_base_id AND kb.owner_tenant_id = 0 AND kb.deleted_at IS NULL
		      WHERE k.deleted_at IS NULL AND k.metadata->>'external_id' = d.id)`
	params := []any{typeList, fmt.Sprintf("%04d-01-01", minYear), legalExpiredStatusPrefix + "%", legalExpiredStatusPrefix + "%"}
	if !req.GetBool("include_metadata_only", false) {
		query += " AND NOT (d.signer = '' AND d.field = '')"
	}
	if after := strings.TrimSpace(req.GetString("issued_after", "")); after != "" {
		if _, err := time.Parse("2006-01-02", after); err != nil {
			return mcp.NewToolResultError("issued_after must be YYYY-MM-DD"), nil
		}
		query += " AND d.issued_on > ?"
		params = append(params, after)
	}
	query += " ORDER BY d.issued_on DESC, d.id LIMIT ?"
	params = append(params, limit+1)

	var rows []legalMissingRow
	if err := db.Raw(query, params...).Scan(&rows).Error; err != nil {
		return mcp.NewToolResultErrorFromErr("listing failed", err), nil
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	out := make([]legalMissingRow, 0, len(rows))
	skippedLocal := 0
	for _, r := range rows {
		if legalLocalIssuer.MatchString(r.Issuer) {
			skippedLocal++
			continue
		}
		if kb, ok := targets[r.DocType]; ok {
			r.KnowledgeBaseID, r.KnowledgeBaseName = kb.ID, kb.Name
		}
		r.URL = fmt.Sprintf(vbplDocURL, r.VBPLID)
		out = append(out, r)
	}
	text := fmt.Sprintf("%d in-scope document(s) are not in a public knowledge base", len(out))
	if skippedLocal > 0 {
		text += fmt.Sprintf(" (%d province-level documents skipped)", skippedLocal)
	}
	if hasMore {
		text += "; more exist, narrow with doc_type or issued_after"
	}
	return mcp.NewToolResultStructured(map[string]any{
		"documents": out, "has_more": hasMore, "skipped_local": skippedLocal, "types": typeList, "min_year": minYear,
	}, text+"."), nil
}
