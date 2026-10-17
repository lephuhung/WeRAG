package handler

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/errors"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/storageurl"
	"github.com/Tencent/WeKnora/internal/tracing/langfuse"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/utils"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
)

// KnowledgeBaseHandler defines the HTTP handler for knowledge base operations
type KnowledgeBaseHandler struct {
	cfg                  *config.Config
	service              interfaces.KnowledgeBaseService
	profileService       interfaces.KnowledgeBaseProfileService
	knowledgeService     interfaces.KnowledgeService
	kbAccessGrantService interfaces.KBAccessGrantService
	asynqClient          interfaces.TaskEnqueuer
	vectorStoreService   interfaces.VectorStoreService // enriches KB responses with bound store display
	// userService chỉ dùng trong các API list để điền hàng loạt creator_name;
	// việc phân quyền thực sự do middleware RBAC + Lookup đảm nhiệm, ở đây không quyết định.
	userService interfaces.UserService
	// fileService and storageResolver back the optional `resource_urls=public`
	// mode on hybrid-search. Both may be nil in tests, in which case only the
	// default handle mode is available.
	fileService     interfaces.FileService
	storageResolver interfaces.StorageBackendResolver
}

// NewKnowledgeBaseHandler creates a new knowledge base handler instance
func NewKnowledgeBaseHandler(
	cfg *config.Config,
	service interfaces.KnowledgeBaseService,
	knowledgeService interfaces.KnowledgeService,
	kbAccessGrantService interfaces.KBAccessGrantService,
	asynqClient interfaces.TaskEnqueuer,
	vectorStoreService interfaces.VectorStoreService,
	userService interfaces.UserService,
	fileService interfaces.FileService,
	storageResolver interfaces.StorageBackendResolver,
	profileService interfaces.KnowledgeBaseProfileService,
) *KnowledgeBaseHandler {
	return &KnowledgeBaseHandler{
		cfg:                  cfg,
		profileService:       profileService,
		service:              service,
		knowledgeService:     knowledgeService,
		kbAccessGrantService: kbAccessGrantService,
		asynqClient:          asynqClient,
		vectorStoreService:   vectorStoreService,
		userService:          userService,
		fileService:          fileService,
		storageResolver:      storageResolver,
	}
}

// resolveResourceRewriter builds the storage-reference rewriter for one response
// from the request's `resource_urls` parameter, falling back to the deployment
// default. The returned error is already an AppError the caller can hand to
// c.Error: a rejected scope is a 403, a typo in the parameter is a 400.
func (h *KnowledgeBaseHandler) resolveResourceRewriter(c *gin.Context) (*storageurl.Rewriter, error) {
	ctx := c.Request.Context()
	mode, err := storageurl.ResolveMode(ctx, c.Query(storageurl.QueryParam))
	if err != nil {
		if stderrors.Is(err, storageurl.ErrPublicModeForbidden) {
			return nil, apperrors.NewForbiddenError(err.Error())
		}
		return nil, apperrors.NewBadRequestError(err.Error())
	}
	return storageurl.NewRequestRewriter(ctx, mode, h.fileService, h.storageResolver), nil
}

// buildKBResponse turns a knowledge base into a JSON-ready response shape,
// merging the bound vector store's display metadata and any caller-supplied
// extras (e.g., my_permission for shared KBs). Returns the kb pointer
// unchanged on serialization failure so the request still succeeds.
//
// The map-merge approach (rather than a wrapper struct embedding the kb)
// is deliberate: KnowledgeBase has a custom MarshalJSON, and embedding
// would promote it to any wrapper struct and silently swallow the extra
// fields. The same pattern is already used by GetKnowledgeBase to add the
// my_permission field for shared knowledge bases.
//
// Shared-KB suppression: when storeView.Source == StoreSourceShared (the
// caller is not the KB owner), the raw vector_store_id UUID is stripped
// from the response so the owner-tenant's store inventory cannot be
// correlated across multiple shared KBs. Name / engine type / status are
// already empty in the SharedStoreDisplay payload; suppressing the UUID
// completes the cross-tenant metadata hiding.
func buildKBResponse(
	kb *types.KnowledgeBase,
	storeView types.StoreDisplay,
	extras map[string]interface{},
) interface{} {
	b, err := json.Marshal(kb)
	if err != nil {
		return kb
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil || m == nil {
		return kb
	}
	if storeView.Source == types.StoreSourceShared {
		delete(m, "vector_store_id")
	}
	if storeView.Name != "" {
		m["vector_store_name"] = storeView.Name
	}
	if storeView.Source != "" {
		m["vector_store_source"] = storeView.Source
	}
	if storeView.EngineType != "" {
		m["vector_store_engine_type"] = storeView.EngineType
	}
	if storeView.Status != "" {
		m["vector_store_status"] = storeView.Status
	}
	for k, v := range extras {
		m[k] = v
	}
	return m
}

// buildKBListResponse turns a slice of knowledge bases into a JSON-ready
// slice that mirrors the single-KB enrichment in buildKBResponse. Store
// views are batch-resolved once via BatchResolveStoreView to keep the
// list endpoint O(1) in vector-store service calls — the per-KB
// resolveKBStoreView would otherwise be N+1.
//
// Caller-vs-owner semantics match the single-KB path:
//   - KB has no binding         → DefaultStoreDisplay()
//   - KB is owned by another tenant (cross-tenant shared) → SharedStoreDisplay()
//   - KB is own-tenant bound    → look up in the batch result; misses
//     fall back to UnavailableStoreDisplay()
//
// Resolver failures degrade gracefully: every own-tenant bound KB renders
// as unavailable instead of breaking the list response.
func (h *KnowledgeBaseHandler) buildKBListResponse(
	ctx context.Context, kbs []*types.KnowledgeBase, callerTenantID uint64,
) []interface{} {
	defaultView := h.envDefaultStoreView(ctx)
	storeViews := h.batchResolveKBStoreViews(ctx, kbs, callerTenantID)
	out := make([]interface{}, 0, len(kbs))
	for _, kb := range kbs {
		var view types.StoreDisplay
		switch {
		case !kb.HasVectorStore():
			view = defaultView
		case kb.TenantID != callerTenantID:
			view = types.SharedStoreDisplay()
		default:
			v, ok := storeViews[*kb.VectorStoreID]
			if !ok || v.Source == "" {
				view = types.UnavailableStoreDisplay()
			} else {
				view = v
			}
		}
		out = append(out, buildKBResponse(kb, view, nil))
	}
	return out
}

// envDefaultStoreView returns the env-fallback store display enriched with
// the configured env-store engine type when the service is available. The
// service path populates EngineType so the caller can show "postgres" or
// "qdrant" on the env-default badge instead of leaving it blank. A nil
// service (e.g. in narrow unit-test setups) falls back to the bare default
// display rather than failing the list response.
func (h *KnowledgeBaseHandler) envDefaultStoreView(ctx context.Context) types.StoreDisplay {
	if h.vectorStoreService == nil {
		return types.DefaultStoreDisplay()
	}
	return h.vectorStoreService.EnvDefaultStoreView(ctx)
}

// batchResolveKBStoreViews collects the unique own-tenant store IDs across
// the KB slice and resolves them in one BatchResolveStoreView call.
// Cross-tenant shared KBs never enter the batch — they always render
// via SharedStoreDisplay, which deliberately suppresses the owner
// tenant's store name and engine type so cross-tenant viewers cannot
// correlate the owner's store inventory from KB responses alone.
func (h *KnowledgeBaseHandler) batchResolveKBStoreViews(
	ctx context.Context, kbs []*types.KnowledgeBase, callerTenantID uint64,
) map[string]types.StoreDisplay {
	if h.vectorStoreService == nil {
		return nil
	}
	storeIDs := make([]string, 0, len(kbs))
	seen := make(map[string]bool, len(kbs))
	for _, kb := range kbs {
		if !kb.HasVectorStore() || kb.TenantID != callerTenantID {
			continue
		}
		sid := *kb.VectorStoreID
		if !seen[sid] {
			seen[sid] = true
			storeIDs = append(storeIDs, sid)
		}
	}
	if len(storeIDs) == 0 {
		return nil
	}
	views, err := h.vectorStoreService.BatchResolveStoreView(ctx, callerTenantID, storeIDs)
	if err != nil {
		logger.WarnWithFields(ctx, logger.Fields{
			"tenant_id":   callerTenantID,
			"store_count": len(storeIDs),
		}, "[kb.list] batch store view resolve failed; rendering bound KBs as unavailable")
		return nil
	}
	return views
}

// resolveKBStoreView returns the store display payload to embed in the KB
// response. It applies two policies on top of the service-level resolver:
//
//   - When the KB does not have a DB-managed vector store binding,
//     the env-fallback display is returned without touching the service.
//   - When the caller is not the KB owner (shared access), the underlying
//     store's name and engine are suppressed so operator-chosen names do
//     not leak across tenants. The Source value is set to "shared".
//
// On resolution error, an unavailable display is returned and the failure
// is logged for ops; the request itself still succeeds.
func (h *KnowledgeBaseHandler) resolveKBStoreView(
	ctx context.Context, kb *types.KnowledgeBase, callerTenantID uint64,
) types.StoreDisplay {
	if !kb.HasVectorStore() {
		return h.envDefaultStoreView(ctx)
	}
	if kb.TenantID != callerTenantID {
		return types.SharedStoreDisplay()
	}
	if h.vectorStoreService == nil {
		return types.UnavailableStoreDisplay()
	}
	view, err := h.vectorStoreService.ResolveStoreView(ctx, kb.TenantID, *kb.VectorStoreID)
	if err != nil {
		logger.WarnWithFields(ctx, logger.Fields{
			"kb_id":     secutils.SanitizeForLog(kb.ID),
			"tenant_id": kb.TenantID,
		}, "[kb.view] vector store resolve failed; returning unavailable")
		return types.UnavailableStoreDisplay()
	}
	return view
}

// HybridSearch godoc
// @Summary      Tìm kiếm kết hợp
// @Description  Tìm kiếm kết hợp vector và từ khoá trong kho tri thức. Nên dùng POST; GET kèm body JSON vẫn được hỗ trợ (tương thích client cũ).
// @Tags         Kho tri thức
// @Accept       json
// @Produce      json
// @Param        id             path      string             true   "Knowledge BaseID"
// @Param        request        body      types.SearchParams true   "tham số tìm kiếm"
// @Param        resource_urls  query     string  false  "dạng tham chiếu tệp, public trả về liên kết tải trực tiếp"  Enums(handle, public)  default(handle)
// @Success      200            {object}  map[string]interface{}  "kết quả tìm kiếm"
// @Failure      400            {object}  errors.AppError         "tham số không hợp lệ"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases/{id}/hybrid-search [post]
// @Router       /knowledge-bases/{id}/hybrid-search [get]
func (h *KnowledgeBaseHandler) HybridSearch(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Start hybrid search")

	// Validate and check permission for knowledge base access
	_, id, effectiveTenantID, _, err := h.validateAndGetKnowledgeBase(c)
	if err != nil {
		c.Error(err)
		return
	}

	// Parse request body
	var req types.SearchParams
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to parse request parameters", err)
		c.Error(apperrors.NewBadRequestError("Invalid request parameters").WithDetails(err.Error()))
		return
	}
	precomputedVectorOnly := len(req.QueryEmbedding) > 0 && req.DisableKeywordsMatch && !req.DisableVectorMatch
	if strings.TrimSpace(req.QueryText) == "" && !precomputedVectorOnly {
		_ = c.Error(apperrors.NewBadRequestError("query_text is required"))
		return
	}

	logger.Infof(ctx, "Executing hybrid search, knowledge base ID: %s, query: %s, effectiveTenantID: %d",
		secutils.SanitizeForLog(id), secutils.SanitizeForLog(req.QueryText), effectiveTenantID)

	// Resolve before retrieving so a typo or a rejected scope costs nothing.
	rewriter, err := h.resolveResourceRewriter(c)
	if err != nil {
		logger.Warnf(ctx, "Rejected resource URL mode: %v", err)
		_ = c.Error(err)
		return
	}

	// Execute hybrid search with default search parameters
	// Note: For shared KBs, the service uses effectiveTenantID internally via context
	results, err := h.service.HybridSearch(c.Request.Context(), id, req)
	if err != nil {
		// Service-layer typed AppErrors (e.g. ErrVectorStoreBindingInvalid,
		// ErrVectorStoreUnavailable, BadRequest from multi-store fan-out)
		// must reach the client with their original code rather than be
		// downgraded to InternalServerError. Mirrors the pattern used in
		// CreateKnowledgeBase.
		if appErr, ok := apperrors.IsAppError(err); ok {
			c.Error(appErr)
			return
		}
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(apperrors.NewInternalServerError(err.Error()))
		return
	}

	logger.Infof(ctx, "Hybrid search completed, knowledge base ID: %s, result count: %d",
		secutils.SanitizeForLog(id), len(results))
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    rewriter.CopyReferences(ctx, results),
	})
}

// CreateKnowledgeBase godoc
// @Summary      Create Knowledge Base
// @Description  Tạo kho tri thức mới
// @Tags         Kho tri thức
// @Accept       json
// @Produce      json
// @Param        request  body      types.KnowledgeBase  true  "thông tin kho tri thức"
// @Success      201      {object}  map[string]interface{}  "kho tri thức đã tạo"
// @Failure      400      {object}  errors.AppError         "tham số không hợp lệ"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases [post]
func (h *KnowledgeBaseHandler) CreateKnowledgeBase(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Start creating knowledge base")

	// Parse request body
	var req types.KnowledgeBase
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to parse request parameters", err)
		c.Error(apperrors.NewBadRequestError("Invalid request parameters").WithDetails(err.Error()))
		return
	}
	// A Tenant Admin creates the knowledge base; the SuperAdmin configures
	// it. Anything beyond its identity falls back to the platform defaults.
	if !canManagePlatformConfig(ctx) {
		req = types.KnowledgeBase{
			Name:          req.Name,
			Description:   req.Description,
			Type:          req.Type,
			Visibility:    req.Visibility,
			MemberDefault: req.MemberDefault,
		}
	}
	if err := validateExtractConfig(req.ExtractConfig); err != nil {
		logger.Error(ctx, "Invalid extract configuration", err)
		c.Error(err)
		return
	}
	types.NormalizeKnowledgeBasePromptInstructions(&req)
	if err := validateKnowledgeBasePromptInstructions(&req); err != nil {
		c.Error(err)
		return
	}
	provider := strings.ToLower(strings.TrimSpace(req.GetStorageProvider()))
	if provider != "" && !isStorageProviderAllowed(provider) {
		c.Error(apperrors.NewBadRequestError("Storage provider is not allowed by STORAGE_ALLOW_LIST"))
		return
	}

	logger.Infof(ctx, "Creating knowledge base, name: %s", secutils.SanitizeForLog(req.Name))
	// Create knowledge base using the service
	kb, err := h.service.CreateKnowledgeBase(ctx, &req)
	if err != nil {
		// Surface typed AppErrors (notably the 400-class codes
		// ErrVectorStoreBindingInvalid and ErrVectorStoreUnavailable
		// returned by validateVectorStoreBinding) instead of wrapping them
		// into a generic 500. The middleware renders the original code and
		// HTTP status verbatim. Falls through to 500 only for raw infra
		// errors that the service did not classify.
		if appErr, ok := apperrors.IsAppError(err); ok {
			c.Error(appErr)
			return
		}
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(apperrors.NewInternalServerError(err.Error()))
		return
	}

	logger.Infof(ctx, "Knowledge base created successfully, ID: %s, name: %s",
		secutils.SanitizeForLog(kb.ID), secutils.SanitizeForLog(kb.Name))
	callerTenantID := c.GetUint64(types.TenantIDContextKey.String())
	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"data":    buildKBResponse(kb, h.resolveKBStoreView(ctx, kb, callerTenantID), nil),
	})
}

// CreatePublicKnowledgeBase godoc
// @Summary      Create platform public Knowledge Base
// @Description  Create a platform-owned public knowledge base (owner and data scope 0). Explicit human SuperAdmin only; never reachable with an API key.
// @Tags         Kho tri thức
// @Accept       json
// @Produce      json
// @Param        request  body      types.KnowledgeBase  true  "thông tin kho tri thức"
// @Success      201      {object}  map[string]interface{}  "kho tri thức đã tạo"
// @Failure      400      {object}  errors.AppError         "tham số không hợp lệ"
// @Security     Bearer
// @Router       /knowledge-bases/public [post]
func (h *KnowledgeBaseHandler) CreatePublicKnowledgeBase(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Start creating public knowledge base")

	var req types.KnowledgeBase
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to parse request parameters", err)
		c.Error(apperrors.NewBadRequestError("Invalid request parameters").WithDetails(err.Error()))
		return
	}
	if err := validateExtractConfig(req.ExtractConfig); err != nil {
		logger.Error(ctx, "Invalid extract configuration", err)
		c.Error(err)
		return
	}
	types.NormalizeKnowledgeBasePromptInstructions(&req)
	if err := validateKnowledgeBasePromptInstructions(&req); err != nil {
		c.Error(err)
		return
	}
	provider := strings.ToLower(strings.TrimSpace(req.GetStorageProvider()))
	if provider != "" && !isStorageProviderAllowed(provider) {
		c.Error(apperrors.NewBadRequestError("Storage provider is not allowed by STORAGE_ALLOW_LIST"))
		return
	}

	logger.Infof(ctx, "Creating public knowledge base, name: %s", secutils.SanitizeForLog(req.Name))
	kb, err := h.service.CreatePublicKnowledgeBase(ctx, &req)
	if err != nil {
		if appErr, ok := apperrors.IsAppError(err); ok {
			c.Error(appErr)
			return
		}
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(apperrors.NewInternalServerError(err.Error()))
		return
	}

	logger.Infof(ctx, "Public knowledge base created successfully, ID: %s, name: %s",
		secutils.SanitizeForLog(kb.ID), secutils.SanitizeForLog(kb.Name))
	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"data":    buildKBResponse(kb, h.resolveKBStoreView(ctx, kb, 0), nil),
	})
}

// validateAndGetKnowledgeBase validates request parameters and retrieves the knowledge base.
// Enforces per-API-key KB scope before tenant/share/agent resolution.
// Returns the knowledge base, knowledge base ID, effective tenant ID for embedding, permission level, and any errors encountered
// For owned KBs, effectiveTenantID is the caller's tenant ID
// For shared KBs, effectiveTenantID is the source tenant ID (owner's tenant)
func (h *KnowledgeBaseHandler) validateAndGetKnowledgeBase(
	c *gin.Context,
) (*types.KnowledgeBase, string, uint64, types.KBPermission, error) {
	id := secutils.SanitizeForLog(c.Param("id"))
	grant, err := resolveHandlerKBAccess(c, id, h.service, h.kbAccessGrantService)
	if err != nil {
		return nil, id, 0, "", err
	}
	return grant.KnowledgeBase, id, grant.EffectiveTenantID, grant.Permission, nil
}

// GetKnowledgeBase godoc
// @Summary      Lấy chi tiết kho tri thức
// @Description  Lấy chi tiết kho tri thức theo ID. Khi dùng trợ lý chia sẻ, có thể truyền agent_id để kiểm tra trợ lý đó có quyền truy cập kho tri thức này không.
// @Tags         Kho tri thức
// @Accept       json
// @Produce      json
// @Param        id         path      string  true   "Knowledge BaseID"
// @Param        agent_id   query     string  false  "ID trợ lý chia sẻ (để kiểm tra trợ lý có quyền truy cập kho tri thức này không)"
// @Success      200  {object}  map[string]interface{}  "Knowledge BaseDetails "
// @Failure      400  {object}  errors.AppError         "tham số không hợp lệ"
// @Failure      404  {object}  errors.AppError         "không tìm thấy kho tri thức"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases/{id} [get]
func (h *KnowledgeBaseHandler) GetKnowledgeBase(c *gin.Context) {
	// Validate and get the knowledge base
	kb, _, _, permission, err := h.validateAndGetKnowledgeBase(c)
	if err != nil {
		c.Error(err)
		return
	}
	// Fill counts (knowledge_count, chunk_count, is_processing) so hover/detail shows correct numbers
	if fillErr := h.service.FillKnowledgeBaseCounts(c.Request.Context(), kb); fillErr != nil {
		logger.Warnf(c.Request.Context(), "Failed to fill KB counts for %s: %v", kb.ID, fillErr)
	}
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	storeView := h.resolveKBStoreView(c.Request.Context(), kb, tenantID)
	var extras map[string]interface{}
	if kb.TenantID != tenantID && permission != "" {
		// Include my_permission in data so frontend can show role (e.g. "Chỉ đọc") instead of "--" for agent-visible KBs
		extras = map[string]interface{}{"my_permission": permission}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": buildKBResponse(kb, storeView, extras)})
}

// ListKnowledgeBases godoc
// @Summary      Lấy danh sách kho tri thức
// @Description  Lấy mọi kho tri thức của đơn vị hiện tại; hoặc khi truyền agent_id (trợ lý chia sẻ), kiểm tra quyền rồi trả về phạm vi kho tri thức mà trợ lý đó được cấu hình (dùng cho @ nhắc)
// @Tags         Kho tri thức
// @Accept       json
// @Produce      json
// @Param        agent_id  query     string  false  "ID trợ lý chia sẻ (khi truyền sẽ trả về các kho tri thức trợ lý đó dùng được)"
// @Success      200  {object}  map[string]interface{}  "Knowledge BaseList "
// @Failure      500  {object}  errors.AppError         "lỗi hệ thống"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases [get]
func (h *KnowledgeBaseHandler) ListKnowledgeBases(c *gin.Context) {
	ctx := c.Request.Context()

	// Get all knowledge bases for this tenant
	kbs, err := h.service.ListKnowledgeBases(ctx)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(apperrors.NewInternalServerError(err.Error()))
		return
	}

	// Optional creator filter — drives the [All | Mine | Others] segmented
	// control on the list page. We filter in-process rather than pushing
	// down into SQL because the tenant-bounded KB list is small (typically
	// <100 rows) and adding a creator predicate to ListKnowledgeBases would
	// ripple through every other caller (chat pipeline, agent editor, …).
	// Rows with empty CreatorID predate the RBAC migration (PR 5); we treat
	// them as "not anyone in particular" so they never appear under "mine"
	// or "others" — they fall out of both filters cleanly.
	creatorFilter := strings.ToLower(strings.TrimSpace(c.Query("creator")))
	if creatorFilter == "mine" || creatorFilter == "others" {
		callerUserID, _ := c.Get(types.UserIDContextKey.String())
		callerUserIDStr, _ := callerUserID.(string)
		filtered := make([]*types.KnowledgeBase, 0, len(kbs))
		for _, kb := range kbs {
			if kb.CreatorID == "" {
				continue
			}
			if creatorFilter == "mine" && kb.CreatorID == callerUserIDStr {
				filtered = append(filtered, kb)
			} else if creatorFilter == "others" && kb.CreatorID != callerUserIDStr {
				filtered = append(filtered, kb)
			}
		}
		kbs = filtered
	}
	kbs = filterKnowledgeBasesForAPIKeyScope(ctx, kbs)

	// Get share counts for all knowledge bases
	if len(kbs) > 0 && h.kbAccessGrantService != nil {
		kbIDs := make([]string, len(kbs))
		for i, kb := range kbs {
			kbIDs[i] = kb.ID
		}

		shareCounts, err := h.kbAccessGrantService.CountGrantsByKnowledgeBaseIDs(ctx, kbIDs)
		if err != nil {
			logger.Warnf(ctx, "Failed to get share counts: %v", err)
		} else {
			for _, kb := range kbs {
				if count, ok := shareCounts[kb.ID]; ok {
					kb.ShareCount = int64(count)
				}
			}
		}
	}

	// Điền hàng loạt creator_name để danh sách ở frontend phân biệt "tôi tạo" với "thành viên khác trong đơn vị tạo".
	// Chỉ điền trong API list, màn chi tiết / sửa không phụ thuộc trường này; giải mã thất bại (người dùng đã xoá,
	// dữ liệu cũ CreatorID rỗng) thì để trống, frontend tự hiển thị fallback.
	enrichKBCreatorNames(ctx, h.userService, kbs)

	callerTenantID := c.GetUint64(types.TenantIDContextKey.String())
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    h.buildKBListResponse(ctx, kbs, callerTenantID),
	})
}

// ListPublicCatalog godoc
// @Summary      Danh mục kho tri thức công khai của nền tảng
// @Description  Trả về một trang KB công khai của nền tảng (owner_tenant_id=0 và visibility=public) kèm metadata total/page/page_size. Chỉ người dùng đã xác thực gọi được; ẩn danh 401, API key 403. SuperAdmin là người thật không có ngữ cảnh đơn vị vẫn gọi được.
// @Tags         Kho tri thức
// @Produce      json
// @Param        page       query     int     false  "số trang (bắt đầu từ 1, mặc định 1)"
// @Param        page_size  query     int     false  "số mục mỗi trang (mặc định 50, tối đa 200)"
// @Param        q          query     string  false  "lọc theo từ khoá tên/mô tả"
// @Success      200        {object}  map[string]interface{}  "trang danh mục công khai"
// @Security     Bearer
// @Router       /knowledge-bases/public [get]
func (h *KnowledgeBaseHandler) ListPublicCatalog(c *gin.Context) {
	ctx := c.Request.Context()

	// Human-only: anonymous contexts lack an identity and API-key
	// principals never receive implicit public rows (the route also
	// declares no API-key policy, so keys are default-denied upstream).
	if _, isKey := types.TenantAPIKeyScopeFromContext(ctx); isKey {
		c.Error(apperrors.NewForbiddenError("API keys cannot access the public catalog"))
		return
	}
	if !access.IsAuthenticatedHuman(ctx, types.CallerFromContext(ctx)) {
		c.Error(apperrors.NewUnauthorizedError("Unauthorized"))
		return
	}

	page, pageSize := parseCatalogPagination(c)
	list := h.service.ListPublicCatalog
	if c.Query("kind") == types.PublicCatalogKindPublished {
		// Tenant-published KBs, with the caller's subscription flags.
		list = h.service.ListPublishedCatalog
	}
	items, total, err := list(ctx, page, pageSize, strings.TrimSpace(c.Query("q")))
	if err != nil {
		if appErr, ok := apperrors.IsAppError(err); ok {
			c.Error(appErr)
			return
		}
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(apperrors.NewInternalServerError(err.Error()))
		return
	}

	// Creator display names, batched (one query) so the page stays
	// bounded. Share counts are intentionally omitted here: grants do not
	// expand public visibility and Task 5 renders no share affordance on
	// public rows.
	enrichKBCreatorNames(ctx, h.userService, items)

	callerTenantID := c.GetUint64(types.TenantIDContextKey.String())
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"items":     h.buildKBListResponse(ctx, items, callerTenantID),
			"total":     total,
			"page":      page,
			"page_size": pageSize,
		},
	})
}

// SubscribeKnowledgeBase godoc
// @Summary      Subscribe to a published knowledge base
// @Description  Adds a published KB to the default retrieval scope of the caller, or of the whole workspace with scope=tenant (Tenant Admin only).
// @Tags         Kho tri thức
// @Param        id     path   string  true   "Knowledge base ID"
// @Param        scope  query  string  false  "me (default) or tenant"
// @Success      200  {object}  map[string]interface{}
// @Security     Bearer
// @Router       /knowledge-bases/{id}/subscription [post]
func (h *KnowledgeBaseHandler) SubscribeKnowledgeBase(c *gin.Context) {
	h.changeSubscription(c, h.service.SubscribeKnowledgeBase)
}

// UnsubscribeKnowledgeBase godoc
// @Summary      Unsubscribe from a published knowledge base
// @Tags         Kho tri thức
// @Param        id     path   string  true   "Knowledge base ID"
// @Param        scope  query  string  false  "me (default) or tenant"
// @Success      200  {object}  map[string]interface{}
// @Security     Bearer
// @Router       /knowledge-bases/{id}/subscription [delete]
func (h *KnowledgeBaseHandler) UnsubscribeKnowledgeBase(c *gin.Context) {
	h.changeSubscription(c, h.service.UnsubscribeKnowledgeBase)
}

func (h *KnowledgeBaseHandler) changeSubscription(
	c *gin.Context, change func(ctx context.Context, kbID string, tenantWide bool) error,
) {
	scope := c.DefaultQuery("scope", "me")
	if scope != "me" && scope != "tenant" {
		c.Error(apperrors.NewBadRequestError("scope must be me or tenant"))
		return
	}
	if err := change(c.Request.Context(), c.Param("id"), scope == "tenant"); err != nil {
		if appErr, ok := apperrors.IsAppError(err); ok {
			c.Error(appErr)
			return
		}
		logger.ErrorWithFields(c.Request.Context(), err, nil)
		c.Error(apperrors.NewInternalServerError("failed to update subscription"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// parseCatalogPagination normalizes page/page_size query parameters for
// the public catalog: page starts at 1, page_size defaults and clamps to
// the platform catalog bounds. Malformed values fall back to defaults
// rather than failing the request.
func parseCatalogPagination(c *gin.Context) (page, pageSize int) {
	page, pageSize = 1, types.PublicCatalogDefaultPageSize
	if raw := strings.TrimSpace(c.Query("page")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			page = v
		}
	}
	if raw := strings.TrimSpace(c.Query("page_size")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			pageSize = v
		}
	}
	if pageSize > types.PublicCatalogMaxPageSize {
		pageSize = types.PublicCatalogMaxPageSize
	}
	return page, pageSize
}

func filterKnowledgeBasesForAPIKeyScope(ctx context.Context, kbs []*types.KnowledgeBase) []*types.KnowledgeBase {
	scope, ok := types.TenantAPIKeyScopeFromContext(ctx)
	if !ok || len(scope.KnowledgeBaseIDs) == 0 {
		return kbs
	}
	filtered := make([]*types.KnowledgeBase, 0, len(kbs))
	for _, kb := range kbs {
		if kb != nil && scope.AllowsKnowledgeBase(kb.ID) {
			filtered = append(filtered, kb)
		}
	}
	return filtered
}

// enrichKBCreatorNames giải mã hàng loạt CreatorID trong danh sách KB thành tên hiển thị (ưu tiên
// username, không có thì dùng email). Bước nào lỗi cũng nuốt lỗi: thiếu creator_name chỉ ảnh hưởng
// huy hiệu góc dưới phải của thẻ, không được làm hỏng danh sách.
func enrichKBCreatorNames(ctx context.Context, userSvc interfaces.UserService, kbs []*types.KnowledgeBase) {
	if userSvc == nil || len(kbs) == 0 {
		return
	}
	idSet := make(map[string]struct{}, len(kbs))
	for _, kb := range kbs {
		if kb.CreatorID != "" {
			idSet[kb.CreatorID] = struct{}{}
		}
	}
	if len(idSet) == 0 {
		return
	}
	ids := make([]string, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	users, err := userSvc.GetUsersByIDs(ctx, ids)
	if err != nil {
		logger.Warnf(ctx, "Failed to resolve KB creator names: %v", err)
		return
	}
	for _, kb := range kbs {
		if kb.CreatorID == "" {
			continue
		}
		u, ok := users[kb.CreatorID]
		if !ok || u == nil {
			continue
		}
		kb.CreatorName = pickUserDisplayName(u)
	}
}

// pickUserDisplayName picks the field most users will recognise: Username
// if present (it's required at registration), Email as a fallback. Used by
// both KB and Agent list enrichment so the badge text stays consistent.
func pickUserDisplayName(u *types.User) string {
	if u == nil {
		return ""
	}
	if u.Username != "" {
		return u.Username
	}
	return u.Email
}

// TogglePinKnowledgeBase godoc
// @Summary      Ghim / bỏ ghim kho tri thức
// @Description  Chuyển trạng thái ghim của kho tri thức
// @Tags         Kho tri thức
// @Accept       json
// @Produce      json
// @Param        id  path      string  true  "Knowledge BaseID"
// @Success      200  {object}  map[string]interface{}  "kho tri thức sau khi cập nhật"
// @Failure      404  {object}  errors.AppError         "không tìm thấy kho tri thức"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases/{id}/pin [put]
func (h *KnowledgeBaseHandler) TogglePinKnowledgeBase(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")
	if id == "" {
		c.Error(apperrors.NewBadRequestError("knowledge base ID is required"))
		return
	}

	kb, err := h.service.TogglePinKnowledgeBase(ctx, id)
	if err != nil {
		if stderrors.Is(err, repository.ErrKnowledgeBaseNotFound) {
			c.Error(apperrors.NewNotFoundError("knowledge base not found"))
			return
		}
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(apperrors.NewInternalServerError(err.Error()))
		return
	}

	callerTenantID := c.GetUint64(types.TenantIDContextKey.String())
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    buildKBResponse(kb, h.resolveKBStoreView(ctx, kb, callerTenantID), nil),
	})
}

// UpdateKnowledgeBaseRequest defines the request body structure for updating a knowledge base
type UpdateKnowledgeBaseRequest struct {
	Name        string                     `json:"name"        binding:"required"`
	Description string                     `json:"description"`
	Config      *types.KnowledgeBaseConfig `json:"config"`
}

// UpdateKnowledgeBase godoc
// @Summary      Update Knowledge Base
// @Description  Cập nhật tên, mô tả và cấu hình kho tri thức
// @Tags         Kho tri thức
// @Accept       json
// @Produce      json
// @Param        id       path      string                     true  "Knowledge BaseID"
// @Param        request  body      UpdateKnowledgeBaseRequest true  "yêu cầu cập nhật"
// @Success      200      {object}  map[string]interface{}     "kho tri thức sau khi cập nhật"
// @Failure      400      {object}  errors.AppError            "tham số không hợp lệ"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases/{id} [put]
func (h *KnowledgeBaseHandler) UpdateKnowledgeBase(c *gin.Context) {
	ctx := c.Request.Context()
	logger.Info(ctx, "Start updating knowledge base")

	// Validate and get the knowledge base
	kb, id, _, permission, err := h.validateAndGetKnowledgeBase(c)
	if err != nil {
		c.Error(err)
		return
	}

	// Only admin/editor can update knowledge base. Platform-owned public
	// rows admit only an explicit human SuperAdmin (a public Viewer — even
	// a SuperAdmin-backed Viewer grant — is never an editor).
	if kb != nil && access.IsPlatformPublicKB(kb) {
		if !access.IsExplicitHumanSuperAdmin(ctx, types.CallerFromContext(ctx)) {
			c.Error(apperrors.NewForbiddenError("No permission to update knowledge base"))
			return
		}
	} else if permission != types.KBPermissionAdmin && permission != types.KBPermissionEditor {
		c.Error(apperrors.NewForbiddenError("No permission to update knowledge base"))
		return
	}

	// Parse request body
	var req UpdateKnowledgeBaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to parse request parameters", err)
		c.Error(apperrors.NewBadRequestError("Invalid request parameters").WithDetails(err.Error()))
		return
	}
	// Name and description belong to whoever manages the knowledge base;
	// its configuration (models, parsing, graph, wiki, tagging) is the
	// SuperAdmin's.
	if req.Config != nil && !canManagePlatformConfig(ctx) {
		c.Error(apperrors.NewForbiddenError("Knowledge base configuration requires system administrator access"))
		return
	}
	if req.Config != nil {
		probe := &types.KnowledgeBase{
			ChunkingConfig:        req.Config.ChunkingConfig,
			ImageProcessingConfig: req.Config.ImageProcessingConfig,
			WikiConfig:            req.Config.WikiConfig,
			ProfileConfig:         req.Config.ProfileConfig,
		}
		if err := validateKnowledgeBasePromptInstructions(probe); err != nil {
			c.Error(err)
			return
		}
	}

	logger.Infof(ctx, "Updating knowledge base, ID: %s, name: %s",
		secutils.SanitizeForLog(id), secutils.SanitizeForLog(req.Name))

	// Update the knowledge base
	updatedKB, err := h.service.UpdateKnowledgeBase(ctx, id, req.Name, req.Description, req.Config)
	if err != nil {
		// Owner-aware service denials (tenant-ownership, public-only
		// SuperAdmin) are typed AppErrors and must keep their status;
		// only raw infra errors become 500.
		if appErr, ok := apperrors.IsAppError(err); ok {
			c.Error(appErr)
			return
		}
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(apperrors.NewInternalServerError(err.Error()))
		return
	}

	logger.Infof(ctx, "Knowledge base updated successfully, ID: %s",
		secutils.SanitizeForLog(id))
	callerTenantID := c.GetUint64(types.TenantIDContextKey.String())
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    buildKBResponse(updatedKB, h.resolveKBStoreView(ctx, updatedKB, callerTenantID), nil),
	})
}

// UpdateKnowledgeBaseVisibilityRequest changes the KB scope.
// visibility = tenant | public. target_tenant_id carries the destination
// tenant for public→tenant transfers and must be omitted otherwise.
type UpdateKnowledgeBaseVisibilityRequest struct {
	// Visibility: tenant | restricted | published (owner Tenant Admin or
	// SuperAdmin) or public (SuperAdmin). Optional when only
	// member_default changes.
	Visibility     types.KBVisibility `json:"visibility"`
	TargetTenantID uint64             `json:"target_tenant_id"`
	// MemberDefault (optional): view | contribute — what owner-tenant
	// members may do on a 'tenant' KB. Owner Tenant Admin or SuperAdmin.
	MemberDefault *types.KBMemberDefault `json:"member_default"`
}

// UpdateKnowledgeBaseVisibility godoc
// @Summary      Đổi phạm vi hiển thị kho tri thức
// @Description  Chuyển phạm vi hiển thị kho tri thức tenant/restricted/published/public, member_default (view|contribute)
// @Tags         Kho tri thức
// @Accept       json
// @Produce      json
// @Param        id       path      string                                true  "Knowledge BaseID"
// @Param        request  body      UpdateKnowledgeBaseVisibilityRequest true  "yêu cầu đổi phạm vi hiển thị"
// @Success      200      {object}  map[string]interface{}               "kho tri thức sau khi cập nhật"
// @Failure      400      {object}  errors.AppError                      "tham số không hợp lệ"
// @Security     Bearer
// @Router       /knowledge-bases/{id}/visibility [put]
func (h *KnowledgeBaseHandler) UpdateKnowledgeBaseVisibility(c *gin.Context) {
	ctx := c.Request.Context()
	kb, id, _, _, err := h.validateAndGetKnowledgeBase(c)
	if err != nil {
		c.Error(err)
		return
	}
	_ = kb

	var req UpdateKnowledgeBaseVisibilityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(apperrors.NewBadRequestError("Invalid request parameters").WithDetails(err.Error()))
		return
	}
	if req.Visibility == "" && req.MemberDefault == nil {
		c.Error(apperrors.NewBadRequestError("visibility or member_default is required"))
		return
	}
	fail := func(err error) {
		if appErr, ok := apperrors.IsAppError(err); ok {
			c.Error(appErr)
			return
		}
		c.Error(apperrors.NewInternalServerError(err.Error()))
	}
	if req.Visibility != "" {
		if kb, err = h.service.SetKnowledgeBaseVisibility(ctx, id, req.Visibility, req.TargetTenantID); err != nil {
			fail(err)
			return
		}
	}
	if req.MemberDefault != nil {
		if kb, err = h.service.SetKnowledgeBaseMemberDefault(ctx, id, *req.MemberDefault); err != nil {
			fail(err)
			return
		}
	}
	callerTenantID := c.GetUint64(types.TenantIDContextKey.String())
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    buildKBResponse(kb, h.resolveKBStoreView(ctx, kb, callerTenantID), nil),
	})
}

// TransferKnowledgeBaseOwnerRequest moves a KB to another owner tenant.
type TransferKnowledgeBaseOwnerRequest struct {
	TargetTenantID uint64 `json:"target_tenant_id" binding:"required"`
}

// TransferKnowledgeBaseOwnerResponse is the transfer-owner payload: the
// updated KB plus what the transfer revoked.
type TransferKnowledgeBaseOwnerResponse struct {
	KnowledgeBase          interface{}        `json:"knowledge_base"`
	PreviousOwnerTenantID  uint64             `json:"previous_owner_tenant_id"`
	PreviousVisibility     types.KBVisibility `json:"previous_visibility"`
	RevokedInvitations     int64              `json:"revoked_invitations"`
	RevokedGrants          int64              `json:"revoked_grants"`
	WithdrawnFromPublished bool               `json:"withdrawn_from_published"`
}

// TransferKnowledgeBaseOwner godoc
// @Summary      Transfer knowledge base owner
// @Description  Explicit human SuperAdmin only. Moves a tenant-owned (tenant/restricted/published) or platform public KB to target_tenant_id without widening its scope: restricted stays restricted (only the target's Admins and SuperAdmins read it until the target shares it), tenant stays tenant, published is withdrawn to tenant (withdrawn_from_published=true), platform public becomes tenant. Data-scope tenant_id and member_default are kept; every invitation and tenant grant is revoked (revoked_reason owner_changed).
// @Tags         Kho tri thức
// @Accept       json
// @Produce      json
// @Param        id       path      string                             true  "Knowledge Base ID"
// @Param        request  body      TransferKnowledgeBaseOwnerRequest  true  "Target tenant"
// @Success      200      {object}  map[string]interface{}
// @Failure      400      {object}  errors.AppError
// @Failure      403      {object}  errors.AppError
// @Failure      404      {object}  errors.AppError
// @Security     Bearer
// @Router       /knowledge-bases/{id}/transfer-owner [post]
func (h *KnowledgeBaseHandler) TransferKnowledgeBaseOwner(c *gin.Context) {
	ctx := c.Request.Context()
	id := secutils.SanitizeForLog(c.Param("id"))
	if id == "" {
		_ = c.Error(apperrors.NewBadRequestError("knowledge base ID cannot be empty"))
		return
	}
	var req TransferKnowledgeBaseOwnerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apperrors.NewBadRequestError("target_tenant_id is required").WithDetails(err.Error()))
		return
	}
	res, err := h.service.TransferKnowledgeBaseOwner(ctx, c.Param("id"), req.TargetTenantID)
	if err != nil {
		if appErr, ok := apperrors.IsAppError(err); ok {
			_ = c.Error(appErr)
			return
		}
		logger.ErrorWithFields(ctx, err, map[string]interface{}{"knowledge_base_id": id})
		_ = c.Error(apperrors.NewInternalServerError(err.Error()))
		return
	}
	callerTenantID := c.GetUint64(types.TenantIDContextKey.String())
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": TransferKnowledgeBaseOwnerResponse{
			KnowledgeBase: buildKBResponse(res.KnowledgeBase,
				h.resolveKBStoreView(ctx, res.KnowledgeBase, callerTenantID), nil),
			PreviousOwnerTenantID:  res.PreviousOwnerTenantID,
			PreviousVisibility:     res.PreviousVisibility,
			RevokedInvitations:     res.RevokedInvitations,
			RevokedGrants:          res.RevokedGrants,
			WithdrawnFromPublished: res.WithdrawnFromPublished,
		},
	})
}

// GenerateKnowledgeBaseProfile godoc
// @Summary      Sinh mô tả kho tri thức
// @Description  Dựa trên tổng hợp hồ sơ tài liệu, sinh lại ngay mô tả AI của kho tri thức (không ghi đè mô tả viết tay)
// @Tags         Kho tri thức
// @Produce      json
// @Param        id   path      string  true  "Knowledge BaseID"
// @Success      200  {object}  map[string]interface{}  "hồ sơ kho tri thức đã sinh"
// @Failure      400  {object}  errors.AppError         "loại kho tri thức không hỗ trợ hoặc chưa cấu hình mô hình"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases/{id}/profile/generate [post]
func (h *KnowledgeBaseHandler) GenerateKnowledgeBaseProfile(c *gin.Context) {
	ctx := c.Request.Context()
	kb, id, _, permission, err := h.validateAndGetKnowledgeBase(c)
	if err != nil {
		_ = c.Error(err)
		return
	}
	if permission != types.KBPermissionAdmin && permission != types.KBPermissionEditor {
		// Platform-owned public rows admit only an explicit human
		// SuperAdmin for profile regeneration; cross-tenant Viewers stay
		// denied.
		if !access.IsPlatformPublicKB(kb) ||
			!access.IsExplicitHumanSuperAdmin(ctx, types.CallerFromContext(ctx)) {
			_ = c.Error(apperrors.NewForbiddenError("No permission to update knowledge base"))
			return
		}
	}
	if h.profileService == nil {
		_ = c.Error(apperrors.NewInternalServerError("knowledge base profile service unavailable"))
		return
	}
	genCtx, cancel := context.WithTimeout(ctx, knowledgeBaseProfileRequestTimeout)
	defer cancel()
	profile, err := h.profileService.GenerateKnowledgeBaseProfile(genCtx, kb, true)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{"knowledge_base_id": id})
		switch {
		case stderrors.Is(err, types.ErrKnowledgeBaseProfileUnsupported),
			stderrors.Is(err, types.ErrKnowledgeBaseProfileModelNotConfigured):
			_ = c.Error(apperrors.NewBadRequestError(err.Error()))
		default:
			_ = c.Error(apperrors.NewInternalServerError(err.Error()))
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": profile})
}

// knowledgeBaseProfileRequestTimeout bounds the synchronous regeneration a
// user triggers from the settings dialog: one aggregation plus one small
// model call.
const knowledgeBaseProfileRequestTimeout = 2 * time.Minute

// DeleteKnowledgeBase godoc
// @Summary      Delete Knowledge Base
// @Description  Xoá kho tri thức chỉ định cùng toàn bộ nội dung
// @Tags         Kho tri thức
// @Accept       json
// @Produce      json
// @Param        id   path      string  true  "Knowledge BaseID"
// @Success      200  {object}  map[string]interface{}  "xoá thành công"
// @Failure      400  {object}  errors.AppError         "tham số không hợp lệ"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases/{id} [delete]
func (h *KnowledgeBaseHandler) DeleteKnowledgeBase(c *gin.Context) {
	ctx := c.Request.Context()
	logger.Info(ctx, "Start deleting knowledge base")

	// Validate and get the knowledge base
	kb, id, _, permission, err := h.validateAndGetKnowledgeBase(c)
	if err != nil {
		c.Error(err)
		return
	}

	// Deleting a KB is a Manage decision (access.DecideKB, carried by the
	// guard's grant): an explicit human SuperAdmin on every KB, or the
	// owner tenant's Tenant Admin. Members contribute but never delete.
	if permission != types.KBPermissionAdmin {
		c.Error(apperrors.NewForbiddenError("Only knowledge base owner can delete"))
		return
	}

	logger.Infof(ctx, "Deleting knowledge base, ID: %s, name: %s",
		secutils.SanitizeForLog(id), secutils.SanitizeForLog(kb.Name))

	// Delete the knowledge base
	if err := h.service.DeleteKnowledgeBase(ctx, id); err != nil {
		if appErr, ok := apperrors.IsAppError(err); ok {
			c.Error(appErr)
			return
		}
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(apperrors.NewInternalServerError(err.Error()))
		return
	}

	logger.Infof(ctx, "Knowledge base deleted successfully, ID: %s",
		secutils.SanitizeForLog(id))
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Knowledge base deleted successfully",
	})
}

type CopyKnowledgeBaseRequest struct {
	TaskID   string `json:"task_id"`
	SourceID string `json:"source_id" binding:"required"`
	TargetID string `json:"target_id"`
}

// CopyKnowledgeBaseResponse defines the response for copy knowledge base
type CopyKnowledgeBaseResponse struct {
	TaskID   string `json:"task_id"`
	SourceID string `json:"source_id"`
	TargetID string `json:"target_id"`
	Message  string `json:"message"`
}

type DuplicateKnowledgeBaseResponse struct {
	SourceID      string      `json:"source_id"`
	TargetID      string      `json:"target_id"`
	Message       string      `json:"message"`
	KnowledgeBase interface{} `json:"knowledge_base"`
}

// CopyKnowledgeBase godoc
// @Summary      Sao chép kho tri thức
// @Description  Sao chép nội dung một kho tri thức sang kho khác (tác vụ bất đồng bộ)
// @Tags         Kho tri thức
// @Accept       json
// @Produce      json
// @Param        request  body      CopyKnowledgeBaseRequest   true  "yêu cầu sao chép"
// @Success      200      {object}  map[string]interface{}     "ID tác vụ"
// @Failure      400      {object}  errors.AppError            "tham số không hợp lệ"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases/copy [post]
func (h *KnowledgeBaseHandler) CopyKnowledgeBase(c *gin.Context) {
	ctx := c.Request.Context()
	var req CopyKnowledgeBaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to parse request parameters", err)
		c.Error(apperrors.NewBadRequestError("Invalid request parameters").WithDetails(err.Error()))
		return
	}

	caller := types.CallerFromContext(ctx)
	if caller.TenantID == 0 {
		_ = c.Error(errors.NewUnauthorizedError("Unauthorized"))
		return
	}
	tenantID := caller.TenantID
	sourceGrant, err := resolveHandlerKBAccessFor(c, req.SourceID, h.service, h.kbAccessGrantService, types.KBPermissionViewer)
	if err != nil {
		c.Error(err)
		return
	}
	sourceKB := sourceGrant.KnowledgeBase
	if sourceKB.TenantID != caller.TenantID {
		_ = c.Error(errors.NewForbiddenError("No permission to copy this knowledge base"))
		return
	}
	taskID := req.TaskID
	if taskID == "" {
		taskID = utils.GenerateTaskID("kb_clone", caller.TenantID, req.SourceID)
	} else if err := requireTaskProgressTenant(ctx, taskID); err != nil {
		_ = c.Error(err)
		return
	}
	create := req.TargetID == ""
	targetKB := &types.KnowledgeBase{ID: uuid.NewString(), TenantID: caller.TenantID}
	if create {
		if !types.IsSyntheticUserID(caller.UserID) {
			targetKB.CreatorID = caller.UserID
		}
	} else {
		targetGrant, err := resolveHandlerKBAccessFor(c, req.TargetID, h.service, h.kbAccessGrantService, types.KBPermissionEditor)
		if err != nil {
			_ = c.Error(err)
			return
		}
		targetKB = targetGrant.KnowledgeBase
		if targetKB.TenantID != caller.TenantID {
			c.Error(errors.NewForbiddenError("No permission to copy to this knowledge base"))
			return
		}
		if err := middleware.EvaluateOwnershipOrRole(c.Request.Context(),
			h.cfg,
			types.TenantRoleAdmin,
			func() (string,
				error,
			) {
				return targetKB.CreatorID,
					nil
			}); err != nil {
			_ = c.Error(errors.NewForbiddenError("No permission to replace this knowledge base's contents"))
			return
		}
		tenant, _ := ctx.Value(types.TenantInfoContextKey).(*types.Tenant)
		if err := access.ValidateKBTransferCompatibility(sourceKB,
			targetKB,
			access.KBTransferClone,
			"",
			tenant); err != nil {
			_ = c.Error(errors.NewBadRequestError(err.Error()))
			return
		}
	}
	ctx, err = access.WithKBTransfer(c.Request.Context(), sourceKB, targetKB, access.KBTransferClone, taskID, create)
	if err != nil {
		_ = c.Error(kbAccessHTTPError(err))
		return
	}
	// Reserve the destination in the payload; enqueue failures do not leave
	// empty KBs and every worker delivery creates/resumes the same destination.
	req.TargetID = targetKB.ID

	// Create KB clone payload
	payload := types.KBClonePayload{
		TenantID:     tenantID,
		TaskID:       taskID,
		SourceID:     req.SourceID,
		TargetID:     req.TargetID,
		CreateTarget: create,
		CreatorID:    targetKB.CreatorID,
		Initiator:    types.TaskInitiatorFromContext(ctx),
	}
	langfuse.InjectTracing(ctx, &payload)

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		logger.Errorf(ctx, "Failed to marshal KB clone payload: %v", err)
		c.Error(apperrors.NewInternalServerError("Failed to create task"))
		return
	}

	// Enqueue KB clone task to Asynq
	task := asynq.NewTask(types.TypeKBClone, payloadBytes,
		asynq.TaskID(taskID), asynq.Queue(types.QueueMaintenance),
		asynq.MaxRetry(3), asynq.Timeout(2*time.Hour))
	info, err := h.asynqClient.Enqueue(task)
	if err != nil {
		logger.Errorf(ctx, "Failed to enqueue KB clone task: %v", err)
		c.Error(apperrors.NewInternalServerError("Failed to enqueue task"))
		return
	}

	logger.Infof(ctx, "KB clone task enqueued: %s, asynq task ID: %s, source: %s, target: %s",
		taskID, info.ID, secutils.SanitizeForLog(req.SourceID), secutils.SanitizeForLog(req.TargetID))

	// Save initial progress to Redis so frontend can query immediately
	initialProgress := &types.KBCloneProgress{
		TaskID:    taskID,
		SourceID:  req.SourceID,
		TargetID:  req.TargetID,
		Status:    types.KBCloneStatusPending,
		Progress:  0,
		Message:   "Task queued, waiting to start...",
		CreatedAt: time.Now().Unix(),
		UpdatedAt: time.Now().Unix(),
	}
	if err := h.knowledgeService.SaveKBCloneProgress(ctx, initialProgress); err != nil {
		logger.Warnf(ctx, "Failed to save initial KB clone progress: %v", err)
		// Don't fail the request, task is already enqueued
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": CopyKnowledgeBaseResponse{
			TaskID:   taskID,
			SourceID: req.SourceID,
			TargetID: req.TargetID,
			Message:  "Knowledge base copy task started",
		},
	})
}

// DuplicateKnowledgeBase godoc
// @Summary      Tạo bản sao kho tri thức
// @Description  Tạo bản sao kho tri thức chỉ gồm cài đặt, không sao chép tài liệu, FAQ, đoạn (chunk), chỉ mục, trang Wiki, chia sẻ hay trạng thái ghim
// @Tags         Kho tri thức
// @Accept       json
// @Produce      json
// @Param        id       path      string                  true  "ID kho tri thức nguồn"
// @Success      201      {object}  map[string]interface{}  "bản sao kho tri thức đã tạo"
// @Failure      400      {object}  errors.AppError                 "tham số không hợp lệ"
// @Security     Bearer
// @Router       /knowledge-bases/{id}/duplicate [post]
func (h *KnowledgeBaseHandler) DuplicateKnowledgeBase(c *gin.Context) {
	ctx := c.Request.Context()
	sourceID := c.Param("id")
	if sourceID == "" {
		c.Error(apperrors.NewBadRequestError("Knowledge base ID cannot be empty"))
		return
	}

	callerTenantID := c.GetUint64(types.TenantIDContextKey.String())
	sourceKB, err := h.service.GetKnowledgeBaseByID(ctx, sourceID)
	if err != nil {
		if stderrors.Is(err, repository.ErrKnowledgeBaseNotFound) {
			c.Error(errors.NewNotFoundError("Source knowledge base not found"))
			return
		}
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	if sourceKB.TenantID != callerTenantID {
		logger.Warnf(ctx,
			"Knowledge base duplicate rejected: source belongs to another tenant, source_id: %s, caller_tenant: %d, kb_tenant: %d",
			secutils.SanitizeForLog(sourceID), callerTenantID, sourceKB.TenantID)
		c.Error(errors.NewForbiddenError("No permission to duplicate this knowledge base"))
		return
	}

	targetKB, err := h.service.DuplicateKnowledgeBase(ctx, sourceID)
	if err != nil {
		if appErr, ok := apperrors.IsAppError(err); ok {
			c.Error(appErr)
			return
		}
		if stderrors.Is(err, repository.ErrKnowledgeBaseNotFound) {
			c.Error(errors.NewNotFoundError("Source knowledge base not found"))
			return
		}
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}

	logger.Infof(ctx, "Knowledge base duplicate created, source: %s, target: %s",
		secutils.SanitizeForLog(sourceID), secutils.SanitizeForLog(targetKB.ID))
	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"data": DuplicateKnowledgeBaseResponse{
			SourceID:      sourceID,
			TargetID:      targetKB.ID,
			Message:       "Knowledge base duplicate created",
			KnowledgeBase: buildKBResponse(targetKB, h.resolveKBStoreView(ctx, targetKB, callerTenantID), nil),
		},
	})
}

// GetKBCloneProgress godoc
// @Summary      Lấy tiến độ sao chép kho tri thức
// @Description  Lấy tiến độ tác vụ sao chép kho tri thức
// @Tags         Kho tri thức
// @Accept       json
// @Produce      json
// @Param        task_id  path      string  true  "ID tác vụ"
// @Success      200      {object}  map[string]interface{}  "thông tin tiến độ"
// @Failure      404      {object}  errors.AppError         "không tìm thấy tác vụ"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases/copy/progress/{task_id} [get]
func (h *KnowledgeBaseHandler) GetKBCloneProgress(c *gin.Context) {
	ctx := c.Request.Context()

	taskID := c.Param("task_id")
	if taskID == "" {
		logger.Error(ctx, "Task ID is empty")
		c.Error(apperrors.NewBadRequestError("Task ID cannot be empty"))
		return
	}
	if err := requireTaskProgressTenant(ctx, taskID); err != nil {
		c.Error(err)
		return
	}

	progress, err := h.knowledgeService.GetKBCloneProgress(ctx, taskID)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    progress,
	})
}

// validateExtractConfig validates the graph configuration parameters
func validateExtractConfig(config *types.ExtractConfig) error {
	if config == nil {
		return nil
	}
	if !config.Enabled {
		config.Enabled = false
		return nil
	}
	// Validate text field
	if config.Text == "" {
		return apperrors.NewBadRequestError("text cannot be empty")
	}

	// Validate tags field
	if len(config.Tags) == 0 {
		return apperrors.NewBadRequestError("tags cannot be empty")
	}
	for i, tag := range config.Tags {
		if tag == "" {
			return apperrors.NewBadRequestError("tag cannot be empty at index " + strconv.Itoa(i))
		}
	}

	// Validate nodes
	if len(config.Nodes) == 0 {
		return apperrors.NewBadRequestError("nodes cannot be empty")
	}
	nodeNames := make(map[string]bool)
	for i, node := range config.Nodes {
		if node.Name == "" {
			return apperrors.NewBadRequestError("node name cannot be empty at index " + strconv.Itoa(i))
		}
		// Check for duplicate node names
		if nodeNames[node.Name] {
			return apperrors.NewBadRequestError("duplicate node name: " + node.Name)
		}
		nodeNames[node.Name] = true
	}

	if len(config.Relations) == 0 {
		return apperrors.NewBadRequestError("relations cannot be empty")
	}
	// Validate relations
	for i, relation := range config.Relations {
		if relation.Node1 == "" {
			return apperrors.NewBadRequestError("relation node1 cannot be empty at index " + strconv.Itoa(i))
		}
		if relation.Node2 == "" {
			return apperrors.NewBadRequestError("relation node2 cannot be empty at index " + strconv.Itoa(i))
		}
		if relation.Type == "" {
			return apperrors.NewBadRequestError("relation type cannot be empty at index " + strconv.Itoa(i))
		}
		// Check if referenced nodes exist
		if !nodeNames[relation.Node1] {
			return apperrors.NewBadRequestError("relation references non-existent node1: " + relation.Node1)
		}
		if !nodeNames[relation.Node2] {
			return apperrors.NewBadRequestError("relation references non-existent node2: " + relation.Node2)
		}
	}

	return nil
}

func validateKnowledgeBasePromptInstructions(kb *types.KnowledgeBase) error {
	if err := types.ValidateKnowledgeBasePromptInstructions(kb); err != nil {
		return apperrors.NewBadRequestError(err.Error())
	}
	return nil
}

// ListMoveTargets returns knowledge bases eligible as move targets for the given source KB.
// Filters: same Type, same EmbeddingModelID, different ID, not temporary.
//
// ListMoveTargets godoc
// @Summary      Lấy danh sách kho tri thức đích có thể di chuyển tới
// @Description  Trả về các kho tri thức đích cùng Type, cùng EmbeddingModelID với kho nguồn, không tạm thời và không phải chính nó
// @Tags         Kho tri thức
// @Produce      json
// @Param        id   path      string                  true  "ID kho tri thức nguồn"
// @Success      200  {object}  map[string]interface{}  "danh sách đích có thể di chuyển"
// @Failure      400  {object}  errors.AppError         "tham số không hợp lệ"
// @Failure      404  {object}  errors.AppError         "không tìm thấy kho tri thức"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases/{id}/move-targets [get]
func (h *KnowledgeBaseHandler) ListMoveTargets(c *gin.Context) {
	ctx := c.Request.Context()

	sourceKBID := c.Param("id")
	if sourceKBID == "" {
		c.Error(apperrors.NewBadRequestError("Knowledge base ID is required"))
		return
	}

	tenantID, exists := c.Get(types.TenantIDContextKey.String())
	if !exists {
		c.Error(apperrors.NewUnauthorizedError("Unauthorized"))
		return
	}

	// Get source knowledge base
	sourceKB, err := h.service.GetKnowledgeBaseByID(ctx, sourceKBID)
	if err != nil {
		if stderrors.Is(err, repository.ErrKnowledgeBaseNotFound) {
			c.Error(errors.NewNotFoundError("Source knowledge base not found"))
			return
		}
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	if sourceKB.TenantID != tenantID.(uint64) {
		c.Error(errors.NewForbiddenError("No permission to access this knowledge base"))
		return
	}

	// Get all knowledge bases
	allKBs, err := h.service.ListKnowledgeBases(ctx)
	if err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}

	// Filter eligible targets. Platform-owned public rows are excluded:
	// moving tenant content is a tenant-scoped operation and the
	// tenant-scoped contract of this endpoint predates the public catalog
	// (Task 4 preserves it). Content moves into public KBs stay governed
	// by the owner-aware manage guard on the move path itself.
	targets := make([]*types.KnowledgeBase, 0)
	for _, kb := range allKBs {
		if kb.ID == sourceKBID {
			continue
		}
		if access.IsPlatformPublicKB(kb) {
			continue
		}
		if kb.IsTemporary {
			continue
		}
		if kb.Type != sourceKB.Type {
			continue
		}
		if kb.EmbeddingModelID != sourceKB.EmbeddingModelID {
			continue
		}
		// A move target must be writable by the caller: the readable
		// list also carries ACL (view-only) and published rows, which
		// are never move destinations.
		if kb.TenantID != sourceKB.TenantID {
			continue
		}
		if _, derr := access.DecideKB(ctx, types.CallerFromContext(ctx), kb, access.ActionContribute,
			access.KBLookups{}); derr != nil {
			continue
		}
		targets = append(targets, kb)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    targets,
	})
}
