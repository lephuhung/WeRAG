package router

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/handler"
)

// RegisterChunkerDebugRoutes wires the read-only chunker preview endpoint
// used by the KB editor's debug panel. Stateless — uses no service deps.
//
// Viewer+ floor: the endpoint surfaces inside the tenant UI, so any
// authenticated tenant member can call it; revoked accounts whose JWT
// has not yet expired are kept out by the role check, matching the
// rest of the RBAC matrix in this file.
func RegisterChunkerDebugRoutes(r *gin.RouterGroup, g *rbacGuards) {
	g.apiKeyRoute(r, http.MethodPost, "/chunker/preview", apiKeyRetrieve(apiKeyIngest(apiKeyFullAccess())), g.SystemAdmin(), handler.PreviewChunking)
}

// RegisterChunkRoutes đăng ký các route đoạn (chunk)
//
// Mutating routes addressed via :knowledge_id inherit per-KB ownership
// from the owning knowledge entry's KB (PR 5, #1303); the chain hop is
// shared with RegisterKnowledgeRoutes via OwnedChunkKBOrAdmin so the
// same "creator-of-the-KB OR Admin+" rule applies to chunk edits.
func RegisterChunkRoutes(r *gin.RouterGroup, handler *handler.ChunkHandler, g *rbacGuards) {
	// Nhóm route đoạn (chunk). Scoped API key cần capability ingest để ghi, retrieve để đọc;
	// cả hai vẫn bị ràng buộc bởi allow-list KB.
	chunks := g.apiKeyGroup(r.Group("/chunks"), apiKeyIngest(apiKeyFullAccess()))
	chunkRead := chunks.With(apiKeyRetrieve(apiKeyFullAccess()))
	{
		// Lấy danh sách đoạn — Viewer+ và có quyền read trên KB cha (own / shared / qua trợ lý được chia sẻ)
		chunkRead.GET("/:knowledge_id", g.Member(), g.KBAccessReadFromKnowledgeIDParam("knowledge_id"), handler.ListKnowledgeChunks)
		// Lấy một chunk theo chunk_id (không cần knowledge_id) — Viewer+ và có quyền read trên KB cha
		chunkRead.GET("/by-id/:id", g.Member(), g.KBAccessReadFromChunkIDParam("id"), handler.GetChunkByIDOnly)
		chunkRead.GET("/:knowledge_id/:id/revisions", g.Member(), g.KBAccessReadFromKnowledgeIDParam("knowledge_id"), handler.ListChunkRevisions)
		// Xoá đoạn — Tenant Admin và có quyền write trên KB cha
		chunks.DELETE("/:knowledge_id/:id", g.ChunkDocumentEditor(), g.KBAccessManageFromKnowledgeIDParam("knowledge_id"), handler.DeleteChunk)
		// Xoá mọi đoạn của một tài liệu — Tenant Admin và có quyền write trên KB cha
		chunks.DELETE("/:knowledge_id", g.ChunkDocumentEditor(), g.KBAccessManageFromKnowledgeIDParam("knowledge_id"), handler.DeleteChunksByKnowledgeID)
		// Cập nhật thông tin đoạn — Tenant Admin và có quyền write trên KB cha
		chunks.PUT("/:knowledge_id/:id", g.ChunkDocumentEditor(), g.KBAccessManageFromKnowledgeIDParam("knowledge_id"), handler.UpdateChunk)
		chunks.POST("/:knowledge_id/:id/revert", g.ChunkDocumentEditor(), g.KBAccessManageFromKnowledgeIDParam("knowledge_id"), handler.RevertChunk)
		// Xoá một câu hỏi đã sinh (theo id đoạn) — giống các mutation chunk khác:
		// KB owner HOẶC Admin+. Trước đây vì chuỗi tra cứu (chunk_id -> knowledge_id ->
		// kb -> creator_id) chưa nối thông nên tạm hạ xuống Contributor, gây ra sự thiếu nhất quán:
		// route này lại lỏng hơn quy tắc chung cho phép sửa mọi chunk.
		// Nay bổ sung bước đó bằng KBCreatorLookupFromChunkIDParam để thống nhất ma trận.
		chunks.DELETE("/by-id/:id/questions", g.TenantAdmin(), g.KBAccessManageFromChunkIDParam("id"), handler.DeleteGeneratedQuestion)
		chunks.PUT("/by-id/:id/questions", g.TenantAdmin(), g.KBAccessManageFromChunkIDParam("id"), handler.UpsertGeneratedQuestion)
		chunks.POST("/by-id/:id/questions/regenerate", g.TenantAdmin(), g.KBAccessManageFromChunkIDParam("id"), handler.RegenerateGeneratedQuestions)
	}
}

// RegisterKnowledgeRoutes đăng ký các route tài liệu
//
// Per-KB ownership applies on the per-:id mutating routes (PR 5,
// #1303): the URL :id is a knowledge id, OwnedKnowledgeKBOrAdmin
// walks it back to KB.CreatorID so a Contributor who owns the KB can
// edit/delete any of its documents while a non-owner Contributor gets
// 403. KB-scoped upload routes (`/knowledge-bases/:id/knowledge/...`)
// reuse OwnedKBOrAdmin because the URL :id is the KB id directly.
// Body-scoped batch operations have a Contributor route gate and resolve
// their KB ownership plus Editor operation grant inside the handler.
func RegisterKnowledgeRoutes(r *gin.RouterGroup, handler *handler.KnowledgeHandler, g *rbacGuards) {
	// Nhóm route tài liệu trong kho tri thức (URL :id is the KB id). Scoped API key cần
	// capability ingest mới ghi được và vẫn bị giới hạn phạm vi KB; xoá sạch KB chỉ cho phép full-access key.
	kb := g.apiKeyGroup(r.Group("/knowledge-bases/:id/knowledge"), apiKeyIngest(apiKeyFullAccess()))
	kbRead := kb.With(apiKeyRetrieve(apiKeyFullAccess()))
	{
		// Upload (member ingestion): any tenant member may ingest NEW
		// documents into their OWN tenant's KBs. KBAccessManage keeps
		// foreign KBs out, and platform-owned public KBs admit only an
		// explicit human SuperAdmin — a public Viewer is never an editor.
		// Edits/deletes of EXISTING content stay TenantAdmin-only below.
		kb.POST("/file", g.Member(), g.KBAccessManage("id"), handler.CreateKnowledgeFromFile)
		kb.POST("/url", g.Member(), g.KBAccessManage("id"), handler.CreateKnowledgeFromURL)
		kb.POST("/manual", g.Member(), g.KBAccessManage("id"), handler.CreateManualKnowledge)
		kbRead.GET("", g.Member(), g.KBAccessRead("id"), handler.ListKnowledge)
		// Tải hàng loạt tệp gốc đi qua KBAccessDownload: KB của mình theo chính sách tải của đơn vị,
		// người đọc đã xác thực của KB công khai nền tảng được tải, người được mời chỉ đọc vẫn không được tải.
		kbRead.POST("/batch-download", g.Member(), g.KBAccessDownload("id"), handler.BatchDownloadKnowledge)
		kbRead.GET("/folders", g.Member(), g.KBAccessRead("id"), handler.ListKnowledgeFolders)
		kb.PUT("/folders", g.TenantAdmin(), g.KBAccessManage("id"), handler.RenameKnowledgeFolder)
		// Clearing all contents under a KB is a destructive op; gate
		// behind TenantAdmin (strict: enforced even when RBAC rollout is off).
		kb.With(apiKeyFullAccess()).DELETE("", g.TenantAdmin(), g.KBAccessManage("id"), handler.ClearKnowledgeBaseContents)
	}

	// Nhóm route tài liệu (URL :id is a knowledge id; the guard walks it to the parent KB)
	kgrp := r.Group("/knowledge")
	k := g.apiKeyGroup(kgrp, apiKeyIngest(apiKeyFullAccess()))
	kRead := k.With(apiKeyRetrieve(apiKeyFullAccess()))
	{
		// Cross-knowledge endpoints (no :id) can't be gated on a single
		// KB via the URL — they accept a kb_id (or source/target KB) in the
		// body and the handler fans out the access check itself. /batch and
		// /search are read routes (retrieve). /move, /batch-delete,
		// /batch-reparse and /tags are content writes that each bound
		// themselves to a single (or source+target) KB and enforce the API
		// key's KB allow-list downstream — MoveKnowledge via
		// requireTenantAPIKeyKnowledgeBases(source,target); the batch ops via
		// validateKnowledgeBaseAccessWithKBID (requireTenantAPIKeyKnowledgeBase)
		// plus a per-item "belongs to the authorized KB" check in the handler
		// and service. They are therefore declared for API keys under the
		// ingest capability, matching their single-document siblings
		// (k.DELETE("/:id"), k.POST("/:id/reparse"), k.PUT("/:id")).
		kRead.GET("/batch", g.Member(), handler.GetKnowledgeBatch)
		kRead.GET("/:id", g.Member(), g.KBAccessReadFromKnowledgeIDParam("id"), handler.GetKnowledge)
		kRead.GET("/:id/stages", g.Member(), g.KBAccessReadFromKnowledgeIDParam("id"), handler.GetKnowledgeSpans)
		kRead.GET("/:id/spans", g.Member(), g.KBAccessReadFromKnowledgeIDParam("id"), handler.GetKnowledgeSpans)
		k.DELETE("/:id", g.DocumentEditor(), g.KBAccessManageFromKnowledgeIDParam("id"), handler.DeleteKnowledge)
		k.PUT("/:id", g.DocumentEditor(), g.KBAccessManageFromKnowledgeIDParam("id"), handler.UpdateKnowledge)
		k.POST("/:id/regenerate-summary", g.DocumentEditor(), g.KBAccessManageFromKnowledgeIDParam("id"), handler.RegenerateKnowledgeSummary)
		k.PUT("/manual/:id", g.DocumentEditor(), g.KBAccessManageFromKnowledgeIDParam("id"), handler.UpdateManualKnowledge)
		k.POST("/:id/reparse", g.DocumentEditor(), g.KBAccessManageFromKnowledgeIDParam("id"), handler.ReparseKnowledge)
		k.POST("/:id/cancel-parse", g.DocumentEditor(), g.KBAccessManageFromKnowledgeIDParam("id"), handler.CancelKnowledgeParse)
		// Downloading exposes the original source file through the dedicated
		// download guard: same-owner-tenant callers keep the tenant download
		// policy, platform-public Viewers may download, and invitation
		// viewers stay denied. Preview keeps the Viewer boundary below.
		// API keys still follow the retrieve capability declared by kRead;
		// role guards intentionally defer machine-principal authorization
		// to the API-key gate.
		kRead.GET("/:id/download", g.Member(), g.KBAccessDownloadFromKnowledgeIDParam("id"), handler.DownloadKnowledgeFile)
		kRead.GET("/:id/preview", g.Member(), g.KBAccessReadFromKnowledgeIDParam("id"), handler.PreviewKnowledgeFile)
		k.PUT("/image/:id/:chunk_id", g.DocumentEditor(), g.KBAccessManageFromKnowledgeIDParam("id"), handler.UpdateImageInfo)
		kRead.GET("/search", g.Member(), handler.SearchKnowledge)
		kRead.GET("/move/progress/:task_id", g.Member(), handler.GetKnowledgeMoveProgress)
		// Batch / cross-KB content writes: JWT Admin+, or an API key
		// with the ingest capability (or full access). Each handler binds the
		// operation to a single (move: source+target) KB and rejects any KB
		// or knowledge id outside the key's allow-list, so a scoped ingest key
		// can only touch KBs it is already permitted to write.
		k.PUT("/tags", g.TenantAdmin(), handler.UpdateKnowledgeTagBatch)
		k.POST("/batch-reparse", g.TenantAdmin(), handler.BatchReparseKnowledge)
		// Members may batch-delete their own uploads; the handler checks
		// every id's uploader when the caller is not a Tenant Admin.
		k.POST("/batch-delete", g.Member(), handler.BatchDeleteKnowledge)
		k.POST("/folder", g.TenantAdmin(), handler.MoveKnowledgeToFolder)
		k.POST("/move", g.TenantAdmin(), handler.MoveKnowledge)
	}
}

// RegisterFAQRoutes đăng ký các route FAQ
//
// FAQ entries are KB content: reads are Viewer+, all mutations
// (create / update / upsert / delete / batch field+tag updates,
// import display flag) are Contributor+. Search is read-only.
func RegisterFAQRoutes(r *gin.RouterGroup, handler *handler.FAQHandler, g *rbacGuards) {
	if handler == nil {
		return
	}
	// FAQ entries là tài nguyên con của KB (phần nội dung chính của KB kiểu FAQ). Sửa FAQ
	// tương đương sửa nội dung KB, phải theo ma trận "creator OR Admin+" của KB —
	// nhất quán với chunks / wiki pages. Viewer+ đọc được, Contributor không được
	// sửa FAQ của KB không thuộc về mình.
	faq := g.apiKeyGroup(r.Group("/knowledge-bases/:id/faq"), apiKeyIngest(apiKeyFullAccess()))
	faqRead := faq.With(apiKeyRetrieve(apiKeyFullAccess()))
	{
		// KBAccessRead/Write resolve own/shared/agent-visible access and
		// rewrite the request's tenant context — handler no longer
		// carries an effectiveCtxForKB helper.
		faqRead.GET("/entries", g.Member(), g.KBAccessRead("id"), handler.ListEntries)
		faqRead.GET("/entries/export", g.Member(), g.KBAccessRead("id"), handler.ExportEntries)
		faqRead.GET("/entries/:entry_id", g.Member(), g.KBAccessRead("id"), handler.GetEntry)
		faq.POST("/entries", g.TenantAdmin(), g.KBAccessManage("id"), handler.UpsertEntries)
		faq.POST("/entry", g.TenantAdmin(), g.KBAccessManage("id"), handler.CreateEntry)
		faq.PUT("/entries/:entry_id", g.TenantAdmin(), g.KBAccessManage("id"), handler.UpdateEntry)
		faq.POST("/entries/:entry_id/similar-questions", g.TenantAdmin(), g.KBAccessManage("id"), handler.AddSimilarQuestions)
		// Unified batch update API - supports is_enabled, is_recommended, tag_id
		faq.PUT("/entries/fields", g.TenantAdmin(), g.KBAccessManage("id"), handler.UpdateEntryFieldsBatch)
		faq.PUT("/entries/tags", g.TenantAdmin(), g.KBAccessManage("id"), handler.UpdateEntryTagBatch)
		faq.DELETE("/entries", g.TenantAdmin(), g.KBAccessManage("id"), handler.DeleteEntries)
		// Search is a read route: scoped API keys may call it with retrieve
		// even though POST is otherwise an unsafe method.
		faqRead.POST("/search", g.Member(), g.KBAccessRead("id"), handler.SearchFAQ)
		// FAQ import result display status
		faq.PUT("/import/last-result/display", g.TenantAdmin(), g.KBAccessManage("id"), handler.UpdateLastImportResultDisplayStatus)
	}
	// FAQ import progress route (outside of knowledge-base scope) — Viewer+.
	// Scoped API keys that can ingest (they start the import) or retrieve may
	// poll their own import/dry-run progress. The task is tenant-scoped by
	// requireTaskProgressTenant, so a key only ever sees its own tenant's
	// tasks. Declared through apiKeyRoute so the APIKeyGate doesn't fail-closed
	// and 403 the poller with "scope does not allow this operation".
	g.apiKeyRoute(r, http.MethodGet, "/faq/import/progress/:task_id",
		apiKeyRetrieve(apiKeyIngest(apiKeyFullAccess())), g.Member(), handler.GetImportProgress)
}

// RegisterKnowledgeBaseRoutes đăng ký các route kho tri thức
func RegisterKnowledgeBaseRoutes(r *gin.RouterGroup, handler *handler.KnowledgeBaseHandler, g *rbacGuards) {
	// Nhóm route kho tri thức. Khả năng truy cập bằng API key chia hai mức theo capability, tất cả khai báo qua apiKeyGroup,
	// không đăng ký bằng kbgrp.Handle trần nữa (sẽ bỏ qua gateway và âm thầm từ chối mọi key):
	//
	//   1. Đọc (list/detail/search/progress/move-targets) — retrieve OR full-access (kb)
	//   2. Quản lý vòng đời KB (create/copy/duplicate/update/delete)
	//      — manage_kbs OR full-access (kbManagement). create của đơn vị chỉ tạo ra
	//      KB thuộc đơn vị; KB công khai nền tảng đi qua POST /public bên dưới (SuperAdmin là người thật,
	//      không khai báo chính sách API-key).
	//
	// Mức 2: toàn bộ vòng đời KB dùng chung một chính sách: manage_kbs là capability "quản lý kho tri thức",
	// tạo/sao chép/sửa/xoá đều thuộc phận sự của nó. Allow-list KB vẫn có hiệu lực ở hạ nguồn — KB đích của copy/duplicate/
	// update/delete bị allow-list chặn; create không có nguồn để ràng buộc, key có allow-list giới hạn
	// tạo ra KB mới nằm ngoài allow-list của nó (cùng đơn vị, không vượt quyền, chỉ là tạo xong không tự quản được),
	// key có allow-list rỗng thì quản mọi KB của đơn vị, KB mới tự nhiên nằm trong phạm vi. Ghi nội dung KB (tài liệu/
	// đoạn/FAQ/Tag/Wiki) do capability ingest của các route con tương ứng kiểm soát, không thuộc nhóm này.
	kbgrp := r.Group("/knowledge-bases")
	kb := g.apiKeyGroup(kbgrp, apiKeyRetrieve(apiKeyFullAccess()))
	kbManagement := kb.With(apiKeyManageKnowledgeBases(apiKeyFullAccess()))
	{
		// Create Knowledge Base — Tenant Admin only (strict: enforced even
		// when the RBAC rollout flag is off); API key cần manage_kbs hoặc full-access.
		// The tenant create flow mints tenant-owned KBs only: a public
		// visibility payload is rejected by the service. Platform-owned
		// public KBs use POST /knowledge-bases/public below.
		kbManagement.POST("", g.TenantAdmin(), handler.CreateKnowledgeBase)
		// Create platform public Knowledge Base — explicit human SuperAdmin
		// only (no tenant context required). Registered on the raw group
		// so no API-key policy is declared: API-key principals stay
		// default-denied and manage_kbs never becomes SuperAdmin. The
		// service re-verifies explicit-human authority for direct callers.
		kbgrp.POST("/public", g.PlatformSuperAdmin(), handler.CreatePublicKnowledgeBase)
		// Platform public catalog discovery — every authenticated human
		// (tenant members and tenantless callers alike); anonymous and
		// API-key principals are denied by the handler + service even
		// though auth lets tenantless sessions reach the route.
		// Registered on the raw group so no API-key policy is declared:
		// visibility is never an implicit key grant (default-deny).
		// No role guard by design (cf. POST /tenants): auth already rejects
		// anonymous and the handler/service admit humans only. (Tenantless
		// sessions normalize to Member, so a Member floor would also pass;
		// omitting it keeps a single human-enforcement layer instead of
		// two that could diverge.)
		kbgrp.GET("/public", handler.ListPublicCatalog)
		// Lấy danh sách kho tri thức — Viewer+ for JWT callers; retrieve-capable API keys pass via the gate.
		kb.GET("", g.Member(), handler.ListKnowledgeBases)
		// Lấy chi tiết kho tri thức — Viewer+ và có quyền read trên KB
		kb.GET("/:id", g.Member(), g.KBAccessRead("id"), handler.GetKnowledgeBase)
		// Cập nhật / xoá kho tri thức — hai lớp kiểm quyền trực giao, thiếu một là không đủ:
		//   OwnedKBOrAdmin  quản quyền sở hữu "trong đơn vị": Contributor không phải người tạo không sửa được
		//                   KB của đồng nghiệp (KB khác đơn vị ở đây đi lookup=NotFound → giao cho
		//                   hạ nguồn xử lý, không chặn ở đây).
		//   KBAccessWrite   quản mức truy cập "liên đơn vị": KB của mình hoặc được tổ chức chia sẻ (editor).
		// Trong handler còn xét permission/đơn vị sở hữu để quyết định cuối — nhất là DeleteKnowledgeBase
		// kiểm tra kb.TenantID theo đơn vị "của chính" người gọi (c.Keys, chưa bị KBAccess ghi đè),
		// khoá chặt xoá thành "đơn vị sở hữu + Admin"; editor được chia sẻ không xoá được KB nguồn.
		// Update /Delete Knowledge Base, visibility scope changes, and
		// profile regeneration share one owner-aware manage guard
		// (KBAccessManage): the owning tenant keeps its TenantAdmin role
		// gate while platform-owned rows admit only an explicit human
		// SuperAdmin. TenantAdmin itself never rejects a SuperAdmin for
		// missing tenant context (it passes on the system-admin flag),
		// and the service re-checks through access.DecideKB (Manage):
		// the owner tenant's Admin or an explicit human SuperAdmin.
		// API-key manage_kbs keeps working for tenant-owned KBs through
		// its declared policy, but never qualifies on public rows.
		kbManagement.PUT("/:id", g.TenantAdmin(), g.KBAccessManage("id"), handler.UpdateKnowledgeBase)
		// Chuyển phạm vi hiển thị — tenant/restricted/published and member_default:
		// owner Tenant Admin or SuperAdmin; public promotion and transfer:
		// explicit human SuperAdmin only (service-enforced).
		kbManagement.PUT("/:id/visibility", g.TenantAdmin(), g.KBAccessManage("id"), handler.UpdateKnowledgeBaseVisibility)
		// Owner transfer — explicit human SuperAdmin only, no tenant context
		// required. Registered on the raw group so no API-key policy is
		// declared: API keys stay default-denied. Revokes every share of
		// the KB in the same transaction (service).
		kbgrp.POST("/:id/transfer-owner", g.PlatformSuperAdmin(), handler.TransferKnowledgeBaseOwner)
		// Sinh lại ngay mô tả AI của kho tri thức — cùng mức kiểm quyền với cập nhật kho tri thức; gọi đồng bộ một lần mô hình nhỏ.
		kbManagement.POST("/:id/profile/generate", g.TenantAdmin(), g.KBAccessManage("id"),
			handler.GenerateKnowledgeBaseProfile)
		kbManagement.DELETE("/:id", g.TenantAdmin(), g.KBAccessManage("id"), handler.DeleteKnowledgeBase)
		// Ghim/bỏ ghim kho tri thức — chính người tạo HOẶC Admin+ và có quyền write trên KB
		// Pin state is now per-(user, kb) (migration 000050). Anyone with
		// at least Viewer-level read access to the KB — including users
		// who reached it via a shared agent — may pin it for themselves;
		// no edit permission is required. The OwnedKBOrAdmin guard was
		// removed accordingly. The route still requires KB read access
		// so callers can't poke at KBs they can't see.
		kb.PUT("/:id/pin", g.Member(), g.KBAccessRead("id"), handler.TogglePinKnowledgeBase)
		// Subscribe to / unsubscribe from a published KB so chat searches
		// it by default. ?scope=tenant (Tenant Admin, checked by the
		// service) covers the whole workspace; the default covers the
		// caller only. Human-only: registered on the raw group so no
		// API-key policy is declared.
		kbgrp.POST("/:id/subscription", g.Member(), g.KBAccessRead("id"), handler.SubscribeKnowledgeBase)
		kbgrp.DELETE("/:id/subscription", g.Member(), handler.UnsubscribeKnowledgeBase)
		// Tìm kiếm lai — Viewer+ và có quyền read trên KB (read-only)
		// POST is preferred; GET with JSON body is kept for backward compatibility (#1727).
		kb.POST("/:id/hybrid-search", g.Member(), g.KBAccessRead("id"), handler.HybridSearch)
		kb.GET("/:id/hybrid-search", g.Member(), g.KBAccessRead("id"), handler.HybridSearch)
		// Sao chép kho tri thức — tạo KB mới, cùng mức với create: JWT Admin+, API key cần manage_kbs hoặc full-access.
		// KB nguồn truyền qua source_id trong body (không phải tham số đường dẫn :id), nên không áp dụng được KBAccessRead
		// dựa trên tham số đường dẫn; việc kiểm tra đơn vị sở hữu và allow-list của KB nguồn/đích làm trong handler
		// (requireTenantAPIKeyKnowledgeBases đưa source_id/target_id vào kiểm tra allow-list).
		// Bản sao thuộc về người gọi, không cần quyền sở hữu KB gốc.
		kbManagement.POST("/copy", g.TenantAdmin(), handler.CopyKnowledgeBase)
		// Tạo bản sao kho tri thức — tạo KB mới, cùng mức với create: JWT TenantAdmin, API key cần manage_kbs hoặc full-access;
		// và có quyền read trên KB nguồn (KBAccessRead chặn KB nguồn với key giới hạn). Chỉ tạo bản ghi cài đặt KB mới, không sao chép nội dung/chỉ mục/chia sẻ.
		kbManagement.POST("/:id/duplicate", g.TenantAdmin(), g.KBAccessRead("id"), handler.DuplicateKnowledgeBase)
		// Lấy tiến độ sao chép kho tri thức — Viewer+; chỉ đọc. manage_kbs (key đã khởi tạo copy) hoặc
		// retrieve đều thăm dò được; tác vụ cách ly theo đơn vị (requireTaskProgressTenant), key chỉ
		// xem được tác vụ của đơn vị mình.
		kb.With(apiKeyRetrieve(apiKeyManageKnowledgeBases(apiKeyFullAccess()))).
			GET("/copy/progress/:task_id", g.Member(), handler.GetKBCloneProgress)
		// Lấy danh sách kho tri thức đích có thể di chuyển tới — Viewer+ và có quyền read trên KB
		kb.GET("/:id/move-targets", g.Member(), g.KBAccessRead("id"), handler.ListMoveTargets)
	}
}

// RegisterKnowledgeBaseActivityRoutes exposes the read-only per-KB activity
// feed. It intentionally stays JWT-only: audit history is a sensitive owner
// surface and no existing workspace API-key capability grants audit access.
func RegisterKnowledgeBaseActivityRoutes(r *gin.RouterGroup, auditHandler *handler.AuditLogHandler, g *rbacGuards) {
	if auditHandler == nil {
		return
	}
	r.GET("/knowledge-bases/:id/activity",
		g.TenantAdmin(), g.KBAccessRead("id"), auditHandler.ListKnowledgeBaseActivity)
}

// RegisterKnowledgeTagRoutes đăng ký các route thẻ của kho tri thức.
//
// Tags are KB metadata: Viewer reads, Contributor writes. Per-KB
// ownership granularity for tags is out of scope for PR 2; this is
// purely role-based.
func RegisterKnowledgeTagRoutes(r *gin.RouterGroup, tagHandler *handler.TagHandler, g *rbacGuards) {
	if tagHandler == nil {
		return
	}
	// Tags là tài nguyên con của KB — tạo/sửa/xoá thẻ làm thay đổi cách phân loại truy xuất
	// nội dung KB, nên phải theo ma trận "creator OR Admin+" của KB, tránh một Contributor
	// không liên quan tạo/xoá thẻ bừa trong KB người khác làm hỏng cách tổ chức nội dung của KB owner.
	kbTags := g.apiKeyGroup(r.Group("/knowledge-bases/:id/tags"), apiKeyIngest(apiKeyFullAccess()))
	kbTagsRead := kbTags.With(apiKeyRetrieve(apiKeyFullAccess()))
	{
		// KBAccessRead/Write resolve own/shared/agent-visible access and
		// rewrite the request's tenant context to the effective tenant
		// for the duration of the handler — so the handler no longer
		// needs its own effectiveCtxForKB helper.
		kbTagsRead.GET("", g.Member(), g.KBAccessRead("id"), tagHandler.ListTags)
		kbTags.POST("", g.TenantAdmin(), g.KBAccessManage("id"), tagHandler.CreateTag)
		kbTags.PUT("/:tag_id", g.TenantAdmin(), g.KBAccessManage("id"), tagHandler.UpdateTag)
		kbTags.DELETE("/:tag_id", g.TenantAdmin(), g.KBAccessManage("id"), tagHandler.DeleteTag)
	}
}

// RegisterWikiPageRoutes registers wiki page related routes.
//
// Wiki pages are KB content (wiki mode): reads are Viewer+ and gated by
// KBAccessRead (own / org-shared / via shared agent), matching FAQ /
// chunk / tag read routes. Content mutations (create/update/delete) and
// maintenance actions (rebuild-links, auto-fix, change issue status)
// honour per-KB ownership via OwnedWikiKBOrAdmin (PR 5, #1303): the URL
// :kb_id resolves directly to the owning KB so a Contributor who owns
// the KB can manage its wiki, while a non-owner Contributor gets 403.
func RegisterWikiPageRoutes(r *gin.RouterGroup, wikiHandler *handler.WikiPageHandler, catalogueHandler *handler.WikiCatalogueHandler, g *rbacGuards) {
	wiki := g.apiKeyGroup(r.Group("/knowledgebase/:kb_id/wiki"), apiKeyIngest(apiKeyFullAccess()))
	wikiRead := wiki.With(apiKeyRetrieve(apiKeyFullAccess()))
	{
		// Page CRUD
		wikiRead.GET("/pages", g.Member(), g.KBAccessRead("kb_id"), wikiHandler.ListPages)
		wiki.POST("/pages", g.TenantAdmin(), g.KBAccessManage("kb_id"), wikiHandler.CreatePage)
		wiki.PUT("/move-page", g.TenantAdmin(), g.KBAccessManage("kb_id"), wikiHandler.MovePage)
		wikiRead.GET("/pages/*slug", g.Member(), g.KBAccessRead("kb_id"), wikiHandler.GetPage)
		wiki.PUT("/pages/*slug", g.TenantAdmin(), g.KBAccessManage("kb_id"), wikiHandler.UpdatePage)
		wiki.DELETE("/pages/*slug", g.TenantAdmin(), g.KBAccessManage("kb_id"), wikiHandler.DeletePage)

		// Revision history (slug is a catch-all like /pages; revert carries
		// the slug in the body for the same reason move-page does)
		wikiRead.GET("/revisions/*slug", g.Member(), g.KBAccessRead("kb_id"), wikiHandler.ListRevisions)
		wiki.POST("/revert", g.TenantAdmin(), g.KBAccessManage("kb_id"), wikiHandler.RevertPage)

		// Folder tree (directory nodes)
		wikiRead.GET("/folders", g.Member(), g.KBAccessRead("kb_id"), wikiHandler.ListFolders)
		wiki.POST("/folders", g.TenantAdmin(), g.KBAccessManage("kb_id"), wikiHandler.CreateFolder)
		wiki.PUT("/folders/:folder_id", g.TenantAdmin(), g.KBAccessManage("kb_id"), wikiHandler.UpdateFolder)
		wiki.DELETE("/folders/:folder_id", g.TenantAdmin(), g.KBAccessManage("kb_id"), wikiHandler.DeleteFolder)

		// Special pages
		wikiRead.GET("/index", g.Member(), g.KBAccessRead("kb_id"), wikiHandler.GetIndex)

		// Graph and stats
		wikiRead.GET("/graph", g.Member(), g.KBAccessRead("kb_id"), wikiHandler.GetGraph)
		wikiRead.GET("/stats", g.Member(), g.KBAccessRead("kb_id"), wikiHandler.GetStats)

		// Search and maintenance
		wikiRead.GET("/search", g.Member(), g.KBAccessRead("kb_id"), wikiHandler.SearchPages)
		wiki.POST("/rebuild-links", g.TenantAdmin(), g.KBAccessManage("kb_id"), wikiHandler.RebuildLinks)
		// Catalogue-mode wiki (code-generated overview pages, no LLM).
		if catalogueHandler != nil {
			wiki.POST("/catalogue/rebuild", g.TenantAdmin(), g.KBAccessManage("kb_id"), catalogueHandler.Rebuild)
			wikiRead.GET("/catalogue/status", g.Member(), g.KBAccessRead("kb_id"), catalogueHandler.Status)
		}
		wikiRead.GET("/lint", g.Member(), g.KBAccessRead("kb_id"), wikiHandler.Lint)
		wiki.POST("/auto-fix", g.TenantAdmin(), g.KBAccessManage("kb_id"), wikiHandler.AutoFix)

		// Issues
		wikiRead.GET("/issues", g.Member(), g.KBAccessRead("kb_id"), wikiHandler.ListIssues)
		wiki.PUT("/issues/:issue_id/status", g.TenantAdmin(), g.KBAccessManage("kb_id"), wikiHandler.UpdateIssueStatus)
	}
}

// RegisterKnowledgeGraphViewRoutes exposes the graphs drawn on a KB page.
// Read-only, for anyone who may read the KB; JWT only, like the activity
// feed: no API-key capability covers graph browsing.
func RegisterKnowledgeGraphViewRoutes(r *gin.RouterGroup, h *handler.KnowledgeGraphViewHandler, g *rbacGuards) {
	if h == nil {
		return
	}
	r.GET("/knowledge-bases/:id/entity-graph", g.Member(), g.KBAccessRead("id"), h.GetEntityGraph)
	r.GET("/knowledge-bases/:id/legal-graph", g.Member(), g.KBAccessRead("id"), h.GetLegalGraph)
}
