# Local Browser Connection API

Frontend module: `frontend-next/lib/api/browser.ts` — see [README](README.md) for envelope, auth headers, and transport conventions.

---

### `getBrowserConnection` — `GET /api/v1/me/browser`

handler: `BrowserSkillAccount` · `browserskill.go`

**Body** (inline struct):
| field | type | req | notes |
|---|---|---|---|
| `action` | string |  |  |
| `origin` | string |  |  |

**Response**: binary download (file stream, not JSON)
---
### `pairBrowserConnection` — `POST /api/v1/me/browser`

handler: `BrowserSkillAccount` · `browserskill.go`

**Body** (inline struct):
| field | type | req | notes |
|---|---|---|---|
| `action` | string |  |  |
| `origin` | string |  |  |

**Response**: binary download (file stream, not JSON)
---
### `revokeBrowserConnection` — `POST /api/v1/me/browser`

handler: `BrowserSkillAccount` · `browserskill.go`

**Body** (inline struct):
| field | type | req | notes |
|---|---|---|---|
| `action` | string |  |  |
| `origin` | string |  |  |

**Response**: binary download (file stream, not JSON)
---
### `downloadBrowserExtension` — `GET /api/v1/me/browser/extension`

handler: `BrowserSkillDownload` · `browserskill.go`

**Response**: binary download (file stream, not JSON)
---