package service

import (
	"context"
	stderrors "errors"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/access"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// LegalKBNamePrefix starts the names of the public legal KBs: "VBPL –
// <type>, <type>…" (docs/legal-document-feed-spec.md §1); the types after
// it say which document types each one holds.
const LegalKBNamePrefix = "VBPL – "

// LegalTypeTargets maps each document type to the public KB named for it:
// "VBPL – Hiến pháp, Bộ luật, Luật, Pháp lệnh" → four types. A type listed
// by two KBs keeps the first (name order).
func LegalTypeTargets(kbs []types.LegalPublicKB) map[string]types.LegalPublicKB {
	out := map[string]types.LegalPublicKB{}
	for _, kb := range kbs {
		if !strings.HasPrefix(kb.Name, LegalKBNamePrefix) {
			continue
		}
		for _, t := range strings.Split(strings.TrimPrefix(kb.Name, LegalKBNamePrefix), ",") {
			t = strings.TrimSpace(t)
			if _, dup := out[t]; t != "" && !dup {
				out[t] = kb
			}
		}
	}
	return out
}

type legalRequestService struct {
	requests interfaces.LegalRequestRepository
	catalog  interfaces.LegalCatalogRepository
	kbs      interfaces.KnowledgeBaseService
	feed     *LegalFeedService
	// drafts adds the OCR draft summary to request rows (nil: none shown)
	drafts interfaces.LegalDraftRepository
}

// NewLegalRequestService creates the SuperAdmin legal request queue.
func NewLegalRequestService(requests interfaces.LegalRequestRepository, catalog interfaces.LegalCatalogRepository,
	kbs interfaces.KnowledgeBaseService, feed *LegalFeedService, drafts interfaces.LegalDraftRepository,
) interfaces.LegalRequestService {
	return &legalRequestService{requests: requests, catalog: catalog, kbs: kbs, feed: feed, drafts: drafts}
}

func (s *legalRequestService) List(
	ctx context.Context, status string, offset, limit int,
) ([]*types.LegalRequestItem, int64, map[string]int64, error) {
	switch status = strings.ToLower(strings.TrimSpace(status)); status {
	case "all":
		status = ""
	case "", types.LegalRequestRequested, types.LegalRequestFetched, types.LegalRequestReviewing,
		types.LegalRequestAdded, types.LegalRequestRejected:
	default:
		return nil, 0, nil, apperrors.NewBadRequestError("status must be requested, fetched, reviewing, added, rejected or all")
	}
	rows, total, err := s.requests.List(ctx, status, offset, limit)
	if err != nil {
		return nil, 0, nil, err
	}
	counts, err := s.requests.CountByStatus(ctx)
	if err != nil {
		return nil, 0, nil, err
	}
	items, err := s.items(ctx, rows)
	return items, total, counts, err
}

// items adds the catalogue document and the suggested public KB to rows.
func (s *legalRequestService) items(ctx context.Context, rows []*types.LegalDocumentRequest) ([]*types.LegalRequestItem, error) {
	var ids []string
	for _, r := range rows {
		if r.LegalDocumentID != "" {
			ids = append(ids, r.LegalDocumentID)
		}
	}
	docs := map[string]*types.LegalDocument{}
	if len(ids) > 0 && s.catalog != nil {
		found, err := s.catalog.GetByIDs(ctx, ids)
		if err != nil {
			return nil, err
		}
		for _, d := range found {
			docs[d.ID] = d
		}
	}
	var targets map[string]types.LegalPublicKB
	if kbs, err := s.requests.PublicKnowledgeBases(ctx); err != nil {
		logger.Warnf(ctx, "[LegalRequests] public KBs: %v", err)
	} else {
		targets = LegalTypeTargets(kbs)
	}
	var knowledgeIDs []string
	for _, r := range rows {
		if r.KnowledgeID != "" {
			knowledgeIDs = append(knowledgeIDs, r.KnowledgeID)
		}
	}
	parse := map[string]string{}
	if len(knowledgeIDs) > 0 {
		if st, err := s.requests.KnowledgeParseStatuses(ctx, knowledgeIDs); err != nil {
			logger.Warnf(ctx, "[LegalRequests] parse statuses: %v", err)
		} else {
			parse = st
		}
	}
	drafts := map[string]*types.LegalDraftSummary{}
	if len(ids) > 0 && s.drafts != nil {
		if found, err := s.drafts.ListByDocumentIDs(ctx, ids); err != nil {
			logger.Warnf(ctx, "[LegalRequests] drafts: %v", err)
		} else {
			for _, d := range found {
				drafts[d.LegalDocumentID] = d.Summary()
			}
		}
	}
	items := make([]*types.LegalRequestItem, 0, len(rows))
	for _, r := range rows {
		item := &types.LegalRequestItem{LegalDocumentRequest: r, KnowledgeParseStatus: parse[r.KnowledgeID]}
		if r.LegalDocumentID != "" {
			item.Draft = drafts[r.LegalDocumentID]
		}
		docType := r.DocType
		if d := docs[r.LegalDocumentID]; d != nil {
			item.Document = &types.LegalRequestDocument{
				ID: d.ID, DocumentNumber: d.DocumentNumber, Title: d.Title, DocType: d.DocType,
				Issuer: d.Issuer, Status: d.Status, URL: vietnamese_legal.VBPLDocumentURL + d.ID,
			}
			if d.IssuedOn != nil {
				item.Document.IssuedOn = d.IssuedOn.Format("2006-01-02")
			}
			if d.DocType != "" {
				docType = d.DocType
			}
		}
		if kb, ok := targets[strings.TrimSpace(docType)]; ok {
			item.SuggestedKBID = kb.ID
		}
		items = append(items, item)
	}
	return items, nil
}

func (s *legalRequestService) item(ctx context.Context, row *types.LegalDocumentRequest) (*types.LegalRequestItem, error) {
	items, err := s.items(ctx, []*types.LegalDocumentRequest{row})
	if err != nil || len(items) == 0 {
		return nil, err
	}
	return items[0], nil
}

func (s *legalRequestService) Decide(
	ctx context.Context, id string, d *types.LegalRequestDecision,
) (*types.LegalRequestItem, error) {
	if d == nil {
		return nil, apperrors.NewBadRequestError("decision is required")
	}
	row, err := s.requests.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, apperrors.NewNotFoundError("legal document request not found")
	}
	caller := types.CallerFromContext(ctx)
	note := strings.TrimSpace(d.Note)
	var decided *types.LegalDocumentRequest
	switch strings.ToLower(strings.TrimSpace(d.Action)) {
	case types.LegalRequestActionReject:
		decided, err = s.requests.Decide(ctx, id, types.LegalRequestRejected, caller.UserID, note, "", "")
	case types.LegalRequestActionAdded:
		decided, err = s.requests.Decide(ctx, id, types.LegalRequestAdded, caller.UserID, note,
			strings.TrimSpace(d.KnowledgeID), strings.TrimSpace(d.KnowledgeBaseID))
	case types.LegalRequestActionIngest:
		res, ierr := s.ingest(ctx, row, strings.TrimSpace(d.KnowledgeBaseID), caller)
		if ierr != nil {
			return nil, ierr
		}
		if note == "" {
			note = "Bổ sung từ vbpl.vn (" + res.Result + ", " + res.SourceFormat + ")"
		}
		decided, err = s.requests.Decide(ctx, id, types.LegalRequestAdded, caller.UserID, note, res.KnowledgeID, res.KnowledgeBaseID)
	default:
		return nil, apperrors.NewBadRequestError("action must be ingest, reject or added")
	}
	if err != nil {
		return nil, err
	}
	if decided == nil {
		return nil, apperrors.NewNotFoundError("legal document request not found")
	}
	return s.item(ctx, decided)
}

// ingest fetches the request's document from vbpl.vn into a public KB with
// the same code as the MCP tool legal_ingest_document.
func (s *legalRequestService) ingest(
	ctx context.Context, row *types.LegalDocumentRequest, kbID string, caller types.Caller,
) (*LegalIngestResult, error) {
	if row.LegalDocumentID == "" {
		return nil, apperrors.NewBadRequestError("the catalogue does not know this document: add it by hand and mark the request added")
	}
	if kbID == "" {
		return nil, apperrors.NewBadRequestError("knowledge_base_id is required")
	}
	if s.feed == nil || s.kbs == nil {
		return nil, apperrors.NewServiceUnavailableError("the vbpl.vn feed is not available")
	}
	kb, grantCtx, err := publicKBWriteContext(ctx, s.kbs, kbID, caller)
	if err != nil {
		return nil, err
	}
	res, err := s.feed.IngestFromPortal(grantCtx, row.LegalDocumentID, kb,
		LegalFeedActor{TenantID: caller.TenantID, UserID: caller.UserID, Channel: types.ChannelWeb},
		"Đề nghị bổ sung "+row.ID+" ("+row.DocumentNumber+")")
	if err != nil {
		var refusal *LegalFeedRefusal
		var appErr *apperrors.AppError
		switch {
		case stderrors.As(err, &refusal):
			// keep what the portal offers so the queue can show the PDF
			// option (or why nothing can be ingested) without a new check
			if refusal.Portal != nil {
				s.storePortal(ctx, row.ID, refusal.Portal)
			}
			if refusal.PDFOnly {
				return nil, apperrors.NewBadRequestError(LegalRequestPDFOnlyMessage)
			}
			return nil, apperrors.NewBadRequestError(refusal.Error())
		case stderrors.As(err, &appErr):
			return nil, err
		default:
			logger.Warnf(ctx, "[LegalRequests] ingest of %s failed: %v", row.LegalDocumentID, err)
			return nil, apperrors.NewServiceUnavailableError(err.Error())
		}
	}
	return res, nil
}

// LegalRequestPDFOnlyMessage answers an ingest of a document vbpl.vn has
// only as a scanned PDF: the scan goes through a reviewed OCR draft.
const LegalRequestPDFOnlyMessage = "vbpl.vn chỉ có bản PDF scan của văn bản này; dùng \"Kéo về bản nháp\" để OCR thành " +
	"bản nháp, biên tập rồi duyệt trước khi nạp"

// publicKBWriteContext loads the public KB kbID and returns a context
// carrying the caller's write grant on it (a SuperAdmin manages every KB).
func publicKBWriteContext(
	ctx context.Context, kbs interfaces.KnowledgeBaseService, kbID string, caller types.Caller,
) (*types.KnowledgeBase, context.Context, error) {
	if kbs == nil {
		return nil, nil, apperrors.NewServiceUnavailableError("knowledge bases are not available")
	}
	kb, err := kbs.GetKnowledgeBaseByIDOnly(ctx, kbID)
	if err != nil || kb == nil {
		return nil, nil, apperrors.NewNotFoundError("knowledge base not found")
	}
	if kb.OwnerTenantID != 0 {
		return nil, nil, apperrors.NewBadRequestError("knowledge base " + kb.ID + " is not a public legal knowledge base")
	}
	grant, err := access.ResolveKBForManage(ctx, access.KBRequest{Caller: caller}, kb)
	if err != nil {
		return nil, nil, apperrors.NewForbiddenError("not allowed to write to knowledge base " + kb.ID)
	}
	return kb, grant.Context(ctx), nil
}

// Get returns one request as a queue item, nil when absent.
func (s *legalRequestService) Get(ctx context.Context, id string) (*types.LegalRequestItem, error) {
	row, err := s.requests.Get(ctx, id)
	if err != nil || row == nil {
		return nil, err
	}
	return s.item(ctx, row)
}

// CheckPortal reads what vbpl.vn offers for the request's document and
// stores it as detail.portal.
func (s *legalRequestService) CheckPortal(ctx context.Context, id string) (*types.LegalRequestItem, error) {
	row, err := s.requests.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, apperrors.NewNotFoundError("legal document request not found")
	}
	if row.LegalDocumentID == "" {
		return nil, apperrors.NewBadRequestError("the catalogue does not know this document: there is no vbpl.vn id to check")
	}
	if s.feed == nil {
		return nil, apperrors.NewServiceUnavailableError("the vbpl.vn feed is not available")
	}
	portal, err := s.feed.CheckPortal(ctx, row.LegalDocumentID)
	if err != nil {
		var refusal *LegalFeedRefusal
		if stderrors.As(err, &refusal) {
			return nil, apperrors.NewBadRequestError(refusal.Error())
		}
		logger.Warnf(ctx, "[LegalRequests] portal check of %s failed: %v", row.LegalDocumentID, err)
		return nil, apperrors.NewServiceUnavailableError(err.Error())
	}
	updated, err := s.requests.UpdateDetail(ctx, id, map[string]any{types.LegalRequestDetailPortal: portal})
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, apperrors.NewNotFoundError("legal document request not found")
	}
	return s.item(ctx, updated)
}

// storePortal records detail.portal; a failure is logged only.
func (s *legalRequestService) storePortal(ctx context.Context, id string, portal *LegalPortalAvailability) {
	if _, err := s.requests.UpdateDetail(ctx, id, map[string]any{types.LegalRequestDetailPortal: portal}); err != nil {
		logger.Warnf(ctx, "[LegalRequests] storing portal availability of %s: %v", id, err)
	}
}

func (s *legalRequestService) PublicKnowledgeBases(ctx context.Context) ([]types.LegalPublicKB, error) {
	return s.requests.PublicKnowledgeBases(ctx)
}
