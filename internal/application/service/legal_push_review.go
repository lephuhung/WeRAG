package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
)

// LegalLinkCandidateFilter selects candidates for the review page.
type LegalLinkCandidateFilter struct {
	ReviewStatus string // pending (default) | approved | rejected | auto | all
	MatchStatus  string // "" = any
	TargetDocID  string
	Feed         string
}

// LegalLinkCandidateDoc is the catalogue row shown beside a candidate.
type LegalLinkCandidateDoc struct {
	ID             string `json:"id"`
	DocumentNumber string `json:"document_number"`
	Title          string `json:"title"`
	DocType        string `json:"doc_type"`
	Status         string `json:"status"`
}

// LegalLinkCandidateItem is a candidate with its documents and, when one
// exists, the vbpl editors' links on the same provision of A (agree /
// disagree at a glance).
type LegalLinkCandidateItem struct {
	*types.LegalProvisionLinkCandidate
	Target  *LegalLinkCandidateDoc     `json:"target,omitempty"`
	Amender *LegalLinkCandidateDoc     `json:"amender,omitempty"`
	Others  []types.LegalProvisionLink `json:"other_links"`
}

// LegalLinkDecision is the SuperAdmin's decision on one candidate. The
// optional fields correct the candidate before it is approved.
type LegalLinkDecision struct {
	Action       string `json:"action"` // approve | reject
	AmenderDocID string `json:"amender_doc_id,omitempty"`
	SourcePath   string `json:"source_path,omitempty"`
	TargetPath   string `json:"target_path,omitempty"`
	Note         string `json:"note,omitempty"`
}

// ListCandidates pages the candidates, oldest first within a document;
// counts holds the total per review status.
func (s *LegalPushService) ListCandidates(ctx context.Context, f LegalLinkCandidateFilter, offset, limit int,
) ([]*LegalLinkCandidateItem, int64, map[string]int64, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	base := func() *gorm.DB {
		q := s.db.WithContext(ctx).Model(&types.LegalProvisionLinkCandidate{})
		if f.MatchStatus != "" {
			q = q.Where("match_status = ?", f.MatchStatus)
		}
		if f.TargetDocID != "" {
			q = q.Where("target_doc_id = ?", f.TargetDocID)
		}
		if f.Feed != "" {
			q = q.Where("feed = ?", f.Feed)
		}
		return q
	}
	counts := map[string]int64{}
	var rows []struct {
		ReviewStatus string
		N            int64
	}
	if err := base().Select("review_status, COUNT(*) AS n").Group("review_status").Scan(&rows).Error; err != nil {
		return nil, 0, nil, err
	}
	for _, r := range rows {
		counts[r.ReviewStatus] = r.N
	}
	st := strings.ToLower(strings.TrimSpace(f.ReviewStatus))
	if st == "" {
		st = types.LegalLinkReviewPending
	}
	q := base()
	if st != "all" {
		q = q.Where("review_status = ?", st)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, nil, err
	}
	var list []*types.LegalProvisionLinkCandidate
	if err := q.Order("target_doc_id, target_path, id").Offset(offset).Limit(limit).Find(&list).Error; err != nil {
		return nil, 0, nil, err
	}
	items, err := s.decorate(ctx, list)
	return items, total, counts, err
}

// decorate adds the catalogue rows and the links already held on the same
// provisions of A.
func (s *LegalPushService) decorate(ctx context.Context, list []*types.LegalProvisionLinkCandidate) ([]*LegalLinkCandidateItem, error) {
	ids := map[string]bool{}
	var targets []string
	for _, c := range list {
		for _, id := range []string{c.TargetDocID, c.AmenderDocID} {
			if id != "" && !ids[id] {
				ids[id] = true
			}
		}
		targets = append(targets, c.TargetDocID)
	}
	docs := map[string]*LegalLinkCandidateDoc{}
	if len(ids) > 0 {
		keys := make([]string, 0, len(ids))
		for id := range ids {
			keys = append(keys, id)
		}
		var rows []types.LegalDocument
		if err := s.db.WithContext(ctx).Where("id IN ?", keys).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, d := range rows {
			docs[d.ID] = &LegalLinkCandidateDoc{ID: d.ID, DocumentNumber: d.DocumentNumber, Title: d.Title, DocType: d.DocType, Status: d.Status}
		}
	}
	held := map[string][]types.LegalProvisionLink{}
	if len(targets) > 0 {
		var links []types.LegalProvisionLink
		if err := s.db.WithContext(ctx).Where("target_doc_id IN ?", targets).Order("id").Limit(5000).Find(&links).Error; err != nil {
			return nil, err
		}
		for _, l := range links {
			k := l.TargetDocID + "|" + l.TargetPath
			held[k] = append(held[k], l)
		}
	}
	out := make([]*LegalLinkCandidateItem, 0, len(list))
	for _, c := range list {
		it := &LegalLinkCandidateItem{LegalProvisionLinkCandidate: c, Target: docs[c.TargetDocID], Amender: docs[c.AmenderDocID],
			Others: []types.LegalProvisionLink{}}
		for _, l := range held[c.TargetDocID+"|"+c.TargetPath] {
			if c.LinkID != nil && l.ID == *c.LinkID {
				continue
			}
			it.Others = append(it.Others, l)
		}
		out = append(out, it)
	}
	return out, nil
}

// DecideCandidate approves (copies into legal_provision_links with Source =
// the feed) or rejects one pending candidate. Corrections in d are applied
// before an approval; the provision ids are looked up again.
func (s *LegalPushService) DecideCandidate(ctx context.Context, id int64, d LegalLinkDecision, caller types.Caller) (*LegalLinkCandidateItem, error) {
	action := strings.ToLower(strings.TrimSpace(d.Action))
	if action != "approve" && action != "reject" {
		return nil, apperrors.NewBadRequestError("action must be approve or reject")
	}
	var c types.LegalProvisionLinkCandidate
	if err := s.db.WithContext(ctx).Where("id = ?", id).Limit(1).Find(&c).Error; err != nil {
		return nil, err
	}
	if c.ID == 0 {
		return nil, apperrors.NewNotFoundError("candidate not found")
	}
	if c.ReviewStatus != types.LegalLinkReviewPending {
		return nil, apperrors.NewConflictError("candidate is " + c.ReviewStatus + ", not pending")
	}
	now := time.Now()
	by := caller.UserID
	if by == "" {
		by = fmt.Sprintf("tenant:%d", caller.TenantID)
	}
	if note := strings.TrimSpace(d.Note); note != "" {
		c.MatchReason = strings.TrimPrefix(c.MatchReason+"; quyết định: "+note, "; ")
	}
	if action == "reject" {
		c.ReviewStatus, c.ReviewedBy, c.ReviewedAt = types.LegalLinkReviewRejected, by, &now
		if err := s.db.WithContext(ctx).Save(&c).Error; err != nil {
			return nil, err
		}
		return s.one(ctx, &c)
	}
	if err := s.correct(ctx, &c, d); err != nil {
		return nil, err
	}
	if c.AmenderDocID == "" {
		return nil, apperrors.NewBadRequestError("chưa xác định văn bản tác động: chọn amender_doc_id trước khi duyệt")
	}
	if c.TargetPath == "" {
		return nil, apperrors.NewBadRequestError("chưa xác định vị trí trong văn bản: nhập target_path trước khi duyệt")
	}
	if _, ok := legalPathRef(c.TargetPath); !ok && !strings.HasPrefix(c.TargetPath, "phuluc/") {
		return nil, apperrors.NewBadRequestError("target_path phải có dạng dieu:N/khoan:K/diem:D")
	}
	link := types.LegalProvisionLink{
		TargetDocID: c.TargetDocID, TargetProvisionID: c.TargetProvisionID, TargetLevel: c.TargetLevel, TargetPath: c.TargetPath,
		RelationCode: c.RelationCode, Op: c.Op, TagAttr: "feed", SourceDocID: c.AmenderDocID, SourceDocFrom: "feed",
		SourceProvisionID: c.SourceProvisionID, SourcePath: c.SourcePath, EffFrom: c.EffFrom, EffTo: c.EffTo, Source: c.Feed,
	}
	if c.EffFrom != nil {
		link.EffFromSource = "feed"
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&link).Error; err != nil {
			return err
		}
		if link.ID == 0 { // the same edge was already held
			var have types.LegalProvisionLink
			if err := tx.Where("target_provision_id = ? AND relation_code = ? AND source_provision_id = ?",
				link.TargetProvisionID, link.RelationCode, link.SourceProvisionID).Limit(1).Find(&have).Error; err != nil {
				return err
			}
			link.ID = have.ID
		}
		c.ReviewStatus, c.ReviewedBy, c.ReviewedAt = types.LegalLinkReviewApproved, by, &now
		if link.ID != 0 {
			c.LinkID = &link.ID
		}
		return tx.Save(&c).Error
	})
	if err != nil {
		return nil, fmt.Errorf("approve candidate %d: %w", id, err)
	}
	return s.one(ctx, &c)
}

// correct applies the SuperAdmin's corrections and looks the provisions up
// again; the match status is recomputed for the record.
func (s *LegalPushService) correct(ctx context.Context, c *types.LegalProvisionLinkCandidate, d LegalLinkDecision) error {
	changed := false
	if v := strings.TrimSpace(d.AmenderDocID); v != "" && v != c.AmenderDocID {
		var b types.LegalDocument
		if err := s.db.WithContext(ctx).Where("id = ?", v).Limit(1).Find(&b).Error; err != nil {
			return err
		}
		if b.ID == "" {
			return apperrors.NewBadRequestError("amender_doc_id " + v + " không có trong danh mục")
		}
		if b.ID == c.TargetDocID {
			return apperrors.NewBadRequestError("văn bản tác động trùng chính văn bản bị tác động")
		}
		c.AmenderDocID, c.AmenderDocNumber, changed = b.ID, legalCap(b.DocumentNumber, 128), true
	}
	if v := strings.ToLower(strings.TrimSpace(d.SourcePath)); v != "" && v != c.SourcePath {
		c.SourcePath, changed = legalCap(v, 160), true
	}
	if v := strings.ToLower(strings.TrimSpace(d.TargetPath)); v != "" && v != c.TargetPath {
		c.TargetPath, changed = legalCap(v, 160), true
		c.TargetProvisionID = ""
	}
	if !changed && c.TargetProvisionID != "" && c.SourceProvisionID != "" {
		return nil
	}
	// A's provision: the held copy's id at the path, else a stable synthetic id
	if c.TargetProvisionID == "" || changed {
		if held, err := s.heldDoc(ctx, c.TargetDocID); err != nil {
			return err
		} else if held != nil {
			provs, err := s.heldProvisions(ctx, held.KnowledgeID)
			if err != nil {
				return err
			}
			if id := provs.ids[c.TargetPath]; id != "" {
				c.TargetProvisionID = id
			}
		}
		if c.TargetProvisionID == "" {
			c.TargetProvisionID = legalSyntheticProvisionID(c.TargetDocID, c.TargetPath)
		}
	}
	// B's provision
	if c.AmenderDocID != "" {
		c.SourceProvisionID = legalSyntheticProvisionID(c.AmenderDocID, c.SourcePath)
		if held, err := s.heldDoc(ctx, c.AmenderDocID); err != nil {
			return err
		} else if held != nil && c.SourcePath != "" {
			provs, err := s.heldProvisions(ctx, held.KnowledgeID)
			if err != nil {
				return err
			}
			if id := provs.ids[c.SourcePath]; id != "" {
				c.SourceProvisionID = id
			}
		}
	}
	if changed {
		c.MatchReason = strings.TrimPrefix(c.MatchReason+"; đã sửa khi duyệt", "; ")
	}
	return nil
}

func (s *LegalPushService) one(ctx context.Context, c *types.LegalProvisionLinkCandidate) (*LegalLinkCandidateItem, error) {
	items, err := s.decorate(ctx, []*types.LegalProvisionLinkCandidate{c})
	if err != nil || len(items) == 0 {
		return nil, err
	}
	return items[0], nil
}
