package service

import (
	"context"
	stderrors "errors"
	"sort"
	"strings"

	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/docformat/answerdocx"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// AnswerExportService writes one assistant answer of a chat as a .docx
// (GET /sessions/:id/messages/:message_id/export.docx): the stored answer
// and its sources, no model call. Reads follow the message history route
// (MessageService.GetRecentMessagesBySession): the caller must be able to
// read the session, and sources from knowledge bases the caller can no
// longer read are left out.
type AnswerExportService struct {
	// reader is a message service over the same repositories, for its
	// session read rule and reference filter.
	reader  *messageService
	kbs     interfaces.KnowledgeBaseService
	reviews interfaces.LegalReviewRepository
	catalog interfaces.LegalCatalogRepository
}

// AnswerExportFile is the written document.
type AnswerExportFile struct {
	FileName string
	Data     []byte
}

// maxExportReferences bounds the sources listed (distinct documents).
const maxExportReferences = 100

// NewAnswerExportService wires the export. The legal repositories may be
// nil (no số hiệu then).
func NewAnswerExportService(
	messageRepo interfaces.MessageRepository,
	sessionRepo interfaces.SessionRepository,
	kbService interfaces.KnowledgeBaseService,
	knowService interfaces.KnowledgeService,
	kbAccessGrantService interfaces.KBAccessGrantService,
	reviews interfaces.LegalReviewRepository,
	catalog interfaces.LegalCatalogRepository,
) *AnswerExportService {
	return &AnswerExportService{
		reader: &messageService{
			messageRepo:          messageRepo,
			sessionRepo:          sessionRepo,
			kbService:            kbService,
			knowService:          knowService,
			kbAccessGrantService: kbAccessGrantService,
		},
		kbs:     kbService,
		reviews: reviews,
		catalog: catalog,
	}
}

// Export writes the answer messageID of session sessionID. Errors:
// apperrors.ErrSessionNotFound or gorm.ErrRecordNotFound when the caller
// cannot see it, ErrAnswerExportNotAnswer / ErrAnswerExportIncomplete /
// ErrAnswerExportFailedTurn when it is no finished answer.
func (s *AnswerExportService) Export(ctx context.Context, sessionID, messageID string) (*AnswerExportFile, error) {
	answer, question, err := s.load(ctx, sessionID, messageID)
	if err != nil {
		return nil, err
	}
	in := answerdocx.Input{
		AnsweredAt: formatDateTimeVN(answer.CreatedAt),
		Content:    answer.Content,
		References: s.references(ctx, answer.KnowledgeReferences),
	}
	if question != nil {
		in.Question = question.Content
	}
	data, err := answerdocx.Build(in)
	if err != nil {
		return nil, err
	}
	return &AnswerExportFile{FileName: answerdocx.FileName(in.Question), Data: data}, nil
}

// load returns the answer and the question it answers (nil when that is
// gone).
func (s *AnswerExportService) load(ctx context.Context, sessionID, messageID string) (*types.Message, *types.Message, error) {
	tenantID, ok := sessionTenantIDForLookup(ctx)
	if !ok {
		return nil, nil, apperrors.ErrSessionNotFound
	}
	if _, err := loadSessionForRead(ctx, s.reader.sessionRepo, tenantID, sessionUserIDForLookup(ctx), sessionID); err != nil {
		return nil, nil, err
	}
	answer, err := s.reader.messageRepo.GetMessage(ctx, sessionID, messageID)
	if err != nil {
		return nil, nil, err
	}
	if answer == nil {
		return nil, nil, gorm.ErrRecordNotFound
	}
	if answer.Role != "assistant" {
		return nil, nil, apperrors.ErrAnswerExportNotAnswer
	}
	if answer.ExecutionContext.TurnFailure != "" {
		return nil, nil, apperrors.ErrAnswerExportFailedTurn
	}
	if !answer.IsCompleted {
		return nil, nil, apperrors.ErrAnswerExportIncomplete
	}
	if strings.TrimSpace(answer.Content) == "" {
		return nil, nil, apperrors.ErrAnswerExportFailedTurn
	}
	s.reader.hideUnreadableReferences(ctx, []*types.Message{answer})

	var question *types.Message
	if answer.RequestID != "" {
		pair, err := s.reader.messageRepo.GetMessagesByRequestIDs(ctx, sessionID, []string{answer.RequestID})
		if err != nil {
			logger.Warnf(ctx, "answer export: question of %s unavailable: %v", messageID, err)
		}
		sort.SliceStable(pair, func(i, j int) bool { return pair[i].CreatedAt.Before(pair[j].CreatedAt) })
		for _, m := range pair {
			if m != nil && m.Role == "user" && !m.CreatedAt.After(answer.CreatedAt) {
				msg := m.Message
				question = &msg
				break
			}
		}
	}
	return answer, question, nil
}

// references lists each source document once, in first-cited order, with
// its knowledge base and, for a reviewed legal document, its số hiệu.
func (s *AnswerExportService) references(ctx context.Context, refs types.References) []answerdocx.Reference {
	type entry struct {
		ref         answerdocx.Reference
		knowledgeID string
		kbID        string
	}
	var entries []*entry
	seen := map[string]bool{}
	for _, r := range refs {
		if r == nil || len(entries) >= maxExportReferences {
			continue
		}
		e := &entry{ref: answerdocx.Reference{
			Title:    strings.TrimSpace(r.KnowledgeTitle),
			FileName: strings.TrimSpace(r.KnowledgeFilename),
		}}
		key := r.KnowledgeID
		if isWebSearchReference(r) {
			e.ref.URL = strings.TrimSpace(r.Metadata["url"])
			if e.ref.URL == "" && strings.HasPrefix(r.KnowledgeID, "http") {
				e.ref.URL = r.KnowledgeID
			}
			if e.ref.URL == "" && strings.HasPrefix(r.ID, "http") {
				e.ref.URL = r.ID
			}
			key = "web:" + e.ref.URL
		} else {
			e.knowledgeID, e.kbID = r.KnowledgeID, r.KnowledgeBaseID
		}
		if key == "" || key == "web:" {
			key = "title:" + e.ref.Title + "\x00" + e.ref.FileName
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		entries = append(entries, e)
	}
	if len(entries) == 0 {
		return nil
	}

	// knowledge bases: from the reference, else from its document
	if s.reader.knowService != nil {
		for _, e := range entries {
			if e.kbID == "" && e.knowledgeID != "" {
				if k, err := s.reader.knowService.GetKnowledgeByIDOnly(ctx, e.knowledgeID); err == nil && k != nil {
					e.kbID = k.KnowledgeBaseID
				}
			}
		}
	}
	kbIDs := map[string]bool{}
	var knowledgeIDs []string
	for _, e := range entries {
		if e.kbID != "" {
			kbIDs[e.kbID] = true
		}
		if e.knowledgeID != "" {
			knowledgeIDs = append(knowledgeIDs, e.knowledgeID)
		}
	}
	kbNames := s.kbNames(ctx, kbIDs)
	numbers := s.documentNumbers(ctx, knowledgeIDs)

	out := make([]answerdocx.Reference, 0, len(entries))
	for _, e := range entries {
		e.ref.Source = kbNames[e.kbID]
		e.ref.DocumentNumber = numbers[e.knowledgeID]
		out = append(out, e.ref)
	}
	return out
}

func (s *AnswerExportService) kbNames(ctx context.Context, ids map[string]bool) map[string]string {
	names := map[string]string{}
	if s.kbs == nil || len(ids) == 0 {
		return names
	}
	list := make([]string, 0, len(ids))
	for id := range ids {
		list = append(list, id)
	}
	sort.Strings(list)
	kbs, err := s.kbs.GetKnowledgeBasesByIDsOnly(ctx, list)
	if err != nil {
		logger.Warnf(ctx, "answer export: knowledge base names unavailable: %v", err)
		return names
	}
	for _, kb := range kbs {
		if kb != nil {
			names[kb.ID] = strings.TrimSpace(kb.Name)
		}
	}
	return names
}

// documentNumbers maps knowledge IDs with an approved legal link to the số
// hiệu of their catalogue document. Best effort: failures leave it out.
func (s *AnswerExportService) documentNumbers(ctx context.Context, knowledgeIDs []string) map[string]string {
	out := map[string]string{}
	if s.reviews == nil || s.catalog == nil || len(knowledgeIDs) == 0 {
		return out
	}
	links, err := s.reviews.ApprovedLinksForKnowledge(ctx, knowledgeIDs)
	if err != nil || len(links) == 0 {
		if err != nil {
			logger.Warnf(ctx, "answer export: legal links unavailable: %v", err)
		}
		return out
	}
	docIDs := make([]string, 0, len(links))
	for _, l := range links {
		if l != nil && l.LegalDocumentID != "" {
			docIDs = append(docIDs, l.LegalDocumentID)
		}
	}
	sort.Strings(docIDs)
	docs, err := s.catalog.GetByIDs(ctx, docIDs)
	if err != nil {
		logger.Warnf(ctx, "answer export: legal catalogue unavailable: %v", err)
		return out
	}
	byID := map[string]string{}
	for _, d := range docs {
		if d != nil {
			byID[d.ID] = strings.TrimSpace(d.DocumentNumber)
		}
	}
	for kid, l := range links {
		if l != nil && byID[l.LegalDocumentID] != "" {
			out[kid] = byID[l.LegalDocumentID]
		}
	}
	return out
}

// IsAnswerExportRefusal reports an error Export returns for a message that
// is no finished answer (a bad request, not a missing one).
func IsAnswerExportRefusal(err error) bool {
	return stderrors.Is(err, apperrors.ErrAnswerExportNotAnswer) ||
		stderrors.Is(err, apperrors.ErrAnswerExportIncomplete) ||
		stderrors.Is(err, apperrors.ErrAnswerExportFailedTurn)
}
