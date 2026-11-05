package service

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/vbpl"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// LegalPushService takes legal documents pushed by a source other than
// vbpl.vn in the "WeRAG legal HTML v1" format (docs/legal-document-feed-spec.md
// §9): it resolves the document and the documents acting on it in the
// legal catalogue, adds the full text when no public KB holds it, and turns
// every data-amend mark into a provision-level link candidate a SuperAdmin
// reviews before it reaches legal_provision_links.
type LegalPushService struct {
	db       *gorm.DB
	feed     *LegalFeedService
	kbs      interfaces.KnowledgeBaseService
	requests interfaces.LegalRequestRepository
}

// NewLegalPushService creates the push service.
func NewLegalPushService(db *gorm.DB, feed *LegalFeedService, kbs interfaces.KnowledgeBaseService,
	requests interfaces.LegalRequestRepository,
) *LegalPushService {
	return &LegalPushService{db: db, feed: feed, kbs: kbs, requests: requests}
}

// Push modes.
const (
	LegalPushAuto  = "auto"
	LegalPushLinks = "links"
	LegalPushFull  = "full"
)

// legalProvisionIDMax is the length of legal_provision_links.target_provision_id;
// a pushed element id is stored there as is (mode full).
const legalProvisionIDMax = 64

// LegalPushMaxBytes bounds a pushed file.
const LegalPushMaxBytes = 20 << 20

// legalPushFeedRe: a feed name is a short lower-case token.
var legalPushFeedRe = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,31}$`)

// LegalPushInput is one pushed document.
type LegalPushInput struct {
	Content  []byte
	FileName string
	// Metadata is the form field "metadata" (string → string, §9.2).
	Metadata map[string]string
	Mode     string
}

// LegalPushProblem is one mark or element the push could not use as is.
type LegalPushProblem struct {
	ElementID string `json:"element_id,omitempty"`
	Status    string `json:"status,omitempty"`
	DocNumber string `json:"doc_number,omitempty"`
	Reason    string `json:"reason"`
}

// LegalPushMark is the resolution of ONE mark, including the good outcomes.
// Problems only carry the ones needing attention, so a caller that wants to
// show every mark (the TVPL extension's link graph) needs this list: without
// it a matched mark is indistinguishable from one never resolved.
type LegalPushMark struct {
	ElementID string `json:"element_id"`
	DocNumber string `json:"doc_number,omitempty"`
	// Path is the mark's position inside B ("" when the note named none).
	Path   string `json:"path,omitempty"`
	Op     string `json:"op,omitempty"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

// LegalPushResult is the answer of a push.
type LegalPushResult struct {
	LegalDocumentID string `json:"legal_document_id"`
	DocumentNumber  string `json:"document_number"`
	Mode            string `json:"mode"`
	// KnowledgeID is A's full text in a public KB (held before or added now).
	KnowledgeID     string `json:"knowledge_id,omitempty"`
	KnowledgeBaseID string `json:"knowledge_base_id,omitempty"`
	// Ingest is created / replaced / unchanged in mode full, "" otherwise.
	Ingest string `json:"ingest,omitempty"`
	// Links counts the candidates by match status.
	Links map[string]int `json:"links"`
	// Pending is how many candidates wait for review.
	Pending int `json:"pending"`
	// KeptDecisions counts marks a SuperAdmin already approved or rejected.
	KeptDecisions int                `json:"kept_decisions"`
	Problems      []LegalPushProblem `json:"problems"`
	// MarkResults resolves every mark, in document order (see LegalPushMark).
	MarkResults []LegalPushMark `json:"mark_results"`
}

// legalPushMeta is the pushed metadata, read and checked.
type legalPushMeta struct {
	feed, feedDocID, feedURL string
	ref                      legalDocRef
	raw                      map[string]string
}

// legalDocRef names a catalogue document the way a feed can.
type legalDocRef struct {
	ID, Number, IssueDate, Issuer, DocType string
}

func (r legalDocRef) label() string {
	if r.Number != "" {
		return r.Number
	}
	return r.ID
}

func readLegalPushMeta(m map[string]string) (*legalPushMeta, error) {
	get := func(k string) string { return strings.TrimSpace(m[k]) }
	out := &legalPushMeta{feed: strings.ToLower(get("source")), feedDocID: get("source_doc_id"), feedURL: get("source_url"), raw: m}
	switch {
	case get("format") != vbpl.LegalHTMLFormatV1:
		return nil, apperrors.NewBadRequestError(fmt.Sprintf("metadata.format must be %q", vbpl.LegalHTMLFormatV1))
	case !legalPushFeedRe.MatchString(out.feed):
		return nil, apperrors.NewBadRequestError("metadata.source must be a short lower-case name such as \"tvpl\"")
	case out.feed == "vbpl" || out.feed == types.LegalProvisionLinkSourceEditor:
		return nil, apperrors.NewBadRequestError("metadata.source: vbpl.vn documents are ingested from the portal (legal_ingest_document)")
	case out.feedDocID == "" || len(out.feedDocID) > 64:
		return nil, apperrors.NewBadRequestError("metadata.source_doc_id is required (≤ 64 characters)")
	case out.feedURL == "":
		return nil, apperrors.NewBadRequestError("metadata.source_url is required")
	}
	out.ref = legalDocRef{ID: get("legal_document_id"), Number: get("doc_number"), IssueDate: get("issue_date"),
		Issuer: get("issuer"), DocType: get("doc_type")}
	if out.ref.ID == "" && out.ref.Number == "" {
		return nil, apperrors.NewBadRequestError("metadata.legal_document_id or metadata.doc_number is required")
	}
	return out, nil
}

// legalUnprocessable is a 422 with details.
func legalUnprocessable(msg string, details any) error {
	e := &apperrors.AppError{Code: apperrors.ErrValidation, Message: msg, HTTPCode: http.StatusUnprocessableEntity}
	if details != nil {
		return e.WithDetails(details)
	}
	return e
}

// legalDocCandidate is a catalogue row offered when a reference is ambiguous.
type legalDocCandidate struct {
	ID             string `json:"id"`
	DocumentNumber string `json:"document_number"`
	Title          string `json:"title"`
	Issuer         string `json:"issuer"`
	IssuedOn       string `json:"issued_on"`
}

func legalDocCandidates(docs []*types.LegalDocument) []legalDocCandidate {
	out := make([]legalDocCandidate, 0, len(docs))
	for _, d := range docs {
		c := legalDocCandidate{ID: d.ID, DocumentNumber: d.DocumentNumber, Title: d.Title, Issuer: d.Issuer}
		if d.IssuedOn != nil {
			c.IssuedOn = d.IssuedOn.Format("2006-01-02")
		}
		out = append(out, c)
	}
	return out
}

// resolveDoc finds the one catalogue document ref names: by id, else by
// số hiệu narrowed by issue date and issuer. Zero or several matches
// return the candidates and the reason; the số hiệu alone never decides
// between several documents.
func (s *LegalPushService) resolveDoc(ctx context.Context, ref legalDocRef) (*types.LegalDocument, []*types.LegalDocument, string, error) {
	db := s.db.WithContext(ctx)
	if ref.ID != "" {
		var d types.LegalDocument
		err := db.Where("id = ?", ref.ID).Limit(1).Find(&d).Error
		if err != nil {
			return nil, nil, "", err
		}
		if d.ID == "" {
			return nil, nil, "id " + ref.ID + " không có trong danh mục", nil
		}
		return &d, nil, "", nil
	}
	key := vietnamese_legal.CatalogNumberKey(ref.Number)
	if key == "" {
		return nil, nil, "số hiệu " + ref.Number + " không đọc được", nil
	}
	var docs []*types.LegalDocument
	if err := db.Where("number_key = ?", key).Order("issued_on DESC").Limit(50).Find(&docs).Error; err != nil {
		return nil, nil, "", err
	}
	if len(docs) == 0 {
		return nil, nil, "số hiệu " + ref.Number + " không có trong danh mục", nil
	}
	all := docs
	if ref.IssueDate != "" {
		day, err := time.Parse("2006-01-02", ref.IssueDate)
		if err != nil {
			return nil, all, "issue_date " + ref.IssueDate + " không đúng dạng YYYY-MM-DD", nil
		}
		docs = legalFilterDocs(docs, func(d *types.LegalDocument) bool {
			return d.IssuedOn != nil && d.IssuedOn.Format("2006-01-02") == day.Format("2006-01-02")
		})
	}
	if ref.Issuer != "" && len(docs) > 1 {
		want := strings.ToLower(ref.Issuer)
		docs = legalFilterDocs(docs, func(d *types.LegalDocument) bool {
			have := strings.ToLower(d.Issuer)
			return have != "" && (strings.Contains(have, want) || strings.Contains(want, have))
		})
	}
	switch len(docs) {
	case 1:
		return docs[0], nil, "", nil
	case 0:
		return nil, all, fmt.Sprintf("%d văn bản số hiệu %s, không văn bản nào khớp ngày ban hành/cơ quan", len(all), ref.Number), nil
	}
	return nil, docs, fmt.Sprintf("%d văn bản cùng số hiệu %s: cần issue_date hoặc issuer", len(docs), ref.Number), nil
}

func legalFilterDocs(docs []*types.LegalDocument, keep func(*types.LegalDocument) bool) []*types.LegalDocument {
	var out []*types.LegalDocument
	for _, d := range docs {
		if keep(d) {
			out = append(out, d)
		}
	}
	return out
}

// legalHeldDoc is a catalogue document's full text in a public KB.
type legalHeldDoc struct {
	KnowledgeID     string
	KnowledgeBaseID string
}

// heldDoc finds the public-KB copy of a catalogue document, nil when none.
func (s *LegalPushService) heldDoc(ctx context.Context, docID string) (*legalHeldDoc, error) {
	var rows []legalHeldDoc
	ext := legalJSONText(s.db, "k.metadata", "external_id")
	err := s.db.WithContext(ctx).Raw(`SELECT k.id AS knowledge_id, k.knowledge_base_id FROM knowledges k
		JOIN knowledge_bases kb ON kb.id = k.knowledge_base_id AND kb.owner_tenant_id = 0 AND kb.deleted_at IS NULL
		WHERE k.deleted_at IS NULL AND `+ext+` = ? ORDER BY k.created_at LIMIT 1`, docID).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

// legalHeldProvisions is what the chunks of a held document say about its
// provisions: path ("dieu:5/khoan:2") → vbpl provision id where the chunk
// records one, and the provisions it has at all.
type legalHeldProvisions struct {
	ids       map[string]string
	structure LegalDocStructure
}

func (h *legalHeldProvisions) has(path string) bool {
	if h == nil {
		return false
	}
	if _, ok := h.ids[path]; ok {
		return true
	}
	p, ok := legalPathRef(path)
	if !ok {
		return false
	}
	if h.structure.has(p) {
		return true
	}
	// a điểm directly under a Điều ("dieu:5/diem:a"): the chunks record no
	// such ref; the Điều being there is as far as they can tell
	if p.Khoan == "" && p.Diem != "" {
		return h.structure.has(LegalProvisionRef{Article: p.Article})
	}
	return false
}

// legalPathLevel is the level of a provision path: article, clause or item.
func legalPathLevel(path string) string {
	last := path[strings.LastIndex(path, "/")+1:]
	switch {
	case strings.HasPrefix(last, "diem:"):
		return "item"
	case strings.HasPrefix(last, "khoan:"):
		return "clause"
	case strings.HasPrefix(last, "dieu:"):
		return "article"
	}
	return ""
}

// heldProvisions reads the provisions of a held document from its chunks.
func (s *LegalPushService) heldProvisions(ctx context.Context, knowledgeID string) (*legalHeldProvisions, error) {
	var rows []struct{ Metadata []byte }
	if err := s.db.WithContext(ctx).Raw(`SELECT c.metadata FROM chunks c WHERE c.deleted_at IS NULL AND c.knowledge_id = ?`,
		knowledgeID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := &legalHeldProvisions{ids: map[string]string{}}
	var metas []*vietnamese_legal.LegalMetadata
	for _, r := range rows {
		var meta types.DocumentChunkMetadata
		if json.Unmarshal(r.Metadata, &meta) != nil || meta.Legal == nil {
			continue
		}
		metas = append(metas, meta.Legal)
		if len(meta.Legal.ArticleNos) != 1 {
			continue
		}
		article := "dieu:" + strings.ToLower(meta.Legal.ArticleNos[0])
		for key, id := range meta.Legal.ProvisionIDs {
			path := article
			if key != "dieu" {
				path += "/" + strings.ToLower(key)
			}
			if _, dup := out.ids[path]; !dup && id != "" {
				out.ids[path] = id
			}
		}
	}
	out.structure = legalStructureFrom(metas)
	return out, nil
}

// legalSyntheticProvisionID names a provision that has no vbpl id (a path
// of a document no KB holds, or one the chunks do not tag): stable, and
// short enough for legal_provision_links (varchar 64).
func legalSyntheticProvisionID(docID, path string) string {
	sum := sha1.Sum([]byte(docID + "#" + path))
	return "fp-" + hex.EncodeToString(sum[:])[:24]
}

func legalPushDate(s string) *time.Time {
	if s == "" {
		return nil
	}
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil
	}
	return &d
}

// Push takes one pushed document (§9.3). The caller is authorized by the
// handler (platform-tenant API key with ingest, or a human SuperAdmin);
// the KB write of mode full is authorized again here.
func (s *LegalPushService) Push(ctx context.Context, in LegalPushInput, caller types.Caller, actor LegalFeedActor) (*LegalPushResult, error) {
	if s == nil || s.db == nil {
		return nil, apperrors.NewServiceUnavailableError("legal push is not available")
	}
	if len(in.Content) == 0 {
		return nil, apperrors.NewBadRequestError("file is empty")
	}
	if len(in.Content) > LegalPushMaxBytes {
		return nil, apperrors.NewBadRequestError(fmt.Sprintf("file is larger than %d MB", LegalPushMaxBytes>>20))
	}
	meta, err := readLegalPushMeta(in.Metadata)
	if err != nil {
		return nil, err
	}
	mode := strings.ToLower(strings.TrimSpace(in.Mode))
	if mode == "" {
		mode = LegalPushAuto
	}
	if mode != LegalPushAuto && mode != LegalPushLinks && mode != LegalPushFull {
		return nil, apperrors.NewBadRequestError("mode must be auto, links or full")
	}

	// 1. A in the catalogue
	a, cands, reason, err := s.resolveDoc(ctx, meta.ref)
	if err != nil {
		return nil, fmt.Errorf("catalogue lookup: %w", err)
	}
	if a == nil {
		return nil, legalUnprocessable("không xác định được văn bản "+meta.ref.label()+": "+reason,
			map[string]any{"candidates": legalDocCandidates(cands)})
	}

	// 2. structure and marks of the pushed file
	doc, err := vbpl.ConvertBytes(in.Content)
	if err != nil || doc == nil || !doc.Structured || !vbpl.LooksLikeVBPL(in.Content) {
		return nil, legalUnprocessable("tệp không có cấu trúc Điều/khoản dùng được (lớp prov-* của chuẩn v1)", nil)
	}
	places := vbpl.ProvisionPlaces(doc)
	markup, err := vbpl.ReadAmendMarkup(in.Content)
	if err != nil {
		return nil, legalUnprocessable("không đọc được HTML: "+err.Error(), nil)
	}

	res := &LegalPushResult{LegalDocumentID: a.ID, DocumentNumber: a.DocumentNumber, Links: map[string]int{}, Problems: []LegalPushProblem{}}
	for _, p := range markup.Problems {
		res.Problems = append(res.Problems, LegalPushProblem{ElementID: p.ElementID, Reason: p.Reason})
	}

	// 3. where A's full text is
	held, err := s.heldDoc(ctx, a.ID)
	if err != nil {
		return nil, fmt.Errorf("held document lookup: %w", err)
	}
	switch {
	case mode == LegalPushAuto && held != nil:
		mode = LegalPushLinks
	case mode == LegalPushAuto:
		mode = LegalPushFull
	case mode == LegalPushFull && held != nil:
		return nil, apperrors.NewConflictError(fmt.Sprintf("văn bản %s (%s) đã có toàn văn trong kho %s (%s); chỉ nạp liên kết (mode links)",
			a.ID, a.DocumentNumber, held.KnowledgeBaseID, held.KnowledgeID))
	}
	res.Mode = mode

	var target *legalHeldProvisions
	if mode == LegalPushFull {
		kres, err := s.ingest(ctx, a, meta, in, caller, actor)
		if err != nil {
			return nil, err
		}
		res.KnowledgeID, res.KnowledgeBaseID, res.Ingest = kres.KnowledgeID, kres.KnowledgeBaseID, kres.Result
	} else if held != nil {
		res.KnowledgeID, res.KnowledgeBaseID = held.KnowledgeID, held.KnowledgeBaseID
		if target, err = s.heldProvisions(ctx, held.KnowledgeID); err != nil {
			return nil, fmt.Errorf("held provisions: %w", err)
		}
	}
	// links on an A no KB holds: positions are the pushed file's (target nil)

	// 4. one candidate per mark
	r := &legalPushResolver{s: s, ctx: ctx, docs: map[string]*legalResolvedDoc{}, provs: map[string]*legalHeldProvisions{}}
	var out []*types.LegalProvisionLinkCandidate
	for _, mp := range markup.Provisions {
		if len(mp.ElementID) > legalProvisionIDMax {
			res.Problems = append(res.Problems, LegalPushProblem{ElementID: legalCap(mp.ElementID, 160),
				Reason: fmt.Sprintf("id phần tử dài quá %d ký tự", legalProvisionIDMax)})
			continue
		}
		place, ok := places[mp.ElementID]
		if !ok {
			res.Problems = append(res.Problems, LegalPushProblem{ElementID: mp.ElementID,
				Reason: "data-amend trên phần tử không phải Điều/khoản/điểm (prov-article/clause/item) hoặc nằm trong phần trích dẫn"})
			continue
		}
		for _, mk := range mp.Marks {
			c, err := r.candidate(a, meta, mp.ElementID, place, mk, mode, target)
			if err != nil {
				return nil, err
			}
			out = append(out, c)
			res.MarkResults = append(res.MarkResults, LegalPushMark{
				ElementID: mp.ElementID, DocNumber: c.AmenderDocNumber, Path: c.SourcePath,
				Op: c.Op, Status: c.MatchStatus, Reason: c.MatchReason,
			})
			if c.MatchStatus != types.LegalLinkMatched && c.MatchStatus != types.LegalLinkDuplicate {
				res.Problems = append(res.Problems, LegalPushProblem{ElementID: mp.ElementID, Status: c.MatchStatus,
					DocNumber: c.AmenderDocNumber, Reason: c.MatchReason})
			}
		}
	}
	r.recordMissing(meta)

	// 5. store: pending candidates of this document are replaced; decided
	// ones stay and the same mark is not asked again
	kept, err := s.store(ctx, meta, a.ID, out)
	if err != nil {
		return nil, err
	}
	res.KeptDecisions = kept
	for _, c := range out {
		if c.ID == 0 {
			continue
		}
		res.Links[c.MatchStatus]++
		if c.ReviewStatus == types.LegalLinkReviewPending {
			res.Pending++
		}
	}
	WriteLegalFeedLog(ctx, s.db, actor, LegalFeedEntry{
		Action: "push_" + mode, VBPLID: a.ID, KnowledgeID: res.KnowledgeID, KnowledgeBaseID: res.KnowledgeBaseID,
		DocumentNumber: a.DocumentNumber, Reason: meta.feed + " " + meta.feedDocID,
		Detail: map[string]any{"feed": meta.feed, "feed_doc_id": meta.feedDocID, "feed_url": meta.feedURL,
			"links": res.Links, "pending": res.Pending, "kept_decisions": kept, "problems": len(res.Problems), "ingest": res.Ingest},
	})
	return res, nil
}

// ingest adds A's pushed full text to the public KB of its type.
func (s *LegalPushService) ingest(ctx context.Context, a *types.LegalDocument, meta *legalPushMeta, in LegalPushInput,
	caller types.Caller, actor LegalFeedActor,
) (*LegalIngestResult, error) {
	if s.feed == nil || s.requests == nil || s.kbs == nil {
		return nil, apperrors.NewServiceUnavailableError("knowledge ingest is not available")
	}
	if strings.HasPrefix(strings.TrimSpace(a.Status), LegalExpiredStatusPrefix) {
		return nil, apperrors.NewBadRequestError(fmt.Sprintf("văn bản %s (%s) \"%s\": văn bản hết hiệu lực toàn bộ chỉ giữ dòng danh mục, không nạp toàn văn",
			a.ID, a.DocumentNumber, a.Status))
	}
	kbs, err := s.requests.PublicKnowledgeBases(ctx)
	if err != nil {
		return nil, fmt.Errorf("public knowledge bases: %w", err)
	}
	kbRef, ok := LegalTypeTargets(kbs)[strings.TrimSpace(a.DocType)]
	if !ok {
		return nil, legalUnprocessable(fmt.Sprintf("không kho công khai nào nhận loại văn bản %q", a.DocType), nil)
	}
	if scope, isKey := types.TenantAPIKeyScopeFromContext(ctx); isKey && !scope.AllowsKnowledgeBase(kbRef.ID) {
		return nil, apperrors.NewForbiddenError("API key không được ghi vào kho " + kbRef.ID)
	}
	kb, grantCtx, err := publicKBWriteContext(ctx, s.kbs, kbRef.ID, caller)
	if err != nil {
		return nil, err
	}
	md := map[string]string{
		"external_id": a.ID, "source": meta.feed, "source_url": meta.feedURL, "source_doc_id": meta.feedDocID,
		"format": vbpl.LegalHTMLFormatV1, "doc_number": a.DocumentNumber, "doc_type": a.DocType, "eff_status": a.Status,
	}
	if a.IssuedOn != nil {
		md["issue_date"] = a.IssuedOn.Format("2006-01-02")
	}
	if a.EffectiveOn != nil {
		md["eff_from"] = a.EffectiveOn.Format("2006-01-02")
	}
	name := in.FileName
	if name == "" || !strings.HasSuffix(strings.ToLower(name), ".html") {
		name = strings.ReplaceAll(a.DocumentNumber+" - "+a.Title, "/", "-")
		if r := []rune(name); len(r) > 180 {
			name = string(r[:180])
		}
		name += ".html"
	}
	return s.feed.IngestHTML(grantCtx, LegalIngestDocument{
		VBPLID: a.ID, DocumentNumber: a.DocumentNumber, DocType: a.DocType, Status: a.Status, FileName: name,
		Metadata: md, SourceFormat: LegalSourceHTML, ReviewedBy: "feed:" + meta.feed,
		LogDetail: map[string]any{"feed": meta.feed, "feed_doc_id": meta.feedDocID, "feed_url": meta.feedURL},
	}, in.Content, kb, actor, "push "+meta.feed+" "+meta.feedDocID)
}

// legalResolvedDoc is a resolved amender B (or why it is not).
type legalResolvedDoc struct {
	doc    *types.LegalDocument
	reason string
	// missing: the catalogue has no document with the number at all
	missing bool
	ref     legalDocRef
}

// legalPushResolver resolves the amenders of one push, once each.
type legalPushResolver struct {
	s     *LegalPushService
	ctx   context.Context
	docs  map[string]*legalResolvedDoc
	provs map[string]*legalHeldProvisions // B id → provisions of its held copy (nil: none held)
}

func (r *legalPushResolver) amender(mk vbpl.AmendMark) (*legalResolvedDoc, error) {
	ref := legalDocRef{ID: mk.LegalDocumentID, Number: mk.DocNumber, IssueDate: mk.IssueDate, Issuer: mk.Issuer}
	key := ref.ID + "|" + vietnamese_legal.CatalogNumberKey(ref.Number) + "|" + ref.IssueDate + "|" + strings.ToLower(ref.Issuer)
	if d, ok := r.docs[key]; ok {
		return d, nil
	}
	doc, cands, reason, err := r.s.resolveDoc(r.ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("catalogue lookup: %w", err)
	}
	d := &legalResolvedDoc{doc: doc, reason: reason, ref: ref, missing: doc == nil && ref.ID == "" && len(cands) == 0}
	r.docs[key] = d
	return d, nil
}

func (r *legalPushResolver) provisions(docID string) (*legalHeldProvisions, error) {
	if p, ok := r.provs[docID]; ok {
		return p, nil
	}
	held, err := r.s.heldDoc(r.ctx, docID)
	if err != nil {
		return nil, fmt.Errorf("held document lookup: %w", err)
	}
	var p *legalHeldProvisions
	if held != nil {
		if p, err = r.s.heldProvisions(r.ctx, held.KnowledgeID); err != nil {
			return nil, fmt.Errorf("held provisions: %w", err)
		}
	}
	r.provs[docID] = p
	return p, nil
}

// candidate builds the candidate of one mark on A's element.
func (r *legalPushResolver) candidate(a *types.LegalDocument, meta *legalPushMeta, elementID string, place vbpl.ProvisionPlace,
	mk vbpl.AmendMark, mode string, target *legalHeldProvisions,
) (*types.LegalProvisionLinkCandidate, error) {
	c := &types.LegalProvisionLinkCandidate{
		Feed: meta.feed, FeedDocID: meta.feedDocID, FeedURL: meta.feedURL, TargetDocID: a.ID, TargetElementID: legalCap(elementID, 160),
		TargetLevel: place.Level, TargetPath: place.Path, RelationCode: vbpl.AmendOpCodes[mk.Op], Op: mk.Op,
		AmenderDocNumber: legalCap(mk.DocNumber, 128), AmenderIssueDate: legalCap(mk.IssueDate, 16), AmenderIssuer: legalCap(mk.Issuer, 255),
		SourcePath: legalCap(mk.Path, 160), EffFrom: legalPushDate(mk.EffFrom), EffTo: legalPushDate(mk.EffTo), Note: mk.Note,
		MatchStatus: types.LegalLinkMatched, ReviewStatus: types.LegalLinkReviewPending,
	}
	var reasons []string
	status := func(st, why string) {
		reasons = append(reasons, why)
		if legalMatchRank[st] > legalMatchRank[c.MatchStatus] {
			c.MatchStatus = st
		}
	}
	if (mk.EffFrom != "" && c.EffFrom == nil) || (mk.EffTo != "" && c.EffTo == nil) {
		reasons = append(reasons, "eff_from/eff_to không đúng dạng YYYY-MM-DD, bỏ qua")
	}

	// A's provision
	switch {
	case place.Path == "":
		status(types.LegalLinkTargetMissing, "phần tử thiếu số Điều/khoản/điểm, không xác định được vị trí trong văn bản")
	case mode == LegalPushFull:
		c.TargetProvisionID = elementID // the chunks of the ingested copy carry it
	case target == nil:
		c.TargetProvisionID = legalSyntheticProvisionID(a.ID, place.Path)
		reasons = append(reasons, "chưa có toàn văn trong kho: vị trí theo tệp đẩy lên")
	case target != nil && target.ids[place.Path] != "":
		c.TargetProvisionID = target.ids[place.Path]
	case target.has(place.Path):
		c.TargetProvisionID = legalSyntheticProvisionID(a.ID, place.Path)
	default:
		c.TargetProvisionID = legalSyntheticProvisionID(a.ID, place.Path)
		status(types.LegalLinkTargetMissing, "bản toàn văn đang có không có "+place.Path)
	}

	// B and its provision
	b, err := r.amender(mk)
	if err != nil {
		return nil, err
	}
	if b.doc == nil {
		status(types.LegalLinkUnresolved, b.reason)
		c.SourceProvisionID = legalSyntheticProvisionID("ref:"+b.ref.label(), mk.Path)
	} else {
		c.AmenderDocID = b.doc.ID
		if c.AmenderDocNumber == "" {
			c.AmenderDocNumber = legalCap(b.doc.DocumentNumber, 128)
		}
		c.SourceProvisionID = legalSyntheticProvisionID(b.doc.ID, mk.Path)
		switch {
		case b.doc.ID == a.ID:
			status(types.LegalLinkUnresolved, "văn bản tác động trùng chính văn bản này")
		case mk.Path == "":
			status(types.LegalLinkDocOnly, "thiếu path trong văn bản tác động")
		default:
			if _, ok := legalPathRef(mk.Path); !ok && !strings.HasPrefix(mk.Path, "phuluc/") {
				status(types.LegalLinkDocOnly, "path "+mk.Path+" không đúng dạng dieu:N/khoan:K/diem:D")
				break
			}
			provs, err := r.provisions(b.doc.ID)
			if err != nil {
				return nil, err
			}
			switch {
			case provs == nil:
				status(types.LegalLinkDocOnly, b.doc.DocumentNumber+" chưa có toàn văn trong kho công khai, chưa kiểm được "+mk.Path)
			case provs.ids[mk.Path] != "":
				c.SourceProvisionID = provs.ids[mk.Path]
			case provs.has(mk.Path):
				// held without provision ids: the path exists, its text is
				// read by path when the resolver learns to
			default:
				status(types.LegalLinkDocOnly, b.doc.DocumentNumber+" không có "+mk.Path)
			}
		}
	}

	if c.MatchStatus == types.LegalLinkMatched {
		dup, err := r.s.linkHeld(r.ctx, c)
		if err != nil {
			return nil, err
		}
		if dup {
			c.MatchStatus, c.ReviewStatus = types.LegalLinkDuplicate, types.LegalLinkReviewAuto
			reasons = append(reasons, "liên kết đã có trong legal_provision_links")
		}
	}
	c.MatchReason = strings.Join(reasons, "; ")
	// the mark as pushed: a SuperAdmin's corrections change the candidate,
	// the next push still recognizes the decided mark by this key
	if b, err := json.Marshal(map[string]string{"mark_key": legalCandidateKey(c)}); err == nil {
		c.Detail = types.JSON(b)
	}
	return c, nil
}

// legalMatchRank orders the match states, worst last.
var legalMatchRank = map[string]int{
	types.LegalLinkMatched: 0, types.LegalLinkDuplicate: 0, types.LegalLinkDocOnly: 1,
	types.LegalLinkTargetMissing: 2, types.LegalLinkUnresolved: 3,
}

func legalCap(s string, n int) string {
	if len(s) <= n {
		return s
	}
	r := []rune(s)
	for len(string(r)) > n {
		r = r[:len(r)-1]
	}
	return string(r)
}

// linkHeld reports whether legal_provision_links already holds c's link
// (any source): the same provisions and code, or the same A path, code
// and B.
func (s *LegalPushService) linkHeld(ctx context.Context, c *types.LegalProvisionLinkCandidate) (bool, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&types.LegalProvisionLink{}).
		Where("(target_provision_id = ? AND relation_code = ? AND source_provision_id = ?) OR "+
			"(target_doc_id = ? AND target_path = ? AND relation_code = ? AND source_doc_id = ? AND source_path = ?)",
			c.TargetProvisionID, c.RelationCode, c.SourceProvisionID,
			c.TargetDocID, c.TargetPath, c.RelationCode, c.AmenderDocID, c.SourcePath).
		Count(&n).Error
	return n > 0, err
}

// recordMissing files a legal document request for every amender the
// catalogue does not know at all, flagged with the feed as requester.
func (r *legalPushResolver) recordMissing(meta *legalPushMeta) {
	if r.s.requests == nil {
		return
	}
	keys := make([]string, 0, len(r.docs))
	for k := range r.docs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		d := r.docs[k]
		if !d.missing || d.ref.Number == "" {
			continue
		}
		nk := vietnamese_legal.CatalogNumberKey(d.ref.Number)
		if nk == "" {
			continue
		}
		now := time.Now()
		// Name the pushed document: operators picking the row up need to know
		// which A referenced B (several documents reference the same B).
		query := "văn bản tác động trong dữ liệu đẩy từ " + meta.feed
		if a := meta.ref.label(); a != "" {
			query += " (A: " + a + ")"
		}
		if _, err := r.s.requests.Record(r.ctx, &types.LegalDocumentRequest{
			NumberKey: nk, DocumentNumber: d.ref.Number, Source: legalCap("feed:"+meta.feed, 32), Status: types.LegalRequestRequested,
			FirstRequestedAt: now, LastRequestedAt: now, LastQuery: query,
		}); err != nil {
			logger.Warnf(r.ctx, "[LegalPush] request for %s: %v", d.ref.Number, err)
		}
	}
}

// legalCandidateKey identifies the same mark across pushes.
func legalCandidateKey(c *types.LegalProvisionLinkCandidate) string {
	return strings.Join([]string{c.TargetPath, fmt.Sprint(c.RelationCode), vietnamese_legal.CatalogNumberKey(c.AmenderDocNumber),
		c.AmenderDocID, c.SourcePath}, "|")
}

// legalMarkKey is the key of the mark a candidate was pushed as.
func legalMarkKey(c *types.LegalProvisionLinkCandidate) string {
	var d struct {
		MarkKey string `json:"mark_key"`
	}
	if len(c.Detail) > 0 && json.Unmarshal(c.Detail, &d) == nil && d.MarkKey != "" {
		return d.MarkKey
	}
	return legalCandidateKey(c)
}

// store replaces the pending candidates of the pushed document; a mark a
// SuperAdmin already decided is not stored again (counted as kept).
func (s *LegalPushService) store(ctx context.Context, meta *legalPushMeta, aID string, list []*types.LegalProvisionLinkCandidate) (int, error) {
	kept := 0
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("feed = ? AND feed_doc_id = ? AND review_status IN ?", meta.feed, meta.feedDocID,
			[]string{types.LegalLinkReviewPending, types.LegalLinkReviewAuto}).
			Delete(&types.LegalProvisionLinkCandidate{}).Error; err != nil {
			return err
		}
		var decided []*types.LegalProvisionLinkCandidate
		if err := tx.Where("feed = ? AND feed_doc_id = ? AND review_status IN ?", meta.feed, meta.feedDocID,
			[]string{types.LegalLinkReviewApproved, types.LegalLinkReviewRejected}).Find(&decided).Error; err != nil {
			return err
		}
		done := map[string]bool{}
		for _, d := range decided {
			done[legalMarkKey(d)] = true
		}
		seen := map[string]bool{}
		var fresh []*types.LegalProvisionLinkCandidate
		for _, c := range list {
			k := legalCandidateKey(c) + "|" + c.TargetElementID
			if done[legalCandidateKey(c)] {
				kept++
				continue
			}
			if seen[k] {
				continue
			}
			seen[k] = true
			fresh = append(fresh, c)
		}
		if len(fresh) == 0 {
			return nil
		}
		return tx.CreateInBatches(fresh, 200).Error
	})
	if err != nil {
		return 0, fmt.Errorf("store candidates of %s: %w", aID, err)
	}
	return kept, nil
}
