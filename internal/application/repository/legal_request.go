package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type legalRequestRepository struct {
	db *gorm.DB
}

// NewLegalRequestRepository creates the legal document request repository.
func NewLegalRequestRepository(db *gorm.DB) interfaces.LegalRequestRepository {
	return &legalRequestRepository{db: db}
}

func (r *legalRequestRepository) find(ctx context.Context, numberKey, docID string) (*types.LegalDocumentRequest, error) {
	var row types.LegalDocumentRequest
	err := r.db.WithContext(ctx).Where("number_key = ? AND legal_document_id = ?", numberKey, docID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &row, err
}

func (r *legalRequestRepository) Record(
	ctx context.Context, req *types.LegalDocumentRequest,
) (*types.LegalDocumentRequest, error) {
	if req == nil || strings.TrimSpace(req.NumberKey) == "" {
		return nil, errors.New("legal request: number key is required")
	}
	now := time.Now()
	row := *req
	if row.ID == "" {
		row.ID = uuid.NewString()
	}
	row.Status = types.LegalRequestRequested
	row.RequestCount = 1
	row.FirstRequestedAt, row.LastRequestedAt = now, now
	if q := []rune(row.LastQuery); len(q) > types.LegalRequestQueryMaxRunes {
		row.LastQuery = string(q[:types.LegalRequestQueryMaxRunes])
	}
	db := r.db.WithContext(ctx)
	res := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 1 {
		return &row, nil
	}
	existing, err := r.find(ctx, row.NumberKey, row.LegalDocumentID)
	if err != nil || existing == nil {
		if err == nil {
			err = errors.New("legal request: row vanished after a conflict")
		}
		return nil, err
	}
	bump := map[string]any{
		"request_count":     gorm.Expr("request_count + 1"),
		"last_requested_at": now,
		"last_tenant_id":    row.LastTenantID,
		"last_user_id":      row.LastUserID,
		"last_session_id":   row.LastSessionID,
		"last_query":        row.LastQuery,
		"updated_at":        now,
	}
	// fill catalogue fields an earlier request did not know
	if existing.Title == "" && row.Title != "" {
		bump["title"] = row.Title
	}
	if existing.DocType == "" && row.DocType != "" {
		bump["doc_type"] = row.DocType
	}
	switch existing.Status {
	case types.LegalRequestRequested, types.LegalRequestFetched, types.LegalRequestReviewing, types.LegalRequestAutoIngest:
		if existing.RequestCount == 0 {
			// Queued by the import, nobody had asked: the first real request
			// counts, also from a caller without a session (a legal feed).
			err = db.Model(&types.LegalDocumentRequest{}).
				Where("id = ? AND status = ?", existing.ID, existing.Status).Updates(bump).Error
			break
		}
		// still open (a draft of its PDF may be under review): the same
		// session asking again does not count twice, the state is kept
		err = db.Model(&types.LegalDocumentRequest{}).
			Where("id = ? AND status = ? AND last_session_id <> ?", existing.ID, existing.Status, row.LastSessionID).
			Updates(bump).Error
	case types.LegalRequestAdded:
		// asked for again: the added copy is not where this user looks
		// (deleted since, or in another organization's KB)
		bump["status"] = types.LegalRequestRequested
		bump["knowledge_id"], bump["knowledge_base_id"] = "", ""
		bump["decided_by"], bump["decided_at"], bump["decision_note"] = "", nil, ""
		err = db.Model(&types.LegalDocumentRequest{}).
			Where("id = ? AND status = ?", existing.ID, types.LegalRequestAdded).Updates(bump).Error
	}
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, existing.ID)
}

func (r *legalRequestRepository) Get(ctx context.Context, id string) (*types.LegalDocumentRequest, error) {
	var row types.LegalDocumentRequest
	err := r.db.WithContext(ctx).Where("id = ?", id).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *legalRequestRepository) ListByNumberKey(
	ctx context.Context, numberKey string,
) ([]*types.LegalDocumentRequest, error) {
	var rows []*types.LegalDocumentRequest
	if strings.TrimSpace(numberKey) == "" {
		return rows, nil
	}
	err := r.db.WithContext(ctx).Where("number_key = ?", numberKey).
		Order("first_requested_at").Order("id").Find(&rows).Error
	return rows, err
}

func (r *legalRequestRepository) List(
	ctx context.Context, status string, offset, limit int,
) ([]*types.LegalDocumentRequest, int64, error) {
	q := r.db.WithContext(ctx).Model(&types.LegalDocumentRequest{})
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []*types.LegalDocumentRequest
	if limit <= 0 {
		limit = 50
	}
	// Requests people made come before the ones the import queued by itself
	// (request_count 0), or a nightly batch would bury them.
	err := q.Order("CASE WHEN request_count > 0 THEN 0 ELSE 1 END").
		Order("last_requested_at DESC").Order("id").Offset(offset).Limit(limit).Find(&rows).Error
	return rows, total, err
}

func (r *legalRequestRepository) Decide(
	ctx context.Context, id, status, decidedBy, note, knowledgeID, kbID string,
) (*types.LegalDocumentRequest, error) {
	now := time.Now()
	res := r.db.WithContext(ctx).Model(&types.LegalDocumentRequest{}).Where("id = ?", id).Updates(map[string]any{
		"status": status, "decided_by": decidedBy, "decided_at": now, "decision_note": note,
		"knowledge_id": knowledgeID, "knowledge_base_id": kbID, "updated_at": now,
		// A decision closes the work: whoever held the lease is done.
		"claimed_by": "", "claimed_at": nil,
	})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}
	return r.Get(ctx, id)
}

// Claim takes or releases the operator lease. Taking only wins when the lease
// is free, expired, or already the caller's; releasing only clears the caller's
// own lease. Returns nil when the row is absent or the lease belongs to someone
// else (the service turns that into 409).
func (r *legalRequestRepository) Claim(
	ctx context.Context, id, userID string, claim bool,
) (*types.LegalDocumentRequest, error) {
	now := time.Now()
	q := r.db.WithContext(ctx).Model(&types.LegalDocumentRequest{}).Where("id = ?", id)
	if claim {
		// Only an open request can be taken: a queue entry someone else already
		// closed must not restart the fetch pipeline.
		q = q.Where("status IN ?", types.LegalRequestOpenStatuses).
			Where("claimed_by = '' OR claimed_at IS NULL OR claimed_by = ? OR claimed_at < ?",
				userID, now.Add(-types.LegalRequestClaimLease))
	} else {
		q = q.Where("claimed_by = ?", userID)
	}
	values := map[string]any{"claimed_by": userID, "claimed_at": now, "updated_at": now}
	if !claim {
		values = map[string]any{"claimed_by": "", "claimed_at": nil, "updated_at": now}
	}
	res := q.Updates(values)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}
	return r.Get(ctx, id)
}

func (r *legalRequestRepository) NextAutoIngest(ctx context.Context, limit int) ([]*types.LegalDocumentRequest, error) {
	var rows []*types.LegalDocumentRequest
	err := r.db.WithContext(ctx).Where("status = ?", types.LegalRequestAutoIngest).
		Order("updated_at").Order("id").Limit(limit).Find(&rows).Error
	return rows, err
}

func (r *legalRequestRepository) ReturnToOperators(ctx context.Context, id, source string) error {
	return r.db.WithContext(ctx).Model(&types.LegalDocumentRequest{}).
		Where("id = ? AND status = ?", id, types.LegalRequestAutoIngest).
		Updates(map[string]any{
			"status": types.LegalRequestRequested, "source": source,
			"claimed_by": "", "claimed_at": nil, "updated_at": time.Now(),
		}).Error
}

func (r *legalRequestRepository) UpdateDetail(
	ctx context.Context, id string, detail map[string]any,
) (*types.LegalDocumentRequest, error) {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row types.LegalDocumentRequest
		q := tx
		if tx.Dialector.Name() == "postgres" {
			q = tx.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := q.Select("id", "detail").Where("id = ?", id).Take(&row).Error; err != nil {
			return err
		}
		merged := map[string]any{}
		if len(row.Detail) > 0 && string(row.Detail) != "null" {
			if err := json.Unmarshal(row.Detail, &merged); err != nil {
				return fmt.Errorf("legal request %s: bad detail JSON: %w", id, err)
			}
		}
		for k, v := range detail {
			if v == nil { // a nil value removes the key
				delete(merged, k)
				continue
			}
			merged[k] = v
		}
		raw, err := json.Marshal(merged)
		if err != nil {
			return err
		}
		return tx.Model(&types.LegalDocumentRequest{}).Where("id = ?", id).
			Updates(map[string]any{"detail": types.JSON(raw), "updated_at": time.Now()}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}

func (r *legalRequestRepository) KnowledgeParseStatuses(ctx context.Context, knowledgeIDs []string) (map[string]string, error) {
	out := map[string]string{}
	if len(knowledgeIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		ID          string
		ParseStatus string
	}
	err := r.db.WithContext(ctx).Table("knowledges").Select("id, parse_status").
		Where("id IN ? AND deleted_at IS NULL", knowledgeIDs).Scan(&rows).Error
	for _, row := range rows {
		out[row.ID] = row.ParseStatus
	}
	return out, err
}

func (r *legalRequestRepository) MarkAddedByDocument(
	ctx context.Context, legalDocumentID, knowledgeID, kbID, actor string,
) (int64, error) {
	if strings.TrimSpace(legalDocumentID) == "" {
		return 0, nil
	}
	now := time.Now()
	res := r.db.WithContext(ctx).Model(&types.LegalDocumentRequest{}).
		Where("legal_document_id = ? AND status IN ?", legalDocumentID, types.LegalRequestOpenStatuses).
		Updates(map[string]any{
			"status": types.LegalRequestAdded, "decided_by": actor, "decided_at": now,
			"knowledge_id": knowledgeID, "knowledge_base_id": kbID, "updated_at": now,
			// An ingest of this document finishes the queue entry: the lease
			// must not survive a later reopen and block the next operator.
			"claimed_by": "", "claimed_at": nil,
		})
	return res.RowsAffected, res.Error
}

func (r *legalRequestRepository) SetStatusByDocument(
	ctx context.Context, legalDocumentID string, from []string, to string,
) (int64, error) {
	if strings.TrimSpace(legalDocumentID) == "" || len(from) == 0 {
		return 0, nil
	}
	res := r.db.WithContext(ctx).Model(&types.LegalDocumentRequest{}).
		Where("legal_document_id = ? AND status IN ?", legalDocumentID, from).
		Updates(map[string]any{"status": to, "updated_at": time.Now()})
	return res.RowsAffected, res.Error
}

func (r *legalRequestRepository) CountByStatus(ctx context.Context) (map[string]int64, error) {
	var rows []struct {
		Status string
		N      int64
	}
	err := r.db.WithContext(ctx).Model(&types.LegalDocumentRequest{}).
		Select("status, COUNT(*) AS n").Group("status").Scan(&rows).Error
	out := map[string]int64{
		types.LegalRequestRequested: 0, types.LegalRequestFetched: 0, types.LegalRequestReviewing: 0,
		types.LegalRequestAdded: 0, types.LegalRequestRejected: 0, types.LegalRequestAutoIngest: 0,
	}
	for _, row := range rows {
		out[row.Status] = row.N
	}
	return out, err
}

func (r *legalRequestRepository) KnowledgeNamed(ctx context.Context, kbIDs []string, needles []string) (bool, error) {
	if len(kbIDs) == 0 || len(needles) == 0 {
		return false, nil
	}
	var conds []string
	var args []any
	for _, n := range needles {
		if n = strings.TrimSpace(n); n == "" {
			continue
		}
		like := "%" + strings.ToLower(n) + "%"
		conds = append(conds, "LOWER(title) LIKE ? OR LOWER(file_name) LIKE ?")
		args = append(args, like, like)
	}
	if len(conds) == 0 {
		return false, nil
	}
	var n int64
	err := r.db.WithContext(ctx).Table("knowledges").
		Where("knowledge_base_id IN ? AND deleted_at IS NULL", kbIDs).
		Where("("+strings.Join(conds, " OR ")+")", args...).
		Limit(1).Count(&n).Error
	return n > 0, err
}

func (r *legalRequestRepository) PublicKnowledgeByExternalIDs(ctx context.Context, ids []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(ids) == 0 {
		return out, nil
	}
	var found []string
	err := r.db.WithContext(ctx).Raw(`SELECT DISTINCT k.metadata->>'external_id' FROM knowledges k
		JOIN knowledge_bases kb ON kb.id = k.knowledge_base_id AND kb.owner_tenant_id = 0 AND kb.deleted_at IS NULL
		WHERE k.deleted_at IS NULL AND k.metadata->>'external_id' IN ?`, ids).Scan(&found).Error
	for _, id := range found {
		out[id] = true
	}
	return out, err
}

func (r *legalRequestRepository) PublicKnowledgeBases(ctx context.Context) ([]types.LegalPublicKB, error) {
	var kbs []types.LegalPublicKB
	err := r.db.WithContext(ctx).Table("knowledge_bases").Select("id, name").
		Where("owner_tenant_id = 0 AND deleted_at IS NULL").Order("name").Scan(&kbs).Error
	return kbs, err
}

func (r *legalRequestRepository) AddRequester(ctx context.Context, req *types.LegalRequestRequester) error {
	if req == nil || strings.TrimSpace(req.RequestID) == "" || strings.TrimSpace(req.UserID) == "" {
		return errors.New("legal request requester: request and user are required")
	}
	now := time.Now()
	row := *req
	if row.ID == "" {
		row.ID = uuid.NewString()
	}
	if row.MessageID != nil && strings.TrimSpace(*row.MessageID) == "" {
		row.MessageID = nil
	}
	row.NotifiedAt, row.NotifyStatus = nil, ""
	row.CreatedAt, row.UpdatedAt = now, now
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "request_id"}, {Name: "user_id"}},
		// asking again: the latest conversation is where the person waits,
		// and they are told again (they still miss the document)
		DoUpdates: clause.Assignments(map[string]any{
			"tenant_id": row.TenantID, "session_id": row.SessionID, "message_id": row.MessageID,
			"channel": row.Channel, "notified_at": nil, "notify_status": "", "updated_at": now,
		}),
	}).Create(&row).Error
}

func (r *legalRequestRepository) CountRequesters(ctx context.Context, requestIDs []string) (map[string]int, error) {
	out := map[string]int{}
	if len(requestIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		RequestID string
		N         int
	}
	err := r.db.WithContext(ctx).Model(&types.LegalRequestRequester{}).
		Select("request_id, COUNT(*) AS n").Where("request_id IN ?", requestIDs).
		Group("request_id").Scan(&rows).Error
	for _, row := range rows {
		out[row.RequestID] = row.N
	}
	return out, err
}

func (r *legalRequestRepository) PendingAddedNotices(
	ctx context.Context, decidedSince time.Time, limit int,
) ([]*types.LegalRequestAddedNotice, error) {
	if limit <= 0 {
		limit = 100
	}
	var rows []*types.LegalRequestAddedNotice
	err := r.db.WithContext(ctx).Table("legal_request_requesters AS q").
		Select(`q.id AS requester_id, q.request_id, q.tenant_id, q.user_id, q.session_id, q.channel,
			r.document_number, r.title, r.decided_at`).
		Joins("JOIN legal_document_requests r ON r.id = q.request_id").
		Where("q.notified_at IS NULL AND r.status = ? AND r.decided_at IS NOT NULL AND r.decided_at >= ?",
			types.LegalRequestAdded, decidedSince).
		Order("r.decided_at").Order("q.id").Limit(limit).Scan(&rows).Error
	return rows, err
}

func (r *legalRequestRepository) MarkRequesterNotified(
	ctx context.Context, requesterID, status string, at time.Time,
) (bool, error) {
	res := r.db.WithContext(ctx).Model(&types.LegalRequestRequester{}).
		Where("id = ? AND notified_at IS NULL", requesterID).
		Updates(map[string]any{"notified_at": at, "notify_status": status, "updated_at": at})
	return res.RowsAffected == 1, res.Error
}
