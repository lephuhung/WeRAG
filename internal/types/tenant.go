package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/utils"
	"gorm.io/gorm"
)

// retrieverEngineMapping maps RETRIEVE_DRIVER values to retriever engine configurations
var retrieverEngineMapping = map[string][]RetrieverEngineParams{
	"postgres": {
		{RetrieverType: KeywordsRetrieverType, RetrieverEngineType: PostgresRetrieverEngineType},
		{RetrieverType: VectorRetrieverType, RetrieverEngineType: PostgresRetrieverEngineType},
	},
	"sqlite": {
		{RetrieverType: KeywordsRetrieverType, RetrieverEngineType: SQLiteRetrieverEngineType},
		{RetrieverType: VectorRetrieverType, RetrieverEngineType: SQLiteRetrieverEngineType},
	},
}

// GetRetrieverEngineMapping returns the retriever engine mapping
// This allows other packages to access the driver capabilities
func GetRetrieverEngineMapping() map[string][]RetrieverEngineParams {
	return retrieverEngineMapping
}

// GetDefaultRetrieverEngines returns the default retriever engines based on RETRIEVE_DRIVER env
func GetDefaultRetrieverEngines() []RetrieverEngineParams {
	result := []RetrieverEngineParams{}
	seen := make(map[string]bool)

	for _, driver := range strings.Split(os.Getenv("RETRIEVE_DRIVER"), ",") {
		driver = strings.TrimSpace(driver)
		if params, ok := retrieverEngineMapping[driver]; ok {
			for _, p := range params {
				key := string(p.RetrieverType) + ":" + string(p.RetrieverEngineType)
				if !seen[key] {
					seen[key] = true
					result = append(result, p)
				}
			}
		}
	}
	return result
}

// Tenant represents the tenant
type Tenant struct {
	// ID
	ID uint64 `yaml:"id"                  json:"id"                  gorm:"primaryKey"`
	// Name
	Name string `yaml:"name"                json:"name"`
	// Description
	Description string `yaml:"description"         json:"description"`
	// Status
	Status string `yaml:"status"              json:"status"              gorm:"default:'active'"`
	// Retriever engines
	RetrieverEngines RetrieverEngines `yaml:"retriever_engines"   json:"retriever_engines"   gorm:"type:json"`
	// Business
	Business string `yaml:"business"            json:"business"`
	// Kind is TenantKindWorkspace for a real unit, or TenantKindConfigProfile
	// for a hidden configuration-only row that workspaces inherit from.
	Kind string `yaml:"kind"                json:"kind"                gorm:"default:'workspace'"`
	// ConfigProfileID is the configuration profile a workspace inherits its
	// models, agents, engines and settings from. Nil on profiles.
	ConfigProfileID *uint64 `yaml:"config_profile_id"   json:"config_profile_id,omitempty"`
	// DefaultEmbeddingModelID / DefaultSummaryModelID are set on a profile:
	// the models a new knowledge base gets when created without them.
	DefaultEmbeddingModelID string `yaml:"default_embedding_model_id" json:"default_embedding_model_id,omitempty"`
	DefaultSummaryModelID   string `yaml:"default_summary_model_id"   json:"default_summary_model_id,omitempty"`
	// Storage quota (Bytes), default is 10GB, including vector, original file, text, index, etc.
	StorageQuota int64 `yaml:"storage_quota"       json:"storage_quota"       gorm:"default:10737418240"`
	// Storage used (Bytes)
	StorageUsed int64 `yaml:"storage_used"        json:"storage_used"        gorm:"default:0"`
	// Global Context configuration for this workspace (default for all sessions)
	ContextConfig *ContextConfig `yaml:"context_config"      json:"context_config"      gorm:"type:jsonb"`
	// Global WebSearch configuration for this workspace
	WebSearchConfig *WebSearchConfig `yaml:"web_search_config"   json:"web_search_config"   gorm:"type:jsonb"`
	// Parser engine config overrides (OCR endpoint, API key, etc.). Used when parsing documents; overrides env.
	ParserEngineConfig *ParserEngineConfig `yaml:"parser_engine_config" json:"parser_engine_config" gorm:"type:jsonb"`
	// Credentials config: third-party provider credentials (e.g. WeKnoraCloud AppID/AppSecret)
	Credentials *CredentialsConfig `yaml:"credentials" json:"credentials" gorm:"type:jsonb"`
	// Storage engine config: parameters for Local and MinIO. Used for document/file storage and docreader.
	StorageEngineConfig *StorageEngineConfig `yaml:"storage_engine_config" json:"storage_engine_config" gorm:"type:jsonb"`
	// DefaultStorageBackendID is the workspace default concrete storage instance.
	DefaultStorageBackendID *string `yaml:"default_storage_backend_id" json:"default_storage_backend_id,omitempty" gorm:"column:default_storage_backend_id;type:varchar(36)"`
	// Chat history config: knowledge base configuration for indexing and searching chat messages via vector search
	ChatHistoryConfig *ChatHistoryConfig `yaml:"chat_history_config" json:"chat_history_config" gorm:"type:jsonb"`
	// Retrieval config: global search/retrieval parameters shared by knowledge search and message search
	RetrievalConfig *RetrievalConfig `yaml:"retrieval_config" json:"retrieval_config" gorm:"type:jsonb"`
	// Memory config: workspace switch for cross-session long-term memory
	MemoryConfig *MemoryConfig `yaml:"memory_config" json:"memory_config" gorm:"type:jsonb"`
	// API principal config: controls how X-API-Key requests map to terminal principals.
	APIPrincipalConfig *APIPrincipalConfig `yaml:"api_principal_config" json:"-" gorm:"type:jsonb"`
	// Creation time
	CreatedAt time.Time `yaml:"created_at"          json:"created_at"`
	// Last updated time
	UpdatedAt time.Time `yaml:"updated_at"          json:"updated_at"`
	// Deletion time
	DeletedAt gorm.DeletedAt `yaml:"deleted_at"          json:"deleted_at"          gorm:"index"`
}

// Tenant kinds.
// PlatformTenantID is the id of the platform tenant, named "SuperAdmin"
// and seeded by migrations/versioned/000150_platform_tenant_seed. It is
// where SuperAdmins create and feed the platform-wide public knowledge
// bases (laws, decrees, ...) before promoting them to visibility 'public',
// and where the shared built-in model catalog is stored
// (DefaultBuiltinModelTenantID). The tenants_id_seq starts right after it.
// Membership in it grants nothing beyond ordinary tenant rights; platform
// authority still comes from users.is_system_admin alone.
const PlatformTenantID uint64 = 10000

const (
	// TenantKindWorkspace is a real unit: members, knowledge bases, sessions.
	TenantKindWorkspace = "workspace"
	// TenantKindConfigProfile is a configuration profile: a hidden tenant row
	// that only owns shared configuration. It has no members or data and is
	// never listed, joined or switched to.
	TenantKindConfigProfile = "config_profile"
)

// IsConfigProfile reports whether the tenant row is a configuration profile.
func (t *Tenant) IsConfigProfile() bool {
	return t != nil && t.Kind == TenantKindConfigProfile
}

// RetrieverEngines represents the retriever engines for a tenant
type RetrieverEngines struct {
	Engines []RetrieverEngineParams `yaml:"engines" json:"engines" gorm:"type:json"`
}

// GetEffectiveEngines returns the tenant's engines if configured, otherwise returns system defaults
func (t *Tenant) GetEffectiveEngines() []RetrieverEngineParams {
	if len(t.RetrieverEngines.Engines) > 0 {
		return t.RetrieverEngines.Engines
	}
	return GetDefaultRetrieverEngines()
}

// BeforeCreate is a hook function that is called before creating a tenant
func (t *Tenant) BeforeCreate(tx *gorm.DB) error {
	if t.RetrieverEngines.Engines == nil {
		t.RetrieverEngines.Engines = []RetrieverEngineParams{}
	}
	return nil
}

// Value implements the driver.Valuer interface, used to convert RetrieverEngines to database value
func (c RetrieverEngines) Value() (driver.Value, error) {
	return json.Marshal(c)
}

// Scan implements the sql.Scanner interface, used to convert database value to RetrieverEngines.
// It supports both the legacy bare-array format (e.g. [{...}, {...}]) and the current
// object-wrapped format (e.g. {"engines": [{...}, {...}]}).
func (c *RetrieverEngines) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	b, ok := value.([]byte)
	if !ok {
		return nil
	}

	// Try the current object format first: {"engines": [...]}
	if err := json.Unmarshal(b, c); err == nil {
		return nil
	}

	// Fallback: legacy bare-array format: [{...}, {...}]
	var engines []RetrieverEngineParams
	if err := json.Unmarshal(b, &engines); err != nil {
		return fmt.Errorf("retriever_engines: cannot unmarshal as object or array: %w", err)
	}
	c.Engines = engines
	return nil
}

// CredentialsConfig holds third-party provider credentials at the tenant level.
// Stored as a single JSONB column; each provider is a nested object so new
// providers can be added without schema changes.
type CredentialsConfig struct {
	WeKnoraCloud *WeKnoraCloudCredentials `json:"weknoracloud,omitempty"`
}

// WeKnoraCloudCredentials stores WeKnoraCloud AppID and AppSecret.
// AppSecret is AES-256 encrypted before persisting to database.
type WeKnoraCloudCredentials struct {
	AppID     string `json:"app_id"`
	AppSecret string `json:"app_secret"`
}

type APIPrincipalMode string

const (
	APIPrincipalModeTenant      APIPrincipalMode = "tenant"
	APIPrincipalModeDirect      APIPrincipalMode = "direct_header"
	APIPrincipalModeSignedToken APIPrincipalMode = "signed_token"
)

// APIPrincipalConfig controls how tenant API-key requests map to terminal
// principals. Direct header mode is low-assurance and should only be used for
// trusted server-to-server calls; signed-token mode verifies the user claim.
type APIPrincipalConfig struct {
	Mode                  APIPrincipalMode `json:"mode"`
	DirectHeaderName      string           `json:"direct_header_name,omitempty"`
	SignedTokenHeaderName string           `json:"signed_token_header_name,omitempty"`
	// RequireDirectHeader, when true in direct_header mode, rejects API-key
	// requests that omit the configured user-id header instead of falling
	// back to the tenant-level principal.
	RequireDirectHeader bool   `json:"require_direct_header,omitempty"`
	HMACSecret          string `json:"hmac_secret,omitempty"`
}

func (c *APIPrincipalConfig) Value() (driver.Value, error) {
	if c == nil {
		return nil, nil
	}
	cp := *c
	if cp.HMACSecret != "" {
		if key := utils.GetAESKey(); key != nil {
			if encrypted, err := utils.EncryptAESGCM(cp.HMACSecret, key); err == nil {
				cp.HMACSecret = encrypted
			}
		}
	}
	return json.Marshal(&cp)
}

func (c *APIPrincipalConfig) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	b, ok := value.([]byte)
	if !ok {
		return nil
	}
	if err := json.Unmarshal(b, c); err != nil {
		return err
	}
	if plain, ok := utils.DecryptStoredSecretLenient(c.HMACSecret); ok {
		c.HMACSecret = plain
	} else {
		log.Printf("[crypto] tenant api_principal_config.hmac_secret: decrypt failed (SYSTEM_AES_KEY missing/rotated?), treating as unconfigured")
		c.HMACSecret = ""
	}
	return nil
}

// GetWeKnoraCloud returns the WeKnoraCloud credentials, or nil if not configured.
func (c *CredentialsConfig) GetWeKnoraCloud() *WeKnoraCloudCredentials {
	if c == nil || c.WeKnoraCloud == nil {
		return nil
	}
	if c.WeKnoraCloud.AppID == "" || c.WeKnoraCloud.AppSecret == "" {
		return nil
	}
	return c.WeKnoraCloud
}

// Value implements the driver.Valuer interface for CredentialsConfig
func (c *CredentialsConfig) Value() (driver.Value, error) {
	if c == nil {
		return nil, nil
	}
	cp := *c
	if cp.WeKnoraCloud != nil && cp.WeKnoraCloud.AppSecret != "" {
		if key := utils.GetAESKey(); key != nil {
			if encrypted, err := utils.EncryptAESGCM(cp.WeKnoraCloud.AppSecret, key); err == nil {
				cp.WeKnoraCloud = &WeKnoraCloudCredentials{AppID: cp.WeKnoraCloud.AppID, AppSecret: encrypted}
			}
		}
	}
	return json.Marshal(cp)
}

// Scan implements the sql.Scanner interface for CredentialsConfig
func (c *CredentialsConfig) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	b, ok := value.([]byte)
	if !ok {
		return nil
	}
	if err := json.Unmarshal(b, c); err != nil {
		return err
	}
	if c.WeKnoraCloud != nil {
		if plain, ok := utils.DecryptStoredSecretLenient(c.WeKnoraCloud.AppSecret); ok {
			c.WeKnoraCloud.AppSecret = plain
		} else {
			log.Printf("[crypto] tenant credentials we_knora_cloud.app_secret: decrypt failed (SYSTEM_AES_KEY missing/rotated?), treating as unconfigured")
			c.WeKnoraCloud.AppSecret = ""
		}
	}
	return nil
}

// ParserEngineConfig holds tenant-level overrides for document parser engines (e.g. the OCR endpoint).
// These values take precedence over environment variables when parsing documents.
type ParserEngineConfig struct {
	// ChatParserEngineRules selects parser engines for session-scoped chat
	// documents. Knowledge bases keep their own rules in ChunkingConfig.
	ChatParserEngineRules []ParserEngineRule `json:"chat_parser_engine_rules,omitempty"`

	// OpenDataLoader PDF (docreader engine); hybrid requires opendataloader-pdf-hybrid service.
	ODLHybrid           string `json:"odl_hybrid,omitempty"`      // off (default), docling-fast, hancom-ai
	ODLHybridURL        string `json:"odl_hybrid_url,omitempty"`  // e.g. http://odl-hybrid:5002
	ODLHybridMode       string `json:"odl_hybrid_mode,omitempty"` // auto, full
	ODLHybridFallback   *bool  `json:"odl_hybrid_fallback,omitempty"`
	ODLMarkdownWithHTML *bool  `json:"odl_markdown_with_html,omitempty"`

	// PaddleOCR-VL self-hosted pipeline service (full /layout-parsing API),
	// or an OpenAI-compatible VLM OCR endpoint (vLLM /chat/completions)
	// when PaddleOCRVLEndpoint ends in /v1 or PaddleOCRVLAPI is "openai".
	PaddleOCRVLEndpoint string `json:"paddleocr_vl_endpoint,omitempty"` // e.g. http://paddleocr-vl:8080 or http://vllm-ocr:8001/v1
	PaddleOCRVLAPI      string `json:"paddleocr_vl_api,omitempty"`      // auto (default), layout, openai
	PaddleOCRVLModel    string `json:"paddleocr_vl_model,omitempty"`    // openai mode: served model name
	PaddleOCRVLAPIKey   string `json:"paddleocr_vl_api_key,omitempty"`  // openai mode: optional bearer token
	PaddleOCRVLPrompt   string `json:"paddleocr_vl_prompt,omitempty"`   // openai mode: optional OCR prompt
	// openai mode: "1" sends Unlimited-OCR's ngram vllm_xargs +
	// skip_special_tokens=false; default off for SenOCR-Vi / PaddleOCR-VL.
	PaddleOCRVLVllmXargs string `json:"paddleocr_vl_vllm_xargs,omitempty"`
	// openai mode: repetition_penalty for /chat/completions (vLLM extension);
	// default 1.05 breaks degenerate loops, "1"/"0" disables.
	PaddleOCRVLRepetitionPenalty   string `json:"paddleocr_vl_repetition_penalty,omitempty"`
	PaddleOCRVLUseSealRecognition  *bool  `json:"paddleocr_vl_use_seal_recognition,omitempty"`
	PaddleOCRVLUseChartRecognition *bool  `json:"paddleocr_vl_use_chart_recognition,omitempty"`
}

func (c *ParserEngineConfig) ResolveChatParserEngine(fileType string) string {
	if c != nil {
		normalized := normalizeParserFileType(fileType)
		for _, rule := range c.ChatParserEngineRules {
			for _, candidate := range rule.FileTypes {
				if normalizeParserFileType(candidate) == normalized {
					return strings.TrimSpace(rule.Engine)
				}
			}
		}
	}
	return DefaultParserEngine(fileType)
}

// ToOverridesMap returns a map suitable for ParserEngineOverrides in parse requests.
// Keys are snake_case (paddleocr_vl_endpoint, paddleocr_vl_model, etc.).
func (c *ParserEngineConfig) ToOverridesMap() map[string]string {
	if c == nil {
		return nil
	}
	m := make(map[string]string)
	if c.ODLHybrid != "" {
		m["odl_hybrid"] = c.ODLHybrid
	}
	if c.ODLHybridURL != "" {
		m["odl_hybrid_url"] = c.ODLHybridURL
	}
	if c.ODLHybridMode != "" {
		m["odl_hybrid_mode"] = c.ODLHybridMode
	}
	if c.ODLHybridFallback != nil {
		m["odl_hybrid_fallback"] = fmt.Sprintf("%v", *c.ODLHybridFallback)
	}
	if c.ODLMarkdownWithHTML != nil {
		m["odl_markdown_with_html"] = fmt.Sprintf("%v", *c.ODLMarkdownWithHTML)
	}
	if c.PaddleOCRVLEndpoint != "" {
		m["paddleocr_vl_endpoint"] = c.PaddleOCRVLEndpoint
	}
	if c.PaddleOCRVLAPI != "" {
		m["paddleocr_vl_api"] = c.PaddleOCRVLAPI
	}
	if c.PaddleOCRVLModel != "" {
		m["paddleocr_vl_model"] = c.PaddleOCRVLModel
	}
	if c.PaddleOCRVLAPIKey != "" {
		m["paddleocr_vl_api_key"] = c.PaddleOCRVLAPIKey
	}
	if c.PaddleOCRVLPrompt != "" {
		m["paddleocr_vl_prompt"] = c.PaddleOCRVLPrompt
	}
	if c.PaddleOCRVLVllmXargs != "" {
		m["paddleocr_vl_vllm_xargs"] = c.PaddleOCRVLVllmXargs
	}
	if c.PaddleOCRVLRepetitionPenalty != "" {
		m["paddleocr_vl_repetition_penalty"] = c.PaddleOCRVLRepetitionPenalty
	}
	if c.PaddleOCRVLUseSealRecognition != nil {
		m["paddleocr_vl_use_seal_recognition"] = fmt.Sprintf("%v", *c.PaddleOCRVLUseSealRecognition)
	}
	if c.PaddleOCRVLUseChartRecognition != nil {
		m["paddleocr_vl_use_chart_recognition"] = fmt.Sprintf("%v", *c.PaddleOCRVLUseChartRecognition)
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

// Value implements the driver.Valuer interface for ParserEngineConfig
func (c *ParserEngineConfig) Value() (driver.Value, error) {
	if c == nil {
		return nil, nil
	}
	return json.Marshal(c)
}

// Scan implements the sql.Scanner interface for ParserEngineConfig
func (c *ParserEngineConfig) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	b, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(b, c)
}

// StorageEngineConfig holds tenant-level storage engine parameters for Local and MinIO.
// Knowledge bases select which provider to use; parameters are read from here.
type StorageEngineConfig struct {
	DefaultProvider string             `json:"default_provider"` // "local" or "minio"
	Local           *LocalEngineConfig `json:"local,omitempty"`
	MinIO           *MinIOEngineConfig `json:"minio,omitempty"`
}

// LocalEngineConfig is for local file system storage (single-machine deployment only).
type LocalEngineConfig struct {
	PathPrefix string `json:"path_prefix"`
}

// MinIOEngineConfig is for MinIO/S3-compatible object storage.
// Mode "docker" uses env vars for endpoint/credentials; "remote" uses the fields below.
type MinIOEngineConfig struct {
	Mode            string `json:"mode"` // "docker" or "remote"
	Endpoint        string `json:"endpoint"`
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	BucketName      string `json:"bucket_name"`
	UseSSL          bool   `json:"use_ssl"`
	PathPrefix      string `json:"path_prefix"`
}

// Value implements the driver.Valuer interface for StorageEngineConfig
func (c *StorageEngineConfig) Value() (driver.Value, error) {
	if c == nil {
		return nil, nil
	}
	return json.Marshal(c)
}

// Scan implements the sql.Scanner interface for StorageEngineConfig
func (c *StorageEngineConfig) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	b, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(b, c)
}

// TenantSandboxConfig is one named sandbox backend a workspace maintains.
//
// It is self-contained: provider fields are not inherited from process
// environment. Leaving a required provider field empty is rejected on save.
type TenantSandboxConfig struct {
	// SandboxType is cube, e2b, or docker; disabled is the hidden policy row.
	SandboxType string `json:"sandbox_type,omitempty"`

	// ── Cấu hình chung (áp dụng cho mọi backend) ─────────────────────

	// DefaultTimeoutSec is the per-execution timeout in seconds. 0 uses the
	// program's built-in default.
	DefaultTimeoutSec int `json:"default_timeout_sec,omitempty"`

	// TerminalIdleDisconnectSec is how long an interactive terminal or
	// desktop may go without user activity before WeKnora closes the
	// connection so the sandbox can pause on its provider TTL. Terminal
	// counts keystrokes and PTY output; desktop counts mouse and keyboard.
	// 0 uses the built-in default (15 minutes). Not an identity field.
	TerminalIdleDisconnectSec int `json:"terminal_idle_disconnect_sec,omitempty"`

	// DesktopEnabled declares that this config's base template is a desktop
	// image (XFCE + x11vnc + websockify). It is NOT a second template: a
	// config has exactly one boot target, and skill snapshots stack on top of
	// this base generation after generation. Flipping it changes the base, so
	// any installed skills must be rebuilt from the new one.
	DesktopEnabled bool `json:"desktop_enabled,omitempty"`

	// AllowPrivateEndpoints permits this workspace config to reach RFC1918 or
	// loopback cluster endpoints. Link-local/cloud-metadata addresses remain
	// blocked. It is explicit in the UI instead of hidden in process env.
	AllowPrivateEndpoints bool `json:"allow_private_endpoints,omitempty"`

	// EnvVars are additional environment variables injected into every
	// sandbox created for this tenant. 🔒 Values are encrypted at rest.
	// These become visible to all scripts running in the tenant's
	// sandboxes — do not place secrets here that scripts must not access.
	EnvVars map[string]string `json:"env_vars,omitempty"`

	// VolumeMount configures an optional shared volume mounted into every
	// sandbox created for this tenant. Currently used for tenant-installed
	// skills, but the configuration itself is skill-agnostic and can serve
	// any volume-mount use case (shared datasets, pre-installed toolchains,
	// etc.).
	VolumeMount *VolumeMountConfig `json:"volume_mount,omitempty"`

	// SkillImage points at the snapshot that carries this config's installed
	// skills. Empty means "use the base template". Written only by the skill
	// install/remove path: MergeSandboxConfigForUpdate ignores client values
	// so a settings-form save cannot wipe or plant the pointer.
	SkillImage *SkillImageConfig `json:"skill_image,omitempty"`

	// SkillRollout decides whether sessions that already hold a sandbox of
	// this config rebuild after a skill install or removal. Empty and
	// SkillRolloutNextTurn rebuild on the next chat turn. SkillRolloutNewSession
	// leaves those sandboxes on the previous image; only sessions that start
	// afterwards boot the new snapshot.
	SkillRollout string `json:"skill_rollout,omitempty"`

	// Network is the outbound/inbound network policy applied to every sandbox
	// created from this config — chat sessions, skill installs and deep
	// connectivity probes alike. nil and the zero value mean the same thing:
	// outbound egress allowed, inbound public access closed.
	Network *SandboxNetworkPolicy `json:"network,omitempty"`

	// ── Cấu hình riêng từng backend (chỉ một cái có hiệu lực, do SandboxType quyết định) ───

	Cube   *CubeSandboxConfig   `json:"cube,omitempty"`
	E2B    *E2BSandboxConfig    `json:"e2b,omitempty"`
	Docker *DockerSandboxConfig `json:"docker,omitempty"`
}

// CubeSandboxConfig addresses one CubeSandbox deployment. APIURL, ProxyURL,
// SandboxDomain and TemplateID are all required; APIKey is optional because the
// common single-node setup runs unauthenticated.
type CubeSandboxConfig struct {
	APIURL        string `json:"api_url,omitempty"`
	ProxyURL      string `json:"proxy_url,omitempty"`
	SandboxDomain string `json:"sandbox_domain,omitempty"`
	APIKey        string `json:"api_key,omitempty"` // Đã mã hóa
	TemplateID    string `json:"template_id,omitempty"`

	// HTTPTimeoutSec bounds each HTTP call to the sandbox control plane.
	// 0 means use the built-in default (30s), never the deployment's value.
	HTTPTimeoutSec int `json:"http_timeout_sec,omitempty"`

	CubeSandboxTTLSeconds int `json:"cube_sandbox_ttl_seconds,omitempty"`

	// DNSServers are Cube template nameserver IPs. Empty uses Cubelet's default.
	DNSServers []string `json:"dns_servers,omitempty"`
}

// E2BSandboxConfig addresses one E2B-protocol control plane: E2B Cloud, a
// self-hosted E2B Infrastructure, or any E2B-compatible implementation
// (CubeSandbox, Agent-Sandbox, …). APIKey and TemplateID are required; APIURL
// and SandboxDomain are optional because go-e2b resolves both on its own when
// they are empty.
type E2BSandboxConfig struct {
	APIURL        string `json:"api_url,omitempty"`
	SandboxDomain string `json:"sandbox_domain,omitempty"`
	APIKey        string `json:"api_key,omitempty"` // Đã mã hóa
	TemplateID    string `json:"template_id,omitempty"`

	// ProxyURL is the data-plane gateway that fronts envd. E2B Cloud resolves
	// "<port>-<sandboxID>.<sandbox_domain>" through public DNS and TLS, so it
	// needs no value here. Self-hosted E2B-compatible control planes usually
	// serve every sandbox from one gateway address and expect the sandbox
	// authority in the Host header; setting this makes WeKnora dial the
	// gateway directly instead of requiring wildcard DNS and a certificate
	// for the sandbox domain. An "http://" gateway also downgrades the
	// data-plane scheme, which the E2B SDK otherwise pins to https.
	ProxyURL string `json:"proxy_url,omitempty"`

	// HTTPTimeoutSec bounds each HTTP call to the sandbox control plane.
	// 0 means use the built-in default (30s), never the deployment's value.
	HTTPTimeoutSec int `json:"http_timeout_sec,omitempty"`

	E2BSandboxTTLSeconds int `json:"e2b_sandbox_ttl_seconds,omitempty"`
}

// DockerSandboxConfig addresses one Docker daemon. Image is required and plays
// the role a template ID plays for the MicroVM backends: every session
// container is created from it.
//
// The daemon endpoint is deliberately the only connection field, and TLS
// material is referenced by path rather than stored here. Client certificates
// are deployment infrastructure mounted onto the WeKnora host; keeping them
// out of the database keeps them out of backups, exports and API responses.
type DockerSandboxConfig struct {
	Image string `json:"image,omitempty"`

	// Host is the daemon endpoint in DOCKER_HOST form. Empty means the local
	// unix socket.
	Host string `json:"host,omitempty"`

	// TLSCertPath is a directory on the WeKnora host containing ca.pem,
	// cert.pem and key.pem. Required when Host is a TCP endpoint.
	TLSCertPath string `json:"tls_cert_path,omitempty"`

	// CPULimit is the number of CPU cores one sandbox may use. 0 uses the
	// built-in default.
	CPULimit float64 `json:"cpu_limit,omitempty"`

	// MemoryLimitMB caps one sandbox's memory. 0 uses the built-in default.
	MemoryLimitMB int `json:"memory_limit_mb,omitempty"`

	// PidsLimit caps how many processes one sandbox may run. 0 uses the
	// built-in default.
	PidsLimit int `json:"pids_limit,omitempty"`

	// NetworkMode is the Docker network sandboxes join: "bridge" (default) or
	// "none" for no egress. Nothing else is accepted — host and container:
	// modes share another namespace outright, and a named network is usually
	// the deployment's own compose network, which would put the sandbox next
	// to Postgres and Redis.
	NetworkMode string `json:"network_mode,omitempty"`

	// Runtime selects an alternative OCI runtime such as "runsc" (gVisor).
	// Empty uses the daemon default.
	Runtime string `json:"runtime,omitempty"`

	// IdleTTLSeconds is how long a session container may go unused before it
	// is reclaimed. The daemon has no idle timeout of its own, so this is what
	// stops an abandoned session from pinning host memory indefinitely.
	IdleTTLSeconds int `json:"idle_ttl_seconds,omitempty"`

	// HTTPTimeoutSec bounds each Engine API call. 0 uses the built-in default.
	HTTPTimeoutSec int `json:"http_timeout_sec,omitempty"`
}

// VolumeMountConfig configures a shared volume mount into every sandbox
// created for this tenant. Currently implemented for E2B volumes (used to
// share tenant-installed skills across sandbox sessions), but the schema is
// intentionally skill-agnostic so it can serve other volume-based use cases
// in the future.
type VolumeMountConfig struct {
	// Enabled toggles the volume mount for this tenant.
	Enabled bool `json:"enabled"`

	// MountPath is the sandbox-internal path where the volume is mounted.
	// Default: /weknora/tenant/skills (customizable per use case).
	MountPath string `json:"mount_path,omitempty"`

	// Provider identifies the volume backend. Currently "e2b" or "cube".
	Provider string `json:"provider,omitempty"`

	// VolumeID is the provider-specific volume identifier, populated after
	// EnsureVolume / CreateVolume succeeds.
	VolumeID string `json:"volume_id,omitempty"`

	// VolumeName is the human-readable volume name, e.g.
	// "weknora-tenant-<id>-skills".
	VolumeName string `json:"volume_name,omitempty"`

	// VolumeOwnerFingerprint = sha256(provider + APIKey + APIURL).
	// Used to detect when the tenant switched to a different backend or
	// API key, at which point the volume is no longer reachable and must
	// be recreated.
	VolumeOwnerFingerprint string `json:"volume_owner_fingerprint,omitempty"`
}

const (
	// SkillRolloutNextTurn rebuilds a live session's sandbox on its next chat
	// turn after the skill image changes. This is the default.
	SkillRolloutNextTurn = "next_turn"
	// SkillRolloutNewSession leaves live sandboxes on the previous image.
	// Only a session that starts after the install or removal boots the new one.
	SkillRolloutNewSession = "new_session"
)

// RebuildsExistingOnSkillChange reports whether an install or removal should
// mark already-bound sandboxes stale. Unknown values fail toward rebuilding
// so a corrupted row cannot pin every session on a retired image.
func (c *TenantSandboxConfig) RebuildsExistingOnSkillChange() bool {
	if c == nil {
		return true
	}
	return strings.TrimSpace(c.SkillRollout) != SkillRolloutNewSession
}

// SkillImageConfig is the pointer to the snapshot that carries the skills
// installed on this sandbox config. Snapshot IDs double as template IDs, so
// nothing else is needed to boot sessions from it.
//
// It holds no secrets, so it is not encrypted by TenantSandboxConfig.Value.
type SkillImageConfig struct {
	// SnapshotID is the currently effective snapshot; empty = base template.
	SnapshotID string `json:"snapshot_id,omitempty"`
	// Generation increments on every successful install/remove, for naming
	// and troubleshooting.
	Generation int `json:"generation,omitempty"`
	// BuiltAt records when this generation was produced.
	BuiltAt time.Time `json:"built_at,omitempty"`
	// BaseTemplateID is the template this chain was originally built from;
	// the rebuild path starts over from it.
	BaseTemplateID string `json:"base_template_id,omitempty"`
	// OwnerFingerprint identifies the provider account that owns the snapshot.
	// Snapshots are invisible across accounts, so a mismatch means "fall back
	// to the base template" rather than "fail".
	OwnerFingerprint string `json:"owner_fingerprint,omitempty"`
}

// Value implements the driver.Valuer interface. Every secret-bearing field
// (Cube.APIKey, E2B.APIKey, all EnvVars values, and injected header values) is
// encrypted before persisting. EnvVars are included because environment
// variables routinely carry credentials, and their values are handed to tenant
// scripts verbatim. The receiver is never mutated: nested structs and the map
// are copied first.
func (c *TenantSandboxConfig) Value() (driver.Value, error) {
	if c == nil {
		return nil, nil
	}
	cp := *c
	key := utils.GetAESKey()

	encrypt := func(plain string) string {
		if plain == "" || key == nil {
			return plain
		}
		encrypted, err := utils.EncryptAESGCM(plain, key)
		if err != nil {
			return plain
		}
		return encrypted
	}

	if c.Cube != nil {
		cube := *c.Cube
		cube.APIKey = encrypt(cube.APIKey)
		cp.Cube = &cube
	}
	if c.E2B != nil {
		e2b := *c.E2B
		e2b.APIKey = encrypt(e2b.APIKey)
		cp.E2B = &e2b
	}
	// Keys stay readable so operators can still see which variables are set.
	if len(c.EnvVars) > 0 {
		envVars := make(map[string]string, len(c.EnvVars))
		for name, value := range c.EnvVars {
			envVars[name] = encrypt(value)
		}
		cp.EnvVars = envVars
	}
	// Injected headers are the sandbox-side way to call an API without the
	// credential ever entering the sandbox, so their values are secrets.
	if c.Network != nil {
		cp.Network = c.Network.CloneWithSecrets(encrypt)
	}

	return json.Marshal(&cp)
}

// Scan implements the sql.Scanner interface. Secrets that cannot be decrypted
// (missing or rotated SYSTEM_AES_KEY) are blanked and logged rather than
// failing the whole load, matching ModelParameters.Scan — a tenant must stay
// listable even if its sandbox credentials became unreadable.
func (c *TenantSandboxConfig) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	b, ok := value.([]byte)
	if !ok {
		return nil
	}
	if err := json.Unmarshal(b, c); err != nil {
		return err
	}

	decrypt := func(stored, label string) string {
		if stored == "" {
			return ""
		}
		if plain, ok := utils.DecryptStoredSecretLenient(stored); ok {
			return plain
		}
		log.Printf("[crypto] tenant_sandbox_config.%s: decrypt failed "+
			"(SYSTEM_AES_KEY missing/rotated?), treating as unconfigured", label)
		return ""
	}

	if c.Cube != nil {
		c.Cube.APIKey = decrypt(c.Cube.APIKey, "cube.api_key")
	}
	if c.E2B != nil {
		c.E2B.APIKey = decrypt(c.E2B.APIKey, "e2b.api_key")
	}
	for name, stored := range c.EnvVars {
		c.EnvVars[name] = decrypt(stored, "env_vars."+name)
	}
	if c.Network != nil {
		// CloneWithSecrets has no rule/header context, so rotated-key failures
		// share one label rather than identifying the individual credential.
		c.Network = c.Network.CloneWithSecrets(func(stored string) string {
			return decrypt(stored, "network.injected_header")
		})
	}

	return nil
}
