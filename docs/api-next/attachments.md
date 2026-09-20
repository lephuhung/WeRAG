# Attachments API

Frontend module: `frontend-next/lib/api/attachments.ts` — see [README](README.md) for envelope, auth headers, and transport conventions.

---

### `uploadTemporaryAttachment` — `POST /sessions/:session_id/attachments`

handler: `UploadTemporaryDocument` · `temporary_document.go`

**Multipart form**:
| name | type | req | notes |
|---|---|---|---|
| `agent_id` | string |  |  |
| `parser_engine` | string |  |  |
| `file` | file | yes | multipart upload |

**Response**: binary download (file stream, not JSON)
---
### `getTemporaryAttachment` — `GET /sessions/:id/attachments/:attachment_id`

handler: `GetTemporaryDocument` · `temporary_document.go`

**Response**:
```json
{ success: true, data: Connector }
```
---
### `previewTemporaryAttachment` — `GET /sessions/:id/attachments/:attachment_id/preview`

handler: `PreviewTemporaryDocument` · `temporary_document.go`

**Response**: binary download (file stream, not JSON)
---
### `deleteTemporaryAttachment` — `DELETE /sessions/:id/attachments/:attachment_id`

handler: `DeleteTemporaryDocument` · `temporary_document.go`

**Response**: `204 No Content`
---