package service

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/vbpl"
)

// LegalFeedService adds documents from vbpl.vn to the public legal
// knowledge bases. The MCP tool legal_ingest_document and the SuperAdmin
// legal request queue both ingest through IngestFromPortal, so a document
// fed either way gets the same file, metadata and log row (and matches
// scripts/legal_catalog/ingest_vbpl.py byte for byte).
type LegalFeedService struct {
	db        *gorm.DB
	knowledge interfaces.KnowledgeService
	requests  interfaces.LegalRequestRepository
	// HTTP fetches from vbpl.vn; nil uses a default client (tests swap it).
	HTTP *http.Client
	// Reviews receives the approved legal review link of every ingested
	// document (its identity is the vbpl.vn id it was fetched by), so the
	// review scan never has to pick it up; nil skips it.
	Reviews interfaces.LegalReviewRepository
}

// NewLegalFeedService creates the feed service.
func NewLegalFeedService(db *gorm.DB, knowledge interfaces.KnowledgeService,
	requests interfaces.LegalRequestRepository,
) *LegalFeedService {
	return &LegalFeedService{db: db, knowledge: knowledge, requests: requests}
}

// LegalExpiredStatusPrefix starts the catalogue / portal status of a
// document that no longer applies at all ("Hết hiệu lực toàn bộ").
const LegalExpiredStatusPrefix = "Hết hiệu lực toàn bộ"

// LegalFeedActor is who made a feed write: an MCP endpoint or a SuperAdmin.
type LegalFeedActor struct {
	EndpointID string
	TenantID   uint64
	UserID     string
	// Channel is the knowledge channel of created documents ("mcp", "web").
	Channel string
}

// LegalFeedEntry is one row of legal_feed_log.
type LegalFeedEntry struct {
	Action          string
	VBPLID          string
	KnowledgeID     string
	KnowledgeBaseID string
	DocumentNumber  string
	Reason          string
	Detail          map[string]any
}

// Source formats of an ingested document (LegalIngestResult.SourceFormat,
// knowledges.metadata.source_format for a reviewed draft).
const (
	LegalSourceHTML = "html"
	// LegalSourcePDFReviewed: HTML built from the OCR draft of the portal's
	// scanned PDF after a SuperAdmin reviewed and approved it.
	LegalSourcePDFReviewed = "pdf_reviewed"
)

// LegalPortalAvailability is what vbpl.vn offers for one document: its
// HTML full text, its original PDF, or nothing. Stored on a request as
// detail.portal.
type LegalPortalAvailability struct {
	CheckedAt time.Time `json:"checked_at"`
	// Status is the portal's validity status ("Còn hiệu lực"…).
	Status string `json:"status"`
	HTML   bool   `json:"html"`
	// PDFFile is the portal's original PDF ("VanBanGoc_….pdf"), "" when none.
	PDFFile string `json:"pdf_file"`
	// Exists is false when the portal has no document with the id.
	Exists bool `json:"exists"`
}

func portalAvailability(d *vbpl.Detail) *LegalPortalAvailability {
	a := &LegalPortalAvailability{CheckedAt: time.Now(), Exists: d != nil}
	if d != nil {
		a.Status, a.HTML = strings.TrimSpace(d.EffStatus.Name), d.HasHTML()
		if d.HasPDF() {
			a.PDFFile = strings.TrimSpace(d.ContentFileName)
		}
	}
	return a
}

// CheckPortal reads what vbpl.vn offers for a document without downloading
// it. A stale id is a result (Exists false), not an error.
func (s *LegalFeedService) CheckPortal(ctx context.Context, vbplID string) (*LegalPortalAvailability, error) {
	var client *http.Client
	if s != nil {
		client = s.HTTP
	}
	detail, err := vbpl.FetchDetail(ctx, client, vbplID)
	if err != nil {
		var missing *vbpl.NoDocumentError
		if stderrors.As(err, &missing) {
			return portalAvailability(nil), nil
		}
		if strings.Contains(err.Error(), "is not a vbpl.vn document id") {
			return nil, &LegalFeedRefusal{msg: err.Error()}
		}
		return nil, err
	}
	return portalAvailability(detail), nil
}

// LegalIngestResult reports one ingest.
type LegalIngestResult struct {
	// Result is "created", "replaced" or "unchanged".
	Result              string
	VBPLID              string
	KnowledgeID         string
	KnowledgeBaseID     string
	ReplacedKnowledgeID string
	DocumentNumber      string
	DocType             string
	Status              string
	VBPLMarkup          bool
	// SourceFormat is LegalSourceHTML or LegalSourcePDFReviewed.
	SourceFormat string
	// Knowledge is the created document (nil when unchanged).
	Knowledge *types.Knowledge
}

// LegalFeedRefusal is an ingest the feed will not do (fully expired, no
// full text, held by another KB, a bad id): the caller's input, not a
// failure — an HTTP API answers it with 4xx.
type LegalFeedRefusal struct {
	msg string
	// Portal is what vbpl.vn offers when the refusal is about the full
	// text (none, or only a scanned PDF); nil otherwise.
	Portal *LegalPortalAvailability
	// PDFOnly: the portal has only the original PDF, which is never
	// ingested as raw OCR: it goes through a reviewed draft
	// (LegalDraftService).
	PDFOnly bool
}

func (e *LegalFeedRefusal) Error() string { return e.msg }

func refuse(format string, args ...any) error {
	return &LegalFeedRefusal{msg: fmt.Sprintf(format, args...)}
}

// LegalPDFOnlyMessage is the refusal for a document vbpl.vn has only as a
// scanned PDF: a scan is never ingested as raw OCR.
const LegalPDFOnlyMessage = "vbpl.vn chỉ có bản PDF scan của văn bản này; bản scan không được nạp thẳng: " +
	"dùng \"Kéo về bản nháp\" (MCP: legal_fetch_draft) để OCR thành bản nháp, biên tập và duyệt trước khi nạp"

// IngestFromPortal fetches one document from vbpl.vn and adds its HTML
// full text to kb, a public KB (no owner organization). ctx must already
// carry the write grant for kb (the caller authorizes: endpoint scope or
// SuperAdmin). A document with only a scanned PDF is refused (PDFOnly):
// it goes through a reviewed OCR draft. See IngestHTML for the rest.
func (s *LegalFeedService) IngestFromPortal(
	ctx context.Context, vbplID string, kb *types.KnowledgeBase, actor LegalFeedActor, reason string,
) (*LegalIngestResult, error) {
	if s == nil || s.knowledge == nil || s.db == nil {
		return nil, fmt.Errorf("knowledge service is not available")
	}
	vbplID = strings.TrimSpace(vbplID)
	if vbplID == "" || kb == nil {
		return nil, refuse("vbpl_id and knowledge_base_id are required")
	}
	if kb.OwnerTenantID != 0 {
		return nil, refuse("knowledge base %q is not a public legal knowledge base", kb.ID)
	}
	detail, err := s.fetchDetail(ctx, vbplID)
	if err != nil {
		return nil, err
	}
	if !detail.HasHTML() {
		portal := portalAvailability(detail)
		if !detail.HasPDF() {
			return nil, &LegalFeedRefusal{Portal: portal, msg: fmt.Sprintf(
				"vbpl.vn không có toàn văn (HTML hay PDF) cho văn bản %s", vbplID)}
		}
		return nil, &LegalFeedRefusal{Portal: portal, PDFOnly: true, msg: fmt.Sprintf(
			"văn bản %s (%s): %s", vbplID, portal.PDFFile, LegalPDFOnlyMessage)}
	}
	return s.IngestHTML(ctx, LegalIngestDocument{
		VBPLID: vbplID, DocumentNumber: detail.DocNum, DocType: detail.DocType.Name,
		Status: strings.TrimSpace(detail.EffStatus.Name), FileName: vbpl.FileName(detail),
		Metadata: vbpl.Metadata(detail), SourceFormat: LegalSourceHTML,
	}, []byte(detail.Content), kb, actor, reason)
}

// fetchDetail reads one document from the portal; a stale or malformed id
// and a fully expired document are refusals.
func (s *LegalFeedService) fetchDetail(ctx context.Context, vbplID string) (*vbpl.Detail, error) {
	var client *http.Client
	if s != nil {
		client = s.HTTP
	}
	detail, err := vbpl.FetchDetail(ctx, client, vbplID)
	if err != nil {
		var missing *vbpl.NoDocumentError
		if stderrors.As(err, &missing) {
			return nil, refuse("vbpl.vn không có văn bản với id %s (danh mục có thể đã cũ)", missing.ID)
		}
		if strings.Contains(err.Error(), "is not a vbpl.vn document id") {
			return nil, &LegalFeedRefusal{msg: err.Error()}
		}
		return nil, err
	}
	status := strings.TrimSpace(detail.EffStatus.Name)
	if strings.HasPrefix(status, LegalExpiredStatusPrefix) {
		return nil, refuse("document %s (%s) is \"%s\": fully expired documents keep their catalogue "+
			"row only and are never fed into a knowledge base", vbplID, detail.DocNum, status)
	}
	return detail, nil
}

// LegalIngestDocument is one document IngestHTML adds: its identity, the
// knowledge's file name and metadata (vbpl.Metadata plus extras).
type LegalIngestDocument struct {
	VBPLID         string
	DocumentNumber string
	DocType        string
	Status         string
	FileName       string
	Metadata       map[string]string
	// SourceFormat is LegalSourceHTML or LegalSourcePDFReviewed.
	SourceFormat string
	// LogDetail is added to the legal_feed_log row.
	LogDetail map[string]any
	// ReviewedBy is who vouches for the document's identity on its legal
	// review link: the approving SuperAdmin of a reviewed draft;
	// "" = types.LegalReviewerVBPL (fetched by its vbpl.vn id).
	ReviewedBy string
}

// IngestHTML adds a document's HTML (the portal's, or one built from a
// reviewed draft) to kb, a public KB, through the same dedup as every feed
// write. Idempotent: the same bytes in kb are "unchanged", other bytes
// replace the old copy, a copy in another public KB is refused. On success
// the write is logged and open requests for the document are marked added.
// ctx must carry the write grant for kb.
func (s *LegalFeedService) IngestHTML(
	ctx context.Context, doc LegalIngestDocument, content []byte, kb *types.KnowledgeBase, actor LegalFeedActor, reason string,
) (*LegalIngestResult, error) {
	if s == nil || s.knowledge == nil || s.db == nil {
		return nil, fmt.Errorf("knowledge service is not available")
	}
	vbplID := strings.TrimSpace(doc.VBPLID)
	if vbplID == "" || kb == nil {
		return nil, refuse("vbpl_id and knowledge_base_id are required")
	}
	if kb.OwnerTenantID != 0 {
		return nil, refuse("knowledge base %q is not a public legal knowledge base", kb.ID)
	}
	format := doc.SourceFormat
	if format == "" {
		format = LegalSourceHTML
	}
	sum := md5.Sum(content)
	hash := hex.EncodeToString(sum[:])
	res := &LegalIngestResult{
		VBPLID: vbplID, KnowledgeBaseID: kb.ID, DocumentNumber: doc.DocumentNumber,
		DocType: doc.DocType, Status: doc.Status, SourceFormat: format,
		VBPLMarkup: vbpl.LooksLikeVBPL(content),
	}

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
		return nil, fmt.Errorf("lookup failed: %w", err)
	}
	for _, e := range existing {
		if e.KnowledgeBaseID != kb.ID {
			return nil, refuse("document %s is already held by knowledge base %s as %s; a document lives "+
				"in one knowledge base only", vbplID, e.KnowledgeBaseID, e.ID)
		}
		if e.FileHash == hash {
			res.Result, res.KnowledgeID = "unchanged", e.ID
			s.saveReviewLink(ctx, doc, e.ID, kb.ID, false)
			s.markRequestsAdded(ctx, res, actor)
			return res, nil
		}
		if err := s.knowledge.DeleteKnowledge(ctx, e.ID); err != nil {
			return nil, fmt.Errorf("failed to delete the outdated copy %s: %w", e.ID, err)
		}
		res.ReplacedKnowledgeID = e.ID
	}

	fh, err := memoryFileHeader("file", doc.FileName, content)
	if err != nil {
		return nil, fmt.Errorf("failed to stage the file: %w", err)
	}
	channel := actor.Channel
	if channel == "" {
		channel = types.ChannelAPI
	}
	created, err := s.knowledge.CreateKnowledgeFromFile(ctx, kb.ID, fh, doc.Metadata, nil, doc.FileName, nil, channel, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to add document: %w", err)
	}
	res.Result = "created"
	if res.ReplacedKnowledgeID != "" {
		res.Result = "replaced"
	}
	res.KnowledgeID, res.Knowledge = created.ID, created
	logDetail := map[string]any{"replaced_knowledge_id": res.ReplacedKnowledgeID, "status": doc.Status, "md5": hash,
		"vbpl_markup": res.VBPLMarkup, "source_format": format}
	for k, v := range doc.LogDetail {
		logDetail[k] = v
	}
	if actor.UserID != "" {
		logDetail["actor_user_id"] = actor.UserID
	}
	WriteLegalFeedLog(ctx, s.db, actor, LegalFeedEntry{
		Action: "ingest_" + res.Result, VBPLID: vbplID, KnowledgeID: created.ID, KnowledgeBaseID: kb.ID,
		DocumentNumber: doc.DocumentNumber, Reason: reason, Detail: logDetail,
	})
	s.saveReviewLink(ctx, doc, created.ID, kb.ID, true)
	s.markRequestsAdded(ctx, res, actor)
	return res, nil
}

// saveReviewLink records the approved legal review link of an ingested
// document, as the review scan does for a document imported from vbpl.vn:
// legal_document_id is the vbpl.vn id it was fetched by. An unchanged copy
// gets one only when it has none (a reviewer's decision is kept). Best
// effort: a failure is logged and the scan links the document later.
func (s *LegalFeedService) saveReviewLink(ctx context.Context, doc LegalIngestDocument, knowledgeID, kbID string, fresh bool) {
	if s.Reviews == nil || knowledgeID == "" {
		return
	}
	if !fresh {
		if existing, err := s.Reviews.GetLink(ctx, knowledgeID); err != nil || existing != nil {
			return
		}
	}
	by := doc.ReviewedBy
	if by == "" {
		by = types.LegalReviewerVBPL
	}
	sug, _ := json.Marshal(types.LegalIdentitySuggestion{
		DocumentNumber: strings.TrimSpace(doc.DocumentNumber), DocType: strings.TrimSpace(doc.DocType),
		CatalogMatches: []string{doc.VBPLID},
	})
	now := time.Now()
	link := &types.LegalDocumentLink{
		KnowledgeID: knowledgeID, KnowledgeBaseID: kbID, LegalDocumentID: doc.VBPLID,
		ReviewStatus: types.LegalReviewApproved, Suggestion: types.JSON(sug), ReviewedBy: by, ReviewedAt: &now,
	}
	if err := s.Reviews.SaveLink(ctx, link); err != nil {
		logger.Warnf(ctx, "[LegalFeed] review link of %s (%s): %v", knowledgeID, doc.VBPLID, err)
	}
}

// markRequestsAdded closes the open requests for the ingested document.
func (s *LegalFeedService) markRequestsAdded(ctx context.Context, res *LegalIngestResult, actor LegalFeedActor) {
	if s.requests == nil {
		return
	}
	by := actor.UserID
	if by == "" {
		by = actor.EndpointID
	}
	if n, err := s.requests.MarkAddedByDocument(ctx, res.VBPLID, res.KnowledgeID, res.KnowledgeBaseID, by); err != nil {
		logger.Warnf(ctx, "[LegalFeed] marking requests of %s added: %v", res.VBPLID, err)
	} else if n > 0 {
		logger.Infof(ctx, "[LegalFeed] %d request(s) for %s marked added", n, res.VBPLID)
	}
}

// WriteLegalFeedLog appends one row to legal_feed_log; a failure is
// logged, never returned, so the write that succeeded is reported as such.
func WriteLegalFeedLog(ctx context.Context, db *gorm.DB, actor LegalFeedActor, e LegalFeedEntry) {
	if db == nil {
		return
	}
	var detail any
	if e.Detail != nil {
		if b, err := json.Marshal(e.Detail); err == nil && len(b) > 0 {
			detail = string(b)
		}
	}
	err := db.WithContext(ctx).Exec(`INSERT INTO legal_feed_log
		(action, vbpl_id, knowledge_id, knowledge_base_id, document_number, reason, actor_endpoint_id, actor_tenant_id, detail, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.Action, e.VBPLID, e.KnowledgeID, e.KnowledgeBaseID, e.DocumentNumber, e.Reason, actor.EndpointID,
		actor.TenantID, detail, time.Now().UTC()).Error
	if err != nil {
		logger.Warnf(ctx, "[LegalFeed] legal_feed_log insert failed (%s %s): %v", e.Action, e.VBPLID, err)
	}
}

// memoryFileHeader builds a *multipart.FileHeader from bytes, the type
// KnowledgeService.CreateKnowledgeFromFile expects from an HTTP upload.
func memoryFileHeader(field, name string, content []byte) (*multipart.FileHeader, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	hdr := textproto.MIMEHeader{}
	hdr.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, field, strings.ReplaceAll(name, `"`, "'")))
	hdr.Set("Content-Type", "text/html")
	if strings.HasSuffix(strings.ToLower(name), ".pdf") {
		hdr.Set("Content-Type", "application/pdf")
	}
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

// MemoryFileHeader is memoryFileHeader for other packages (the MCP tests).
func MemoryFileHeader(field, name string, content []byte) (*multipart.FileHeader, error) {
	return memoryFileHeader(field, name, content)
}
