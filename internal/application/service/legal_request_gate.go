package service

import (
	"context"
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// A question that names a legal document by its full số hiệu the system
// holds no full text of is answered in code: the turn says the document
// is missing and records a request to add it (legal_document_requests),
// which a SuperAdmin ingests from vbpl.vn or rejects. Without the gate the
// agent searched, found nothing and either said "no evidence" or answered
// from memory about a law it never read.

// legalMetadataQuestionRe marks questions about a document's validity or
// identity rather than its content: check_legal_status answers those from
// the catalogue without the full text, so the gate lets them through.
var legalMetadataQuestionRe = regexp.MustCompile(`(?i)hiệu lực|thay thế|bãi bỏ|sửa đổi|ban hành\s+(ngày|khi|năm)|có hiệu lực từ|ngày ký|ai ký|cơ quan ban hành`)

// legalTextActionRe marks a request to produce or check text that cites
// the document ("Soạn tờ trình căn cứ Thông tư 05/2023/TT-BNV", "viết công
// văn trả lời Quyết định 12/2024/QĐ-UBND", "kiểm tra chính tả văn bản…"):
// the user does not ask what the document says, so the turn is not
// answered with the missing-document card. The request is still recorded
// (silently) and the turn runs on.
var legalTextActionRe = regexp.MustCompile(`(?i)^\s*(?:(?:hãy|vui lòng|bạn|giúp|nhờ|cho)\s+(?:tôi\s+|mình\s+|em\s+)?)*` +
	`(?:soạn|viết|tạo|lập|dự thảo|làm\s+(?:giúp\s+)?(?:tôi\s+)?(?:một\s+|cái\s+)?(?:tờ trình|công văn|báo cáo|kế hoạch|quyết định|thông báo|biên bản|văn bản)|` +
	`sửa|chỉnh|rà soát|kiểm tra\s+(?:chính tả|lỗi|định dạng|thể thức)|dịch|định dạng)`)

// legalRequestGateLookup caps the catalogue rows one số hiệu is matched to.
const legalRequestGateLookup = 5

// SetLegalRequests gives the chat the legal document request queue and the
// review links that say which catalogue documents a public KB holds.
func (s *sessionService) SetLegalRequests(requests interfaces.LegalRequestRepository, reviews interfaces.LegalReviewRepository) {
	s.legalRequests, s.legalReviews = requests, reviews
}

// legalMissingDocumentGate answers a turn whose question names a full số
// hiệu that neither a public KB (approved legal link) nor the agent's own
// KBs (a document titled with the number) hold:
//   - a catalogue document that is fully expired: an "expired" card naming
//     what replaced it, nothing recorded;
//   - a catalogue document otherwise: a request is recorded and a
//     "requested" card shown ("rejected" with the reason when a SuperAdmin
//     turned the request down);
//   - a number the catalogue does not know: a request is recorded and the
//     turn runs on, its ctx marked so a "no evidence" answer says so.
//
// The engine does not start when handled is true. kbIDs are the KBs the
// agent searches; an agent without any is left alone, and so is a turn
// about a highlighted passage, an attachment or an @-named document.
func (s *sessionService) legalMissingDocumentGate(
	ctx context.Context, req *types.QARequest, bus *event.EventBus, kbIDs []string,
) (context.Context, bool, error) {
	if s.legalRequests == nil || s.legalCatalog == nil || req == nil || req.Session == nil || bus == nil ||
		req.CustomAgent == nil || len(kbIDs) == 0 {
		return ctx, false, nil
	}
	if req.DocumentSelection.Normalized() != nil || len(req.Attachments) > 0 || len(req.MentionedDocumentIDs) > 0 {
		return ctx, false, nil
	}
	query := norm.NFC.String(req.Query)
	number, start, _ := vietnamese_legal.FindDocumentNumberIndex(query)
	if number == "" || legalMetadataQuestionRe.MatchString(query) {
		return ctx, false, nil
	}
	key := vietnamese_legal.CatalogNumberKey(number)
	docType := tools.LegalDocTypeBefore(query[:start])

	reports, err := s.legalCatalog.Lookup(ctx, number, "", legalRequestGateLookup)
	if err != nil {
		logger.Warnf(ctx, "legal request gate: catalogue lookup of %s: %v", number, err)
		return ctx, false, nil
	}
	reports = legalExactReports(reports, key, docType)

	needles := legalNumberNeedles(number, reports)
	held, err := s.legalRequests.KnowledgeNamed(ctx, kbIDs, needles)
	if err != nil {
		logger.Warnf(ctx, "legal request gate: knowledge lookup of %s: %v", number, err)
		return ctx, false, nil
	}
	if held {
		return ctx, false, nil
	}
	base := types.LegalDocumentRequest{
		NumberKey: key, DocumentNumber: number, DocType: docType,
		LastTenantID: req.Session.TenantID, LastUserID: legalRequestUserID(ctx, req),
		LastSessionID: req.Session.ID, LastQuery: strings.TrimSpace(req.Query),
	}

	textAction := legalTextActionRe.MatchString(query)

	if len(reports) == 0 {
		// an organization's own paperwork (TTr-, CV-, KH-…) is never on
		// vbpl.vn: only a normative type code is worth a request
		if !vietnamese_legal.IsNormativeDocumentNumber(number) {
			return ctx, false, nil
		}
		row, err := s.legalRequests.Record(ctx, &base)
		if err != nil {
			logger.Warnf(ctx, "legal request gate: recording %s: %v", number, err)
			return ctx, false, nil
		}
		logger.Infof(ctx, "Legal document %s is not in the catalogue: request %s recorded (count %d), the turn runs on",
			number, row.ID, row.RequestCount)
		if row.Status == types.LegalRequestRequested {
			ctx = types.WithLegalRequestRecorded(ctx, number)
		}
		return ctx, false, nil
	}

	ids := make([]string, 0, len(reports))
	for _, r := range reports {
		ids = append(ids, r.Document.ID)
	}
	if s.legalReviews != nil {
		approved, err := s.legalReviews.ApprovedDocumentIDs(ctx, ids)
		if err != nil {
			logger.Warnf(ctx, "legal request gate: approved links of %v: %v", ids, err)
			return ctx, false, nil
		}
		if len(approved) > 0 {
			return ctx, false, nil
		}
	}
	// ingested but not linked yet (the review scan runs after parsing)
	ingested, err := s.legalRequests.PublicKnowledgeByExternalIDs(ctx, ids)
	if err != nil {
		logger.Warnf(ctx, "legal request gate: public copies of %v: %v", ids, err)
		return ctx, false, nil
	}
	if len(ingested) > 0 {
		return ctx, false, nil
	}

	card := &tools.LegalRequestCard{Reference: number, DocType: docType}
	for _, r := range reports {
		card.Documents = append(card.Documents, r.Document)
	}
	if legalAllExpired(reports) {
		if textAction {
			return ctx, false, nil
		}
		card.Mode = tools.LegalRequestModeExpired
		card.Replacement = legalReplacement(reports)
		logger.Infof(ctx, "Legal document %s is fully expired and not held: answering with the expired card", number)
		return ctx, true, emitPipelineCard(ctx, bus, req, tools.LegalRequestType, "legalreq-", card.Message(), card.Data())
	}

	var rejected *types.LegalDocumentRequest
	open := 0
	for _, r := range reports {
		d := r.Document
		item := base
		item.LegalDocumentID, item.Source = d.ID, types.LegalRequestSourceCatalogue
		item.Title = d.Title
		if d.DocType != "" {
			item.DocType = d.DocType
		}
		if n := strings.TrimSpace(d.DocumentNumber); n != "" {
			item.DocumentNumber = n
		}
		row, err := s.legalRequests.Record(ctx, &item)
		if err != nil {
			logger.Warnf(ctx, "legal request gate: recording %s (%s): %v", number, d.ID, err)
			return ctx, false, nil
		}
		if row.Status == types.LegalRequestRejected {
			if rejected == nil {
				rejected = row
			}
			continue
		}
		open++
		if row.RequestCount > card.RequestCount {
			card.RequestCount = row.RequestCount
		}
	}
	if textAction {
		// drafting or checking text that cites it: recorded, no card
		logger.Infof(ctx, "Legal document %s has no full text; a text request cites it: recorded, the turn runs on", number)
		if open > 0 {
			ctx = types.WithLegalRequestRecorded(ctx, number)
		}
		return ctx, false, nil
	}
	if open == 0 && rejected != nil {
		card.Mode = tools.LegalRequestModeRejected
		card.DecisionNote = rejected.DecisionNote
	} else {
		card.Mode = tools.LegalRequestModeRequested
	}
	logger.Infof(ctx, "Legal document %s has no full text: answering with the %s card (count %d)",
		number, card.Mode, card.RequestCount)
	return ctx, true, emitPipelineCard(ctx, bus, req, tools.LegalRequestType, "legalreq-", card.Message(), card.Data())
}

// legalExactReports keeps the catalogue rows whose number is the asked one
// (Lookup falls back to a prefix match) and, when the question names a
// type, those of that type — unless none is, the type word being a slip.
func legalExactReports(reports []*types.LegalStatusReport, key, docType string) []*types.LegalStatusReport {
	var exact, typed []*types.LegalStatusReport
	for _, r := range reports {
		if r == nil || r.Document == nil || vietnamese_legal.CatalogNumberKey(r.Document.DocumentNumber) != key {
			continue
		}
		exact = append(exact, r)
		if docType != "" && strings.EqualFold(strings.TrimSpace(r.Document.DocType), docType) {
			typed = append(typed, r)
		}
	}
	if len(typed) > 0 {
		return typed
	}
	return exact
}

// legalNumberNeedles are the spellings a document title or file name may
// carry the number in: as written and as catalogued, with Đ and with D,
// with "/" and with "-" (vbpl file names replace "/").
func legalNumberNeedles(number string, reports []*types.LegalStatusReport) []string {
	seen := map[string]bool{}
	var out []string
	add := func(n string) {
		n = strings.ToLower(strings.TrimSpace(n))
		for _, v := range []string{n, strings.ReplaceAll(n, "/", "-")} {
			for _, w := range []string{v, strings.ReplaceAll(v, "đ", "d")} {
				if w != "" && !seen[w] {
					seen[w] = true
					out = append(out, w)
				}
			}
		}
	}
	add(number)
	for _, r := range reports {
		add(r.Document.DocumentNumber)
	}
	return out
}

func legalAllExpired(reports []*types.LegalStatusReport) bool {
	for _, r := range reports {
		if !strings.HasPrefix(strings.TrimSpace(r.Document.Status), LegalExpiredStatusPrefix) {
			return false
		}
	}
	return len(reports) > 0
}

// legalReplacement is a document replacing or repealing one of reports,
// preferring one still in force.
func legalReplacement(reports []*types.LegalStatusReport) *types.LegalDocument {
	var fallback *types.LegalDocument
	for _, r := range reports {
		for _, l := range r.Links {
			if !l.Incoming || l.Other == nil ||
				(l.Relation != types.LegalRelationReplaces && l.Relation != types.LegalRelationRepeals) {
				continue
			}
			if !strings.HasPrefix(l.Other.Status, LegalExpiredStatusPrefix) {
				return l.Other
			}
			if fallback == nil {
				fallback = l.Other
			}
		}
	}
	return fallback
}

func legalRequestUserID(ctx context.Context, req *types.QARequest) string {
	id := sessionUserIDFromContext(ctx)
	if id == "" && req.Session != nil {
		id = req.Session.UserID
	}
	if len(id) > 36 {
		id = id[:36]
	}
	return id
}
