# Agents API

Frontend module: `frontend-next/lib/api/agents.ts` — see [README](README.md) for envelope, auth headers, and transport conventions.

---

### `listAgents` — `GET /agents`

handler: `ListAgents` · `custom_agent.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `creator` | string |  |  |

**Response**:
```json
{ success: true, data: agents, disabled_own_agent_ids: disabledOwnIDs }
```
---
### `getAgentById` — `GET /agents/:id`

handler: `GetAgent` · `custom_agent.go`

**Response**:
```json
{ success: true, data: types.CustomAgent }
```
`data` is `CustomAgent`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes | Unique identifier of the agent (composite primary key with TenantID) For built-in agents, this is 'builtin-quick-answer' or 'builtin-smart-reasoning' For custom agents, this is a U |
| `name` | string | yes | Name of the agent |
| `description` | string | yes | Description of the agent |
| `avatar` | string | yes | Avatar/Icon of the agent (emoji or icon name) |
| `is_builtin` | boolean | yes | Whether this is a built-in agent (normal mode / agent mode) |
| `tenant_id` | number | yes | Tenant ID (composite primary key with ID) |
| `created_by` | string | yes | Created by user ID |
| `config` | CustomAgentConfig | yes | Agent configuration |
| `created_at` | string(time) | yes | Timestamps |
| `updated_at` | string(time) | yes |  |
| `deleted_at` | DeletedAt | yes |  |
| `creator_name` | string |  | CreatorName is batch-filled by the list handler before returning, same role as KnowledgeBase.CreatorName: lets list cards distinguish "created by me" vs "created by another workspace member". Not persisted; may be empty for built-in agents and legacy rows. |

---
### `createAgent` — `POST /agents`

handler: `CreateAgent` · `custom_agent.go`

**Body** `CreateAgentRequest`:
| field | type | req | notes |
|---|---|---|---|
| `name` | string | yes |  |
| `description` | string |  |  |
| `avatar` | string |  |  |
| `config` | CustomAgentConfig |  |  |

**Response**:
```json
{ success: true, data: types.CustomAgent }
```
`data` is `CustomAgent`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes | Unique identifier of the agent (composite primary key with TenantID) For built-in agents, this is 'builtin-quick-answer' or 'builtin-smart-reasoning' For custom agents, this is a U |
| `name` | string | yes | Name of the agent |
| `description` | string | yes | Description of the agent |
| `avatar` | string | yes | Avatar/Icon of the agent (emoji or icon name) |
| `is_builtin` | boolean | yes | Whether this is a built-in agent (normal mode / agent mode) |
| `tenant_id` | number | yes | Tenant ID (composite primary key with ID) |
| `created_by` | string | yes | Created by user ID |
| `config` | CustomAgentConfig | yes | Agent configuration |
| `created_at` | string(time) | yes | Timestamps |
| `updated_at` | string(time) | yes |  |
| `deleted_at` | DeletedAt | yes |  |
| `creator_name` | string |  | CreatorName is batch-filled by the list handler before returning, same role as KnowledgeBase.CreatorName: lets list cards distinguish "created by me" vs "created by another workspace member". Not persisted; may be empty for built-in agents and legacy rows. |

---
### `updateAgent` — `PUT /agents/:id`

handler: `UpdateAgent` · `custom_agent.go`

**Body** `UpdateAgentRequest`:
| field | type | req | notes |
|---|---|---|---|
| `name` | string |  |  |
| `description` | string |  |  |
| `avatar` | string |  | Avatar travels as a pointer so an omitted field can be told apart from an explicit clear: nil keeps the stored avatar, a pointer to "" wipes it. As a plain string the two cases wer |
| `config` | CustomAgentConfig |  |  |

**Response**:
```json
{ success: true, data: types.CustomAgent }
```
`data` is `CustomAgent`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes | Unique identifier of the agent (composite primary key with TenantID) For built-in agents, this is 'builtin-quick-answer' or 'builtin-smart-reasoning' For custom agents, this is a U |
| `name` | string | yes | Name of the agent |
| `description` | string | yes | Description of the agent |
| `avatar` | string | yes | Avatar/Icon of the agent (emoji or icon name) |
| `is_builtin` | boolean | yes | Whether this is a built-in agent (normal mode / agent mode) |
| `tenant_id` | number | yes | Tenant ID (composite primary key with ID) |
| `created_by` | string | yes | Created by user ID |
| `config` | CustomAgentConfig | yes | Agent configuration |
| `created_at` | string(time) | yes | Timestamps |
| `updated_at` | string(time) | yes |  |
| `deleted_at` | DeletedAt | yes |  |
| `creator_name` | string |  | CreatorName is batch-filled by the list handler before returning, same role as KnowledgeBase.CreatorName: lets list cards distinguish "created by me" vs "created by another workspace member". Not persisted; may be empty for built-in agents and legacy rows. |

---
### `deleteAgent` — `DELETE /agents/:id`

handler: `DeleteAgent` · `custom_agent.go`

**Response**:
```json
{ success: true, message: string }
```
---
### `copyAgent` — `POST /agents/:id/copy`

handler: `CopyAgent` · `custom_agent.go`

**Response**:
```json
{ success: true, data: types.CustomAgent }
```
`data` is `CustomAgent`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes | Unique identifier of the agent (composite primary key with TenantID) For built-in agents, this is 'builtin-quick-answer' or 'builtin-smart-reasoning' For custom agents, this is a U |
| `name` | string | yes | Name of the agent |
| `description` | string | yes | Description of the agent |
| `avatar` | string | yes | Avatar/Icon of the agent (emoji or icon name) |
| `is_builtin` | boolean | yes | Whether this is a built-in agent (normal mode / agent mode) |
| `tenant_id` | number | yes | Tenant ID (composite primary key with ID) |
| `created_by` | string | yes | Created by user ID |
| `config` | CustomAgentConfig | yes | Agent configuration |
| `created_at` | string(time) | yes | Timestamps |
| `updated_at` | string(time) | yes |  |
| `deleted_at` | DeletedAt | yes |  |
| `creator_name` | string |  | CreatorName is batch-filled by the list handler before returning, same role as KnowledgeBase.CreatorName: lets list cards distinguish "created by me" vs "created by another workspace member". Not persisted; may be empty for built-in agents and legacy rows. |

---
### `getPlaceholders` — `GET /agents/placeholders`

handler: `GetPlaceholders` · `custom_agent.go`

**Response**:
```json
{ success: true, data: {all, system_prompt, agent_system_prompt, context_template, rewrite_system_prompt, rewrite_prompt, fallback_prompt} }
```
---
### `getAgentTypePresets` — `GET /agents/type-presets`

handler: `GetAgentTypePresets` · `custom_agent.go`

**Response**:
```json
{ success: true, data: ListAgentTypePresetsWithContext }
```
---
### `listIMChannels` — `GET /agents/:id/im-channels`

handler: `ListIMChannels` · `im.go`

**Response**:
```json
{ data: im.SummarizeIMChannels(channels) }
```
---
### `listAllIMChannels` — `GET /im-channels`

handler: `ListAllIMChannels` · `im.go`

**Response**:
```json
{ data: channels }
```
---
### `createIMChannel` — `POST /agents/:id/im-channels`

handler: `CreateIMChannel` · `im.go`

**Body** (inline struct):
| field | type | req | notes |
|---|---|---|---|
| `platform` | string | yes |  |
| `name` | string |  |  |
| `mode` | string |  |  |
| `output_mode` | string |  |  |
| `session_mode` | string |  |  |
| `knowledge_base_id` | string |  |  |
| `credentials` | JSON |  |  |
| `enabled` | boolean |  |  |

**Response**:
```json
{ data: channel }
```
---
### `updateIMChannel` — `PUT /im-channels/:id`

handler: `UpdateIMChannel` · `im.go`

**Body** (inline struct):
| field | type | req | notes |
|---|---|---|---|
| `name` | string |  |  |
| `mode` | string |  |  |
| `output_mode` | string |  |  |
| `session_mode` | string |  |  |
| `knowledge_base_id` | string |  |  |
| `credentials` | JSON |  |  |
| `enabled` | boolean |  |  |
| `agent_id` | string |  |  |

**Response**:
```json
{ data: IMChannel }
```
`data` is `IMChannel`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes |  |
| `tenant_id` | number | yes |  |
| `agent_id` | string | yes |  |
| `platform` | string | yes |  |
| `name` | string | yes |  |
| `enabled` | boolean | yes |  |
| `mode` | string | yes |  |
| `output_mode` | string | yes |  |
| `knowledge_base_id` | string | yes |  |
| `bot_identity` | string | yes |  |
| `session_mode` | string | yes |  |
| `credentials` | JSON | yes |  |
| `created_at` | string(time) | yes |  |
| `updated_at` | string(time) | yes |  |
| `deleted_at` | DeletedAt | yes |  |

---
### `deleteIMChannel` — `DELETE /im-channels/:id`

handler: `DeleteIMChannel` · `im.go`

**Response**:
```json
{ success: true }
```
---
### `toggleIMChannel` — `POST /im-channels/:id/toggle`

handler: `ToggleIMChannel` · `im.go`

**Response**:
```json
{ data: IMChannel }
```
`data` is `IMChannel`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes |  |
| `tenant_id` | number | yes |  |
| `agent_id` | string | yes |  |
| `platform` | string | yes |  |
| `name` | string | yes |  |
| `enabled` | boolean | yes |  |
| `mode` | string | yes |  |
| `output_mode` | string | yes |  |
| `knowledge_base_id` | string | yes |  |
| `bot_identity` | string | yes |  |
| `session_mode` | string | yes |  |
| `credentials` | JSON | yes |  |
| `created_at` | string(time) | yes |  |
| `updated_at` | string(time) | yes |  |
| `deleted_at` | DeletedAt | yes |  |

---
### `getSuggestedQuestions` — `GET /agents/:id/suggested-questions`

handler: `GetSuggestedQuestions` · `custom_agent.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `knowledge_base_ids` | string |  | knowledge base ID list (comma-separated); overrides the agent's default config |
| `knowledge_ids` | string |  | knowledge ID list (comma-separated); restricts to specific documents |
| `tag_scopes` | string |  | tag scopes with knowledge-base ownership (JSON) |
| `limit` | integer |  | max number to return (defaults to the agent's configured opening-question count, max 30) |

**Response**:
```json
{ success: true, data: {questions} }
```
---
### `getWeChatQRCode` — `POST /wechat/qrcode`

handler: `WeChatGetQRCode` · `wechat_qrcode.go`

**Response**:
```json
{ data: {qrcode_url, qrcode} }
```
---
### `pollWeChatQRCodeStatus` — `POST /wechat/qrcode/status`

handler: `WeChatPollQRCodeStatus` · `wechat_qrcode.go`

**Body** (inline struct):
| field | type | req | notes |
|---|---|---|---|
| `qrcode` | string | yes |  |

**Response**:
```json
{ data: resp }
```
---

## Referenced types

#### `CustomAgentConfig`
| field | type | req | notes |
|---|---|---|---|
| `agent_mode` | string | yes | ===== Basic Settings ===== Agent mode: "quick-answer" for RAG mode, "smart-reasoning" for ReAct agent mode |
| `agent_type` | string |  | AgentType is a preset category under smart-reasoning mode that pre-fills system prompt, allowed tools and recommended KB compatibility. Valid values: "rag-qa", "wiki-qa", "hybrid-r |
| `system_prompt` | string | yes | System prompt for the agent (unified prompt, uses web_search_status placeholder for dynamic behavior) |
| `system_prompt_id` | string |  | SystemPromptID references a template ID in prompt_templates/ YAML files. If set and SystemPrompt is empty, the template content is resolved at request time for saved agents. |
| `context_template` | string | yes | Context template for normal mode (how to format retrieved chunks) |
| `context_template_id` | string |  | ContextTemplateID references a template ID in prompt_templates/ YAML files. If set and ContextTemplate is empty, the template content is resolved at request time for saved agents. |
| `model_id` | string | yes | ===== Model Settings ===== Model ID to use for conversations |
| `rerank_model_id` | string | yes | ReRank model ID for retrieval |
| `temperature` | number | yes | Temperature for LLM (0-1) |
| `max_completion_tokens` | number | yes | Maximum completion tokens. Quick-answer uses this for the RAG answer. Smart-reasoning ReAct rounds send this value as-is (zero becomes DefaultMaxCompletionTokens at call time: 4096 |
| `thinking` | boolean | yes | Whether to enable thinking mode (for models that support extended thinking) |
| `citation_enabled` | boolean | yes | Whether final answers include knowledge/web source citations. Nil defaults to true so agents saved before this option was introduced keep their existing behavior. |
| `max_iterations` | number | yes | ===== Agent Mode Settings ===== Maximum iterations for the ReAct loop. Zero is unset (filled with a default). A negative value is unlimited: the loop runs until the model stops, th |
| `llm_call_timeout` | number |  | Timeout for a single LLM call in seconds (0 = use global default) |
| `allowed_tools` | string[] | yes | Allowed tools (only for agent type) |
| `mcp_selection_mode` | string | yes | MCP service selection mode: "all" = all enabled MCP services, "selected" = specific services, "none" = no MCP |
| `mcp_services` | string[] | yes | Selected MCP service IDs (only used when MCPSelectionMode is "selected") |
| `mcp_auth_wait_timeout` | number |  | MCPAuthWaitTimeout is how many seconds to wait for in-conversation OAuth authorization before skipping. <=0 uses the gate's configured timeout. |
| `skills_selection_mode` | string | yes | ===== Skills Settings (only for smart-reasoning mode) ===== Skills selection mode: "all" = all installed skills, "selected" = specific skills, "none" = no skills |
| `selected_skills` | string[] | yes | Selected skill names (only used when SkillsSelectionMode is "selected") |
| `sandbox_config_id` | string |  | ===== Sandbox Settings ===== SandboxConfigID selects which workspace sandbox config this agent's skill scripts run on. Empty means sandbox execution is disabled. This references th |
| `kb_selection_mode` | string | yes | ===== Knowledge Base Settings ===== Knowledge base selection mode: "all" = all KBs, "selected" = specific KBs, "none" = no KB |
| `knowledge_bases` | string[] | yes | Associated knowledge base IDs (only used when KBSelectionMode is "selected") |
| `retrieve_kb_only_when_mentioned` | boolean | yes | Whether to retrieve knowledge base only when explicitly mentioned with @ (default: false) When true, knowledge base retrieval only happens if user explicitly mentions KB/files with |
| `retain_retrieval_history` | boolean | yes | Whether to retain retrieval history across turns |
| `image_upload_enabled` | boolean | yes | ===== Image Upload / Multimodal Settings ===== Whether image upload is enabled for this agent (default: false) |
| `vlm_model_id` | string | yes | VLM model ID for image analysis (optional, falls back to workspace-level VLM) |
| `audio_upload_enabled` | boolean | yes | Whether audio upload (ASR transcription) is enabled for this agent (default: false) |
| `asr_model_id` | string | yes | ASR model ID for audio transcription (optional) |
| `image_storage_provider` | string | yes | Storage provider for image uploads: "local", "minio", "cos", "tos", "s3", "oss", "ks3". Empty means use the global/workspace default provider. |
| `supported_file_types` | string[] | yes | ===== File Type Restriction Settings ===== Supported file types for this agent (e.g., ["csv", "xlsx", "xls"]) Empty means all file types are supported When set, only files with mat |
| `chat_parser_engine_rules` | ParserEngineRule[] |  | ===== Chat Attachment Parsing Settings ===== ChatParserEngineRules selects parser engines for session-scoped chat attachments by file type. Takes precedence over the tenant-level P |
| `attachment_image_understanding` | boolean | yes | AttachmentImageUnderstanding enables VLM OCR fallback for image-only / scanned documents (PDF/PPT whose pages are images). Disabled by default because it materially increases parse |
| `attachment_ocr_max_pages` | number |  | AttachmentOCRMaxPages caps how many pages of a scanned / image-only document this agent sends to the VLM for OCR. 0 falls back to the global default (WEKNORA_CHAT_ATTACHMENT_OCR_MA |
| `attachment_parse_wait_timeout_sec` | number |  | AttachmentParseWaitTimeoutSec bounds, in seconds, how long a chat turn waits for this agent's still-parsing attachments before proceeding with only the finished ones. 0 falls back  |
| `data_analysis_enabled` | boolean | yes | ===== Data Analysis Settings ===== Whether to run the legacy in-pipeline DuckDB SQL data-analysis stage when the retrieved chunks include CSV/Excel files. This issues an extra LLM  |
| `faq_priority_enabled` | boolean | yes | ===== FAQ Strategy Settings ===== Whether FAQ priority strategy is enabled (FAQ answers prioritized over document chunks) |
| `faq_direct_answer_threshold` | number | yes | FAQ direct answer threshold - if similarity > this value, use FAQ answer directly |
| `faq_score_boost` | number | yes | FAQ score boost multiplier - FAQ results score multiplied by this factor |
| `web_search_enabled` | boolean | yes | ===== Web Search Settings ===== Whether web search is enabled |
| `web_search_max_results` | number | yes | Maximum web search results |
| `web_search_provider_id` | string |  | WebSearchProviderID references a specific WebSearchProviderEntity. If empty, the workspace's default provider (is_default=true) is used. |
| `web_fetch_enabled` | boolean | yes | Whether to auto-fetch full page content for reranked web search results |
| `web_fetch_top_n` | number |  | Max number of pages to fetch after rerank (default: 3) |
| `multi_turn_enabled` | boolean | yes | ===== Multi-turn Conversation Settings ===== Whether multi-turn conversation is enabled |
| `history_turns` | number | yes | Number of history turns to keep in context. Quick-answer only; smart-reasoning sizes history by context window |
| `memory_enabled` | boolean |  | Whether this agent may read the user's long-term memory. Nil inherits the workspace setting; false opts a single agent out of memory even when the workspace has it on. There is no  |
| `embedding_top_k` | number | yes | ===== Retrieval Strategy Settings (for both modes) ===== Embedding/Vector retrieval top K |
| `keyword_threshold` | number | yes | Keyword retrieval threshold |
| `vector_threshold` | number | yes | Vector retrieval threshold |
| `rerank_top_k` | number | yes | Rerank top K |
| `rerank_threshold` | number | yes | Rerank threshold |
| `enable_query_expansion` | boolean | yes | ===== Advanced Settings (mainly for normal mode) ===== Whether to enable query expansion |
| `enable_rewrite` | boolean | yes | Whether to enable query rewrite for multi-turn conversations |
| `rewrite_prompt_system` | string | yes | Rewrite prompt system message |
| `rewrite_prompt_user` | string | yes | Rewrite prompt user message template |
| `query_understand_model_id` | string |  | Dedicated chat model ID for the query-understanding (rewrite + intent) step. When empty, the main conversation ModelID is used as a fallback. |
| `fallback_strategy` | string | yes | Fallback strategy: "fixed" for fixed response, "model" for model generation |
| `fallback_response` | string | yes | Fixed fallback response (when FallbackStrategy is "fixed") |
| `fallback_prompt` | string | yes | Fallback prompt (when FallbackStrategy is "model") |
| `intent_prompts` | map[string]string |  | IntentPrompts holds per-intent system prompt overrides for non-retrieval intents (greeting, chitchat, etc.). Empty values fall back to templates under config/prompt_templates/inten |
| `question_suggestions` | QuestionSuggestionConfig |  | ===== Conversation Question Suggestions ===== QuestionSuggestions owns both the static/knowledge-backed prompts shown before the first user turn and the contextual follow-up questi |
