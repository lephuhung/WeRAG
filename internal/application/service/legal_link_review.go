package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// The link review page groups the pushed candidates by the document they
// act on (A) and draws each A as a diagram: A's provisions on one side, the
// amending documents (B) and their provisions on the other. This file holds
// what that page needs beyond the candidate list: the documents with their
// counts, a provision's wording, a held document's provisions, the recheck
// of candidates whose B has since entered the catalogue, and the batch
// decision.

// LegalLinkAmenderRequest is the legal document request of an amender the
// catalogue does not know yet.
type LegalLinkAmenderRequest struct {
	ID             string `json:"id"`
	DocumentNumber string `json:"document_number"`
	Status         string `json:"status"`
}

// amenderRequests finds, per number key, the request of every unknown
// amender in list: an open one first, else the latest. A failure only
// leaves the requests out (the page still works without them).
func (s *LegalPushService) amenderRequests(ctx context.Context, list []*types.LegalProvisionLinkCandidate) map[string]*LegalLinkAmenderRequest {
	keys := map[string]bool{}
	for _, c := range list {
		if c.AmenderDocID == "" {
			if k := vietnamese_legal.CatalogNumberKey(c.AmenderDocNumber); k != "" {
				keys[k] = true
			}
		}
	}
	out := map[string]*LegalLinkAmenderRequest{}
	if len(keys) == 0 {
		return out
	}
	want := make([]string, 0, len(keys))
	for k := range keys {
		want = append(want, k)
	}
	var rows []types.LegalDocumentRequest
	if err := s.db.WithContext(ctx).Where("number_key IN ?", want).Order("updated_at DESC").Limit(500).Find(&rows).Error; err != nil {
		logger.Warnf(ctx, "[LegalLinkReview] amender requests: %v", err)
		return out
	}
	for _, r := range rows {
		have, ok := out[r.NumberKey]
		if ok && (types.IsLegalRequestOpen(have.Status) || !types.IsLegalRequestOpen(r.Status)) {
			continue
		}
		out[r.NumberKey] = &LegalLinkAmenderRequest{ID: r.ID, DocumentNumber: r.DocumentNumber, Status: r.Status}
	}
	return out
}

// LegalLinkTargetGroup is one document A with candidates and their counts.
// PendingApprovable: pending with B known (a person can decide them);
// PendingBlocked: pending while B is not in the catalogue.
type LegalLinkTargetGroup struct {
	TargetDocID       string                 `json:"target_doc_id"`
	Document          *LegalLinkCandidateDoc `json:"document,omitempty"`
	Held              bool                   `json:"held"`
	KnowledgeID       string                 `json:"knowledge_id,omitempty"`
	PendingApprovable int64                  `json:"pending_approvable"`
	PendingBlocked    int64                  `json:"pending_blocked"`
	Approved          int64                  `json:"approved"`
	Rejected          int64                  `json:"rejected"`
	Auto              int64                  `json:"auto"`
}

// LegalLinkTargetTotals sums the groups' pending counts (the tab badge).
type LegalLinkTargetTotals struct {
	PendingApprovable int64 `json:"pending_approvable"`
	PendingBlocked    int64 `json:"pending_blocked"`
}

// legalLinkGroupsMax bounds the documents listed.
const legalLinkGroupsMax = 1000

// ListCandidateDocuments lists the documents A with candidates: those with
// links to decide first, then those only waiting for a document, then the
// rest; by số hiệu within each.
func (s *LegalPushService) ListCandidateDocuments(ctx context.Context, feed string) ([]*LegalLinkTargetGroup, LegalLinkTargetTotals, error) {
	var totals LegalLinkTargetTotals
	q := s.db.WithContext(ctx).Model(&types.LegalProvisionLinkCandidate{}).
		Select(`target_doc_id,
			SUM(CASE WHEN review_status = ? AND amender_doc_id <> '' THEN 1 ELSE 0 END) AS pending_approvable,
			SUM(CASE WHEN review_status = ? AND amender_doc_id = '' THEN 1 ELSE 0 END) AS pending_blocked,
			SUM(CASE WHEN review_status = ? THEN 1 ELSE 0 END) AS approved,
			SUM(CASE WHEN review_status = ? THEN 1 ELSE 0 END) AS rejected,
			SUM(CASE WHEN review_status = ? THEN 1 ELSE 0 END) AS auto`,
			types.LegalLinkReviewPending, types.LegalLinkReviewPending, types.LegalLinkReviewApproved,
			types.LegalLinkReviewRejected, types.LegalLinkReviewAuto).
		Group("target_doc_id")
	if f := strings.TrimSpace(feed); f != "" {
		q = q.Where("feed = ?", f)
	}
	var rows []struct {
		TargetDocID                                                 string
		PendingApprovable, PendingBlocked, Approved, Rejected, Auto int64
	}
	if err := q.Limit(legalLinkGroupsMax).Scan(&rows).Error; err != nil {
		return nil, totals, err
	}
	groups := make([]*LegalLinkTargetGroup, 0, len(rows))
	for _, r := range rows {
		groups = append(groups, &LegalLinkTargetGroup{TargetDocID: r.TargetDocID, PendingApprovable: r.PendingApprovable,
			PendingBlocked: r.PendingBlocked, Approved: r.Approved, Rejected: r.Rejected, Auto: r.Auto})
	}
	if len(groups) == 0 {
		return []*LegalLinkTargetGroup{}, totals, nil
	}
	ids := make([]string, 0, len(groups))
	for _, g := range groups {
		ids = append(ids, g.TargetDocID)
		totals.PendingApprovable += g.PendingApprovable
		totals.PendingBlocked += g.PendingBlocked
	}
	var docs []types.LegalDocument
	if err := s.db.WithContext(ctx).Where("id IN ?", ids).Find(&docs).Error; err != nil {
		return nil, totals, err
	}
	byID := map[string]*LegalLinkCandidateDoc{}
	for _, d := range docs {
		byID[d.ID] = &LegalLinkCandidateDoc{ID: d.ID, DocumentNumber: d.DocumentNumber, Title: d.Title, DocType: d.DocType, Status: d.Status}
	}
	held, err := s.heldDocs(ctx, ids)
	if err != nil {
		return nil, totals, err
	}
	for _, g := range groups {
		g.Document = byID[g.TargetDocID]
		if h := held[g.TargetDocID]; h != nil {
			g.Held, g.KnowledgeID = true, h.KnowledgeID
		}
	}
	rank := func(g *LegalLinkTargetGroup) int {
		switch {
		case g.PendingApprovable > 0:
			return 0
		case g.PendingBlocked > 0:
			return 1
		}
		return 2
	}
	number := func(g *LegalLinkTargetGroup) string {
		if g.Document != nil {
			return g.Document.DocumentNumber
		}
		return g.TargetDocID
	}
	sort.SliceStable(groups, func(i, j int) bool {
		if ri, rj := rank(groups[i]), rank(groups[j]); ri != rj {
			return ri < rj
		}
		return number(groups[i]) < number(groups[j])
	})
	return groups, totals, nil
}

// heldDocs is heldDoc for several documents at once.
func (s *LegalPushService) heldDocs(ctx context.Context, ids []string) (map[string]*legalHeldDoc, error) {
	out := map[string]*legalHeldDoc{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []struct {
		DocID           string
		KnowledgeID     string
		KnowledgeBaseID string
	}
	ext := legalJSONText(s.db, "k.metadata", "external_id")
	err := s.db.WithContext(ctx).Raw(`SELECT `+ext+` AS doc_id, k.id AS knowledge_id, k.knowledge_base_id FROM knowledges k
		JOIN knowledge_bases kb ON kb.id = k.knowledge_base_id AND kb.owner_tenant_id = 0 AND kb.deleted_at IS NULL
		WHERE k.deleted_at IS NULL AND `+ext+` IN ? ORDER BY k.created_at`, ids).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if _, ok := out[r.DocID]; !ok { // the oldest copy, as heldDoc
			out[r.DocID] = &legalHeldDoc{KnowledgeID: r.KnowledgeID, KnowledgeBaseID: r.KnowledgeBaseID}
		}
	}
	return out, nil
}

// Why a provision's wording is not shown (LegalProvisionText.Reason).
const (
	LegalTextUnknownDoc = "unknown_doc" // the catalogue has no such document
	LegalTextNotHeld    = "not_held"    // no public KB holds the document's full text
	LegalTextNoPath     = "no_path"     // the link names no provision
	LegalTextAppendix   = "appendix"    // a provision of an appendix: not numbered with the body
	LegalTextNotFound   = "not_found"   // the held copy has no such provision
)

// legalProvisionTextMaxRunes bounds the wording returned.
const legalProvisionTextMaxRunes = 4000

// legalProvisionTextChunksMax bounds the chunks read for one document.
const legalProvisionTextChunksMax = 5000

// LegalProvisionText is the wording of one provision of a catalogue
// document as its held copy has it.
type LegalProvisionText struct {
	DocID    string                 `json:"doc_id"`
	Path     string                 `json:"path"`
	Label    string                 `json:"label"`
	Document *LegalLinkCandidateDoc `json:"document,omitempty"`
	Held     bool                   `json:"held"`
	Found    bool                   `json:"found"`
	Text     string                 `json:"text"`
	// WholeArticle: the path names a điểm directly under a Điều, which the
	// chunks do not mark; the text is the whole Điều.
	WholeArticle    bool   `json:"whole_article,omitempty"`
	Truncated       bool   `json:"truncated,omitempty"`
	KnowledgeID     string `json:"knowledge_id,omitempty"`
	KnowledgeBaseID string `json:"knowledge_base_id,omitempty"`
	Reason          string `json:"reason,omitempty"`
}

// ProvisionText reads the wording of provision path ("dieu:5/khoan:2") of
// document docID from the chunks of its public-KB copy, with the same span
// reader the amendment resolver quotes B's provisions with
// (legalProvisionSpan). A provision split over several chunks is joined.
func (s *LegalPushService) ProvisionText(ctx context.Context, docID, path string) (*LegalProvisionText, error) {
	docID, path = strings.TrimSpace(docID), strings.ToLower(strings.TrimSpace(path))
	if docID == "" {
		return nil, apperrors.NewBadRequestError("doc_id is required")
	}
	out := &LegalProvisionText{DocID: docID, Path: path}
	var d types.LegalDocument
	if err := s.db.WithContext(ctx).Where("id = ?", docID).Limit(1).Find(&d).Error; err != nil {
		return nil, err
	}
	if d.ID == "" {
		out.Reason = LegalTextUnknownDoc
		return out, nil
	}
	out.Document = &LegalLinkCandidateDoc{ID: d.ID, DocumentNumber: d.DocumentNumber, Title: d.Title, DocType: d.DocType, Status: d.Status}
	ref, refOK := legalPathRef(path)
	if refOK {
		out.Label = ref.String()
	}
	held, err := s.heldDoc(ctx, docID)
	if err != nil {
		return nil, err
	}
	if held != nil {
		out.Held, out.KnowledgeID, out.KnowledgeBaseID = true, held.KnowledgeID, held.KnowledgeBaseID
	}
	switch {
	case path == "":
		out.Reason = LegalTextNoPath
		return out, nil
	case strings.HasPrefix(path, "phuluc/"):
		out.Reason = LegalTextAppendix
		return out, nil
	case !refOK:
		out.Reason = LegalTextNotFound
		return out, nil
	case held == nil:
		out.Reason = LegalTextNotHeld
		return out, nil
	}
	key := "dieu"
	if ref.Khoan != "" {
		key = "khoan:" + ref.Khoan
		if ref.Diem != "" {
			key += "/diem:" + ref.Diem
		}
	} else if ref.Diem != "" {
		out.WholeArticle = true
	}
	text, err := s.provisionSpanText(ctx, held.KnowledgeID, ref.Article, key)
	if err != nil {
		return nil, err
	}
	if text == "" {
		out.Reason = LegalTextNotFound
		return out, nil
	}
	out.Found = true
	if utf8.RuneCountInString(text) > legalProvisionTextMaxRunes {
		r := []rune(text)
		text, out.Truncated = strings.TrimSpace(string(r[:legalProvisionTextMaxRunes]))+"…", true
	}
	out.Text = text
	return out, nil
}

// provisionSpanText joins the spans of provision key of Điều article over
// the chunks of a held copy, in chunk order; when a chunk starts the
// provision (labelled), the spans before it (a mention of the same number
// elsewhere) are dropped.
func (s *LegalPushService) provisionSpanText(ctx context.Context, knowledgeID, article, key string) (string, error) {
	var rows []struct {
		Content  string
		Metadata []byte
	}
	if err := s.db.WithContext(ctx).Raw(`SELECT c.content, c.metadata FROM chunks c WHERE c.deleted_at IS NULL AND c.knowledge_id = ?
		ORDER BY c.chunk_index, c.id LIMIT ?`, knowledgeID, legalProvisionTextChunksMax).Scan(&rows).Error; err != nil {
		return "", fmt.Errorf("provision text chunks: %w", err)
	}
	var pieces []string
	start := -1
	seen := map[string]bool{}
	for _, r := range rows {
		var meta types.DocumentChunkMetadata
		if json.Unmarshal(r.Metadata, &meta) != nil || meta.Legal == nil || len(meta.Legal.ArticleNos) != 1 ||
			!strings.EqualFold(meta.Legal.ArticleNos[0], article) {
			continue
		}
		text, _, labelled, ok := legalProvisionSpan(r.Content, meta.Legal, key)
		if !ok || text == "" || seen[text] {
			continue
		}
		seen[text] = true
		if labelled && start < 0 {
			start = len(pieces)
		}
		pieces = append(pieces, text)
	}
	if start > 0 {
		pieces = pieces[start:]
	}
	return strings.TrimSpace(strings.Join(pieces, "\n")), nil
}

// LegalHeldProvision is one provision of a held copy.
type LegalHeldProvision struct {
	Path  string `json:"path"`
	Label string `json:"label"`
}

// LegalHeldProvisionList lists a document's provisions as its held copy's
// chunks record them; Held false: no public KB holds it (structure unknown).
type LegalHeldProvisionList struct {
	DocID       string               `json:"doc_id"`
	Held        bool                 `json:"held"`
	KnowledgeID string               `json:"knowledge_id,omitempty"`
	Provisions  []LegalHeldProvision `json:"provisions"`
}

// HeldProvisionList reads the provisions of docID's held copy (the paths a
// correction may pick), sorted Điều → khoản → điểm.
func (s *LegalPushService) HeldProvisionList(ctx context.Context, docID string) (*LegalHeldProvisionList, error) {
	docID = strings.TrimSpace(docID)
	if docID == "" {
		return nil, apperrors.NewBadRequestError("doc_id is required")
	}
	out := &LegalHeldProvisionList{DocID: docID, Provisions: []LegalHeldProvision{}}
	held, err := s.heldDoc(ctx, docID)
	if err != nil || held == nil {
		return out, err
	}
	out.Held, out.KnowledgeID = true, held.KnowledgeID
	provs, err := s.heldProvisions(ctx, held.KnowledgeID)
	if err != nil {
		return nil, err
	}
	refs := map[string]LegalProvisionRef{}
	for key := range provs.structure {
		parts := strings.SplitN(key, "|", 3)
		if len(parts) != 3 || parts[0] == "" {
			continue
		}
		p := LegalProvisionRef{Article: parts[0], Khoan: parts[1], Diem: parts[2]}
		refs[legalRefPath(p)] = p
	}
	for path := range provs.ids {
		if p, ok := legalPathRef(path); ok {
			refs[legalRefPath(p)] = p
		}
	}
	list := make([]LegalProvisionRef, 0, len(refs))
	for _, p := range refs {
		list = append(list, p)
	}
	sort.Slice(list, func(i, j int) bool { return legalRefLess(list[i], list[j]) })
	for _, p := range list {
		out.Provisions = append(out.Provisions, LegalHeldProvision{Path: legalRefPath(p), Label: p.String()})
	}
	return out, nil
}

// legalRefPath is the path of a provision ("dieu:5/khoan:2/diem:a").
func legalRefPath(p LegalProvisionRef) string {
	path := "dieu:" + p.Article
	if p.Khoan != "" {
		path += "/khoan:" + p.Khoan
	}
	if p.Diem != "" {
		path += "/diem:" + p.Diem
	}
	return path
}

// legalNumLess orders provision numbers: 2 < 10 < 10a, "" first.
func legalNumLess(a, b string) (less, equal bool) {
	if a == b {
		return false, true
	}
	if a == "" || b == "" {
		return a == "", false
	}
	na, ra := legalLeadingNumber(a)
	nb, rb := legalLeadingNumber(b)
	if na != nb {
		return na < nb, false
	}
	return ra < rb, false
}

func legalLeadingNumber(s string) (int, string) {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 {
		return 1 << 30, s
	}
	n, _ := strconv.Atoi(s[:i])
	return n, s[i:]
}

func legalRefLess(a, b LegalProvisionRef) bool {
	if l, eq := legalNumLess(a.Article, b.Article); !eq {
		return l
	}
	if l, eq := legalNumLess(a.Khoan, b.Khoan); !eq {
		return l
	}
	l, _ := legalNumLess(a.Diem, b.Diem)
	return l
}

// LegalLinkRecheckResult counts what a recheck did.
type LegalLinkRecheckResult struct {
	// Checked: pending candidates whose amender was unknown.
	Checked int `json:"checked"`
	// Resolved: the amender is now one catalogue document.
	Resolved int `json:"resolved"`
	// Matched: resolved and both provisions found (approvable as is).
	Matched int `json:"matched"`
	// AutoApproved: matched and approved without a person (same setting
	// as a push).
	AutoApproved int `json:"auto_approved"`
	// Duplicates: resolved to a link legal_provision_links already holds.
	Duplicates int `json:"duplicates"`
	// StillMissing: the catalogue still has no document with the number.
	StillMissing int `json:"still_missing"`
	// Ambiguous: several catalogue documents share the number (never guessed).
	Ambiguous int `json:"ambiguous"`
}

// legalRecheckMax bounds the candidates one recheck reads.
const legalRecheckMax = 5000

// RecheckCandidates resolves again the amender of every pending candidate
// the catalogue did not know when it was pushed (amender_doc_id empty),
// optionally only those acting on targetDocID. The resolution is the push's
// (resolveDoc: several documents with the number stay unresolved), the
// provisions and the match status are recomputed as a correction does, a
// link already held becomes a duplicate as in a push, and a fully matched
// candidate is approved without a person only under the push's own
// setting. Decided candidates are never touched; each candidate is updated
// in its own transaction, guarded on still being pending and unresolved.
func (s *LegalPushService) RecheckCandidates(ctx context.Context, targetDocID string) (*LegalLinkRecheckResult, error) {
	return s.recheck(ctx, strings.TrimSpace(targetDocID), "")
}

// recheck is RecheckCandidates, narrowed to the amenders of number key nk
// when nk is set (after a push brought that document).
func (s *LegalPushService) recheck(ctx context.Context, targetDocID, nk string) (*LegalLinkRecheckResult, error) {
	res := &LegalLinkRecheckResult{}
	if s == nil || s.db == nil {
		return res, nil
	}
	q := s.db.WithContext(ctx).Where("review_status = ? AND amender_doc_id = '' AND amender_doc_number <> ''", types.LegalLinkReviewPending)
	if targetDocID != "" {
		q = q.Where("target_doc_id = ?", targetDocID)
	}
	var list []*types.LegalProvisionLinkCandidate
	if err := q.Order("id").Limit(legalRecheckMax).Find(&list).Error; err != nil {
		return nil, err
	}
	auto := s.settings != nil && s.settings.GetBool(ctx, LegalFeedAutoApproveSetting, "", legalFeedAutoApproveDefault)
	type resolved struct {
		doc       *types.LegalDocument
		ambiguous bool
	}
	docs := map[string]resolved{}
	cache := legalProvCache{}
	changed := false
	for _, c := range list {
		if nk != "" && vietnamese_legal.CatalogNumberKey(c.AmenderDocNumber) != nk {
			continue
		}
		res.Checked++
		ref := legalDocRef{Number: c.AmenderDocNumber, IssueDate: c.AmenderIssueDate, Issuer: c.AmenderIssuer}
		rk := vietnamese_legal.CatalogNumberKey(ref.Number) + "|" + ref.IssueDate + "|" + strings.ToLower(ref.Issuer)
		r, ok := docs[rk]
		if !ok {
			doc, cands, _, err := s.resolveDoc(ctx, ref)
			if err != nil {
				return nil, fmt.Errorf("catalogue lookup: %w", err)
			}
			r = resolved{doc: doc, ambiguous: doc == nil && len(cands) > 0}
			docs[rk] = r
		}
		switch {
		case r.doc == nil && r.ambiguous:
			res.Ambiguous++
			continue
		case r.doc == nil:
			res.StillMissing++
			continue
		case r.doc.ID == c.TargetDocID:
			// the number is A's own: a push leaves it unresolved too
			res.StillMissing++
			continue
		}
		nc := *c
		nc.AmenderDocID = r.doc.ID
		if err := s.rematch(ctx, &nc, false, cache); err != nil {
			return nil, err
		}
		if nc.MatchStatus == types.LegalLinkMatched {
			dup, err := s.linkHeld(ctx, &nc)
			if err != nil {
				return nil, err
			}
			if dup {
				nc.MatchStatus, nc.ReviewStatus = types.LegalLinkDuplicate, types.LegalLinkReviewAuto
			}
		}
		nc.MatchReason = strings.TrimPrefix(nc.MatchReason+"; kiểm lại: "+r.doc.DocumentNumber+" đã có trong danh mục", "; ")
		if d, ok := legalMarkKeyWithAmender(nc.Detail, r.doc.ID); ok {
			nc.Detail = d
		}
		now := time.Now()
		approved, applied := false, false
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			upd := tx.Model(&types.LegalProvisionLinkCandidate{}).
				Where("id = ? AND review_status = ? AND amender_doc_id = ''", nc.ID, types.LegalLinkReviewPending).
				Updates(map[string]any{
					"amender_doc_id": nc.AmenderDocID, "target_provision_id": nc.TargetProvisionID,
					"source_provision_id": nc.SourceProvisionID, "match_status": nc.MatchStatus, "match_reason": nc.MatchReason,
					"review_status": nc.ReviewStatus, "detail": nc.Detail, "updated_at": now,
				})
			if upd.Error != nil {
				return upd.Error
			}
			if upd.RowsAffected == 0 { // decided or replaced meanwhile
				return nil
			}
			applied = true
			if auto && legalCandidateAutoApprovable(&nc) {
				approved = true
				return approveCandidateTx(tx, &nc, types.LegalLinkAutoReviewer, now)
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("recheck candidate %d: %w", c.ID, err)
		}
		if !applied {
			continue
		}
		changed = true
		res.Resolved++
		switch nc.MatchStatus {
		case types.LegalLinkMatched:
			res.Matched++
		case types.LegalLinkDuplicate:
			res.Duplicates++
		}
		if approved {
			res.AutoApproved++
		}
	}
	if changed {
		s.Notices.LegalLinksChanged(ctx)
	}
	return res, nil
}

// legalMarkKeyWithAmender fills the amender id of a candidate's mark key
// ("target path|code|number key|amender id|source path") pushed while the
// catalogue did not know the amender: the next push resolves it and must
// still recognize a decided mark (the same rewrite as an adoption's).
func legalMarkKeyWithAmender(detail types.JSON, amenderID string) (types.JSON, bool) {
	if len(detail) == 0 {
		return nil, false
	}
	var m map[string]any
	if json.Unmarshal(detail, &m) != nil {
		return nil, false
	}
	key, _ := m["mark_key"].(string)
	parts := strings.Split(key, "|")
	if len(parts) != 5 || parts[3] != "" {
		return nil, false
	}
	parts[3] = amenderID
	m["mark_key"] = strings.Join(parts, "|")
	b, err := json.Marshal(m)
	if err != nil {
		return nil, false
	}
	return types.JSON(b), true
}

// recheckAfterPush resolves the pending candidates that named document a
// as their amender before the catalogue knew it. Called once a push has
// stored a; a failure is only logged (the push itself succeeded).
func (s *LegalPushService) recheckAfterPush(ctx context.Context, a *types.LegalDocument) {
	nk := a.NumberKey
	if nk == "" {
		nk = vietnamese_legal.CatalogNumberKey(a.DocumentNumber)
	}
	if nk == "" {
		return
	}
	res, err := s.recheck(ctx, "", nk)
	if err != nil {
		logger.Warnf(ctx, "[LegalPush] recheck links naming %s: %v", a.DocumentNumber, err)
		return
	}
	if res.Resolved > 0 {
		logger.Infof(ctx, "[LegalPush] %s: %d pending links now name it (matched %d, auto-approved %d)",
			a.DocumentNumber, res.Resolved, res.Matched, res.AutoApproved)
	}
}

// LegalLinkBatchMax bounds the candidates of one batch decision.
const LegalLinkBatchMax = 200

// LegalLinkBatchDecision decides several candidates the same way (no
// corrections: those go one by one).
type LegalLinkBatchDecision struct {
	IDs    []int64 `json:"ids"`
	Action string  `json:"action"`
	Note   string  `json:"note,omitempty"`
}

// LegalLinkBatchFailure is one candidate a batch could not decide.
type LegalLinkBatchFailure struct {
	ID    int64  `json:"id"`
	Error string `json:"error"`
}

// LegalLinkBatchResult lists the candidates decided and those that failed.
type LegalLinkBatchResult struct {
	Done   []int64                 `json:"done"`
	Failed []LegalLinkBatchFailure `json:"failed"`
}

// DecideCandidates decides each candidate as DecideCandidate does; one
// failing does not stop the others.
func (s *LegalPushService) DecideCandidates(ctx context.Context, b LegalLinkBatchDecision, caller types.Caller) (*LegalLinkBatchResult, error) {
	action := strings.ToLower(strings.TrimSpace(b.Action))
	if action != "approve" && action != "reject" {
		return nil, apperrors.NewBadRequestError("action must be approve or reject")
	}
	if len(b.IDs) == 0 {
		return nil, apperrors.NewBadRequestError("ids is empty")
	}
	if len(b.IDs) > LegalLinkBatchMax {
		return nil, apperrors.NewBadRequestError(fmt.Sprintf("at most %d ids per batch", LegalLinkBatchMax))
	}
	out := &LegalLinkBatchResult{Done: []int64{}, Failed: []LegalLinkBatchFailure{}}
	seen := map[int64]bool{}
	for _, id := range b.IDs {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		if _, err := s.decide(ctx, id, LegalLinkDecision{Action: action, Note: b.Note}, caller); err != nil {
			msg := err.Error()
			if app, ok := apperrors.IsAppError(err); ok {
				msg = app.Message
			}
			out.Failed = append(out.Failed, LegalLinkBatchFailure{ID: id, Error: msg})
			continue
		}
		out.Done = append(out.Done, id)
	}
	if len(out.Done) > 0 {
		s.Notices.LegalLinksChanged(ctx)
	}
	return out, nil
}
