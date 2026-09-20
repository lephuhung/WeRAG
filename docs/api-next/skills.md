# Skills API

Frontend module: `frontend-next/lib/api/skills.ts` — see [README](README.md) for envelope, auth headers, and transport conventions.

---

### `listSkills` — `GET /skills`

handler: `ListSkills` · `skill_handler.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `sandbox_config_id` | string |  | Sandbox config ID |

**Response**:
```json
{ success: true, data: response, skills_available: true }
```
---
### `listSkillCatalog` — `GET /skills/catalog`

handler: `ListCatalog` · `skill_catalog.go`

**Response**:
```json
{ success: true, data: rows }
```
---
### `registerSkillCatalogFromSource` — `POST /skills/catalog`

handler: `RegisterCatalog` · `skill_catalog.go`

**Multipart form**:
| name | type | req | notes |
|---|---|---|---|
| `file` | file | yes | multipart upload |

**Response**:
```json
{ success: true, data: catalogIDResponse(cat) }
```
---
### `registerSkillCatalogFromFile` — `POST /skills/catalog`

handler: `RegisterCatalog` · `skill_catalog.go`

**Multipart form**:
| name | type | req | notes |
|---|---|---|---|
| `file` | file | yes | multipart upload |

**Response**:
```json
{ success: true, data: catalogIDResponse(cat) }
```
---
### `installSkillCatalog` — `POST /skills/catalog/:id/install`

handler: `InstallCatalog` · `skill_catalog.go`

**Body** `catalogInstallRequest`:
| field | type | req | notes |
|---|---|---|---|
| `sandbox_config_ids` | string[] |  |  |

**Response**:
```json
{ success: len(result.Errors) == 0, data: data }
```
---
### `deleteSkillCatalog` — `DELETE /skills/catalog/:id`

handler: `DeleteCatalog` · `skill_catalog.go`

**Response**:
```json
{ success: true }
```
---
### `listCatalogSkillFiles` — `GET /skills/catalog/:id/files`

handler: `ListCatalogFiles` · `skill_catalog.go`

**Response**:
```json
{ success: true, data: files }
```
---
### `getCatalogSkillFile` — `GET /skills/catalog/:id/files/content`

handler: `GetCatalogFile` · `skill_catalog.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `path` | string | yes | Skill-root-relative file path |

**Response**:
```json
{ success: true, data: service.SkillFileContent }
```
`data` is `SkillFileContent`:
| field | type | req | notes |
|---|---|---|---|
| `path` | string | yes |  |
| `size` | number | yes |  |
| `encoding` | string | yes |  |
| `content` | string |  |  |
| `media_type` | string |  |  |
| `truncated` | boolean |  |  |
| `binary` | boolean |  |  |

---