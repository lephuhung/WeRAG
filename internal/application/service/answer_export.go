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

// maxExportReferences bounds the sources listed (distinct documents) of an
// answer whose citations name them.
const maxExportReferences = 100

// maxUncitedReferences bounds the sources of an answer without citation
// tags: then the list is everything retrieved, mostly not used.
const maxUncitedReferences = 10

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
		References: s.references(ctx, citedReferences(answer.Content, answer.KnowledgeReferences)),
	}
	if question != nil {
		in.Question = question.Content
	}
	data, err := answerdocx.Build(ctx, in)
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

// citedReferences keeps the references the answer cites with <kb/> and
// <web/> tags, in order of first citation: a chunk by its id, else a
// document by its title (in the cited knowledge base), a web page by its
// URL. Only references the caller may read are left by then, so a tag
// matching none of them is dropped (an answer citing only sources the
// caller lost lists none), except a web page, which is listed from its
// tag. Only an answer with no tags at all keeps the first
// maxUncitedReferences documents retrieved.
func citedReferences(content string, refs types.References) types.References {
	cites := answerdocx.Citations(content)
	if len(cites) == 0 {
		return firstDocuments(refs, maxUncitedReferences)
	}
	var out types.References
	for _, c := range cites {
		if r := matchCitation(c, refs); r != nil {
			out = append(out, r)
		} else if c.URL != "" {
			out = append(out, &types.SearchResult{ID: c.URL, KnowledgeID: c.URL, KnowledgeTitle: c.Title,
				ChunkType: string(types.ChunkTypeWebSearch), Metadata: map[string]string{"url": c.URL}})
		}
	}
	return out
}

func matchCitation(c answerdocx.Citation, refs types.References) *types.SearchResult {
	if c.URL != "" {
		for _, r := range refs {
			if r != nil && isWebSearchReference(r) && (r.Metadata["url"] == c.URL || r.KnowledgeID == c.URL || r.ID == c.URL) {
				return r
			}
		}
		return nil
	}
	if c.ChunkID != "" {
		for _, r := range refs {
			if r != nil && !isWebSearchReference(r) && (r.ID == c.ChunkID || r.ParentChunkID == c.ChunkID) {
				return r
			}
		}
	}
	if c.Doc != "" {
		for _, r := range refs {
			if r == nil || isWebSearchReference(r) || c.KBID != "" && r.KnowledgeBaseID != "" && r.KnowledgeBaseID != c.KBID {
				continue
			}
			if r.KnowledgeTitle == c.Doc || r.KnowledgeFilename == c.Doc {
				return r
			}
		}
	}
	return nil
}

// firstDocuments keeps the references of the first n distinct documents.
func firstDocuments(refs types.References, n int) types.References {
	var out types.References
	docs := map[string]bool{}
	for _, r := range refs {
		if r == nil {
			continue
		}
		key := r.KnowledgeID
		if key == "" {
			key = r.ID
		}
		if !docs[key] {
			if len(docs) == n {
				break
			}
			docs[key] = true
		}
		out = append(out, r)
	}
	return out
}

// references lists each source document once, in first-cited order, with
// its knowledge base and, for a reviewed legal document, its catalogue
// title and số hiệu; other titles are the file name without extension.
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
		e := &entry{}
		key := r.KnowledgeID
		if isWebSearchReference(r) {
			e.ref.Title = strings.TrimSpace(r.KnowledgeTitle) // a page title, not a file name
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
			e.ref.Title = answerdocx.DisplayTitle(r.KnowledgeTitle)
			if f := strings.TrimSpace(r.KnowledgeFilename); f != "" && answerdocx.DisplayTitle(f) != e.ref.Title && f != r.KnowledgeTitle {
				e.ref.FileName = f
			}
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
	legal := s.legalDocuments(ctx, knowledgeIDs)

	out := make([]answerdocx.Reference, 0, len(entries))
	// the first document of a catalogue entry takes its title; another
	// one on the same entry (a phụ lục, a second copy) keeps its own name
	titled := map[string]bool{}
	printed := map[answerdocx.Reference]bool{}
	for _, e := range entries {
		e.ref.Source = kbNames[e.kbID]
		if d := legal[e.knowledgeID]; d != nil {
			e.ref.DocumentNumber = strings.TrimSpace(d.DocumentNumber)
			if t := legalReferenceTitle(d); t != "" && !titled[d.ID] {
				titled[d.ID] = true
				e.ref.Title, e.ref.FileName = t, ""
			}
		}
		if printed[e.ref] {
			continue // the same line again (one văn bản held twice)
		}
		printed[e.ref] = true
		out = append(out, e.ref)
	}
	return out
}

// legalReferenceTitle names a catalogue document the way legal citations
// do: "Nghị định số 80/2021/NĐ-CP Quy định chi tiết …", "Luật Doanh nghiệp
// số 59/2020/QH14"; a catalogue title already holding the số hiệu is kept,
// and "" (no catalogue title) keeps the document's own.
func legalReferenceTitle(d *types.LegalDocument) string {
	title := strings.Join(strings.Fields(d.Title), " ")
	num := strings.TrimSpace(d.DocumentNumber)
	docType := strings.TrimSpace(d.DocType)
	switch {
	case num == "" || title == "":
		return title // "" keeps the document's own title
	case strings.Contains(title, num):
		return title
	case docType != "" && strings.HasPrefix(strings.ToLower(title), strings.ToLower(docType)):
		return title + " số " + num
	case docType != "":
		return docType + " số " + num + " " + title
	default:
		return num + " " + title
	}
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

// legalDocuments maps knowledge IDs with an approved legal link to their
// catalogue document. Best effort: failures leave it out.
func (s *AnswerExportService) legalDocuments(ctx context.Context, knowledgeIDs []string) map[string]*types.LegalDocument {
	out := map[string]*types.LegalDocument{}
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
	byID := map[string]*types.LegalDocument{}
	for _, d := range docs {
		if d != nil {
			byID[d.ID] = d
		}
	}
	for kid, l := range links {
		if l != nil && byID[l.LegalDocumentID] != nil {
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
