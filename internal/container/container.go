// Package container implements dependency injection container setup
// Provides centralized configuration for services, repositories, and handlers
// This package is responsible for wiring up all dependencies and ensuring proper lifecycle management
package container

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/neo4j/neo4j-go-driver/v6/neo4j"
	"github.com/panjf2000/ants/v2"
	"github.com/redis/go-redis/v9"
	"go.uber.org/dig"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	domainpackassets "github.com/Tencent/WeKnora/domain-packs"
	"github.com/Tencent/WeKnora/internal/agent/approval"
	"github.com/Tencent/WeKnora/internal/agent/domainpacks"
	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/application/repository"
	neo4jRepo "github.com/Tencent/WeKnora/internal/application/repository/retriever/neo4j"
	postgresRepo "github.com/Tencent/WeKnora/internal/application/repository/retriever/postgres"
	"github.com/Tencent/WeKnora/internal/application/service"
	chatpipeline "github.com/Tencent/WeKnora/internal/application/service/chat_pipeline"
	"github.com/Tencent/WeKnora/internal/application/service/file"
	"github.com/Tencent/WeKnora/internal/application/service/memory"
	"github.com/Tencent/WeKnora/internal/application/service/retriever"
	"github.com/Tencent/WeKnora/internal/browserskill"
	"github.com/Tencent/WeKnora/internal/common"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/database"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/handler/session"
	imPkg "github.com/Tencent/WeKnora/internal/im"
	"github.com/Tencent/WeKnora/internal/im/dingtalk"
	"github.com/Tencent/WeKnora/internal/im/feishu"
	"github.com/Tencent/WeKnora/internal/im/mattermost"
	"github.com/Tencent/WeKnora/internal/im/qqbot"
	"github.com/Tencent/WeKnora/internal/im/slack"
	"github.com/Tencent/WeKnora/internal/im/telegram"
	"github.com/Tencent/WeKnora/internal/im/wechat"
	"github.com/Tencent/WeKnora/internal/im/wecom"
	"github.com/Tencent/WeKnora/internal/im/yunzhijia"
	"github.com/Tencent/WeKnora/internal/infrastructure/docparser"
	infra_web_search "github.com/Tencent/WeKnora/internal/infrastructure/web_search"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/mcp"
	"github.com/Tencent/WeKnora/internal/mcpserver"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/models/limiter"
	"github.com/Tencent/WeKnora/internal/models/utils/ollama"
	"github.com/Tencent/WeKnora/internal/router"
	"github.com/Tencent/WeKnora/internal/sandbox"
	"github.com/Tencent/WeKnora/internal/storageallowlist"
	"github.com/Tencent/WeKnora/internal/stream"
	"github.com/Tencent/WeKnora/internal/tracing/langfuse"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal/people"
	"github.com/hibiken/asynq"
)

// BuildContainer constructs the dependency injection container
// Registers all components, services, repositories and handlers needed by the application
// Creates a fully configured application container with proper dependency resolution
// Parameters:
//   - container: Base dig container to add dependencies to
//
// Returns:
//   - Configured container with all application dependencies registered
func BuildContainer(container *dig.Container) *dig.Container {
	ctx := context.Background()
	logger.Debugf(ctx, "[Container] Starting container initialization...")

	// Register resource cleaner for proper cleanup of resources
	must(container.Provide(NewResourceCleaner, dig.As(new(interfaces.ResourceCleaner))))

	// Core infrastructure configuration
	logger.Debugf(ctx, "[Container] Registering core infrastructure...")
	must(container.Provide(config.LoadConfig))
	must(container.Provide(initLangfuse))
	must(container.Provide(initDatabase))
	must(container.Provide(initFileService))
	must(container.Provide(initRedisClient))
	must(container.Provide(initAntsPool))

	must(container.Invoke(registerLangfuseCleanup))

	// Register goroutine pool cleanup handler
	must(container.Invoke(registerPoolCleanup))

	// Initialize retrieval engine registry for search capabilities
	logger.Debugf(ctx, "[Container] Registering retrieval engine registry...")
	must(container.Provide(initRetrieveEngineRegistry))

	// External service clients
	logger.Debugf(ctx, "[Container] Registering external service clients...")
	must(container.Provide(initDocReaderClient))
	must(container.Provide(docparser.NewImageResolver))
	must(container.Provide(initOllamaService))
	must(container.Provide(initNeo4jClient))
	must(container.Provide(stream.NewStreamManager))
	logger.Debugf(ctx, "[Container] Initializing DuckDB...")
	must(container.Provide(NewDuckDB))
	logger.Debugf(ctx, "[Container] DuckDB registered")

	// Data repositories layer
	logger.Debugf(ctx, "[Container] Registering repositories...")
	must(container.Provide(repository.NewTenantRepository))
	must(container.Provide(repository.NewTenantAPIKeyRepository))
	must(container.Provide(repository.NewTenantMemberRepository))
	must(container.Provide(repository.NewTenantInvitationRepository))
	must(container.Provide(repository.NewKBAccessGrantRepository))
	must(container.Provide(repository.NewKBInvitationRepository))
	must(container.Provide(repository.NewKBSubscriptionRepository))
	must(container.Provide(repository.NewAuditLogRepository))
	must(container.Provide(repository.NewKnowledgeBaseRepository))
	must(container.Provide(repository.NewKnowledgeRepository))
	must(container.Provide(repository.NewKnowledgeSpanRepository))
	must(container.Provide(repository.NewChunkRepository))
	must(container.Provide(repository.NewKnowledgeTagRepository))
	must(container.Provide(repository.NewSessionRepository))
	must(container.Provide(repository.NewMessageRepository))
	must(container.Provide(repository.NewUsageCounterRepository))
	must(container.Provide(repository.NewUsagePlanRepository))
	must(container.Provide(repository.NewUsageBillingRepository))
	must(container.Provide(repository.NewMessageSuggestionRepository))
	must(container.Provide(repository.NewModelRepository))
	must(container.Provide(repository.NewUserRepository))
	must(container.Provide(repository.NewAuthTokenRepository))
	must(container.Provide(repository.NewSystemSettingRepository))
	must(container.Provide(neo4jRepo.NewNeo4jRepository))
	must(container.Provide(repository.NewMCPServiceRepository))
	must(container.Provide(repository.NewMCPToolApprovalRepository))
	must(container.Provide(repository.NewMCPOAuthRepository))
	must(container.Provide(repository.NewTenantSandboxConfigRepository))
	must(container.Provide(repository.NewTenantSkillRepository))
	must(container.Provide(repository.NewCustomAgentRepository))
	must(container.Provide(repository.NewEmbedChannelRepository))
	must(container.Provide(repository.NewMCPEndpointRepository))
	must(container.Provide(repository.NewUserResourceFavoriteRepository))
	must(container.Provide(service.NewWebSearchStateService))
	must(container.Provide(repository.NewWikiPageRepository))
	must(container.Provide(repository.NewMemoryRepository))
	must(container.Provide(repository.NewTaskPendingOpsRepository))
	must(container.Provide(repository.NewTaskDeadLetterRepository))
	must(container.Provide(repository.NewAbbreviationRepository))
	// Platform-wide legal catalogue (vbpl.vn metadata + relations) behind
	// the check_legal_status agent tool.
	must(container.Provide(repository.NewLegalCatalogRepository))
	must(container.Provide(repository.NewLegalReviewRepository))
	// Requests to add legal documents users asked about that no KB holds.
	must(container.Provide(repository.NewLegalRequestRepository))
	// OCR drafts of documents vbpl.vn has only as a scanned PDF
	must(container.Provide(repository.NewLegalDraftRepository))
	must(container.Provide(repository.NewAbbreviationTurnRepository))

	// MCP manager for managing MCP client connections
	logger.Debugf(ctx, "[Container] Registering MCP manager...")
	must(container.Provide(mcp.NewMCPManager))
	must(container.Provide(mcp.NewOAuthManager))

	// Sandbox manager fallback is disabled; executable backends are resolved
	// from named workspace configurations.
	logger.Debugf(ctx, "[Container] Registering sandbox manager...")
	must(container.Provide(newSandboxManager))
	// Per-tenant sandbox backends: the resolver builds a manager per request
	// from the tenant's own configuration, falling back to the singleton above
	// for tenants that configured nothing.
	must(container.Provide(service.NewTenantSandboxConfigLoader))
	must(container.Provide(service.NewForkBootstrapperFromRepos))
	must(container.Provide(func(b *service.ForkBootstrapper) sandbox.SessionBootstrapper {
		if b == nil {
			return nil
		}
		return b
	}))
	must(container.Provide(newTenantSandboxResolver))

	// Business service layer
	logger.Debugf(ctx, "[Container] Registering business services...")
	must(container.Provide(service.NewTenantService))
	must(container.Provide(service.NewTenantAPIKeyService))
	must(container.Provide(service.NewTenantMemberService))
	must(container.Provide(service.NewTenantInvitationService))
	must(container.Provide(service.NewAuditLogService))
	must(container.Provide(service.NewAuditLogRetentionRunner))
	must(container.Provide(service.NewKnowledgeBaseService))
	must(container.Provide(service.NewKBAccessGrantService)) // KBAccessGrantService must be registered before KnowledgeService and KnowledgeTagService
	must(container.Provide(func(repo interfaces.KnowledgeBaseRepository) interfaces.KBInviteKBReader {
		return repo
	}))
	must(container.Provide(func(repo interfaces.UserRepository) interfaces.KBInviteUserReader {
		return repo
	}))
	must(container.Provide(func(svc interfaces.TenantMemberService) interfaces.KBInviteMemberReader {
		return svc
	}))
	must(container.Provide(func(audit interfaces.AuditLogService) interfaces.KBInviteAuditWriter {
		return audit
	}))
	must(container.Provide(service.NewKBInvitationService))
	must(container.Provide(service.NewKnowledgeService))
	must(container.Provide(service.NewSpanTracker))
	must(container.Provide(service.NewChunkService))
	must(container.Provide(service.NewKnowledgeTagService))
	// Global Vietnamese abbreviation dictionary (AIRAG port) — feeds both the
	// /abbreviations API and the resolve_abbreviation agent tool.
	must(container.Provide(service.NewAbbreviationService))
	must(container.Provide(service.NewLegalCatalogService))
	must(container.Provide(service.NewLegalReviewService))
	// vbpl.vn ingest shared by the MCP feed and the SuperAdmin request queue
	must(container.Provide(service.NewLegalFeedService))
	must(container.Provide(service.NewLegalRequestService))
	// a scanned PDF is OCRed into a draft the SuperAdmin reviews before ingest
	must(container.Provide(service.NewLegalDraftService))
	must(container.Provide(service.NewLegalDraftServiceInterface))
	// Usage metering: questions and tokens per workspace and person.
	must(container.Provide(service.NewUsageMeter))
	// Usage packages: plans, subscriptions, and the pre-flight guard.
	must(container.Provide(service.NewUsageGuard))
	must(container.Provide(service.NewInputGate))
	must(container.Provide(service.NewUsagePlanService))
	// Self-service purchase: promo codes, orders, bank-transfer settlement.
	must(container.Provide(service.NewUsageBillingService))
	// Platform-wide aggregate stats for the SystemAdmin overview dashboard.
	must(container.Provide(service.NewStatsService))
	// People lookup against the external MongoDB store. Always constructed;
	// the service reports Enabled()=false and the agent tool is never
	// registered when the people_search config section is absent/disabled.
	must(container.Provide(func(cfg *config.Config) *people.Service {
		var pc people.Config
		if cfg.PeopleSearch != nil {
			pc = people.Config{
				Enabled:        cfg.PeopleSearch.Enabled,
				URI:            cfg.PeopleSearch.URI,
				Host:           cfg.PeopleSearch.Host,
				Port:           cfg.PeopleSearch.Port,
				User:           cfg.PeopleSearch.User,
				Password:       cfg.PeopleSearch.Password,
				Database:       cfg.PeopleSearch.Database,
				AuthSource:     cfg.PeopleSearch.AuthSource,
				QueryTimeout:   time.Duration(cfg.PeopleSearch.QueryTimeoutMS) * time.Millisecond,
				PerSchemaLimit: int64(cfg.PeopleSearch.PerSchemaLimit),
			}
		}
		return people.NewService(pc)
	}))
	must(container.Provide(embedding.NewBatchEmbedder))
	must(container.Provide(service.NewModelService))
	must(container.Provide(service.NewAbbreviationMeaningSelector))
	must(container.Provide(service.NewAbbreviationTurnCoordinator))
	must(container.Provide(repository.NewAbbreviationFeedbackRepository))
	must(container.Provide(repository.NewDocumentSpellingFeedbackRepository))
	must(container.Provide(repository.NewDocumentFormatFeedbackRepository))
	must(container.Provide(repository.NewTenantAgencyProfileRepository))
	must(container.Provide(repository.NewTenantTemplateStyleRepository))
	must(container.Provide(service.NewDatasetService))
	must(container.Provide(service.NewEvaluationService))
	must(container.Provide(service.NewUserService))
	must(container.Provide(service.NewSystemSettingService))
	must(container.Provide(func(
		repo repository.TenantSandboxConfigRepository,
		agents interfaces.CustomAgentRepository,
		skills repository.TenantSkillRepository,
		files interfaces.StorageBackendResolver,
	) *service.TenantSandboxConfigService {
		return service.NewTenantSandboxConfigService(repo, agents, buildGlobalSandboxConfig(), skills, files)
	}))
	must(container.Provide(func(s *service.TenantSandboxConfigService) service.WorkspaceSandboxPolicy {
		return s
	}))
	must(container.Provide(service.NewWeKnoraCloudService))

	// Extract services - register individual extracters with names
	must(container.Provide(service.NewChunkExtractService, dig.Name("chunkExtractor")))
	must(container.Provide(service.NewDataTableSummaryService, dig.Name("dataTableSummary")))
	must(container.Provide(service.NewImageMultimodalService, dig.Name("imageMultimodal")))
	must(container.Provide(service.NewKnowledgePostProcessService, dig.Name("knowledgePostProcess")))
	must(container.Provide(service.NewKnowledgeAutoTagService, dig.Name("knowledgeAutoTag")))
	must(container.Provide(service.NewKnowledgeBaseProfileService))
	must(container.Provide(func(s *service.KnowledgeBaseProfileService) interfaces.KnowledgeBaseProfileService {
		return s
	}))
	must(container.Provide(func(s *service.KnowledgeBaseProfileService) interfaces.TaskHandler { return s },
		dig.Name("knowledgeBaseProfile")))

	must(container.Provide(service.NewMessageService))
	must(container.Provide(service.NewMessageSuggestionService))
	must(container.Provide(service.NewMCPServiceService))
	must(container.Provide(service.NewMCPToolApprovalService))
	must(container.Provide(service.NewCustomAgentService))
	must(container.Provide(service.NewUserResourceFavoriteService))
	must(container.Provide(service.NewWikiPageService))
	must(container.Provide(service.NewWikiCatalogueOverviewService))
	must(container.Provide(service.NewWikiIngestService, dig.Name("wikiIngest")))
	must(container.Provide(service.NewWikiLintService))
	must(container.Provide(service.NewEmbedChannelService))
	must(container.Provide(service.NewMCPEndpointService))
	must(container.Provide(mcpserver.NewServer))

	// Web search service (needed by AgentService)
	logger.Debugf(ctx, "[Container] Registering web search registry and providers...")
	must(container.Provide(infra_web_search.NewRegistry))
	must(container.Invoke(registerWebSearchProviders))
	must(container.Provide(repository.NewWebSearchProviderRepository))
	must(container.Provide(repository.NewVectorStoreRepository))
	must(container.Provide(repository.NewStorageBackendRepository))
	must(container.Provide(repository.NewResourceRepository))
	must(container.Provide(repository.NewTemporaryDocumentRepository))
	must(container.Provide(repository.NewDocumentWorkspaceRepository))
	must(container.Provide(repository.NewDocumentRevisionRepository))
	must(container.Provide(service.NewResourceCatalog))
	// TenantStoreOwnership adapter used by the retriever factory functions
	// to verify that a resolved VectorStore belongs to the caller's tenant.
	must(container.Provide(retriever.NewVectorStoreRepoOwnership))
	must(container.Provide(service.NewWebSearchService))
	must(container.Provide(service.NewWebSearchProviderService))
	must(container.Provide(NewEngineFactory))
	// StoreRegistry: same instance as RetrieveEngineRegistry, exposed as StoreRegistry interface.
	// NewRetrieveEngineRegistry always returns *retriever.RetrieveEngineRegistry which implements both.
	must(container.Provide(func(r interfaces.RetrieveEngineRegistry) (interfaces.StoreRegistry, error) {
		sr, ok := r.(*retriever.RetrieveEngineRegistry)
		if !ok {
			return nil, fmt.Errorf("registry does not implement StoreRegistry")
		}
		return sr, nil
	}))
	must(container.Provide(service.NewVectorStoreService))
	must(container.Provide(service.NewStorageBackendServiceWithResources))
	must(container.Provide(func(s *service.StorageBackendService) interfaces.StorageBackendService { return s }))
	must(container.Provide(func(s *service.StorageBackendService) interfaces.StorageBackendResolver { return s }))

	// Agent service layer (requires event bus, web search service)
	// SessionService is passed as parameter to CreateAgentEngine method when creating AgentService
	logger.Debugf(ctx, "[Container] Registering event bus and agent service...")
	must(container.Provide(event.NewEventBus))
	must(container.Provide(service.NewSessionSandboxPinner))
	must(container.Provide(func(cfg *config.Config, s interfaces.MCPToolApprovalService, rdb *redis.Client) *approval.Gate {
		return approval.NewGate(cfg, &approval.Adapter{Svc: s}, rdb)
	}))
	// Expose Gate as MCPApproval interface so AgentService and others can depend on the abstraction.
	must(container.Provide(func(g *approval.Gate) approval.MCPApproval { return g }))
	must(container.Provide(func(cleaner interfaces.ResourceCleaner, db *gorm.DB) (*browserskill.Manager, error) {
		manager := browserskill.NewManager(browserskill.NewStore(db))
		if err := manager.ValidateConfiguration(); err != nil {
			return nil, err
		}
		cleaner.RegisterWithName("BrowserSkill", func() error { manager.Close(); return nil })
		return manager, nil
	}))
	must(container.Provide(service.NewAgentService))

	// Session service (depends on agent service)
	// SessionService is created after AgentService and passes itself to AgentService.CreateAgentEngine when needed
	logger.Debugf(ctx, "[Container] Registering memory service...")
	must(container.Provide(memory.NewMemoryService))

	logger.Debugf(ctx, "[Container] Registering session service...")
	must(container.Provide(service.NewSessionService))
	must(container.Provide(service.NewSessionContextUsageService))
	must(container.Provide(service.NewTenantSkillService))
	// The member-facing half of env vars is its own service because its
	// authority is different in kind: it derives the identity from the context
	// and touches only that identity's rows.
	must(container.Provide(service.NewUserEnvService))

	// ArtifactCollector drains skill-generated files from the sandbox on
	// each agent turn (see spec at
	// docs/superpowers/specs/2026-07-10-skill-artifact-download-design.md).
	// The factory returns nil when the sandbox backend does not support
	// per-session file inspection; downstream code guards on nil.
	must(container.Provide(service.NewArtifactCollectorFromSandboxManager))

	// WorkspaceCheckpointer commits the sandbox /workspace after each agent
	// turn so session fork can roll a forked sandbox back to a given message.
	// The process-wide Manager is DisabledManager, so the runner and ID lookup
	// go through the session pin + per-tenant resolver — the same path
	// ArtifactCollector already uses. Direct Manager type-asserts still win
	// when a deployment injects a SessionBoundManager as the process default.
	must(container.Provide(func(
		mgr sandbox.Manager,
		resolver sandbox.TenantSandboxResolver,
		pinner *service.SessionSandboxPinner,
	) *service.PinnedSessionSandbox {
		return service.NewPinnedSessionSandbox(pinner, resolver, mgr)
	}))
	must(container.Provide(func(
		mgr sandbox.Manager,
		pinned *service.PinnedSessionSandbox,
	) *service.WorkspaceCheckpointer {
		if runner, ok := mgr.(service.SandboxShellRunner); ok {
			return service.NewWorkspaceCheckpointer(runner)
		}
		return service.NewWorkspaceCheckpointer(pinned)
	}))
	must(container.Provide(func(
		mgr sandbox.Manager,
		pinned *service.PinnedSessionSandbox,
	) session.SandboxIDLookup {
		if lookup, ok := mgr.(session.SandboxIDLookup); ok {
			return lookup
		}
		if pinned == nil {
			return nil
		}
		return pinned
	}))
	must(container.Provide(func(
		mgr sandbox.Manager,
		pinned *service.PinnedSessionSandbox,
	) service.SessionForkSandboxPort {
		if port, ok := mgr.(service.SessionForkSandboxPort); ok {
			return port
		}
		if pinned == nil {
			return nil
		}
		return pinned
	}))
	must(container.Provide(service.NewSessionForkServiceFromRepos))

	// SandboxTerminalService opens interactive PTYs on session sandboxes for
	// the frontend terminal panel. First-use provisioning takes a sandbox
	// config ID already resolved by the WebSocket handler (own or shared agent).
	must(container.Provide(service.NewSandboxTerminalService))
	// One-shot desktop handshake tickets. Falls back to an in-process store
	// when Redis is absent (Lite mode), same as the binding store.
	must(container.Provide(service.NewSandboxDesktopTicketStore))
	must(container.Provide(service.NewSandboxDesktopLastStore))
	// Desktop relay. It rides SandboxTerminalService's resolution path so the
	// desktop always lands on the session's existing sandbox.
	must(container.Provide(service.NewSandboxDesktopService))

	logger.Debugf(ctx, "[Container] Registering task enqueuer...")
	redisAvailable := os.Getenv("REDIS_ADDR") != ""
	if redisAvailable {
		must(container.Provide(router.NewAsyncqClient, dig.As(new(interfaces.TaskEnqueuer))))
		// Dedicated pools guarantee capacity for each stage. The shared pool
		// additionally subscribes to core/enrichment queues to provide elastic
		// borrowing while post-process and maintenance remain hard-isolated.
		must(container.Provide(router.NewCoreAsynqServer, dig.Name("coreAsynqServer")))
		must(container.Provide(router.NewPostProcessAsynqServer, dig.Name("postProcessAsynqServer")))
		must(container.Provide(router.NewEnrichmentAsynqServer, dig.Name("enrichmentAsynqServer")))
		must(container.Provide(router.NewMaintenanceAsynqServer, dig.Name("maintenanceAsynqServer")))
		must(container.Provide(router.NewSharedAsynqServer, dig.Name("sharedAsynqServer")))
		must(container.Provide(router.NewWikiAsynqServer, dig.Name("wikiAsynqServer")))
		// Asynq inspector for cancel-by-knowledge-id (best-effort
		// dequeue of pending/scheduled/retry tasks + active-task cancel).
		must(container.Provide(router.NewAsynqInspector))
		must(container.Provide(router.NewAsynqTaskInspector))
		// Wiki generation runs only inside the SuperAdmin's run window
		// (wiki.run_window, overnight by default) so chat keeps the LLM.
		must(container.Invoke(startWikiRunWindow))
		// Install the distributed per-model chat concurrency governor. Only
		// available with Redis (the shared semaphore backend); Lite mode is
		// single-process and low-volume, so it runs ungated.
		must(container.Invoke(registerModelConcurrencyLimiter))
	} else {
		syncExec := router.NewSyncTaskExecutor()
		must(container.Provide(func() interfaces.TaskEnqueuer { return syncExec }))
		must(container.Provide(func() *router.SyncTaskExecutor { return syncExec }))
		// Lite mode: no Redis means no asynq inspector. SyncTaskExecutor
		// dispatches inline goroutines that the checkpoint-based abort
		// already handles.
		must(container.Provide(router.NewNoopTaskInspector))
		// Even without Redis, background ingestion/enrichment can burst the
		// worker pool against one provider, so install an in-process governor.
		must(container.Invoke(registerLiteModelConcurrencyLimiter))
	}
	// Publish the SystemAdmin-owned parse defaults (system_settings key
	// knowledge.parse_defaults) to the parse pipeline so every ingestion
	// path can resolve them at parse time.
	must(container.Invoke(registerParseDefaultsProvider))
	must(container.Invoke(registerPlatformMemoryConfigProvider))
	must(container.Provide(service.NewTemporaryDocumentService))
	// Document assistant: the session's editable .docx and the ONLYOFFICE
	// Document Server bridge (disabled unless ONLYOFFICE_* is configured).
	must(container.Provide(service.NewDocumentWorkspaceService))
	// sub-chats ("chat nhỏ") work on their root chat's documents and uploads
	must(container.Decorate(service.NewSubChatUploads))
	must(container.Decorate(service.NewSubChatWorkspaces))
	must(container.Provide(service.NewDocumentFormatPrecheck))
	// background format-check results and states outlive a restart in Redis
	must(container.Invoke(func(rdb *redis.Client) { tools.UseFormatCheckRedis(rdb) }))
	must(container.Invoke(func(rdb *redis.Client) { tools.UseDocumentTemplateRedis(rdb) }))
	must(container.Invoke(func(rdb *redis.Client) { handler.UseDeviceLoginRedis(rdb) }))
	must(container.Invoke(startTemporaryDocumentCleanup))

	// Chat pipeline components for processing chat requests
	logger.Debugf(ctx, "[Container] Registering chat pipeline plugins...")

	must(container.Invoke(startAuditLogRetention))
	logger.Debugf(ctx, "[Container] Audit log retention runner registered")
	must(container.Provide(service.NewHousekeepingService))
	must(container.Invoke(startHousekeepingService))
	logger.Debugf(ctx, "[Container] Knowledge housekeeping runner registered")
	must(container.Provide(chatpipeline.NewEventManager))
	must(container.Invoke(chatpipeline.NewPluginSearch))
	must(container.Invoke(chatpipeline.NewPluginRerank))
	must(container.Invoke(chatpipeline.NewPluginWebFetch))
	must(container.Invoke(chatpipeline.NewPluginMerge))
	must(container.Invoke(chatpipeline.NewPluginDataAnalysis))
	must(container.Invoke(chatpipeline.NewPluginIntoChatMessage))
	must(container.Invoke(chatpipeline.NewPluginChatCompletion))
	must(container.Invoke(chatpipeline.NewPluginChatCompletionStream))
	must(container.Invoke(chatpipeline.NewPluginFilterTopK))
	must(container.Invoke(chatpipeline.NewPluginQueryUnderstand))
	must(container.Invoke(chatpipeline.NewPluginAbbreviationResolve))
	must(container.Invoke(chatpipeline.NewPluginLoadHistory))
	must(container.Invoke(chatpipeline.NewPluginMemoryRecall))
	must(container.Invoke(chatpipeline.NewPluginExtractEntity))
	must(container.Invoke(chatpipeline.NewPluginSearchEntity))
	must(container.Invoke(chatpipeline.NewPluginSearchParallel))
	must(container.Invoke(chatpipeline.NewPluginWikiBoost))
	must(container.Invoke(chatpipeline.NewPluginMemoryAffinity))
	logger.Debugf(ctx, "[Container] Chat pipeline plugins registered")

	// TenantSkillService is provided next to SessionService (handlers need
	// it), but Invoke constructs the whole chain. SessionService needs
	// *chatpipeline.EventManager, which only exists after the pipeline
	// block above — starting the reaper any earlier panics.
	must(container.Invoke(startTenantSkillReaper))
	logger.Debugf(ctx, "[Container] Tenant skill reaper registered")
	must(container.Provide(func(
		sessions interfaces.SessionRepository,
		resolver sandbox.TenantSandboxResolver,
		mgr sandbox.Manager,
	) *service.ForkSnapshotReaper {
		return service.NewForkSnapshotReaperFromRepos(
			sessions, service.NewResolverForkSnapshotDeleter(resolver, mgr),
		)
	}))
	must(container.Invoke(startForkSnapshotReaper))
	logger.Debugf(ctx, "[Container] Fork snapshot reaper registered")

	// HTTP handlers layer
	logger.Debugf(ctx, "[Container] Registering HTTP handlers...")
	must(container.Provide(handler.NewTenantHandler))
	must(container.Provide(handler.NewTenantMemberHandler))
	must(container.Provide(handler.NewTenantInvitationHandler))
	must(container.Provide(handler.NewKBAccessGrantHandler))
	must(container.Provide(handler.NewKBInvitationHandler))
	must(container.Provide(handler.NewKBAccessOverviewHandler))
	must(container.Provide(handler.NewAuditLogHandler))
	must(container.Provide(handler.NewKnowledgeBaseHandler))
	must(container.Provide(handler.NewKnowledgeHandler))
	must(container.Provide(handler.NewChunkHandler))
	must(container.Provide(handler.NewFAQHandler))
	must(container.Provide(handler.NewTagHandler))
	must(container.Provide(session.NewHandler))
	must(container.Provide(session.NewDocumentWorkspaceHandler))
	must(container.Provide(handler.NewMessageHandler))
	must(container.Provide(handler.NewMessageSuggestionHandler))
	must(container.Provide(handler.NewModelHandler))
	must(container.Provide(handler.NewDocumentFormatHandler))
	must(container.Provide(handler.NewSandboxConfigHandler))
	must(container.Provide(func(
		s *service.TenantSkillService, streams interfaces.StreamManager,
	) *handler.SandboxSkillHandler {
		return handler.NewSandboxSkillHandler(s, streams)
	}))
	must(container.Provide(handler.NewMeEnvVarHandler))
	must(container.Provide(handler.NewMeUsageHandler))
	must(container.Provide(handler.NewUsageAdminHandler))
	must(container.Provide(handler.NewTenantUsageHandler))
	must(container.Provide(handler.NewUsageBillingHandler))
	must(container.Provide(handler.NewEvaluationHandler))
	must(container.Provide(handler.NewInitializationHandler))
	must(container.Provide(handler.NewAuthHandler))
	must(container.Provide(handler.NewSystemHandler))
	must(container.Provide(handler.NewMCPServiceHandler))
	must(container.Provide(handler.NewMCPCredentialsHandler))
	must(container.Provide(handler.NewMCPOAuthHandler))
	must(container.Provide(handler.NewModelCredentialsHandler))
	must(container.Provide(handler.NewWebSearchProviderCredentialsHandler))
	must(container.Provide(handler.NewWebSearchHandler))
	must(container.Provide(handler.NewWebSearchProviderHandler))
	must(container.Provide(handler.NewVectorStoreHandler))
	must(container.Provide(handler.NewStorageBackendHandler))
	// domain packs ship in the binary; an invalid one stops startup, like a
	// broken built-in skill
	must(container.Provide(func() (*domainpacks.Registry, error) {
		registry, err := domainpacks.Load(domainpackassets.FS)
		if err != nil {
			return nil, err
		}
		for _, name := range registry.Names() {
			pack, _ := registry.Get(name)
			for _, finding := range pack.Lint() {
				logger.Warnf(ctx, "[Container] Domain pack %s lint: %s", name, finding)
			}
		}
		return registry, nil
	}))
	must(container.Provide(handler.NewCustomAgentHandler))
	must(container.Provide(handler.NewUserResourceFavoriteHandler))
	must(container.Provide(func(s *service.TenantSkillService) *handler.SkillHandler {
		return handler.NewSkillHandler(s, s)
	}))

	must(container.Provide(handler.NewMemoryHandler))
	must(container.Provide(handler.NewAbbreviationHandler))
	must(container.Provide(handler.NewAgencyProfileHandler))
	must(container.Provide(handler.NewTemplateStyleHandler))
	must(container.Provide(handler.NewLegalReviewHandler))
	must(container.Provide(handler.NewLegalRequestHandler))
	must(container.Provide(handler.NewLegalDraftHandler))
	must(container.Provide(handler.NewKnowledgeGraphViewHandler))
	must(container.Provide(handler.NewStatsHandler))

	// Data source handler
	// Wiki page handler
	must(container.Provide(handler.NewWikiPageHandler))
	must(container.Provide(handler.NewWikiCatalogueHandler))
	// IM integration
	logger.Debugf(ctx, "[Container] Registering IM integration...")
	must(container.Provide(imPkg.NewService))
	must(container.Invoke(registerIMService))
	must(container.Provide(handler.NewIMHandler))
	must(container.Provide(handler.NewEmbedChannelHandler))
	must(container.Provide(handler.NewMCPEndpointHandler))
	must(container.Provide(handler.NewWeKnoraCloudHandler))
	logger.Debugf(ctx, "[Container] HTTP handlers registered")

	// Wire the chat package's local image resolver so multimodal chat can read
	// local:// images that live under a tenant's configured storage PathPrefix
	// (which is not encoded in the local:// URL).
	must(container.Invoke(registerChatLocalImageResolver))

	// Router configuration
	logger.Debugf(ctx, "[Container] Registering router and starting task server...")
	must(container.Provide(router.NewRouter))
	if redisAvailable {
		must(container.Invoke(router.RunAsynqServer))
	} else {
		must(container.Invoke(router.RegisterSyncHandlers))
	}
	// Wiki operation rows are durable, while their wake-up triggers may be
	// lost across a process restart (always in Lite mode, and in Redis mode if
	// persistence succeeded immediately before trigger enqueue failed). Re-arm
	// them only after the matching handlers are ready.
	must(container.Invoke(recoverPendingWikiTasks))

	// session titles and follow-up suggestions use the platform's small
	// auxiliary model (system setting llm.auxiliary_model_id)
	must(container.Invoke(func(settings interfaces.SystemSettingService, sessions interfaces.SessionService,
		suggestions interfaces.MessageSuggestionService) {
		type auxiliaryUser interface {
			SetAuxiliaryModels(interfaces.SystemSettingService)
		}
		for _, svc := range []any{sessions, suggestions} {
			if u, ok := svc.(auxiliaryUser); ok {
				u.SetAuxiliaryModels(settings)
			}
		}
	}))
	// documents drafted from chat take the header from the agency profile
	// and the font sizes from the template style
	must(container.Invoke(func(sessions interfaces.SessionService, profiles interfaces.TenantAgencyProfileRepository,
		styles interfaces.TenantTemplateStyleRepository,
	) {
		type agencyProfileUser interface {
			SetAgencyProfiles(interfaces.TenantAgencyProfileRepository)
			SetTemplateStyles(interfaces.TenantTemplateStyleRepository)
		}
		if u, ok := sessions.(agencyProfileUser); ok {
			u.SetAgencyProfiles(profiles)
			u.SetTemplateStyles(styles)
		}
	}))
	// a question naming a số hiệu no KB holds records a legal document
	// request; the MCP feed lists the queue and closes it when it ingests
	must(container.Invoke(func(sessions interfaces.SessionService, requests interfaces.LegalRequestRepository,
		reviews interfaces.LegalReviewRepository, feed *service.LegalFeedService, mcpServer *mcpserver.Server,
		drafts interfaces.LegalDraftService,
	) {
		type legalRequestUser interface {
			SetLegalRequests(interfaces.LegalRequestRepository, interfaces.LegalReviewRepository)
		}
		if u, ok := sessions.(legalRequestUser); ok {
			u.SetLegalRequests(requests, reviews)
		}
		// an ingest links the document to its catalogue row at once
		feed.Reviews = reviews
		mcpServer.SetLegalFeed(feed, requests)
		mcpServer.SetLegalDrafts(drafts)
	}))
	// users' verdicts on the format check reports
	must(container.Invoke(func(precheck *service.DocumentFormatPrecheck, feedback interfaces.DocumentFormatFeedbackRepository) {
		precheck.SetFormatFeedback(feedback)
	}))
	// the document assistant's spellcheck asks the auxiliary model again
	// about findings on real words and skips what users said is no mistake
	must(container.Invoke(func(settings interfaces.SystemSettingService, agents interfaces.AgentService,
		precheck *service.DocumentFormatPrecheck, feedback interfaces.DocumentSpellingFeedbackRepository) {
		type spellingUser interface {
			SetSpellingSupport(interfaces.SystemSettingService, interfaces.DocumentSpellingFeedbackRepository)
		}
		for _, svc := range []any{agents, precheck} {
			if u, ok := svc.(spellingUser); ok {
				u.SetSpellingSupport(settings, feedback)
			}
		}
	}))
	// the agent, session and custom-agent services read the shipped domain packs
	must(container.Invoke(func(registry *domainpacks.Registry, agents interfaces.AgentService,
		sessions interfaces.SessionService, customAgents interfaces.CustomAgentService) error {
		type domainPackUser interface {
			SetDomainPacks(*domainpacks.Registry)
		}
		for _, svc := range []any{agents, sessions, customAgents} {
			u, ok := svc.(domainPackUser)
			if !ok {
				// a silent skip would leave every agent without its packs
				return fmt.Errorf("%T cannot take the domain packs", svc)
			}
			u.SetDomainPacks(registry)
		}
		logger.Infof(ctx, "[Container] Domain packs shipped: %v", registry.Names())
		return nil
	}))
	// answers to the chat gate's "unclear word" question are kept as
	// training data for the spell-check model
	must(container.Invoke(func(c *service.AbbreviationTurnCoordinator, f interfaces.AbbreviationFeedbackRepository) {
		c.SetFeedbackStore(f)
	}))
	logger.Infof(ctx, "[Container] Container initialization completed successfully")
	return container
}

// registerChatLocalImageResolver wires the chat package's LocalImageResolver
// hook. Stored local:// URLs are relative to the resolved storage base dir and
// do NOT encode the owning tenant's configured PathPrefix, so resolving them to
// disk bytes requires rebuilding the FileService from that tenant's storage
// config. The owning tenant is parsed from the URL's first path segment, which
// correctly handles cross-tenant shared resources (e.g. shared KB images).
func registerChatLocalImageResolver(
	tenantRepo interfaces.TenantRepository,
	storageResolver interfaces.StorageBackendResolver,
	resourceCatalog interfaces.ResourceCatalog,
) {
	chat.LocalImageResolver = func(storageURL string) ([]byte, bool) {
		ctx := context.Background()
		physicalPath, resource, err := resourceCatalog.ResolvePath(ctx, storageURL)
		if err != nil {
			return nil, false
		}
		tenantID := secutils.ParseTenantIDFromStoragePath(physicalPath)
		if resource != nil {
			tenantID = resource.TenantID
		}
		if tenantID == 0 {
			return nil, false
		}
		tenant, err := tenantRepo.GetTenantByID(ctx, tenantID)
		if err != nil || tenant == nil {
			return nil, false
		}
		baseDir := strings.TrimSpace(os.Getenv("LOCAL_STORAGE_BASE_DIR"))
		backendID, inner, scoped := types.ParseStorageBackendPath(physicalPath)
		if resource != nil && resource.StorageBackendID != "" {
			backendID = resource.StorageBackendID
		}
		providerPath := physicalPath
		if scoped {
			providerPath = inner
		}
		provider := types.ParseProviderScheme(providerPath)
		if provider == "" {
			provider = "local"
		}
		fileSvc, _, err := storageResolver.ResolveFileService(ctx, tenant, backendID, provider, baseDir)
		if err != nil {
			return nil, false
		}
		rc, err := fileSvc.GetFile(ctx, physicalPath)
		if err != nil {
			return nil, false
		}
		defer rc.Close()
		data, err := io.ReadAll(rc)
		if err != nil {
			return nil, false
		}
		return data, true
	}
}

// must is a helper function for error handling
// Panics if the error is not nil, useful for configuration steps that must succeed
// Parameters:
//   - err: Error to check
func must(err error) {
	if err != nil {
		panic(err)
	}
}

// initLangfuse initializes the Langfuse ingestion client.
// Configuration is read from LANGFUSE_* environment variables (see
// docs/langfuse.md). Returns a disabled manager if credentials are absent —
// never an error — so deployments that don't use Langfuse are unaffected.
func initLangfuse() (*langfuse.Manager, error) {
	cfg := langfuse.LoadConfigFromEnv()
	return langfuse.Init(cfg)
}

// defaultModelMaxConcurrency is the per-model cap on concurrent background
// (ingestion/enrichment) chat calls when WEKNORA_MODEL_MAX_CONCURRENCY /
// model.max_concurrency is unset. summary / question / graph enrichment all
// share the same model, so this bounds their combined pressure on one provider
// across every replica. Interactive chat is never gated.
const defaultModelMaxConcurrency = 32

// resolveModelMaxConcurrency reads the per-model background concurrency limit
// from system settings / env, defaulting to defaultModelMaxConcurrency when
// unset. A configured value of 0 (or negative) is honoured and disables the
// governor — that is the supported way to turn throttling off via config/env.
func resolveModelMaxConcurrency(ss interfaces.SystemSettingService) int {
	if ss == nil {
		return defaultModelMaxConcurrency
	}
	return int(ss.GetInt(context.Background(), "model.max_concurrency",
		"WEKNORA_MODEL_MAX_CONCURRENCY", int64(defaultModelMaxConcurrency)))
}

// registerModelConcurrencyLimiter builds the Redis-backed per-model background
// concurrency governor (chat + vlm) and installs it. Only available with Redis
// (the shared semaphore backend); Lite mode uses registerLiteModelConcurrencyLimiter.
func registerModelConcurrencyLimiter(rdb *redis.Client, ss interfaces.SystemSettingService) {
	limit := resolveModelMaxConcurrency(ss)
	limiter.SetGovernor(limiter.NewRedisLimiter(rdb), limit)
	if limit <= 0 {
		logger.Infof(context.Background(),
			"[ModelLimiter] background concurrency governor DISABLED (model.max_concurrency<=0)")
		return
	}
	logger.Infof(context.Background(),
		"[ModelLimiter] background model concurrency governed per-model, limit=%d (distributed via redis)", limit)
}

// registerLiteModelConcurrencyLimiter installs an in-process per-model governor
// for Lite mode (no Redis). Lite runs a single process, so an in-process
// semaphore is sufficient to keep a background ingestion storm from bursting
// the whole worker pool against one provider.
func registerLiteModelConcurrencyLimiter(ss interfaces.SystemSettingService) {
	limit := resolveModelMaxConcurrency(ss)
	limiter.SetGovernor(limiter.NewLocalLimiter(), limit)
	if limit <= 0 {
		logger.Infof(context.Background(),
			"[ModelLimiter] background concurrency governor DISABLED (model.max_concurrency<=0)")
		return
	}
	logger.Infof(context.Background(),
		"[ModelLimiter] background model concurrency governed per-model, limit=%d (in-process, lite mode)", limit)
}

// registerParseDefaultsProvider installs the resolver the parse pipeline
// consults for the SystemAdmin-published default parse settings
// (system_settings key knowledge.parse_defaults). Reads go through the
// settings service's cached resolver, so a hot parse path never hits the DB.
// A missing or malformed row yields nil — the feature stays off and uploads
// fall back to per-KB / per-upload configuration.
// registerPlatformMemoryConfigProvider lets the model service see the
// SuperAdmin's platform-wide memory configuration (system setting
// memory.config) when it checks whether a model is still in use.
func registerPlatformMemoryConfigProvider(ss interfaces.SystemSettingService) {
	service.SetPlatformMemoryConfigProvider(func() *types.MemoryConfig {
		raw, ok := ss.GetJSON(context.Background(), types.MemoryConfigSettingKey)
		if !ok || len(raw) == 0 {
			return nil
		}
		cfg, err := types.ParseMemoryConfig(raw)
		if err != nil {
			logger.Warnf(context.Background(), "[memory] cannot decode %q: %v", types.MemoryConfigSettingKey, err)
			return nil
		}
		return cfg
	})
}

func registerParseDefaultsProvider(ss interfaces.SystemSettingService) {
	service.SetParseDefaultsProvider(func() *types.SystemParseDefaults {
		raw, ok := ss.GetJSON(context.Background(), service.SystemParseDefaultsKey)
		if !ok || len(raw) == 0 {
			return nil
		}
		var def types.SystemParseDefaults
		if err := json.Unmarshal(raw, &def); err != nil {
			logger.Warnf(context.Background(),
				"[parse-defaults] cannot decode %q: %v", service.SystemParseDefaultsKey, err)
			return nil
		}
		return &def
	})
}

func initRedisClient() (*redis.Client, error) {
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		logger.Infof(context.Background(), "[Redis] No REDIS_ADDR configured, Redis disabled (Lite mode)")
		return nil, nil
	}
	db, err := strconv.Atoi(os.Getenv("REDIS_DB"))
	if err != nil {
		db = 0
	}

	client := redis.NewClient(&redis.Options{
		Addr:      redisAddr,
		Username:  os.Getenv("REDIS_USERNAME"),
		Password:  os.Getenv("REDIS_PASSWORD"),
		DB:        db,
		TLSConfig: common.RedisTLSConfig(),
	})

	_, err = client.Ping(context.Background()).Result()
	if err != nil {
		return nil, fmt.Errorf("kết nối Redis thất bại: %w", err)
	}

	return client, nil
}

// initDatabase initializes database connection
// Creates and configures database connection based on environment configuration
// Supports multiple database backends (PostgreSQL)
// Parameters:
//   - cfg: Application configuration
//
// Returns:
//   - Configured database connection
//   - Error if connection fails
func initDatabase(cfg *config.Config) (*gorm.DB, error) {
	var dialector gorm.Dialector
	var migrateDSN string
	switch os.Getenv("DB_DRIVER") {
	case "postgres":
		// DSN for GORM (key-value format)
		gormDSN := fmt.Sprintf(
			"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s TimeZone=UTC",
			os.Getenv("DB_HOST"),
			os.Getenv("DB_PORT"),
			os.Getenv("DB_USER"),
			os.Getenv("DB_PASSWORD"),
			os.Getenv("DB_NAME"),
			"disable",
		)
		dialector = postgres.Open(gormDSN)

		// DSN for golang-migrate (URL format)
		// URL-encode password to handle special characters like !@#
		dbPassword := os.Getenv("DB_PASSWORD")
		encodedPassword := url.QueryEscape(dbPassword)

		// Check if postgres is in RETRIEVE_DRIVER to determine skip_embedding
		retrieveDriver := strings.Split(os.Getenv("RETRIEVE_DRIVER"), ",")
		skipEmbedding := "true"
		if slices.Contains(retrieveDriver, "postgres") {
			skipEmbedding = "false"
		}
		logger.Infof(context.Background(), "Skip embedding: %s", skipEmbedding)

		migrateDSN = fmt.Sprintf(
			"postgres://%s:%s@%s:%s/%s?sslmode=disable&options=-c%%20app.skip_embedding=%s",
			os.Getenv("DB_USER"),
			encodedPassword, // Use encoded password
			os.Getenv("DB_HOST"),
			os.Getenv("DB_PORT"),
			os.Getenv("DB_NAME"),
			skipEmbedding,
		)

		// Debug log (don't log password)
		logger.Infof(context.Background(), "DB Config: user=%s host=%s port=%s dbname=%s",
			os.Getenv("DB_USER"),
			os.Getenv("DB_HOST"),
			os.Getenv("DB_PORT"),
			os.Getenv("DB_NAME"),
		)
	default:
		return nil, fmt.Errorf("unsupported database driver: %s", os.Getenv("DB_DRIVER"))
	}
	db, err := gorm.Open(dialector, &gorm.Config{
		NowFunc: func() time.Time {
			return time.Now().UTC()
		},
	})
	if err != nil {
		return nil, err
	}

	// Sanity check: dialect-specific code in services (notably the
	// vector_stores delete guard) compares Dialector.Name() to "postgres".
	// A future driver swap that produces a different name (e.g., a wrapper
	// dialect for managed PG) would silently drop the row-level X-lock.
	// Catching the mismatch at startup is loud and inexpensive.
	if name := db.Dialector.Name(); name != "postgres" {
		return nil, fmt.Errorf(
			"unsupported gorm dialector %q; expected postgres "+
				"(see vectorStoreService.isPostgres for impact)", name)
	}

	// Run database migrations automatically (optional, can be disabled via env var)
	// To disable auto-migration, set AUTO_MIGRATE=false
	// To enable auto-recovery from dirty state, set AUTO_RECOVER_DIRTY=true
	if os.Getenv("AUTO_MIGRATE") != "false" {
		logger.Infof(context.Background(), "Running database migrations...")

		autoRecover := os.Getenv("AUTO_RECOVER_DIRTY") != "false"
		migrationOpts := database.MigrationOptions{
			AutoRecoverDirty: autoRecover,
		}

		// Run base migrations (all versioned migrations including embeddings)
		// The embeddings migration will be conditionally executed based on skip_embedding parameter in DSN
		if err := database.RunMigrationsWithOptions(migrateDSN, migrationOpts); err != nil {
			// Log warning but don't fail startup - migrations might be handled externally
			logger.Warnf(context.Background(), "Database migration failed: %v", err)
			logger.Warnf(
				context.Background(),
				"Continuing with application startup. Please run migrations manually if needed.",
			)
		}

		// Post-migration: resolve __pending_env__ storage provider markers for historical KBs.
		// The SQL migration marks KBs that have documents but no provider with "__pending_env__";
		// we replace that with the actual STORAGE_TYPE from the environment.
		resolveStorageProviderPending(db)
		migrateLegacyStorageBackends(db)

		// Post-migration: declarative built-in models from config/builtin_models.yaml (optional).
		if err := types.LoadBuiltinModelsConfig(context.Background(), db, config.ConfigDir()); err != nil {
			logger.Warnf(context.Background(), "Load builtin models config failed: %v", err)
		}
	} else {
		logger.Infof(context.Background(), "Auto-migration is disabled (AUTO_MIGRATE=false)")
	}

	// Get underlying SQL DB object
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	// Configure connection pool parameters
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetConnMaxLifetime(time.Duration(10) * time.Minute)

	return db, nil
}

// resolveStorageProviderPending replaces the "__pending_env__" sentinel in
// knowledge_bases.storage_provider_config with the actual STORAGE_TYPE from the environment.
// This runs once after SQL migrations to bind historical KBs to their real storage provider.
func resolveStorageProviderPending(db *gorm.DB) {
	storageType := strings.TrimSpace(os.Getenv("STORAGE_TYPE"))
	if storageType == "" {
		storageType = "local"
	}
	storageType = strings.ToLower(storageType)

	result := db.Exec(
		`UPDATE knowledge_bases SET storage_provider_config = ? WHERE storage_provider_config IS NOT NULL AND storage_provider_config->>'provider' = '__pending_env__'`,
		fmt.Sprintf(`{"provider":"%s"}`, storageType),
	)
	if result.Error != nil {
		logger.Warnf(context.Background(), "Failed to resolve __pending_env__ storage providers: %v", result.Error)
	} else if result.RowsAffected > 0 {
		logger.Infof(context.Background(), "Resolved %d knowledge bases with __pending_env__ storage provider → %s", result.RowsAffected, storageType)
	}

	// Sync PostgreSQL sequences with actual MAX values to prevent duplicate key
	// errors. The old code assigned seq_id via SELECT MAX()+1 in application
	// code, which could push values past the DB sequence counter.
	syncSequences(db)

	// Reset any pending tasks left over from previous aborted runs (Lite App mode)
	resetPendingTasks(db)
}

// migrateLegacyStorageBackends backfills the storage_backends table from each
// workspace's legacy StorageEngineConfig (or environment defaults) and binds
// existing knowledge bases to the resulting backend.
//
// The table, columns and indexes are created by the SQL migrations
// (migrations/versioned/000068); this step only handles data that cannot be expressed portably in
// SQL: environment snapshots, JSON→config mapping, AES-encrypted credentials,
// UUID generation and the per-startup refresh of env-backed aliases.
// The migration is idempotent: one legacy_alias row per tenant/provider.
func migrateLegacyStorageBackends(db *gorm.DB) {
	var tenants []*types.Tenant
	if err := db.Find(&tenants).Error; err != nil {
		logger.Warnf(context.Background(), "Failed to load organizations for storage backend migration: %v", err)
		return
	}
	if len(tenants) == 0 {
		return
	}

	// Load every alias in a single query. Probing each tenant/provider pair with
	// First() makes GORM log "record not found" for every miss, which floods the
	// startup log with workspaces × providers lines on fresh installs.
	var aliases []*types.StorageBackend
	if err := db.Where("legacy_alias = ?", true).Find(&aliases).Error; err != nil {
		logger.Warnf(context.Background(), "Failed to load legacy storage aliases: %v", err)
		return
	}
	existingAliases := make(map[uint64]map[string]*types.StorageBackend, len(aliases))
	for _, alias := range aliases {
		byProvider := existingAliases[alias.TenantID]
		if byProvider == nil {
			byProvider = make(map[string]*types.StorageBackend)
			existingAliases[alias.TenantID] = byProvider
		}
		byProvider[alias.Provider] = alias
	}

	for _, tenant := range tenants {
		legacy := tenant.StorageEngineConfig
		defaultProvider := ""
		if legacy != nil {
			defaultProvider = strings.ToLower(strings.TrimSpace(legacy.DefaultProvider))
		}
		if defaultProvider == "" {
			defaultProvider = strings.ToLower(strings.TrimSpace(os.Getenv("STORAGE_TYPE")))
		}
		if defaultProvider == "" {
			defaultProvider = "local"
		}

		backendIDs := make(map[string]string)
		for _, provider := range storageallowlist.Supported() {
			if existing := existingAliases[tenant.ID][provider]; existing != nil {
				// Environment-backed aliases are snapshots, not user-owned config.
				// Refresh them at every startup so credential rotation does not
				// leave the persisted resolver on stale values. If the workspace
				// later gains an explicit legacy config, promote the alias to user
				// source and stop automatic refreshes.
				if existing.Source == types.StorageBackendSourceEnv {
					desired := types.StorageBackendFromLegacy(tenant.ID, provider, legacy)
					if desired == nil && provider == defaultProvider {
						desired = types.StorageBackendFromEnvironment(tenant.ID)
					}
					if desired != nil && desired.Provider == provider {
						_ = db.Model(&types.StorageBackend{}).Where("id = ?", existing.ID).Updates(map[string]interface{}{
							"name": desired.Name, "config": desired.Config, "source": desired.Source, "status": desired.Status, "updated_at": time.Now(),
						}).Error
					}
				}
				backendIDs[provider] = existing.ID
				continue
			}
			backend := types.StorageBackendFromLegacy(tenant.ID, provider, legacy)
			if backend == nil && provider == defaultProvider {
				backend = types.StorageBackendFromEnvironment(tenant.ID)
			}
			if backend == nil {
				continue
			}
			if err := db.Create(backend).Error; err != nil {
				logger.Warnf(context.Background(), "Failed to migrate %s storage for organization %d: %v", provider, tenant.ID, err)
				continue
			}
			backendIDs[provider] = backend.ID
		}
		if tenant.DefaultStorageBackendID == nil {
			if id := backendIDs[defaultProvider]; id != "" {
				if err := db.Model(&types.Tenant{}).Where("id = ?", tenant.ID).Update("default_storage_backend_id", id).Error; err != nil {
					logger.Warnf(context.Background(), "Failed to set default storage backend for organization %d: %v", tenant.ID, err)
				}
			}
		}

		var kbs []*types.KnowledgeBase
		if err := db.Where("tenant_id = ? AND storage_backend_id IS NULL", tenant.ID).Find(&kbs).Error; err != nil {
			continue
		}
		for _, kb := range kbs {
			provider := kb.GetStorageProvider()
			if provider == "" {
				provider = defaultProvider
			}
			if id := backendIDs[provider]; id != "" {
				_ = db.Model(&types.KnowledgeBase{}).Where("id = ? AND storage_backend_id IS NULL", kb.ID).Update("storage_backend_id", id).Error
			}
		}
	}
}

// syncSequences ensures PostgreSQL sequences for auto-increment columns (seq_id)
// are at least as high as the current MAX value in each table. This is needed
// because older code assigned seq_id via application-level MAX()+1, which could
// advance values past the DB sequence counter and cause duplicate key errors.
func syncSequences(db *gorm.DB) {
	if db.Dialector.Name() != "postgres" {
		return
	}
	pairs := [][2]string{
		{"chunks", "chunks_seq_id_seq"},
		{"knowledge_tags", "knowledge_tags_seq_id_seq"},
	}
	for _, p := range pairs {
		table, seq := p[0], p[1]
		sql := fmt.Sprintf(
			`SELECT setval('%s', GREATEST(nextval('%s'), (SELECT COALESCE(MAX(seq_id), 0) FROM %s)))`,
			seq, seq, table,
		)
		if err := db.Exec(sql).Error; err != nil {
			logger.Warnf(context.Background(), "Failed to sync sequence %s: %v", seq, err)
		} else {
			logger.Infof(context.Background(), "Synced sequence %s with table %s", seq, table)
		}
	}
}

// initFileService initializes file storage service
// Creates the appropriate file storage service based on configuration
// Supports the MinIO and local filesystem storage backends
// Parameters:
//   - cfg: Application configuration
//
// Returns:
//   - Configured file service implementation
//   - Error if initialization fails
func initFileService(cfg *config.Config, catalog interfaces.ResourceCatalog) (interfaces.FileService, error) {
	inner, err := initRawFileService(cfg)
	if err != nil {
		return nil, err
	}
	return file.NewResourceCatalogFileService(inner, catalog), nil
}

func initRawFileService(_ *config.Config) (interfaces.FileService, error) {
	storageType := strings.TrimSpace(os.Getenv("STORAGE_TYPE"))
	if storageType == "" {
		storageType = "local"
	}
	switch storageType {
	case "minio":
		if os.Getenv("MINIO_ENDPOINT") == "" ||
			os.Getenv("MINIO_ACCESS_KEY_ID") == "" ||
			os.Getenv("MINIO_SECRET_ACCESS_KEY") == "" ||
			os.Getenv("MINIO_BUCKET_NAME") == "" {
			return nil, fmt.Errorf("missing MinIO configuration")
		}
		return file.NewMinioFileService(
			os.Getenv("MINIO_ENDPOINT"),
			os.Getenv("MINIO_ACCESS_KEY_ID"),
			os.Getenv("MINIO_SECRET_ACCESS_KEY"),
			os.Getenv("MINIO_BUCKET_NAME"),
			strings.EqualFold(os.Getenv("MINIO_USE_SSL"), "true"),
		)
	case "local":
		baseDir := os.Getenv("LOCAL_STORAGE_BASE_DIR")
		if baseDir == "" {
			baseDir = "/data/files"
		}
		externalURL := strings.TrimSpace(os.Getenv("APP_EXTERNAL_URL"))
		return file.NewLocalFileService(baseDir, externalURL), nil
	case "dummy":
		return file.NewDummyFileService(), nil
	default:
		return nil, fmt.Errorf("unsupported storage type: %s", storageType)
	}
}

// initRetrieveEngineRegistry initializes the retrieval engine registry
// Sets up and configures various search engine backends based on configuration
// Registers the Postgres retrieval engine
// Parameters:
//   - db: Database connection
//   - cfg: Application configuration
//
// Returns:
//   - Configured retrieval engine registry
//   - Error if initialization fails
func initRetrieveEngineRegistry(
	db *gorm.DB,
	storeRepo interfaces.VectorStoreRepository, engineFactory interfaces.EngineFactory,
) (interfaces.RetrieveEngineRegistry, error) {
	// storeRepo and engineFactory let the registry rebuild a store engine that
	// is absent from this process, which happens when startup skipped it after
	// a construction failure or when another instance registered it.
	registry := retriever.NewRetrieveEngineRegistry(storeRepo, engineFactory)
	retrieveDriver := strings.Split(os.Getenv("RETRIEVE_DRIVER"), ",")
	log := logger.GetLogger(context.Background())

	if slices.Contains(retrieveDriver, "postgres") {
		postgresRepo := postgresRepo.NewPostgresRetrieveEngineRepository(db)
		if err := registry.Register(
			retriever.NewKVHybridRetrieveEngine(postgresRepo, types.PostgresRetrieverEngineType),
		); err != nil {
			log.Errorf("Register postgres retrieve engine failed: %v", err)
		} else {
			log.Infof("Register postgres retrieve engine success")
		}
	}
	// ─── DB store registration (byStoreID) ───
	if storeReg, ok := registry.(*retriever.RetrieveEngineRegistry); ok {
		loadDBStoresIntoRegistry(storeReg, db)
	}

	return registry, nil
}

// loadDBStoresIntoRegistry loads VectorStore records from DB and registers them
// in the registry's byStoreID map. Failures are logged and skipped (non-fatal).
func loadDBStoresIntoRegistry(
	storeRegistry interfaces.StoreRegistry, db *gorm.DB,
) {
	ctx := context.Background()
	log := logger.GetLogger(ctx)

	var stores []types.VectorStore
	// GORM soft delete automatically adds "deleted_at IS NULL" condition
	if err := db.Find(&stores).Error; err != nil {
		log.Warnf("Failed to load vector stores from DB: %v", err)
		return
	}

	if len(stores) == 0 {
		return
	}

	log.Infof("Loading %d vector store(s) from database", len(stores))
	for _, store := range stores {
		svc, err := createEngineServiceFromStore(ctx, store, db)
		if err != nil {
			log.Errorf("Failed to create engine for store %s (%s): %v", store.ID, store.Name, err)
			continue
		}
		storeRegistry.RegisterWithStoreID(store.ID, svc)
		log.Infof("Registered DB vector store: id=%s, name=%s, engine=%s", store.ID, store.Name, store.EngineType)
	}
}

// initAntsPool initializes the goroutine pool
// Creates a managed goroutine pool for concurrent task execution
// Parameters:
//   - cfg: Application configuration
//
// Returns:
//   - Configured goroutine pool
//   - Error if initialization fails
func initAntsPool(cfg *config.Config) (*ants.Pool, error) {
	// Default to 5 if not specified in config
	poolSize := os.Getenv("CONCURRENCY_POOL_SIZE")
	if poolSize == "" {
		poolSize = "5"
	}
	poolSizeInt, err := strconv.Atoi(poolSize)
	if err != nil {
		return nil, err
	}
	// Set up the pool with pre-allocation for better performance
	return ants.NewPool(poolSizeInt, ants.WithPreAlloc(true))
}

// registerPoolCleanup registers the goroutine pool for cleanup
// Ensures proper cleanup of the goroutine pool when application shuts down
// Parameters:
//   - pool: Goroutine pool
//   - cleaner: Resource cleaner
func registerPoolCleanup(pool *ants.Pool, cleaner interfaces.ResourceCleaner) {
	cleaner.RegisterWithName("AntsPool", func() error {
		pool.Release()
		return nil
	})
}

// registerLangfuseCleanup ensures buffered Langfuse events are flushed on
// shutdown. A 5-second timeout matches other external-service cleanups and
// balances data durability against a slow remote endpoint holding up exit.
func registerLangfuseCleanup(mgr *langfuse.Manager, cleaner interfaces.ResourceCleaner) {
	if mgr == nil {
		return
	}
	cleaner.RegisterWithName("Langfuse", func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return mgr.Shutdown(ctx)
	})
}

// initDocReaderClient initializes the DocumentReader client (lightweight API).
func initDocReaderClient(cfg *config.Config) (interfaces.DocumentReader, error) {
	addr := strings.TrimSpace(os.Getenv("DOCREADER_ADDR"))
	transport := strings.TrimSpace(os.Getenv("DOCREADER_TRANSPORT"))
	if transport == "" {
		transport = "grpc"
	}
	if addr == "" {
		logger.Infof(context.Background(), "[DocConverter] No DOCREADER_ADDR configured, starting disconnected")
	}
	transport = strings.ToLower(transport)
	switch transport {
	case "http", "https":
		if addr != "" && !strings.HasPrefix(addr, "http://") && !strings.HasPrefix(addr, "https://") {
			addr = "http://" + addr
		}
		return docparser.NewHTTPDocumentReader(addr)
	default:
		return docparser.NewGRPCDocumentReader(addr)
	}
}

// initOllamaService initializes the Ollama service client
// Creates a client for interacting with Ollama API for model inference
// Parameters:
//   - None
//
// Returns:
//   - Configured Ollama service client
//   - Error if initialization fails
func initOllamaService() (*ollama.OllamaService, error) {
	// Get Ollama service from existing factory function
	return ollama.GetOllamaService()
}

// initNeo4jClient connects to the graph store. Neo4j is a required service:
// knowledge graph extraction, graph retrieval and the graph view all use it,
// so a missing or unreachable Neo4j stops the app at startup.
func initNeo4jClient() (neo4j.Driver, error) {
	ctx := context.Background()
	uri := strings.TrimSpace(os.Getenv("NEO4J_URI"))
	if uri == "" {
		return nil, fmt.Errorf("NEO4J_URI is not set; Neo4j is required")
	}
	username := os.Getenv("NEO4J_USERNAME")
	password := os.Getenv("NEO4J_PASSWORD")

	// Retry configuration
	maxRetries := 30                 // Max retry attempts
	retryInterval := 2 * time.Second // Wait between retries

	var driver neo4j.Driver
	var err error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		driver, err = neo4j.NewDriver(uri, neo4j.BasicAuth(username, password, ""))
		if err != nil {
			logger.Warnf(ctx, "Failed to create Neo4j driver (attempt %d/%d): %v", attempt, maxRetries, err)
			time.Sleep(retryInterval)
			continue
		}

		err = driver.VerifyAuthentication(ctx, nil)
		if err == nil {
			if attempt > 1 {
				logger.Infof(ctx, "Successfully connected to Neo4j after %d attempts", attempt)
			}
			return driver, nil
		}

		logger.Warnf(ctx, "Failed to verify Neo4j authentication (attempt %d/%d): %v", attempt, maxRetries, err)
		driver.Close(ctx)
		time.Sleep(retryInterval)
	}

	return nil, fmt.Errorf("failed to connect to Neo4j after %d attempts: %w", maxRetries, err)
}

func NewDuckDB() (*sql.DB, error) {
	sqlDB, err := sql.Open("duckdb", ":memory:")
	if err != nil {
		return nil, fmt.Errorf("failed to open duckdb: %w", err)
	}

	// Try to install and load required extensions unless explicitly disabled.
	//   - spatial: used for st_read_meta() to enumerate layer (sheet) names from .xlsx/.xls
	//   - excel:   used for read_xlsx() which gives proper type inference per sheet
	//
	// INSTALL hits extensions.duckdb.org (public internet). In locked-down
	// runtimes with no egress, set DUCKDB_SKIP_EXTENSION_LOAD=1 to avoid a
	// startup hang; xlsx/xls ingest may fail later without these extensions.
	if strings.EqualFold(os.Getenv("DUCKDB_SKIP_EXTENSION_LOAD"), "true") ||
		os.Getenv("DUCKDB_SKIP_EXTENSION_LOAD") == "1" {
		logger.Infof(context.Background(),
			"[DuckDB] Skipping spatial/excel extension install/load "+
				"(DUCKDB_SKIP_EXTENSION_LOAD is set; xlsx ingest may fail without them)")
	} else {
		bgCtx := context.Background()
		for _, ext := range []string{"spatial", "excel"} {
			if _, err := sqlDB.ExecContext(bgCtx, fmt.Sprintf("INSTALL %s;", ext)); err != nil {
				logger.Warnf(bgCtx, "[DuckDB] Failed to install %s extension: %v", ext, err)
			}
			if _, err := sqlDB.ExecContext(bgCtx, fmt.Sprintf("LOAD %s;", ext)); err != nil {
				logger.Warnf(bgCtx, "[DuckDB] Failed to load %s extension: %v", ext, err)
			}
		}
	}

	return sqlDB, nil
}

// registerWebSearchProviders registers all web search provider types to the registry.
// Each provider type is registered with its factory function that accepts parameters.
// Provider instances are created on-demand when tenants configure them.
func registerWebSearchProviders(registry *infra_web_search.Registry) {
	registry.Register("duckduckgo", infra_web_search.NewDuckDuckGoProvider)
	registry.Register("google", infra_web_search.NewGoogleProvider)
	registry.Register("bing", infra_web_search.NewBingProvider)
	registry.Register("tavily", infra_web_search.NewTavilyProvider)
	registry.Register("searxng", infra_web_search.NewSearxngProvider)
	registry.Register("brave", infra_web_search.NewBraveProvider)
}

// registerIMService registers adapter factories, loads enabled channels, and
// wires the process-lifetime shutdown hook. Each platform's factory lives in
// its own subpackage to keep this file focused on wiring.
func registerIMService(imService *imPkg.Service, cleaner interfaces.ResourceCleaner) {
	imService.RegisterAdapterFactory("wecom", wecom.NewFactory())
	imService.RegisterAdapterFactory("feishu", feishu.NewFactory(feishu.RegionFeishu))
	// Lark is Feishu's international cloud: same adapter, different host/tenant.
	imService.RegisterAdapterFactory("lark", feishu.NewFactory(feishu.RegionLark))
	imService.RegisterAdapterFactory("slack", slack.NewFactory())
	imService.RegisterAdapterFactory("telegram", telegram.NewFactory())
	imService.RegisterAdapterFactory("dingtalk", dingtalk.NewFactory())
	imService.RegisterAdapterFactory("mattermost", mattermost.NewFactory())
	imService.RegisterAdapterFactory("wechat", wechat.NewFactory())
	imService.RegisterAdapterFactory("qqbot", qqbot.NewFactory())
	imService.RegisterAdapterFactory("yunzhijia", yunzhijia.NewFactory())

	// Load and start all enabled channels from database
	if err := imService.LoadAndStartChannels(); err != nil {
		logger.Warnf(context.Background(), "[IM] Failed to load channels from database: %v", err)
	}

	cleaner.RegisterWithName("IMService", func() error {
		imService.Stop()
		return nil
	})
}

// startHousekeepingService starts the knowledge housekeeping cron and registers
// cleanup. This is the safety net that recovers any knowledge stuck in
// "processing" past a configurable threshold (see HousekeepingService for
// rationale). Best-effort: a startup error is logged but does NOT abort the
// container — the rest of the system stays usable.
func startHousekeepingService(svc *service.HousekeepingService, cleaner interfaces.ResourceCleaner) {
	if svc == nil {
		return
	}
	if err := svc.Start(context.Background()); err != nil {
		logger.Warnf(context.Background(), "[Container] housekeeping start failed: %v", err)
	}
	cleaner.RegisterWithName("KnowledgeHousekeeping", func() error {
		svc.Stop()
		return nil
	})
}

// startTenantSkillReaper starts the stuck-install / orphan-snapshot cron and
// registers cleanup. Best-effort: a startup error is logged but does NOT abort
// the container — the rest of the system stays usable.
func startTenantSkillReaper(svc *service.TenantSkillService, cleaner interfaces.ResourceCleaner) {
	if svc == nil {
		return
	}
	if err := svc.Start(context.Background()); err != nil {
		logger.Warnf(context.Background(), "[Container] tenant skill reaper start failed: %v", err)
	}
	cleaner.RegisterWithName("TenantSkillReaper", func() error {
		svc.Stop()
		return nil
	})
}

// startForkSnapshotReaper collects snapshots of forks that were never opened.
// Best-effort: a wiring gap is logged but does NOT abort the container.
func startForkSnapshotReaper(reaper *service.ForkSnapshotReaper, cleaner interfaces.ResourceCleaner) {
	if reaper == nil {
		logger.Warnf(context.Background(), "[Container] fork snapshot reaper unavailable")
		return
	}
	if cleaner == nil {
		logger.Warnf(context.Background(), "[Container] fork snapshot reaper start failed: resource cleaner missing")
		return
	}
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if _, err := reaper.ReapOnce(context.Background()); err != nil {
					logger.Warnf(context.Background(), "[ForkSnapshotReaper] reap failed: %v", err)
				}
			case <-stop:
				return
			}
		}
	}()
	cleaner.RegisterWithName("ForkSnapshotReaper", func() error {
		close(stop)
		return nil
	})
}

// startWikiRunWindow pauses the wiki queue outside the configured run window
// and resumes it inside; see router.StartWikiRunWindow.
func startWikiRunWindow(inspector *asynq.Inspector, settings interfaces.SystemSettingService,
	cleaner interfaces.ResourceCleaner,
) {
	stop := router.StartWikiRunWindow(inspector, settings)
	cleaner.RegisterWithName("WikiRunWindow", func() error {
		stop()
		return nil
	})
}

// Idle time after which a session nobody chatted in is removed: a plain chat,
// and one with a document open (kept alive by the editor's syncs). Same
// defaults as purge_expired_temporary_sessions in migration 000153.
const (
	temporarySessionPlainTTL    = time.Hour
	temporarySessionDocumentTTL = 24 * time.Hour
)

// startTemporaryDocumentCleanup removes expired session attachments and their
// extracted images, and the temporary sessions nobody chatted in (the insert
// trigger of migration 000153 purges those too; this covers quiet hours).
// The durable expiry timestamp is the source of truth; the ticker only
// controls how quickly storage is reclaimed.
func startTemporaryDocumentCleanup(
	svc interfaces.TemporaryDocumentService, db *gorm.DB, cleaner interfaces.ResourceCleaner,
) {
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := svc.CleanupExpired(context.Background()); err != nil {
					logger.Warnf(context.Background(), "[TemporaryDocument] cleanup failed: %v", err)
				}
				if n, err := repository.PurgeExpiredTemporarySessions(
					context.Background(), db, temporarySessionPlainTTL, temporarySessionDocumentTTL,
				); err != nil {
					logger.Warnf(context.Background(), "[TemporarySession] purge failed: %v", err)
				} else if n > 0 {
					logger.Infof(context.Background(), "[TemporarySession] purged %d idle temporary sessions", n)
				}
			case <-stop:
				return
			}
		}
	}()
	cleaner.RegisterWithName("TemporaryDocumentCleanup", func() error {
		close(stop)
		return nil
	})
}

// startAuditLogRetention spins up the daily audit_logs purge sweep
// and registers shutdown cleanup. Mirrors the data-source-scheduler
// pattern: container init kicks the goroutine, ResourceCleaner stops
// it during graceful shutdown so a SIGTERM during a sweep doesn't
// orphan the goroutine.
//
// retention_days <= 0 is the configured way to disable retention;
// the runner short-circuits Start() on that path so we don't need
// to gate the wiring here.
func startAuditLogRetention(
	runner *service.AuditLogRetentionRunner, cleaner interfaces.ResourceCleaner,
) {
	runner.Start(context.Background())
	cleaner.RegisterWithName("AuditLogRetentionRunner", func() error {
		runner.Stop()
		return nil
	})
}
