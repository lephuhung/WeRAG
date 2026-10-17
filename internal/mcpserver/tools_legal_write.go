package mcpserver

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vbpl"
	"github.com/mark3labs/mcp-go/mcp"
)

// The legal_write group lets an external feeding agent keep the public legal
// knowledge bases in step with vbpl.vn without SQL or a SuperAdmin login:
// ingest or replace one document (fetched from the portal on demand, so the
// bytes and the md5 match scripts/legal_catalog/ingest_vbpl.py), remove one,
// apply the expiry audit, run the legal review scan and rebuild a catalogue
// wiki. Every write goes through the endpoint's knowledge-base scope and
// editor permission like add/delete_document, and is appended to
// legal_feed_log with the endpoint that made it.

const (
	legalFeedChannel      = "mcp"
	defaultLegalApplyMax  = 200
	maxLegalApplyMax      = 1000
	legalReviewScanRounds = 10
)

func legalIngestTool() mcp.Tool {
	return mcp.NewTool(types.MCPEndpointToolLegalIngest,
		mcp.WithDescription("Fetch one document from vbpl.vn by id and add it to a public legal knowledge base "+
			"with the standard metadata (external_id, source=vbpl, số hiệu, dates, status, appendix files). "+
			"Idempotent: an unchanged copy is reported as unchanged, a changed page replaces the old copy. "+
			"Refuses documents the portal marks fully expired (\"Hết hiệu lực toàn bộ\") and empty pages. "+
			"Pick the knowledge base by document type (Luật → the Luật base, Nghị định → Nghị định…)."),
		mcp.WithString("vbpl_id", mcp.Required(), mcp.Description("vbpl.vn document id (legal_catalog_search returns it)")),
		mcp.WithString("knowledge_base_id", mcp.Required(), mcp.Description("Target knowledge base id or exact name")),
		mcp.WithString("reason", mcp.Description("Why this document is fed (logged)")),
	)
}

func legalRemoveTool() mcp.Tool {
	return mcp.NewTool(types.MCPEndpointToolLegalRemove,
		mcp.WithDescription("Delete one document from a public legal knowledge base, by vbpl.vn id or by knowledge_id. "+
			"The catalogue row stays, so validity questions still get an answer. Logged with the reason."),
		mcp.WithString("vbpl_id", mcp.Description("vbpl.vn id (metadata.external_id) of the document")),
		mcp.WithString("knowledge_id", mcp.Description("Document id, when known")),
		mcp.WithString("reason", mcp.Required(), mcp.Description("Why it is removed, e.g. \"Hết hiệu lực toàn bộ theo danh mục 2026-10-17\"")),
	)
}

func legalApplyExpiryTool() mcp.Tool {
	return mcp.NewTool(types.MCPEndpointToolLegalApplyExpiry,
		mcp.WithDescription("Delete from the public legal knowledge bases every document whose catalogue status "+
			"says it expired as a whole — the \"remove\" list of legal_expiry_audit. The \"review\" list is never "+
			"touched: those need a person. Run legal_expiry_audit first and pass its count as expected to guard "+
			"against a catalogue that changed in between."),
		mcp.WithString("knowledge_base_id", mcp.Description("Restrict to one public knowledge base")),
		mcp.WithNumber("expected", mcp.Description("Number of documents legal_expiry_audit listed under remove; the call "+
			"refuses when the live count is larger")),
		mcp.WithNumber("limit", mcp.Description("Maximum documents to delete in this call, default 200, max 1000")),
		mcp.WithString("reason", mcp.Description("Logged with every deletion")),
	)
}

func legalReviewScanTool() mcp.Tool {
	return mcp.NewTool(types.MCPEndpointToolLegalReviewScan,
		mcp.WithDescription("Create the legal-identity links for public-KB documents that have none yet (after an "+
			"ingest). Documents from vbpl.vn are approved automatically; others wait for a SuperAdmin. Repeats "+
			"until nothing is left."),
	)
}

func legalWikiRebuildTool() mcp.Tool {
	return mcp.NewTool(types.MCPEndpointToolLegalWikiRebuild,
		mcp.WithDescription("Rebuild the code-generated (catalogue mode) wiki of one knowledge base after its "+
			"documents changed. Runs in the background; the result reports the start state."),
		mcp.WithString("knowledge_base_id", mcp.Required(), mcp.Description("Knowledge base id or exact name")),
	)
}

// --- handlers ---------------------------------------------------------

func (s *Server) handleLegalIngest(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ep, err := endpointFromContext(ctx)
	if err != nil {
		return mcp.NewToolResultError("unauthorized"), nil
	}
	if s.knowledgeService == nil || s.db == nil {
		return mcp.NewToolResultError("knowledge service is not available"), nil
	}
	vbplID := strings.TrimSpace(req.GetString("vbpl_id", ""))
	selector := strings.TrimSpace(req.GetString("knowledge_base_id", ""))
	if vbplID == "" || selector == "" {
		return mcp.NewToolResultError("vbpl_id and knowledge_base_id are required"), nil
	}
	kbs, err := s.selectKnowledgeBases(ctx, ep, []string{selector})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	kb := kbs[0]
	if kb.OwnerTenantID != 0 {
		return mcp.NewToolResultErrorf("knowledge base %q is not a public legal knowledge base", kb.ID), nil
	}
	scoped, err := s.scopedKBContext(ctx, kb, types.KBPermissionEditor)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := access.RequireKBWrite(scoped, kb); err != nil {
		return mcp.NewToolResultError("this endpoint is not allowed to write to knowledge base " + kb.ID), nil
	}

	detail, err := vbplFetchDetail(ctx, s.vbplClient(), vbplID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	status := strings.TrimSpace(detail.EffStatus.Name)
	if strings.HasPrefix(status, legalExpiredStatusPrefix) {
		return mcp.NewToolResultErrorf("document %s (%s) is \"%s\": fully expired documents keep their catalogue "+
			"row only and are never fed into a knowledge base", vbplID, detail.DocNum, status), nil
	}
	content := []byte(detail.Content)
	if len(content) < vbplMinHTMLBytes {
		return mcp.NewToolResultErrorf("document %s has no published full text on vbpl.vn (%d bytes)", vbplID, len(content)), nil
	}
	sum := md5.Sum(content)
	hash := hex.EncodeToString(sum[:])

	// Where is this document already? Same KB + same bytes → nothing to do;
	// same KB + other bytes → replace; another public KB → refuse (one copy).
	var existing []struct {
		ID              string
		KnowledgeBaseID string
		FileHash        string
	}
	if err := s.db.WithContext(ctx).Raw(`SELECT k.id, k.knowledge_base_id, k.file_hash FROM knowledges k
		JOIN knowledge_bases kb ON kb.id = k.knowledge_base_id AND kb.owner_tenant_id = 0 AND kb.deleted_at IS NULL
		WHERE k.deleted_at IS NULL AND k.metadata->>'external_id' = ?`, vbplID).Scan(&existing).Error; err != nil {
		return mcp.NewToolResultErrorFromErr("lookup failed", err), nil
	}
	var replaced string
	for _, e := range existing {
		if e.KnowledgeBaseID != kb.ID {
			return mcp.NewToolResultErrorf("document %s is already held by knowledge base %s as %s; a document lives "+
				"in one knowledge base only", vbplID, e.KnowledgeBaseID, e.ID), nil
		}
		if e.FileHash == hash {
			return mcp.NewToolResultStructured(map[string]any{
				"result": "unchanged", "vbpl_id": vbplID, "knowledge_id": e.ID, "knowledge_base_id": kb.ID,
			}, fmt.Sprintf("Document %s (%s) is already in %s and unchanged.", vbplID, detail.DocNum, kb.Name)), nil
		}
		if err := s.knowledgeService.DeleteKnowledge(scoped, e.ID); err != nil {
			return mcp.NewToolResultErrorFromErr("failed to delete the outdated copy "+e.ID, err), nil
		}
		replaced = e.ID
	}

	name := vbplFileName(detail)
	fh, err := memoryFileHeader("file", name, content)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("failed to stage the file", err), nil
	}
	created, err := s.knowledgeService.CreateKnowledgeFromFile(scoped, kb.ID, fh, vbplMetadata(detail), nil, name, nil, legalFeedChannel, nil)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("failed to add document", err), nil
	}
	result := "created"
	if replaced != "" {
		result = "replaced"
	}
	s.legalFeedLog(ctx, ep, legalFeedEntry{
		Action: "ingest_" + result, VBPLID: vbplID, KnowledgeID: created.ID, KnowledgeBaseID: kb.ID,
		DocumentNumber: detail.DocNum, Reason: req.GetString("reason", ""),
		Detail: map[string]any{"replaced_knowledge_id": replaced, "status": status, "md5": hash,
			"vbpl_markup": vbpl.LooksLikeVBPL(content)},
	})
	return mcp.NewToolResultStructured(map[string]any{
		"result": result, "vbpl_id": vbplID, "knowledge_id": created.ID, "knowledge_base_id": kb.ID,
		"replaced_knowledge_id": replaced, "document": summarizeKnowledge(created), "document_number": detail.DocNum,
		"doc_type": detail.DocType.Name, "status": status, "vbpl_markup": vbpl.LooksLikeVBPL(content),
	}, fmt.Sprintf("Document %s (%s, %s) %s in %s as %s; indexing runs asynchronously (parse_status).",
		vbplID, detail.DocNum, detail.DocType.Name, result, kb.Name, created.ID)), nil
}

func (s *Server) handleLegalRemove(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ep, err := endpointFromContext(ctx)
	if err != nil {
		return mcp.NewToolResultError("unauthorized"), nil
	}
	if s.knowledgeService == nil || s.db == nil {
		return mcp.NewToolResultError("knowledge service is not available"), nil
	}
	reason := strings.TrimSpace(req.GetString("reason", ""))
	if reason == "" {
		return mcp.NewToolResultError("reason is required"), nil
	}
	knowledgeID := strings.TrimSpace(req.GetString("knowledge_id", ""))
	vbplID := strings.TrimSpace(req.GetString("vbpl_id", ""))
	if knowledgeID == "" && vbplID == "" {
		return mcp.NewToolResultError("vbpl_id or knowledge_id is required"), nil
	}
	if knowledgeID == "" {
		var ids []string
		if err := s.db.WithContext(ctx).Raw(`SELECT k.id FROM knowledges k
			JOIN knowledge_bases kb ON kb.id = k.knowledge_base_id AND kb.owner_tenant_id = 0 AND kb.deleted_at IS NULL
			WHERE k.deleted_at IS NULL AND k.metadata->>'external_id' = ?`, vbplID).Scan(&ids).Error; err != nil {
			return mcp.NewToolResultErrorFromErr("lookup failed", err), nil
		}
		if len(ids) == 0 {
			return mcp.NewToolResultErrorf("no public knowledge base holds document %s", vbplID), nil
		}
		if len(ids) > 1 {
			return mcp.NewToolResultErrorf("document %s has %d copies (%s); pass knowledge_id", vbplID, len(ids), strings.Join(ids, ", ")), nil
		}
		knowledgeID = ids[0]
	}
	removed, err := s.legalRemoveKnowledge(ctx, ep, knowledgeID, "remove", reason)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultStructured(removed, fmt.Sprintf("Deleted %s (%s) from %s.", removed["knowledge_id"],
		removed["title"], removed["knowledge_base_id"])), nil
}

// legalRemoveKnowledge deletes one public-KB document inside the endpoint's
// scope and logs it. The returned map is the structured result row.
func (s *Server) legalRemoveKnowledge(ctx context.Context, ep *types.MCPEndpoint, knowledgeID, action, reason string) (map[string]any, error) {
	k, kb, err := s.knowledgeInScope(ctx, ep, knowledgeID)
	if err != nil {
		return nil, err
	}
	if kb.OwnerTenantID != 0 {
		return nil, fmt.Errorf("document %s is not in a public legal knowledge base", knowledgeID)
	}
	scoped, err := s.scopedKBContext(ctx, kb, types.KBPermissionEditor)
	if err != nil {
		return nil, err
	}
	if err := s.knowledgeService.DeleteKnowledge(scoped, k.ID); err != nil {
		return nil, fmt.Errorf("failed to delete %s: %w", k.ID, err)
	}
	externalID := knowledgeExternalID(k)
	s.legalFeedLog(ctx, ep, legalFeedEntry{
		Action: action, VBPLID: externalID, KnowledgeID: k.ID, KnowledgeBaseID: kb.ID,
		DocumentNumber: knowledgeMetaString(k, "doc_number"), Reason: reason,
	})
	return map[string]any{
		"deleted": true, "knowledge_id": k.ID, "knowledge_base_id": kb.ID, "vbpl_id": externalID, "title": k.Title,
	}, nil
}

func (s *Server) handleLegalApplyExpiry(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ep, err := endpointFromContext(ctx)
	if err != nil {
		return mcp.NewToolResultError("unauthorized"), nil
	}
	if s.knowledgeService == nil || s.db == nil {
		return mcp.NewToolResultError("knowledge service is not available"), nil
	}
	kbID := strings.TrimSpace(req.GetString("knowledge_base_id", ""))
	limit := clampInt(req.GetInt("limit", defaultLegalApplyMax), 1, maxLegalApplyMax, defaultLegalApplyMax)
	reason := strings.TrimSpace(req.GetString("reason", ""))
	if reason == "" {
		reason = "Hết hiệu lực toàn bộ theo danh mục pháp lý"
	}
	query := legalIngestedSelect + " AND (d.status LIKE ? OR d.source_pending->>'status' LIKE ?)"
	params := []any{legalExpiredStatusPrefix + "%", legalExpiredStatusPrefix + "%"}
	if kbID != "" {
		query += " AND k.knowledge_base_id = ?"
		params = append(params, kbID)
	}
	query += " ORDER BY k.knowledge_base_id, k.title"
	var rows []legalIngestedRow
	if err := s.db.WithContext(ctx).Raw(query, params...).Scan(&rows).Error; err != nil {
		return mcp.NewToolResultErrorFromErr("audit failed", err), nil
	}
	if expected := req.GetInt("expected", -1); expected >= 0 && len(rows) > expected {
		return mcp.NewToolResultErrorf("the catalogue now lists %d documents to remove, more than the expected %d; "+
			"run legal_expiry_audit again and review the list", len(rows), expected), nil
	}
	deleted := []map[string]any{}
	failed := []map[string]any{}
	for i, r := range rows {
		if i >= limit {
			break
		}
		row, err := s.legalRemoveKnowledge(ctx, ep, r.KnowledgeID, "apply_expiry", reason+" ("+r.Status+")")
		if err != nil {
			failed = append(failed, map[string]any{"knowledge_id": r.KnowledgeID, "vbpl_id": r.ExternalID, "error": err.Error()})
			continue
		}
		row["document_number"] = r.DocumentNumber
		deleted = append(deleted, row)
	}
	remaining := len(rows) - len(deleted) - len(failed)
	if remaining < 0 {
		remaining = 0
	}
	return mcp.NewToolResultStructured(map[string]any{
		"deleted": deleted, "failed": failed, "remaining": remaining, "listed": len(rows),
	}, fmt.Sprintf("Deleted %d fully expired document(s); %d failed; %d left for another call.", len(deleted), len(failed), remaining)), nil
}

func (s *Server) handleLegalReviewScan(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ep, err := endpointFromContext(ctx)
	if err != nil {
		return mcp.NewToolResultError("unauthorized"), nil
	}
	if s.legalReviewService == nil {
		return mcp.NewToolResultError("legal review is not available"), nil
	}
	total, rounds := 0, 0
	for rounds < legalReviewScanRounds {
		n, err := s.legalReviewService.Scan(ctx)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("scan failed", err), nil
		}
		rounds++
		total += n
		if n == 0 {
			break
		}
	}
	var pending int64
	if s.db != nil {
		_ = s.db.WithContext(ctx).Raw("SELECT COUNT(*) FROM legal_document_links WHERE review_status = 'pending'").Scan(&pending).Error
	}
	s.legalFeedLog(ctx, ep, legalFeedEntry{Action: "review_scan", Detail: map[string]any{"created": total, "rounds": rounds, "pending": pending}})
	return mcp.NewToolResultStructured(map[string]any{"created": total, "rounds": rounds, "pending": pending},
		fmt.Sprintf("Scan created %d link(s) in %d round(s); %d link(s) wait for a SuperAdmin.", total, rounds, pending)), nil
}

func (s *Server) handleLegalWikiRebuild(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ep, err := endpointFromContext(ctx)
	if err != nil {
		return mcp.NewToolResultError("unauthorized"), nil
	}
	if s.wikiCatalogue == nil {
		return mcp.NewToolResultError("catalogue wiki is not available"), nil
	}
	selector := strings.TrimSpace(req.GetString("knowledge_base_id", ""))
	if selector == "" {
		return mcp.NewToolResultError("knowledge_base_id is required"), nil
	}
	kbs, err := s.selectKnowledgeBases(ctx, ep, []string{selector})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	kb := kbs[0]
	scoped, err := s.scopedKBContext(ctx, kb, types.KBPermissionEditor)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	st, err := s.wikiCatalogue.StartRebuild(scoped, kb.ID)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("rebuild failed to start", err), nil
	}
	s.legalFeedLog(ctx, ep, legalFeedEntry{Action: "wiki_rebuild", KnowledgeBaseID: kb.ID})
	return mcp.NewToolResultStructured(st, fmt.Sprintf("Catalogue wiki rebuild of %s started (running=%t, phase %q).",
		kb.Name, st.Running, st.Phase)), nil
}

// --- helpers ----------------------------------------------------------

// vbplClient is the HTTP client for vbpl.vn; tests swap it.
func (s *Server) vbplClient() *http.Client {
	if s.vbplHTTP != nil {
		return s.vbplHTTP
	}
	return &http.Client{Timeout: vbplTimeout}
}

// memoryFileHeader builds a *multipart.FileHeader from bytes, the type
// KnowledgeService.CreateKnowledgeFromFile expects from an HTTP upload.
func memoryFileHeader(field, name string, content []byte) (*multipart.FileHeader, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	hdr := textproto.MIMEHeader{}
	hdr.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, field, strings.ReplaceAll(name, `"`, "'")))
	hdr.Set("Content-Type", "text/html")
	part, err := w.CreatePart(hdr)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(content); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	form, err := multipart.NewReader(&buf, w.Boundary()).ReadForm(int64(len(content)) + 1<<20)
	if err != nil {
		return nil, err
	}
	files := form.File[field]
	if len(files) == 0 {
		return nil, fmt.Errorf("multipart form lost the file part")
	}
	return files[0], nil
}

type legalFeedEntry struct {
	Action          string
	VBPLID          string
	KnowledgeID     string
	KnowledgeBaseID string
	DocumentNumber  string
	Reason          string
	Detail          map[string]any
}

// legalFeedLog appends one row to legal_feed_log; a failure is logged, never
// returned, so the write that succeeded is reported as such.
func (s *Server) legalFeedLog(ctx context.Context, ep *types.MCPEndpoint, e legalFeedEntry) {
	if s.db == nil {
		return
	}
	var detail []byte
	if e.Detail != nil {
		detail, _ = json.Marshal(e.Detail)
	}
	var tenantID uint64
	var epID string
	if ep != nil {
		tenantID, epID = ep.TenantID, ep.ID
	}
	err := s.db.WithContext(ctx).Exec(`INSERT INTO legal_feed_log
		(action, vbpl_id, knowledge_id, knowledge_base_id, document_number, reason, actor_endpoint_id, actor_tenant_id, detail, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.Action, e.VBPLID, e.KnowledgeID, e.KnowledgeBaseID, e.DocumentNumber, e.Reason, epID, tenantID, nullableJSON(detail), time.Now().UTC()).Error
	if err != nil {
		logger.Warnf(ctx, "[mcpserver] legal_feed_log insert failed (%s %s): %v", e.Action, e.VBPLID, err)
	}
}

func nullableJSON(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return string(b)
}

func knowledgeExternalID(k *types.Knowledge) string {
	return knowledgeMetaString(k, "external_id")
}

func knowledgeMetaString(k *types.Knowledge, key string) string {
	if k == nil || len(k.Metadata) == 0 {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal(k.Metadata, &m); err != nil {
		return ""
	}
	v, _ := m[key].(string)
	return v
}
