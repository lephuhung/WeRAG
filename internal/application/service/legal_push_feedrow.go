package service

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"html"
	"regexp"
	"strings"
	"time"
	"unicode"

	"gorm.io/gorm/clause"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/legalcatalog"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// Statuses a feed row may get without a status from the feed: never "Còn
// hiệu lực" — a document is only called in force on a source's word.
const (
	legalFeedStatusPending = "Chưa có hiệu lực"
	legalFeedStatusUnknown = "Chưa xác định"
)

// legalFeedTitleMinLetters is how many letters a title needs besides its
// type and number: the title is what tells this document from another one
// of the same number (legalcatalog.SameDocument) when vbpl.vn lists it.
const legalFeedTitleMinLetters = 10

var legalHTMLTitleRe = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

// resolveByNumber goes on where resolveDoc found no single row for the
// pushed document: the số hiệu is the key shared with vbpl.vn, and among
// the rows of that number (cands) the one that is the same document
// (legalcatalog.SameDocument) is A. When no row of the number can be A, a
// mode full / auto push gets a new feed row to create (feedRow). Otherwise
// both are nil and why adds to the refusal.
func (s *LegalPushService) resolveByNumber(meta *legalPushMeta, content []byte, mode string,
	cands []*types.LegalDocument,
) (a, feedRow *types.LegalDocument, why string) {
	if meta.ref.ID != "" {
		return nil, nil, "" // an id names one row; it is missing
	}
	proposed, missing := legalFeedRow(meta, content, time.Now())
	if proposed == nil {
		if len(cands) == 0 && mode != LegalPushLinks {
			return nil, nil, "; muốn tạo dòng danh mục mới cần metadata " + strings.Join(missing, ", ")
		}
		return nil, nil, ""
	}
	var same []*types.LegalDocument
	near := false
	for _, c := range cands {
		if legalcatalog.SameDocument(c, proposed) {
			same = append(same, c)
		}
		sameDay := c.IssuedOn != nil && c.IssuedOn.Format("2006-01-02") == proposed.IssuedOn.Format("2006-01-02")
		sameType := vietnamese_legal.DocTypeClass(c.DocType) == vietnamese_legal.DocTypeClass(proposed.DocType)
		// same day and type, or the same title and issuer on another day
		// (a date typo on either side): not clearly another document
		if sameDay && sameType || sameType && legalcatalog.IssuersAgree(c.Issuer, proposed.Issuer) &&
			legalcatalog.TitleMatch(c.Title, c.DocumentNumber, proposed.Title, proposed.DocumentNumber) {
			near = true
		}
	}
	switch {
	case len(same) == 1:
		return same[0], nil, ""
	case len(same) > 1:
		return nil, nil, "; nhiều dòng danh mục cùng là văn bản này: chọn legal_document_id"
	case near:
		// same number, day and type but the title or issuer disagree: a
		// person tells whether it is this document
		return nil, nil, "; có văn bản cùng số hiệu gần giống (cùng ngày và loại, hoặc cùng tiêu đề và cơ quan nhưng khác ngày ban hành): chọn legal_document_id"
	case mode == LegalPushLinks:
		return nil, nil, "; chế độ links cần văn bản có trong danh mục (gửi full hoặc auto để tạo dòng mới)"
	}
	return nil, proposed, ""
}

// legalFeedRow builds the catalogue row of a pushed document the catalogue
// does not hold; nil and the missing fields when the metadata cannot
// describe it well enough to be matched later.
func legalFeedRow(meta *legalPushMeta, content []byte, now time.Time) (*types.LegalDocument, []string) {
	number := legalcatalog.CleanVBPLNumber(strings.TrimSpace(meta.ref.Number))
	title := strings.Join(strings.Fields(meta.title), " ")
	if title == "" {
		if m := legalHTMLTitleRe.FindSubmatch(content); m != nil {
			title = strings.Join(strings.Fields(html.UnescapeString(string(m[1]))), " ")
		}
	}
	var missing []string
	key := vietnamese_legal.CatalogNumberKey(number)
	if key == "" {
		missing = append(missing, "doc_number")
	}
	if strings.TrimSpace(meta.ref.DocType) == "" {
		missing = append(missing, "doc_type")
	}
	if strings.TrimSpace(meta.ref.Issuer) == "" {
		missing = append(missing, "issuer")
	}
	issued := legalPushDate(meta.ref.IssueDate)
	if issued == nil {
		missing = append(missing, "issue_date (YYYY-MM-DD)")
	}
	if !legalFeedTitleUsable(title, number, meta.ref.DocType) {
		missing = append(missing, "title (tên đầy đủ của văn bản)")
	}
	if len(missing) > 0 {
		return nil, missing
	}
	eff := legalPushDate(meta.effFrom)
	status := strings.TrimSpace(meta.effStatus)
	switch strings.ToLower(strings.Join(strings.Fields(status), " ")) {
	case "đã biết": // TVPL's placeholder for a hidden value
		status = ""
	case "hết hiệu lực": // TVPL's wording of the portal's whole expiry
		status = LegalExpiredStatusPrefix
	}
	if status == "" {
		status = legalFeedStatusUnknown
		if eff != nil && eff.After(now) {
			status = legalFeedStatusPending
		}
	}
	snap := now
	return &types.LegalDocument{
		ID: legalFeedRowID(meta.feed, meta.feedDocID), Source: meta.feed,
		DocumentNumber: legalCap(number, 128), NumberKey: legalCap(key, 128),
		Title: title, DocType: legalCap(strings.TrimSpace(meta.ref.DocType), 64),
		Issuer:   legalCap(strings.TrimSpace(meta.ref.Issuer), 255),
		IssuedOn: issued, EffectiveOn: eff, Status: legalCap(status, 64), SourceUpdatedAt: &snap,
	}, nil
}

// legalFeedTitleUsable: the title says more than "<type> <number>".
func legalFeedTitleUsable(title, number, docType string) bool {
	rest := strings.ToLower(title)
	for _, cut := range []string{strings.ToLower(number), strings.ToLower(docType), "số"} {
		if cut != "" {
			rest = strings.ReplaceAll(rest, cut, " ")
		}
	}
	n := 0
	for _, r := range rest {
		if unicode.IsLetter(r) {
			n++
		}
	}
	return n >= legalFeedTitleMinLetters
}

// legalFeedRowID is the catalogue id of a feed's document: "<feed>-<doc id
// at the feed>", stable across pushes, within varchar(64).
func legalFeedRowID(feed, feedDocID string) string {
	id := feed + "-" + feedDocID
	if len(id) <= 64 {
		return id
	}
	sum := sha1.Sum([]byte(feedDocID))
	return feed + "-" + hex.EncodeToString(sum[:])[:24]
}

// insertFeedRow adds a feed row; created is false when a row with its id
// is there already (a concurrent push of the same document) — then it must
// be the same number. Open requests of the number that name no catalogue
// row are attached to it, so the ingest closes them.
func (s *LegalPushService) insertFeedRow(ctx context.Context, row *types.LegalDocument) (bool, error) {
	db := s.db.WithContext(ctx)
	res := db.Clauses(clause.OnConflict{DoNothing: true}).Create(row)
	if res.Error != nil {
		return false, fmt.Errorf("create catalogue row: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		var have types.LegalDocument
		if err := db.Where("id = ?", row.ID).Limit(1).Find(&have).Error; err != nil {
			return false, fmt.Errorf("read catalogue row: %w", err)
		}
		if have.ID == "" || have.NumberKey != row.NumberKey {
			return false, apperrors.NewConflictError(fmt.Sprintf("dòng danh mục %s đã có với số hiệu khác (%s)", row.ID, have.DocumentNumber))
		}
		return false, nil
	}
	if err := db.Model(&types.LegalDocumentRequest{}).
		Where("number_key = ? AND legal_document_id = '' AND status IN ?", row.NumberKey, types.LegalRequestOpenStatuses).
		Updates(map[string]any{"legal_document_id": row.ID, "updated_at": time.Now()}).Error; err != nil {
		logger.Warnf(ctx, "[LegalPush] attach requests of %s: %v", row.DocumentNumber, err)
	}
	logger.Infof(ctx, "[LegalPush] catalogue row %s created for %s (feed %s)", row.ID, row.DocumentNumber, row.Source)
	return true, nil
}

// dropFeedRow undoes insertFeedRow after a failed ingest.
func (s *LegalPushService) dropFeedRow(ctx context.Context, row *types.LegalDocument) {
	db := s.db.WithContext(ctx)
	if err := db.Model(&types.LegalDocumentRequest{}).
		Where("legal_document_id = ? AND status IN ?", row.ID, types.LegalRequestOpenStatuses).
		Updates(map[string]any{"legal_document_id": "", "updated_at": time.Now()}).Error; err != nil {
		logger.Warnf(ctx, "[LegalPush] detach requests of %s: %v", row.ID, err)
	}
	if err := db.Where("id = ? AND source = ? AND reviewed_at IS NULL", row.ID, row.Source).
		Delete(&types.LegalDocument{}).Error; err != nil {
		logger.Warnf(ctx, "[LegalPush] remove catalogue row %s after a failed ingest: %v", row.ID, err)
	}
}
