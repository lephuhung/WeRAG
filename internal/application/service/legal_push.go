package service

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"
	"gorm.io/gorm"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/legalcatalog"
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
	settings interface {
		GetBool(ctx context.Context, key string, envName string, def bool) bool
	}
	// Notices tells the SuperAdmins about the links a push leaves pending
	// (nil: nobody is told).
	Notices *SuperAdminQueueNotifier
}

// LegalFeedAutoApproveSetting: a pushed mark matched at both ends (A's
// provision and B's) is approved at once instead of waiting for a SuperAdmin.
const LegalFeedAutoApproveSetting = "legal.feed.auto_approve_matched"

// legalFeedAutoApproveDefault is the setting's value when unset.
const legalFeedAutoApproveDefault = true

// NewLegalPushService creates the push service.
func NewLegalPushService(db *gorm.DB, feed *LegalFeedService, kbs interfaces.KnowledgeBaseService,
	requests interfaces.LegalRequestRepository, settings interfaces.SystemSettingService,
) *LegalPushService {
	return &LegalPushService{db: db, feed: feed, kbs: kbs, requests: requests, settings: settings}
}

// Push modes.
const (
	LegalPushAuto  = "auto"
	LegalPushLinks = "links"
	LegalPushFull  = "full"
	// LegalPushCatalogue adds the catalogue row only: for a document the
	// source lists without a full text (the file may be empty).
	LegalPushCatalogue = "catalogue"
)

// legalProvisionIDMax is the length of legal_provision_links.target_provision_id
// and of the candidates' target_element_id check.
const legalProvisionIDMax = 64

// LegalPushMaxBytes bounds a pushed file.
const LegalPushMaxBytes = 20 << 20

// legalPushMinTextRunes is the least text a pushed file without Điều must
// hold: less is a placeholder page, not a document.
const legalPushMinTextRunes = 200

const legalNoStructureMsg = "tệp không có cấu trúc Điều/khoản dùng được (lớp prov-* của chuẩn v1)"

var (
	legalHTMLDropRe  = regexp.MustCompile(`(?is)<(script|style|head)[^>]*>.*?</(script|style|head)>`)
	legalHTMLBlockRe = regexp.MustCompile(`(?i)</?(p|div|br|li|tr|td|th|h[1-6]|table|section|article)\b[^>]*>`)
	legalHTMLTagRe   = regexp.MustCompile(`(?s)<[^>]+>`)
	// legalAppendixOpenRe: the element opening the appendices
	legalAppendixOpenRe = regexp.MustCompile(`<[a-zA-Z]+[^>]*\sclass="[^"]*\bdoc-region-appendix\b[^"]*"`)
	// legalArticleLineRe: a line starting "Điều 5." / "Điều 5:"
	legalArticleLineRe = regexp.MustCompile(`(?m)^[\s\p{Zs}]*(?:Điều|ĐIỀU)[\s\p{Zs}]+\d+[a-zđ]?[\s\p{Zs}]*[.:]`)
)

// legalHTMLText is an HTML file's text, one line per block element.
func legalHTMLText(content []byte) string {
	s := legalHTMLDropRe.ReplaceAllString(string(content), " ")
	s = legalHTMLBlockRe.ReplaceAllString(s, "\n")
	return norm.NFC.String(html.UnescapeString(legalHTMLTagRe.ReplaceAllString(s, "")))
}

// legalUnmarkedArticles counts the lines of a file's body that start a
// Điều; the appendices (doc-region-appendix, at the end) are left out: a
// biểu mẫu there holds a sample decision with its own "Điều 1.".
func legalUnmarkedArticles(content []byte) int {
	if loc := legalAppendixOpenRe.FindIndex(content); loc != nil {
		content = content[:loc[0]]
	}
	return len(legalArticleLineRe.FindAllStringIndex(legalHTMLText(content), -1))
}

// legalHTMLTextRunes counts the letters and digits of an HTML file's text.
func legalHTMLTextRunes(content []byte) int {
	n := 0
	for _, r := range legalHTMLText(content) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			n++
		}
	}
	return n
}

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
	// AutoApproved counts the matched marks approved without a person.
	AutoApproved int `json:"auto_approved"`
	// KeptDecisions counts marks a SuperAdmin already approved or rejected.
	KeptDecisions int                `json:"kept_decisions"`
	Problems      []LegalPushProblem `json:"problems"`
	// MarkResults resolves every mark, in document order (see LegalPushMark).
	MarkResults []LegalPushMark `json:"mark_results"`
	// CatalogueCreated: the catalogue had no row for the document, the push
	// created a provisional feed row (source = feed, waiting for a SuperAdmin;
	// a later vbpl.vn import of the same document replaces it).
	CatalogueCreated bool `json:"catalogue_created,omitempty"`
	// FullTextRequested: mode catalogue with request_full_text queued the
	// document in the legal request queue (the TVPL extension's work list).
	FullTextRequested bool `json:"full_text_requested,omitempty"`
}

// legalPushMeta is the pushed metadata, read and checked.
type legalPushMeta struct {
	feed, feedDocID, feedURL string
	ref                      legalDocRef
	// title, effFrom (YYYY-MM-DD) and effStatus describe the document for a
	// catalogue row the push creates; unused when the catalogue has one.
	title, effFrom, effStatus string
	raw                       map[string]string
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
	case out.feed == "vbpl" || out.feed == types.LegalProvisionLinkSourceEditor || out.feed == types.LegalDocumentSourceManual:
		return nil, apperrors.NewBadRequestError("metadata.source: vbpl.vn documents are ingested from the portal (legal_ingest_document)")
	case out.feedDocID == "" || len(out.feedDocID) > 64:
		return nil, apperrors.NewBadRequestError("metadata.source_doc_id is required (≤ 64 characters)")
	case out.feedURL == "":
		return nil, apperrors.NewBadRequestError("metadata.source_url is required")
	}
	out.ref = legalDocRef{ID: get("legal_document_id"), Number: get("doc_number"), IssueDate: get("issue_date"),
		Issuer: get("issuer"), DocType: get("doc_type")}
	out.title, out.effFrom, out.effStatus = get("title"), get("eff_from"), get("eff_status")
	if out.ref.ID == "" && out.ref.Number == "" {
		return nil, apperrors.NewBadRequestError("metadata.legal_document_id or metadata.doc_number is required")
	}
	// the Party numbers its documents again every term: its số hiệu names a
	// document only together with the day
	if out.ref.ID == "" && out.ref.IssueDate == "" && vietnamese_legal.IsPartyDocumentNumber(out.ref.Number) {
		return nil, apperrors.NewBadRequestError("metadata.issue_date is required for a Party document (" + out.ref.Number +
			"): its number is reused every term")
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
		// an id sent with a số hiệu must be that document's (the TVPL
		// extension sends its request's row with the number of the page it read)
		if ref.Number != "" && vietnamese_legal.CatalogNumberKey(ref.Number) != d.NumberKey {
			return nil, nil, fmt.Sprintf("id %s là văn bản %s, không phải %s", ref.ID, d.DocumentNumber, ref.Number), nil
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
	return legalcatalog.SyntheticProvisionID(docID, path)
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
	mode := strings.ToLower(strings.TrimSpace(in.Mode))
	if mode == "" {
		mode = LegalPushAuto
	}
	if mode != LegalPushAuto && mode != LegalPushLinks && mode != LegalPushFull && mode != LegalPushCatalogue {
		return nil, apperrors.NewBadRequestError("mode must be auto, links, full or catalogue")
	}
	if len(in.Content) == 0 && mode != LegalPushCatalogue {
		return nil, apperrors.NewBadRequestError("file is empty")
	}
	if len(in.Content) > LegalPushMaxBytes {
		return nil, apperrors.NewBadRequestError(fmt.Sprintf("file is larger than %d MB", LegalPushMaxBytes>>20))
	}
	meta, err := readLegalPushMeta(in.Metadata)
	if err != nil {
		return nil, err
	}

	// 1. A in the catalogue; the số hiệu is the key shared with vbpl.vn,
	// SameDocument tells the documents of one number apart
	a, cands, reason, err := s.resolveDoc(ctx, meta.ref)
	if err != nil {
		return nil, fmt.Errorf("catalogue lookup: %w", err)
	}
	var feedRow *types.LegalDocument // a catalogue row to create (mode full only)
	if a == nil {
		var why string
		a, feedRow, why = s.resolveByNumber(meta, in.Content, mode, cands)
		if a == nil && feedRow == nil {
			return nil, legalUnprocessable("không xác định được văn bản "+meta.ref.label()+": "+reason+why,
				map[string]any{"candidates": legalDocCandidates(cands)})
		}
		if feedRow != nil {
			a = feedRow
		}
	}
	if mode == LegalPushCatalogue {
		return s.pushCatalogueRow(ctx, a, feedRow, meta, actor)
	}

	// 2. structure and marks of the pushed file. A document without Điều
	// (Kết luận, Thông báo, Hướng dẫn…) carries no marks: only its full text
	// is taken, read by the regular parser.
	markup, err := vbpl.ReadAmendMarkup(in.Content)
	if err != nil {
		return nil, legalUnprocessable("không đọc được HTML: "+err.Error(), nil)
	}
	doc, err := vbpl.ConvertBytes(in.Content)
	structured := err == nil && doc != nil && doc.Structured && vbpl.LooksLikeVBPL(in.Content)
	places := map[string]vbpl.ProvisionPlace{}
	switch {
	case structured:
		places = vbpl.ProvisionPlaces(doc)
	case vbpl.LooksLikeVBPL(in.Content) || len(markup.Provisions) > 0:
		return nil, legalUnprocessable(legalNoStructureMsg, nil)
	case legalUnmarkedArticles(in.Content) >= 2:
		// one chunk per Điều needs the markup: unmarked "Điều 1." lines are
		// the pusher's mistake, not a document without Điều
		return nil, legalUnprocessable("tệp có các dòng \"Điều N.\" nhưng không đánh dấu prov-article: văn bản có Điều phải theo cấu trúc của chuẩn v1", nil)
	case legalHTMLTextRunes(in.Content) < legalPushMinTextRunes:
		return nil, legalUnprocessable(fmt.Sprintf("tệp gần như không có chữ (dưới %d ký tự)", legalPushMinTextRunes), nil)
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
	case feedRow != nil:
		mode = LegalPushFull // a new row: nothing holds the document
	case mode == LegalPushAuto && held != nil:
		mode = LegalPushLinks
	case mode == LegalPushAuto:
		mode = LegalPushFull
	case mode == LegalPushFull && held != nil:
		return nil, apperrors.NewConflictError(fmt.Sprintf("văn bản %s (%s) đã có toàn văn trong kho %s (%s); chỉ nạp liên kết (mode links)",
			a.ID, a.DocumentNumber, held.KnowledgeBaseID, held.KnowledgeID))
	}
	if !structured && mode != LegalPushFull {
		// a file without structure only brings a full text: as links it
		// would replace the source's earlier marks of the document with none
		return nil, legalUnprocessable(legalNoStructureMsg+"; tệp không có Điều/khoản chỉ được nạp toàn văn khi chưa kho nào giữ văn bản", nil)
	}
	res.Mode = mode

	var target *legalHeldProvisions
	if mode == LegalPushFull {
		kres, created, err := s.ingest(ctx, a, feedRow != nil, meta, in, caller, actor)
		if err != nil {
			return nil, err
		}
		res.KnowledgeID, res.KnowledgeBaseID, res.Ingest = kres.KnowledgeID, kres.KnowledgeBaseID, kres.Result
		res.CatalogueCreated = created
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
	wasPending := s.pendingCandidateKeys(ctx, meta)
	kept, err := s.store(ctx, meta, a.ID, out)
	if err != nil {
		return nil, err
	}
	res.KeptDecisions = kept
	var newKeys []string
	for _, c := range out {
		if c.ID == 0 {
			continue
		}
		res.Links[c.MatchStatus]++
		switch c.ReviewStatus {
		case types.LegalLinkReviewPending:
			res.Pending++
			if k := LegalLinkNoticeKey(c); !wasPending[k] {
				newKeys = append(newKeys, k)
			}
		case types.LegalLinkReviewApproved:
			res.AutoApproved++
		}
	}
	// the links newly left to a person (not auto-approved, not pending
	// already before this push: a re-push of an unchanged document adds
	// none) go to the SuperAdmins' digest; the replaced pending candidates
	// may have emptied the queue
	if len(newKeys) > 0 {
		ref := strings.TrimSpace(a.DocumentNumber)
		if ref == "" {
			ref = a.ID
		}
		s.Notices.LegalLinksPending(ctx, actor.UserID, ref, meta.feed, meta.feedDocID, newKeys)
	} else {
		s.Notices.LegalLinksChanged(ctx)
	}
	WriteLegalFeedLog(ctx, s.db, actor, LegalFeedEntry{
		Action: "push_" + mode, VBPLID: a.ID, KnowledgeID: res.KnowledgeID, KnowledgeBaseID: res.KnowledgeBaseID,
		DocumentNumber: a.DocumentNumber, Reason: meta.feed + " " + meta.feedDocID,
		Detail: map[string]any{"feed": meta.feed, "feed_doc_id": meta.feedDocID, "feed_url": meta.feedURL,
			"links": res.Links, "pending": res.Pending, "auto_approved": res.AutoApproved, "kept_decisions": kept, "problems": len(res.Problems), "ingest": res.Ingest,
			"catalogue_created": res.CatalogueCreated},
	})
	// links other documents' pushes named a by its số hiệu before the
	// catalogue knew it can be resolved now
	s.recheckAfterPush(ctx, a)
	return res, nil
}

// pushCatalogueRow is mode catalogue: the row of a document the source
// lists without a full text. A row the catalogue already holds is left as
// it is (its values are vbpl.vn's or a reviewer's).
func (s *LegalPushService) pushCatalogueRow(ctx context.Context, a, feedRow *types.LegalDocument, meta *legalPushMeta,
	actor LegalFeedActor,
) (*LegalPushResult, error) {
	res := &LegalPushResult{LegalDocumentID: a.ID, DocumentNumber: a.DocumentNumber, Mode: LegalPushCatalogue,
		Links: map[string]int{}, Problems: []LegalPushProblem{}}
	if feedRow != nil {
		if strings.HasPrefix(strings.TrimSpace(feedRow.Status), LegalExpiredStatusPrefix) {
			return nil, apperrors.NewBadRequestError(fmt.Sprintf("văn bản %s \"%s\" theo nguồn %s: văn bản hết hiệu lực toàn bộ "+
				"không tạo dòng danh mục mới", feedRow.DocumentNumber, feedRow.Status, feedRow.Source))
		}
		created, err := s.insertFeedRow(ctx, feedRow)
		if err != nil {
			return nil, err
		}
		res.CatalogueCreated = created
	}
	held, err := s.heldDoc(ctx, a.ID)
	if err != nil {
		return nil, fmt.Errorf("held document lookup: %w", err)
	}
	if held != nil {
		res.KnowledgeID, res.KnowledgeBaseID = held.KnowledgeID, held.KnowledgeBaseID
	} else if strings.EqualFold(strings.TrimSpace(meta.raw["request_full_text"]), "true") {
		if res.FullTextRequested, err = s.requestFullText(ctx, a, meta.feed); err != nil {
			return nil, err
		}
	}
	WriteLegalFeedLog(ctx, s.db, actor, LegalFeedEntry{
		Action: "push_" + LegalPushCatalogue, VBPLID: a.ID, DocumentNumber: a.DocumentNumber,
		Reason: meta.feed + " " + meta.feedDocID,
		Detail: map[string]any{"feed": meta.feed, "feed_doc_id": meta.feedDocID, "feed_url": meta.feedURL,
			"catalogue_created": res.CatalogueCreated, "full_text_requested": res.FullTextRequested},
	})
	s.recheckAfterPush(ctx, a)
	return res, nil
}

// legalFeedNoTextSource is the source of a request a feed queued for a
// document it lists without a usable full text ("dangcongsan_no_text").
func legalFeedNoTextSource(feed string) string { return legalCap(feed, 24) + "_no_text" }

// requestFullText queues a document no KB holds in the legal request queue,
// like the nightly import's vbpl_no_text rows (request_count 0: nobody
// asked yet), unless an open request of its number already waits for it.
func (s *LegalPushService) requestFullText(ctx context.Context, a *types.LegalDocument, feed string) (bool, error) {
	if strings.HasPrefix(strings.TrimSpace(a.Status), LegalExpiredStatusPrefix) {
		return false, nil // a fully expired document never gets a full text
	}
	db := s.db.WithContext(ctx)
	// any request of the number naming no document (open or decided, as the
	// nightly import), or one of this document (ON CONFLICT below)
	var asked int64
	if err := db.Model(&types.LegalDocumentRequest{}).
		Where("number_key = ? AND legal_document_id = ''", a.NumberKey).Count(&asked).Error; err != nil {
		return false, fmt.Errorf("requests of the number: %w", err)
	}
	if asked > 0 {
		return false, nil
	}
	// an OCR draft of the document in progress or under review: a TVPL copy
	// would race it
	var drafts int64
	if err := db.Model(&types.LegalDocumentDraft{}).
		Where("legal_document_id = ? AND status <> ?", a.ID, types.LegalDraftFailed).Count(&drafts).Error; err != nil {
		return false, fmt.Errorf("drafts of the document: %w", err)
	}
	if drafts > 0 {
		return false, nil
	}
	now := time.Now()
	// Raw: gorm would turn the zero request_count into its default 1.
	res := db.Exec(`INSERT INTO legal_document_requests
		(id, number_key, document_number, doc_type, title, legal_document_id, knowledge_base_id, source, status,
		 request_count, first_requested_at, last_requested_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, '', ?, ?, 0, ?, ?, ?, ?)
		ON CONFLICT (number_key, legal_document_id) DO NOTHING`,
		uuid.NewString(), a.NumberKey, a.DocumentNumber, a.DocType, a.Title, a.ID, legalFeedNoTextSource(feed),
		types.LegalRequestRequested, now, now, now, now)
	if res.Error != nil {
		return false, fmt.Errorf("queue request: %w", res.Error)
	}
	return res.RowsAffected > 0, nil
}

// ingest adds A's pushed full text to the public KB of its type. With
// create, a is a feed row the catalogue does not hold yet: it is inserted
// once every check passed and removed again when the ingest fails, so a
// refused push leaves no row behind. created reports the insert.
func (s *LegalPushService) ingest(ctx context.Context, a *types.LegalDocument, create bool, meta *legalPushMeta, in LegalPushInput,
	caller types.Caller, actor LegalFeedActor,
) (res *LegalIngestResult, created bool, err error) {
	if s.feed == nil || s.requests == nil || s.kbs == nil {
		return nil, false, apperrors.NewServiceUnavailableError("knowledge ingest is not available")
	}
	if strings.HasPrefix(strings.TrimSpace(a.Status), LegalExpiredStatusPrefix) {
		if create {
			return nil, false, apperrors.NewBadRequestError(fmt.Sprintf("văn bản %s \"%s\" theo nguồn %s: văn bản hết hiệu lực toàn bộ "+
				"không được nạp toàn văn, cũng không tạo dòng danh mục mới", a.DocumentNumber, a.Status, a.Source))
		}
		return nil, false, apperrors.NewBadRequestError(fmt.Sprintf("văn bản %s (%s) \"%s\": văn bản hết hiệu lực toàn bộ chỉ giữ dòng danh mục, không nạp toàn văn",
			a.ID, a.DocumentNumber, a.Status))
	}
	kbs, err := s.requests.PublicKnowledgeBases(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("public knowledge bases: %w", err)
	}
	kbRef, err := legalPushTarget(kbs, meta.feed, a.DocumentNumber, a.DocType)
	if err != nil {
		return nil, false, err
	}
	if scope, isKey := types.TenantAPIKeyScopeFromContext(ctx); isKey && !scope.AllowsKnowledgeBase(kbRef.ID) {
		return nil, false, apperrors.NewForbiddenError("API key không được ghi vào kho " + kbRef.ID)
	}
	kb, grantCtx, err := publicKBWriteContext(ctx, s.kbs, kbRef.ID, caller)
	if err != nil {
		return nil, false, err
	}
	if create {
		if created, err = s.insertFeedRow(ctx, a); err != nil {
			return nil, false, err
		}
		if created {
			defer func() {
				if err != nil {
					s.dropFeedRow(ctx, a)
					created = false
				}
			}()
		}
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
	// Named like a vbpl copy: the title prefixes every chunk in the keyword
	// index, so a bare "15-2026-NĐ-CP.html" left the copy hard to find. A
	// vbpl row keeps vbpl's title; a feed row takes the vbpl-style title
	// written from the document's enacting clause (vbpl.OfficialTitle), and
	// once the copy is in, the row takes it too (unless a SuperAdmin
	// reviewed the row by hand: its title stays, the copy is still named
	// from the document). Only a row without a copy
	// gets here (mode full); copies made before this keep their names until
	// renamed and re-indexed by hand.
	title := a.Title
	if a.IsFeedRow() {
		if t := vbpl.OfficialTitle(in.Content, a.DocType, a.DocumentNumber); t != "" {
			title = t
		}
	}
	name := vbpl.FileNameOf(a.DocumentNumber, title, a.ID)
	res, err = s.feed.IngestHTML(grantCtx, LegalIngestDocument{
		VBPLID: a.ID, DocumentNumber: a.DocumentNumber, DocType: a.DocType, Status: a.Status, FileName: name,
		Metadata: md, SourceFormat: LegalSourceHTML, ReviewedBy: LegalFeedReviewerPrefix + meta.feed,
		LogDetail: map[string]any{"feed": meta.feed, "feed_doc_id": meta.feedDocID, "feed_url": meta.feedURL,
			"catalogue_created": created},
	}, in.Content, kb, actor, "push "+meta.feed+" "+meta.feedDocID)
	if err == nil && title != a.Title {
		s.retitleFeedRow(ctx, a, title)
	}
	return res, created, err
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
		// the ingested copy is a feed's text: its chunks carry no portal
		// ids (processVBPLFile), the provision is named by its path
		c.TargetProvisionID = legalSyntheticProvisionID(a.ID, place.Path)
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

// LegalLinkNoticeKey names one pending link of a pushed document for the
// SuperAdmins' digest across pushes (the rows are recreated with new ids on
// every push) and adoptions (which rewrite amender_doc_id): the element and
// path it is on, the relation, the amending số hiệu and its provision.
func LegalLinkNoticeKey(c *types.LegalProvisionLinkCandidate) string {
	return strings.Join([]string{c.TargetElementID, c.TargetPath, fmt.Sprint(c.RelationCode),
		vietnamese_legal.CatalogNumberKey(c.AmenderDocNumber), c.SourcePath}, "|")
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

// pendingCandidateKeys are the keys (candidate key + element) of the
// pushed document's candidates waiting for review before this push; none
// when they cannot be read (the push then counts all its pending links).
func (s *LegalPushService) pendingCandidateKeys(ctx context.Context, meta *legalPushMeta) map[string]bool {
	var rows []*types.LegalProvisionLinkCandidate
	if err := s.db.WithContext(ctx).Where("feed = ? AND feed_doc_id = ? AND review_status = ?", meta.feed, meta.feedDocID,
		types.LegalLinkReviewPending).Find(&rows).Error; err != nil {
		logger.Warnf(ctx, "[LegalPush] pending candidates of %s %s: %v", meta.feed, meta.feedDocID, err)
		return nil
	}
	keys := make(map[string]bool, len(rows))
	for _, c := range rows {
		keys[LegalLinkNoticeKey(c)] = true
	}
	return keys
}

// store replaces the pending candidates of the pushed document; a mark a
// SuperAdmin already decided is not stored again (counted as kept).
func (s *LegalPushService) store(ctx context.Context, meta *legalPushMeta, aID string, list []*types.LegalProvisionLinkCandidate) (int, error) {
	kept := 0
	auto := s.settings != nil && s.settings.GetBool(ctx, LegalFeedAutoApproveSetting, "", legalFeedAutoApproveDefault)
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
		pushed := make(map[string]bool, len(list))
		for _, c := range list {
			pushed[legalCandidateKey(c)] = true
		}
		done := map[string]bool{}
		for _, d := range decided {
			// A link nobody reviewed lasts as long as the source says it: the
			// mark is gone from the document, so the link goes too.
			if d.ReviewedBy == types.LegalLinkAutoReviewer && !pushed[legalMarkKey(d)] {
				if err := revokeAutoApproved(tx, d, meta.feed); err != nil {
					return err
				}
				continue
			}
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
		if err := tx.CreateInBatches(fresh, 200).Error; err != nil {
			return err
		}
		if !auto {
			return nil
		}
		now := time.Now()
		for _, c := range fresh {
			if legalCandidateAutoApprovable(c) {
				if err := approveCandidateTx(tx, c, types.LegalLinkAutoReviewer, now); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("store candidates of %s: %w", aID, err)
	}
	return kept, nil
}

// retitleFeedRow gives a feed row its vbpl-style title (vbpl.OfficialTitle)
// when a push of it brings the enacting clause. Rows of vbpl.vn and feed
// rows a SuperAdmin reviewed by hand are never touched. A failure only
// leaves the old title.
func (s *LegalPushService) retitleFeedRow(ctx context.Context, a *types.LegalDocument, title string) {
	res := s.db.WithContext(ctx).Model(&types.LegalDocument{}).
		Where("id = ? AND source = ?", a.ID, a.Source).
		Where("reviewed_by IS NULL OR reviewed_by = '' OR reviewed_by LIKE ?", LegalFeedReviewerPrefix+"%").
		Update("title", title)
	if res.Error != nil {
		logger.Warnf(ctx, "[LegalPush] retitle %s: %v", a.ID, res.Error)
		return
	}
	if res.RowsAffected > 0 {
		logger.Infof(ctx, "[LegalPush] %s (%s) retitled %q → %q", a.ID, a.DocumentNumber, a.Title, title)
		a.Title = title
	}
}
