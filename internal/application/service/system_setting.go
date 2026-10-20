package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/limiter"
	"github.com/Tencent/WeKnora/internal/sandbox"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/utils"
)

// pubsubChannelBase is the Redis channel base for system_settings change
// notifications. Mirrors the convention from approval/gate.go: optional
// suffix WEKNORA_REDIS_NAMESPACE so two deployments sharing one Redis
// instance don't cross-talk.
const pubsubChannelBase = "weknora:system_settings:changed"

// SystemParseDefaultsKey is the system_settings key holding the platform-wide
// default parse configuration (types.SystemParseDefaults JSON document).
// Shared by the registry entry, the member-facing read endpoint, and the
// parse-config resolver hook.
const SystemParseDefaultsKey = "knowledge.parse_defaults"

// SystemUIThemeKey is the system_settings key holding the platform-wide UI
// theme family. Read by the public GET /system/ui-theme endpoint so the web
// app and the Word add-in render the same theme, login page included.
const SystemUIThemeKey = "ui.theme"

// pubsubChannel resolves the effective channel name (with optional
// namespace suffix). Called both at publish time and inside the
// subscriber loop — keep it pure.
func pubsubChannel() string {
	if ns := strings.TrimSpace(os.Getenv("WEKNORA_REDIS_NAMESPACE")); ns != "" {
		return pubsubChannelBase + ":" + ns
	}
	return pubsubChannelBase
}

// changeMessage is the JSON payload published whenever a setting is
// updated. OriginID lets the publishing replica skip its own message
// (it already updated its local cache inline) — without it every
// publish would trigger a redundant DB roundtrip per replica.
type changeMessage struct {
	Key      string `json:"key"`
	OriginID string `json:"origin_id"`
}

// settingSpec is the in-code registry entry for a known system setting.
// The registry serves as the **only** authority on which keys are legal
// + what type they hold + what their ENV-fallback name is + what the
// built-in default is. Adding a new tunable is a matter of:
//  1. Adding an entry here.
//  2. (Optional) adding a SQL seed row in a new migration so the UI
//     shows the row even before any operator hits Update.
//  3. Replacing existing os.Getenv() reads with calls into the
//     service.
//
// Update rejects any key not in this registry — so the UI cannot inject
// arbitrary keys into the DB, even with an attacker-controlled body.
type settingSpec struct {
	// Type is one of "int" | "string" | "bool" | "string_list". Update
	// validates the payload's Go type against this; reads decode accordingly.
	Type string
	// EnvName is the legacy environment variable consulted when the DB
	// row is absent. Empty string means "no ENV fallback for this key"
	// (the caller passes the desired default explicitly via the GetXxx
	// def parameter — useful when the cfg already coerced it at startup).
	EnvName string
	// Default is the built-in fallback used when both DB and ENV miss.
	// Type must match the Type field (int → int64, string → string,
	// bool → bool, string_list → []string); the typed Get* methods cast
	// accordingly. Currently unused by the resolver (callers pass def
	// inline) but kept for future cfg-less callsites.
	Default any
	// Enum, when non-empty, restricts Update to values in this set.
	// Only meaningful for Type=="string". Other types ignore it.
	// Empty/nil means no restriction (free-form string).
	Enum []string
	// Category drives UI grouping. Stored on the row at first write;
	// the seed migration sets it explicitly so management UI can
	// render even before any Update.
	Category string
	// Description is shown in the UI under the key. Stored on the row
	// at first write (mirrors Category).
	Description string
	// RequiresRestart marks keys whose value is bound at process startup
	// (e.g. asynq worker pool size). The UI shows a restart badge; the
	// service persists the flag on first write.
	RequiresRestart bool
	// Secret marks keys whose value must not leave the server once saved:
	// List/Get/Update return it masked (see maskSecretSetting) and the
	// change audit carries no value. Only the typed Get* reads see it.
	Secret bool
}

// registry pins the set of legal keys. Expanding it is a deliberate,
// reviewable operation — the implicit contract is "every key here is
// safely runtime-tunable (no startup caching that would not honour
// the new value, no in-memory state bound at init time we cannot
// re-derive)".
var registry = map[string]settingSpec{
	// ui.theme is the platform-wide UI theme family. "elevenlabs" is the
	// built-in design language; "anthropic" is the Anthropic-derived preset.
	// Read by GET /system/ui-theme; applied at <html data-theme>.
	"ui.theme": {
		Type:     "string",
		EnvName:  "WEKNORA_UI_THEME",
		Default:  "elevenlabs",
		Enum:     []string{"elevenlabs", "anthropic"},
		Category: "appearance",
		Description: "Giao diện cấp nền tảng. elevenlabs = ngôn ngữ thiết kế tích hợp; anthropic = preset kiểu Anthropic. " +
			"Có hiệu lực ngay với mọi người dùng sau khi thay đổi, không cần khởi động lại.",
	},
	// NOTE: file.max_size_mb is intentionally NOT registered. Although
	// the Go upload handlers accept a runtime override via
	// systemSettingSvc.GetInt, the actual upload limit is gated end-to-end
	// by three independent layers:
	//   1. nginx client_max_body_size (templated at container startup
	//      from the MAX_FILE_SIZE_MB env var; envsubst writes the
	//      computed value into nginx.conf; nginx is never reloaded
	//      during the container's lifetime).
	//   2. docreader gRPC max_send/recv_message_length (read from the
	//      MAX_FILE_SIZE_MB env at python startup).
	//   3. The frontend client-side check (utils/index.ts) reads
	//      window.__RUNTIME_CONFIG__.MAX_FILE_SIZE_MB which is
	//      written into /usr/share/nginx/html/config.js by the
	//      docker-entrypoint at container start.
	// Surfacing a UI knob whose effect is silently capped by nginx /
	// docreader / the in-page bundle is worse than not having it.
	// Keep MAX_FILE_SIZE_MB as a deploy-time env var until all four
	// layers can be reconfigured in lockstep without restarts.
	"ssrf.whitelist": {
		Type:     "string_list",
		EnvName:  "SSRF_WHITELIST",
		Default:  []string{},
		Category: "security",
		Description: "Danh sách trắng chống SSRF. Có thể nhập example.com / *.foo.com / 10.0.0.0/8 / 2001:db8::1. " +
			"Có hiệu lực ngay sau khi thay đổi. Biến môi trường SSRF_WHITELIST_EXTRA vẫn do bên triển khai quản lý, không bị ghi đè ở đây.",
	},
	"sandbox.docker_enabled": {
		Type:     "bool",
		EnvName:  sandbox.DockerBackendEnabledEnv,
		Default:  false,
		Category: "security",
		Description: "Cho phép backend sandbox Docker hay không. docker.sock cục bộ tương đương root của máy chủ, mặc định tắt. " +
			"Chỉ quản trị hệ thống mới bật được; có hiệu lực ngay, không cần khởi động lại. Chỉ bật khi triển khai đơn máy đã mount daemon socket, hoặc đã cấu hình tcp:// từ xa có TLS.",
	},
	"auth.registration_mode": {
		Type:     "string",
		EnvName:  "", // No env fallback — handler passes cfg.Auth.RegistrationMode as default
		Default:  "self_serve",
		Enum:     []string{"self_serve", "invite_only"},
		Category: "auth",
		Description: "Chế độ tự đăng ký. self_serve = ai cũng có thể đăng ký; invite_only = tắt đăng ký công khai, " +
			"chỉ Owner/Admin mới mời được. Có hiệu lực ngay sau khi thay đổi, nhưng cân nhắc kỹ với self_serve (Internet công khai sẽ nhận spam).",
	},
	"auth.complex_password_enabled": {
		Type:     "bool",
		EnvName:  "WEKNORA_AUTH_COMPLEX_PASSWORD_ENABLED",
		Default:  false,
		Category: "auth",
		Description: "Bật mật khẩu phức tạp hay không. Khi bật, mật khẩu phải có chữ hoa, chữ thường, chữ số và ký tự đặc biệt. " +
			"Có hiệu lực ngay sau khi thay đổi, chỉ ảnh hưởng người dùng đăng ký mới hoặc thao tác đổi/đặt lại mật khẩu. Ký tự đặc biệt gồm: !@#$%^&*()_+-=[]{}|;:,.<>?",
	},
	// tenant.max_owned_per_user caps how many tenants a single non-superuser
	// can create (and Own) via self-service POST /tenants. Read on every
	// request — UI edits take effect immediately, no restart required. The
	// EnvName is the same WEKNORA_TENANT_MAX_OWNED_PER_USER that
	// applyAuthAndTenantDefaults parses at boot, so a deployment that
	// hasn't created a DB row keeps reading from env exactly as before.
	// 0 = use the in-code default (10); negative = disable the cap entirely.
	// limits.* are the input gates: page caps an upload must pass before it
	// is parsed (see types/input_limits.go and input_gate.go). Read on every
	// upload, so a change applies at once. 0 disables the cap.
	types.SettingLimitsChatQuestionMaxRunes: {
		Type: "int", Default: int64(types.DefaultLimitsChatQuestionMaxRunes), Category: "limits",
		Description: "Số ký tự tối đa của một câu hỏi trong chat. 0 = không giới hạn.",
	},
	types.SettingLimitsChatBroadQuestionGate: {
		Type: "bool", Default: true, Category: "limits",
		Description: "Hỏi lại khi câu hỏi quá rộng (vài từ chung như \"luật đất đai\" khớp hàng chục văn bản trong danh mục): đưa thẻ chọn loại văn bản, cơ quan ban hành hoặc vẫn tìm chung.",
	},
	types.SettingLimitsParseMaxConcurrentPerUser: {
		Type: "int", Default: int64(types.DefaultLimitsParseMaxConcurrentPerUser), Category: "limits",
		Description: "Số tài liệu của một người dùng được xử lý (parse, OCR) cùng lúc; tài liệu còn lại chờ trong hàng đợi. 0 = không giới hạn.",
	},
	types.SettingLimitsKBPDFTextPages: {
		Type: "int", Default: int64(types.DefaultLimitsKBPDFTextPages), Category: "limits",
		Description: "Số trang tối đa của một PDF có lớp chữ tải lên kho tri thức. 0 = không giới hạn.",
	},
	types.SettingLimitsKBPDFScanPages: {
		Type: "int", Default: int64(types.DefaultLimitsKBPDFScanPages), Category: "limits",
		Description: "Số trang tối đa của một PDF scan (không có lớp chữ, phải OCR từng trang) tải lên kho tri thức. 0 = không giới hạn.",
	},
	types.SettingLimitsKBDocxPages: {
		Type: "int", Default: int64(types.DefaultLimitsKBDocxPages), Category: "limits",
		Description: "Số trang tối đa của một tệp Word tải lên kho tri thức. 0 = không giới hạn.",
	},
	types.SettingLimitsChatPDFTextPages: {
		Type: "int", Default: int64(types.DefaultLimitsChatPDFTextPages), Category: "limits",
		Description: "Số trang tối đa của một PDF có lớp chữ gửi kèm trong chat hoặc trợ lý văn bản. 0 = không giới hạn.",
	},
	types.SettingLimitsChatPDFScanPages: {
		Type: "int", Default: int64(types.DefaultLimitsChatPDFScanPages), Category: "limits",
		Description: "Số trang tối đa của một PDF scan gửi kèm trong chat hoặc trợ lý văn bản. 0 = không giới hạn.",
	},
	types.SettingLimitsChatDocxPages: {
		Type: "int", Default: int64(types.DefaultLimitsChatDocxPages), Category: "limits",
		Description: "Số trang tối đa của một tệp Word gửi kèm trong chat hoặc trợ lý văn bản. 0 = không giới hạn.",
	},
	"tenant.max_owned_per_user": {
		Type:     "int",
		EnvName:  "WEKNORA_TENANT_MAX_OWNED_PER_USER",
		Default:  int64(10),
		Category: "tenant",
		Description: "Số tổ chức tối đa mỗi người dùng không phải SuperAdmin được tự tạo. Đọc trực tiếp mỗi lần tạo tổ chức, " +
			"có hiệu lực ngay sau khi thay đổi. 0 = dùng mặc định tích hợp 10; số âm = bỏ hoàn toàn giới hạn (không khuyến nghị khi triển khai công khai).",
	},
	"tenant.self_service_creation_enabled": {
		Type:     "bool",
		EnvName:  "WEKNORA_TENANT_SELF_SERVICE_CREATION_ENABLED",
		Default:  true,
		Category: "tenant",
		Description: "Cho phép người dùng không phải SuperAdmin tự tạo tổ chức hay không. Khi tắt, người dùng thường chỉ tham gia tổ chức có sẵn qua lời mời; " +
			"SuperAdmin vẫn tạo được. Có hiệu lực ngay sau khi thay đổi.",
	},
	// tenant.default_storage_quota_gb is the default storage quota (in GB)
	// applied to a newly-created tenant when the caller doesn't specify
	// one explicitly. Read at create time only — changing the value does
	// NOT retroactively resize already-existing tenants (they keep the
	// quota stored on their row at creation; superusers can edit
	// individual tenants via the existing tenant-update path).
	// 0 or negative = use the in-code default (10 GB).
	"tenant.default_storage_quota_gb": {
		Type:     "int",
		EnvName:  "WEKNORA_TENANT_DEFAULT_STORAGE_QUOTA_GB",
		Default:  int64(10),
		Category: "tenant",
		Description: "Hạn mức lưu trữ (GB) cấp mặc định khi tạo tổ chức mới, gồm vector, tệp gốc, văn bản, chỉ mục... " +
			"Chỉ đọc lúc tạo; thay đổi chỉ áp dụng cho tổ chức tạo sau đó, không ghi lại cho tổ chức đã có. " +
			"0 hoặc số âm = dùng mặc định tích hợp 10GB.",
	},
	// tenant.auto_create_api_key restores the legacy behaviour where creating
	// a tenant also minted a full-access API key and returned its plaintext
	// token in the create response. Newer versions stopped doing this (keys
	// are created explicitly via tenant_api_keys), which is a breaking change
	// for integrations that relied on the create response carrying a key.
	// Deployments that need the old behaviour set this to true (or the
	// WEKNORA_TENANT_AUTO_CREATE_API_KEY env var). Default false keeps the
	// current, safer no-implicit-key behaviour. Read at create time only.
	"tenant.auto_create_api_key": {
		Type:     "bool",
		EnvName:  "WEKNORA_TENANT_AUTO_CREATE_API_KEY",
		Default:  false,
		Category: "tenant",
		Description: "Khi tạo tổ chức có tự sinh một API key toàn quyền (full_access) và trả token dạng rõ trong phản hồi của API tạo hay không. " +
			"Dùng để tương thích hành vi cũ \"tạo tổ chức là cấp API key mặc định\" (công tắc quay lui cho thay đổi phá vỡ tương thích). " +
			"Đọc trực tiếp mỗi lần tạo tổ chức, có hiệu lực ngay. Mặc định false (không tự tạo, phải tạo tường minh trong quản lý API key).",
	},
	// tenant.auto_accept_invitation: invite = auto-join switch (default false).
	"tenant.auto_accept_invitation": {
		Type:     "bool",
		EnvName:  "WEKNORA_TENANT_AUTO_ACCEPT_INVITATION",
		Default:  false,
		Category: "tenant",
		Description: "Công tắc toàn cục: khi bật, lúc quản trị tổ chức mời người dùng đã đăng ký qua email, " +
			"người được mời sẽ được thêm ngay (ghi thẳng quan hệ thành viên), không cần chấp nhận trong hộp thư và không tạo bản ghi lời mời chờ. " +
			"Khi tắt giữ nguyên luồng \"gửi lời mời → người được mời xác nhận trong hộp thư\". Đọc trực tiếp mỗi lần mời, có hiệu lực ngay. Mặc định false.",
	},
	"asynq.core_concurrency": {
		Type:            "int",
		EnvName:         "WEKNORA_ASYNQ_CORE_CONCURRENCY",
		Default:         int64(types.DefaultCoreWorkerConcurrency),
		Category:        "worker",
		RequiresRestart: true,
		Description:     "Số luồng đồng thời bảo đảm mỗi instance cho các tác vụ cốt lõi như phân tích tài liệu, phân tích lại thủ công. Có thể dùng thêm pool co giãn chung; cần khởi động lại sau khi thay đổi.",
	},
	"asynq.postprocess_concurrency": {
		Type:            "int",
		EnvName:         "WEKNORA_ASYNQ_POSTPROCESS_CONCURRENCY",
		Default:         int64(types.DefaultPostProcessWorkerConcurrency),
		Category:        "worker",
		RequiresRestart: true,
		Description:     "Số luồng đồng thời riêng cho điều phối nhẹ và fan-out làm giàu sau khi phân tích xong, tránh bị phân tích tài liệu dài chặn; cần khởi động lại sau khi thay đổi.",
	},
	"asynq.enrichment_concurrency": {
		Type:            "int",
		EnvName:         "WEKNORA_ASYNQ_ENRICHMENT_CONCURRENCY",
		Default:         int64(types.DefaultEnrichmentWorkerConcurrency),
		Category:        "worker",
		RequiresRestart: true,
		Description:     "Số luồng đồng thời bảo đảm mỗi instance cho sinh tóm tắt, hình ảnh, đồ thị và câu hỏi. Có thể dùng thêm pool co giãn chung; cần khởi động lại sau khi thay đổi.",
	},
	"asynq.maintenance_concurrency": {
		Type:            "int",
		EnvName:         "WEKNORA_ASYNQ_MAINTENANCE_CONCURRENCY",
		Default:         int64(types.DefaultMaintenanceWorkerConcurrency),
		Category:        "worker",
		RequiresRestart: true,
		Description:     "Số luồng đồng thời bảo đảm mỗi instance cho đồng bộ nguồn dữ liệu, xử lý lô, di chuyển và dọn dẹp xóa, cách ly cứng với pipeline phía người dùng; cần khởi động lại sau khi thay đổi.",
	},
	"asynq.shared_concurrency": {
		Type:            "int",
		EnvName:         "WEKNORA_ASYNQ_SHARED_CONCURRENCY",
		Default:         int64(types.DefaultSharedWorkerConcurrency),
		Category:        "worker",
		RequiresRestart: true,
		Description:     "Số luồng co giãn mỗi instance dùng chung cho phân tích cốt lõi và làm giàu nội dung. Bên đang tồn đọng sẽ mượn phần rảnh; cần khởi động lại sau khi thay đổi.",
	},
	// asynq.wiki_concurrency is the size of the DEDICATED wiki worker pool,
	// separate from the upstream pools. Read once when the wiki asynq server
	// starts — changing it in the UI requires a process restart. Mirrors
	// WEKNORA_WIKI_ASYNQ_CONCURRENCY (default 8).
	"asynq.wiki_concurrency": {
		Type:            "int",
		EnvName:         "WEKNORA_WIKI_ASYNQ_CONCURRENCY",
		Default:         int64(types.DefaultWikiWorkerConcurrency),
		Category:        "worker",
		RequiresRestart: true,
		Description: "Số worker đồng thời của pool riêng cho sinh Wiki (cách ly với pool phân tích tài liệu). " +
			"Sinh Wiki chủ yếu là gọi LLM tổng hợp; ngân sách đồng thời riêng giúp không bị tác vụ phân tích bỏ đói lúc tải lên cao điểm, " +
			"đồng thời không để đợt Wiki dồn dập làm chậm phân tích phía người dùng. Cần khởi động lại tiến trình dịch vụ mới có hiệu lực.",
	},
	// wiki.run_window confines wiki generation to off-hours: outside the
	// window the wiki queue is paused so chat gets the LLM. Read every 30s by
	// the wiki run-window scheduler, so edits apply without a restart.
	types.WikiRunWindowSettingKey: {
		Type:     "json",
		EnvName:  "",
		Default:  map[string]any{"enabled": true, "start": "19:00", "end": "06:00", "timezone": "Asia/Ho_Chi_Minh"},
		Category: "worker",
		Description: "Daily window (HH:MM, IANA timezone; end before start spans midnight) in which wiki " +
			"generation runs. Outside it the wiki queue is paused so interactive chat keeps the LLM; " +
			"queued wiki work resumes when the window opens. enabled=false runs wiki at any time.",
	},
	// model.max_concurrency is the DEFAULT per-model cap on concurrent
	// background (ingestion/enrichment) LLM/embedding/VLM calls, keyed by
	// model ID and shared across replicas. Read at every gated call via the
	// limiter governor; a runtime bridge (applyModelMaxConcurrency) pushes UI
	// edits into limiter.SetGlobalLimit so no restart is needed. Individual
	// models may override this via their own max_concurrency parameter.
	// Mirrors WEKNORA_MODEL_MAX_CONCURRENCY (default 32). 0/negative disables
	// the default cap.
	"model.max_concurrency": {
		Type:     "int",
		EnvName:  "WEKNORA_MODEL_MAX_CONCURRENCY",
		Default:  int64(32),
		Category: "worker",
		Description: "Giới hạn đồng thời mặc định của tác vụ nền (nhập tài liệu/làm giàu) trên một mô hình, chia sẻ giữa mọi bản sao theo ID mô hình. " +
			"Đọc trực tiếp mỗi lần gọi, có hiệu lực ngay, không cần khởi động lại. 0 hoặc số âm = tắt giới hạn mặc định " +
			"(mỗi mô hình vẫn tuân theo giới hạn riêng cấu hình trong quản lý mô hình). Chỉ ảnh hưởng tác vụ nền, không ảnh hưởng trò chuyện tương tác.",
	},
	// memory.platform_enabled is the platform-wide memory kill switch. When
	// false, long-term memory is off for every tenant and every user — the
	// workspace MemoryConfig.Enabled switch and the per-user opt-out stay
	// stored but have no effect. Read live on every gated call (memory
	// recall, retrieval conditioning, settings UI), so edits take effect
	// immediately without a restart.
	"memory.platform_enabled": {
		Type:     "bool",
		EnvName:  "",
		Default:  true,
		Category: "memory",
		Description: "Công tắc tổng bộ nhớ dài hạn cấp nền tảng. Khi tắt, bộ nhớ dài hạn của mọi tổ chức và mọi người dùng dừng ngay, " +
			"cài đặt bộ nhớ của cá nhân và tổ chức được giữ nhưng không có tác dụng. Có hiệu lực ngay sau khi thay đổi.",
	},
	// memory.config is the platform-wide memory configuration (on/off,
	// write mode, extraction and embedding models, budgets). It replaces the
	// per-workspace setting: tenants inherit it and users cannot opt out.
	// Read live on every memory call, so edits apply without a restart.
	types.MemoryConfigSettingKey: {
		Type:     "json",
		EnvName:  "",
		Default:  map[string]any{"enabled": true},
		Category: "memory",
		Description: "Platform-wide long-term memory configuration (same fields as the former organization " +
			"memory config). Every organization and user inherits it; edits apply immediately.",
	},
	// usage.enforce turns the usage-package pre-flight check on: a turn
	// is refused (HTTP 429) when the workspace or person has no current
	// subscription, holds no seat, or has used up a limit. ON by default;
	// switching it off is for testing only. SystemAdmins are exempt.
	// Read live per QA turn.
	// document_template.font_sizes: the platform-wide font sizes of the
	// NĐ30 templates drafted from chat, within the NĐ30 ranges
	// (types.TemplateFontSizeSpecs); a workspace may override them. Read per
	// template, so edits apply to the next document.
	types.DocumentTemplateFontSizesSettingKey: {
		Type:     "json",
		EnvName:  "",
		Default:  map[string]any{},
		Category: "chat",
		Description: "Cỡ chữ (quốc hiệu, tiêu ngữ, cơ quan, …) và lề trang (le_tren, le_duoi, le_trai, le_phai, mm) mặc định của văn bản mẫu NĐ30 tạo từ chat, " +
			"trong khoảng NĐ30 cho phép. Tổ chức có thể ghi đè trong Hồ sơ tổ chức. Có hiệu lực từ văn bản tạo tiếp theo.",
	},
	types.UsageEnforceSettingKey: {
		Type:     "bool",
		EnvName:  "",
		Default:  types.UsageEnforceDefault,
		Category: "chat",
		Description: "Bật kiểm tra gói sử dụng trước mỗi lượt hỏi: không có thuê bao, hết hạn, " +
			"không có seat hoặc hết hạn mức → từ chối (429). Quản trị hệ thống không bị chặn. Có hiệu lực ngay. " +
			"Mặc định bật; chỉ tắt khi kiểm thử.",
	},
	types.SettingChatAttachmentSectionPicker: {
		Type: "bool", Default: true, Category: "chat",
		Description: "Tệp đính kèm quá dài cho một lượt hỏi: model trả lời đọc mục lục của tệp (Chương, Mục, Điều, đề mục) " +
			"và chọn các phần cần cho câu hỏi (một lượt gọi ngắn), thay vì lấy các đoạn chứa nhiều từ của câu hỏi nhất. " +
			"Chọn lỗi thì tự quay về cách lấy theo từ. Có hiệu lực ngay.",
	},
	types.SettingChatAttachmentInlineTokens: {
		Type: "int", Default: int64(types.DefaultChatAttachmentInlineTokens), Category: "chat",
		Description: "Tệp đính kèm dài tới bao nhiêu token (ước lượng) thì đưa nguyên văn vào lượt hỏi; dài hơn thì " +
			"model chọn phần theo mục lục (khi bật chọn theo mục lục), hoặc lấy các đoạn khớp câu hỏi. " +
			"Tối đa bằng ngân sách một lượt hỏi (12000); giá trị lớn hơn không có tác dụng. Có hiệu lực ngay.",
	},
	// llm.auxiliary_model_id: the small model for session titles and
	// follow-up suggestions (measured 2026-10-14: as good as Qwen on
	// follow-ups, slightly weaker on titles; NOT used for query rewrite,
	// where it answered in Chinese and broke JSON). Read per call.
	types.AuxiliaryModelSettingKey: {
		Type:     "string",
		EnvName:  "WEKNORA_AUXILIARY_MODEL_ID",
		Default:  types.DefaultAuxiliaryModelID,
		Category: "chat",
		Description: "Model nhỏ cho việc phụ ít rủi ro: đặt tiêu đề phiên và gợi ý câu hỏi tiếp theo, để model chính " +
			"chỉ lo trả lời. Để trống thì dùng model trả lời. Model lỗi thì tự quay về model trả lời. Có hiệu lực ngay.",
	},
	// llm.compaction_model_id: the model that writes context-compaction
	// summaries. Empty = the answering model. Its own context window sizes
	// each summarization request; on failure the answering model retries
	// once before the summary degrades. Read per turn.
	types.CompactionModelSettingKey: {
		Type:     "string",
		EnvName:  "WEKNORA_COMPACTION_MODEL_ID",
		Default:  "",
		Category: "chat",
		Description: "Model viết bản tóm tắt khi nén ngữ cảnh hội thoại (lịch sử dài, phiên nhiều lượt). Để trống thì " +
			"dùng model trả lời. Model lỗi thì model trả lời thử lại một lần. Có hiệu lực từ lượt hỏi tiếp theo.",
	},
	// Billing: the bank account that receives package payments (VietQR
	// needs the bank BIN + account number), how long a pending order
	// stays payable, and a contact line for the payment page.
	types.BillingBankBINSettingKey: {
		Type: "string", EnvName: "", Default: "", Category: "billing",
		Description: "Mã BIN ngân hàng nhận tiền (ví dụ Vietcombank 970436, BIDV 970418, VietinBank 970415) để tạo mã VietQR.",
	},
	types.BillingBankNameSettingKey: {
		Type: "string", EnvName: "", Default: "", Category: "billing",
		Description: "Tên ngân hàng hiển thị trên trang thanh toán.",
	},
	types.BillingBankAccountNoSettingKey: {
		Type: "string", EnvName: "", Default: "", Category: "billing",
		Description: "Số tài khoản nhận tiền. Để trống = chưa mở thanh toán (chỉ xem gói).",
	},
	types.BillingBankAccountNameSettingKey: {
		Type: "string", EnvName: "", Default: "", Category: "billing",
		Description: "Tên chủ tài khoản nhận tiền.",
	},
	types.BillingOrderTTLHoursSettingKey: {
		Type: "int", EnvName: "", Default: int64(72), Category: "billing",
		Description: "Đơn hàng chưa thanh toán tự hết hạn sau số giờ này.",
	},
	types.BillingContactSettingKey: {
		Type: "string", EnvName: "", Default: "", Category: "billing",
		Description: "Thông tin liên hệ hỗ trợ thanh toán hiển thị trên trang thanh toán.",
	},
	// billing.webhook_secret authenticates the payment checker on POST
	// /billing/payments/webhook; read per request, so a new value takes
	// effect at once. Empty (and no ENV) = webhook off.
	types.BillingWebhookSecretSettingKey: {
		Type: "string", EnvName: types.BillingWebhookSecretEnv, Default: "", Category: "billing", Secret: true,
		Description: "Khóa bí mật của dịch vụ tự kiểm tra giao dịch khi đẩy giao dịch vào webhook thanh toán. " +
			"Để trống = tắt webhook. Có hiệu lực ngay; chỉ hiện đầy đủ một lần lúc tạo.",
	},
	// chat.web_search_enabled gates web search (network providers and the
	// local-browser tool) platform-wide. The per-agent / per-request switches
	// and the tenant provider config stay in place but are neutralised while
	// this is off. Read live per QA turn.
	"chat.web_search_enabled": {
		Type:     "bool",
		EnvName:  "",
		Default:  true,
		Category: "chat",
		Description: "Công tắc tìm kiếm Internet cấp nền tảng. Khi tắt, mọi cuộc trò chuyện (gồm trợ lý và công cụ trình duyệt cục bộ) không còn tìm kiếm Internet, " +
			"công tắc phía người dùng được giữ nhưng không có tác dụng. Có hiệu lực ngay sau khi thay đổi.",
	},
	// chat.mcp_enabled gates MCP tool invocation platform-wide. Agent configs
	// keep their MCP selection but every run resolves to "none" while this is
	// off. Read live per QA turn.
	"chat.mcp_enabled": {
		Type:     "bool",
		EnvName:  "",
		Default:  true,
		Category: "chat",
		Description: "Công tắc công cụ MCP cấp nền tảng. Khi tắt, mọi lượt chạy trợ lý không nạp dịch vụ MCP nào, " +
			"lựa chọn MCP trong cấu hình trợ lý được giữ nhưng không có tác dụng. Có hiệu lực ngay sau khi thay đổi.",
	},
	// legal.current_wording.resolver switches the wording of retrieved legal
	// provisions and the "Lịch sử sửa đổi" block to the amendment resolver
	// (legal_amend_wiring.go). Off = the previous phrase-only wording and
	// history. Read live on every call.
	LegalWordingResolverSetting: {
		Type:     "bool",
		EnvName:  "",
		Default:  true,
		Category: "chat",
		Description: "Đọc lời văn hiện hành của điều khoản pháp luật qua bộ giải sửa đổi (trạng thái từng khoản, điểm; lời văn sửa đổi chưa xác minh được đánh dấu). " +
			"Khi tắt, dùng lại cách cũ (chỉ thay cụm từ). Có hiệu lực ngay sau khi thay đổi.",
	},
	// knowledge.parse_defaults is the platform-wide default parse
	// configuration published by a SystemAdmin. While enabled it replaces
	// per-upload/per-document process overrides — ResolveProcessConfig merges
	// it over the KB-level config, so uploads parse with the admin-approved
	// models/settings without anyone touching the parse dialog. The value is
	// a JSON document: {"enabled": bool, ...KnowledgeProcessOverrides}.
	SystemParseDefaultsKey: {
		Type:     "json",
		EnvName:  "",
		Default:  map[string]any{"enabled": false},
		Category: "knowledge",
		Description: "Cấu hình phân tích tài liệu mặc định toàn hệ thống. Khi enabled=true, mọi lượt tải lên/phân tích lại/nhập từ nguồn dữ liệu đều chạy theo cấu hình này, " +
			"bỏ qua process_config gửi kèm mỗi lần tải lên (chế độ khóa). Dành cho trường hợp quản trị viên muốn phân tích ngay mà không cần hiểu tham số; " +
			"các trường giống process_config của API tải lên (vlm_config, asr_config, chunking_config, parser_engine_rules...).",
	},
}

// systemSettingService wires the repository, audit log, and (P2)
// the Redis client + an in-memory cache. Cache strategy is "preload
// at boot, invalidate via pubsub":
//
//   - On startup we async-load every row into `cache` (best-effort —
//     a DB hiccup just means a slower warmup, not a fatal error).
//   - GetXxx reads from cache (microsecond latency).
//   - Update writes DB → updates local cache → publishes a change
//     notification to Redis.
//   - Subscribers on every replica read the notification and re-fetch
//     the row from DB (NOT from the message payload — the message only
//     carries the key, never the value, so we never trust pubsub-as-
//     transport with config bytes).
//   - The publishing replica skips its own messages by matching
//     OriginID against its instanceID.
//
// When Redis is nil (lite mode / REDIS_ADDR unset), every code path
// degrades back to P1 behaviour: no cache invalidation, but local
// edits still take effect (since Update does write the local cache
// inline). This is the right behaviour for single-replica deployments.
type systemSettingService struct {
	repo  interfaces.SystemSettingRepository
	audit interfaces.AuditLogService
	rdb   *redis.Client // may be nil in lite mode
	cfg   *config.Config

	// instanceID disambiguates this replica from its peers in the
	// pubsub stream. Generated once at construction; never changes.
	instanceID string

	// cache holds every known setting indexed by key. Populated by
	// loadCache (preload + after every pubsub message). All access
	// goes through `mu`. A nil entry means "we know there's no row
	// and the resolver should fall through to ENV/default".
	mu    sync.RWMutex
	cache map[string]*types.SystemSetting

	// loaded flips true once the initial preload finishes. Reads
	// before this point fall through to the DB so the very first
	// hot request after boot doesn't get a default-valued surprise.
	loaded atomic.Bool

	// subOnce guarantees SubscribeRedis can be called multiple times
	// without spawning duplicate goroutines (defensive — main only
	// calls it once).
	subOnce sync.Once
}

// NewSystemSettingService is the dig provider. audit may be nil
// (matches the tenantMemberService convention — tests that don't care
// about audit can pass nil and emitAudit no-ops). rdb may also be nil
// when REDIS_ADDR is unset — the service still uses its local cache,
// but skips cross-replica pubsub invalidation.
func NewSystemSettingService(
	repo interfaces.SystemSettingRepository,
	audit interfaces.AuditLogService,
	rdb *redis.Client,
	cfg *config.Config,
) interfaces.SystemSettingService {
	s := &systemSettingService{
		repo:       repo,
		audit:      audit,
		rdb:        rdb,
		cfg:        cfg,
		instanceID: uuid.NewString(),
		cache:      make(map[string]*types.SystemSetting),
	}
	// Async preload — don't block container build / handler readiness
	// on a slow DB. The first few requests may miss cache and hit the
	// DB directly via the resolver fallback; that's a few ms each and
	// completes long before the cache is full.
	go s.preload(context.Background())
	return s
}

// preload populates the cache with every row from the system_settings
// table. Best-effort: a DB error here is logged and silently swallowed,
// because the resolver's DB-fallback path will still serve correct
// values (just slower). Logging the count gives operators a single line
// in the startup log they can grep for ("how many keys did P2 load?").
func (s *systemSettingService) preload(ctx context.Context) {
	rows, err := s.repo.List(ctx)
	if err != nil {
		logger.Warnf(ctx, "[system_settings] preload failed, falling back to per-request DB reads: %v", err)
		return
	}
	s.mu.Lock()
	for _, row := range rows {
		s.cache[row.Key] = row
	}
	s.mu.Unlock()

	s.loaded.Store(true)
	s.mu.RLock()
	loadedCount := len(s.cache)
	s.mu.RUnlock()
	logger.Infof(ctx, "[system_settings] cache loaded %d keys (instance=%s)", loadedCount, s.instanceID[:8])

	// Side-effect bridges: any setting whose live value affects an
	// in-process subsystem needs to be pushed there after preload, so
	// the subsystem doesn't lag the cache by a full request cycle.
	// Add new bridges here as more env vars get migrated.
	s.applySSRFWhitelist(ctx)
	s.applyModelMaxConcurrency(ctx)
	s.applyDockerBackendEnabled(ctx)
}

// encodeDefault produces the JSONB encoding for a spec's built-in
// default. Mirrors encodeForType but operates on already-typed Go
// values from registry so we never have to round-trip through `any`
// type assertions on the seed path. Returns an error when spec.Default
// is missing or its Go type doesn't match spec.Type — that's a code
// bug in the registry entry, surface it loudly rather than silently
// seeding the wrong shape.
func encodeDefault(spec settingSpec) (types.JSON, error) {
	switch spec.Type {
	case "int":
		var n int64
		switch v := spec.Default.(type) {
		case int:
			n = int64(v)
		case int64:
			n = v
		case float64:
			n = int64(v)
		default:
			return nil, fmt.Errorf("registry spec for int has wrong default type %T", spec.Default)
		}
		b, _ := json.Marshal(n)
		return types.JSON(b), nil
	case "string":
		v, ok := spec.Default.(string)
		if !ok {
			return nil, fmt.Errorf("registry spec for string has wrong default type %T", spec.Default)
		}
		b, _ := json.Marshal(v)
		return types.JSON(b), nil
	case "bool":
		v, ok := spec.Default.(bool)
		if !ok {
			return nil, fmt.Errorf("registry spec for bool has wrong default type %T", spec.Default)
		}
		b, _ := json.Marshal(v)
		return types.JSON(b), nil
	case "string_list":
		switch v := spec.Default.(type) {
		case []string:
			if v == nil {
				v = []string{}
			}
			b, _ := json.Marshal(v)
			return types.JSON(b), nil
		case nil:
			return types.JSON(`[]`), nil
		default:
			return nil, fmt.Errorf("registry spec for string_list has wrong default type %T", spec.Default)
		}
	case "json":
		b, err := json.Marshal(spec.Default)
		if err != nil {
			return nil, fmt.Errorf("registry spec for json has unencodable default: %w", err)
		}
		return types.JSON(b), nil
	default:
		return nil, errors.New("unknown declared type: " + spec.Type)
	}
}

// reload re-fetches a single key from DB and updates the cache. Called
// from the pubsub subscriber loop after another replica publishes a
// change. A repo.Get(nil) result removes the entry — the row must have
// been deleted by an out-of-band tool (P1 has no Delete endpoint, but
// hand-edits still work).
func (s *systemSettingService) reload(ctx context.Context, key string) {
	row, err := s.repo.Get(ctx, key)
	if err != nil {
		logger.Warnf(ctx, "[system_settings] reload %q failed: %v", key, err)
		return
	}
	s.mu.Lock()
	if row == nil {
		delete(s.cache, key)
	} else {
		s.cache[key] = row
	}
	s.mu.Unlock()

	// Push any side-effect bridges for the changed key. Bridges are
	// idempotent — calling them on every reload (even when the change
	// is to a different key) is fine and lets us avoid plumbing a
	// per-key dispatch table.
	s.dispatchSideEffects(ctx, key)
}

// dispatchSideEffects fans out post-Update / post-reload work to
// subsystems whose state depends on a system_setting. Each bridge
// looks up its own keys and decides whether to act — this keeps the
// dispatcher trivial as we add more.
func (s *systemSettingService) dispatchSideEffects(ctx context.Context, changedKey string) {
	switch changedKey {
	case "ssrf.whitelist":
		s.applySSRFWhitelist(ctx)
	case "model.max_concurrency":
		s.applyModelMaxConcurrency(ctx)
	case sandbox.DockerBackendEnabledSettingKey:
		s.applyDockerBackendEnabled(ctx)
	}
}

// applySSRFWhitelist resolves the active ssrf.whitelist via the 3-tier
// resolver and pushes the result (merged with SSRF_WHITELIST_EXTRA)
// to utils.SetSSRFWhitelistFromRaw. SSRF_WHITELIST_EXTRA stays env-only:
// it's typically set by docker-compose / k8s for sidecar service names
// and shouldn't be subject to UI accidents.
//
// Called at preload (initial sync), after Update (this replica's edit),
// and after reload (peer's edit via pubsub).
func (s *systemSettingService) applySSRFWhitelist(ctx context.Context) {
	list := s.GetStringList(ctx, "ssrf.whitelist", "SSRF_WHITELIST", []string{})
	primary := strings.Join(list, ",")
	extra := strings.TrimSpace(os.Getenv("SSRF_WHITELIST_EXTRA"))
	merged := primary
	if extra != "" {
		if merged == "" {
			merged = extra
		} else {
			merged = merged + "," + extra
		}
	}
	utils.SetSSRFWhitelistFromRaw(merged)
	logger.Infof(ctx, "[system_settings] SSRF whitelist applied (%d primary entries, extra=%v)",
		len(list), extra != "")
}

// applyModelMaxConcurrency resolves model.max_concurrency via the 3-tier
// resolver and pushes it into the model concurrency governor so UI edits take
// effect without a restart. Only the process-wide default limit is retuned;
// the installed limiter backend (redis/local) stays intact. The default (8)
// deliberately mirrors container.defaultModelMaxConcurrency so the value here
// matches what the container installs at boot.
//
// Called at preload (initial sync), after Update (this replica's edit), and
// after reload (peer's edit via pubsub).
func (s *systemSettingService) applyModelMaxConcurrency(ctx context.Context) {
	limit := int(s.GetInt(ctx, "model.max_concurrency", "WEKNORA_MODEL_MAX_CONCURRENCY", 32))
	limiter.SetGlobalLimit(limit)
	logger.Infof(ctx, "[system_settings] model.max_concurrency applied (limit=%d)", limit)
}

func (s *systemSettingService) applyDockerBackendEnabled(ctx context.Context) {
	enabled := s.GetBool(ctx, sandbox.DockerBackendEnabledSettingKey, sandbox.DockerBackendEnabledEnv, false)
	sandbox.SetDockerBackendEnabled(enabled)
	logger.Infof(ctx, "[system_settings] sandbox.docker_enabled applied (enabled=%v)", enabled)
}

// publishChange fans the change out to peers. Best-effort: a Redis
// outage logs a warning but does not fail the Update — the DB write
// already succeeded and our local cache is up-to-date. Other replicas
// will pick up the new value on their next preload (e.g. restart) or
// when their own resolver detects a stale cache via fallback.
func (s *systemSettingService) publishChange(ctx context.Context, key string) {
	if s.rdb == nil {
		return
	}
	payload, err := json.Marshal(changeMessage{Key: key, OriginID: s.instanceID})
	if err != nil {
		logger.Warnf(ctx, "[system_settings] marshal change for %q: %v", key, err)
		return
	}
	pubCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := s.rdb.Publish(pubCtx, pubsubChannel(), payload).Err(); err != nil {
		logger.Warnf(ctx, "[system_settings] publish %q: %v", key, err)
	}
}

// NewSystemSettingService is the dig provider. audit may be nil
// (matches the tenantMemberService convention — tests that don't care
// about audit can pass nil and emitAudit no-ops).
//
// Compatibility shim: the real ctor lives above (with rdb). Keeping this
// alternate signature would break dig (two providers for one type), so
// it is intentionally NOT exported separately. Tests that don't have a
// Redis client should pass nil — the service detects nil and degrades
// to the P1 "no cache, no pubsub" path.

// resolveRaw runs the 3-tier fallback ladder for an arbitrary key and
// returns either the raw DB value bytes (when present), or nil with
// the boolean fromDB=false to signal the caller to consult ENV / default.
//
// P2: cache-first. If the preload finished and the cache has an entry
// for this key, return it. Cache misses (key absent) are AUTHORITATIVE
// when loaded.IsTrue — preload populated every existing row, and any
// subsequent Update would have updated the cache inline. So a miss
// after preload means the row genuinely doesn't exist and we should
// skip the DB query entirely. Before preload finishes we still consult
// the DB to avoid a "cold-start serves defaults" surprise.
//
// Errors at the DB layer degrade to ENV/default with a warning log —
// upstream business code (file upload, etc.) gets a usable answer
// instead of a 500. This is the deliberate degradation policy spelled
// out in the interface comment.
func (s *systemSettingService) resolveRaw(ctx context.Context, key string) (raw types.JSON, fromDB bool) {
	spec, known := registry[key]
	if s.loaded.Load() {
		s.mu.RLock()
		row, ok := s.cache[key]
		s.mu.RUnlock()
		if ok && row != nil {
			if known && isBootstrapDefaultRow(row, spec) {
				return nil, false
			}
			return row.Value, true
		}
		// Cache populated and key not present → authoritative miss.
		return nil, false
	}
	// Pre-warmup path: hit the DB so a request that lands in the
	// startup window doesn't get the env/default surprise.
	row, err := s.repo.Get(ctx, key)
	if err != nil {
		logger.Warnf(ctx, "[system_settings] resolve %q failed, falling through to env/default: %v", key, err)
		return nil, false
	}
	if row == nil {
		return nil, false
	}
	if known && isBootstrapDefaultRow(row, spec) {
		return nil, false
	}
	return row.Value, true
}

// GetInt resolves an int64 setting. Priority: DB > ENV > def. Returns
// def on every error path so business code never has to handle the
// "the settings store is broken" case.
func (s *systemSettingService) GetInt(ctx context.Context, key string, envName string, def int64) int64 {
	if raw, ok := s.resolveRaw(ctx, key); ok {
		// Try canonical number form first.
		var n int64
		if err := json.Unmarshal(raw, &n); err == nil {
			return n
		}
		// Tolerate `"42"` so hand-edited rows still work.
		var quoted string
		if err := json.Unmarshal(raw, &quoted); err == nil {
			if v, err := strconv.ParseInt(quoted, 10, 64); err == nil {
				return v
			}
		}
		logger.Warnf(ctx, "[system_settings] %q: cannot parse %s as int, falling back", key, string(raw))
	}
	if envName != "" {
		if v := os.Getenv(envName); v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				return n
			}
		}
	}
	return def
}

// GetString resolves a string setting. Same priority + degradation as GetInt.
func (s *systemSettingService) GetString(ctx context.Context, key string, envName string, def string) string {
	if raw, ok := s.resolveRaw(ctx, key); ok {
		var v string
		if err := json.Unmarshal(raw, &v); err == nil {
			return v
		}
		logger.Warnf(ctx, "[system_settings] %q: cannot parse %s as string, falling back", key, string(raw))
	}
	if envName != "" {
		if v := os.Getenv(envName); v != "" {
			return v
		}
	}
	return def
}

// GetBool resolves a bool setting. Tolerates legacy ENV values like
// "1", "0", "yes", "no" via strconv.ParseBool. Same priority + degradation.
func (s *systemSettingService) GetBool(ctx context.Context, key string, envName string, def bool) bool {
	if raw, ok := s.resolveRaw(ctx, key); ok {
		var v bool
		if err := json.Unmarshal(raw, &v); err == nil {
			return v
		}
		logger.Warnf(ctx, "[system_settings] %q: cannot parse %s as bool, falling back", key, string(raw))
	}
	if envName != "" {
		if v := os.Getenv(envName); v != "" {
			if b, err := strconv.ParseBool(v); err == nil {
				return b
			}
		}
	}
	return def
}

// GetStringList resolves a []string setting. Priority: DB > ENV > def.
//
// At the ENV level the value is parsed as a comma-separated string
// (matches the legacy SSRF_WHITELIST format and means operators don't
// have to learn a new convention to migrate). Whitespace around each
// entry is trimmed; empty entries are dropped. The returned slice is
// always non-nil so callers can iterate without a nil check.
//
// Same degradation policy as the other Get*: a DB-layer error logs a
// warning and falls through to ENV/default, so consumer paths
// (SSRF check, etc.) never have to handle "settings store broken".
func (s *systemSettingService) GetStringList(ctx context.Context, key string, envName string, def []string) []string {
	if raw, ok := s.resolveRaw(ctx, key); ok {
		var v []string
		if err := json.Unmarshal(raw, &v); err == nil {
			if v == nil {
				v = []string{}
			}
			return v
		}
		logger.Warnf(ctx, "[system_settings] %q: cannot parse %s as string_list, falling back", key, string(raw))
	}
	if envName != "" {
		if raw := os.Getenv(envName); raw != "" {
			out := make([]string, 0, 4)
			for _, entry := range strings.Split(raw, ",") {
				entry = strings.TrimSpace(entry)
				if entry != "" {
					out = append(out, entry)
				}
			}
			return out
		}
	}
	if def == nil {
		return []string{}
	}
	return def
}

// GetJSON resolves a structured ("json" value_type) setting and returns the
// raw stored document. found=false means no DB row exists — callers then fall
// back to their own default (JSON settings have no ENV mapping). Same
// cache-first + DB-degradation policy as the other Get* resolvers.
func (s *systemSettingService) GetJSON(ctx context.Context, key string) (raw types.JSON, found bool) {
	return s.resolveRaw(ctx, key)
}

// List returns all known settings for the management UI. Persisted rows
// are enriched with registry metadata. Registry keys without a saved DB
// override are returned as virtual rows using the effective fallback
// value (ENV/config/default), so merely migrating the schema never
// changes runtime behaviour.
// retiredSettingKeys are former settings whose leftover DB rows are ignored
// and hidden from the management UI:
//   - asynq.concurrency: the old fixed-ratio aggregate.
//   - auth.default_tenant_mode: sign-up no longer creates a personal
//     organization; new accounts are always tenantless.
var retiredSettingKeys = []string{"asynq.concurrency", "auth.default_tenant_mode"}

func (s *systemSettingService) List(ctx context.Context) ([]*types.SystemSetting, error) {
	rows, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]*types.SystemSetting, len(rows))
	out := make([]*types.SystemSetting, 0, len(rows)+len(registry))
	for _, row := range rows {
		byKey[row.Key] = row
	}
	// Retired keys: keeping an old DB row visible would suggest it still
	// controls something, so hide them explicitly while preserving all
	// genuinely unknown rows for diagnostics.
	for _, key := range retiredSettingKeys {
		delete(byKey, key)
	}

	keys := make([]string, 0, len(registry))
	for key := range registry {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		spec := registry[key]
		if row := byKey[key]; row != nil {
			row.Enum = spec.Enum
			if isBootstrapDefaultRow(row, spec) {
				row.Value = s.fallbackJSONForSpec(key, spec)
			}
			out = append(out, maskSecretSetting(row, spec))
			delete(byKey, key)
			continue
		}
		out = append(out, maskSecretSetting(s.virtualSetting(key, spec), spec))
	}

	// Preserve out-of-band rows so operators can still see unexpected
	// data instead of having it disappear from the UI.
	extraKeys := make([]string, 0, len(byKey))
	for key := range byKey {
		extraKeys = append(extraKeys, key)
	}
	sort.Strings(extraKeys)
	for _, key := range extraKeys {
		out = append(out, byKey[key])
	}
	return out, nil
}

// Get returns one row by key. Used by the management UI's "load before
// edit" pattern. Returns (nil, nil) when missing (unknown-key handling
// is done at the handler layer for nicer 404 vs 200-with-default UX).
//
// Enriches the row with registry-side `Enum` for the same UI reason
// as List.
func (s *systemSettingService) Get(ctx context.Context, key string) (*types.SystemSetting, error) {
	spec, ok := registry[key]
	if !ok {
		return nil, fmt.Errorf("unknown setting key %q", key)
	}
	row, err := s.repo.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	if row != nil {
		row.Enum = spec.Enum
		if isBootstrapDefaultRow(row, spec) {
			row.Value = s.fallbackJSONForSpec(key, spec)
		}
		return maskSecretSetting(row, spec), nil
	}
	return maskSecretSetting(s.virtualSetting(key, spec), spec), nil
}

// IsSecretSetting reports whether key is a Secret setting, so callers can
// keep its value out of places the service does not control (request logs).
func IsSecretSetting(key string) bool { return registry[key].Secret }

// maskSecretSetting returns row as API callers may see it: for a Secret
// key a copy whose value keeps only the last 4 characters ("" stays ""),
// so the page can tell a secret is set without the server handing it
// out. The copy matters: Update's row is also the cache entry.
func maskSecretSetting(row *types.SystemSetting, spec settingSpec) *types.SystemSetting {
	if row == nil || !spec.Secret {
		return row
	}
	cp := *row
	cp.IsSecret = true
	var v string
	_ = json.Unmarshal(row.Value, &v)
	if v != "" {
		tail := v
		if len(tail) > 4 {
			tail = tail[len(tail)-4:]
		}
		v = "••••••••" + tail
	}
	cp.Value, _ = json.Marshal(v)
	return &cp
}

func (s *systemSettingService) virtualSetting(key string, spec settingSpec) *types.SystemSetting {
	category := spec.Category
	if category == "" {
		category = "general"
	}
	return &types.SystemSetting{
		Key:             key,
		Value:           s.fallbackJSONForSpec(key, spec),
		ValueType:       spec.Type,
		Category:        category,
		Description:     spec.Description,
		IsSecret:        spec.Secret,
		RequiresRestart: spec.RequiresRestart,
		LastModifiedBy:  "",
		Enum:            spec.Enum,
	}
}

func (s *systemSettingService) fallbackJSONForSpec(key string, spec settingSpec) types.JSON {
	if spec.EnvName != "" {
		if raw := strings.TrimSpace(os.Getenv(spec.EnvName)); raw != "" {
			switch spec.Type {
			case "int":
				if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
					if encoded, err := encodeForType(spec.Type, n); err == nil {
						return encoded
					}
				}
			case "string":
				if encoded, err := encodeForType(spec.Type, raw); err == nil {
					return encoded
				}
			case "bool":
				if b, err := strconv.ParseBool(raw); err == nil {
					if encoded, err := encodeForType(spec.Type, b); err == nil {
						return encoded
					}
				}
			case "string_list":
				entries := make([]string, 0, 4)
				for _, entry := range strings.Split(raw, ",") {
					entry = strings.TrimSpace(entry)
					if entry != "" {
						entries = append(entries, entry)
					}
				}
				if encoded, err := encodeForType(spec.Type, entries); err == nil {
					return encoded
				}
			}
		}
	}
	if key == "auth.registration_mode" {
		mode := config.AuthRegistrationModeSelfServe
		if s.cfg != nil && s.cfg.Auth != nil {
			if configured := strings.TrimSpace(s.cfg.Auth.RegistrationMode); configured != "" {
				mode = configured
			}
		}
		if encoded, err := encodeForType(spec.Type, mode); err == nil {
			return encoded
		}
	}
	encoded, err := encodeDefault(spec)
	if err != nil {
		return types.JSON(`null`)
	}
	return encoded
}

// isBootstrapDefaultRow treats old migration/service seeded defaults as
// placeholders rather than operator-owned overrides. Those rows used an
// empty last_modified_by and the registry default value, so deployments
// that already ran the unsafe seed regain the intended ENV/config
// fallback behaviour until a SystemAdmin explicitly saves a value.
//
// Secret keys are never treated so: their saved "" deliberately turns the
// feature off, and must not fall back to the ENV value even when written
// without a user in context (a script, an internal caller).
func isBootstrapDefaultRow(row *types.SystemSetting, spec settingSpec) bool {
	if row == nil || spec.Secret || strings.TrimSpace(row.LastModifiedBy) != "" {
		return false
	}
	def, err := encodeDefault(spec)
	if err != nil {
		return false
	}
	return jsonEqual(row.Value, def)
}

func jsonEqual(a, b types.JSON) bool {
	var ca, cb bytes.Buffer
	if err := json.Compact(&ca, a); err != nil {
		return false
	}
	if err := json.Compact(&cb, b); err != nil {
		return false
	}
	return bytes.Equal(ca.Bytes(), cb.Bytes())
}

// Update validates and persists a new value. Steps:
//  1. Look up the registry spec — reject unknown keys with 400 semantics.
//  2. Coerce + validate the rawValue against spec.Type. Numeric inputs
//     from JSON unmarshalling arrive as float64; we accept both int64
//     and float64 for "int" and round-trip through strconv to surface
//     rejection of e.g. floats like 3.14 cleanly.
//  3. Build the SystemSetting row, write via repo.Upsert.
//  4. Emit an audit log carrying old + new values for forensics.
//
// Returns the persisted row (re-read from DB so updated_at /
// last_modified_by are fresh).
func (s *systemSettingService) Update(ctx context.Context, key string, rawValue any) (*types.SystemSetting, error) {
	spec, ok := registry[key]
	if !ok {
		return nil, fmt.Errorf("unknown setting key %q", key)
	}

	encoded, err := encodeForType(spec.Type, rawValue)
	if err != nil {
		return nil, fmt.Errorf("invalid value for %q (expected %s): %w", key, spec.Type, err)
	}

	// Enum check: only meaningful for "string". Compare the decoded
	// string against the registry-declared whitelist. Done after
	// encodeForType so we know the raw value passed type validation.
	//
	// Use a local name `str` rather than `s` to avoid shadowing the
	// outer `*systemSettingService` receiver — the previous version
	// relied on the shadow being unused inside the block, which was a
	// trap for future edits.
	if len(spec.Enum) > 0 && spec.Type == "string" {
		str, _ := rawValue.(string)
		allowed := false
		for _, opt := range spec.Enum {
			if str == opt {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil, fmt.Errorf("invalid value for %q: %q not in %v", key, str, spec.Enum)
		}
	}

	// Per-key structural validation. encodeForType already enforces the
	// value_type contract (int / string / bool / string_list); this hook
	// is for keys whose value carries an internal grammar that, if
	// silently malformed, would either fail to take effect at runtime
	// (e.g. SSRF whitelist parser drops bad CIDRs) or actively
	// mis-classify input. Reject with 400 so the UI can show a clear
	// inline error instead of "saved" + nothing happens.
	if err := validateRegistryEntry(key, rawValue); err != nil {
		return nil, fmt.Errorf("invalid value for %q: %w", key, err)
	}

	// Capture pre-image for the audit log — pulled fresh, not from
	// any cache, so concurrent admin edits race-fairly (last writer
	// wins, audit reflects what was actually replaced).
	prev, _ := s.repo.Get(ctx, key)
	var oldValue types.JSON
	var category, description string
	var isSecret, requiresRestart bool
	if prev != nil {
		oldValue = prev.Value
		category = prev.Category
		description = prev.Description
		isSecret = prev.IsSecret
		requiresRestart = prev.RequiresRestart
	} else {
		// First-write path: derive category/description from registry
		// so the row matches the seeded migration shape. Operators can
		// hand-edit description in the DB if they want richer copy.
		category = spec.Category
		if category == "" {
			category = "general"
		}
		description = spec.Description
		requiresRestart = spec.RequiresRestart
	}

	row := &types.SystemSetting{
		Key:             key,
		Value:           encoded,
		ValueType:       spec.Type,
		Category:        category,
		Description:     description,
		IsSecret:        isSecret || spec.Secret,
		RequiresRestart: requiresRestart,
		LastModifiedBy:  auditActor(ctx),
	}
	if err := s.repo.Upsert(ctx, row); err != nil {
		return nil, fmt.Errorf("upsert system setting %q: %w", key, err)
	}

	// Re-read so caller sees DB-side defaults (id, updated_at) populated.
	persisted, err := s.repo.Get(ctx, key)
	if err != nil || persisted == nil {
		// Don't fail the operation just because the read-back hiccuped —
		// the upsert already succeeded. Return the optimistic value.
		persisted = row
	}
	// Mirror the registry-derived enrichment that List/Get apply.
	// Without this, the persisted row hands back an empty Enum field
	// to the API client, which causes the management UI to swap a
	// t-select for a plain text input on the post-save patch and
	// display the raw enum value (e.g. "self_serve") until the next
	// full reload. Other registry-only fields (Type via ValueType is
	// already on the row; Description/Category are persisted) don't
	// need the same fix-up because they're stored on the row itself.
	persisted.Enum = spec.Enum

	// Update local cache inline so this replica's next read sees the
	// new value without waiting for the pubsub roundtrip. Other replicas
	// pick it up via publishChange below.
	s.mu.Lock()
	s.cache[key] = persisted
	s.mu.Unlock()

	// Push to side-effect bridges (e.g. utils.SetSSRFWhitelistFromRaw).
	s.dispatchSideEffects(ctx, key)

	s.publishChange(ctx, key)
	s.emitChangeAudit(ctx, key, spec.Type, oldValue, encoded)
	return maskSecretSetting(persisted, spec), nil
}

// Reset deletes the DB override for `key` so the resolver falls back
// to ENV / built-in default. Idempotent — deleting a key that was
// never persisted is treated as success (no audit row written, since
// nothing actually changed) so retries from the UI can't pile up
// noise. Mirrors Update's cache+pubsub+audit+side-effect plumbing on
// the success path so other replicas drop the entry too.
//
// Unknown keys still 400 — we don't want a typo on the URL to silently
// pretend it cleared something.
func (s *systemSettingService) Reset(ctx context.Context, key string) error {
	spec, ok := registry[key]
	if !ok {
		return fmt.Errorf("unknown setting key %q", key)
	}

	// Capture pre-image for the audit log before the row vanishes.
	prev, _ := s.repo.Get(ctx, key)

	deleted, err := s.repo.Delete(ctx, key)
	if err != nil {
		return fmt.Errorf("delete system setting %q: %w", key, err)
	}

	// Drop from the local cache regardless of `deleted` so a stale
	// entry from before the row existed (shouldn't happen, but cheap
	// to defend) gets cleared too.
	s.mu.Lock()
	delete(s.cache, key)
	s.mu.Unlock()

	// Side-effect bridges and pubsub fire even for no-op resets so
	// peers that may have a stale cached row converge. publishChange
	// is best-effort; reload() on the peer side handles "row absent"
	// by deleting the cache entry, which is what we want here.
	s.dispatchSideEffects(ctx, key)
	s.publishChange(ctx, key)

	// Only emit an audit row on real deletions. The new_value field
	// is intentionally null to flag this as a reset (vs an Update
	// which always writes a concrete new_value).
	if deleted && prev != nil {
		s.emitChangeAudit(ctx, key, spec.Type, prev.Value, nil)
	}
	return nil
}

// SubscribeRedis starts a single goroutine that subscribes to the
// pubsub channel and refreshes the local cache when peers publish
// changes. Idempotent (subOnce). When Redis is nil (lite mode) returns
// nil immediately — single-replica deployments don't need pubsub
// because Update already writes the local cache inline.
//
// The subscriber loop runs until ctx is cancelled (server shutdown).
// On Redis disconnection we reconnect with exponential backoff up to
// 30s, mirroring the approval/gate.go convention so operators see the
// same recovery behaviour across pubsub-using subsystems.
func (s *systemSettingService) SubscribeRedis(ctx context.Context) error {
	if s.rdb == nil {
		logger.Infof(ctx, "[system_settings] Redis not configured, skipping pubsub (single-replica mode)")
		return nil
	}
	s.subOnce.Do(func() {
		go s.runSubscribeLoop(ctx)
	})
	return nil
}

// runSubscribeLoop is the long-running goroutine spawned by
// SubscribeRedis. Reconnects on transient errors; exits on ctx.Done().
func (s *systemSettingService) runSubscribeLoop(ctx context.Context) {
	channel := pubsubChannel()
	logger.Infof(ctx, "[system_settings] subscribed to %s (instance=%s)", channel, s.instanceID[:8])

	const maxBackoff = 30 * time.Second
	backoff := time.Second
	for {
		// ctx may already be cancelled (server shutting down before
		// pubsub became active).
		if ctx.Err() != nil {
			return
		}
		sub := s.rdb.Subscribe(ctx, channel)
		// Verify the subscription is active so a publish-and-disconnect
		// race doesn't silently drop the first message.
		if _, err := sub.Receive(ctx); err != nil {
			logger.Warnf(ctx, "[system_settings] subscribe %s: %v (retry in %s)", channel, err, backoff)
			_ = sub.Close()
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return
			}
			if backoff < maxBackoff {
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
			}
			continue
		}
		backoff = time.Second // reset after a healthy connection
		ch := sub.Channel()
		s.consumeMessages(ctx, ch)
		_ = sub.Close()
		// consumeMessages returns either because ctx is done or the
		// subscription was torn down; loop back and try again.
	}
}

// consumeMessages drains the pubsub channel, dispatching to reload()
// for every key the peer says changed. Returns when the channel
// closes (Redis disconnect) or ctx is done.
func (s *systemSettingService) consumeMessages(ctx context.Context, ch <-chan *redis.Message) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			var m changeMessage
			if err := json.Unmarshal([]byte(msg.Payload), &m); err != nil {
				logger.Warnf(ctx, "[system_settings] bad pubsub payload: %v", err)
				continue
			}
			// Skip our own publish — the local cache is already fresh
			// (Update wrote it inline). Without this every Update would
			// trigger a redundant DB roundtrip on the publishing replica.
			if m.OriginID == s.instanceID {
				continue
			}
			s.reload(ctx, m.Key)
		}
	}
}

// emitChangeAudit writes one audit row per successful Update. Best-
// effort — a nil audit service or a write failure does not bubble up.
// This mirrors tenantMemberService.emitAudit's failure semantics: the
// business op (config update) succeeds even if audit is broken.
func (s *systemSettingService) emitChangeAudit(
	ctx context.Context, key, valueType string, oldValue, newValue types.JSON,
) {
	if s.audit == nil {
		return
	}
	entry := map[string]any{
		"key":        key,
		"value_type": valueType,
		"old_value":  json.RawMessage(oldValue),
		"new_value":  json.RawMessage(newValue),
	}
	if registry[key].Secret {
		// a secret never lands in the audit log, only that it changed
		entry["old_value"] = secretAuditValue(oldValue)
		entry["new_value"] = secretAuditValue(newValue)
	}
	details, _ := json.Marshal(entry)
	_ = s.audit.Log(ctx, &types.AuditLog{
		// tenant_id=0 marks the row as system-scope (the audit_logs
		// table itself is tenant-scoped; 0 is the convention for
		// platform-wide events).
		TenantID:    0,
		ActorUserID: auditActor(ctx),
		ActorRole:   "system_admin",
		Action:      types.AuditActionSystemSettingChanged,
		TargetType:  "system_setting",
		TargetID:    key,
		Outcome:     types.AuditOutcomeSuccess,
		Details:     types.JSON(details),
	})
}

// encodeForType validates rawValue against the declared type and
// returns the canonical JSON encoding for the DB. Rejects type
// mismatches (e.g. passing "abc" for an int field) with a clear error
// the handler can surface to the UI.
func encodeForType(declared string, rawValue any) (types.JSON, error) {
	switch declared {
	case "int":
		var n int64
		switch v := rawValue.(type) {
		case int:
			n = int64(v)
		case int32:
			n = int64(v)
		case int64:
			n = v
		case float64:
			// JSON unmarshalling delivers numbers as float64; reject
			// non-integer floats (e.g. 3.14) cleanly rather than
			// silently truncating.
			if v != float64(int64(v)) {
				return nil, fmt.Errorf("expected integer, got %v", v)
			}
			n = int64(v)
		case string:
			parsed, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("expected integer, got %q", v)
			}
			n = parsed
		default:
			return nil, fmt.Errorf("expected integer, got %T", rawValue)
		}
		b, _ := json.Marshal(n)
		return types.JSON(b), nil
	case "string":
		v, ok := rawValue.(string)
		if !ok {
			return nil, fmt.Errorf("expected string, got %T", rawValue)
		}
		b, _ := json.Marshal(v)
		return types.JSON(b), nil
	case "bool":
		v, ok := rawValue.(bool)
		if !ok {
			return nil, fmt.Errorf("expected bool, got %T", rawValue)
		}
		b, _ := json.Marshal(v)
		return types.JSON(b), nil
	case "string_list":
		// Accept either a JSON array of strings (the canonical UI shape
		// — t-tag-input emits string[]) or a single comma-separated
		// string (operator pasting from a legacy ENV value). Reject
		// arrays containing non-strings to avoid silently coercing
		// `[1, 2]` into `["1", "2"]` — that hides typos.
		var entries []string
		switch v := rawValue.(type) {
		case []any:
			entries = make([]string, 0, len(v))
			for i, item := range v {
				s, ok := item.(string)
				if !ok {
					return nil, fmt.Errorf("expected string at index %d, got %T", i, item)
				}
				s = strings.TrimSpace(s)
				if s != "" {
					entries = append(entries, s)
				}
			}
		case []string:
			entries = make([]string, 0, len(v))
			for _, s := range v {
				s = strings.TrimSpace(s)
				if s != "" {
					entries = append(entries, s)
				}
			}
		case string:
			for _, s := range strings.Split(v, ",") {
				s = strings.TrimSpace(s)
				if s != "" {
					entries = append(entries, s)
				}
			}
			if entries == nil {
				entries = []string{}
			}
		default:
			return nil, fmt.Errorf("expected string array, got %T", rawValue)
		}
		b, _ := json.Marshal(entries)
		return types.JSON(b), nil
	case "json":
		// Structured-document settings (e.g. knowledge.parse_defaults).
		// Only objects/arrays are accepted — a "json" setting is always a
		// document, never a bare scalar; per-key shape enforcement lives in
		// validateRegistryEntry.
		switch rawValue.(type) {
		case map[string]any, []any, []string:
		default:
			return nil, fmt.Errorf("expected JSON object or array, got %T", rawValue)
		}
		b, err := json.Marshal(rawValue)
		if err != nil {
			return nil, fmt.Errorf("cannot encode JSON value: %w", err)
		}
		return types.JSON(b), nil
	default:
		return nil, errors.New("unknown declared type: " + declared)
	}
}

// secretAuditValue says whether a secret was set, without its value.
func secretAuditValue(raw types.JSON) string {
	var v string
	if len(raw) == 0 || json.Unmarshal(raw, &v) != nil || v == "" {
		return ""
	}
	return "[set]"
}

// validateRegistryEntry runs key-specific structural validation that
// goes beyond the type contract enforced by encodeForType. It is called
// by Update right after the type/enum checks; nil error means the value
// is acceptable for persistence.
//
// Adding new validators:
//   - keep them strict (reject silently-broken input rather than fixing
//     it server-side — the user should see what they typed),
//   - keep them deterministic (no DNS lookups, no env reads),
//   - keep error messages user-facing (the handler surfaces them as the
//     400 body verbatim).
func validateRegistryEntry(key string, rawValue any) error {
	switch key {
	case types.BillingWebhookSecretSettingKey:
		// "" turns the webhook off; anything else must look generated, which
		// also refuses the masked value the page shows being saved back.
		v, _ := rawValue.(string)
		if v == "" {
			return nil
		}
		if len(v) < 32 || len(v) > 256 {
			return errors.New("the webhook secret must be 32 to 256 characters")
		}
		for _, r := range v {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
				return errors.New("the webhook secret may only contain letters, digits, '-' and '_'")
			}
		}
	case "asynq.core_concurrency", "asynq.postprocess_concurrency",
		"asynq.enrichment_concurrency", "asynq.maintenance_concurrency",
		"asynq.shared_concurrency", "asynq.wiki_concurrency":
		n, err := coerceToPositiveInt64(rawValue)
		if err != nil {
			return err
		}
		if n < 1 {
			return errors.New("concurrency must be at least 1")
		}
	case "ssrf.whitelist":
		// Coerce into the same shape encodeForType produced. We don't
		// look at the encoded JSON because that's already canonicalised
		// — easier to validate the raw input the user typed.
		entries, err := coerceToStringSlice(rawValue)
		if err != nil {
			return err
		}
		return utils.ValidateSSRFWhitelistEntries(entries)
	case types.MemoryConfigSettingKey:
		raw, err := json.Marshal(rawValue)
		if err != nil {
			return fmt.Errorf("cannot encode value: %w", err)
		}
		if _, err := types.ParseMemoryConfig(raw); err != nil {
			return err
		}
	case types.DocumentTemplateFontSizesSettingKey:
		raw, err := json.Marshal(rawValue)
		if err != nil {
			return fmt.Errorf("cannot encode value: %w", err)
		}
		if _, err := types.ParseTemplateFontSizes(raw); err != nil {
			return err
		}
	case types.WikiRunWindowSettingKey:
		raw, err := json.Marshal(rawValue)
		if err != nil {
			return fmt.Errorf("cannot encode value: %w", err)
		}
		if _, err := types.ParseWikiRunWindow(raw); err != nil {
			return err
		}
	case SystemParseDefaultsKey:
		// Strict-decode into SystemParseDefaults: unknown keys would be
		// silently dropped at read time, which in a locked config means the
		// admin believes a setting applies while it never does. Reject so
		// the UI surfaces the typo.
		raw, err := json.Marshal(rawValue)
		if err != nil {
			return fmt.Errorf("cannot encode value: %w", err)
		}
		var def types.SystemParseDefaults
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&def); err != nil {
			return fmt.Errorf("invalid parse defaults payload: %w", err)
		}
	}
	return nil
}

// coerceToPositiveInt64 accepts int / int64 / float64 from JSON decoding.
func coerceToPositiveInt64(rawValue any) (int64, error) {
	switch v := rawValue.(type) {
	case int:
		return int64(v), nil
	case int64:
		return v, nil
	case float64:
		if v != float64(int64(v)) {
			return 0, errors.New("expected integer value")
		}
		return int64(v), nil
	default:
		return 0, fmt.Errorf("expected integer, got %T", rawValue)
	}
}

// coerceToStringSlice mirrors the input shapes accepted by
// encodeForType for "string_list": []any of strings, []string, or a
// comma-separated string. Returns the trimmed, empty-stripped result.
//
// Kept private because the only caller is validateRegistryEntry; the
// main encode path has its own (slightly different) coercion that
// preserves rejection of non-string elements at a specific index for
// clearer error messages.
func coerceToStringSlice(rawValue any) ([]string, error) {
	switch v := rawValue.(type) {
	case []any:
		out := make([]string, 0, len(v))
		for i, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("expected string at index %d, got %T", i, item)
			}
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
		return out, nil
	case []string:
		out := make([]string, 0, len(v))
		for _, s := range v {
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
		return out, nil
	case string:
		out := make([]string, 0, 4)
		for _, s := range strings.Split(v, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("expected string array, got %T", rawValue)
	}
}
