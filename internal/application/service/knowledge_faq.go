package service

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/application/service/retriever"
	werrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/google/uuid"
)

// ListFAQEntries lists FAQ entries under a FAQ knowledge base.
func (s *knowledgeService) ListFAQEntries(ctx context.Context,
	kbID string, page *types.Pagination, tagUUIDs []string, legacyTagSeqID int64, keyword string, searchField string, sortOrder string,
	isEnabled *bool,
) (*types.PageResult, error) {
	if page == nil {
		page = &types.Pagination{}
	}
	keyword = strings.TrimSpace(keyword)
	kb, err := s.validateFAQKnowledgeBase(ctx, kbID)
	if err != nil {
		return nil, err
	}

	effectiveTenantID, err := resolveKBReadTenant(ctx, kb, s.kbAccessGrantService)
	if err != nil {
		return nil, err
	}

	faqKnowledge, err := s.findFAQKnowledge(ctx, effectiveTenantID, kb.ID)
	if err != nil {
		return nil, err
	}
	if faqKnowledge == nil {
		return types.NewPageResult(0, page, []*types.FAQEntry{}), nil
	}

	if len(tagUUIDs) == 0 && legacyTagSeqID > 0 {
		tag, err := s.tagRepo.GetBySeqID(ctx, effectiveTenantID, legacyTagSeqID)
		if err != nil {
			return nil, werrors.NewNotFoundError("không tìm thấy thẻ")
		}
		tagUUIDs = []string{tag.ID}
	}

	chunkType := []types.ChunkType{types.ChunkTypeFAQ}
	chunks, total, err := s.chunkRepo.ListPagedChunksByKnowledgeID(
		ctx, effectiveTenantID, faqKnowledge.ID, page, chunkType, tagUUIDs, keyword, searchField, sortOrder, types.KnowledgeTypeFAQ,
		isEnabled,
	)
	if err != nil {
		return nil, err
	}

	// Build tag ID to name and seq_id mapping for all unique tag IDs (batch query)
	tagNameMap := make(map[string]string)
	tagSeqIDMap := make(map[string]int64)
	tagIDs := make([]string, 0)
	tagIDSet := make(map[string]struct{})
	for _, chunk := range chunks {
		if chunk.TagID != "" {
			if _, exists := tagIDSet[chunk.TagID]; !exists {
				tagIDSet[chunk.TagID] = struct{}{}
				tagIDs = append(tagIDs, chunk.TagID)
			}
		}
	}
	if len(tagIDs) > 0 {
		tags, err := s.tagRepo.GetByIDs(ctx, effectiveTenantID, tagIDs)
		if err == nil {
			for _, tag := range tags {
				tagNameMap[tag.ID] = tag.Name
				tagSeqIDMap[tag.ID] = tag.SeqID
			}
		}
	}

	kb.EnsureDefaults()
	entries := make([]*types.FAQEntry, 0, len(chunks))
	for _, chunk := range chunks {
		entry, err := s.chunkToFAQEntry(chunk, kb, tagSeqIDMap)
		if err != nil {
			return nil, err
		}
		// Set tag name from mapping
		if chunk.TagID != "" {
			entry.TagName = tagNameMap[chunk.TagID]
		}
		entries = append(entries, entry)
	}
	return types.NewPageResult(total, page, entries), nil
}

// faqCreateIndexBudget caps the indexing step of a single interactive FAQ
// create. Indexing embeds inline and the embedding call retries with
// exponential backoff, so a degraded embedding service can stretch one create
// past ten seconds — long enough for an impatient caller to resend and pile up
// concurrent creates. Failing fast is the better trade here: the caller can
// retry a clear error, whereas a request left hanging invites duplicates.
// Background and bulk indexing keep the full retry budget.
const faqCreateIndexBudget = 5 * time.Second

// CreateFAQEntry creates a single FAQ entry synchronously.
func (s *knowledgeService) CreateFAQEntry(ctx context.Context,
	kbID string, payload *types.FAQEntryPayload,
) (*types.FAQEntry, error) {
	if payload == nil {
		return nil, werrors.NewBadRequestError("nội dung yêu cầu không được để trống")
	}

	kb, ctx, err := s.writableFAQKnowledgeBase(ctx, kbID)
	if err != nil {
		return nil, err
	}
	kb.EnsureDefaults()

	tenantID := ctx.Value(types.TenantIDContextKey).(uint64)

	// Kiểm tra và làm sạch đầu vào
	meta, err := sanitizeFAQEntryPayload(payload)
	if err != nil {
		return nil, err
	}

	// Phân giải TagID
	tagID, err := s.resolveTagID(ctx, kbID, payload)
	if err != nil {
		return nil, err
	}

	// Tạo đồng thời cùng một câu hỏi chuẩn phải tuần tự: kiểm tra trùng bên dưới chỉ thấy mục đã ghi DB,
	// không chặn được request anh em còn đang đánh chỉ mục (retry do timeout phía trên gây ra kiểu đồng thời này).
	releaseGuard, err := s.acquireFAQCreateGuard(ctx, tenantID, kb.ID, meta.StandardQuestion)
	if err != nil {
		return nil, err
	}
	defer releaseGuard()

	// Kiểm tra câu hỏi chuẩn và câu hỏi tương tự có trùng với mục khác không
	if err := s.checkFAQQuestionDuplicate(ctx, tenantID, kb.ID, "", meta); err != nil {
		return nil, err
	}

	// Bảo đảm knowledge FAQ tồn tại
	faqKnowledge, err := s.ensureFAQKnowledge(ctx, tenantID, kb)
	if err != nil {
		return nil, fmt.Errorf("failed to ensure FAQ knowledge: %w", err)
	}

	// Lấy chế độ đánh chỉ mục
	indexMode := types.FAQIndexModeQuestionOnly
	if kb.FAQConfig != nil && kb.FAQConfig.IndexMode != "" {
		indexMode = kb.FAQConfig.IndexMode
	}

	// Lấy mô hình embedding
	embeddingModel, err := s.modelService.GetEmbeddingModel(ctx, kb.EmbeddingModelID)
	if err != nil {
		return nil, fmt.Errorf("failed to get embedding model: %w", err)
	}

	// Create chunk
	isEnabled := true
	if payload.IsEnabled != nil {
		isEnabled = *payload.IsEnabled
	}
	// Mặc định cho phép gợi ý
	flags := types.ChunkFlagRecommended
	if payload.IsRecommended != nil && !*payload.IsRecommended {
		flags = 0
	}

	chunk := &types.Chunk{
		ID:              uuid.New().String(),
		TenantID:        tenantID,
		KnowledgeID:     faqKnowledge.ID,
		KnowledgeBaseID: kb.ID,
		Content:         buildFAQChunkContent(meta, indexMode),
		IsEnabled:       isEnabled,
		Flags:           flags,
		ChunkType:       types.ChunkTypeFAQ,
		TagID:           tagID, // dùng TagID đã phân giải
		Status:          int(types.ChunkStatusStored),
	}
	// Nếu có chỉ định ID (dùng khi di trú dữ liệu) thì đặt SeqID
	if payload.ID != nil && *payload.ID > 0 {
		chunk.SeqID = *payload.ID
	}

	if err := chunk.SetFAQMetadata(meta); err != nil {
		return nil, fmt.Errorf("failed to set FAQ metadata: %w", err)
	}

	// Lưu chunk
	if err := s.chunkService.CreateChunks(ctx, []*types.Chunk{chunk}); err != nil {
		return nil, fmt.Errorf("failed to create chunk: %w", err)
	}

	// Đánh chỉ mục chunk: tạo tương tác đặt giới hạn cứng cho bước đánh chỉ mục, tránh embedding chập chờn kéo dài request
	indexCtx, cancelIndex := context.WithTimeout(ctx, faqCreateIndexBudget)
	indexErr := s.indexFAQChunks(indexCtx, kb, faqKnowledge, []*types.Chunk{chunk}, embeddingModel, true, false)
	cancelIndex()
	if indexErr != nil {
		// Nếu đánh chỉ mục thất bại thì xóa chunk đã tạo. Rollback thất bại sẽ để lại một bản ghi trạng thái stored
		// mồ côi: không hiện trong danh sách nhưng bị kiểm tra trùng bắt được, nên phải cảnh báo thay vì im lặng.
		if delErr := s.chunkService.DeleteChunk(ctx, chunk.ID); delErr != nil {
			logger.Errorf(ctx,
				"CreateFAQEntry: rollback failed, chunk %s left in stored state: %v", chunk.ID, delErr)
		}
		return nil, fmt.Errorf("failed to index chunk: %w", indexErr)
	}

	// Cập nhật trạng thái chunk thành đã đánh chỉ mục
	chunk.Status = int(types.ChunkStatusIndexed)
	if err := s.chunkService.UpdateChunk(ctx, chunk); err != nil {
		return nil, fmt.Errorf("failed to update chunk status: %w", err)
	}

	// Build tag seq_id map for conversion
	tagSeqIDMap := make(map[string]int64)
	if chunk.TagID != "" {
		tag, tagErr := s.tagRepo.GetByID(ctx, tenantID, chunk.TagID)
		if tagErr == nil && tag != nil {
			tagSeqIDMap[tag.ID] = tag.SeqID
		}
	}

	// Chuyển sang FAQEntry để trả về
	entry, err := s.chunkToFAQEntry(chunk, kb, tagSeqIDMap)
	if err != nil {
		return nil, err
	}

	// Query TagName
	if chunk.TagID != "" {
		tag, tagErr := s.tagRepo.GetByID(ctx, tenantID, chunk.TagID)
		if tagErr == nil && tag != nil {
			entry.TagName = tag.Name
		}
	}
	recordKBActivity(ctx, s.audit, tenantID, kb.ID, types.AuditActionKnowledgeCreated,
		"faq_entry", chunk.ID, types.AuditOutcomeSuccess,
		map[string]any{"entry_id": chunk.SeqID, "source_type": "faq"})

	return entry, nil
}

// GetFAQEntry retrieves a single FAQ entry by seq_id.
func (s *knowledgeService) GetFAQEntry(ctx context.Context,
	kbID string, entrySeqID int64,
) (*types.FAQEntry, error) {
	if entrySeqID <= 0 {
		return nil, werrors.NewBadRequestError("thiếu ID mục")
	}

	kb, err := s.validateFAQKnowledgeBase(ctx, kbID)
	if err != nil {
		return nil, err
	}
	kb.EnsureDefaults()

	tenantID := ctx.Value(types.TenantIDContextKey).(uint64)

	// Lấy chunk theo seq_id
	chunk, err := s.chunkRepo.GetChunkBySeqID(ctx, tenantID, entrySeqID)
	if err != nil {
		return nil, werrors.NewNotFoundError("không tìm thấy mục FAQ")
	}

	// Kiểm tra chunk thuộc kho tri thức hiện tại
	if chunk.KnowledgeBaseID != kb.ID || chunk.TenantID != tenantID {
		return nil, werrors.NewNotFoundError("không tìm thấy mục FAQ")
	}

	// Kiểm tra là loại FAQ
	if chunk.ChunkType != types.ChunkTypeFAQ {
		return nil, werrors.NewNotFoundError("không tìm thấy mục FAQ")
	}

	// Build tag seq_id map for conversion
	tagSeqIDMap := make(map[string]int64)
	if chunk.TagID != "" {
		tag, tagErr := s.tagRepo.GetByID(ctx, tenantID, chunk.TagID)
		if tagErr == nil && tag != nil {
			tagSeqIDMap[tag.ID] = tag.SeqID
		}
	}

	// Chuyển sang FAQEntry để trả về
	entry, err := s.chunkToFAQEntry(chunk, kb, tagSeqIDMap)
	if err != nil {
		return nil, err
	}

	// Query TagName
	if chunk.TagID != "" {
		tag, tagErr := s.tagRepo.GetByID(ctx, tenantID, chunk.TagID)
		if tagErr == nil && tag != nil {
			entry.TagName = tag.Name
		}
	}
	return entry, nil
}

// UpdateFAQEntry updates a single FAQ entry.
func (s *knowledgeService) UpdateFAQEntry(ctx context.Context,
	kbID string, entrySeqID int64, payload *types.FAQEntryPayload,
) (*types.FAQEntry, error) {
	if payload == nil {
		return nil, werrors.NewBadRequestError("nội dung yêu cầu không được để trống")
	}
	kb, ctx, err := s.writableFAQKnowledgeBase(ctx, kbID)
	if err != nil {
		return nil, err
	}
	kb.EnsureDefaults()
	tenantID := ctx.Value(types.TenantIDContextKey).(uint64)

	chunk, err := s.chunkRepo.GetChunkBySeqID(ctx, tenantID, entrySeqID)
	if err != nil {
		return nil, werrors.NewNotFoundError("không tìm thấy mục FAQ")
	}
	if chunk.KnowledgeBaseID != kb.ID {
		return nil, werrors.NewForbiddenError("không có quyền thao tác trên mục FAQ này")
	}
	if chunk.ChunkType != types.ChunkTypeFAQ {
		return nil, werrors.NewBadRequestError("chỉ hỗ trợ cập nhật mục FAQ")
	}
	meta, err := sanitizeFAQEntryPayload(payload)
	if err != nil {
		return nil, err
	}

	// Kiểm tra câu hỏi chuẩn và câu hỏi tương tự có trùng với mục khác không
	if err := s.checkFAQQuestionDuplicate(ctx, tenantID, kb.ID, chunk.ID, meta); err != nil {
		return nil, err
	}

	// Lấy danh sách câu hỏi tương tự cũ để cập nhật tăng dần
	var oldSimilarQuestions []string
	var oldStandardQuestion string
	var oldAnswers []string
	questionIndexMode := types.FAQQuestionIndexModeCombined
	if kb.FAQConfig != nil && kb.FAQConfig.QuestionIndexMode != "" {
		questionIndexMode = kb.FAQConfig.QuestionIndexMode
	}
	if existing, err := chunk.FAQMetadata(); err == nil && existing != nil {
		meta.Version = existing.Version + 1
		// Lưu nội dung cũ để so sánh tăng dần
		if questionIndexMode == types.FAQQuestionIndexModeSeparate {
			oldSimilarQuestions = existing.SimilarQuestions
			oldStandardQuestion = existing.StandardQuestion
			oldAnswers = existing.Answers
		}
	}
	if err := chunk.SetFAQMetadata(meta); err != nil {
		return nil, err
	}
	// Lấy chế độ đánh chỉ mục
	indexMode := types.FAQIndexModeQuestionOnly
	if kb.FAQConfig != nil && kb.FAQConfig.IndexMode != "" {
		indexMode = kb.FAQConfig.IndexMode
	}
	chunk.Content = buildFAQChunkContent(meta, indexMode)

	// Convert tag seq_id to UUID
	if payload.TagID > 0 {
		tag, tagErr := s.tagRepo.GetBySeqID(ctx, tenantID, payload.TagID)
		if tagErr != nil {
			return nil, werrors.NewNotFoundError("không tìm thấy thẻ")
		}
		chunk.TagID = tag.ID
	} else {
		chunk.TagID = ""
	}

	if payload.IsEnabled != nil {
		chunk.IsEnabled = *payload.IsEnabled
	}
	// Xử lý trạng thái gợi ý
	if payload.IsRecommended != nil {
		if *payload.IsRecommended {
			chunk.Flags = chunk.Flags.SetFlag(types.ChunkFlagRecommended)
		} else {
			chunk.Flags = chunk.Flags.ClearFlag(types.ChunkFlagRecommended)
		}
	}
	chunk.UpdatedAt = time.Now()
	if err := s.chunkService.UpdateChunk(ctx, chunk); err != nil {
		return nil, err
	}

	// Note: We don't need to call BatchUpdateChunkEnabledStatus here because
	// indexFAQChunks will delete old vectors and re-insert with the latest chunk data
	// (including the updated is_enabled status). Calling both would cause version conflicts.

	faqKnowledge, err := s.repo.GetKnowledgeByID(ctx, tenantID, chunk.KnowledgeID)
	if err != nil {
		return nil, err
	}

	embeddingModel, err := s.modelService.GetEmbeddingModel(ctx, kb.EmbeddingModelID)
	if err != nil {
		return nil, err
	}

	// Tối ưu chỉ mục tăng dần: chỉ đánh chỉ mục phần nội dung thay đổi
	if questionIndexMode == types.FAQQuestionIndexModeSeparate && len(oldSimilarQuestions) > 0 {
		// Cập nhật tăng dần ở chế độ đánh chỉ mục riêng rẽ
		if err := s.incrementalIndexFAQEntry(ctx, kb, faqKnowledge, chunk, embeddingModel,
			oldStandardQuestion, oldSimilarQuestions, oldAnswers, meta); err != nil {
			return nil, err
		}
	} else {
		// Chế độ Combined hoặc tạo lần đầu: đánh chỉ mục toàn bộ
		// Xóa tăng dần: chỉ xóa chỉ mục của câu hỏi tương tự bị gỡ
		oldSimilarQuestionCount := len(oldSimilarQuestions)
		newSimilarQuestionCount := len(meta.SimilarQuestions)
		if questionIndexMode == types.FAQQuestionIndexModeSeparate && oldSimilarQuestionCount > newSimilarQuestionCount {
			retrieveEngine, engineErr := retriever.CreateRetrieveEngineForKB(
				ctx, s.retrieveEngine, s.ownership, types.MustTenantIDFromContext(ctx), kb.VectorStoreID)
			if engineErr == nil {
				sourceIDsToDelete := make([]string, 0, oldSimilarQuestionCount-newSimilarQuestionCount)
				for i := newSimilarQuestionCount; i < oldSimilarQuestionCount; i++ {
					sourceIDsToDelete = append(sourceIDsToDelete, fmt.Sprintf("%s-%d", chunk.ID, i))
				}
				if len(sourceIDsToDelete) > 0 {
					logger.Debugf(ctx, "UpdateFAQEntry: incremental delete %d obsolete source IDs", len(sourceIDsToDelete))
					if delErr := retrieveEngine.DeleteBySourceIDList(ctx, sourceIDsToDelete, embeddingModel.GetDimensions(), types.KnowledgeTypeFAQ); delErr != nil {
						logger.Warnf(ctx, "UpdateFAQEntry: failed to delete obsolete source IDs: %v", delErr)
					}
				}
			}
		}

		// Dùng needDelete=false vì EFPutDocument tự ghi đè tài liệu cùng SourceID
		if err := s.indexFAQChunks(ctx, kb, faqKnowledge, []*types.Chunk{chunk}, embeddingModel, false, false); err != nil {
			return nil, err
		}
	}

	// Build tag seq_id map for conversion
	tagSeqIDMap := make(map[string]int64)
	if chunk.TagID != "" {
		tag, tagErr := s.tagRepo.GetByID(ctx, tenantID, chunk.TagID)
		if tagErr == nil && tag != nil {
			tagSeqIDMap[tag.ID] = tag.SeqID
		}
	}

	// Chuyển sang FAQEntry để trả về
	entry, err := s.chunkToFAQEntry(chunk, kb, tagSeqIDMap)
	if err != nil {
		return nil, err
	}

	// Query TagName
	if chunk.TagID != "" {
		tag, tagErr := s.tagRepo.GetByID(ctx, tenantID, chunk.TagID)
		if tagErr == nil && tag != nil {
			entry.TagName = tag.Name
		}
	}
	recordKBActivity(ctx, s.audit, tenantID, kb.ID, types.AuditActionKnowledgeUpdated,
		"faq_entry", chunk.ID, types.AuditOutcomeSuccess,
		map[string]any{"entry_id": chunk.SeqID, "source_type": "faq"})

	return entry, nil
}

// AddSimilarQuestions adds similar questions to a FAQ entry.
// This will append the new questions to the existing similar questions list.
func (s *knowledgeService) AddSimilarQuestions(ctx context.Context,
	kbID string, entrySeqID int64, questions []string,
) (*types.FAQEntry, error) {
	if len(questions) == 0 {
		return nil, werrors.NewBadRequestError("danh sách câu hỏi tương tự không được để trống")
	}

	kb, ctx, err := s.writableFAQKnowledgeBase(ctx, kbID)
	if err != nil {
		return nil, err
	}
	kb.EnsureDefaults()
	tenantID := ctx.Value(types.TenantIDContextKey).(uint64)

	// Get existing FAQ entry
	chunk, err := s.chunkRepo.GetChunkBySeqID(ctx, tenantID, entrySeqID)
	if err != nil {
		return nil, werrors.NewNotFoundError("không tìm thấy mục FAQ")
	}
	if chunk.KnowledgeBaseID != kb.ID {
		return nil, werrors.NewForbiddenError("không có quyền thao tác trên mục FAQ này")
	}
	if chunk.ChunkType != types.ChunkTypeFAQ {
		return nil, werrors.NewBadRequestError("chỉ hỗ trợ cập nhật mục FAQ")
	}

	// Get existing metadata
	meta, err := chunk.FAQMetadata()
	if err != nil || meta == nil {
		return nil, werrors.NewBadRequestError("lấy metadata FAQ thất bại")
	}

	// Deduplicate and sanitize new questions
	existingSet := make(map[string]struct{})
	for _, q := range meta.SimilarQuestions {
		existingSet[q] = struct{}{}
	}
	// Also add standard question to prevent duplicates
	existingSet[meta.StandardQuestion] = struct{}{}

	newQuestions := make([]string, 0, len(questions))
	for _, q := range questions {
		q = strings.TrimSpace(q)
		if q == "" {
			continue
		}
		if _, exists := existingSet[q]; exists {
			continue
		}
		existingSet[q] = struct{}{}
		newQuestions = append(newQuestions, q)
	}

	if len(newQuestions) == 0 {
		// No new questions to add, return current entry
		tagSeqIDMap := make(map[string]int64)
		if chunk.TagID != "" {
			tag, tagErr := s.tagRepo.GetByID(ctx, tenantID, chunk.TagID)
			if tagErr == nil && tag != nil {
				tagSeqIDMap[tag.ID] = tag.SeqID
			}
		}
		return s.chunkToFAQEntry(chunk, kb, tagSeqIDMap)
	}

	// Check for duplicates with other entries
	tempMeta := &types.FAQChunkMetadata{
		StandardQuestion: meta.StandardQuestion,
		SimilarQuestions: append(meta.SimilarQuestions, newQuestions...),
	}
	if err := s.checkFAQQuestionDuplicate(ctx, tenantID, kb.ID, chunk.ID, tempMeta); err != nil {
		return nil, err
	}

	// Update metadata
	oldSimilarQuestions := meta.SimilarQuestions
	meta.SimilarQuestions = append(meta.SimilarQuestions, newQuestions...)
	meta.Version++

	if err := chunk.SetFAQMetadata(meta); err != nil {
		return nil, err
	}

	// Update chunk content
	indexMode := types.FAQIndexModeQuestionOnly
	if kb.FAQConfig != nil && kb.FAQConfig.IndexMode != "" {
		indexMode = kb.FAQConfig.IndexMode
	}
	chunk.Content = buildFAQChunkContent(meta, indexMode)
	chunk.UpdatedAt = time.Now()

	if err := s.chunkService.UpdateChunk(ctx, chunk); err != nil {
		return nil, err
	}

	// Index new similar questions
	faqKnowledge, err := s.repo.GetKnowledgeByID(ctx, tenantID, chunk.KnowledgeID)
	if err != nil {
		return nil, err
	}

	embeddingModel, err := s.modelService.GetEmbeddingModel(ctx, kb.EmbeddingModelID)
	if err != nil {
		return nil, err
	}

	questionIndexMode := types.FAQQuestionIndexModeCombined
	if kb.FAQConfig != nil && kb.FAQConfig.QuestionIndexMode != "" {
		questionIndexMode = kb.FAQConfig.QuestionIndexMode
	}

	if questionIndexMode == types.FAQQuestionIndexModeSeparate {
		// Only index the new similar questions
		if err := s.incrementalIndexFAQEntry(ctx, kb, faqKnowledge, chunk, embeddingModel,
			meta.StandardQuestion, oldSimilarQuestions, meta.Answers, meta); err != nil {
			return nil, err
		}
	} else {
		// Combined mode, re-index the whole entry
		if err := s.indexFAQChunks(ctx, kb, faqKnowledge, []*types.Chunk{chunk}, embeddingModel, false, false); err != nil {
			return nil, err
		}
	}

	// Build response
	tagSeqIDMap := make(map[string]int64)
	if chunk.TagID != "" {
		tag, tagErr := s.tagRepo.GetByID(ctx, tenantID, chunk.TagID)
		if tagErr == nil && tag != nil {
			tagSeqIDMap[tag.ID] = tag.SeqID
		}
	}

	entry, err := s.chunkToFAQEntry(chunk, kb, tagSeqIDMap)
	if err != nil {
		return nil, err
	}

	if chunk.TagID != "" {
		tag, tagErr := s.tagRepo.GetByID(ctx, tenantID, chunk.TagID)
		if tagErr == nil && tag != nil {
			entry.TagName = tag.Name
		}
	}

	return entry, nil
}

// UpdateFAQEntryStatus updates enable status for a FAQ entry.
func (s *knowledgeService) UpdateFAQEntryStatus(ctx context.Context,
	kbID string, entryID string, isEnabled bool,
) error {
	kb, ctx, err := s.writableFAQKnowledgeBase(ctx, kbID)
	if err != nil {
		return err
	}
	tenantID := ctx.Value(types.TenantIDContextKey).(uint64)
	chunk, err := s.chunkRepo.GetChunkByID(ctx, tenantID, entryID)
	if err != nil {
		return err
	}
	if chunk.KnowledgeBaseID != kb.ID || chunk.ChunkType != types.ChunkTypeFAQ {
		return werrors.NewBadRequestError("chỉ hỗ trợ cập nhật mục FAQ")
	}
	if chunk.IsEnabled == isEnabled {
		return nil
	}
	chunk.IsEnabled = isEnabled
	chunk.UpdatedAt = time.Now()
	if err := s.chunkService.UpdateChunk(ctx, chunk); err != nil {
		return err
	}

	// Sync update to retriever engines
	chunkStatusMap := map[string]bool{chunk.ID: isEnabled}
	retrieveEngine, err := retriever.CreateRetrieveEngineForKB(
		ctx, s.retrieveEngine, s.ownership, tenantID, kb.VectorStoreID)
	if err != nil {
		return err
	}
	if err := retrieveEngine.BatchUpdateChunkEnabledStatus(ctx, chunkStatusMap); err != nil {
		return err
	}
	recordKBActivity(ctx, s.audit, tenantID, kb.ID, types.AuditActionKnowledgeUpdated,
		"faq_entry", chunk.ID, types.AuditOutcomeSuccess,
		map[string]any{"entry_id": chunk.SeqID, "changed_fields": []string{"enabled"}})

	return nil
}

// UpdateFAQEntryFieldsBatch updates multiple fields for FAQ entries in batch.
// This is the unified API for batch updating FAQ entry fields.
// Supports two modes:
// 1. By entry seq_id: use ByID field
// 2. By Tag seq_id: use ByTag field to apply the same update to all entries under a tag
func (s *knowledgeService) UpdateFAQEntryFieldsBatch(ctx context.Context,
	kbID string, req *types.FAQEntryFieldsBatchUpdate,
) error {
	if req == nil || (len(req.ByID) == 0 && len(req.ByTag) == 0) {
		return nil
	}
	kb, ctx, err := s.writableFAQKnowledgeBase(ctx, kbID)
	if err != nil {
		return err
	}
	tenantID := ctx.Value(types.TenantIDContextKey).(uint64)

	enabledUpdates := make(map[string]bool)
	tagUpdates := make(map[string]string)

	plan, err := s.planFAQFields(ctx, kb, req)
	if err != nil {
		return err
	}
	excludeUUIDs := plan.excludeIDs

	// Handle ByTag updates first (by tag seq_id)
	if len(req.ByTag) > 0 {
		for _, tagSeqID := range sortedFAQIDs(req.ByTag) {
			update := req.ByTag[tagSeqID]
			tag := plan.tags[tagSeqID]

			var setFlags, clearFlags types.ChunkFlags

			// Handle IsRecommended
			if update.IsRecommended != nil {
				if *update.IsRecommended {
					setFlags = types.ChunkFlagRecommended
				} else {
					clearFlags = types.ChunkFlagRecommended
				}
			}

			// Convert new tag seq_id to UUID if provided
			var newTagUUID *string
			if update.TagID != nil {
				if *update.TagID > 0 {
					newTag := plan.tags[*update.TagID]
					newTagUUID = &newTag.ID
				} else {
					emptyStr := ""
					newTagUUID = &emptyStr
				}
			}

			// Update all chunks with this tag
			affectedIDs, err := s.chunkRepo.UpdateChunkFieldsByTagID(
				ctx, tenantID, kb.ID, tag.ID,
				update.IsEnabled, setFlags, clearFlags, newTagUUID, excludeUUIDs,
			)
			if err != nil {
				return err
			}

			// Preserve group changes when an explicit ByID patch follows it.
			for _, id := range affectedIDs {
				if chunk := plan.chunksByID[id]; chunk != nil {
					if update.IsEnabled != nil {
						chunk.IsEnabled = *update.IsEnabled
					}
					chunk.Flags = (chunk.Flags | setFlags) &^ clearFlags
					if newTagUUID != nil {
						chunk.TagID = *newTagUUID
					}
				}
			}

			// Collect affected IDs for retriever sync
			if len(affectedIDs) > 0 {
				if update.IsEnabled != nil {
					for _, id := range affectedIDs {
						enabledUpdates[id] = *update.IsEnabled
					}
				}
				if newTagUUID != nil {
					for _, id := range affectedIDs {
						tagUpdates[id] = *newTagUUID
					}
				}
			}
		}
	}

	// Handle ByID updates (by entry seq_id)
	if len(req.ByID) > 0 {
		chunkBySeqID := plan.chunks

		setFlags := make(map[string]types.ChunkFlags)
		clearFlags := make(map[string]types.ChunkFlags)
		chunksToUpdate := make([]*types.Chunk, 0)

		for _, entrySeqID := range sortedFAQIDs(req.ByID) {
			update := req.ByID[entrySeqID]
			chunk := chunkBySeqID[entrySeqID]

			needUpdate := false

			// Handle IsEnabled
			if update.IsEnabled != nil && chunk.IsEnabled != *update.IsEnabled {
				chunk.IsEnabled = *update.IsEnabled
				enabledUpdates[chunk.ID] = *update.IsEnabled
				needUpdate = true
			}

			// Handle IsRecommended (via Flags)
			if update.IsRecommended != nil {
				currentRecommended := chunk.Flags.HasFlag(types.ChunkFlagRecommended)
				if currentRecommended != *update.IsRecommended {
					if *update.IsRecommended {
						setFlags[chunk.ID] = types.ChunkFlagRecommended
					} else {
						clearFlags[chunk.ID] = types.ChunkFlagRecommended
					}
				}
			}

			// Handle TagID (convert seq_id to UUID)
			if update.TagID != nil {
				var newTagID string
				if *update.TagID > 0 {
					newTag := plan.tags[*update.TagID]
					newTagID = newTag.ID
				}
				if chunk.TagID != newTagID {
					chunk.TagID = newTagID
					tagUpdates[chunk.ID] = newTagID
					needUpdate = true
				}
			}

			if needUpdate {
				chunk.UpdatedAt = time.Now()
				chunksToUpdate = append(chunksToUpdate, chunk)
			}
		}

		// Batch update chunks (for IsEnabled and TagID)
		if len(chunksToUpdate) > 0 {
			if err := s.chunkRepo.UpdateChunks(ctx, chunksToUpdate); err != nil {
				return err
			}
		}

		// Batch update flags (for IsRecommended)
		if len(setFlags) > 0 || len(clearFlags) > 0 {
			if err := s.chunkRepo.UpdateChunkFlagsBatch(ctx, tenantID, kb.ID, setFlags, clearFlags); err != nil {
				return err
			}
		}
	}

	// Sync to retriever engines
	if len(enabledUpdates) > 0 || len(tagUpdates) > 0 {
		retrieveEngine, err := retriever.CreateRetrieveEngineForKB(
			ctx, s.retrieveEngine, s.ownership, tenantID, kb.VectorStoreID)
		if err != nil {
			return err
		}
		if len(enabledUpdates) > 0 {
			if err := retrieveEngine.BatchUpdateChunkEnabledStatus(ctx, enabledUpdates); err != nil {
				return err
			}
		}
		if len(tagUpdates) > 0 {
			if err := retrieveEngine.BatchUpdateChunkTagID(ctx, tagUpdates); err != nil {
				return err
			}
		}
	}
	recordKBActivity(ctx, s.audit, tenantID, kb.ID, types.AuditActionKnowledgeUpdated,
		"faq_entry", "", types.AuditOutcomeSuccess,
		map[string]any{"count": len(req.ByID), "tag_groups": len(req.ByTag), "batch": true})

	return nil
}

// UpdateFAQEntryTag updates the tag assigned to an FAQ entry.
func (s *knowledgeService) UpdateFAQEntryTag(ctx context.Context, kbID string, entryID string, tagID *string) error {
	kb, ctx, err := s.writableFAQKnowledgeBase(ctx, kbID)
	if err != nil {
		return err
	}
	tenantID := ctx.Value(types.TenantIDContextKey).(uint64)
	chunk, err := s.chunkRepo.GetChunkByID(ctx, tenantID, entryID)
	if err != nil {
		return err
	}
	if chunk.KnowledgeBaseID != kb.ID || chunk.ChunkType != types.ChunkTypeFAQ {
		return werrors.NewBadRequestError("chỉ hỗ trợ cập nhật thẻ của mục FAQ")
	}

	var resolvedTagID string
	if tagID != nil && *tagID != "" {
		tag, err := s.tagRepo.GetByID(ctx, tenantID, *tagID)
		if err != nil {
			return err
		}
		if tag.KnowledgeBaseID != kb.ID {
			return werrors.NewBadRequestError("thẻ không thuộc kho tri thức hiện tại")
		}
		resolvedTagID = tag.ID
	}

	// Check if tag actually changed
	if chunk.TagID == resolvedTagID {
		return nil
	}

	chunk.TagID = resolvedTagID
	chunk.UpdatedAt = time.Now()
	if err := s.chunkRepo.UpdateChunk(ctx, chunk); err != nil {
		return err
	}

	// Sync tag update to retriever engines
	retrieveEngine, err := retriever.CreateRetrieveEngineForKB(
		ctx, s.retrieveEngine, s.ownership, tenantID, kb.VectorStoreID)
	if err != nil {
		return err
	}
	return retrieveEngine.BatchUpdateChunkTagID(ctx, map[string]string{chunk.ID: resolvedTagID})
}

// UpdateFAQEntryTagBatch updates tags for FAQ entries in batch.
// Key: entry seq_id, Value: tag seq_id (nil to remove tag)
func (s *knowledgeService) UpdateFAQEntryTagBatch(ctx context.Context, kbID string, updates map[int64]*int64) error {
	req := &types.FAQEntryFieldsBatchUpdate{ByID: make(map[int64]types.FAQEntryFieldsUpdate, len(updates))}
	for id, tag := range updates {
		value := int64(0) // nil in the tag API means remove the tag.
		if tag != nil {
			value = *tag
		}
		req.ByID[id] = types.FAQEntryFieldsUpdate{TagID: &value}
	}
	return s.UpdateFAQEntryFieldsBatch(ctx, kbID, req)
}

// SearchFAQEntries searches FAQ entries using hybrid search.
func (s *knowledgeService) SearchFAQEntries(ctx context.Context,
	kbID string, req *types.FAQSearchRequest,
) ([]*types.FAQEntry, error) {
	// Validate FAQ knowledge base
	kb, err := s.validateFAQKnowledgeBase(ctx, kbID)
	if err != nil {
		return nil, err
	}

	// Set default values
	if req.VectorThreshold <= 0 {
		req.VectorThreshold = 0.7
	}
	if req.MatchCount <= 0 {
		req.MatchCount = 10
	}
	if req.MatchCount > 50 {
		req.MatchCount = 50
	}

	tenantID := ctx.Value(types.TenantIDContextKey).(uint64)

	// Convert tag seq_ids to UUIDs
	var firstPriorityTagUUIDs, secondPriorityTagUUIDs []string
	firstPrioritySeqIDSet := make(map[int64]struct{})
	secondPrioritySeqIDSet := make(map[int64]struct{})

	if len(req.FirstPriorityTagIDs) > 0 {
		tags, err := s.tagRepo.GetBySeqIDs(ctx, tenantID, req.FirstPriorityTagIDs)
		if err == nil {
			firstPriorityTagUUIDs = make([]string, 0, len(tags))
			for _, tag := range tags {
				firstPriorityTagUUIDs = append(firstPriorityTagUUIDs, tag.ID)
				firstPrioritySeqIDSet[tag.SeqID] = struct{}{}
			}
		}
	}
	if len(req.SecondPriorityTagIDs) > 0 {
		tags, err := s.tagRepo.GetBySeqIDs(ctx, tenantID, req.SecondPriorityTagIDs)
		if err == nil {
			secondPriorityTagUUIDs = make([]string, 0, len(tags))
			for _, tag := range tags {
				secondPriorityTagUUIDs = append(secondPriorityTagUUIDs, tag.ID)
				secondPrioritySeqIDSet[tag.SeqID] = struct{}{}
			}
		}
	}

	// Build priority tag sets for sorting (using UUID)
	hasFirstPriority := len(firstPriorityTagUUIDs) > 0
	hasSecondPriority := len(secondPriorityTagUUIDs) > 0
	hasPriorityFilter := hasFirstPriority || hasSecondPriority

	firstPrioritySet := make(map[string]struct{}, len(firstPriorityTagUUIDs))
	for _, tagID := range firstPriorityTagUUIDs {
		firstPrioritySet[tagID] = struct{}{}
	}
	secondPrioritySet := make(map[string]struct{}, len(secondPriorityTagUUIDs))
	for _, tagID := range secondPriorityTagUUIDs {
		secondPrioritySet[tagID] = struct{}{}
	}

	// Perform separate searches for each priority level to ensure FirstPriority results
	// are not crowded out by higher-scoring SecondPriority results in TopK truncation
	var searchResults []*types.SearchResult

	if hasPriorityFilter {
		// Use goroutines to search both priority levels concurrently
		var (
			firstResults  []*types.SearchResult
			secondResults []*types.SearchResult
			firstErr      error
			secondErr     error
			wg            sync.WaitGroup
		)

		if hasFirstPriority {
			wg.Add(1)
			go func() {
				defer wg.Done()
				firstParams := types.SearchParams{
					QueryText:            secutils.SanitizeForLog(req.QueryText),
					VectorThreshold:      req.VectorThreshold,
					MatchCount:           req.MatchCount,
					DisableKeywordsMatch: true,
					TagIDs:               firstPriorityTagUUIDs,
					OnlyRecommended:      req.OnlyRecommended,
				}
				firstResults, firstErr = s.kbService.HybridSearch(ctx, kbID, firstParams)
			}()
		}

		if hasSecondPriority {
			wg.Add(1)
			go func() {
				defer wg.Done()
				secondParams := types.SearchParams{
					QueryText:            secutils.SanitizeForLog(req.QueryText),
					VectorThreshold:      req.VectorThreshold,
					MatchCount:           req.MatchCount,
					DisableKeywordsMatch: true,
					TagIDs:               secondPriorityTagUUIDs,
					OnlyRecommended:      req.OnlyRecommended,
				}
				secondResults, secondErr = s.kbService.HybridSearch(ctx, kbID, secondParams)
			}()
		}

		wg.Wait()

		// Check errors
		if firstErr != nil {
			return nil, firstErr
		}
		if secondErr != nil {
			return nil, secondErr
		}

		// Merge results: FirstPriority first, then SecondPriority (deduplicated)
		seenChunkIDs := make(map[string]struct{})
		for _, result := range firstResults {
			if _, exists := seenChunkIDs[result.ID]; !exists {
				seenChunkIDs[result.ID] = struct{}{}
				searchResults = append(searchResults, result)
			}
		}
		for _, result := range secondResults {
			if _, exists := seenChunkIDs[result.ID]; !exists {
				seenChunkIDs[result.ID] = struct{}{}
				searchResults = append(searchResults, result)
			}
		}
	} else {
		// No priority filter, search all
		searchParams := types.SearchParams{
			QueryText:            secutils.SanitizeForLog(req.QueryText),
			VectorThreshold:      req.VectorThreshold,
			MatchCount:           req.MatchCount,
			DisableKeywordsMatch: true,
		}
		var err error
		searchResults, err = s.kbService.HybridSearch(ctx, kbID, searchParams)
		if err != nil {
			return nil, err
		}
	}

	if len(searchResults) == 0 {
		return []*types.FAQEntry{}, nil
	}

	// Extract chunk IDs and build score/match type/matched content maps
	chunkIDs := make([]string, 0, len(searchResults))
	chunkScores := make(map[string]float64)
	chunkMatchTypes := make(map[string]types.MatchType)
	chunkMatchedContents := make(map[string]string)
	for _, result := range searchResults {
		// SearchResult.ID is the chunk ID
		chunkID := result.ID
		chunkIDs = append(chunkIDs, chunkID)
		chunkScores[chunkID] = result.Score
		chunkMatchTypes[chunkID] = result.MatchType
		chunkMatchedContents[chunkID] = result.MatchedContent
	}

	// Batch fetch chunks
	chunks, err := s.chunkRepo.ListChunksByID(ctx, tenantID, chunkIDs)
	if err != nil {
		return nil, err
	}

	// Build tag UUID to seq_id map for conversion
	tagSeqIDMap := make(map[string]int64)
	tagIDs := make([]string, 0)
	tagIDSet := make(map[string]struct{})
	for _, chunk := range chunks {
		if chunk.TagID != "" {
			if _, exists := tagIDSet[chunk.TagID]; !exists {
				tagIDSet[chunk.TagID] = struct{}{}
				tagIDs = append(tagIDs, chunk.TagID)
			}
		}
	}
	if len(tagIDs) > 0 {
		tags, err := s.tagRepo.GetByIDs(ctx, tenantID, tagIDs)
		if err == nil {
			for _, tag := range tags {
				tagSeqIDMap[tag.ID] = tag.SeqID
			}
		}
	}

	// Filter FAQ chunks and convert to FAQEntry
	kb.EnsureDefaults()
	entries := make([]*types.FAQEntry, 0, len(chunks))
	for _, chunk := range chunks {
		// Only process FAQ type chunks
		if chunk.ChunkType != types.ChunkTypeFAQ {
			continue
		}
		if !chunk.IsEnabled {
			continue
		}

		entry, err := s.chunkToFAQEntry(chunk, kb, tagSeqIDMap)
		if err != nil {
			logger.Warnf(ctx, "Failed to convert chunk to FAQ entry: %v", err)
			continue
		}

		// Preserve score and match type from search results
		// Note: Negative question filtering is now handled in HybridSearch
		if score, ok := chunkScores[chunk.ID]; ok {
			entry.Score = score
		}
		if matchType, ok := chunkMatchTypes[chunk.ID]; ok {
			entry.MatchType = matchType
		}

		// Set MatchedQuestion from search result's matched content
		if matchedContent, ok := chunkMatchedContents[chunk.ID]; ok && matchedContent != "" {
			entry.MatchedQuestion = matchedContent
		}

		entries = append(entries, entry)
	}

	// Sort entries with two-level priority tag support
	if hasPriorityFilter {
		// getPriorityLevel returns: 0 = first priority, 1 = second priority, 2 = no priority
		// Use chunk.TagID (UUID) for comparison
		getPriorityLevel := func(chunk *types.Chunk) int {
			if _, ok := firstPrioritySet[chunk.TagID]; ok {
				return 0
			}
			if _, ok := secondPrioritySet[chunk.TagID]; ok {
				return 1
			}
			return 2
		}

		// Build chunk map for priority lookup
		chunkMap := make(map[int64]*types.Chunk)
		for _, chunk := range chunks {
			chunkMap[chunk.SeqID] = chunk
		}

		slices.SortFunc(entries, func(a, b *types.FAQEntry) int {
			aChunk := chunkMap[a.ID]
			bChunk := chunkMap[b.ID]
			var aPriority, bPriority int
			if aChunk != nil {
				aPriority = getPriorityLevel(aChunk)
			} else {
				aPriority = 2
			}
			if bChunk != nil {
				bPriority = getPriorityLevel(bChunk)
			} else {
				bPriority = 2
			}

			// Compare by priority level first
			if aPriority != bPriority {
				return aPriority - bPriority // Lower level = higher priority
			}

			// Same priority level, sort by score descending
			if b.Score > a.Score {
				return 1
			} else if b.Score < a.Score {
				return -1
			}
			return 0
		})
	} else {
		// No priority tags, sort by score only
		slices.SortFunc(entries, func(a, b *types.FAQEntry) int {
			if b.Score > a.Score {
				return 1
			} else if b.Score < a.Score {
				return -1
			}
			return 0
		})
	}

	// Limit results to requested match count
	if len(entries) > req.MatchCount {
		entries = entries[:req.MatchCount]
	}

	// Truy vấn TagName theo lô và bổ sung vào kết quả
	if len(entries) > 0 {
		// Gom mọi TagID (seq_id) cần truy vấn
		tagSeqIDs := make([]int64, 0)
		tagSeqIDSet := make(map[int64]struct{})
		for _, entry := range entries {
			if entry.TagID != 0 {
				if _, exists := tagSeqIDSet[entry.TagID]; !exists {
					tagSeqIDs = append(tagSeqIDs, entry.TagID)
					tagSeqIDSet[entry.TagID] = struct{}{}
				}
			}
		}

		// Truy vấn thẻ theo lô
		if len(tagSeqIDs) > 0 {
			tags, err := s.tagRepo.GetBySeqIDs(ctx, tenantID, tagSeqIDs)
			if err != nil {
				logger.Warnf(ctx, "Failed to batch query tags: %v", err)
			} else {
				// Dựng ánh xạ TagSeqID -> TagName
				tagNameMap := make(map[int64]string)
				for _, tag := range tags {
					tagNameMap[tag.SeqID] = tag.Name
				}

				// Bổ sung TagName
				for _, entry := range entries {
					if entry.TagID != 0 {
						if tagName, exists := tagNameMap[entry.TagID]; exists {
							entry.TagName = tagName
						}
					}
				}
			}
		}
	}

	return entries, nil
}

// DeleteFAQEntries deletes FAQ entries in batch by seq_id.
func (s *knowledgeService) DeleteFAQEntries(ctx context.Context,
	kbID string, entrySeqIDs []int64,
) error {
	if len(entrySeqIDs) == 0 {
		return werrors.NewBadRequestError("vui lòng chọn mục FAQ cần xóa")
	}
	kb, ctx, err := s.writableFAQKnowledgeBase(ctx, kbID)
	if err != nil {
		return err
	}

	tenantID := ctx.Value(types.TenantIDContextKey).(uint64)
	selected, err := s.loadFAQWriteChunks(ctx, kb, entrySeqIDs)
	if err != nil {
		return err
	}
	chunksToRemove := make([]*types.Chunk, 0, len(selected))
	knowledges := make(map[string]*types.Knowledge)
	groups := make(map[string][]*types.Chunk)
	for _, id := range sortedFAQIDs(selected) {
		chunk := selected[id]
		if knowledges[chunk.KnowledgeID] == nil {
			knowledge, err := s.repo.GetKnowledgeByID(ctx, tenantID, chunk.KnowledgeID)
			if err != nil {
				return err
			}
			if knowledge == nil || knowledge.TenantID != tenantID || knowledge.KnowledgeBaseID != kb.ID ||
				knowledge.Type != types.KnowledgeTypeFAQ {
				return werrors.NewForbiddenError("tài liệu FAQ không thuộc kho tri thức hiện tại")
			}
			knowledges[chunk.KnowledgeID] = knowledge
		}
		groups[chunk.KnowledgeID] = append(groups[chunk.KnowledgeID], chunk)
		chunksToRemove = append(chunksToRemove, chunk)
	}
	// All entries and parent documents are authorized before any deletion.
	for _, chunk := range chunksToRemove {
		if err := s.chunkService.DeleteChunk(ctx, chunk.ID); err != nil {
			return err
		}
	}
	for id, chunks := range groups {
		if err := s.deleteFAQChunkVectors(ctx, kb, knowledges[id], chunks); err != nil {
			return err
		}
	}

	details := map[string]any{"count": len(chunksToRemove), "source_type": "faq"}
	titles := make([]string, 0, len(chunksToRemove))
	for _, chunk := range chunksToRemove {
		titles = append(titles, faqChunkQuestion(chunk))
	}
	kbActivityAppendSampleTitles(details, titles...)
	recordKBActivity(ctx, s.audit, tenantID, kb.ID, types.AuditActionKnowledgeBatchDeleted,
		"faq_entry", "", types.AuditOutcomeSuccess, details)
	return nil
}

// ExportFAQEntries exports all FAQ entries for a knowledge base as CSV data.
// The CSV format matches the import example format with 8 columns:
// Phân loại(bắt buộc), Câu hỏi(bắt buộc), Câu hỏi tương tự(tùy chọn-nhiều giá trị cách nhau bằng ##), Câu hỏi phản ví dụ(tùy chọn-nhiều giá trị cách nhau bằng ##),
// Câu trả lời(bắt buộc-nhiều giá trị cách nhau bằng ##), Trả lời tất cả(tùy chọn-mặc định FALSE), Ngừng dùng(tùy chọn-mặc định FALSE),
// Không gợi ý(tùy chọn-mặc định FALSE, được gợi ý)
func (s *knowledgeService) ExportFAQEntries(ctx context.Context, kbID string) ([]byte, error) {
	kb, err := s.validateFAQKnowledgeBase(ctx, kbID)
	if err != nil {
		return nil, err
	}

	tenantID := ctx.Value(types.TenantIDContextKey).(uint64)
	faqKnowledge, err := s.findFAQKnowledge(ctx, tenantID, kb.ID)
	if err != nil {
		return nil, err
	}
	if faqKnowledge == nil {
		// Return empty CSV with headers only
		return s.buildFAQCSV(nil, nil), nil
	}

	// Get all FAQ chunks
	chunks, err := s.chunkRepo.ListAllFAQChunksForExport(ctx, tenantID, faqKnowledge.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to list FAQ chunks: %w", err)
	}

	// Build tag map for tag_id -> tag_name conversion
	tagMap, err := s.buildTagMap(ctx, tenantID, kbID)
	if err != nil {
		return nil, fmt.Errorf("failed to build tag map: %w", err)
	}

	return s.buildFAQCSV(chunks, tagMap), nil
}

// ExportFAQEntriesJSON xuất toàn bộ mục của kho tri thức FAQ dưới dạng mảng JSON,
// trường tương thích FAQEntryPayload, thuận tiện cho vòng "xuất → sửa → nhập lại ở chế độ append".
func (s *knowledgeService) ExportFAQEntriesJSON(ctx context.Context, kbID string) ([]byte, error) {
	kb, err := s.validateFAQKnowledgeBase(ctx, kbID)
	if err != nil {
		return nil, err
	}

	tenantID := ctx.Value(types.TenantIDContextKey).(uint64)
	faqKnowledge, err := s.findFAQKnowledge(ctx, tenantID, kb.ID)
	if err != nil {
		return nil, err
	}
	if faqKnowledge == nil {
		return json.Marshal([]types.FAQExportEntry{})
	}

	chunks, err := s.chunkRepo.ListAllFAQChunksForExport(ctx, tenantID, faqKnowledge.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to list FAQ chunks: %w", err)
	}

	tagMap, err := s.buildTagMap(ctx, tenantID, kbID)
	if err != nil {
		return nil, fmt.Errorf("failed to build tag map: %w", err)
	}

	return s.buildFAQJSON(chunks, tagMap)
}

func (s *knowledgeService) buildFAQJSON(chunks []*types.Chunk, tagMap map[string]string) ([]byte, error) {
	entries := make([]types.FAQExportEntry, 0, len(chunks))
	for _, chunk := range chunks {
		meta, err := chunk.FAQMetadata()
		if err != nil || meta == nil {
			continue
		}

		tagName := ""
		if chunk.TagID != "" && tagMap != nil {
			if name, ok := tagMap[chunk.TagID]; ok {
				tagName = name
			}
		}

		entries = append(entries, types.FAQExportEntry{
			ID:                chunk.SeqID,
			TagName:           tagName,
			StandardQuestion:  meta.StandardQuestion,
			SimilarQuestions:  meta.SimilarQuestions,
			NegativeQuestions: meta.NegativeQuestions,
			Answers:           meta.Answers,
			AnswerStrategy:    meta.AnswerStrategy,
			IsEnabled:         chunk.IsEnabled,
			IsRecommended:     chunk.Flags.HasFlag(types.ChunkFlagRecommended),
		})
	}
	return json.Marshal(entries)
}

// buildTagMap builds a map from tag_id to tag_name for the given knowledge base.
func (s *knowledgeService) buildTagMap(ctx context.Context, tenantID uint64, kbID string) (map[string]string, error) {
	const pageSize = 1000
	tagMap := make(map[string]string)

	for pageNum := 1; ; pageNum++ {
		page := &types.Pagination{Page: pageNum, PageSize: pageSize}
		tags, _, err := s.tagRepo.ListByKB(ctx, tenantID, kbID, page, "")
		if err != nil {
			return nil, err
		}
		for _, tag := range tags {
			if tag != nil {
				tagMap[tag.ID] = tag.Name
			}
		}
		if len(tags) < pageSize {
			break
		}
	}
	return tagMap, nil
}

// buildFAQCSV builds CSV content from FAQ chunks.
func (s *knowledgeService) buildFAQCSV(chunks []*types.Chunk, tagMap map[string]string) []byte {
	var buf strings.Builder

	// Write CSV header (matching import example format)
	headers := []string{
		"Phân loại(bắt buộc)",
		"Câu hỏi(bắt buộc)",
		"Câu hỏi tương tự(tùy chọn-nhiều giá trị cách nhau bằng ##)",
		"Câu hỏi phản ví dụ(tùy chọn-nhiều giá trị cách nhau bằng ##)",
		"Câu trả lời(bắt buộc-nhiều giá trị cách nhau bằng ##)",
		"Trả lời tất cả(tùy chọn-mặc định FALSE)",
		"Ngừng dùng(tùy chọn-mặc định FALSE)",
		"Không gợi ý(tùy chọn-mặc định FALSE, được gợi ý)",
	}
	buf.WriteString(strings.Join(headers, ","))
	buf.WriteString("\n")

	// Write data rows
	for _, chunk := range chunks {
		meta, err := chunk.FAQMetadata()
		if err != nil || meta == nil {
			continue
		}

		// Get tag name
		tagName := ""
		if chunk.TagID != "" && tagMap != nil {
			if name, ok := tagMap[chunk.TagID]; ok {
				tagName = name
			}
		}

		// Build row
		row := []string{
			escapeCSVField(tagName),
			escapeCSVField(meta.StandardQuestion),
			escapeCSVField(strings.Join(meta.SimilarQuestions, "##")),
			escapeCSVField(strings.Join(meta.NegativeQuestions, "##")),
			escapeCSVField(strings.Join(meta.Answers, "##")),
			boolToCSV(meta.AnswerStrategy == types.AnswerStrategyAll),
			boolToCSV(!chunk.IsEnabled),                                 // Ngừng dùng: đảo giá trị
			boolToCSV(!chunk.Flags.HasFlag(types.ChunkFlagRecommended)), // Không gợi ý: đảo giá trị
		}
		buf.WriteString(strings.Join(row, ","))
		buf.WriteString("\n")
	}

	return []byte(buf.String())
}

// escapeCSVField escapes a field for CSV format.
func escapeCSVField(field string) string {
	// If field contains comma, newline, or quote, wrap in quotes and escape internal quotes
	if strings.ContainsAny(field, ",\"\n\r") {
		return "\"" + strings.ReplaceAll(field, "\"", "\"\"") + "\""
	}
	return field
}

// boolToCSV converts a boolean to CSV TRUE/FALSE string.
func boolToCSV(b bool) string {
	if b {
		return "TRUE"
	}
	return "FALSE"
}

func (s *knowledgeService) validateFAQKnowledgeBase(ctx context.Context, kbID string) (*types.KnowledgeBase, error) {
	if kbID == "" {
		return nil, werrors.NewBadRequestError("thiếu ID kho tri thức")
	}
	kb, err := s.kbService.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		return nil, err
	}
	if kb == nil || kb.ID != kbID {
		return nil, werrors.NewNotFoundError("không tìm thấy kho tri thức")
	}
	kb.EnsureDefaults()
	if kb.Type != types.KnowledgeBaseTypeFAQ {
		return nil, werrors.NewBadRequestError("chỉ kho tri thức FAQ mới hỗ trợ thao tác này")
	}
	return kb, nil
}

func (s *knowledgeService) findFAQKnowledge(
	ctx context.Context,
	tenantID uint64,
	kbID string,
) (*types.Knowledge, error) {
	knowledges, err := s.repo.ListKnowledgeByKnowledgeBaseID(ctx, tenantID, kbID)
	if err != nil {
		return nil, err
	}
	for _, knowledge := range knowledges {
		if knowledge.Type == types.KnowledgeTypeFAQ {
			return knowledge, nil
		}
	}
	return nil, nil
}

func (s *knowledgeService) ensureFAQKnowledge(
	ctx context.Context,
	tenantID uint64,
	kb *types.KnowledgeBase,
) (*types.Knowledge, error) {
	existing, err := s.findFAQKnowledge(ctx, tenantID, kb.ID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}
	knowledge := &types.Knowledge{
		TenantID:         tenantID,
		KnowledgeBaseID:  kb.ID,
		Type:             types.KnowledgeTypeFAQ,
		Channel:          types.ChannelWeb,
		Title:            buildFAQKnowledgeTitle(kb.Name),
		Description:      "Vùng chứa các mục FAQ",
		Source:           types.KnowledgeTypeFAQ,
		ParseStatus:      "completed",
		EnableStatus:     "enabled",
		EmbeddingModelID: kb.EmbeddingModelID,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}
	if err := s.repo.CreateKnowledge(ctx, knowledge); err != nil {
		return nil, err
	}
	return knowledge, nil
}

// buildFAQKnowledgeTitle derives the display title for the FAQ container
// knowledge. The title is simply the knowledge base name (falling back to "FAQ"
// when empty) — no suffix is appended, so a KB named "FAQ" stays "FAQ" instead
// of becoming the redundant "FAQ - FAQ".
func buildFAQKnowledgeTitle(kbName string) string {
	name := strings.TrimSpace(kbName)
	if name == "" {
		return "FAQ"
	}
	return name
}

func faqChunkQuestion(chunk *types.Chunk) string {
	if chunk == nil {
		return ""
	}
	if meta, err := chunk.FAQMetadata(); err == nil && meta != nil {
		if question := strings.TrimSpace(meta.StandardQuestion); question != "" {
			return question
		}
	}
	return strings.TrimSpace(chunk.Content)
}

func (s *knowledgeService) chunkToFAQEntry(chunk *types.Chunk, kb *types.KnowledgeBase, tagSeqIDMap map[string]int64) (*types.FAQEntry, error) {
	meta, err := chunk.FAQMetadata()
	if err != nil {
		return nil, err
	}
	if meta == nil {
		meta = &types.FAQChunkMetadata{StandardQuestion: chunk.Content}
	}
	// Mặc định dùng chiến lược all
	answerStrategy := meta.AnswerStrategy
	if answerStrategy == "" {
		answerStrategy = types.AnswerStrategyAll
	}

	// Get tag seq_id from map
	var tagSeqID int64
	if chunk.TagID != "" && tagSeqIDMap != nil {
		tagSeqID = tagSeqIDMap[chunk.TagID]
	}

	entry := &types.FAQEntry{
		ID:                chunk.SeqID,
		ChunkID:           chunk.ID,
		KnowledgeID:       chunk.KnowledgeID,
		KnowledgeBaseID:   chunk.KnowledgeBaseID,
		TagID:             tagSeqID,
		IsEnabled:         chunk.IsEnabled,
		IsRecommended:     chunk.Flags.HasFlag(types.ChunkFlagRecommended),
		StandardQuestion:  meta.StandardQuestion,
		SimilarQuestions:  meta.SimilarQuestions,
		NegativeQuestions: meta.NegativeQuestions,
		Answers:           meta.Answers,
		AnswerStrategy:    answerStrategy,
		IndexMode:         kb.FAQConfig.IndexMode,
		UpdatedAt:         chunk.UpdatedAt,
		CreatedAt:         chunk.CreatedAt,
		ChunkType:         chunk.ChunkType,
	}
	return entry, nil
}

func buildFAQChunkContent(meta *types.FAQChunkMetadata, mode types.FAQIndexMode) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("Q: %s\n", meta.StandardQuestion))
	if len(meta.SimilarQuestions) > 0 {
		builder.WriteString("Similar Questions:\n")
		for _, q := range meta.SimilarQuestions {
			builder.WriteString(fmt.Sprintf("- %s\n", q))
		}
	}
	// Phản ví dụ không nên nằm trong Content vì không được đánh chỉ mục
	// Câu trả lời có đưa vào hay không tùy chế độ đánh chỉ mục
	if mode == types.FAQIndexModeQuestionAnswer && len(meta.Answers) > 0 {
		builder.WriteString("Answers:\n")
		for _, ans := range meta.Answers {
			builder.WriteString(fmt.Sprintf("- %s\n", ans))
		}
	}
	return builder.String()
}

// checkFAQQuestionDuplicate kiểm tra câu hỏi chuẩn và câu hỏi tương tự có trùng với mục khác trong kho tri thức không
// excludeChunkID dùng để loại trừ mục đang sửa (khi cập nhật)
// Theo cách kiểm tra của nhập hàng loạt: dựng tập câu hỏi đã có rồi kiểm tra thống nhất
func (s *knowledgeService) checkFAQQuestionDuplicate(
	ctx context.Context,
	tenantID uint64,
	kbID string,
	excludeChunkID string,
	meta *types.FAQChunkMetadata,
) error {
	// 1. Trước hết kiểm tra câu hỏi tương tự của chính mục này có trùng câu hỏi chuẩn không
	for _, q := range meta.SimilarQuestions {
		if q == meta.StandardQuestion {
			return werrors.NewBadRequestError(fmt.Sprintf("câu hỏi tương tự \"%s\" không được trùng câu hỏi chuẩn", q))
		}
	}

	// 2. Kiểm tra các câu hỏi tương tự của chính mục này có trùng nhau không
	seen := make(map[string]struct{})
	for _, q := range meta.SimilarQuestions {
		if _, exists := seen[q]; exists {
			return werrors.NewBadRequestError(fmt.Sprintf("câu hỏi tương tự \"%s\" bị trùng", q))
		}
		seen[q] = struct{}{}
	}

	// 3. Kiểm tra phản ví dụ có trùng câu hỏi chuẩn hoặc câu hỏi tương tự không (phản ví dụ không được trùng ví dụ dương)
	positiveQuestions := make(map[string]struct{})
	positiveQuestions[meta.StandardQuestion] = struct{}{}
	for _, q := range meta.SimilarQuestions {
		positiveQuestions[q] = struct{}{}
	}
	negativeQuestionsSeen := make(map[string]struct{})
	for _, q := range meta.NegativeQuestions {
		if q == "" {
			continue
		}
		// Kiểm tra phản ví dụ có trùng câu hỏi chuẩn không
		if q == meta.StandardQuestion {
			return werrors.NewBadRequestError(fmt.Sprintf("câu hỏi phản ví dụ \"%s\" không được trùng câu hỏi chuẩn", q))
		}
		// Kiểm tra phản ví dụ có trùng câu hỏi tương tự không
		if _, exists := positiveQuestions[q]; exists {
			return werrors.NewBadRequestError(fmt.Sprintf("câu hỏi phản ví dụ \"%s\" không được trùng câu hỏi tương tự", q))
		}
		// Kiểm tra các phản ví dụ có trùng nhau không
		if _, exists := negativeQuestionsSeen[q]; exists {
			return werrors.NewBadRequestError(fmt.Sprintf("câu hỏi phản ví dụ \"%s\" bị trùng", q))
		}
		negativeQuestionsSeen[q] = struct{}{}
	}

	// 4. Gộp câu hỏi chuẩn và mọi câu hỏi tương tự, dùng một truy vấn DB kiểm tra xung đột với mục khác (thay cho quét toàn bộ)
	allQuestions := make([]string, 0, 1+len(meta.SimilarQuestions))
	allQuestions = append(allQuestions, meta.StandardQuestion)
	allQuestions = append(allQuestions, meta.SimilarQuestions...)

	dupChunk, err := s.chunkRepo.FindFAQChunkWithDuplicateQuestion(ctx, tenantID, kbID, excludeChunkID, allQuestions)
	if err != nil {
		return fmt.Errorf("failed to check FAQ question duplicate: %w", err)
	}
	if dupChunk == nil {
		return nil
	}

	existingMeta, err := dupChunk.FAQMetadata()
	if err != nil || existingMeta == nil {
		return werrors.NewBadRequestError("câu hỏi chuẩn hoặc câu hỏi tương tự trùng với mục đã có")
	}

	// 5–7. Giữ ngữ nghĩa báo lỗi như quét toàn bộ trước đây: kiểm tra câu hỏi chuẩn trước, rồi từng câu hỏi tương tự
	existingSimilarSet := make(map[string]struct{}, len(existingMeta.SimilarQuestions))
	for _, q := range existingMeta.SimilarQuestions {
		if q != "" {
			existingSimilarSet[q] = struct{}{}
		}
	}

	if meta.StandardQuestion != "" {
		if meta.StandardQuestion == existingMeta.StandardQuestion {
			return werrors.NewBadRequestError(fmt.Sprintf("câu hỏi chuẩn \"%s\" đã tồn tại", meta.StandardQuestion))
		}
		if _, ok := existingSimilarSet[meta.StandardQuestion]; ok {
			return werrors.NewBadRequestError(fmt.Sprintf("câu hỏi chuẩn \"%s\" đã tồn tại", meta.StandardQuestion))
		}
	}

	for _, q := range meta.SimilarQuestions {
		if q == "" {
			continue
		}
		if q == existingMeta.StandardQuestion {
			return werrors.NewBadRequestError(fmt.Sprintf("câu hỏi tương tự \"%s\" đã tồn tại", q))
		}
		if _, ok := existingSimilarSet[q]; ok {
			return werrors.NewBadRequestError(fmt.Sprintf("câu hỏi tương tự \"%s\" đã tồn tại", q))
		}
	}

	return werrors.NewBadRequestError("câu hỏi chuẩn hoặc câu hỏi tương tự trùng với mục đã có")
}

// faqTagResolver resolves a payload's TagID/TagName to the tag's internal UUID.
type faqTagResolver func(*types.FAQEntryPayload) (string, error)

// buildFAQTagResolver nạp trước ánh xạ UUID của các thẻ mà entries tham chiếu, tránh resolveTagID từng mục.
func (s *knowledgeService) buildFAQTagResolver(
	ctx context.Context, kbID string, entries []types.FAQEntryPayload,
) faqTagResolver {
	tenantID := ctx.Value(types.TenantIDContextKey).(uint64)

	var seqIDs []int64
	var names []string
	seqSeen := make(map[int64]struct{})
	nameSeen := make(map[string]struct{})
	for i := range entries {
		e := &entries[i]
		switch {
		case e.TagID != 0:
			if _, ok := seqSeen[e.TagID]; !ok {
				seqSeen[e.TagID] = struct{}{}
				seqIDs = append(seqIDs, e.TagID)
			}
		case e.TagName != "":
			if _, ok := nameSeen[e.TagName]; !ok {
				nameSeen[e.TagName] = struct{}{}
				names = append(names, e.TagName)
			}
		}
	}

	seqIDToUUID := make(map[int64]string)
	if len(seqIDs) > 0 {
		if tags, err := s.tagRepo.GetBySeqIDs(ctx, tenantID, seqIDs); err == nil {
			for _, t := range tags {
				if validateFAQTagScope(t, tenantID, kbID) == nil {
					seqIDToUUID[t.SeqID] = t.ID
				}
			}
		} else {
			logger.Warnf(ctx, "buildFAQTagResolver: batch GetBySeqIDs failed (%d ids), fallback per-entry: %v",
				len(seqIDs), err)
		}
	}

	nameToUUID := make(map[string]string)
	for _, name := range names {
		if tag, err := s.tagRepo.GetByName(ctx, tenantID, kbID, name); err == nil && tag != nil {
			nameToUUID[name] = tag.ID
		}
	}

	return func(e *types.FAQEntryPayload) (string, error) {
		if e.TagID != 0 {
			if uuid, ok := seqIDToUUID[e.TagID]; ok {
				return uuid, nil
			}
		} else if e.TagName != "" {
			if uuid, ok := nameToUUID[e.TagName]; ok {
				return uuid, nil
			}
		}
		return s.resolveTagID(ctx, kbID, e)
	}
}

// hashQuestion tính hash nội dung câu hỏi để sinh sourceID ổn định.
func hashQuestion(question string) string {
	h := md5.Sum([]byte(question))
	return hex.EncodeToString(h[:4])
}

// resolveTagID resolves tag ID (UUID) from payload, prioritizing tag_id (seq_id) over tag_name
// If no tag is specified, creates or finds the default untagged tag (types.UntaggedTagName)
// Returns the internal UUID of the tag
func (s *knowledgeService) resolveTagID(ctx context.Context, kbID string, payload *types.FAQEntryPayload) (string, error) {
	tenantID := ctx.Value(types.TenantIDContextKey).(uint64)

	// Nếu có tag_id (seq_id) thì ưu tiên tag_id
	if payload.TagID != 0 {
		tag, err := s.tagRepo.GetBySeqID(ctx, tenantID, payload.TagID)
		if err != nil {
			return "", fmt.Errorf("failed to find tag by seq_id %d: %w", payload.TagID, err)
		}
		if err := validateFAQTagScope(tag, tenantID, kbID); err != nil {
			return "", err
		}
		return tag.ID, nil
	}

	// Nếu có tag_name thì tìm hoặc tạo thẻ
	if payload.TagName != "" {
		tag, err := s.tagService.FindOrCreateTagByName(ctx, kbID, payload.TagName)
		if err != nil {
			return "", fmt.Errorf("failed to resolve tag by name '%s': %w", payload.TagName, err)
		}
		return tag.ID, nil
	}

	// Không có cả hai thì dùng thẻ mặc định (UntaggedTagName)
	tag, err := s.tagService.FindOrCreateTagByName(ctx, kbID, types.UntaggedTagName)
	if err != nil {
		return "", fmt.Errorf("failed to get or create default untagged tag: %w", err)
	}
	return tag.ID, nil
}

func sanitizeFAQEntryPayload(payload *types.FAQEntryPayload) (*types.FAQChunkMetadata, error) {
	// Xử lý AnswerStrategy, mặc định all
	answerStrategy := types.AnswerStrategyAll
	if payload.AnswerStrategy != nil && *payload.AnswerStrategy != "" {
		switch *payload.AnswerStrategy {
		case types.AnswerStrategyAll, types.AnswerStrategyRandom:
			answerStrategy = *payload.AnswerStrategy
		default:
			return nil, werrors.NewBadRequestError("answer_strategy phải là 'all' hoặc 'random'")
		}
	}
	meta := &types.FAQChunkMetadata{
		StandardQuestion:  strings.TrimSpace(payload.StandardQuestion),
		SimilarQuestions:  payload.SimilarQuestions,
		NegativeQuestions: payload.NegativeQuestions,
		Answers:           payload.Answers,
		AnswerStrategy:    answerStrategy,
		Version:           1,
		Source:            "faq",
	}
	meta.Normalize()
	if meta.StandardQuestion == "" {
		return nil, werrors.NewBadRequestError("câu hỏi chuẩn không được để trống")
	}
	if len(meta.Answers) == 0 {
		return nil, werrors.NewBadRequestError("cần ít nhất một câu trả lời")
	}
	return meta, nil
}

func buildFAQIndexContent(meta *types.FAQChunkMetadata, mode types.FAQIndexMode) string {
	var builder strings.Builder
	builder.WriteString(meta.StandardQuestion)
	for _, q := range meta.SimilarQuestions {
		builder.WriteString("\n")
		builder.WriteString(q)
	}
	if mode == types.FAQIndexModeQuestionAnswer {
		for _, ans := range meta.Answers {
			builder.WriteString("\n")
			builder.WriteString(ans)
		}
	}
	return builder.String()
}

// buildFAQIndexInfoList dựng danh sách thông tin chỉ mục FAQ, hỗ trợ chế độ đánh chỉ mục riêng rẽ
func (s *knowledgeService) buildFAQIndexInfoList(
	ctx context.Context,
	kb *types.KnowledgeBase,
	chunk *types.Chunk,
) ([]*types.IndexInfo, error) {
	indexMode := types.FAQIndexModeQuestionAnswer
	questionIndexMode := types.FAQQuestionIndexModeCombined
	if kb.FAQConfig != nil {
		if kb.FAQConfig.IndexMode != "" {
			indexMode = kb.FAQConfig.IndexMode
		}
		if kb.FAQConfig.QuestionIndexMode != "" {
			questionIndexMode = kb.FAQConfig.QuestionIndexMode
		}
	}

	meta, err := chunk.FAQMetadata()
	if err != nil {
		return nil, err
	}
	if meta == nil {
		meta = &types.FAQChunkMetadata{StandardQuestion: chunk.Content}
	}

	// Chế độ đánh chỉ mục gộp thì dùng logic cũ
	if questionIndexMode == types.FAQQuestionIndexModeCombined {
		content := buildFAQIndexContent(meta, indexMode)
		return []*types.IndexInfo{
			{
				Content:         content,
				SourceID:        chunk.ID,
				SourceType:      types.ChunkSourceType,
				ChunkID:         chunk.ID,
				KnowledgeID:     chunk.KnowledgeID,
				KnowledgeBaseID: chunk.KnowledgeBaseID,
				KnowledgeType:   types.KnowledgeTypeFAQ,
				TagID:           chunk.TagID,
				IsEnabled:       chunk.IsEnabled,
				IsRecommended:   chunk.Flags.HasFlag(types.ChunkFlagRecommended),
			},
		}, nil
	}

	// Chế độ riêng rẽ: mỗi câu hỏi một mục chỉ mục độc lập
	indexInfoList := make([]*types.IndexInfo, 0)

	// Mục chỉ mục cho câu hỏi chuẩn
	standardContent := meta.StandardQuestion
	if indexMode == types.FAQIndexModeQuestionAnswer && len(meta.Answers) > 0 {
		var builder strings.Builder
		builder.WriteString(meta.StandardQuestion)
		for _, ans := range meta.Answers {
			builder.WriteString("\n")
			builder.WriteString(ans)
		}
		standardContent = builder.String()
	}
	indexInfoList = append(indexInfoList, &types.IndexInfo{
		Content:         standardContent,
		SourceID:        chunk.ID,
		SourceType:      types.ChunkSourceType,
		ChunkID:         chunk.ID,
		KnowledgeID:     chunk.KnowledgeID,
		KnowledgeBaseID: chunk.KnowledgeBaseID,
		KnowledgeType:   types.KnowledgeTypeFAQ,
		TagID:           chunk.TagID,
		IsEnabled:       chunk.IsEnabled,
		IsRecommended:   chunk.Flags.HasFlag(types.ChunkFlagRecommended),
	})

	// Mỗi câu hỏi tương tự một mục chỉ mục
	for i, similarQ := range meta.SimilarQuestions {
		similarContent := similarQ
		if indexMode == types.FAQIndexModeQuestionAnswer && len(meta.Answers) > 0 {
			var builder strings.Builder
			builder.WriteString(similarQ)
			for _, ans := range meta.Answers {
				builder.WriteString("\n")
				builder.WriteString(ans)
			}
			similarContent = builder.String()
		}
		sourceID := fmt.Sprintf("%s-%d", chunk.ID, i)
		indexInfoList = append(indexInfoList, &types.IndexInfo{
			Content:         similarContent,
			SourceID:        sourceID,
			SourceType:      types.ChunkSourceType,
			ChunkID:         chunk.ID,
			KnowledgeID:     chunk.KnowledgeID,
			KnowledgeBaseID: chunk.KnowledgeBaseID,
			KnowledgeType:   types.KnowledgeTypeFAQ,
			TagID:           chunk.TagID,
			IsEnabled:       chunk.IsEnabled,
			IsRecommended:   chunk.Flags.HasFlag(types.ChunkFlagRecommended),
		})
	}

	return indexInfoList, nil
}
