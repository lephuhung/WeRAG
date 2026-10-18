package service

import (
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/docmeasure"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/infrastructure/chunker"
	"github.com/Tencent/WeKnora/internal/infrastructure/docparser"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/vbpl"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// The reviewed-OCR path for legal documents vbpl.vn publishes only as a
// scanned PDF (the SuperAdmin's rule: a scan is never embedded as raw OCR):
//
//	Fetch     → the PDF is downloaded and stored in the platform organization,
//	            then OCRed (vietnamese_legal engine, not billed) into a draft
//	Open      → the OCR text becomes a .docx (docxwrite) in a hidden
//	            document-assistant session of the SuperAdmin, edited in the
//	            embedded editor next to the PDF
//	Structure → the current text is checked: Phần/Chương/Mục/Điều tree,
//	            numbering, khoản/điểm, OCR slips
//	Approve   → the reviewed text is converted to vbpl-style HTML (prov-*
//	            markup), checked with the vbpl converter and ingested through
//	            LegalFeedService.IngestHTML like a portal page.
const (
	// legalDraftSyncOCRPages: a scan of up to this many pages is OCRed
	// while the request waits; a longer one in the background.
	legalDraftSyncOCRPages = 20
	// legalDraftOCRTimeout bounds one OCR run; a draft still "ocr" (or
	// "fetching") longer than legalDraftStaleAfter is fetched again.
	legalDraftOCRTimeout = 30 * time.Minute
	legalDraftStaleAfter = 35 * time.Minute
	// legalDraftFlushWait is how long Structure/Approve wait for the editor
	// to save the latest keystrokes.
	legalDraftFlushWait   = 15 * time.Second
	legalDraftPDFGrantTTL = 2 * time.Hour
	// LegalDraftApprovedNote is the decision note of a request closed by an
	// approved draft.
	LegalDraftApprovedNote = "Bổ sung từ bản PDF đã biên tập"
)

// LegalDraftOCR reads a scanned PDF: its Markdown (with "<!-- page N -->"
// markers), the pages read and the engine used.
type LegalDraftOCR func(ctx context.Context, pdf []byte, fileName string) (markdown string, pages int, engine string, err error)

// LegalDraftService implements interfaces.LegalDraftService.
type LegalDraftService struct {
	drafts      interfaces.LegalDraftRepository
	requests    interfaces.LegalRequestRepository
	requestSvc  interfaces.LegalRequestService
	feed        *LegalFeedService
	files       interfaces.FileService
	catalog     interfaces.ResourceCatalog
	reader      interfaces.DocumentReader
	tenants     interfaces.TenantService
	sessions    interfaces.SessionService
	attachments interfaces.TemporaryDocumentService
	workspaces  interfaces.DocumentWorkspaceService
	kbs         interfaces.KnowledgeBaseService

	// OCR reads a PDF; nil uses the platform organization's pdf parser
	// (tests swap it).
	OCR LegalDraftOCR
	// Go runs a background OCR; nil starts a goroutine (tests run inline).
	Go func(func())
	// PageImages renders PDF pages and ImageOCR reads one page image: the
	// second pass over pages the first OCR looped on (nil: docreader's
	// builtin renderer and the Go paddleocr_vl image reader).
	PageImages LegalDraftPageImages
	ImageOCR   LegalDraftImageOCR
	// StorageImage fetches a page image docreader left in shared storage
	// (nil: the file service, or the SSRF-safe client for a URL).
	StorageImage LegalDraftStorageImage

	// fetching serializes fetches of one document within this process
	fetching sync.Map
}

// NewLegalDraftService wires the draft review.
func NewLegalDraftService(
	drafts interfaces.LegalDraftRepository,
	requests interfaces.LegalRequestRepository,
	requestSvc interfaces.LegalRequestService,
	feed *LegalFeedService,
	files interfaces.FileService,
	catalog interfaces.ResourceCatalog,
	reader interfaces.DocumentReader,
	tenants interfaces.TenantService,
	sessions interfaces.SessionService,
	attachments interfaces.TemporaryDocumentService,
	workspaces interfaces.DocumentWorkspaceService,
	kbs interfaces.KnowledgeBaseService,
) *LegalDraftService {
	return &LegalDraftService{
		drafts: drafts, requests: requests, requestSvc: requestSvc, feed: feed, files: files, catalog: catalog,
		reader: reader, tenants: tenants, sessions: sessions, attachments: attachments, workspaces: workspaces, kbs: kbs,
	}
}

// NewLegalDraftServiceInterface exposes the service under its interface.
func NewLegalDraftServiceInterface(s *LegalDraftService) interfaces.LegalDraftService { return s }

var _ interfaces.LegalDraftService = (*LegalDraftService)(nil)

// ---------------------------------------------------------------------------
// Fetch

// FetchForRequest fetches the PDF of a request's document into a draft.
func (s *LegalDraftService) FetchForRequest(ctx context.Context, requestID string) (*types.LegalDocumentDraft, error) {
	if s.requests == nil {
		return nil, apperrors.NewServiceUnavailableError("legal document requests are not available")
	}
	row, err := s.requests.Get(ctx, strings.TrimSpace(requestID))
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, apperrors.NewNotFoundError("legal document request not found")
	}
	if row.LegalDocumentID == "" {
		return nil, apperrors.NewBadRequestError("the catalogue does not know this document: there is no vbpl.vn PDF to fetch")
	}
	if !types.IsLegalRequestOpen(row.Status) {
		return nil, apperrors.NewConflictError("this request is already " + row.Status)
	}
	return s.fetch(ctx, row.LegalDocumentID, row.ID, types.CallerFromContext(ctx).UserID)
}

// FetchDocument fetches the PDF of a vbpl.vn document into a draft.
func (s *LegalDraftService) FetchDocument(ctx context.Context, vbplID, by string) (*types.LegalDocumentDraft, error) {
	vbplID = strings.TrimSpace(vbplID)
	if vbplID == "" {
		return nil, apperrors.NewBadRequestError("vbpl_id is required")
	}
	requestID := ""
	if s.requests != nil {
		// link the draft to the request users are waiting on, when any
		if rows, _, err := s.requests.List(ctx, types.LegalRequestRequested, 0, 500); err == nil {
			for _, r := range rows {
				if r.LegalDocumentID == vbplID {
					requestID = r.ID
					break
				}
			}
		}
	}
	return s.fetch(ctx, vbplID, requestID, by)
}

func (s *LegalDraftService) fetch(ctx context.Context, vbplID, requestID, by string) (*types.LegalDocumentDraft, error) {
	if s.drafts == nil || s.files == nil {
		return nil, apperrors.NewServiceUnavailableError("legal drafts are not available")
	}
	// one fetch of a document at a time in this process; the unique index
	// on legal_document_id guards the others
	lock, _ := s.fetching.LoadOrStore(vbplID, &sync.Mutex{})
	mu := lock.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()

	existing, err := s.drafts.GetByDocumentID(ctx, vbplID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		stale := (existing.Status == types.LegalDraftOCR || existing.Status == types.LegalDraftFetching) &&
			time.Since(existing.UpdatedAt) > legalDraftStaleAfter
		if existing.Status != types.LegalDraftFailed && !stale {
			if existing.RequestID == "" && requestID != "" {
				if err := s.drafts.Update(ctx, existing.ID, map[string]any{"request_id": requestID}); err == nil {
					existing.RequestID = requestID
				}
			}
			existing.OCRMarkdown = ""
			return existing, nil
		}
		// a failed (or abandoned) draft is fetched again from scratch
		if err := s.drafts.Delete(ctx, existing.ID); err != nil {
			return nil, err
		}
	}

	detail, err := s.feed.fetchDetail(ctx, vbplID)
	if err != nil {
		return nil, legalDraftError(ctx, "portal", vbplID, err)
	}
	portal := portalAvailability(detail)
	if requestID != "" && s.requests != nil {
		if _, err := s.requests.UpdateDetail(ctx, requestID, map[string]any{types.LegalRequestDetailPortal: portal}); err != nil {
			logger.Warnf(ctx, "[LegalDraft] storing portal availability of %s: %v", requestID, err)
		}
	}
	if detail.HasHTML() {
		return nil, apperrors.NewBadRequestError("vbpl.vn có toàn văn HTML của văn bản này: nạp trực tiếp từ vbpl.vn " +
			"(\"Nạp từ vbpl.vn\", MCP: legal_ingest_document), không cần bản nháp")
	}
	if !detail.HasPDF() {
		return nil, apperrors.NewBadRequestError(fmt.Sprintf("vbpl.vn không có toàn văn (HTML hay PDF) cho văn bản %s", vbplID))
	}

	draft := &types.LegalDocumentDraft{
		RequestID: requestID, LegalDocumentID: vbplID, DocumentNumber: strings.TrimSpace(detail.DocNum),
		DocType: strings.TrimSpace(detail.DocType.Name), Title: strings.Join(strings.Fields(detail.Title), " "),
		PDFFileName: portal.PDFFile, Status: types.LegalDraftFetching, CreatedBy: by,
	}
	if err := s.drafts.Create(ctx, draft); err != nil {
		if isUniqueViolation(err) {
			if again, gerr := s.drafts.GetByDocumentID(ctx, vbplID); gerr == nil && again != nil {
				again.OCRMarkdown = ""
				return again, nil
			}
		}
		return nil, err
	}
	s.moveRequests(ctx, vbplID, []string{types.LegalRequestRequested}, types.LegalRequestFetched)

	pdf, err := vbpl.FetchAttachment(ctx, s.feed.HTTP, vbplID, portal.PDFFile)
	if err != nil {
		return s.failDraft(ctx, draft, "Không tải được bản PDF từ vbpl.vn: "+err.Error()), nil
	}
	ref, err := s.files.SaveBytes(ctx, pdf, types.PlatformTenantID, "legal_draft_"+vbplID+".pdf", false)
	if err != nil {
		return s.failDraft(ctx, draft, "Không lưu được bản PDF: "+err.Error()), nil
	}
	if s.catalog != nil {
		if err := s.catalog.Bind(ctx, ref, types.ResourceOwnerLegalDraft, draft.ID, types.ResourceRelationSourceFile); err != nil {
			logger.Warnf(ctx, "[LegalDraft] bind PDF of draft %s: %v", draft.ID, err)
		}
	}
	pages := docmeasure.MeasureFile(".pdf", pdf).Pages
	draft.PDFRef, draft.PDFBytes, draft.Status = ref, int64(len(pdf)), types.LegalDraftOCR
	draft.OCRPages = pages
	if err := s.drafts.Update(ctx, draft.ID, map[string]any{
		"pdf_ref": ref, "pdf_bytes": draft.PDFBytes, "status": draft.Status, "ocr_pages": pages,
	}); err != nil {
		return nil, err
	}
	logger.Infof(ctx, "[LegalDraft] draft %s of %s (%s): PDF %s, %d bytes, %d pages", draft.ID, vbplID,
		draft.DocumentNumber, portal.PDFFile, len(pdf), pages)

	// OCR outlives the request: a detached context with its own deadline
	ocrCtx := context.WithoutCancel(ctx)
	if pages > 0 && pages <= legalDraftSyncOCRPages {
		s.runOCR(ocrCtx, draft, pdf)
	} else {
		run := s.Go
		if run == nil {
			run = func(fn func()) { go fn() }
		}
		copyDraft := *draft
		run(func() { s.runOCR(ocrCtx, &copyDraft, pdf) })
	}
	draft.OCRMarkdown = ""
	return draft, nil
}

// runOCR reads the PDF into the draft: ready with its text, or failed.
func (s *LegalDraftService) runOCR(ctx context.Context, draft *types.LegalDocumentDraft, pdf []byte) {
	ctx, cancel := context.WithTimeout(ctx, legalDraftOCRTimeout)
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf(ctx, "[LegalDraft] OCR of draft %s panicked: %v", draft.ID, r)
			s.failDraft(ctx, draft, fmt.Sprintf("OCR bị lỗi: %v", r))
		}
	}()
	ocr := s.OCR
	if ocr == nil {
		ocr = s.ocrPDF
	}
	started := time.Now()
	markdown, pages, engine, err := ocr(ctx, pdf, draft.PDFFileName)
	if err != nil {
		s.failDraft(ctx, draft, "OCR không thành công: "+err.Error())
		return
	}
	if ParsedTextTooThin(markdown) {
		s.failDraft(ctx, draft, "OCR không đọc được chữ nào trong bản PDF")
		return
	}
	if pages <= 0 {
		pages = draft.OCRPages
	}
	// pages the OCR looped on are read again; what stays broken is flagged
	markdown, repair := s.repairDegeneratePages(ctx, markdown, pdf, draft.PDFFileName)
	reportJSON, _ := json.Marshal(repair)
	draft.OCRMarkdown, draft.OCRPages, draft.OCREngine, draft.Status, draft.Error = markdown, pages, engine, types.LegalDraftReady, ""
	draft.OCRReport = types.JSON(reportJSON)
	if err := s.drafts.Update(ctx, draft.ID, map[string]any{
		"ocr_markdown": markdown, "ocr_pages": pages, "ocr_engine": engine, "status": types.LegalDraftReady, "error": "",
		"ocr_report": types.JSON(reportJSON),
	}); err != nil {
		logger.Warnf(ctx, "[LegalDraft] storing OCR of draft %s: %v", draft.ID, err)
		return
	}
	logger.Infof(ctx, "[LegalDraft] draft %s OCRed: %d pages, %d chars, engine %s, %s", draft.ID, pages,
		len([]rune(markdown)), engine, time.Since(started).Round(time.Second))
}

// failDraft records a failure; the draft's requests go back to the queue.
func (s *LegalDraftService) failDraft(ctx context.Context, draft *types.LegalDocumentDraft, msg string) *types.LegalDocumentDraft {
	if r := []rune(msg); len(r) > 2000 {
		msg = string(r[:2000])
	}
	draft.Status, draft.Error = types.LegalDraftFailed, msg
	if err := s.drafts.Update(ctx, draft.ID, map[string]any{"status": types.LegalDraftFailed, "error": msg}); err != nil {
		logger.Warnf(ctx, "[LegalDraft] marking draft %s failed: %v", draft.ID, err)
	}
	s.moveRequests(ctx, draft.LegalDocumentID, []string{types.LegalRequestFetched, types.LegalRequestReviewing},
		types.LegalRequestRequested)
	logger.Warnf(ctx, "[LegalDraft] draft %s of %s failed: %s", draft.ID, draft.LegalDocumentID, msg)
	draft.OCRMarkdown = ""
	return draft
}

func (s *LegalDraftService) moveRequests(ctx context.Context, vbplID string, from []string, to string) {
	if s.requests == nil {
		return
	}
	if _, err := s.requests.SetStatusByDocument(ctx, vbplID, from, to); err != nil {
		logger.Warnf(ctx, "[LegalDraft] requests of %s → %s: %v", vbplID, to, err)
	}
}

// ocrPDF reads a PDF with the platform organization's pdf parser (the
// vietnamese_legal engine by default, which OCRs scanned pages in docreader
// and marks pages "<!-- page N -->"), like a chat attachment but unbilled.
func (s *LegalDraftService) ocrPDF(ctx context.Context, pdf []byte, fileName string) (string, int, string, error) {
	engine := types.DefaultParserEngine("pdf")
	var overrides map[string]string
	if s.tenants != nil {
		if tenant, err := s.tenants.GetTenantByID(ctx, types.PlatformTenantID); err == nil && tenant != nil {
			ctx = context.WithValue(ctx, types.TenantIDContextKey, tenant.ID)
			ctx = context.WithValue(ctx, types.TenantInfoContextKey, tenant)
			if tenant.ParserEngineConfig != nil {
				engine = tenant.ParserEngineConfig.ResolveChatParserEngine("pdf")
				overrides = tenant.ParserEngineConfig.ToOverridesMap()
			}
		}
	}
	if engine == "auto" {
		engine = ""
	}
	deps := docparser.ReaderDeps{Overrides: overrides}
	if s.reader != nil {
		deps.Remote = s.reader
	}
	reader, err := docparser.NewReader(ctx, engine, "pdf", false, deps)
	if err != nil {
		return "", 0, engine, err
	}
	result, err := reader.Read(ctx, &types.ReadRequest{
		FileContent: pdf, FileName: fileName, FileType: "pdf", ParserEngine: engine, ParserEngineOverrides: overrides,
	})
	if err != nil {
		return "", 0, engine, err
	}
	if result == nil {
		return "", 0, engine, fmt.Errorf("empty parse result")
	}
	if result.Error != "" {
		return "", 0, engine, fmt.Errorf("%s", result.Error)
	}
	md := chunker.NormalizeLineEndings(result.MarkdownContent)
	md = docparser.NormalizeHTMLTables(md)
	md = vietnamese_legal.FixScatteredVietnamese(md)
	pages := 0
	if n, err := fmt.Sscan(result.Metadata[metadataVNOCRPages], &pages); n != 1 || err != nil {
		pages = 0
	}
	if pages == 0 {
		pages = len(ocrPageMarkerRe.FindAllStringIndex(md, -1))
	}
	if engine == "" {
		engine = result.Metadata["parser"]
	}
	return md, pages, engine, nil
}

// legalDraftError maps a portal/feed error to an AppError.
func legalDraftError(ctx context.Context, stage, vbplID string, err error) error {
	var refusal *LegalFeedRefusal
	var appErr *apperrors.AppError
	switch {
	case stderrors.As(err, &refusal):
		return apperrors.NewBadRequestError(refusal.Error())
	case stderrors.As(err, &appErr):
		return err
	}
	logger.Warnf(ctx, "[LegalDraft] %s of %s failed: %v", stage, vbplID, err)
	return apperrors.NewServiceUnavailableError(err.Error())
}

// ---------------------------------------------------------------------------
// Read

func (s *LegalDraftService) load(ctx context.Context, id string) (*types.LegalDocumentDraft, error) {
	if s.drafts == nil {
		return nil, apperrors.NewServiceUnavailableError("legal drafts are not available")
	}
	d, err := s.drafts.Get(ctx, strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	if d == nil {
		return nil, apperrors.NewNotFoundError("legal draft not found")
	}
	return d, nil
}

// Get returns one draft with its OCR text and request.
func (s *LegalDraftService) Get(ctx context.Context, id string) (*types.LegalDraftItem, error) {
	d, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	item := &types.LegalDraftItem{LegalDocumentDraft: d}
	if d.RequestID != "" && s.requests != nil {
		item.Request, _ = s.requests.Get(ctx, d.RequestID)
	}
	return item, nil
}

// List pages drafts by status ("" or "all" = every status).
func (s *LegalDraftService) List(ctx context.Context, status string, offset, limit int) ([]*types.LegalDraftItem, int64, error) {
	if s.drafts == nil {
		return nil, 0, apperrors.NewServiceUnavailableError("legal drafts are not available")
	}
	switch status = strings.ToLower(strings.TrimSpace(status)); status {
	case "all":
		status = ""
	case "", types.LegalDraftFetching, types.LegalDraftOCR, types.LegalDraftReady, types.LegalDraftReviewing,
		types.LegalDraftApproved, types.LegalDraftFailed:
	default:
		return nil, 0, apperrors.NewBadRequestError("status must be fetching, ocr, ready, reviewing, approved, failed or all")
	}
	rows, total, err := s.drafts.List(ctx, status, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	items := make([]*types.LegalDraftItem, 0, len(rows))
	for _, d := range rows {
		item := &types.LegalDraftItem{LegalDocumentDraft: d}
		if d.RequestID != "" && s.requests != nil {
			item.Request, _ = s.requests.Get(ctx, d.RequestID)
		}
		items = append(items, item)
	}
	return items, total, nil
}

// Summaries returns the draft summary per vbpl.vn id.
func (s *LegalDraftService) Summaries(ctx context.Context, vbplIDs []string) (map[string]*types.LegalDraftSummary, error) {
	out := map[string]*types.LegalDraftSummary{}
	if s == nil || s.drafts == nil || len(vbplIDs) == 0 {
		return out, nil
	}
	rows, err := s.drafts.ListByDocumentIDs(ctx, vbplIDs)
	for _, d := range rows {
		out[d.LegalDocumentID] = d.Summary()
	}
	return out, err
}

// Discard deletes a draft that was not approved; the PDF stays stored and
// the hidden session stays for the record. Its requests go back to the queue.
func (s *LegalDraftService) Discard(ctx context.Context, id string) error {
	d, err := s.load(ctx, id)
	if err != nil {
		return err
	}
	if d.Status == types.LegalDraftApproved {
		return apperrors.NewConflictError("this draft was approved and ingested; delete the document from its knowledge base instead")
	}
	if err := s.drafts.Delete(ctx, d.ID); err != nil {
		return err
	}
	s.moveRequests(ctx, d.LegalDocumentID, []string{types.LegalRequestFetched, types.LegalRequestReviewing},
		types.LegalRequestRequested)
	logger.Infof(ctx, "[LegalDraft] draft %s of %s discarded by %s", d.ID, d.LegalDocumentID, types.CallerFromContext(ctx).UserID)
	return nil
}

// ---------------------------------------------------------------------------
// Open

// docxContentType is the MIME type of the draft's .docx.
const legalDraftDocxMIME = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"

// Open returns the draft's editor, creating its .docx, hidden session and
// workspace the first time. The session belongs to the platform
// organization and to the SuperAdmin opening it, so the session document
// routes (/sessions/:id/documents/:doc_id/…) pass their owner checks when
// called with X-Tenant-ID = types.PlatformTenantID.
func (s *LegalDraftService) Open(ctx context.Context, id string, viewer types.LegalDraftViewer) (*types.LegalDraftOpenResult, error) {
	d, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	switch d.Status {
	case types.LegalDraftReady, types.LegalDraftReviewing:
	case types.LegalDraftApproved:
		// an approved draft still opens read-back (the record of what was ingested)
		if d.WorkspaceID == "" {
			return nil, apperrors.NewConflictError("this draft was approved without an editor document")
		}
	case types.LegalDraftFailed:
		return nil, apperrors.NewConflictError("the draft failed: " + d.Error)
	default:
		return nil, apperrors.NewConflictError("the PDF is still being read (" + d.Status + "); try again when the draft is ready")
	}
	if s.sessions == nil || s.attachments == nil || s.workspaces == nil {
		return nil, apperrors.NewServiceUnavailableError("the document editor is not available")
	}
	userID := strings.TrimSpace(viewer.UserID)
	if userID == "" {
		userID = types.CallerFromContext(ctx).UserID
	}
	if userID == "" {
		return nil, apperrors.NewUnauthorizedError("a user is required to open a draft")
	}
	tenantID := types.PlatformTenantID
	// the session routes only find a session of the viewer: a draft opened
	// before by another SuperAdmin moves to this one
	if d.SessionID != "" {
		if sess, err := s.sessions.GetSessionByID(ctx, tenantID, d.SessionID); err == nil && sess != nil && sess.UserID != userID {
			if err := s.sessions.SetSessionOwnerID(ctx, tenantID, d.SessionID, userID); err != nil {
				return nil, fmt.Errorf("hand the review session over: %w", err)
			}
			logger.Infof(ctx, "[LegalDraft] draft %s: review session %s handed over from %s to %s", d.ID, d.SessionID,
				sess.UserID, userID)
			sess.UserID = userID
		}
	}

	var ws *types.DocumentWorkspace
	if d.WorkspaceID != "" && d.SessionID != "" {
		ws, err = s.workspaces.Get(ctx, tenantID, d.SessionID, d.WorkspaceID)
		if err != nil {
			var appErr *apperrors.AppError
			if !stderrors.As(err, &appErr) || appErr.HTTPCode != http.StatusNotFound {
				return nil, err
			}
			ws = nil // the editor document is gone: built again from the OCR text
		}
	}
	if ws == nil {
		ws, err = s.createEditorDocument(ctx, d, userID)
		if err != nil {
			return nil, err
		}
	}
	// the latest reviewer is whoever opened it last
	fields := map[string]any{"reviewed_by": userID}
	if d.Status == types.LegalDraftReady {
		d.Status = types.LegalDraftReviewing
		fields["status"] = d.Status
		s.moveRequests(ctx, d.LegalDocumentID, []string{types.LegalRequestRequested, types.LegalRequestFetched},
			types.LegalRequestReviewing)
	}
	d.ReviewedBy = userID
	if err := s.drafts.Update(ctx, d.ID, fields); err != nil {
		return nil, err
	}

	viewCtx := WithDocumentEditorOrigin(ctx, viewer.Origin)
	view, err := s.workspaces.View(viewCtx, ws, userID, viewer.UserName, viewer.Lang)
	if err != nil {
		return nil, err
	}
	res := &types.LegalDraftOpenResult{
		Draft: d, Editor: view.Editor, SessionID: ws.SessionID, DocumentID: ws.ID, TenantID: tenantID,
		EditorKey: view.EditorKey, Revision: ws.Revision,
	}
	if s.catalog != nil && d.PDFRef != "" {
		if token, err := s.catalog.CreateAccessGrant(ctx, d.PDFRef, legalDraftPDFGrantTTL); err != nil {
			logger.Warnf(ctx, "[LegalDraft] PDF grant of draft %s: %v", d.ID, err)
		} else {
			res.PDFURL = "/r/" + token
		}
	}
	d.OCRMarkdown = ""
	return res, nil
}

// createEditorDocument builds the draft's .docx from its OCR text and opens
// it in a hidden session of userID. Each step stores its id on the draft,
// so a failed open resumes where it stopped.
func (s *LegalDraftService) createEditorDocument(
	ctx context.Context, d *types.LegalDocumentDraft, userID string,
) (*types.DocumentWorkspace, error) {
	tenantID := types.PlatformTenantID
	// the session must be the opener's: the session routes check the owner
	if d.SessionID != "" {
		sess, err := s.sessions.GetSessionByID(ctx, tenantID, d.SessionID)
		if err != nil || sess == nil || sess.UserID != userID {
			d.SessionID, d.AttachmentID = "", ""
		}
	}
	if d.SessionID == "" {
		sess, err := s.sessions.CreateSession(ctx, &types.Session{
			TenantID:    tenantID,
			UserID:      userID,
			Title:       "Biên tập OCR " + firstNonEmpty(d.DocumentNumber, d.LegalDocumentID),
			Description: types.LegalDraftSessionMarker + d.ID,
			LastRequestState: &types.SessionLastRequestState{
				AgentID: types.BuiltinDocumentAssistantID, AgentEnabled: true,
			},
		})
		if err != nil {
			return nil, fmt.Errorf("create the review session: %w", err)
		}
		d.SessionID, d.AttachmentID = sess.ID, ""
		if err := s.drafts.Update(ctx, d.ID, map[string]any{"session_id": sess.ID, "attachment_id": "", "workspace_id": ""}); err != nil {
			return nil, err
		}
	}
	if d.AttachmentID != "" {
		if doc, err := s.attachments.Get(ctx, tenantID, d.SessionID, d.AttachmentID); err != nil || doc == nil {
			d.AttachmentID = "" // expired upload: stored again
		}
	}
	if d.AttachmentID == "" {
		data, err := BuildLegalDraftDocx(d)
		if err != nil {
			return nil, fmt.Errorf("build the draft document: %w", err)
		}
		doc, err := s.attachments.Create(ctx, tenantID, d.SessionID, legalDraftDocxName(d), legalDraftDocxMIME,
			int64(len(data)), bytes.NewReader(data), types.TemporaryDocumentCreateOptions{
				ResourceTenantID: tenantID, InputGate: types.InputGateDocument,
			})
		if err != nil {
			return nil, fmt.Errorf("store the draft document: %w", err)
		}
		d.AttachmentID = doc.ID
		if err := s.drafts.Update(ctx, d.ID, map[string]any{"attachment_id": doc.ID}); err != nil {
			return nil, err
		}
	}
	ws, err := s.workspaces.CreateFromAttachmentFor(ctx, tenantID, d.SessionID, userID, d.AttachmentID,
		types.DocumentEditorKindOnlyOffice)
	if err != nil {
		return nil, err
	}
	d.WorkspaceID = ws.ID
	if err := s.drafts.Update(ctx, d.ID, map[string]any{"workspace_id": ws.ID}); err != nil {
		return nil, err
	}
	logger.Infof(ctx, "[LegalDraft] draft %s opened for review: session %s, workspace %s", d.ID, d.SessionID, ws.ID)
	return ws, nil
}

func legalDraftDocxName(d *types.LegalDocumentDraft) string {
	name := firstNonEmpty(d.DocumentNumber, d.LegalDocumentID)
	name = strings.NewReplacer("/", "-", "\\", "-", ":", " ", "*", " ", "?", " ", "\"", " ", "<", " ", ">", " ", "|", " ").Replace(name)
	return "Bản nháp OCR " + strings.Join(strings.Fields(name), " ") + ".docx"
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Structure and approval

// currentDocx force-saves the editor and returns the draft document's latest
// bytes and workspace.
func (s *LegalDraftService) currentDocx(ctx context.Context, d *types.LegalDocumentDraft) ([]byte, *types.DocumentWorkspace, error) {
	if d.WorkspaceID == "" || d.SessionID == "" {
		return nil, nil, apperrors.NewConflictError("the draft has not been opened in the editor yet")
	}
	if s.workspaces == nil {
		return nil, nil, apperrors.NewServiceUnavailableError("the document editor is not available")
	}
	ws, data, err := s.workspaces.PrepareExternalWrite(ctx, types.PlatformTenantID, d.SessionID, d.WorkspaceID, legalDraftFlushWait)
	if err != nil {
		return nil, nil, err
	}
	return data, ws, nil
}

// Structure checks the draft's current text and caches the report.
func (s *LegalDraftService) Structure(ctx context.Context, id string) (*types.LegalDraftStructure, error) {
	d, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	data, ws, err := s.currentDocx(ctx, d)
	if err != nil {
		return nil, err
	}
	paras, err := ReadLegalDraftParagraphs(data)
	if err != nil {
		return nil, apperrors.NewConflictError("the draft document cannot be read: " + err.Error())
	}
	report := AnalyzeLegalDraft(paras)
	report.Revision = ws.Revision
	s.storeStructure(ctx, d.ID, report)
	return report, nil
}

func (s *LegalDraftService) storeStructure(ctx context.Context, id string, report *types.LegalDraftStructure) {
	raw, err := json.Marshal(report)
	if err != nil {
		return
	}
	if err := s.drafts.Update(ctx, id, map[string]any{"structure": types.JSON(raw)}); err != nil {
		logger.Warnf(ctx, "[LegalDraft] storing the structure of draft %s: %v", id, err)
	}
}

// Approve converts the reviewed draft to vbpl-style HTML and ingests it.
func (s *LegalDraftService) Approve(ctx context.Context, id, kbID string) (*types.LegalDraftApproveResult, error) {
	d, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	switch d.Status {
	case types.LegalDraftReviewing:
	case types.LegalDraftApproved:
		return nil, apperrors.NewConflictError("this draft is already approved")
	default:
		return nil, apperrors.NewConflictError("only a draft opened for review can be approved (status " + d.Status + ")")
	}
	kbID = strings.TrimSpace(kbID)
	if kbID == "" {
		return nil, apperrors.NewBadRequestError("knowledge_base_id is required")
	}
	if s.feed == nil {
		return nil, apperrors.NewServiceUnavailableError("the vbpl.vn feed is not available")
	}
	caller := types.CallerFromContext(ctx)
	kb, grantCtx, err := publicKBWriteContext(ctx, s.kbs, kbID, caller)
	if err != nil {
		return nil, err
	}

	data, _, err := s.currentDocx(ctx, d)
	if err != nil {
		return nil, err
	}
	paras, err := ReadLegalDraftParagraphs(data)
	if err != nil {
		return nil, apperrors.NewConflictError("the draft document cannot be read: " + err.Error())
	}
	report := AnalyzeLegalDraft(paras)
	s.storeStructure(ctx, d.ID, report)
	if !report.Ready {
		return nil, apperrors.NewBadRequestError("the draft is not ready to approve: " + blockingMessages(report)).
			WithDetails(report)
	}
	html := BuildLegalDraftHTML(paras)
	converted, err := vbpl.ConvertBytes(html)
	if err != nil {
		return nil, fmt.Errorf("convert the draft HTML: %w", err)
	}
	if !converted.Structured || converted.Stats.Articles != len(report.Articles) {
		msg := fmt.Sprintf("the HTML built from the draft reads as %d article(s), the structure check found %d",
			converted.Stats.Articles, len(report.Articles))
		if len(converted.Warnings) > 0 {
			msg += ": " + strings.Join(converted.Warnings, "; ")
		}
		return nil, apperrors.NewBadRequestError(msg).WithDetails(map[string]any{
			"converter_warnings": converted.Warnings, "converter_stats": converted.Stats,
		})
	}

	// the portal's metadata (dates, status, appendices) as of now
	detail, err := s.feed.fetchDetail(ctx, d.LegalDocumentID)
	if err != nil {
		return nil, legalDraftError(ctx, "approve", d.LegalDocumentID, err)
	}
	now := time.Now()
	meta := vbpl.Metadata(detail)
	meta["source_format"] = LegalSourcePDFReviewed
	meta["source_file"] = d.PDFFileName
	meta["reviewed_by"] = caller.UserID
	meta["reviewed_at"] = now.Format(time.RFC3339)
	meta["draft_id"] = d.ID
	res, err := s.feed.IngestHTML(grantCtx, LegalIngestDocument{
		VBPLID: d.LegalDocumentID, DocumentNumber: detail.DocNum, DocType: detail.DocType.Name,
		Status: strings.TrimSpace(detail.EffStatus.Name), FileName: vbpl.FileName(detail), Metadata: meta,
		SourceFormat: LegalSourcePDFReviewed, ReviewedBy: caller.UserID,
		LogDetail: map[string]any{"source_file": d.PDFFileName, "draft_id": d.ID, "articles": len(report.Articles)},
	}, html, kb, LegalFeedActor{TenantID: caller.TenantID, UserID: caller.UserID, Channel: types.ChannelWeb},
		"Bản nháp OCR đã duyệt "+d.ID+" ("+d.DocumentNumber+")")
	if err != nil {
		return nil, legalDraftError(ctx, "ingest", d.LegalDocumentID, err)
	}

	d.Status, d.ApprovedBy, d.ApprovedAt = types.LegalDraftApproved, caller.UserID, &now
	d.ApprovedKnowledgeID, d.ApprovedKBID = res.KnowledgeID, kb.ID
	if err := s.drafts.Update(ctx, d.ID, map[string]any{
		"status": d.Status, "approved_by": caller.UserID, "approved_at": now,
		"approved_knowledge_id": res.KnowledgeID, "approved_kb_id": kb.ID,
	}); err != nil {
		logger.Warnf(ctx, "[LegalDraft] marking draft %s approved: %v", d.ID, err)
	}
	out := &types.LegalDraftApproveResult{Result: res.Result, KnowledgeID: res.KnowledgeID, Articles: len(report.Articles)}
	if d.RequestID != "" && s.requests != nil {
		if _, err := s.requests.Decide(ctx, d.RequestID, types.LegalRequestAdded, caller.UserID, LegalDraftApprovedNote,
			res.KnowledgeID, kb.ID); err != nil {
			logger.Warnf(ctx, "[LegalDraft] closing request %s: %v", d.RequestID, err)
		}
		if s.requestSvc != nil {
			out.Request, _ = s.requestSvc.Get(ctx, d.RequestID)
		}
	}
	logger.Infof(ctx, "[LegalDraft] draft %s approved by %s: %s %s in %s (%d articles)", d.ID, caller.UserID,
		res.Result, res.KnowledgeID, kb.ID, len(report.Articles))
	d.OCRMarkdown = ""
	out.Draft = d
	return out, nil
}

func blockingMessages(report *types.LegalDraftStructure) string {
	var msgs []string
	for _, w := range report.Warnings {
		if w.Blocking {
			msgs = append(msgs, w.Message)
		}
	}
	return strings.Join(msgs, "; ")
}
