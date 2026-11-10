package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"strings"
	"time"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vbpl"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// OCR drafts of scans a legal feed pushes (dangcongsan.vn publishes most
// Party documents only as signed scans). The scan follows the vbpl.vn draft
// path — OCR, review in the editor, Approve — with three differences:
//
//   - the catalogue row is resolved (or created as a feed row) like a push
//     in mode full, and the feed's page address is kept on the draft;
//   - a document without Điều (Kết luận, Thông báo, Hướng dẫn…) may be
//     approved: its text is ingested without provision markup;
//   - on approval the metadata is the catalogue row's and the feed's, not
//     the portal's.

// LegalFeedDraftMaxBytes bounds a pushed scan (signed scans run to tens of MB).
const LegalFeedDraftMaxBytes = 60 << 20

// legalDraftArticleTypes are the types whose text is made of Điều: a draft
// of one without any Điều is an OCR or layout slip, not a document without Điều.
var legalDraftArticleTypes = map[string]bool{"Quy định": true, "Quy chế": true, "Điều lệ": true}

// LegalFeedDraftMeta is what a feed said about its scan (SourceMeta).
type LegalFeedDraftMeta struct {
	Source      string `json:"source"`
	SourceURL   string `json:"source_url"`
	SourceDocID string `json:"source_doc_id"`
	FileName    string `json:"file_name,omitempty"`
}

// LegalFeedDraftInput is one scan a feed pushes: the PDF (main document and
// its appendices in one file) and the push metadata (§9.2 keys; format may
// be left out).
type LegalFeedDraftInput struct {
	PDF      []byte
	FileName string
	Metadata map[string]string
}

// LegalFeedDraftResult answers a pushed scan.
type LegalFeedDraftResult struct {
	Draft            *types.LegalDocumentDraft `json:"draft"`
	LegalDocumentID  string                    `json:"legal_document_id"`
	CatalogueCreated bool                      `json:"catalogue_created,omitempty"`
	// Existing: the document already had a draft in progress or done, which
	// is returned unchanged.
	Existing bool `json:"existing,omitempty"`
}

// draftRow resolves the document of a pushed scan like a push in mode full:
// an existing row, or a feed row inserted now. A document a KB already
// holds, or a fully expired one, gets no draft.
func (s *LegalPushService) draftRow(ctx context.Context, metadata map[string]string) (*types.LegalDocument, *legalPushMeta, bool, error) {
	m := make(map[string]string, len(metadata)+1)
	for k, v := range metadata {
		m[k] = v
	}
	if strings.TrimSpace(m["format"]) == "" {
		m["format"] = vbpl.LegalHTMLFormatV1
	}
	meta, err := readLegalPushMeta(m)
	if err != nil {
		return nil, nil, false, err
	}
	a, cands, reason, err := s.resolveDoc(ctx, meta.ref)
	if err != nil {
		return nil, nil, false, fmt.Errorf("catalogue lookup: %w", err)
	}
	var feedRow *types.LegalDocument
	if a == nil {
		var why string
		a, feedRow, why = s.resolveByNumber(meta, nil, LegalPushFull, cands)
		if a == nil && feedRow == nil {
			return nil, nil, false, legalUnprocessable("không xác định được văn bản "+meta.ref.label()+": "+reason+why,
				map[string]any{"candidates": legalDocCandidates(cands)})
		}
		if feedRow != nil {
			a = feedRow
		}
	}
	if strings.HasPrefix(strings.TrimSpace(a.Status), LegalExpiredStatusPrefix) {
		return nil, nil, false, apperrors.NewBadRequestError(fmt.Sprintf("văn bản %s \"%s\": văn bản hết hiệu lực toàn bộ không nạp toàn văn",
			a.DocumentNumber, a.Status))
	}
	held, err := s.heldDoc(ctx, a.ID)
	if err != nil {
		return nil, nil, false, fmt.Errorf("held document lookup: %w", err)
	}
	if held != nil {
		return nil, nil, false, apperrors.NewConflictError(fmt.Sprintf("văn bản %s (%s) đã có toàn văn trong kho %s (%s)",
			a.ID, a.DocumentNumber, held.KnowledgeBaseID, held.KnowledgeID))
	}
	created := false
	if feedRow != nil {
		if created, err = s.insertFeedRow(ctx, feedRow); err != nil {
			return nil, nil, false, err
		}
	}
	return a, meta, created, nil
}

// PushFeedDraft takes a scan a legal feed pushes and OCRs it into a draft
// for SuperAdmin review. The caller is authorized by the handler like a push.
func (s *LegalDraftService) PushFeedDraft(ctx context.Context, in LegalFeedDraftInput, actor LegalFeedActor) (*LegalFeedDraftResult, error) {
	if s.drafts == nil || s.files == nil || s.Push == nil {
		return nil, apperrors.NewServiceUnavailableError("legal drafts are not available")
	}
	if len(in.PDF) == 0 || !bytes.HasPrefix(bytes.TrimLeft(in.PDF[:min(len(in.PDF), 1024)], "\x00\r\n\t "), []byte("%PDF")) {
		return nil, apperrors.NewBadRequestError("file must be a PDF")
	}
	if len(in.PDF) > LegalFeedDraftMaxBytes {
		return nil, apperrors.NewBadRequestError(fmt.Sprintf("file is larger than %d MB", LegalFeedDraftMaxBytes>>20))
	}
	row, meta, created, err := s.Push.draftRow(ctx, in.Metadata)
	if err != nil {
		return nil, err
	}
	res := &LegalFeedDraftResult{LegalDocumentID: row.ID, CatalogueCreated: created}
	defer s.lockDocument(row.ID)()
	if existing, err := s.reuseDraft(ctx, row.ID, ""); err != nil {
		return nil, err
	} else if existing != nil {
		res.Draft, res.Existing = existing, true
		return res, nil
	}
	fileName := strings.TrimSpace(in.FileName)
	if fileName == "" || !strings.HasSuffix(strings.ToLower(fileName), ".pdf") {
		fileName = strings.ReplaceAll(row.DocumentNumber, "/", "-") + ".pdf"
	}
	src, _ := json.Marshal(LegalFeedDraftMeta{Source: meta.feed, SourceURL: meta.feedURL, SourceDocID: meta.feedDocID, FileName: in.FileName})
	by := actor.UserID
	if by == "" {
		by = legalCap(actor.EndpointID, 36)
	}
	draft := &types.LegalDocumentDraft{
		LegalDocumentID: row.ID, DocumentNumber: row.DocumentNumber, DocType: row.DocType, Title: row.Title,
		PDFFileName: fileName, Status: types.LegalDraftFetching, CreatedBy: by, SourceMeta: types.JSON(src),
	}
	if again, err := s.createDraft(ctx, draft); err != nil {
		return nil, err
	} else if again != nil {
		res.Draft, res.Existing = again, true
		return res, nil
	}
	WriteLegalFeedLog(ctx, s.feed.db, actor, LegalFeedEntry{
		Action: "push_draft", VBPLID: row.ID, DocumentNumber: row.DocumentNumber, Reason: meta.feed + " " + meta.feedDocID,
		Detail: map[string]any{"feed": meta.feed, "feed_doc_id": meta.feedDocID, "feed_url": meta.feedURL, "draft_id": draft.ID,
			"pdf_bytes": len(in.PDF), "catalogue_created": created},
	})
	res.Draft, err = s.storeAndOCR(ctx, draft, in.PDF, "legal_draft_"+row.ID+".pdf")
	return res, err
}

// catalogueRow is the draft's catalogue row (nil when there is none).
func (s *LegalDraftService) catalogueRow(ctx context.Context, d *types.LegalDocumentDraft) *types.LegalDocument {
	if s.feed == nil || s.feed.db == nil {
		return nil
	}
	var row types.LegalDocument
	if err := s.feed.db.WithContext(ctx).Where("id = ?", d.LegalDocumentID).Limit(1).Find(&row).Error; err != nil || row.ID == "" {
		return nil
	}
	return &row
}

// isFeedDraft: the draft is a scan a legal feed pushed, not a vbpl.vn PDF.
func isFeedDraft(d *types.LegalDocumentDraft, row *types.LegalDocument) bool {
	return len(d.SourceMeta) > 0 && string(d.SourceMeta) != "null" || row != nil && row.IsFeedRow()
}

// allowNoArticles lets a feed draft without any Điều be approved: the
// too_few_articles error becomes a warning when no Điều was found at all
// and the type is not one made of Điều.
func allowNoArticles(report *types.LegalDraftStructure, docType string) {
	if report == nil || len(report.Articles) > 0 || legalDraftArticleTypes[strings.TrimSpace(docType)] {
		return
	}
	report.Blocking = 0
	for i := range report.Warnings {
		w := &report.Warnings[i]
		if w.Code == "too_few_articles" {
			w.Severity, w.Blocking = legalDraftSeverityWarning, false
			w.Message = "Không có Điều nào: văn bản được nạp như toàn văn không chia Điều (Kết luận, Thông báo, Hướng dẫn…). " +
				"Nếu văn bản có Điều, sửa để mỗi Điều bắt đầu một đoạn bằng \"Điều N.\""
		}
		if w.Blocking {
			report.Blocking++
		}
	}
	report.Ready = report.Blocking == 0
}

// BuildLegalDraftPlainHTML is the HTML of a reviewed draft without Điều:
// one paragraph per line, no provision markup (the regular parser reads it).
func BuildLegalDraftPlainHTML(title string, all []LegalDraftParagraph) []byte {
	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n<html lang=\"vi\"><head><meta charset=\"utf-8\"><title>")
	b.WriteString(html.EscapeString(title))
	b.WriteString("</title></head><body>\n")
	for _, p := range newDraftLines(all).paras {
		b.WriteString("<p>")
		b.WriteString(html.EscapeString(p.Text))
		b.WriteString("</p>\n")
	}
	b.WriteString("</body></html>\n")
	return []byte(b.String())
}

// approveFeedDraft ingests a reviewed feed draft: the catalogue row's
// metadata and the feed's address, into the KB the SuperAdmin chose (a
// Party document only into LegalPartyKBName).
func (s *LegalDraftService) approveFeedDraft(ctx context.Context, d *types.LegalDocumentDraft, row *types.LegalDocument,
	kb *types.KnowledgeBase, grantCtx context.Context, content []byte, articles int, caller types.Caller,
) (*LegalIngestResult, error) {
	if row == nil {
		return nil, apperrors.NewConflictError("dòng danh mục " + d.LegalDocumentID + " của bản nháp không còn")
	}
	if strings.HasPrefix(strings.TrimSpace(row.Status), LegalExpiredStatusPrefix) {
		return nil, apperrors.NewBadRequestError(fmt.Sprintf("văn bản %s \"%s\": văn bản hết hiệu lực toàn bộ không nạp toàn văn",
			row.DocumentNumber, row.Status))
	}
	var src LegalFeedDraftMeta
	if len(d.SourceMeta) > 0 {
		_ = json.Unmarshal(d.SourceMeta, &src)
	}
	if src.Source == "" {
		src.Source = row.Source
	}
	party := LegalPartyFeeds[src.Source] || vietnamese_legal.IsPartyDocumentNumber(row.DocumentNumber)
	if party && strings.TrimSpace(kb.Name) != LegalPartyKBName {
		return nil, apperrors.NewBadRequestError(fmt.Sprintf("văn bản của Đảng %s nạp vào kho %q, không vào %q",
			row.DocumentNumber, LegalPartyKBName, kb.Name))
	}
	now := time.Now()
	md := map[string]string{
		"external_id": row.ID, "source": src.Source, "source_url": src.SourceURL, "source_doc_id": src.SourceDocID,
		"format": vbpl.LegalHTMLFormatV1, "doc_number": row.DocumentNumber, "doc_type": row.DocType, "eff_status": row.Status,
		"source_format": LegalSourcePDFReviewed, "source_file": d.PDFFileName, "reviewed_by": caller.UserID,
		"reviewed_at": now.Format(time.RFC3339), "draft_id": d.ID,
	}
	if row.IssuedOn != nil {
		md["issue_date"] = row.IssuedOn.Format("2006-01-02")
	}
	if row.EffectiveOn != nil {
		md["eff_from"] = row.EffectiveOn.Format("2006-01-02")
	}
	name := strings.ReplaceAll(row.DocumentNumber+" - "+row.Title, "/", "-")
	if r := []rune(name); len(r) > 180 {
		name = string(r[:180])
	}
	res, err := s.feed.IngestHTML(grantCtx, LegalIngestDocument{
		VBPLID: row.ID, DocumentNumber: row.DocumentNumber, DocType: row.DocType, Status: row.Status, FileName: name + ".html",
		Metadata: md, SourceFormat: LegalSourcePDFReviewed, ReviewedBy: caller.UserID,
		LogDetail: map[string]any{"source_file": d.PDFFileName, "draft_id": d.ID, "articles": articles, "feed": src.Source,
			"feed_url": src.SourceURL},
	}, content, kb, LegalFeedActor{TenantID: caller.TenantID, UserID: caller.UserID, Channel: types.ChannelWeb},
		"Bản nháp OCR đã duyệt "+d.ID+" ("+d.DocumentNumber+", nguồn "+src.Source+")")
	if err != nil {
		logger.Warnf(ctx, "[LegalDraft] ingest of feed draft %s: %v", d.ID, err)
	}
	return res, err
}
