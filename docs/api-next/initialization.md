# Initialization & Bootstrap API

Frontend module: `frontend-next/lib/api/initialization.ts` — see [README](README.md) for envelope, auth headers, and transport conventions.

---

### `updateKBConfig` — `PUT /initialization/config/:kbId`

handler: `UpdateKBConfig` · `initialization.go`

**Body** `KBModelConfigRequest`:
| field | type | req | notes |
|---|---|---|---|
| `llmModelId` | string | yes |  |
| `embeddingModelId` | string |  |  |
| `vlm_config` | VLMConfig |  |  |
| `asr_config` | ASRConfig |  |  |
| `chunkSize` | number |  |  |
| `chunkOverlap` | number |  |  |
| `separators` | string[] |  |  |
| `parserEngineRules` | ParserEngineRule[] |  |  |
| `enableParentChild` | boolean |  |  |
| `parentChunkSize` | number |  |  |
| `childChunkSize` | number |  |  |
| `strategy` | string |  | Strategy / TokenLimit / Languages use pointer types so the handler can distinguish "field absent in payload" (no change) from "field present with empty/zero value" (clear / disable |
| `tokenLimit` | number |  |  |
| `languages` | string[] |  |  |
| `tableMetadataInstructions` | string |  |  |
| `enabled` | boolean |  |  |
| `storageProvider` | string |  | storage engine selection ("local" ...) | "minio" | "cos"); affects document upload and inline image storage; parameters are read from global settings |
| `storageBackendId` | string |  |  |
| `enabled` | boolean |  |  |
| `text` | string |  |  |
| `tags` | string[] |  |  |
| `nodes` | GraphNode[] |  |  |
| `relations` | GraphRelation[] |  |  |
| `customInstructions` | string |  |  |
| `enabled` | boolean |  |  |
| `questionCount` | number |  |  |
| `customInstructions` | string |  |  |

**Response**:
```json
{ success: true, message: string }
```
---
### `initializeSystemByKB` — `POST /initialization/initialize/:kbId`

handler: `InitializeByKB` · `initialization.go`

**Body** `InitializationRequest`:
| field | type | req | notes |
|---|---|---|---|
| `source` | string | yes |  |
| `modelName` | string | yes |  |
| `baseUrl` | string |  |  |
| `apiKey` | string |  |  |
| `source` | string | yes |  |
| `modelName` | string | yes |  |
| `baseUrl` | string |  |  |
| `apiKey` | string |  |  |
| `dimension` | number |  |  |
| `enabled` | boolean |  |  |
| `modelName` | string |  |  |
| `baseUrl` | string |  |  |
| `apiKey` | string |  |  |
| `enabled` | boolean |  |  |
| `modelName` | string |  |  |
| `baseUrl` | string |  |  |
| `apiKey` | string |  |  |
| `interfaceType` | string |  |  |
| `storageType` | string |  |  |
| `secretId` | string |  |  |
| `secretKey` | string |  |  |
| `region` | string |  |  |
| `bucketName` | string |  |  |
| `appId` | string |  |  |
| `pathPrefix` | string |  |  |
| `bucketName` | string |  |  |
| `pathPrefix` | string |  |  |
| `chunkSize` | number | yes |  |
| `chunkOverlap` | number |  |  |
| `separators` | string[] | yes |  |
| `enabled` | boolean |  |  |
| `text` | string |  |  |
| `tags` | string[] |  |  |
| `name` | string |  |  |
| `attributes` | string[] |  |  |
| `node1` | string |  |  |
| `node2` | string |  |  |
| `type` | string |  |  |
| `enabled` | boolean |  |  |
| `questionCount` | number |  |  |

**Response**:
```json
{ success: true, message: string, data: {models, knowledge_base} }
```
---
### `checkOllamaStatus` — `GET /initialization/ollama/status`

handler: `CheckOllamaStatus` · `initialization.go`

**Response**:
```json
{ success: true, data: {available, version, baseUrl} }
```
---
### `listOllamaModels` — `GET /initialization/ollama/models`

handler: `ListOllamaModels` · `initialization.go`

**Response**:
```json
{ success: true, data: {models} }
```
---
### `checkOllamaModels` — `POST /initialization/ollama/models/check`

handler: `CheckOllamaModels` · `initialization.go`

**Body** (inline struct):
| field | type | req | notes |
|---|---|---|---|
| `models` | string[] | yes |  |

**Response**:
```json
{ success: true, data: {models} }
```
---
### `downloadOllamaModel` — `POST /initialization/ollama/models/download`

handler: `DownloadOllamaModel` · `initialization.go`

**Body** (inline struct):
| field | type | req | notes |
|---|---|---|---|
| `modelName` | string | yes |  |

**Response**:
```json
{ success: true, message: string, data: {taskId, modelName, status, progress} }
```
---
### `getDownloadProgress` — `GET /initialization/ollama/download/progress/:taskId`

handler: `GetDownloadProgress` · `initialization.go`

**Response**:
```json
{ success: true, data: task }
```
---
### `listDownloadTasks` — `GET /initialization/ollama/download/tasks`

handler: `ListDownloadTasks` · `initialization.go`

**Response**:
```json
{ success: true, data: tasks }
```
---
### `getCurrentConfigByKB` — `GET /initialization/config/:kbId`

handler: `GetCurrentConfigByKB` · `initialization.go`

**Response**:
```json
{ success: true, data: config }
```
---
### `checkRemoteModel` — `POST /initialization/remote/check`

handler: `CheckRemoteModel` · `initialization.go`

**Body** `RemoteModelCheckRequest`:
| field | type | req | notes |
|---|---|---|---|
| `apiKey` | string |  |  |
| `appSecret` | string |  | AppSecret is for providers needing a second credential (LKEAP, Volcengine Rerank, ...); maps to model Parameters.AppSecret. |
| `baseUrl` | string |  |  |
| `customHeaders` | map[string]string |  |  |
| `dimension` | integer |  |  |
| `extraConfig` | map[string]string |  |  |
| `interfaceType` | string |  |  |
| `modelId` | string |  | ModelID, when set, instructs the handler to substitute any missing secrets (APIKey, AppSecret via ExtraConfig) from the stored model record before assembling the test client. This  |
| `modelName` | string | yes |  |
| `provider` | string |  |  |
| `source` | string |  | defaults to "remote" when empty |
| `supportsDimensionOverride` | boolean |  |  |

**Response**:
```json
{ success: true, data: {available, message} }
```
---
### `testEmbeddingModel` — `POST /initialization/embedding/test`

handler: `TestEmbeddingModel` · `initialization.go`

**Body** `ModelTestRequest`:
| field | type | req | notes |
|---|---|---|---|
| `source` | string |  |  |
| `modelName` | string | yes |  |
| `baseUrl` | string |  |  |
| `apiKey` | string |  |  |
| `provider` | string |  |  |
| `interfaceType` | string |  |  |
| `dimension` | number |  |  |
| `supportsDimensionOverride` | boolean |  |  |
| `customHeaders` | map[string]string |  |  |
| `extraConfig` | map[string]string |  |  |
| `appSecret` | string |  | AppSecret is for providers needing a second credential (LKEAP, Volcengine Rerank, ...); maps to model Parameters.AppSecret. |
| `modelId` | string |  | ModelID, when set, instructs the handler to substitute any missing secrets (APIKey, AppSecret via ExtraConfig) from the stored model record before assembling the test client. This  |

**Response**:
```json
{ success: true, data: {} }
```
---
### `checkRerankModel` — `POST /initialization/rerank/check`

handler: `CheckRerankModel` · `initialization.go`

**Body** `ModelTestRequest`:
| field | type | req | notes |
|---|---|---|---|
| `source` | string |  |  |
| `modelName` | string | yes |  |
| `baseUrl` | string |  |  |
| `apiKey` | string |  |  |
| `provider` | string |  |  |
| `interfaceType` | string |  |  |
| `dimension` | number |  |  |
| `supportsDimensionOverride` | boolean |  |  |
| `customHeaders` | map[string]string |  |  |
| `extraConfig` | map[string]string |  |  |
| `appSecret` | string |  | AppSecret is for providers needing a second credential (LKEAP, Volcengine Rerank, ...); maps to model Parameters.AppSecret. |
| `modelId` | string |  | ModelID, when set, instructs the handler to substitute any missing secrets (APIKey, AppSecret via ExtraConfig) from the stored model record before assembling the test client. This  |

**Response**:
```json
{ success: true, data: {available, message} }
```
---
### `checkASRModel` — `POST /initialization/asr/check`

handler: `CheckASRModel` · `initialization.go`

**Body** `ModelTestRequest`:
| field | type | req | notes |
|---|---|---|---|
| `source` | string |  |  |
| `modelName` | string | yes |  |
| `baseUrl` | string |  |  |
| `apiKey` | string |  |  |
| `provider` | string |  |  |
| `interfaceType` | string |  |  |
| `dimension` | number |  |  |
| `supportsDimensionOverride` | boolean |  |  |
| `customHeaders` | map[string]string |  |  |
| `extraConfig` | map[string]string |  |  |
| `appSecret` | string |  | AppSecret is for providers needing a second credential (LKEAP, Volcengine Rerank, ...); maps to model Parameters.AppSecret. |
| `modelId` | string |  | ModelID, when set, instructs the handler to substitute any missing secrets (APIKey, AppSecret via ExtraConfig) from the stored model record before assembling the test client. This  |

**Response**:
```json
{ success: true, data: {available, message} }
```
---
### `testMultimodalFunction` — `POST /initialization/multimodal/test`

handler: `TestMultimodalFunction` · `initialization.go`

**Multipart form**:
| name | type | req | notes |
|---|---|---|---|
| `image` | file | yes |  |
| `vlm_model` | string | yes |  |
| `vlm_base_url` | string | yes |  |
| `vlm_api_key` | string |  |  |
| `vlm_interface_type` | string |  |  |
| `storage_type` | string | yes |  |

**Body** `testMultimodalForm`:
| field | type | req | notes |
|---|---|---|---|
| `VLMModel` | string |  |  |
| `VLMBaseURL` | string |  |  |
| `VLMAPIKey` | string |  |  |
| `VLMInterfaceType` | string |  |  |
| `StorageType` | string |  |  |
| `COSSecretID` | string |  | COS configuration |
| `COSSecretKey` | string |  |  |
| `COSRegion` | string |  |  |
| `COSBucketName` | string |  |  |
| `COSAppID` | string |  |  |
| `COSPathPrefix` | string |  |  |
| `MinioBucketName` | string |  | MinIO configuration (when storage is minio) |
| `MinioPathPrefix` | string |  |  |
| `ChunkSize` | string |  | document split config (string parsed later, to avoid binding failures) |
| `ChunkOverlap` | string |  |  |
| `SeparatorsRaw` | string |  |  |

**Response**:
```json
{ success: true, data: {success, caption, ocr, processing_time} }
```
---
### `extractTextRelations` — `POST /initialization/extract/text-relation`

handler: `ExtractTextRelations` · `initialization.go`

**Body** `TextRelationExtractionRequest`:
| field | type | req | notes |
|---|---|---|---|
| `text` | string | yes |  |
| `tags` | string[] | yes |  |
| `model_id` | string | yes |  |

**Response**:
```json
{ success: true, data: result }
```
`data` is `result`:
| field | type | req | notes |
|---|---|---|---|
| `KnowledgeBaseID` | string | yes |  |
| `Count` | number | yes |  |

---
### `fabriText` — `POST /initialization/extract/fabri-text`

handler: `FabriText` · `initialization.go`

**Body** `FabriTextRequest`:
| field | type | req | notes |
|---|---|---|---|
| `tags` | string[] |  |  |
| `model_id` | string | yes |  |

**Response**:
```json
{ success: true, data: FabriTextResponse{Text: result} }
```
---
### `fabriTag` — `POST /initialization/extract/fabri-tag`

handler: `FabriTag` · `initialization.go`

**Response**:
```json
{ success: true, data: FabriTagResponse{Tags: tagRandom} }
```
---

## Referenced types

#### `ASRConfig`
| field | type | req | notes |
|---|---|---|---|
| `enabled` | boolean | yes |  |
| `model_id` | string | yes |  |
| `language` | string | yes |  |

#### `GraphNode`
| field | type | req | notes |
|---|---|---|---|
| `name` | string |  |  |
| `type` | string |  |  |
| `chunks` | string[] |  |  |
| `attributes` | string[] |  |  |

#### `GraphRelation`
| field | type | req | notes |
|---|---|---|---|
| `node1` | string |  |  |
| `node2` | string |  |  |
| `type` | string |  |  |

#### `ParserEngineRule`
| field | type | req | notes |
|---|---|---|---|
| `file_types` | string[] | yes |  |
| `engine` | string | yes |  |
| `xlsx_first_row_as_header` | boolean |  | XLSXFirstRowAsHeader restores row-1 column context for flat XLSX tables. nil preserves the parser default; an explicit false disables the mode. |

#### `VLMConfig`
| field | type | req | notes |
|---|---|---|---|
| `enabled` | boolean | yes |  |
| `model_id` | string | yes |  |
| `description_language` | string |  | DescriptionLanguage controls the language used for generated image captions. Empty means follow the document/request language. |
| `custom_instructions` | string |  | CustomInstructions adds KB-specific image interpretation guidance without replacing the system-owned OCR and Markdown output contract. |
| `model_name` | string | yes | legacy-compatible model name |
| `base_url` | string | yes | Base URL |
| `api_key` | string | yes | API Key |
| `interface_type` | string | yes | Interface Type: "ollama" or "openai" |
