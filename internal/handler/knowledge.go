package handler

import (
	"context"
	"encoding/json"
	goerrors "errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/filetransport"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/tracing/langfuse"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/utils"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/gin-gonic/gin"
	"github.com/hibiken/asynq"
)

// KnowledgeHandler processes HTTP requests related to knowledge resources
type KnowledgeHandler struct {
	cfg                  *config.Config
	kgService            interfaces.KnowledgeService
	kbService            interfaces.KnowledgeBaseService
	kbAccessGrantService interfaces.KBAccessGrantService
	asynqClient          interfaces.TaskEnqueuer
	spanRepo             repository.KnowledgeSpanRepository
}

// NewKnowledgeHandler creates a new knowledge handler instance
func NewKnowledgeHandler(
	cfg *config.Config,
	kgService interfaces.KnowledgeService,
	kbService interfaces.KnowledgeBaseService,
	kbAccessGrantService interfaces.KBAccessGrantService,
	asynqClient interfaces.TaskEnqueuer,
	spanRepo repository.KnowledgeSpanRepository,
) *KnowledgeHandler {
	return &KnowledgeHandler{
		cfg:                  cfg,
		kgService:            kgService,
		kbService:            kbService,
		kbAccessGrantService: kbAccessGrantService,
		asynqClient:          asynqClient,
		spanRepo:             spanRepo,
	}
}

// requireKBOwnershipOrAdmin enforces the same "KB creator OR Admin+" matrix
// used by OwnedKBOrAdmin for routes whose KB id comes from the request body.
func (h *KnowledgeHandler) requireKBOwnershipOrAdmin(c *gin.Context, kbID string) error {
	evalErr := middleware.EvaluateOwnershipOrRole(
		c.Request.Context(),
		h.cfg,
		types.TenantRoleAdmin,
		func() (string, error) { return resolveKBCreatorByKBID(c, h.kbService, kbID) },
	)
	if evalErr == nil {
		return nil
	}
	if goerrors.Is(evalErr, middleware.ErrResourceNotFound) {
		return errors.NewNotFoundError("knowledge base not found")
	}
	if goerrors.Is(evalErr, middleware.ErrOwnershipForbidden) {
		return errors.NewForbiddenError("No permission to operate on this knowledge base")
	}
	logger.ErrorWithFields(c.Request.Context(), evalErr, map[string]interface{}{
		"kb_id": secutils.SanitizeForLog(kbID),
	})
	return errors.NewInternalServerError("cannot verify knowledge base ownership")
}

// validateKnowledgeBaseAccess validates access permissions to a knowledge base
// using the ":id" URL path parameter. It delegates to validateKnowledgeBaseAccessWithKBID.
func (h *KnowledgeHandler) validateKnowledgeBaseAccess(c *gin.Context) (*types.KnowledgeBase, string, uint64, types.KBPermission, error) {
	kbID := secutils.SanitizeForLog(c.Param("id"))
	return h.validateKnowledgeBaseAccessWithKBID(c, kbID)
}

// validateKnowledgeBaseAccessWithKBID validates access to the given knowledge base ID (e.g. from query or body).
// Enforces per-API-key KB scope before tenant/share/agent resolution.
// Returns the knowledge base, kbID, effective tenant ID, permission, and error.
func (h *KnowledgeHandler) validateKnowledgeBaseAccessWithKBID(
	c *gin.Context,
	kbID string,
) (*types.KnowledgeBase, string, uint64, types.KBPermission, error) {
	kbID = secutils.SanitizeForLog(kbID)
	grant, err := resolveHandlerKBAccess(c, kbID, h.kbService, h.kbAccessGrantService)
	if err != nil {
		return nil, kbID, 0, "", err
	}
	return grant.KnowledgeBase, kbID, grant.EffectiveTenantID, grant.Permission, nil
}

func (h *KnowledgeHandler) validateKnowledgeBaseWriteAccessWithKBID(
	c *gin.Context,
	kbID string,
) (*types.KnowledgeBase, string, uint64, types.KBPermission, error) {
	grant, err := resolveHandlerKBManageAccessFor(
		c,
		kbID,
		h.kbService,
		h.kbAccessGrantService,
	)
	if err != nil {
		return nil, kbID, 0, "", err
	}
	if err := access.RequireKBManage(grant.Context(c.Request.Context()), grant.KnowledgeBase); err != nil {
		return nil, kbID, 0, "", kbAccessHTTPError(err)
	}
	return grant.KnowledgeBase, kbID, grant.EffectiveTenantID, grant.Permission, nil
}

// resolveKnowledgeAndValidateKBAccess resolves a document and checks its parent
// KB using the same policy as KB routes. The persisted document supplies the KB
// id and data-scope reference; the KB row itself is loaded for owner-aware
// resolution so platform-public rows keep their recorded data scope.
// Reads use ResolveKB; mutations use the owner-aware manage decision so
// platform-owned rows admit only an explicit human SuperAdmin.
func (h *KnowledgeHandler) resolveKnowledgeAndValidateKBAccess(
	c *gin.Context,
	knowledgeID string,
	requiredPermission types.KBPermission,
) (*types.Knowledge, context.Context, error) {
	ctx := c.Request.Context()
	request := middleware.KBAccessRequest(c)
	request.Caller = request.Caller.Normalize()
	// Tenantless callers: an explicit human SuperAdmin always proceeds; a
	// tenantless authenticated human (Guest) proceeds only for Viewer
	// reads, where DecideKB admits public/published KBs and denies the
	// rest (same rule as the route's KBAccessRead guard). Tenantless
	// mutation stays SuperAdmin-only.
	if request.Caller.TenantID == 0 && !access.IsExplicitHumanSuperAdmin(ctx, request.Caller) &&
		(requiredPermission != types.KBPermissionViewer || !access.IsAuthenticatedHuman(ctx, request.Caller)) {
		return nil, ctx, errors.NewUnauthorizedError("Unauthorized")
	}
	knowledge, err := h.kgService.GetKnowledgeByIDOnly(ctx, knowledgeID)
	if err != nil || knowledge == nil {
		return nil, ctx, errors.NewNotFoundError("Knowledge not found")
	}
	if err := requireTenantAPIKeyKnowledgeBase(ctx, knowledge.KnowledgeBaseID); err != nil {
		return nil, ctx, err
	}
	if grant, ok := resolvedKBAccess(c, knowledge.KnowledgeBaseID, requiredPermission); ok {
		if grant.EffectiveTenantID != knowledge.TenantID {
			return nil, ctx, errors.NewForbiddenError("Permission denied to access this knowledge")
		}
		return knowledge, grant.Context(ctx), nil
	}
	kb := &types.KnowledgeBase{ID: knowledge.KnowledgeBaseID, TenantID: knowledge.TenantID}
	if h.kbService != nil {
		if loaded, lerr := h.kbService.GetKnowledgeBaseByID(ctx, knowledge.KnowledgeBaseID); lerr == nil && loaded != nil {
			kb = loaded
		}
	}
	var grant *access.KBAccess
	if requiredPermission == types.KBPermissionViewer {
		if invites, ok := h.kbAccessGrantService.(access.KBInviteLookup); ok {
			grant, err = access.ResolveKBWithInvite(ctx, request, kb, requiredPermission,
				h.kbAccessGrantService, invites)
		} else {
			grant, err = access.ResolveKB(ctx, request, kb, requiredPermission, h.kbAccessGrantService)
		}
	} else {
		grant, err = access.ResolveKBForManage(ctx, request, kb)
	}
	if goerrors.Is(err, access.ErrForbidden) {
		return nil, ctx, errors.NewForbiddenError("Permission denied to access this knowledge")
	}
	if err != nil {
		return nil, ctx, kbAccessHTTPError(err)
	}
	if grant.EffectiveTenantID != knowledge.TenantID {
		return nil, ctx, errors.NewForbiddenError("Permission denied to access this knowledge")
	}
	return knowledge, grant.Context(ctx), nil
}

// resolveKnowledgeAndValidateKBDownload resolves a document for original-file
// download through the dedicated download decision. Invitation reads are
// never consulted, so invite Viewers keep their no-download behavior while
// platform-public Viewers may fetch originals through the recorded data scope.
func (h *KnowledgeHandler) resolveKnowledgeAndValidateKBDownload(
	c *gin.Context,
	knowledgeID string,
) (*types.Knowledge, context.Context, error) {
	ctx := c.Request.Context()
	request := middleware.KBAccessRequest(c)
	request.Caller = request.Caller.Normalize()
	// A tenantless authenticated human (Guest) may download originals of
	// public/published KBs; ResolveKBForDownload (DecideKB) decides.
	if request.Caller.TenantID == 0 && !access.IsExplicitHumanSuperAdmin(ctx, request.Caller) &&
		!access.IsAuthenticatedHuman(ctx, request.Caller) {
		return nil, ctx, errors.NewUnauthorizedError("Unauthorized")
	}
	knowledge, err := h.kgService.GetKnowledgeByIDOnly(ctx, knowledgeID)
	if err != nil || knowledge == nil {
		return nil, ctx, errors.NewNotFoundError("Knowledge not found")
	}
	if err := requireTenantAPIKeyKnowledgeBase(ctx, knowledge.KnowledgeBaseID); err != nil {
		return nil, ctx, err
	}
	// Never reuse a stashed read grant here: a Viewer stash may come from a
	// recipient-bound invitation, which must not confer original download.
	// The download decision is always re-resolved against the KB row (one
	// extra lookup per download); the route guard already enforced the same
	// policy, so this is a consistent second check, not a wider one.
	kb := &types.KnowledgeBase{ID: knowledge.KnowledgeBaseID, TenantID: knowledge.TenantID}
	if h.kbService != nil {
		if loaded, lerr := h.kbService.GetKnowledgeBaseByID(ctx, knowledge.KnowledgeBaseID); lerr == nil && loaded != nil {
			kb = loaded
		}
	}
	grant, err := access.ResolveKBForDownload(ctx, request, kb)
	if goerrors.Is(err, access.ErrForbidden) {
		return nil, ctx, errors.NewForbiddenError("Permission denied to access this knowledge")
	}
	if err != nil {
		return nil, ctx, kbAccessHTTPError(err)
	}
	if grant.EffectiveTenantID != knowledge.TenantID {
		return nil, ctx, errors.NewForbiddenError("Permission denied to access this knowledge")
	}
	return knowledge, grant.Context(ctx), nil
}

// handleDuplicateKnowledgeError handles cases where duplicate knowledge is detected
// Returns true if the error was a duplicate error and was handled, false otherwise
func (h *KnowledgeHandler) handleDuplicateKnowledgeError(c *gin.Context,
	err error, knowledge *types.Knowledge, duplicateType string,
) bool {
	if dupErr, ok := err.(*types.DuplicateKnowledgeError); ok {
		ctx := c.Request.Context()
		logger.Warnf(ctx, "Detected duplicate %s: %s", duplicateType, secutils.SanitizeForLog(dupErr.Error()))
		c.JSON(http.StatusConflict, gin.H{
			"success": false,
			"message": dupErr.Error(),
			"data":    knowledge, // knowledge contains the existing document
			"code":    fmt.Sprintf("duplicate_%s", duplicateType),
		})
		return true
	}
	return false
}

// enqueueKnowledgeListDelete enqueues an async batch-delete task for the
// given knowledge IDs and returns the asynq task ID.
func (h *KnowledgeHandler) enqueueKnowledgeListDelete(
	ctx context.Context, tenantID uint64, kbID string, ids []string,
) (string, error) {
	payload := types.KnowledgeListDeletePayload{
		KnowledgeBaseID: kbID,
		TenantID:        tenantID,
		KnowledgeIDs:    ids,
		Initiator:       types.TaskInitiatorFromContext(ctx),
	}
	langfuse.InjectTracing(ctx, &payload)
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal payload: %w", err)
	}
	task := asynq.NewTask(types.TypeKnowledgeListDelete, payloadBytes,
		asynq.Queue(types.QueueMaintenance), asynq.MaxRetry(3), asynq.Timeout(2*time.Hour))
	info, err := h.asynqClient.Enqueue(task)
	if err != nil {
		return "", fmt.Errorf("enqueue task: %w", err)
	}
	return info.ID, nil
}

// enqueueKnowledgeListReparse enqueues an async batch-reparse task for the
// given knowledge IDs and returns the asynq task ID.
func (h *KnowledgeHandler) enqueueKnowledgeListReparse(
	ctx context.Context, tenantID uint64, kbID string, ids []string, processConfig *types.KnowledgeProcessOverrides,
) (string, error) {
	payload := types.KnowledgeListReparsePayload{
		KnowledgeBaseID: kbID,
		TenantID:        tenantID,
		KnowledgeIDs:    ids,
		ProcessConfig:   processConfig,
		Initiator:       types.TaskInitiatorFromContext(ctx),
	}
	langfuse.InjectTracing(ctx, &payload)
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal payload: %w", err)
	}
	task := asynq.NewTask(types.TypeKnowledgeListReparse, payloadBytes,
		asynq.Queue(types.QueueMaintenance), asynq.MaxRetry(3), asynq.Timeout(time.Hour))
	info, err := h.asynqClient.Enqueue(task)
	if err != nil {
		return "", fmt.Errorf("enqueue task: %w", err)
	}
	return info.ID, nil
}

// CreateKnowledgeFromFile godoc
// @Summary      Tạo tài liệu từ tệp
// @Description  Tải tệp lên và tạo mục tài liệu
// @Tags         Tài liệu
// @Accept       multipart/form-data
// @Produce      json
// @Param        id                path      string  true   "Knowledge BaseID"
// @Param        file              formData  file    true   "Tệp tải lên"
// @Param        fileName          formData  string  false  "Tên tệp tuỳ chỉnh"
// @Param        metadata          formData  string  false  "Metadata dạng JSON"
// @Param        enable_multimodel formData  bool    false  "Bật xử lý đa phương thức"
// @Param        tag_ids       formData  string  false  "Danh sách ID thẻ, phân cách bằng dấu phẩy"
// @Param        process_config    formData  string  false  "Cấu hình xử lý dạng JSON (KnowledgeProcessOverrides)"
// @Success      200               {object}  map[string]interface{}  "Tài liệu đã tạo"
// @Failure      400               {object}  errors.AppError         "tham số yêu cầu không hợp lệ"
// @Failure      409               {object}  map[string]interface{}  "tệp bị trùng"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases/{id}/knowledge/file [post]
func (h *KnowledgeHandler) CreateKnowledgeFromFile(c *gin.Context) {
	ctx := c.Request.Context()
	logger.Info(ctx, "Start creating knowledge from file")

	// Validate management access to the knowledge base (owning tenant or an
	// explicit human SuperAdmin on platform-owned rows); public Viewers stay
	// denied by the permission check below.
	_, kbID, effectiveTenantID, permission, err := h.validateKnowledgeBaseWriteAccessWithKBID(c, c.Param("id"))
	if err != nil {
		c.Error(err)
		return
	}
	ctx = types.WithExecutionTenant(c.Request.Context(), effectiveTenantID)

	// Check write permission
	if permission != types.KBPermissionAdmin && permission != types.KBPermissionEditor {
		c.Error(errors.NewForbiddenError("No permission to create knowledge"))
		return
	}

	// Validate file size — read MAX_FILE_SIZE_MB env (50MB default).
	// Deliberately not a runtime system_setting; see filesize.go for the
	// rationale (nginx / docreader / browser bundle all cache this at
	// container startup, so a UI knob would silently mismatch).
	maxSizeMB := utils.GetMaxFileSizeMB()
	maxSize := maxSizeMB * 1024 * 1024
	// Capped before the multipart parse, not after: FormFile buffers the whole
	// body first, so the size check below only ever sees an upload we already
	// accepted. nginx location /api/ still enforces MAX_FILE_SIZE; this is the
	// same cap for requests that reach the app without that proxy.
	limitUploadBody(c, maxSize)

	// Get the uploaded file
	file, err := c.FormFile("file")
	if err != nil {
		if isRequestBodyTooLarge(err) {
			logger.Error(ctx, "File size too large")
			c.Error(errors.NewBadRequestError(fmt.Sprintf("Tệp vượt quá giới hạn %d MB.", maxSizeMB)))
			return
		}
		logger.Error(ctx, "File upload failed", err)
		c.Error(errors.NewBadRequestError("File upload failed").WithDetails(err.Error()))
		return
	}
	if file.Size > maxSize {
		logger.Error(ctx, "File size too large")
		c.Error(errors.NewBadRequestError(fmt.Sprintf("Tệp vượt quá giới hạn %d MB.", maxSizeMB)))
		return
	}

	// Get custom filename if provided (for folder uploads with path)
	customFileName := c.PostForm("fileName")
	customFileName = secutils.SanitizeForLog(customFileName)
	displayFileName := file.Filename
	displayFileName = secutils.SanitizeForLog(displayFileName)
	if customFileName != "" {
		displayFileName = customFileName
		logger.Infof(ctx, "Using custom filename: %s (original: %s)", customFileName, displayFileName)
	}

	logger.Infof(ctx, "File upload successful, filename: %s, size: %.2f KB", displayFileName, float64(file.Size)/1024)
	logger.Infof(ctx, "Creating knowledge, knowledge base ID: %s, filename: %s", kbID, displayFileName)

	// Parse metadata if provided
	var metadata map[string]string
	metadataStr := c.PostForm("metadata")
	if metadataStr != "" {
		if err := json.Unmarshal([]byte(metadataStr), &metadata); err != nil {
			logger.Error(ctx, "Failed to parse metadata", err)
			c.Error(errors.NewBadRequestError("Invalid metadata format").WithDetails(err.Error()))
			return
		}
		logger.Infof(ctx, "Received file metadata: %s", secutils.SanitizeForLog(fmt.Sprintf("%v", metadata)))
	}

	enableMultimodelForm := c.PostForm("enable_multimodel")
	var enableMultimodel *bool
	if enableMultimodelForm != "" {
		parseBool, err := strconv.ParseBool(enableMultimodelForm)
		if err != nil {
			logger.Error(ctx, "Failed to parse enable_multimodel", err)
			c.Error(errors.NewBadRequestError("Invalid enable_multimodel format").WithDetails(err.Error()))
			return
		}
		enableMultimodel = &parseBool
	}

	var processOverrides *types.KnowledgeProcessOverrides
	if raw := c.PostForm("process_config"); raw != "" {
		processOverrides = &types.KnowledgeProcessOverrides{}
		if err := json.Unmarshal([]byte(raw), processOverrides); err != nil {
			logger.Error(ctx, "Failed to parse process_config", err)
			c.Error(errors.NewBadRequestError("Invalid process_config format").WithDetails(err.Error()))
			return
		}
	}
	if enableMultimodel != nil && (processOverrides == nil || processOverrides.EnableMultimodel == nil) {
		if processOverrides == nil {
			processOverrides = &types.KnowledgeProcessOverrides{EnableMultimodel: enableMultimodel}
		} else {
			processOverrides.EnableMultimodel = enableMultimodel
		}
	}
	processOverrides = platformProcessOverrides(ctx, processOverrides)

	// Lấy danh sách ID thẻ (nếu có), phân cách bằng dấu phẩy, dùng để gắn nhiều thẻ cho tài liệu
	tagIDs := parseCommaSeparatedTagIDs(c.PostForm("tag_ids"))

	channel := c.PostForm("channel")

	// Create knowledge entry from the file
	knowledge, err := h.kgService.CreateKnowledgeFromFile(ctx, kbID, file, metadata, enableMultimodel, customFileName, tagIDs, channel, processOverrides)
	// Check for duplicate knowledge error
	if err != nil {
		if h.handleDuplicateKnowledgeError(c, err, knowledge, "file") {
			return
		}
		if appErr, ok := errors.IsAppError(err); ok {
			c.Error(appErr)
			return
		}
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}

	logger.Infof(
		ctx,
		"Knowledge created successfully, ID: %s, title: %s",
		secutils.SanitizeForLog(knowledge.ID),
		secutils.SanitizeForLog(knowledge.Title),
	)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    knowledge,
	})
}

// CreateKnowledgeFromURL godoc
// @Summary      Tạo tài liệu từ URL
// @Description  Lấy nội dung từ URL chỉ định và tạo mục tài liệu. Khi có file_name/file_type hoặc đường dẫn URL chứa phần mở rộng tệp đã biết thì tự chuyển sang chế độ tải tệp
// @Tags         Tài liệu
// @Accept       json
// @Produce      json
// @Param        id       path      string  true  "Knowledge BaseID"
// @Param        request  body      object{url=string,file_name=string,file_type=string,enable_multimodel=bool,title=string,tag_ids=[]string}  true  "Yêu cầu theo URL"
// @Success      201      {object}  map[string]interface{}  "Tài liệu đã tạo"
// @Failure      400      {object}  errors.AppError         "tham số yêu cầu không hợp lệ"
// @Failure      409      {object}  map[string]interface{}  "URL bị trùng"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases/{id}/knowledge/url [post]
func (h *KnowledgeHandler) CreateKnowledgeFromURL(c *gin.Context) {
	ctx := c.Request.Context()
	logger.Info(ctx, "Start creating knowledge from URL")

	// Validate management access to the knowledge base (owning tenant or an
	// explicit human SuperAdmin on platform-owned rows); public Viewers stay
	// denied by the permission check below.
	_, kbID, effectiveTenantID, permission, err := h.validateKnowledgeBaseWriteAccessWithKBID(c, c.Param("id"))
	if err != nil {
		c.Error(err)
		return
	}
	ctx = types.WithExecutionTenant(c.Request.Context(), effectiveTenantID)

	// Check write permission
	if permission != types.KBPermissionAdmin && permission != types.KBPermissionEditor {
		c.Error(errors.NewForbiddenError("No permission to create knowledge"))
		return
	}

	// Parse URL from request body
	var req struct {
		URL              string                           `json:"url" binding:"required"`
		FileName         string                           `json:"file_name"`
		FileType         string                           `json:"file_type"`
		EnableMultimodel *bool                            `json:"enable_multimodel"`
		Title            string                           `json:"title"`
		TagIDs           []string                         `json:"tag_ids"`
		Channel          string                           `json:"channel"`
		ProcessConfig    *types.KnowledgeProcessOverrides `json:"process_config"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to parse URL request", err)
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}

	logger.Infof(ctx, "Received URL request: %s, file_name: %s, file_type: %s",
		secutils.SanitizeForLog(req.URL),
		secutils.SanitizeForLog(req.FileName),
		secutils.SanitizeForLog(req.FileType),
	)

	// SSRF validation for user-supplied URL
	if err := secutils.ValidateURLForSSRF(req.URL); err != nil {
		logger.Warnf(ctx, "SSRF validation failed for knowledge URL: %v", err)
		c.Error(errors.NewBadRequestError(secutils.FormatSSRFError("URL", req.URL, err)))
		return
	}

	logger.Infof(ctx,
		"Creating knowledge from URL, knowledge base ID: %s, URL: %s",
		secutils.SanitizeForLog(kbID),
		secutils.SanitizeForLog(req.URL),
	)

	enableMultimodel := req.EnableMultimodel
	if !canManagePlatformConfig(ctx) {
		enableMultimodel = nil
	}

	// Create knowledge entry from the URL
	knowledge, err := h.kgService.CreateKnowledgeFromURL(
		ctx, kbID, req.URL, req.FileName, req.FileType, enableMultimodel, req.Title, req.TagIDs, req.Channel,
		platformProcessOverrides(ctx, req.ProcessConfig),
	)
	// Check for duplicate knowledge error
	if err != nil {
		if h.handleDuplicateKnowledgeError(c, err, knowledge, "url") {
			return
		}
		if appErr, ok := errors.IsAppError(err); ok {
			c.Error(appErr)
			return
		}
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}

	logger.Infof(
		ctx,
		"Knowledge created successfully from URL, ID: %s, title: %s",
		secutils.SanitizeForLog(knowledge.ID),
		secutils.SanitizeForLog(knowledge.Title),
	)
	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"data":    knowledge,
	})
}

// CreateManualKnowledge godoc
// @Summary      Tạo tài liệu thủ công
// @Description  Nhập thủ công nội dung tài liệu ở định dạng Markdown
// @Tags         Tài liệu
// @Accept       json
// @Produce      json
// @Param        id       path      string                       true  "Knowledge BaseID"
// @Param        request  body      types.ManualKnowledgePayload true  "Nội dung tài liệu thủ công"
// @Success      200      {object}  map[string]interface{}       "Tài liệu đã tạo"
// @Failure      400      {object}  errors.AppError              "tham số yêu cầu không hợp lệ"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases/{id}/knowledge/manual [post]
func (h *KnowledgeHandler) CreateManualKnowledge(c *gin.Context) {
	ctx := c.Request.Context()
	logger.Info(ctx, "Start creating manual knowledge")

	// Validate management access to the knowledge base (owning tenant or an
	// explicit human SuperAdmin on platform-owned rows); public Viewers stay
	// denied by the permission check below.
	_, kbID, effectiveTenantID, permission, err := h.validateKnowledgeBaseWriteAccessWithKBID(c, c.Param("id"))
	if err != nil {
		c.Error(err)
		return
	}
	ctx = types.WithExecutionTenant(c.Request.Context(), effectiveTenantID)

	// Check write permission
	if permission != types.KBPermissionAdmin && permission != types.KBPermissionEditor {
		c.Error(errors.NewForbiddenError("No permission to create knowledge"))
		return
	}

	var req types.ManualKnowledgePayload
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to parse manual knowledge request", err)
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}

	knowledge, err := h.kgService.CreateKnowledgeFromManual(ctx, kbID, &req, req.Channel)
	if err != nil {
		if appErr, ok := errors.IsAppError(err); ok {
			c.Error(appErr)
			return
		}
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"kb_id": kbID,
		})
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}

	logger.Infof(ctx, "Manual knowledge created successfully, knowledge ID: %s",
		secutils.SanitizeForLog(knowledge.ID))
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    knowledge,
	})
}

// GetKnowledge godoc
// @Summary      Lấy chi tiết tài liệu
// @Description  Lấy chi tiết mục tài liệu theo ID
// @Tags         Tài liệu
// @Accept       json
// @Produce      json
// @Param        id   path      string  true  "ID tài liệu"
// @Success      200  {object}  map[string]interface{}  "Chi tiết tài liệu"
// @Failure      400  {object}  errors.AppError         "tham số yêu cầu không hợp lệ"
// @Failure      404  {object}  errors.AppError         "tài liệu không tồn tại"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge/{id} [get]
func (h *KnowledgeHandler) GetKnowledge(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Start retrieving knowledge")

	id := secutils.SanitizeForLog(c.Param("id"))
	if id == "" {
		logger.Error(ctx, "Knowledge ID is empty")
		c.Error(errors.NewBadRequestError("Knowledge ID cannot be empty"))
		return
	}

	// Resolve knowledge and validate KB access (at least viewer)
	knowledge, effCtx, err := h.resolveKnowledgeAndValidateKBAccess(c, id, types.KBPermissionViewer)
	if err != nil {
		c.Error(err)
		return
	}

	// Re-fetch with tenant-scoped service so tags and other joined fields are populated.
	if knowledge, err = h.kgService.GetKnowledgeByID(effCtx, id); err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewNotFoundError("Knowledge not found"))
		return
	}

	logger.Infof(ctx, "Knowledge retrieved successfully, ID: %s, title: %s",
		secutils.SanitizeForLog(knowledge.ID), secutils.SanitizeForLog(knowledge.Title))
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    knowledge,
	})
}

// GetKnowledgeSpans godoc
// @Summary      Lấy cây Span của quá trình phân tích tài liệu (gồm các lần thử trước)
// @Description  Trả về trace tree của tài liệu trong pipeline phân tích (root → stage → subspan): trạng thái, thời gian, input/output, mã lỗi, langfuse_trace_id của từng đoạn. Hỗ trợ ?attempt=N để xem lần thử trước; không truyền thì trả về lần mới nhất. Frontend dùng để vẽ dòng thời gian + nút con đa phương thức/embedding + mở nhanh Langfuse.
// @Tags         Tài liệu
// @Accept       json
// @Produce      json
// @Param        id        path   string  true   "ID tài liệu"
// @Param        attempt   query  int     false  "Số lần thử; bỏ trống = mới nhất"
// @Success      200       {object}  map[string]interface{}
// @Router       /api/v1/knowledge/{id}/spans [get]
//
// Always returns the canonical 5-stage timeline; missing stage rows are
// synthesized as "pending" so the frontend timeline always renders five
// segments. Subspans (multimodal.image[i], generation.*) ride along under
// each stage as children when present.
func (h *KnowledgeHandler) GetKnowledgeSpans(c *gin.Context) {
	ctx := c.Request.Context()

	id := secutils.SanitizeForLog(c.Param("id"))
	if id == "" {
		c.Error(errors.NewBadRequestError("Knowledge ID cannot be empty"))
		return
	}

	knowledge, _, err := h.resolveKnowledgeAndValidateKBAccess(c, id, types.KBPermissionViewer)
	if err != nil {
		c.Error(err)
		return
	}

	// Pick attempt: explicit ?attempt=N wins; otherwise pull the
	// latest attempt from the spans table. Lite-mode / fresh installs
	// with zero rows fall through to attempt=0, in which case we
	// return a placeholder tree (5 pending stages, no root, no
	// children) so the UI still renders.
	requestedAttempt := 0
	if v := strings.TrimSpace(c.Query("attempt")); v != "" {
		if n, perr := strconv.Atoi(v); perr == nil && n > 0 {
			requestedAttempt = n
		}
	}

	rows := []types.KnowledgeProcessingSpan{}
	currentAttempt := 0
	latestAttempt := 0
	if h.spanRepo != nil {
		latest, lerr := h.spanRepo.LatestAttempt(ctx, knowledge.ID)
		if lerr != nil {
			logger.Warnf(ctx, "spans LatestAttempt failed for %s: %v", knowledge.ID, lerr)
		} else {
			latestAttempt = latest
		}
		if requestedAttempt > 0 {
			currentAttempt = requestedAttempt
		} else {
			currentAttempt = latestAttempt
		}
		if currentAttempt > 0 {
			rows, err = h.spanRepo.ListByAttempt(ctx, knowledge.ID, currentAttempt)
			if err != nil {
				logger.Warnf(ctx, "spans ListByAttempt failed kid=%s attempt=%d: %v",
					knowledge.ID, currentAttempt, err)
				rows = nil
			}
		}
	}

	// Build tree: index by SpanID, then attach to parents. Stages
	// missing from the DB are synthesized as "pending" placeholders
	// under a synthetic (or real, if present) root so the timeline
	// always renders five segments. parse_status threads through so
	// pre-tracker historical knowledge (no rows but parse_status is
	// already terminal) renders as done/failed instead of pending —
	// otherwise legacy completed documents would forever look like
	// they're still waiting in the queue.
	tree, currentStageName, lastErr := buildSpanTree(knowledge.ID, currentAttempt, rows, knowledge.ParseStatus)

	resp := gin.H{
		"knowledge_id":    knowledge.ID,
		"attempt":         currentAttempt,
		"latest_attempt":  latestAttempt,
		"parse_status":    knowledge.ParseStatus,
		"current_attempt": currentAttempt,
		"current_stage":   currentStageName,
		"trace":           tree,
	}
	if lastError := knowledgeSpansLastError(
		currentAttempt,
		latestAttempt,
		knowledge.ParseStatus,
		knowledge.ErrorMessage,
		knowledge.UpdatedAt,
		lastErr,
	); lastError != nil {
		resp["last_error"] = lastError
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    resp,
	})
}

// knowledgeSpansLastError builds the last_error payload for GetKnowledgeSpans.
// Span failures win when present; otherwise a failed knowledge row without a
// matching span error (recovery / dead-letter paths) surfaces ErrorMessage.
func knowledgeSpansLastError(
	currentAttempt, latestAttempt int,
	parseStatus, knowledgeErrorMessage string,
	knowledgeUpdatedAt time.Time,
	spanFailure *types.KnowledgeProcessingSpan,
) gin.H {
	if spanFailure != nil {
		return gin.H{
			"stage":         spanFailure.Name,
			"code":          spanFailure.ErrorCode,
			"message":       spanFailure.ErrorMessage,
			"name":          spanFailure.Name,
			"error_code":    spanFailure.ErrorCode,
			"error_message": spanFailure.ErrorMessage,
			"finished_at":   spanFailure.FinishedAt,
		}
	}
	if currentAttempt != latestAttempt || parseStatus != types.ParseStatusFailed || knowledgeErrorMessage == "" {
		return nil
	}
	errorCode := "UNKNOWN"
	if strings.EqualFold(strings.TrimSpace(knowledgeErrorMessage),
		"Task interrupted due to application restart") {
		errorCode = "SERVER_RESTART"
	}
	return gin.H{
		"stage":         "knowledge_processing",
		"code":          errorCode,
		"message":       knowledgeErrorMessage,
		"name":          "knowledge_processing",
		"error_code":    errorCode,
		"error_message": knowledgeErrorMessage,
		"finished_at":   knowledgeUpdatedAt,
	}
}

// buildSpanTree assembles a flat list of span rows into a parent-child
// tree rooted at the (knowledge, attempt)'s root span. Missing canonical
// stages are filled in with pending placeholders so the UI always renders
// the five timeline segments. Returns the root, the current_stage name
// (the running stage if any), and the most recent failed span if one
// exists.
//
// parseStatus is the knowledge.parse_status string. When the spans table
// has zero rows for this attempt (legacy data parsed before tracking, or
// a fresh knowledge before the pipeline starts), the placeholder status
// is inferred from parseStatus: completed → done, failed → failed,
// otherwise pending. Without this, every historical knowledge would
// render as "all 5 stages pending" forever despite having actually
// completed parsing.
func buildSpanTree(knowledgeID string, attempt int, rows []types.KnowledgeProcessingSpan, parseStatus string) (
	root *types.SpanTreeNode, currentStage string, lastFailure *types.KnowledgeProcessingSpan,
) {
	now := time.Now()
	// Build node lookup, identify root.
	nodes := make(map[string]*types.SpanTreeNode, len(rows))
	var rootRow *types.KnowledgeProcessingSpan
	stageRowByName := map[string]*types.KnowledgeProcessingSpan{}
	for i := range rows {
		r := rows[i]
		nodes[r.SpanID] = &types.SpanTreeNode{KnowledgeProcessingSpan: r}
		if r.Kind == types.SpanKindRoot && rootRow == nil {
			cp := r
			rootRow = &cp
		}
		if r.Kind == types.SpanKindStage {
			cp := r
			stageRowByName[r.Name] = &cp
		}
		if r.Status == types.SpanStatusRunning && r.Kind == types.SpanKindStage && currentStage == "" {
			currentStage = r.Name
		}
		if r.Status == types.SpanStatusFailed {
			cp := r
			lastFailure = &cp
		}
	}

	// Pick the synthesized stage status from parse_status. Without this,
	// historical knowledge that completed before span tracking was wired
	// would render as "5 pending stages" forever — the rows simply
	// weren't recorded, but parse_status correctly reads "completed".
	// The synthesized stages don't carry duration/timing data; they
	// just communicate the inferred terminal state.
	syntheticStatus := types.SpanStatusPending
	switch parseStatus {
	case types.ParseStatusCompleted:
		syntheticStatus = types.SpanStatusDone
	case types.ParseStatusFailed:
		syntheticStatus = types.SpanStatusFailed
	}

	// Synthesize root if no rows came back so the API contract stays
	// stable (frontend always expects a `trace` object).
	if rootRow == nil {
		root = &types.SpanTreeNode{KnowledgeProcessingSpan: types.KnowledgeProcessingSpan{
			KnowledgeID: knowledgeID,
			Attempt:     attempt,
			SpanID:      "",
			Name:        "knowledge_processing",
			Kind:        types.SpanKindRoot,
			Status:      syntheticStatus,
			CreatedAt:   now,
			UpdatedAt:   now,
		}}
	} else {
		root = nodes[rootRow.SpanID]
	}

	// Link real children to their parents. Walk `rows` (not the
	// `nodes` map!) so the append order matches the repo's stable
	// `ORDER BY id ASC`. Iterating the map directly would give
	// callers a different child ordering on every request — Go map
	// iteration is intentionally randomised — and the UI would
	// flicker subspans into a different order on each refresh.
	for i := range rows {
		r := &rows[i]
		n := nodes[r.SpanID]
		if n == nil || n == root {
			continue
		}
		if r.ParentSpanID == "" {
			// Real top-level row with no parent and not the root
			// itself — attach to root so it doesn't dangle.
			root.Children = append(root.Children, n)
			continue
		}
		parent, ok := nodes[r.ParentSpanID]
		if !ok {
			// Unknown parent (orphan); attach to root.
			root.Children = append(root.Children, n)
			continue
		}
		parent.Children = append(parent.Children, n)
	}

	// Synthesize missing stage rows as children of root so the timeline
	// always shows 5 segments. Status mirrors the synthesized root —
	// pending while the pipeline is still running, done/failed for
	// historical knowledge whose terminal state we know but whose
	// per-stage timing was never recorded. Appended in AllStages order
	// so the canonical stage layout is deterministic regardless of
	// which rows are missing.
	for _, name := range types.AllStages {
		if _, ok := stageRowByName[name]; ok {
			continue
		}
		placeholder := types.KnowledgeProcessingSpan{
			KnowledgeID: knowledgeID,
			Attempt:     attempt,
			Name:        name,
			Kind:        types.SpanKindStage,
			Status:      syntheticStatus,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		root.Children = append(root.Children, &types.SpanTreeNode{KnowledgeProcessingSpan: placeholder})
	}

	return root, currentStage, lastFailure
}

// ListKnowledge godoc
// @Summary      Lấy danh sách tài liệu
// @Description  Lấy danh sách tài liệu trong kho tri thức, hỗ trợ phân trang và lọc
// @Tags         Tài liệu
// @Accept       json
// @Produce      json
// @Param        id         path      string  true   "Knowledge BaseID"
// @Param        page       query     int     false  "Số trang"
// @Param        page_size  query     int     false  "Số mục mỗi trang"
// @Param        tag_ids       query     string  false  "Lọc theo ID thẻ, phân cách bằng dấu phẩy (ngữ nghĩa OR)"
// @Param        keyword       query     string  false  "Tìm theo từ khoá"
// @Param        file_type     query     string  false  "Lọc theo loại tệp"
// @Param        parse_status  query     string  false  "Lọc theo trạng thái phân tích (pending/processing/completed/failed)"
// @Param        source        query     string  false  "Lọc theo nguồn/kênh (web/api/feishu/notion/yuque/wechat/..., hoặc manual/url lọc theo type)"
// @Param        start_time    query     string  false  "Mốc bắt đầu thời gian cập nhật, định dạng RFC3339"
// @Param        end_time      query     string  false  "Mốc kết thúc thời gian cập nhật, định dạng RFC3339"
// @Param        folder_path      query     string  false  "Lọc theo đường dẫn thư mục, chuỗi rỗng là thư mục gốc của kho tri thức; không truyền thì không lọc theo thư mục"
// @Param        folder_recursive query     bool    false  "Nếu true thì trả về cả tài liệu trong thư mục con"
// @Success      200        {object}  map[string]interface{}  "Danh sách tài liệu"
// @Failure      400        {object}  errors.AppError         "tham số yêu cầu không hợp lệ"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases/{id}/knowledge [get]
func (h *KnowledgeHandler) ListKnowledge(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Start retrieving knowledge list")

	// Validate access to the knowledge base (read access - any permission level)
	_, kbID, effectiveTenantID, _, err := h.validateKnowledgeBaseAccess(c)
	if err != nil {
		c.Error(err)
		return
	}

	// Update context with effective tenant ID for shared KB access
	ctx = types.WithExecutionTenant(c.Request.Context(), effectiveTenantID)

	// Parse pagination parameters from query string
	var pagination types.Pagination
	if err := c.ShouldBindQuery(&pagination); err != nil {
		logger.Error(ctx, "Failed to parse pagination parameters", err)
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}

	filter := types.KnowledgeListFilter{
		TagIDs:      parseCommaSeparatedTagIDs(c.Query("tag_ids")),
		Keyword:     c.Query("keyword"),
		FileType:    c.Query("file_type"),
		ParseStatus: c.Query("parse_status"),
		Source:      c.Query("source"),
	}
	if raw := c.Query("start_time"); raw != "" {
		t, err := parseFilterTime(raw)
		if err != nil {
			c.Error(errors.NewBadRequestError("invalid start_time: " + err.Error()))
			return
		}
		filter.UpdatedFrom = t
	}
	if raw := c.Query("end_time"); raw != "" {
		t, err := parseFilterTime(raw)
		if err != nil {
			c.Error(errors.NewBadRequestError("invalid end_time: " + err.Error()))
			return
		}
		filter.UpdatedTo = t
	}
	// The folder dimension is opt-in by parameter *presence*: an empty
	// folder_path is meaningful (the knowledge base root), so it cannot be
	// distinguished from "no folder filter" by value alone.
	if raw, ok := c.GetQuery("folder_path"); ok {
		filter.FolderPath = types.NormalizeKnowledgeFolderPath(raw)
		filter.FolderScope = types.FolderScopeExact
		if recursive, err := strconv.ParseBool(c.DefaultQuery("folder_recursive", "false")); err == nil && recursive {
			filter.FolderScope = types.FolderScopeSubtree
		}
	}

	logger.Infof(
		ctx,
		"Retrieving knowledge list under knowledge base, kb_id=%s tag_ids=%s keyword=%s file_type=%s parse_status=%s source=%s start_time=%s end_time=%s folder_path=%s folder_scope=%s page=%d page_size=%d effectiveTenantID=%d",
		secutils.SanitizeForLog(kbID),
		secutils.SanitizeForLog(strings.Join(filter.TagIDs, ",")),
		secutils.SanitizeForLog(filter.Keyword),
		secutils.SanitizeForLog(filter.FileType),
		secutils.SanitizeForLog(filter.ParseStatus),
		secutils.SanitizeForLog(filter.Source),
		secutils.SanitizeForLog(c.Query("start_time")),
		secutils.SanitizeForLog(c.Query("end_time")),
		secutils.SanitizeForLog(filter.FolderPath),
		string(filter.FolderScope),
		pagination.Page,
		pagination.PageSize,
		effectiveTenantID,
	)

	// Retrieve paginated knowledge entries
	result, err := h.kgService.ListPagedKnowledgeByKnowledgeBaseID(ctx, kbID, &pagination, filter)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}

	logger.Infof(
		ctx,
		"Knowledge list retrieved successfully, knowledge base ID: %s, total: %d",
		secutils.SanitizeForLog(kbID),
		result.Total,
	)
	c.JSON(http.StatusOK, gin.H{
		"success":   true,
		"data":      result.Data,
		"total":     result.Total,
		"page":      result.Page,
		"page_size": result.PageSize,
	})
}

// ListKnowledgeFolders godoc
// @Summary      Lấy cây thư mục của kho tri thức
// @Description  Trả về cây thư mục hình thành từ việc tải thư mục lên kho tri thức, gồm số tài liệu trực tiếp và tổng số kể cả thư mục con của mỗi thư mục
// @Tags         Tài liệu
// @Accept       json
// @Produce      json
// @Param        id   path      string  true  "Knowledge BaseID"
// @Success      200  {object}  map[string]interface{}  "Cây thư mục"
// @Failure      400  {object}  errors.AppError         "tham số yêu cầu không hợp lệ"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases/{id}/knowledge/folders [get]
func (h *KnowledgeHandler) ListKnowledgeFolders(c *gin.Context) {
	ctx := c.Request.Context()

	// Read access mirrors ListKnowledge so the sidebar tree is available to
	// every viewer of a shared knowledge base.
	_, kbID, effectiveTenantID, _, err := h.validateKnowledgeBaseAccess(c)
	if err != nil {
		c.Error(err)
		return
	}
	ctx = types.WithExecutionTenant(c.Request.Context(), effectiveTenantID)

	tree, err := h.kgService.ListKnowledgeFolderTree(ctx, kbID)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}

	logger.Infof(ctx, "Knowledge folder tree retrieved, kb_id=%s folders=%d",
		secutils.SanitizeForLog(kbID), len(tree.Folders))
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    tree,
	})
}

// MoveKnowledgeToFolderRequest is the body schema for POST /knowledge/folder.
type MoveKnowledgeToFolderRequest struct {
	KBID string   `json:"kb_id" binding:"required"`
	IDs  []string `json:"knowledge_ids" binding:"required"`
	// FolderPath is the destination folder; the empty string is the knowledge
	// base top level. It is deliberately not `binding:"required"` so documents
	// can be moved back out of every folder.
	FolderPath string `json:"folder_path"`
}

// MoveKnowledgeToFolder godoc
// @Summary      Chuyển tài liệu vào thư mục
// @Description  Đổi thư mục của nhiều mục tài liệu cùng lúc. Thư mục được suy ra từ đường dẫn nên đường dẫn đích chưa có sẽ được tạo tự động; đường dẫn rỗng là cấp cao nhất của kho tri thức. Chỉ đổi phân loại, không phân tích lại tài liệu
// @Tags         Tài liệu
// @Accept       json
// @Produce      json
// @Param        request  body      MoveKnowledgeToFolderRequest  true  "Yêu cầu di chuyển"
// @Success      200      {object}  map[string]interface{}        "Di chuyển thành công"
// @Failure      400      {object}  errors.AppError               "tham số yêu cầu không hợp lệ"
// @Failure      403      {object}  errors.AppError               "không có quyền"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge/folder [post]
func (h *KnowledgeHandler) MoveKnowledgeToFolder(c *gin.Context) {
	ctx := c.Request.Context()

	var req MoveKnowledgeToFolderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewBadRequestError("Invalid request parameters: " + err.Error()))
		return
	}

	ids := dedupeKnowledgeIDs(req.IDs)
	if len(ids) == 0 {
		c.Error(errors.NewBadRequestError("knowledge_ids cannot be empty"))
		return
	}
	const maxBatch = 200
	if len(ids) > maxBatch {
		c.Error(errors.NewBadRequestError(fmt.Sprintf("too many ids (max %d per batch)", maxBatch)))
		return
	}

	kbID, effectiveTenantID, err := h.requireKnowledgeWriteAccess(c, req.KBID)
	if err != nil {
		c.Error(err)
		return
	}
	ctx = types.WithExecutionTenant(c.Request.Context(), effectiveTenantID)

	// Guard against cross-KB moves: the service layer scopes by tenant, so the
	// handler must confirm every entry belongs to the requested knowledge base.
	if err := h.requireKnowledgeInKB(ctx, effectiveTenantID, kbID, ids); err != nil {
		c.Error(err)
		return
	}

	affected, err := h.kgService.MoveKnowledgeToFolder(ctx, kbID, ids, req.FolderPath)
	if err != nil {
		if appErr, ok := errors.IsAppError(err); ok {
			c.Error(appErr)
			return
		}
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"moved_count": affected,
			"folder_path": types.NormalizeKnowledgeFolderPath(req.FolderPath),
		},
	})
}

// RenameKnowledgeFolderRequest is the body schema for
// PUT /knowledge-bases/:id/knowledge/folders.
type RenameKnowledgeFolderRequest struct {
	From string `json:"from" binding:"required"`
	To   string `json:"to"   binding:"required"`
}

// RenameKnowledgeFolder godoc
// @Summary      Đổi tên hoặc di chuyển thư mục
// @Description  Chuyển một thư mục cùng toàn bộ thư mục con sang đường dẫn mới. Nếu đường dẫn đích đã tồn tại thì hai thư mục được gộp; không thể chuyển vào thư mục con của chính nó
// @Tags         Tài liệu
// @Accept       json
// @Produce      json
// @Param        id       path      string                        true  "Knowledge BaseID"
// @Param        request  body      RenameKnowledgeFolderRequest  true  "Yêu cầu đổi tên"
// @Success      200      {object}  map[string]interface{}        "Đổi tên thành công"
// @Failure      400      {object}  errors.AppError               "tham số yêu cầu không hợp lệ"
// @Failure      403      {object}  errors.AppError               "không có quyền"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases/{id}/knowledge/folders [put]
func (h *KnowledgeHandler) RenameKnowledgeFolder(c *gin.Context) {
	ctx := c.Request.Context()

	var req RenameKnowledgeFolderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewBadRequestError("Invalid request parameters: " + err.Error()))
		return
	}

	_, kbID, effectiveTenantID, permission, err := h.validateKnowledgeBaseWriteAccessWithKBID(c, c.Param("id"))
	if err != nil {
		c.Error(err)
		return
	}
	if permission != types.KBPermissionAdmin && permission != types.KBPermissionEditor {
		c.Error(errors.NewForbiddenError("No permission to modify knowledge"))
		return
	}
	if err := h.requireKBOwnershipOrAdmin(c, kbID); err != nil {
		c.Error(err)
		return
	}
	ctx = types.WithExecutionTenant(c.Request.Context(), effectiveTenantID)

	affected, err := h.kgService.RenameKnowledgeFolder(ctx, kbID, req.From, req.To)
	if err != nil {
		if appErr, ok := errors.IsAppError(err); ok {
			c.Error(appErr)
			return
		}
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"moved_count": affected,
			"folder_path": types.NormalizeKnowledgeFolderPath(req.To),
		},
	})
}

// dedupeKnowledgeIDs trims, drops empty and de-duplicates a batch of IDs while
// preserving the caller's order.
func dedupeKnowledgeIDs(raw []string) []string {
	seen := make(map[string]struct{}, len(raw))
	ids := make([]string, 0, len(raw))
	for _, item := range raw {
		id := strings.TrimSpace(item)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

// requireKnowledgeWriteAccess resolves a knowledge base from a request body and
// enforces the editor-or-admin plus ownership gate shared by the batch routes.
func (h *KnowledgeHandler) requireKnowledgeWriteAccess(
	c *gin.Context,
	requestedKBID string,
) (string, uint64, error) {
	_, kbID, effectiveTenantID, permission, err := h.validateKnowledgeBaseWriteAccessWithKBID(c, requestedKBID)
	if err != nil {
		return "", 0, err
	}
	if permission != types.KBPermissionAdmin && permission != types.KBPermissionEditor {
		return "", 0, errors.NewForbiddenError("No permission to modify knowledge")
	}
	if err := h.requireKBOwnershipOrAdmin(c, kbID); err != nil {
		return "", 0, err
	}
	return kbID, effectiveTenantID, nil
}

// requireKnowledgeInKB verifies every ID exists and belongs to the given
// knowledge base, so a batch operation cannot reach across knowledge bases.
func (h *KnowledgeHandler) requireKnowledgeInKB(
	ctx context.Context,
	tenantID uint64,
	kbID string,
	ids []string,
) error {
	knowledgeList, err := h.kgService.GetKnowledgeBatch(ctx, tenantID, ids)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		return errors.NewInternalServerError(err.Error())
	}
	if len(knowledgeList) != len(ids) {
		return errors.NewBadRequestError("One or more knowledge entries not found")
	}
	for _, k := range knowledgeList {
		if k.KnowledgeBaseID != kbID {
			return errors.NewBadRequestError(
				fmt.Sprintf("Knowledge %s does not belong to knowledge base %s",
					secutils.SanitizeForLog(k.ID), secutils.SanitizeForLog(kbID)))
		}
	}
	return nil
}

// DeleteKnowledge godoc
// @Summary      Xoá tài liệu
// @Description  Xoá bất đồng bộ mục tài liệu theo ID. Yêu cầu được đưa vào cùng hàng đợi bất đồng bộ (asynq) với xoá hàng loạt;
// @Description  API trả 200 chỉ có nghĩa là tác vụ đã được gửi (data.task_id là ID tác vụ), việc xoá thật do worker nền thực hiện.
// @Tags         Tài liệu
// @Accept       json
// @Produce      json
// @Param        id   path      string  true  "ID tài liệu"
// @Success      200  {object}  map[string]interface{}  "Tác vụ đã được gửi, trả về task_id"
// @Failure      400  {object}  errors.AppError         "tham số yêu cầu không hợp lệ"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge/{id} [delete]
func (h *KnowledgeHandler) DeleteKnowledge(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Start deleting knowledge")

	id := secutils.SanitizeForLog(c.Param("id"))
	if id == "" {
		logger.Error(ctx, "Knowledge ID is empty")
		c.Error(errors.NewBadRequestError("Knowledge ID cannot be empty"))
		return
	}

	knowledge, effCtx, err := h.resolveKnowledgeAndValidateKBAccess(c, id, types.KBPermissionEditor)
	if err != nil {
		c.Error(err)
		return
	}

	if err := access.RejectMovingKnowledge(knowledge); err != nil {
		_ = c.Error(err)
		return
	}

	// Reuse the batch async pipeline so single-item delete shares the same
	// hardening (asynq retries, business-aware queue routing, marking-as-deleting
	// inside the worker) as BatchDeleteKnowledge / ClearKnowledgeBaseContents.
	effectiveTenantID, _ := effCtx.Value(types.TenantIDContextKey).(uint64)
	if effectiveTenantID == 0 {
		logger.Error(ctx, "Effective tenant ID missing after access validation")
		c.Error(errors.NewInternalServerError("organization context unavailable"))
		return
	}

	logger.Infof(ctx, "Enqueuing knowledge delete, ID: %s", secutils.SanitizeForLog(id))
	taskID, err := h.enqueueKnowledgeListDelete(effCtx, effectiveTenantID, knowledge.KnowledgeBaseID, []string{id})
	if err != nil {
		logger.Errorf(ctx, "Failed to enqueue knowledge delete task: %v", err)
		c.Error(errors.NewInternalServerError("Failed to enqueue delete task"))
		return
	}

	logger.Infof(ctx, "Knowledge delete task enqueued: %s, knowledge_id: %s", taskID, secutils.SanitizeForLog(id))
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Delete task submitted",
		"data": gin.H{
			"task_id": taskID,
		},
	})
}

// BatchDeleteKnowledgeRequest is the body schema for POST /knowledge/batch-delete.
type BatchDeleteKnowledgeRequest struct {
	KBID string   `json:"kb_id" binding:"required"`
	IDs  []string `json:"ids"  binding:"required"`
}

// BatchDeleteKnowledge godoc
// @Summary      Xoá nhiều tài liệu
// @Description  Xoá nhiều mục tài liệu trong một kho tri thức theo danh sách ID
// @Tags         Tài liệu
// @Accept       json
// @Produce      json
// @Param        request  body      BatchDeleteKnowledgeRequest  true  "Yêu cầu xoá hàng loạt"
// @Success      200      {object}  map[string]interface{}       "Xoá thành công"
// @Failure      400      {object}  errors.AppError              "tham số yêu cầu không hợp lệ"
// @Failure      403      {object}  errors.AppError              "không có quyền"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge/batch-delete [post]
func (h *KnowledgeHandler) BatchDeleteKnowledge(c *gin.Context) {
	ctx := c.Request.Context()

	var req BatchDeleteKnowledgeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewBadRequestError("Invalid request parameters: " + err.Error()))
		return
	}

	ids := dedupeKnowledgeIDs(req.IDs)
	if len(ids) == 0 {
		c.Error(errors.NewBadRequestError("ids cannot be empty"))
		return
	}
	const maxBatch = 200
	if len(ids) > maxBatch {
		c.Error(errors.NewBadRequestError(fmt.Sprintf("too many ids (max %d per batch)", maxBatch)))
		return
	}

	// Validate KB access (editor or admin) using the kb_id from body.
	_, kbID, effectiveTenantID, permission, err := h.validateKnowledgeBaseWriteAccessWithKBID(c, req.KBID)
	if err != nil {
		c.Error(err)
		return
	}
	if permission != types.KBPermissionAdmin && permission != types.KBPermissionEditor {
		c.Error(errors.NewForbiddenError("No permission to delete knowledge"))
		return
	}
	// A Member who is not the KB's creator may still delete documents they
	// uploaded themselves; every id in the batch must then be theirs.
	uploaderOnly := false
	if err := h.requireKBOwnershipOrAdmin(c, kbID); err != nil {
		if appErr, ok := errors.IsAppError(err); !ok || appErr.HTTPCode != http.StatusForbidden || !isHumanMember(ctx) {
			c.Error(err)
			return
		}
		uploaderOnly = true
	}
	ctx = types.WithExecutionTenant(c.Request.Context(), effectiveTenantID)

	// Single batch fetch to validate that every id exists and belongs to the
	// requested KB before admitting a task with that exact KB scope.
	knowledgeList, err := h.kgService.GetKnowledgeBatch(ctx, effectiveTenantID, ids)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	if len(knowledgeList) != len(ids) {
		c.Error(errors.NewBadRequestError("One or more knowledge entries not found"))
		return
	}
	for _, k := range knowledgeList {
		if err := access.RejectMovingKnowledge(k); err != nil {
			_ = c.Error(err)
			return
		}
		if k.KnowledgeBaseID != kbID {
			c.Error(errors.NewBadRequestError(
				fmt.Sprintf("Knowledge %s does not belong to knowledge base %s",
					secutils.SanitizeForLog(k.ID), secutils.SanitizeForLog(kbID))))
			return
		}
		if uploaderOnly && (k.CreatedBy == "" || k.CreatedBy != types.CallerFromContext(ctx).UserID) {
			c.Error(errors.NewForbiddenError("Members may delete only the documents they uploaded"))
			return
		}
	}

	taskID, err := h.enqueueKnowledgeListDelete(ctx, effectiveTenantID, kbID, ids)
	if err != nil {
		logger.Errorf(ctx, "Failed to enqueue batch knowledge delete task: %v", err)
		c.Error(errors.NewInternalServerError("Failed to enqueue batch delete task"))
		return
	}

	logger.Infof(ctx, "Batch knowledge delete task enqueued: %s, kb_id: %s, count: %d",
		taskID, secutils.SanitizeForLog(kbID), len(ids))

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Batch delete task submitted",
		"data": gin.H{
			"task_id":       taskID,
			"deleted_count": len(ids),
		},
	})
}

// ClearKnowledgeBaseContents godoc
// @Summary      Xoá toàn bộ nội dung kho tri thức
// @Description  Xoá mọi mục tài liệu trong kho tri thức (tác vụ bất đồng bộ). Kho tri thức vẫn được giữ, chỉ xoá nội dung bên trong
// @Tags         Tài liệu
// @Accept       json
// @Produce      json
// @Param        id   path      string  true  "Knowledge BaseID"
// @Success      200  {object}  map[string]interface{}  "Tác vụ xoá toàn bộ đã được gửi"
// @Failure      400  {object}  errors.AppError         "tham số yêu cầu không hợp lệ"
// @Failure      403  {object}  errors.AppError         "không có quyền"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases/{id}/knowledge [delete]
func (h *KnowledgeHandler) ClearKnowledgeBaseContents(c *gin.Context) {
	ctx := c.Request.Context()
	logger.Info(ctx, "Start clearing knowledge base contents")

	kb, kbID, effectiveTenantID, permission, err := h.validateKnowledgeBaseWriteAccessWithKBID(c, c.Param("id"))
	if err != nil {
		c.Error(err)
		return
	}

	// Only owner (admin with matching tenant) can clear knowledge base contents
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	if kb.TenantID != tenantID || permission != types.KBPermissionAdmin {
		c.Error(errors.NewForbiddenError("Only knowledge base owner can clear contents"))
		return
	}

	ctx = types.WithExecutionTenant(c.Request.Context(), effectiveTenantID)

	knowledgeList, err := h.kgService.ListKnowledgeByKnowledgeBaseID(ctx, kbID)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError("Failed to list knowledge entries").WithDetails(err.Error()))
		return
	}

	if len(knowledgeList) == 0 {
		logger.Infof(ctx, "Knowledge base %s is already empty", secutils.SanitizeForLog(kbID))
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "Knowledge base is already empty",
			"data":    gin.H{"deleted_count": 0},
		})
		return
	}

	knowledgeIDs := make([]string, 0, len(knowledgeList))
	for _, knowledge := range knowledgeList {
		if err := access.RejectMovingKnowledge(knowledge); err != nil {
			_ = c.Error(err)
			return
		}
		knowledgeIDs = append(knowledgeIDs, knowledge.ID)
	}

	taskID, err := h.enqueueKnowledgeListDelete(ctx, effectiveTenantID, kbID, knowledgeIDs)
	if err != nil {
		logger.Errorf(ctx, "Failed to enqueue knowledge list delete task: %v", err)
		c.Error(errors.NewInternalServerError("Failed to enqueue cleanup task"))
		return
	}

	logger.Infof(ctx, "Knowledge base contents clear task enqueued: %s, kb_id: %s, count: %d",
		taskID, secutils.SanitizeForLog(kbID), len(knowledgeIDs))

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Knowledge base contents clear task submitted",
		"data":    gin.H{"deleted_count": len(knowledgeIDs)},
	})
}

// DownloadKnowledgeFile godoc
// @Summary      Tải tệp tài liệu
// @Description  Tải tệp gốc gắn với mục tài liệu
// @Tags         Tài liệu
// @Accept       json
// @Produce      application/octet-stream
// @Param        id   path      string  true  "ID tài liệu"
// @Success      200  {file}    file    "Nội dung tệp"
// @Failure      400  {object}  errors.AppError  "tham số yêu cầu không hợp lệ"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge/{id}/download [get]
func (h *KnowledgeHandler) DownloadKnowledgeFile(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Start downloading knowledge file")

	id := secutils.SanitizeForLog(c.Param("id"))
	if id == "" {
		logger.Error(ctx, "Knowledge ID is empty")
		c.Error(errors.NewBadRequestError("Knowledge ID cannot be empty"))
		return
	}

	// Original bytes leave through the dedicated download decision: public
	// Viewers may fetch them, invitation Viewers stay denied, and preview
	// keeps its Viewer boundary in PreviewKnowledgeFile below.
	_, effCtx, err := h.resolveKnowledgeAndValidateKBDownload(c, id)
	if err != nil {
		c.Error(err)
		return
	}
	logger.Infof(ctx, "Retrieving knowledge file, ID: %s", secutils.SanitizeForLog(id))

	file, filename, err := h.kgService.GetKnowledgeFile(effCtx, id)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError("Failed to retrieve file").WithDetails(err.Error()))
		return
	}
	c.Header("Content-Description", "File Transfer")
	c.Header("Content-Transfer-Encoding", "binary")
	c.Header("Expires", "0")
	if err := filetransport.Serve(c.Writer, c.Request, file, filetransport.Options{
		Filename: filename, Download: true, ContentType: "application/octet-stream", CacheControl: "private, no-store",
	}); err != nil {
		logger.Errorf(ctx, "Failed to send file: %v", err)
	}
}

// PreviewKnowledgeFile godoc
// @Summary      Xem trước tệp tài liệu
// @Description  Trả về tệp gốc gắn với mục tài liệu, Content-Type đặt theo loại tệp, dùng để xem trước nhúng trong trình duyệt
// @Tags         Tài liệu
// @Accept       json
// @Produce      application/pdf,image/jpeg,image/png,text/plain
// @Param        id   path      string  true  "ID tài liệu"
// @Success      200  {file}    file    "Nội dung tệp"
// @Failure      400  {object}  errors.AppError  "tham số yêu cầu không hợp lệ"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge/{id}/preview [get]
func (h *KnowledgeHandler) PreviewKnowledgeFile(c *gin.Context) {
	ctx := c.Request.Context()

	id := secutils.SanitizeForLog(c.Param("id"))
	if id == "" {
		c.Error(errors.NewBadRequestError("Knowledge ID cannot be empty"))
		return
	}

	_, effCtx, err := h.resolveKnowledgeAndValidateKBAccess(c, id, types.KBPermissionViewer)
	if err != nil {
		c.Error(err)
		return
	}

	file, filename, err := h.kgService.GetKnowledgeFile(effCtx, id)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError("Failed to retrieve file").WithDetails(err.Error()))
		return
	}
	if err := filetransport.Serve(c.Writer, c.Request, file, filetransport.Options{
		Filename: filename, CacheControl: "private, no-store",
	}); err != nil {
		logger.Errorf(ctx, "Failed to stream preview: %v", err)
	}
}

// GetKnowledgeBatchRequest defines parameters for batch knowledge retrieval
type GetKnowledgeBatchRequest struct {
	IDs  []string `form:"ids" binding:"required"` // List of knowledge IDs
	KBID string   `form:"kb_id"`                  // Optional: scope to this KB (validates access and uses effective tenant for shared KB)

}

// GetKnowledgeBatch godoc
// @Summary      Lấy nhiều tài liệu
// @Description  Lấy nhiều mục tài liệu theo danh sách ID. kb_id tuỳ chọn: khi truyền sẽ kiểm tra quyền theo kho tri thức đó và dùng để xác định tổ chức của kho tri thức chia sẻ; agent_id tuỳ chọn: truyền khi dùng trợ lý chia sẻ, backend truy vấn theo tổ chức của trợ lý (dùng để khôi phục tệp trong kho tri thức chia sẻ sau khi tải lại trang)
// @Tags         Tài liệu
// @Accept       json
// @Produce      json
// @Param        ids       query     []string  true   "Danh sách ID tài liệu"
// @Param        kb_id     query     string   false  "Tuỳ chọn, ID kho tri thức (xác định phạm vi khi dùng kho tri thức chia sẻ)"
// @Param        agent_id  query     string   false  "Tuỳ chọn, ID trợ lý chia sẻ (lấy chi tiết tệp theo tổ chức của trợ lý)"
// @Success      200       {object}  map[string]interface{}  "Danh sách tài liệu"
// @Failure      400       {object}  errors.AppError        "tham số yêu cầu không hợp lệ"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge/batch [get]
func (h *KnowledgeHandler) GetKnowledgeBatch(c *gin.Context) {
	ctx := c.Request.Context()

	effectiveTenantID := middleware.KBAccessRequest(c).Caller.TenantID
	if effectiveTenantID == 0 {
		c.Error(errors.NewUnauthorizedError("Unauthorized"))
		return
	}

	var req GetKnowledgeBatchRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		logger.Error(ctx, "Failed to parse request parameters", err)
		c.Error(errors.NewBadRequestError("Invalid request parameters").WithDetails(err.Error()))
		return
	}
	var knowledges []*types.Knowledge
	var err error

	// scopeKBID tracks the single KB the results must belong to (set by explicit kb_id).
	var scopeKBID string

	// Optional kb_id: validate KB access and use effective tenant for shared KB
	if kbID := secutils.SanitizeForLog(req.KBID); kbID != "" {
		_, _, effID, _, accessErr := h.validateKnowledgeBaseAccessWithKBID(c, kbID)
		if accessErr != nil {
			_ = c.Error(accessErr)
			return
		}
		scopeKBID = kbID
		effectiveTenantID = effID
		ctx = types.WithExecutionTenant(c.Request.Context(), effectiveTenantID)

		logger.Infof(ctx, "Batch retrieving knowledge with kb_id, effective tenant ID: %d, IDs count: %d",
			effectiveTenantID, len(req.IDs))

		knowledges, err = h.kgService.GetKnowledgeBatch(ctx, effectiveTenantID, req.IDs)
	} else {
		knowledges, err = h.kgService.GetKnowledgeBatchWithSharedAccess(ctx, effectiveTenantID, req.IDs)
	}
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		_ = c.Error(errors.NewInternalServerError("Failed to retrieve knowledge list").WithDetails(err.Error()))
		return
	}

	// Build the effective allowed-KB set from explicit kb_id and
	// per-API-key KB restrictions.
	var allowedKBSet map[string]bool
	if scopeKBID != "" {
		allowedKBSet = map[string]bool{scopeKBID: true}
	}

	if apiKeySet := tenantAPIKeyAllowedKBSet(ctx); apiKeySet != nil {
		allowedKBSet = intersectKBAllowSet(allowedKBSet, apiKeySet)
	}
	if allowedKBSet != nil {
		knowledges = filterKnowledgesByKBAllowSet(knowledges, allowedKBSet)
	}

	logger.Infof(ctx, "Batch knowledge retrieval successful, requested count: %d, returned count: %d",
		len(req.IDs), len(knowledges))

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    knowledges,
	})
}

// UpdateKnowledgeRequest defines the partial-update body for PUT /knowledge/:id.
// Omitted fields are left unchanged; an explicit empty description clears the summary.
type UpdateKnowledgeRequest struct {
	Title          *string         `json:"title"`
	Description    *string         `json:"description"`
	CustomMetadata json.RawMessage `json:"custom_metadata"`
}

// UpdateKnowledge godoc
// @Summary      Cập nhật tài liệu
// @Description  Cập nhật một phần mục tài liệu (tiêu đề/mô tả/metadata tuỳ chỉnh); trường không truyền giữ nguyên, truyền description rỗng để xoá tóm tắt
// @Tags         Tài liệu
// @Accept       json
// @Produce      json
// @Param        id       path      string                   true  "ID tài liệu"
// @Param        request  body      UpdateKnowledgeRequest   true  "Các trường cập nhật (đều tuỳ chọn)"
// @Success      200      {object}  map[string]interface{}  "Cập nhật thành công"
// @Failure      400      {object}  errors.AppError         "tham số yêu cầu không hợp lệ"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge/{id} [put]
func (h *KnowledgeHandler) UpdateKnowledge(c *gin.Context) {
	ctx := c.Request.Context()

	id := secutils.SanitizeForLog(c.Param("id"))
	if id == "" {
		logger.Error(ctx, "Knowledge ID is empty")
		c.Error(errors.NewBadRequestError("Knowledge ID cannot be empty"))
		return
	}

	_, effCtx, err := h.resolveKnowledgeAndValidateKBAccess(c, id, types.KBPermissionEditor)
	if err != nil {
		c.Error(err)
		return
	}

	var req UpdateKnowledgeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to parse request parameters", err)
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	knowledge := types.Knowledge{ID: id, CustomMetadata: types.JSON(req.CustomMetadata)}
	if req.Title != nil {
		knowledge.Title = *req.Title
	}
	if req.Description != nil {
		knowledge.Description = *req.Description
		knowledge.DescriptionSpecified = true
	}

	if err := h.kgService.UpdateKnowledge(effCtx, &knowledge); err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		var appErr *errors.AppError
		if goerrors.As(err, &appErr) {
			_ = c.Error(appErr)
		} else {
			_ = c.Error(errors.NewInternalServerError(err.Error()))
		}
		return
	}
	updated, getErr := h.kgService.GetKnowledgeByID(effCtx, id)
	if getErr != nil {
		logger.Warnf(ctx, "Knowledge updated but failed to reload status for %s: %v", id, getErr)
	}

	logger.Infof(ctx, "Knowledge updated successfully, knowledge ID: %s", id)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Knowledge updated successfully",
		"data":    updated,
	})
}

// RegenerateKnowledgeSummary refreshes a stale summary after chunk or metadata edits.
func (h *KnowledgeHandler) RegenerateKnowledgeSummary(c *gin.Context) {
	ctx := c.Request.Context()
	id := secutils.SanitizeForLog(c.Param("id"))
	if id == "" {
		c.Error(errors.NewBadRequestError("Knowledge ID cannot be empty"))
		return
	}
	_, effCtx, err := h.resolveKnowledgeAndValidateKBAccess(c, id, types.KBPermissionEditor)
	if err != nil {
		c.Error(err)
		return
	}
	knowledge, err := h.kgService.GetKnowledgeByID(effCtx, id)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		var appErr *errors.AppError
		if goerrors.As(err, &appErr) {
			_ = c.Error(appErr)
		} else {
			_ = c.Error(errors.NewInternalServerError(err.Error()))
		}
		return
	}
	if knowledge.SummaryStatus == "" || knowledge.SummaryStatus == types.SummaryStatusNone {
		knowledge, err = h.kgService.RegenerateKnowledgeSummary(effCtx, id)
	} else {
		err = h.kgService.RequestKnowledgeSummaryRefresh(effCtx, id)
		if err == nil {
			knowledge, err = h.kgService.GetKnowledgeByID(effCtx, id)
		}
	}
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		var appErr *errors.AppError
		if goerrors.As(err, &appErr) {
			_ = c.Error(appErr)
		} else {
			_ = c.Error(errors.NewBadRequestError(err.Error()))
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": knowledge})
}

// UpdateManualKnowledge godoc
// @Summary      Cập nhật tài liệu thủ công
// @Description  Cập nhật nội dung Markdown của tài liệu nhập thủ công
// @Tags         Tài liệu
// @Accept       json
// @Produce      json
// @Param        id       path      string                       true  "ID tài liệu"
// @Param        request  body      types.ManualKnowledgePayload true  "Nội dung tài liệu thủ công"
// @Success      200      {object}  map[string]interface{}       "Tài liệu sau khi cập nhật"
// @Failure      400      {object}  errors.AppError              "tham số yêu cầu không hợp lệ"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge/manual/{id} [put]
func (h *KnowledgeHandler) UpdateManualKnowledge(c *gin.Context) {
	ctx := c.Request.Context()
	logger.Info(ctx, "Start updating manual knowledge")

	id := secutils.SanitizeForLog(c.Param("id"))
	if id == "" {
		logger.Error(ctx, "Knowledge ID is empty")
		c.Error(errors.NewBadRequestError("Knowledge ID cannot be empty"))
		return
	}

	_, effCtx, err := h.resolveKnowledgeAndValidateKBAccess(c, id, types.KBPermissionEditor)
	if err != nil {
		c.Error(err)
		return
	}

	var req types.ManualKnowledgePayload
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to parse manual knowledge update request", err)
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}

	knowledge, err := h.kgService.UpdateManualKnowledge(effCtx, id, &req)
	if err != nil {
		if appErr, ok := errors.IsAppError(err); ok {
			c.Error(appErr)
			return
		}
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"knowledge_id": id,
		})
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}

	logger.Infof(ctx, "Manual knowledge updated successfully, knowledge ID: %s", id)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    knowledge,
	})
}

// ReparseKnowledge godoc
// @Summary      Phân tích lại tài liệu
// @Description  Xoá nội dung hiện có của tài liệu và phân tích lại, xử lý bằng tác vụ bất đồng bộ
// @Tags         Tài liệu
// @Accept       json
// @Produce      json
// @Param        id   path      string  true  "ID tài liệu"
// @Param        body body      object  false  "Cấu hình xử lý ghi đè tuỳ chọn: {\"process_config\": KnowledgeProcessOverrides}"
// @Success      200  {object}  map[string]interface{}  "Tác vụ phân tích lại đã được gửi"
// @Failure      400  {object}  errors.AppError         "tham số yêu cầu không hợp lệ"
// @Failure      403  {object}  errors.AppError         "không có quyền"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge/{id}/reparse [post]
func (h *KnowledgeHandler) ReparseKnowledge(c *gin.Context) {
	ctx := c.Request.Context()
	logger.Info(ctx, "Start re-parsing knowledge")

	id := secutils.SanitizeForLog(c.Param("id"))
	if id == "" {
		logger.Error(ctx, "Knowledge ID is empty")
		c.Error(errors.NewBadRequestError("Knowledge ID cannot be empty"))
		return
	}

	// Validate KB access with editor permission (reparse requires write access)
	_, effCtx, err := h.resolveKnowledgeAndValidateKBAccess(c, id, types.KBPermissionEditor)
	if err != nil {
		c.Error(err)
		return
	}

	// Optional per-reparse parse config override. Empty body keeps the
	// overrides stored at upload time.
	var processOverrides *types.KnowledgeProcessOverrides
	if c.Request.ContentLength != 0 {
		var req struct {
			ProcessConfig *types.KnowledgeProcessOverrides `json:"process_config"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			logger.Error(ctx, "Failed to parse reparse request body", err)
			c.Error(errors.NewBadRequestError("Invalid reparse request body").WithDetails(err.Error()))
			return
		}
		processOverrides = platformProcessOverrides(ctx, req.ProcessConfig)
	}

	// Call service to reparse knowledge
	knowledge, err := h.kgService.ReparseKnowledge(effCtx, id, processOverrides)
	if err != nil {
		if appErr, ok := errors.IsAppError(err); ok {
			c.Error(appErr)
			return
		}
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"knowledge_id": id,
		})
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}

	logger.Infof(ctx, "Knowledge reparse task submitted successfully, knowledge ID: %s", id)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Knowledge reparse task submitted",
		"data":    knowledge,
	})
}

// CancelKnowledgeParse godoc
// @Summary      Huỷ phân tích tài liệu
// @Description  Huỷ tác vụ phân tích tài liệu đang chạy. Các đoạn (chunk) / chỉ mục đã ghi được giữ lại, có thể phân tích lại qua API reparse. Tài liệu đã hoàn thành / thất bại / đang xoá không huỷ được.
// @Tags         Tài liệu
// @Accept       json
// @Produce      json
// @Param        id   path      string  true  "ID tài liệu"
// @Success      200  {object}  map[string]interface{}  "Yêu cầu huỷ đã được gửi"
// @Failure      400  {object}  errors.AppError         "trạng thái hiện tại không cho phép huỷ"
// @Failure      403  {object}  errors.AppError         "không có quyền"
// @Failure      404  {object}  errors.AppError         "tài liệu không tồn tại"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge/{id}/cancel-parse [post]
func (h *KnowledgeHandler) CancelKnowledgeParse(c *gin.Context) {
	ctx := c.Request.Context()
	logger.Info(ctx, "Start cancelling knowledge parse")

	id := secutils.SanitizeForLog(c.Param("id"))
	if id == "" {
		logger.Error(ctx, "Knowledge ID is empty")
		c.Error(errors.NewBadRequestError("Knowledge ID cannot be empty"))
		return
	}

	// Editor permission — same gate as ReparseKnowledge / DeleteKnowledge.
	_, effCtx, err := h.resolveKnowledgeAndValidateKBAccess(c, id, types.KBPermissionEditor)
	if err != nil {
		c.Error(err)
		return
	}

	knowledge, err := h.kgService.CancelKnowledgeParse(effCtx, id)
	if err != nil {
		if appErr, ok := errors.IsAppError(err); ok {
			c.Error(appErr)
			return
		}
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"knowledge_id": id,
		})
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}

	logger.Infof(ctx, "Knowledge parse cancelled successfully, knowledge ID: %s", id)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Knowledge parse cancelled",
		"data":    knowledge,
	})
}

type knowledgeTagBatchRequest struct {
	Updates map[string][]string `json:"updates" binding:"required,min=1"`
	KBID    string              `json:"kb_id"` // Optional: scope to this KB (validates editor access and uses effective tenant for shared KB)
}

// UpdateKnowledgeTagBatch godoc
// @Summary      Cập nhật thẻ cho nhiều tài liệu
// @Description  Cập nhật thẻ của nhiều mục tài liệu. kb_id tuỳ chọn: khi truyền sẽ kiểm tra quyền sửa theo kho tri thức đó và dùng để xác định tổ chức của kho tri thức chia sẻ
// @Tags         Tài liệu
// @Accept       json
// @Produce      json
// @Param        request  body      object  true  "Yêu cầu cập nhật thẻ (updates bắt buộc, kb_id tuỳ chọn)"
// @Success      200      {object}  map[string]interface{}  "Cập nhật thành công"
// @Failure      400      {object}  errors.AppError         "tham số yêu cầu không hợp lệ"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge/tags [put]
func (h *KnowledgeHandler) UpdateKnowledgeTagBatch(c *gin.Context) {
	ctx := c.Request.Context()

	// Ensure tenant ID is in context (service reads it; may be missing if request context was not set by auth)
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	if tenantID == 0 {
		c.Error(errors.NewUnauthorizedError("Unauthorized"))
		return
	}
	ctx = types.WithExecutionTenant(ctx, tenantID)

	var req knowledgeTagBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to parse knowledge tag batch request", err)
		c.Error(errors.NewBadRequestError("tham số yêu cầu không hợp lệ").WithDetails(err.Error()))
		return
	}
	// Resolve effective tenant and the authorized KB scope.
	var authorizedKBID string
	if kbID := secutils.SanitizeForLog(req.KBID); kbID != "" {
		_, _, effID, permission, err := h.validateKnowledgeBaseWriteAccessWithKBID(c, kbID)
		if err != nil {
			c.Error(err)
			return
		}
		if permission != types.KBPermissionAdmin && permission != types.KBPermissionEditor {
			c.Error(errors.NewForbiddenError("No permission to update knowledge tags"))
			return
		}
		authorizedKBID = kbID
		ctx = types.WithExecutionTenant(c.Request.Context(), effID)
	} else if len(req.Updates) > 0 {
		// No kb_id: infer from first knowledge ID so shared-KB updates work without client sending kb_id
		var firstKnowledgeID string
		for id := range req.Updates {
			firstKnowledgeID = id
			break
		}
		if firstKnowledgeID != "" {
			knowledge, effCtx, err := h.resolveKnowledgeAndValidateKBAccess(c, firstKnowledgeID, types.KBPermissionEditor)
			if err != nil {
				c.Error(err)
				return
			}
			authorizedKBID = knowledge.KnowledgeBaseID
			ctx = effCtx
		}
	}
	if err := h.requireKBOwnershipOrAdmin(c, authorizedKBID); err != nil {
		_ = c.Error(err)
		return
	}

	if err := h.kgService.UpdateKnowledgeTagBatch(ctx, authorizedKBID, req.Updates); err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
	})
}

// UpdateImageInfo godoc
// @Summary      Cập nhật thông tin ảnh
// @Description  Cập nhật thông tin ảnh của đoạn (chunk) tài liệu
// @Tags         Tài liệu
// @Accept       json
// @Produce      json
// @Param        id        path      string  true  "ID tài liệu"
// @Param        chunk_id  path      string  true  "ID đoạn (chunk)"
// @Param        request   body      object{image_info=string}  true  "Thông tin ảnh"
// @Success      200       {object}  map[string]interface{}     "Cập nhật thành công"
// @Failure      400       {object}  errors.AppError            "tham số yêu cầu không hợp lệ"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge/image/{id}/{chunk_id} [put]
func (h *KnowledgeHandler) UpdateImageInfo(c *gin.Context) {
	ctx := c.Request.Context()
	logger.Info(ctx, "Start updating image info")

	id := secutils.SanitizeForLog(c.Param("id"))
	if id == "" {
		logger.Error(ctx, "Knowledge ID is empty")
		c.Error(errors.NewBadRequestError("Knowledge ID cannot be empty"))
		return
	}
	chunkID := secutils.SanitizeForLog(c.Param("chunk_id"))
	if chunkID == "" {
		logger.Error(ctx, "Chunk ID is empty")
		c.Error(errors.NewBadRequestError("Chunk ID cannot be empty"))
		return
	}

	_, effCtx, err := h.resolveKnowledgeAndValidateKBAccess(c, id, types.KBPermissionEditor)
	if err != nil {
		c.Error(err)
		return
	}

	var request struct {
		ImageInfo string `json:"image_info"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		logger.Error(ctx, "Failed to parse request parameters", err)
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}

	logger.Infof(ctx, "Updating knowledge chunk, knowledge ID: %s, chunk ID: %s", id, chunkID)
	err = h.kgService.UpdateImageInfo(effCtx, id, chunkID, secutils.SanitizeForLog(request.ImageInfo))
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		var appErr *errors.AppError
		if goerrors.As(err, &appErr) {
			_ = c.Error(appErr)
		} else {
			_ = c.Error(errors.NewInternalServerError(err.Error()))
		}
		return
	}

	logger.Infof(ctx, "Knowledge chunk updated successfully, knowledge ID: %s, chunk ID: %s", id, chunkID)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Knowledge chunk image updated successfully",
	})
}

// SearchKnowledge godoc
// @Summary      Search knowledge
// @Description  Search knowledge files by keyword. Pass recent=true without a keyword to browse recent files. When agent_id is set (shared agent), scope is the agent's configured knowledge bases.
// @Tags         Tài liệu
// @Accept       json
// @Produce      json
// @Param        keyword    query     string  false "Keyword to search"
// @Param        offset     query     int     false "Offset for pagination (minimum 0)" minimum(0)
// @Param        limit      query     int     false "Limit for pagination (default 20, maximum 100)" minimum(1) maximum(100)
// @Param        file_types query     string  false "Comma-separated file extensions to filter (e.g., csv,xlsx)"
// @Param        agent_id   query     string  false "Shared agent ID (search within agent's KB scope)"
// @Param        recent     query     bool    false "Return recent files when keyword is empty"
// @Success      200         {object}  map[string]interface{}     "Search results"
// @Failure      400         {object}  errors.AppError            "Invalid request"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge/search [get]
func (h *KnowledgeHandler) SearchKnowledge(c *gin.Context) {
	ctx := c.Request.Context()
	if userID, ok := c.Get(types.UserIDContextKey.String()); ok {
		ctx = context.WithValue(ctx, types.UserIDContextKey, userID)
	}
	// Accept both ?keyword= (legacy / upstream name) and ?query= (what most
	// MCP / agent integrations send). Empty input is only valid for an explicit
	// recent-file browse request; ordinary callers still get a clear 400 instead
	// of silently receiving the same newest cards for every missing query.
	keyword := c.Query("keyword")
	if keyword == "" {
		keyword = c.Query("query")
	}
	recent, _ := strconv.ParseBool(c.DefaultQuery("recent", "false"))
	if strings.TrimSpace(keyword) == "" && !recent {
		c.Error(errors.NewBadRequestError("missing search keyword: pass ?keyword=... or ?query=..."))
		return
	}
	keyword = strings.TrimSpace(keyword)
	offset, limit, ok := parseOffsetPagination(c)
	if !ok {
		return
	}

	var fileTypes []string
	if fileTypesStr := c.Query("file_types"); fileTypesStr != "" {
		for _, ft := range strings.Split(fileTypesStr, ",") {
			ft = strings.TrimSpace(ft)
			if ft != "" {
				fileTypes = append(fileTypes, ft)
			}
		}
	}

	if scopes, restricted := tenantAPIKeySearchScopes(ctx); restricted {
		if len(scopes) == 0 {
			c.JSON(http.StatusOK, gin.H{
				"success":  true,
				"data":     []interface{}{},
				"has_more": false,
				"total":    0,
			})
			return
		}
		knowledges, hasMore, total, err := h.kgService.SearchKnowledgeForScopes(ctx, scopes, keyword, offset, limit, fileTypes)
		if err != nil {
			logger.ErrorWithFields(ctx, err, nil)
			c.Error(errors.NewInternalServerError("Failed to search knowledge").WithDetails(err.Error()))
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"success":  true,
			"data":     knowledges,
			"has_more": hasMore,
			"total":    total,
		})
		return
	}

	// Default: own + shared KBs
	knowledges, hasMore, total, err := h.kgService.SearchKnowledge(ctx, keyword, offset, limit, fileTypes)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError("Failed to search knowledge").WithDetails(err.Error()))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"data":     knowledges,
		"has_more": hasMore,
		"total":    total,
	})
}

// MoveKnowledgeRequest defines the request for moving knowledge items
type MoveKnowledgeRequest struct {
	KnowledgeIDs []string `json:"knowledge_ids" binding:"required,min=1"`
	SourceKBID   string   `json:"source_kb_id"  binding:"required"`
	TargetKBID   string   `json:"target_kb_id"  binding:"required"`
	Mode         string   `json:"mode"          binding:"required,oneof=reuse_vectors reparse"`
}

// MoveKnowledgeResponse defines the response for move knowledge
type MoveKnowledgeResponse struct {
	TaskID         string `json:"task_id"`
	SourceKBID     string `json:"source_kb_id"`
	TargetKBID     string `json:"target_kb_id"`
	KnowledgeCount int    `json:"knowledge_count"`
	Message        string `json:"message"`
}

// MoveKnowledge moves knowledge items from one knowledge base to another (async task).
//
// MoveKnowledge godoc
// @Summary      Chuyển tài liệu sang kho tri thức khác
// @Description  Chuyển một hoặc nhiều tài liệu từ kho tri thức nguồn sang kho tri thức đích (bất đồng bộ), trả về ID tác vụ để theo dõi tiến độ
// @Tags         Tài liệu
// @Accept       json
// @Produce      json
// @Param        request  body      handler.MoveKnowledgeRequest  true  "{source_kb_id, target_kb_id, knowledge_ids}"
// @Success      200      {object}  handler.MoveKnowledgeResponse  "Thông tin tác vụ"
// @Failure      400      {object}  errors.AppError                "tham số yêu cầu không hợp lệ"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge/move [post]
func (h *KnowledgeHandler) MoveKnowledge(c *gin.Context) {
	ctx := c.Request.Context()

	var req MoveKnowledgeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "MoveKnowledge: failed to parse request", err)
		c.Error(errors.NewBadRequestError("Invalid request parameters: " + err.Error()))
		return
	}

	// Validate source != target
	if req.SourceKBID == req.TargetKBID {
		c.Error(errors.NewBadRequestError("Source and target knowledge base cannot be the same"))
		return
	}

	tenantID, exists := c.Get(types.TenantIDContextKey.String())
	if !exists {
		c.Error(errors.NewUnauthorizedError("Unauthorized"))
		return
	}
	if err := requireTenantAPIKeyKnowledgeBases(ctx, req.SourceKBID, req.TargetKBID); err != nil {
		c.Error(err)
		return
	}

	// Validate source KB
	sourceKB, err := h.kbService.GetKnowledgeBaseByID(ctx, req.SourceKBID)
	if err != nil {
		if goerrors.Is(err, repository.ErrKnowledgeBaseNotFound) {
			c.Error(errors.NewNotFoundError("Source knowledge base not found"))
			return
		}
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	if sourceKB.TenantID != tenantID.(uint64) {
		c.Error(errors.NewForbiddenError("No permission to access source knowledge base"))
		return
	}
	if err := h.requireKBOwnershipOrAdmin(c, req.SourceKBID); err != nil {
		c.Error(err)
		return
	}

	// Validate target KB
	targetKB, err := h.kbService.GetKnowledgeBaseByID(ctx, req.TargetKBID)
	if err != nil {
		if goerrors.Is(err, repository.ErrKnowledgeBaseNotFound) {
			c.Error(errors.NewNotFoundError("Target knowledge base not found"))
			return
		}
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	if targetKB.TenantID != tenantID.(uint64) {
		c.Error(errors.NewForbiddenError("No permission to access target knowledge base"))
		return
	}
	if err := h.requireKBOwnershipOrAdmin(c, req.TargetKBID); err != nil {
		c.Error(err)
		return
	}

	// Resolve explicit management grants independently for both body IDs:
	// owning tenant via the existing policy, platform-owned rows via
	// explicit human SuperAdmin only.
	if _, err := resolveHandlerKBManageAccessFor(c, req.SourceKBID, h.kbService, h.kbAccessGrantService); err != nil {
		_ = c.Error(err)
		return
	}
	if _, err := resolveHandlerKBManageAccessFor(c, req.TargetKBID, h.kbService, h.kbAccessGrantService); err != nil {
		_ = c.Error(err)
		return
	}
	taskID := utils.GenerateTaskID("kg_move", tenantID.(uint64), req.SourceKBID)
	ctx, err = access.WithKBTransfer(c.Request.Context(), sourceKB, targetKB, access.KBTransferMove, taskID, false)
	if err != nil {
		_ = c.Error(kbAccessHTTPError(err))
		return
	}
	tenant, _ := ctx.Value(types.TenantInfoContextKey).(*types.Tenant)
	if err := access.ValidateKBTransferCompatibility(sourceKB,
		targetKB,
		access.KBTransferMove,
		req.Mode,
		tenant); err != nil {
		_ = c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	uniqueIDs := make([]string, 0, len(req.KnowledgeIDs))
	seen := make(map[string]bool, len(req.KnowledgeIDs))
	for _, id := range req.KnowledgeIDs {
		if strings.TrimSpace(id) == "" {
			_ = c.Error(errors.NewBadRequestError("Knowledge ID cannot be empty"))
			return
		}
		if !seen[id] {
			seen[id] = true
			uniqueIDs = append(uniqueIDs, id)
		}
	}
	req.KnowledgeIDs = uniqueIDs

	// Validate all knowledge IDs belong to source KB and are in completed status
	for _, kID := range req.KnowledgeIDs {
		knowledge, err := h.kgService.GetKnowledgeByID(ctx, kID)
		if err != nil {
			c.Error(errors.NewBadRequestError(fmt.Sprintf("Knowledge item %s not found", kID)))
			return
		}
		if knowledge == nil || knowledge.ID != kID || knowledge.TenantID != tenantID.(uint64) ||
			knowledge.KnowledgeBaseID != req.SourceKBID {
			_ = c.Error(
				errors.NewBadRequestError(
					fmt.Sprintf("Knowledge item %s does not belong to the source knowledge base", kID),
				),
			)
			return
		}
		if knowledge.ParseStatus != types.ParseStatusCompleted {
			c.Error(errors.NewBadRequestError(fmt.Sprintf("Knowledge item %s is not in completed status (current: %s)", kID, knowledge.ParseStatus)))
			return
		}
	}

	// Create move payload
	payload := types.KnowledgeMovePayload{
		TenantID:     tenantID.(uint64),
		TaskID:       taskID,
		KnowledgeIDs: req.KnowledgeIDs,
		SourceKBID:   req.SourceKBID,
		TargetKBID:   req.TargetKBID,
		Mode:         req.Mode,
		Initiator:    types.TaskInitiatorFromContext(ctx),
	}
	langfuse.InjectTracing(ctx, &payload)

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		logger.Errorf(ctx, "MoveKnowledge: failed to marshal payload: %v", err)
		c.Error(errors.NewInternalServerError("Failed to create task"))
		return
	}

	// Enqueue move task
	task := asynq.NewTask(types.TypeKnowledgeMove, payloadBytes,
		asynq.TaskID(taskID), asynq.Queue(types.QueueMaintenance),
		asynq.MaxRetry(3), asynq.Timeout(2*time.Hour))
	info, err := h.asynqClient.Enqueue(task)
	if err != nil {
		logger.Errorf(ctx, "MoveKnowledge: failed to enqueue task: %v", err)
		c.Error(errors.NewInternalServerError("Failed to enqueue task"))
		return
	}

	logger.Infof(ctx, "MoveKnowledge: task enqueued: %s, asynq_id: %s, source: %s, target: %s, count: %d",
		taskID, info.ID, secutils.SanitizeForLog(req.SourceKBID), secutils.SanitizeForLog(req.TargetKBID), len(req.KnowledgeIDs))

	// Save initial progress
	initialProgress := &types.KnowledgeMoveProgress{
		TaskID:     taskID,
		SourceKBID: req.SourceKBID,
		TargetKBID: req.TargetKBID,
		Status:     types.KBCloneStatusPending,
		Total:      len(req.KnowledgeIDs),
		Progress:   0,
		Message:    "Task queued, waiting to start...",
		CreatedAt:  time.Now().Unix(),
		UpdatedAt:  time.Now().Unix(),
	}
	if err := h.kgService.SaveKnowledgeMoveProgress(ctx, initialProgress); err != nil {
		logger.Warnf(ctx, "MoveKnowledge: failed to save initial progress: %v", err)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": MoveKnowledgeResponse{
			TaskID:         taskID,
			SourceKBID:     req.SourceKBID,
			TargetKBID:     req.TargetKBID,
			KnowledgeCount: len(req.KnowledgeIDs),
			Message:        "Knowledge move task started",
		},
	})
}

// GetKnowledgeMoveProgress retrieves the progress of a knowledge move task.
//
// GetKnowledgeMoveProgress godoc
// @Summary      Lấy tiến độ chuyển tài liệu
// @Description  Truy vấn tiến độ chuyển theo ID tác vụ
// @Tags         Tài liệu
// @Produce      json
// @Param        task_id  path      string                       true  "ID tác vụ di chuyển"
// @Success      200      {object}  types.KnowledgeMoveProgress  "Thông tin tiến độ"
// @Failure      404      {object}  errors.AppError              "tác vụ không tồn tại"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge/move/progress/{task_id} [get]
func (h *KnowledgeHandler) GetKnowledgeMoveProgress(c *gin.Context) {
	ctx := c.Request.Context()

	taskID := c.Param("task_id")
	if taskID == "" {
		c.Error(errors.NewBadRequestError("Task ID cannot be empty"))
		return
	}
	if err := requireTaskProgressTenant(ctx, taskID); err != nil {
		c.Error(err)
		return
	}

	progress, err := h.kgService.GetKnowledgeMoveProgress(ctx, taskID)
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

// parseCommaSeparatedTagIDs splits a comma-separated string of tag IDs and
// filters out empty strings and the "__untagged__" sentinel value.
func parseCommaSeparatedTagIDs(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || p == "__untagged__" {
			continue
		}
		result = append(result, p)
	}
	return result
}

// parseFilterTime parses a query-string timestamp accepted by knowledge list
// filters. It supports RFC3339, RFC3339 with milliseconds, and the date-only
// "2006-01-02" form (interpreted at start of day in the local timezone).
func parseFilterTime(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	layouts := []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"}
	var lastErr error
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			return t, nil
		} else {
			lastErr = err
		}
	}
	return time.Time{}, lastErr
}

type batchReparseKnowledgeRequest struct {
	KBID          string                           `json:"kb_id" binding:"required"`
	IDs           []string                         `json:"ids" binding:"required"`
	ProcessConfig *types.KnowledgeProcessOverrides `json:"process_config,omitempty"`
}

// BatchReparseKnowledge godoc
// @Summary      Phân tích lại nhiều tài liệu
// @Description  Phân tích lại nhiều mục tài liệu trong một kho tri thức theo danh sách ID
// @Tags         Tài liệu
// @Accept       json
// @Produce      json
// @Param        request  body      batchReparseKnowledgeRequest  true  "Yêu cầu phân tích lại hàng loạt"
// @Success      200      {object}  map[string]interface{}        "Tác vụ đã được gửi"
// @Failure      400      {object}  errors.AppError               "tham số yêu cầu không hợp lệ"
// @Failure      403      {object}  errors.AppError               "không có quyền"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge/batch-reparse [post]
func (h *KnowledgeHandler) BatchReparseKnowledge(c *gin.Context) {
	ctx := c.Request.Context()
	var req batchReparseKnowledgeRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Errorf(ctx, "failed to parse batch reparse knowledge request: %v", err)
		c.Error(errors.NewBadRequestError("invalid batch reparse knowledge request parameters"))
		return
	}

	seen := make(map[string]struct{}, len(req.IDs))
	ids := make([]string, 0, len(req.IDs))
	for _, raw := range req.IDs {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}

	if len(ids) == 0 {
		c.Error(errors.NewBadRequestError("no knowledge IDs provided for batch reparse"))
		return
	}
	const maxBatch = 200
	if len(ids) > maxBatch {
		c.Error(errors.NewBadRequestError(fmt.Sprintf("too many ids (max %d per batch)", maxBatch)))
		return
	}

	_, kbID, effectiveTenantID, permission, err := h.validateKnowledgeBaseWriteAccessWithKBID(c, req.KBID)
	if err != nil {
		c.Error(err)
		return
	}
	if permission != types.KBPermissionAdmin && permission != types.KBPermissionEditor {
		c.Error(errors.NewForbiddenError("no permission to reparse knowledge in this kb"))
		return
	}
	if err := h.requireKBOwnershipOrAdmin(c, kbID); err != nil {
		_ = c.Error(err)
		return
	}
	ctx = types.WithExecutionTenant(c.Request.Context(), effectiveTenantID)

	knowledgeList, err := h.kgService.GetKnowledgeBatch(ctx, effectiveTenantID, ids)
	if err != nil {
		logger.Errorf(ctx, "failed to get knowledge batch, kb_id: %s, size: %d, err: %v", kbID, len(ids), err)
		c.Error(errors.NewInternalServerError("failed to get knowledge batch"))
		return
	}
	if len(knowledgeList) != len(ids) {
		c.Error(errors.NewBadRequestError("some knowledge entries were not found"))
		return
	}
	for _, k := range knowledgeList {
		if err := access.RejectMovingKnowledge(k); err != nil {
			_ = c.Error(err)
			return
		}
		if k.KnowledgeBaseID != kbID {
			c.Error(errors.NewBadRequestError(
				fmt.Sprintf("Knowledge %s does not belong to knowledge base %s",
					secutils.SanitizeForLog(k.ID), secutils.SanitizeForLog(kbID))))
			return
		}
	}

	taskID, err := h.enqueueKnowledgeListReparse(ctx, effectiveTenantID, kbID, ids,
		platformProcessOverrides(ctx, req.ProcessConfig))
	if err != nil {
		logger.Errorf(ctx, "Failed to enqueue batch knowledge reparse task: %v", err)
		c.Error(errors.NewInternalServerError("Failed to enqueue batch reparse task"))
		return
	}

	logger.Infof(ctx, "Batch knowledge reparse task enqueued: %s, kb_id: %s, count: %d",
		taskID, secutils.SanitizeForLog(kbID), len(ids))

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Batch reparse task submitted",
		"data": gin.H{
			"task_id":       taskID,
			"reparse_count": len(ids),
		},
	})
}

func requireTenantAPIKeyKnowledgeBase(ctx context.Context, kbID string) error {
	return requireTenantAPIKeyKnowledgeBases(ctx, kbID)
}

func requireTenantAPIKeyKnowledgeBases(ctx context.Context, kbIDs ...string) error {
	return types.AuthorizeTenantAPIKeyKnowledgeBases(ctx, kbIDs...)
}

func tenantAPIKeyAllowedKBSet(ctx context.Context) map[string]bool {
	scope, ok := types.TenantAPIKeyScopeFromContext(ctx)
	if !ok || !scope.IsKnowledgeBaseRestricted() {
		return nil
	}
	allowed := make(map[string]bool, len(scope.KnowledgeBaseIDs))
	for _, id := range scope.KnowledgeBaseIDs {
		allowed[id] = true
	}
	return allowed
}

func intersectKBAllowSet(base, restrict map[string]bool) map[string]bool {
	if restrict == nil {
		return base
	}
	if base == nil {
		return restrict
	}
	out := make(map[string]bool)
	for id := range base {
		if restrict[id] {
			out[id] = true
		}
	}
	return out
}

func filterKnowledgesByKBAllowSet(knowledges []*types.Knowledge, allowed map[string]bool) []*types.Knowledge {
	if allowed == nil {
		return knowledges
	}
	filtered := make([]*types.Knowledge, 0, len(knowledges))
	for _, k := range knowledges {
		if k != nil && allowed[k.KnowledgeBaseID] {
			filtered = append(filtered, k)
		}
	}
	return filtered
}

func filterKnowledgeSearchScopesForAPIKey(ctx context.Context, scopes []types.KnowledgeSearchScope) []types.KnowledgeSearchScope {
	allowed := tenantAPIKeyAllowedKBSet(ctx)
	if allowed == nil {
		return scopes
	}
	filtered := make([]types.KnowledgeSearchScope, 0, len(scopes))
	for _, scope := range scopes {
		if allowed[scope.KBID] {
			filtered = append(filtered, scope)
		}
	}
	return filtered
}

func tenantAPIKeySearchScopes(ctx context.Context) ([]types.KnowledgeSearchScope, bool) {
	scope, ok := types.TenantAPIKeyScopeFromContext(ctx)
	if !ok || !scope.IsKnowledgeBaseRestricted() {
		return nil, false
	}
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok || tenantID == 0 {
		return nil, true
	}
	scopes := make([]types.KnowledgeSearchScope, 0, len(scope.KnowledgeBaseIDs))
	for _, kbID := range scope.KnowledgeBaseIDs {
		scopes = append(scopes, types.KnowledgeSearchScope{TenantID: tenantID, KBID: kbID})
	}
	return scopes, true
}

// isHumanMember reports whether ctx is a human Member (not an API key or a
// Tenant Admin), the caller the per-uploader document rules apply to.
func isHumanMember(ctx context.Context) bool {
	caller := types.CallerFromContext(ctx)
	return caller.Role.HasPermission(types.TenantRoleMember) && !caller.Role.IsTenantAdmin() &&
		access.IsAuthenticatedHuman(ctx, caller)
}

// platformProcessOverrides keeps per-upload parse overrides only for callers
// who may configure the platform. Parse settings (models, chunking, graph,
// OCR) are SuperAdmin configuration; everyone else parses with what the
// knowledge base and the system defaults already say, so their overrides are
// dropped rather than rejected — the upload itself is still theirs to make.
func platformProcessOverrides(
	ctx context.Context, overrides *types.KnowledgeProcessOverrides,
) *types.KnowledgeProcessOverrides {
	if overrides == nil || canManagePlatformConfig(ctx) {
		return overrides
	}
	logger.Infof(ctx, "[knowledge] ignoring parse overrides from a caller who cannot configure the platform")
	return nil
}
