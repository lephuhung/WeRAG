package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
	"github.com/google/uuid"
)

const (
	// legalReviewScanBatch / legalReviewScanMax bound one Scan call.
	legalReviewScanBatch = 200
	legalReviewScanMax   = 5000
	// legalReviewHeaderChars is the header region read for the số hiệu.
	legalReviewHeaderChars = 2500
	// LegalReviewStatusAlerts lists approved documents a newer one supersedes.
	LegalReviewStatusAlerts = "alerts"
	// LegalReviewStatusSourceChanged lists approved documents whose reviewed
	// catalogue row has newer portal values waiting in source_pending.
	LegalReviewStatusSourceChanged = "source_changed"
)

// legalSupersedingRelations are the incoming edges that end a document.
var legalSupersedingRelations = []types.LegalRelation{
	types.LegalRelationReplaces, types.LegalRelationRepeals, types.LegalRelationExpires,
}

type legalReviewService struct {
	catalog interfaces.LegalCatalogRepository
	reviews interfaces.LegalReviewRepository
	// kbRepo and models serve Spellcheck only.
	kbRepo interfaces.KnowledgeBaseRepository
	models interfaces.ModelService
}

// NewLegalReviewService creates the legal review service.
func NewLegalReviewService(
	catalog interfaces.LegalCatalogRepository, reviews interfaces.LegalReviewRepository,
	kbRepo interfaces.KnowledgeBaseRepository, models interfaces.ModelService,
) interfaces.LegalReviewService {
	return &legalReviewService{catalog: catalog, reviews: reviews, kbRepo: kbRepo, models: models}
}

func (s *legalReviewService) Scan(ctx context.Context) (int, error) {
	created := 0
	for created < legalReviewScanMax {
		batch, err := s.reviews.UnlinkedPublicKnowledge(ctx, legalReviewScanBatch)
		if err != nil {
			return created, err
		}
		if len(batch) == 0 {
			break
		}
		for _, k := range batch {
			link, err := s.suggest(ctx, k)
			if err != nil {
				return created, err
			}
			if err := s.reviews.SaveLink(ctx, link); err != nil {
				return created, err
			}
			created++
		}
	}
	logger.Infof(ctx, "Legal review scan created %d suggestions", created)
	return created, nil
}

// suggest reads the knowledge's header and proposes its identity: the
// parsed số hiệu, type, issuer and date, plus the catalogue rows sharing
// that number. A single match is pre-linked; the reviewer still confirms.
func (s *legalReviewService) suggest(
	ctx context.Context, k interfaces.LegalPublicKnowledge,
) (*types.LegalDocumentLink, error) {
	header, err := s.reviews.HeaderText(ctx, k.KnowledgeID, legalReviewHeaderChars)
	if err != nil {
		return nil, err
	}
	meta := vietnamese_legal.ParseLegalHeaderMeta(header)
	sug := types.LegalIdentitySuggestion{
		DocumentNumber: vietnamese_legal.NormalizeOwnDocumentNumber(meta.SoHieu),
		Issuer:         suggestedIssuer(meta.CoQuanBanHanh),
		IssuedOn:       meta.NgayBanHanh,
	}
	if d := vietnamese_legal.DocTypeBySlug(meta.DocType.Slug); d != nil {
		sug.DocType = d.Name
	}
	link := &types.LegalDocumentLink{
		KnowledgeID:     k.KnowledgeID,
		KnowledgeBaseID: k.KnowledgeBaseID,
		ReviewStatus:    types.LegalReviewPending,
	}
	if key := vietnamese_legal.CatalogNumberKey(sug.DocumentNumber); key != "" {
		matches, err := s.catalog.FindByNumberKey(ctx, key, 5)
		if err != nil {
			return nil, err
		}
		var exact []*types.LegalDocument
		for _, m := range matches {
			// FindByNumberKey falls back to a prefix match; only an exact
			// number is a candidate here.
			if m.NumberKey == key {
				exact = append(exact, m)
				sug.CatalogMatches = append(sug.CatalogMatches, m.ID)
			}
		}
		link.LegalDocumentID = pickCatalogMatch(exact, k.ExternalID, sug.DocType)
	}
	// A document ingested from vbpl.vn names the portal row it came from
	// (metadata.external_id): its identity is known, not parsed, so the link
	// is approved without a reviewer. The catalogue row is left unreviewed so
	// that later portal imports keep updating its status.
	if k.ExternalID != "" {
		docs, err := s.catalog.GetByIDs(ctx, []string{k.ExternalID})
		if err != nil {
			return nil, err
		}
		for _, d := range docs {
			if d.ID == k.ExternalID && d.Source == types.LegalDocumentSourceVBPL {
				now := time.Now()
				link.LegalDocumentID = d.ID
				link.ReviewStatus = types.LegalReviewApproved
				link.ReviewedBy, link.ReviewedAt = types.LegalReviewerVBPL, &now
			}
		}
	}
	raw, err := json.Marshal(sug)
	if err != nil {
		return nil, err
	}
	link.Suggestion = types.JSON(raw)
	return link, nil
}

// pickCatalogMatch pre-links a suggestion when the candidates sharing its
// số hiệu narrow to one: the row the document was imported from (its
// external ID), else the only one of the parsed type — the Quốc hội numbers
// Luật and Nghị quyết in one series, so "49/2010/QH12" is both. vbpl also
// lists some documents twice; those stay for the reviewer to pick.
func pickCatalogMatch(candidates []*types.LegalDocument, externalID, docType string) string {
	for _, c := range candidates {
		if externalID != "" && c.ID == externalID {
			return c.ID
		}
	}
	if len(candidates) == 1 {
		return candidates[0].ID
	}
	var typed []*types.LegalDocument
	for _, c := range candidates {
		if docType != "" && strings.EqualFold(c.DocType, docType) {
			typed = append(typed, c)
		}
	}
	if len(typed) == 1 {
		return typed[0].ID
	}
	return ""
}

// suggestedIssuer cleans the header parser's issuer guess (the first
// all-caps line) and drops it when that line is not an issuer: Markdown
// noise, the tên loại itself ("## LUẬT") or the national motto / gazette
// banner of a scanned copy.
func suggestedIssuer(line string) string {
	line = strings.Trim(strings.TrimSpace(line), "#* ")
	upper := strings.ToUpper(line)
	if line == "" || strings.Contains(upper, "CỘNG HÒA") || strings.Contains(upper, "CÔNG BÁO") ||
		strings.Contains(upper, "ĐỘC LẬP") {
		return ""
	}
	for _, d := range vietnamese_legal.DocTypes {
		if strings.EqualFold(line, d.Name) {
			return ""
		}
	}
	return line
}

func (s *legalReviewService) List(
	ctx context.Context, status, kbID string, offset, limit int,
) ([]*types.LegalReviewItem, int64, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var rows []interfaces.LegalLinkRow
	var total int64
	var err error
	switch status {
	case LegalReviewStatusAlerts:
		rows, total, err = s.reviews.ListSupersededLinks(ctx, kbID, offset, limit)
	case LegalReviewStatusSourceChanged:
		rows, total, err = s.reviews.ListSourceChangedLinks(ctx, kbID, offset, limit)
	case "", string(types.LegalReviewPending), string(types.LegalReviewApproved), string(types.LegalReviewRejected):
		rows, total, err = s.reviews.ListLinks(ctx, types.LegalReviewStatus(status), kbID, offset, limit)
	default:
		return nil, 0, errors.NewBadRequestError("unknown review status " + status)
	}
	if err != nil {
		return nil, 0, err
	}
	items := make([]*types.LegalReviewItem, 0, len(rows))
	for _, row := range rows {
		item, err := s.buildItem(ctx, row)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, nil
}

// buildItem loads the linked row, the parser's candidates and, for an
// approved link, the newer documents that supersede it.
func (s *legalReviewService) buildItem(ctx context.Context, row interfaces.LegalLinkRow) (*types.LegalReviewItem, error) {
	item := &types.LegalReviewItem{
		Link: row.Link, KnowledgeTitle: row.KnowledgeTitle, KnowledgeBaseName: row.KnowledgeBaseName,
	}
	if len(row.Link.Suggestion) > 0 {
		var sug types.LegalIdentitySuggestion
		if err := json.Unmarshal(row.Link.Suggestion, &sug); err == nil {
			item.Suggestion = &sug
		}
	}
	ids := []string{}
	if row.Link.LegalDocumentID != "" {
		ids = append(ids, row.Link.LegalDocumentID)
	}
	if item.Suggestion != nil {
		ids = append(ids, item.Suggestion.CatalogMatches...)
	}
	docs, err := s.catalog.GetByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, d := range docs {
		if d.ID == row.Link.LegalDocumentID {
			item.Document = d
		} else {
			item.Candidates = append(item.Candidates, d)
		}
	}
	if item.Document == nil || row.Link.ReviewStatus != types.LegalReviewApproved {
		return item, nil
	}
	links, err := s.catalog.ListLinks(ctx, item.Document.ID, nil, legalSupersedingRelations)
	if err != nil || len(links) == 0 {
		return item, err
	}
	otherIDs := make([]string, 0, len(links))
	for _, l := range links {
		otherIDs = append(otherIDs, l.Other.ID)
	}
	inKB, err := s.reviews.ApprovedDocumentIDs(ctx, otherIDs)
	if err != nil {
		return nil, err
	}
	for _, l := range links {
		item.SupersededBy = append(item.SupersededBy, types.LegalSupersession{
			Relation: l.Relation, Document: l.Other, InKB: inKB[l.Other.ID],
		})
	}
	return item, nil
}

func (s *legalReviewService) Decide(
	ctx context.Context, knowledgeID string, d *types.LegalReviewDecision,
) (*types.LegalReviewItem, error) {
	link, err := s.reviews.GetLink(ctx, knowledgeID)
	if err != nil {
		return nil, err
	}
	if link == nil {
		return nil, errors.NewNotFoundError("no legal review for this document; run a scan first")
	}
	reviewer, _ := types.UserIDFromContext(ctx)
	now := time.Now()
	switch d.Action {
	case "reject":
		link.ReviewStatus = types.LegalReviewRejected
	case "approve":
		doc, err := s.reviewedDocument(ctx, d)
		if err != nil {
			return nil, err
		}
		doc.ReviewedBy, doc.ReviewedAt = reviewer, &now
		// The reviewer saw (and settled) any portal values parked earlier.
		doc.SourcePending = nil
		if err := s.catalog.SaveDocument(ctx, doc); err != nil {
			return nil, err
		}
		link.LegalDocumentID = doc.ID
		link.ReviewStatus = types.LegalReviewApproved
	default:
		return nil, errors.NewBadRequestError(`action must be "approve" or "reject"`)
	}
	link.ReviewedBy, link.ReviewedAt = reviewer, &now
	if err := s.reviews.SaveLink(ctx, link); err != nil {
		return nil, err
	}
	return s.buildItem(ctx, interfaces.LegalLinkRow{Link: link})
}

// reviewedDocument returns the catalogue row an approval writes: the picked
// row, or a new manual one, with the reviewer's fields applied.
func (s *legalReviewService) reviewedDocument(
	ctx context.Context, d *types.LegalReviewDecision,
) (*types.LegalDocument, error) {
	var doc *types.LegalDocument
	if id := strings.TrimSpace(d.LegalDocumentID); id != "" {
		docs, err := s.catalog.GetByIDs(ctx, []string{id})
		if err != nil {
			return nil, err
		}
		if len(docs) == 0 {
			return nil, errors.NewNotFoundError("catalogue document not found")
		}
		doc = docs[0]
	} else {
		if d.Document == nil || strings.TrimSpace(d.Document.DocumentNumber) == "" && strings.TrimSpace(d.Document.Title) == "" {
			return nil, errors.NewBadRequestError("pick a catalogue document or give at least a number or title")
		}
		doc = &types.LegalDocument{ID: uuid.NewString(), Source: types.LegalDocumentSourceManual}
	}
	if f := d.Document; f != nil {
		var err error
		if doc.IssuedOn, err = parseReviewDate(f.IssuedOn, "issued_on"); err != nil {
			return nil, err
		}
		if doc.EffectiveOn, err = parseReviewDate(f.EffectiveOn, "effective_on"); err != nil {
			return nil, err
		}
		if doc.ExpiredOn, err = parseReviewDate(f.ExpiredOn, "expired_on"); err != nil {
			return nil, err
		}
		doc.DocumentNumber = strings.TrimSpace(f.DocumentNumber)
		doc.NumberKey = vietnamese_legal.CatalogNumberKey(doc.DocumentNumber)
		doc.Title = strings.TrimSpace(f.Title)
		doc.DocType = strings.TrimSpace(f.DocType)
		doc.Issuer = strings.TrimSpace(f.Issuer)
		doc.Signer = strings.TrimSpace(f.Signer)
		doc.Field = strings.TrimSpace(f.Field)
		doc.Status = strings.TrimSpace(f.Status)
	}
	return doc, nil
}

func parseReviewDate(s, field string) (*time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil, errors.NewBadRequestError(field + " must be YYYY-MM-DD")
	}
	return &t, nil
}

func (s *legalReviewService) SearchCatalog(ctx context.Context, query string, limit int) ([]*types.LegalDocument, error) {
	if limit <= 0 || limit > 20 {
		limit = 10
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return []*types.LegalDocument{}, nil
	}
	if strings.Contains(query, "/") {
		docs, err := s.catalog.FindByNumberKey(ctx, vietnamese_legal.CatalogNumberKey(query), limit)
		if err != nil || len(docs) > 0 {
			return docs, err
		}
	}
	return s.catalog.SearchByTitle(ctx, strings.Fields(query), limit)
}

// legalSupersessionLabels words an incoming superseding edge for the model.
var legalSupersessionLabels = map[types.LegalRelation]string{
	types.LegalRelationReplaces: "Bị thay thế bởi",
	types.LegalRelationRepeals:  "Bị bãi bỏ (toàn bộ hoặc một phần) bởi",
	types.LegalRelationExpires:  "Bị làm hết hiệu lực bởi",
}

func (s *legalReviewService) AnnotateSearchResults(ctx context.Context, results []*types.SearchResult) {
	if len(results) == 0 {
		return
	}
	seen := map[string]bool{}
	var knowledgeIDs []string
	for _, r := range results {
		if r != nil && r.KnowledgeID != "" && !seen[r.KnowledgeID] {
			seen[r.KnowledgeID] = true
			knowledgeIDs = append(knowledgeIDs, r.KnowledgeID)
		}
	}
	links, err := s.reviews.ApprovedLinksForKnowledge(ctx, knowledgeIDs)
	if err != nil {
		logger.Warnf(ctx, "Legal annotation skipped: %v", err)
		return
	}
	if len(links) == 0 {
		return
	}
	docIDs := make([]string, 0, len(links))
	for _, l := range links {
		docIDs = append(docIDs, l.LegalDocumentID)
	}
	docs, err := s.catalog.GetByIDs(ctx, docIDs)
	if err != nil {
		logger.Warnf(ctx, "Legal annotation skipped: %v", err)
		return
	}
	type note struct {
		text       string
		superseded bool
	}
	notes := map[string]note{} // by legal document ID
	for _, doc := range docs {
		text, superseded, err := s.legalNote(ctx, doc)
		if err != nil {
			logger.Warnf(ctx, "Legal annotation of %s skipped: %v", doc.ID, err)
			continue
		}
		notes[doc.ID] = note{text, superseded}
	}
	for _, r := range results {
		if r == nil {
			continue
		}
		l := links[r.KnowledgeID]
		if l == nil {
			continue
		}
		n, ok := notes[l.LegalDocumentID]
		if !ok {
			continue
		}
		if r.KnowledgeCustomMetadata == "" {
			r.KnowledgeCustomMetadata = n.text
		} else {
			r.KnowledgeCustomMetadata = n.text + "\n" + r.KnowledgeCustomMetadata
		}
		r.LegalSuperseded = n.superseded
	}
}

// legalNote renders a reviewed document's identity and validity for the
// model, and reports whether it no longer applies: its status says it
// fully expired, or a document replacing / ending it is already in force.
func (s *legalReviewService) legalNote(ctx context.Context, doc *types.LegalDocument) (string, bool, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "Văn bản pháp lý (đã được quản trị duyệt): %s", legalReviewDocLabel(doc))
	fmt.Fprintf(&b, "; ban hành %s; có hiệu lực %s", legalReviewDate(doc.IssuedOn), legalReviewDate(doc.EffectiveOn))
	if doc.ExpiredOn != nil {
		fmt.Fprintf(&b, "; hết hiệu lực %s", legalReviewDate(doc.ExpiredOn))
	}
	if doc.Status != "" {
		fmt.Fprintf(&b, "; tình trạng: %s", doc.Status)
	}
	b.WriteString(".")
	superseded := strings.HasPrefix(doc.Status, "Hết hiệu lực toàn bộ")

	links, err := s.catalog.ListLinks(ctx, doc.ID, nil, legalSupersedingRelations)
	if err != nil {
		return "", false, err
	}
	if len(links) == 0 {
		return b.String(), superseded, nil
	}
	otherIDs := make([]string, 0, len(links))
	for _, l := range links {
		otherIDs = append(otherIDs, l.Other.ID)
	}
	inKB, err := s.reviews.ApprovedDocumentIDs(ctx, otherIDs)
	if err != nil {
		return "", false, err
	}
	now := time.Now()
	for _, l := range links {
		o := l.Other
		fmt.Fprintf(&b, "\n%s %s", legalSupersessionLabels[l.Relation], legalReviewDocLabel(o))
		if o.EffectiveOn != nil {
			fmt.Fprintf(&b, ", có hiệu lực từ %s", legalReviewDate(o.EffectiveOn))
		}
		if inKB[o.ID] {
			b.WriteString(" — văn bản này có trong kho tài liệu.")
		} else {
			b.WriteString(" — văn bản này CHƯA có trong kho tài liệu: hãy nói rõ với người dùng " +
				"rằng nội dung trên có thể đã bị thay đổi.")
		}
		inForce := o.EffectiveOn == nil || !o.EffectiveOn.After(now)
		if inForce && l.Relation != types.LegalRelationRepeals {
			superseded = true
		}
	}
	return b.String(), superseded, nil
}

func legalReviewDocLabel(doc *types.LegalDocument) string {
	title := strings.TrimSpace(doc.Title)
	num := strings.TrimSpace(doc.DocumentNumber)
	switch {
	case num != "" && strings.Contains(title, num):
		return title
	case num != "" && title != "":
		return strings.TrimSpace(fmt.Sprintf("%s %s (%s)", doc.DocType, num, title))
	case num != "":
		return strings.TrimSpace(doc.DocType + " " + num)
	default:
		return title
	}
}

func legalReviewDate(t *time.Time) string {
	if t == nil {
		return "chưa rõ"
	}
	return t.Format("02/01/2006")
}

// legalSpellcheckMaxItems bounds one Spellcheck call (one page of chunks).
const legalSpellcheckMaxItems = 40

func (s *legalReviewService) Spellcheck(
	ctx context.Context, knowledgeID string, items []types.LegalSpellcheckItem,
) ([]types.LegalSpellcheckFinding, error) {
	if len(items) == 0 {
		return []types.LegalSpellcheckFinding{}, nil
	}
	if len(items) > legalSpellcheckMaxItems {
		return nil, errors.NewBadRequestError(fmt.Sprintf("at most %d chunks per call", legalSpellcheckMaxItems))
	}
	// Only documents of the review queue (platform public KBs) qualify.
	link, err := s.reviews.GetLink(ctx, knowledgeID)
	if err != nil {
		return nil, err
	}
	if link == nil {
		return nil, errors.NewNotFoundError("no legal review for this document; run a scan first")
	}
	if s.kbRepo == nil || s.models == nil {
		return nil, errors.NewInternalServerError("spellcheck is not available")
	}
	kb, err := s.kbRepo.GetKnowledgeBaseByID(ctx, link.KnowledgeBaseID)
	if err != nil {
		return nil, err
	}
	if kb == nil || strings.TrimSpace(kb.SummaryModelID) == "" {
		return nil, errors.NewBadRequestError("the knowledge base has no summary model to proofread with")
	}
	model, err := s.models.GetChatModel(ctx, kb.SummaryModelID)
	if err != nil {
		return nil, err
	}
	texts := make([]string, len(items))
	for i, it := range items {
		texts[i] = it.Content
	}
	found, err := tools.ReviewSpelling(ctx, model, "", texts)
	if err != nil {
		return nil, errors.NewInternalServerError(err.Error())
	}
	out := make([]types.LegalSpellcheckFinding, 0, len(found))
	for _, f := range found {
		if f.Paragraph < 0 || f.Paragraph >= len(items) {
			continue
		}
		out = append(out, types.LegalSpellcheckFinding{
			ChunkID: items[f.Paragraph].ChunkID, Wrong: f.Wrong, Correct: f.Correct, Reason: f.Reason,
		})
	}
	return out, nil
}
