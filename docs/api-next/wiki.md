# Wiki API

Frontend module: `frontend-next/lib/api/wiki.ts` — see [README](README.md) for envelope, auth headers, and transport conventions.

---

### `listWikiPages` — `GET /knowledgebase/:kb_id/wiki/pages`

handler: `ListPages` · `wiki_page.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `page_type` | string |  | Filter by page type; comma-separated for multiple (e.g. entity,concept) |
| `status` | string |  | Filter by status |
| `query` | string |  | Full-text search |
| `page` | integer |  | Page number |
| `page_size` | integer |  | Page size |
| `sort_by` | string |  | Sort field |
| `sort_order` | string |  | Sort order (asc/desc) |
| `category_path` | string |  |  |
| `category_depth` | string |  |  |

**Response** `WikiPageListResponse`:
| field | type | req | notes |
|---|---|---|---|
| `pages` | *WikiPage[] | yes |  |
| `total` | number | yes |  |
| `page` | number | yes |  |
| `page_size` | number | yes |  |
| `total_pages` | number | yes |  |

---
### `listWikiFolders` — `GET /knowledgebase/:kb_id/wiki/folders`

handler: `ListFolders` · `wiki_page.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `parent_id` | string |  | Parent folder id (empty = root) |
| `page_types` | string |  |  |

**Response** `WikiFolderListResponse`:
| field | type | req | notes |
|---|---|---|---|
| `parent_id` | string | yes |  |
| `folders` | WikiFolderNode[] | yes |  |

---
### `createWikiFolder` — `POST /knowledgebase/:kb_id/wiki/folders`

handler: `CreateFolder` · `wiki_page.go`

**Body** `WikiFolderCreateRequest`:
| field | type | req | notes |
|---|---|---|---|
| `parent_id` | string |  |  |
| `name` | string |  |  |

**Response** `WikiFolder`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes |  |
| `tenant_id` | number | yes |  |
| `knowledge_base_id` | string | yes |  |
| `parent_id` | string | yes |  |
| `name` | string | yes |  |
| `path` | string | yes |  |
| `depth` | number | yes |  |
| `sort_order` | number | yes |  |
| `created_at` | string(time) | yes |  |
| `updated_at` | string(time) | yes |  |
| `deleted_at` | DeletedAt | yes |  |

---
### `updateWikiFolder` — `PUT /knowledgebase/:kb_id/wiki/folders/:folder_id`

handler: `UpdateFolder` · `wiki_page.go`

**Body** `WikiFolderUpdateRequest`:
| field | type | req | notes |
|---|---|---|---|
| `name` | string |  |  |
| `parent_id` | string |  |  |
| `move_parent` | boolean |  |  |

**Response** `WikiFolder`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes |  |
| `tenant_id` | number | yes |  |
| `knowledge_base_id` | string | yes |  |
| `parent_id` | string | yes |  |
| `name` | string | yes |  |
| `path` | string | yes |  |
| `depth` | number | yes |  |
| `sort_order` | number | yes |  |
| `created_at` | string(time) | yes |  |
| `updated_at` | string(time) | yes |  |
| `deleted_at` | DeletedAt | yes |  |

---
### `deleteWikiFolder` — `DELETE /knowledgebase/:kb_id/wiki/folders/:folder_id`

handler: `DeleteFolder` · `wiki_page.go`

**Response**: `204 No Content`
---
### `moveWikiPage` — `PUT /knowledgebase/:kb_id/wiki/move-page`

handler: `MovePage` · `wiki_page.go`

**Body** `WikiPageMoveRequest`:
| field | type | req | notes |
|---|---|---|---|
| `slug` | string | yes |  |
| `folder_id` | string |  |  |

**Response** `WikiPage`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes | Unique identifier (UUID) |
| `tenant_id` | number | yes | Workspace ID for multi-workspace isolation |
| `knowledge_base_id` | string | yes | Knowledge base this page belongs to |
| `slug` | string | yes | URL-friendly slug for addressing, e.g. "entity/acme-corp", "concept/rag" Unique within a knowledge base |
| `title` | string | yes | Human-readable title |
| `page_type` | string | yes | Page type: summary, entity, concept, index, synthesis, comparison |
| `status` | string | yes | Page status: draft, published, archived |
| `content` | string | yes | Full markdown content |
| `summary` | string | yes | One-line summary for index listing |
| `aliases` | StringArray | yes | Alternate names, abbreviations, acronyms or translated names |
| `parent_slug` | string |  | ParentSlug optionally points at the wiki page that should act as this page's semantic parent in the directory tree. The parent may be empty when the page is grouped only by FolderI |
| `folder_id` | string |  | FolderID is the single source of truth for where this page sits in the directory tree — a reference to wiki_folders.id ("" = wiki root). The CategoryPath / WikiPath / Depth fields  |
| `category_path` | StringArray |  | CategoryPath is the directory breadcrumb that groups this page in the wiki browser, e.g. ["AI", "LLM Apps", "RAG"]. Derived cache of the folder chain identified by FolderID. |
| `wiki_path` | string |  | WikiPath is a normalized, sortable path derived from page_type, category_path, and title. It keeps large directory listings cheap to sort. |
| `depth` | number |  | Depth is len(CategoryPath), cached for filtering / display. |
| `sort_order` | number |  | SortOrder allows generated or manually edited pages to control sibling ordering before falling back to title. |
| `source_refs` | StringArray | yes | References to source knowledge IDs that contributed to this page. Format matches the legacy "<knowledge_id>|<doc_title>" convention used across the ingest pipeline, so retract / di |
| `chunk_refs` | StringArray | yes | ChunkRefs records the specific source-document chunks this page was built from — one UUID per cited chunk. Populated during ingest from the chunk-citation pass; refreshed wholesale |
| `in_links` | StringArray | yes | Slugs of pages that link TO this page (backlinks) |
| `out_links` | StringArray | yes | Slugs of pages this page links to (outbound links) |
| `page_metadata` | JSON | yes | Arbitrary metadata (tags, categories, dates, etc.) |
| `version` | number | yes | Version number. Incremented only when a user-visible content field (title, content, summary, page_type, status) actually changes; pure bookkeeping writes (link maintenance, same-co |
| `last_edit_source` | string |  | LastEditSource records who authored the CURRENT version: pipeline | agent | user | revert. Empty for legacy rows (treated as pipeline). When the version is superseded this value tr |
| `last_editor_id` | string |  | LastEditorID is the user id of the caller that produced the current version (empty for background pipeline writes). |
| `created_at` | string(time) | yes | Creation time |
| `updated_at` | string(time) | yes | Last update time |
| `deleted_at` | DeletedAt | yes | Soft delete |

---
### `createWikiPage` — `POST /knowledgebase/:kb_id/wiki/pages`

handler: `CreatePage` · `wiki_page.go`

**Body** `WikiPage`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string |  | Unique identifier (UUID) |
| `tenant_id` | number |  | Workspace ID for multi-workspace isolation |
| `knowledge_base_id` | string |  | Knowledge base this page belongs to |
| `slug` | string |  | URL-friendly slug for addressing, e.g. "entity/acme-corp", "concept/rag" Unique within a knowledge base |
| `title` | string |  | Human-readable title |
| `page_type` | string |  | Page type: summary, entity, concept, index, synthesis, comparison |
| `status` | string |  | Page status: draft, published, archived |
| `content` | string |  | Full markdown content |
| `summary` | string |  | One-line summary for index listing |
| `aliases` | StringArray |  | Alternate names, abbreviations, acronyms or translated names |
| `parent_slug` | string |  | ParentSlug optionally points at the wiki page that should act as this page's semantic parent in the directory tree. The parent may be empty when the page is grouped only by FolderI |
| `folder_id` | string |  | FolderID is the single source of truth for where this page sits in the directory tree — a reference to wiki_folders.id ("" = wiki root). The CategoryPath / WikiPath / Depth fields  |
| `category_path` | StringArray |  | CategoryPath is the directory breadcrumb that groups this page in the wiki browser, e.g. ["AI", "LLM Apps", "RAG"]. Derived cache of the folder chain identified by FolderID. |
| `wiki_path` | string |  | WikiPath is a normalized, sortable path derived from page_type, category_path, and title. It keeps large directory listings cheap to sort. |
| `depth` | number |  | Depth is len(CategoryPath), cached for filtering / display. |
| `sort_order` | number |  | SortOrder allows generated or manually edited pages to control sibling ordering before falling back to title. |
| `source_refs` | StringArray |  | References to source knowledge IDs that contributed to this page. Format matches the legacy "<knowledge_id>|<doc_title>" convention used across the ingest pipeline, so retract / di |
| `chunk_refs` | StringArray |  | ChunkRefs records the specific source-document chunks this page was built from — one UUID per cited chunk. Populated during ingest from the chunk-citation pass; refreshed wholesale |
| `in_links` | StringArray |  | Slugs of pages that link TO this page (backlinks) |
| `out_links` | StringArray |  | Slugs of pages this page links to (outbound links) |
| `page_metadata` | JSON |  | Arbitrary metadata (tags, categories, dates, etc.) |
| `version` | number |  | Version number. Incremented only when a user-visible content field (title, content, summary, page_type, status) actually changes; pure bookkeeping writes (link maintenance, same-co |
| `last_edit_source` | string |  | LastEditSource records who authored the CURRENT version: pipeline | agent | user | revert. Empty for legacy rows (treated as pipeline). When the version is superseded this value tr |
| `last_editor_id` | string |  | LastEditorID is the user id of the caller that produced the current version (empty for background pipeline writes). |
| `created_at` | string(time) |  | Creation time |
| `updated_at` | string(time) |  | Last update time |
| `deleted_at` | DeletedAt |  | Soft delete |

**Response** `WikiPage`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes | Unique identifier (UUID) |
| `tenant_id` | number | yes | Workspace ID for multi-workspace isolation |
| `knowledge_base_id` | string | yes | Knowledge base this page belongs to |
| `slug` | string | yes | URL-friendly slug for addressing, e.g. "entity/acme-corp", "concept/rag" Unique within a knowledge base |
| `title` | string | yes | Human-readable title |
| `page_type` | string | yes | Page type: summary, entity, concept, index, synthesis, comparison |
| `status` | string | yes | Page status: draft, published, archived |
| `content` | string | yes | Full markdown content |
| `summary` | string | yes | One-line summary for index listing |
| `aliases` | StringArray | yes | Alternate names, abbreviations, acronyms or translated names |
| `parent_slug` | string |  | ParentSlug optionally points at the wiki page that should act as this page's semantic parent in the directory tree. The parent may be empty when the page is grouped only by FolderI |
| `folder_id` | string |  | FolderID is the single source of truth for where this page sits in the directory tree — a reference to wiki_folders.id ("" = wiki root). The CategoryPath / WikiPath / Depth fields  |
| `category_path` | StringArray |  | CategoryPath is the directory breadcrumb that groups this page in the wiki browser, e.g. ["AI", "LLM Apps", "RAG"]. Derived cache of the folder chain identified by FolderID. |
| `wiki_path` | string |  | WikiPath is a normalized, sortable path derived from page_type, category_path, and title. It keeps large directory listings cheap to sort. |
| `depth` | number |  | Depth is len(CategoryPath), cached for filtering / display. |
| `sort_order` | number |  | SortOrder allows generated or manually edited pages to control sibling ordering before falling back to title. |
| `source_refs` | StringArray | yes | References to source knowledge IDs that contributed to this page. Format matches the legacy "<knowledge_id>|<doc_title>" convention used across the ingest pipeline, so retract / di |
| `chunk_refs` | StringArray | yes | ChunkRefs records the specific source-document chunks this page was built from — one UUID per cited chunk. Populated during ingest from the chunk-citation pass; refreshed wholesale |
| `in_links` | StringArray | yes | Slugs of pages that link TO this page (backlinks) |
| `out_links` | StringArray | yes | Slugs of pages this page links to (outbound links) |
| `page_metadata` | JSON | yes | Arbitrary metadata (tags, categories, dates, etc.) |
| `version` | number | yes | Version number. Incremented only when a user-visible content field (title, content, summary, page_type, status) actually changes; pure bookkeeping writes (link maintenance, same-co |
| `last_edit_source` | string |  | LastEditSource records who authored the CURRENT version: pipeline | agent | user | revert. Empty for legacy rows (treated as pipeline). When the version is superseded this value tr |
| `last_editor_id` | string |  | LastEditorID is the user id of the caller that produced the current version (empty for background pipeline writes). |
| `created_at` | string(time) | yes | Creation time |
| `updated_at` | string(time) | yes | Last update time |
| `deleted_at` | DeletedAt | yes | Soft delete |

---
### `getWikiPage` — `GET /knowledgebase/{kb_id}/wiki/pages/{slug}`

**Response** `WikiPage`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes | Unique identifier (UUID) |
| `tenant_id` | number | yes | Workspace ID for multi-workspace isolation |
| `knowledge_base_id` | string | yes | Knowledge base this page belongs to |
| `slug` | string | yes | URL-friendly slug for addressing, e.g. "entity/acme-corp", "concept/rag" Unique within a knowledge base |
| `title` | string | yes | Human-readable title |
| `page_type` | string | yes | Page type: summary, entity, concept, index, synthesis, comparison |
| `status` | string | yes | Page status: draft, published, archived |
| `content` | string | yes | Full markdown content |
| `summary` | string | yes | One-line summary for index listing |
| `aliases` | StringArray | yes | Alternate names, abbreviations, acronyms or translated names |
| `parent_slug` | string |  | ParentSlug optionally points at the wiki page that should act as this page's semantic parent in the directory tree. The parent may be empty when the page is grouped only by FolderI |
| `folder_id` | string |  | FolderID is the single source of truth for where this page sits in the directory tree — a reference to wiki_folders.id ("" = wiki root). The CategoryPath / WikiPath / Depth fields  |
| `category_path` | StringArray |  | CategoryPath is the directory breadcrumb that groups this page in the wiki browser, e.g. ["AI", "LLM Apps", "RAG"]. Derived cache of the folder chain identified by FolderID. |
| `wiki_path` | string |  | WikiPath is a normalized, sortable path derived from page_type, category_path, and title. It keeps large directory listings cheap to sort. |
| `depth` | number |  | Depth is len(CategoryPath), cached for filtering / display. |
| `sort_order` | number |  | SortOrder allows generated or manually edited pages to control sibling ordering before falling back to title. |
| `source_refs` | StringArray | yes | References to source knowledge IDs that contributed to this page. Format matches the legacy "<knowledge_id>|<doc_title>" convention used across the ingest pipeline, so retract / di |
| `chunk_refs` | StringArray | yes | ChunkRefs records the specific source-document chunks this page was built from — one UUID per cited chunk. Populated during ingest from the chunk-citation pass; refreshed wholesale |
| `in_links` | StringArray | yes | Slugs of pages that link TO this page (backlinks) |
| `out_links` | StringArray | yes | Slugs of pages this page links to (outbound links) |
| `page_metadata` | JSON | yes | Arbitrary metadata (tags, categories, dates, etc.) |
| `version` | number | yes | Version number. Incremented only when a user-visible content field (title, content, summary, page_type, status) actually changes; pure bookkeeping writes (link maintenance, same-co |
| `last_edit_source` | string |  | LastEditSource records who authored the CURRENT version: pipeline | agent | user | revert. Empty for legacy rows (treated as pipeline). When the version is superseded this value tr |
| `last_editor_id` | string |  | LastEditorID is the user id of the caller that produced the current version (empty for background pipeline writes). |
| `created_at` | string(time) | yes | Creation time |
| `updated_at` | string(time) | yes | Last update time |
| `deleted_at` | DeletedAt | yes | Soft delete |

---
### `updateWikiPage` — `PUT /knowledgebase/{kb_id}/wiki/pages/{slug}`

**Body** `WikiPageUpdateRequest`:
| field | type | req | notes |
|---|---|---|---|
| `title` | string |  |  |
| `content` | string |  |  |
| `summary` | string |  |  |
| `page_type` | string |  |  |
| `status` | string |  |  |
| `aliases` | StringArray |  |  |
| `version` | number |  | Version is the optimistic-lock guard: when > 0 the update is rejected with a conflict if the stored version differs (someone else edited the page since the client loaded it). 0 ski |

**Response** `WikiPage`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes | Unique identifier (UUID) |
| `tenant_id` | number | yes | Workspace ID for multi-workspace isolation |
| `knowledge_base_id` | string | yes | Knowledge base this page belongs to |
| `slug` | string | yes | URL-friendly slug for addressing, e.g. "entity/acme-corp", "concept/rag" Unique within a knowledge base |
| `title` | string | yes | Human-readable title |
| `page_type` | string | yes | Page type: summary, entity, concept, index, synthesis, comparison |
| `status` | string | yes | Page status: draft, published, archived |
| `content` | string | yes | Full markdown content |
| `summary` | string | yes | One-line summary for index listing |
| `aliases` | StringArray | yes | Alternate names, abbreviations, acronyms or translated names |
| `parent_slug` | string |  | ParentSlug optionally points at the wiki page that should act as this page's semantic parent in the directory tree. The parent may be empty when the page is grouped only by FolderI |
| `folder_id` | string |  | FolderID is the single source of truth for where this page sits in the directory tree — a reference to wiki_folders.id ("" = wiki root). The CategoryPath / WikiPath / Depth fields  |
| `category_path` | StringArray |  | CategoryPath is the directory breadcrumb that groups this page in the wiki browser, e.g. ["AI", "LLM Apps", "RAG"]. Derived cache of the folder chain identified by FolderID. |
| `wiki_path` | string |  | WikiPath is a normalized, sortable path derived from page_type, category_path, and title. It keeps large directory listings cheap to sort. |
| `depth` | number |  | Depth is len(CategoryPath), cached for filtering / display. |
| `sort_order` | number |  | SortOrder allows generated or manually edited pages to control sibling ordering before falling back to title. |
| `source_refs` | StringArray | yes | References to source knowledge IDs that contributed to this page. Format matches the legacy "<knowledge_id>|<doc_title>" convention used across the ingest pipeline, so retract / di |
| `chunk_refs` | StringArray | yes | ChunkRefs records the specific source-document chunks this page was built from — one UUID per cited chunk. Populated during ingest from the chunk-citation pass; refreshed wholesale |
| `in_links` | StringArray | yes | Slugs of pages that link TO this page (backlinks) |
| `out_links` | StringArray | yes | Slugs of pages this page links to (outbound links) |
| `page_metadata` | JSON | yes | Arbitrary metadata (tags, categories, dates, etc.) |
| `version` | number | yes | Version number. Incremented only when a user-visible content field (title, content, summary, page_type, status) actually changes; pure bookkeeping writes (link maintenance, same-co |
| `last_edit_source` | string |  | LastEditSource records who authored the CURRENT version: pipeline | agent | user | revert. Empty for legacy rows (treated as pipeline). When the version is superseded this value tr |
| `last_editor_id` | string |  | LastEditorID is the user id of the caller that produced the current version (empty for background pipeline writes). |
| `created_at` | string(time) | yes | Creation time |
| `updated_at` | string(time) | yes | Last update time |
| `deleted_at` | DeletedAt | yes | Soft delete |

---
### `listWikiRevisions` — `GET /knowledgebase/{kb_id}/wiki/revisions/{slug}`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `version` | integer |  | Return this single revision with content |
| `limit` | integer |  | Page size (default 50, max 200) |
| `offset` | integer |  | Offset into the newest-first list |

**Response** `WikiPageRevisionListResponse`:
| field | type | req | notes |
|---|---|---|---|
| `revisions` | *WikiPageRevision[] | yes |  |
| `total` | number | yes |  |
| `current_version` | number | yes |  |

---
### `getWikiRevision` — `GET /knowledgebase/{kb_id}/wiki/revisions/{slug}`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `version` | integer |  | Return this single revision with content |
| `limit` | integer |  | Page size (default 50, max 200) |
| `offset` | integer |  | Offset into the newest-first list |

**Response** `WikiPageRevisionListResponse`:
| field | type | req | notes |
|---|---|---|---|
| `revisions` | *WikiPageRevision[] | yes |  |
| `total` | number | yes |  |
| `current_version` | number | yes |  |

---
### `revertWikiPage` — `POST /knowledgebase/:kb_id/wiki/revert`

handler: `RevertPage` · `wiki_page.go`

**Body** `WikiPageRevertRequest`:
| field | type | req | notes |
|---|---|---|---|
| `slug` | string | yes |  |
| `version` | number | yes |  |

**Response** `WikiPage`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes | Unique identifier (UUID) |
| `tenant_id` | number | yes | Workspace ID for multi-workspace isolation |
| `knowledge_base_id` | string | yes | Knowledge base this page belongs to |
| `slug` | string | yes | URL-friendly slug for addressing, e.g. "entity/acme-corp", "concept/rag" Unique within a knowledge base |
| `title` | string | yes | Human-readable title |
| `page_type` | string | yes | Page type: summary, entity, concept, index, synthesis, comparison |
| `status` | string | yes | Page status: draft, published, archived |
| `content` | string | yes | Full markdown content |
| `summary` | string | yes | One-line summary for index listing |
| `aliases` | StringArray | yes | Alternate names, abbreviations, acronyms or translated names |
| `parent_slug` | string |  | ParentSlug optionally points at the wiki page that should act as this page's semantic parent in the directory tree. The parent may be empty when the page is grouped only by FolderI |
| `folder_id` | string |  | FolderID is the single source of truth for where this page sits in the directory tree — a reference to wiki_folders.id ("" = wiki root). The CategoryPath / WikiPath / Depth fields  |
| `category_path` | StringArray |  | CategoryPath is the directory breadcrumb that groups this page in the wiki browser, e.g. ["AI", "LLM Apps", "RAG"]. Derived cache of the folder chain identified by FolderID. |
| `wiki_path` | string |  | WikiPath is a normalized, sortable path derived from page_type, category_path, and title. It keeps large directory listings cheap to sort. |
| `depth` | number |  | Depth is len(CategoryPath), cached for filtering / display. |
| `sort_order` | number |  | SortOrder allows generated or manually edited pages to control sibling ordering before falling back to title. |
| `source_refs` | StringArray | yes | References to source knowledge IDs that contributed to this page. Format matches the legacy "<knowledge_id>|<doc_title>" convention used across the ingest pipeline, so retract / di |
| `chunk_refs` | StringArray | yes | ChunkRefs records the specific source-document chunks this page was built from — one UUID per cited chunk. Populated during ingest from the chunk-citation pass; refreshed wholesale |
| `in_links` | StringArray | yes | Slugs of pages that link TO this page (backlinks) |
| `out_links` | StringArray | yes | Slugs of pages this page links to (outbound links) |
| `page_metadata` | JSON | yes | Arbitrary metadata (tags, categories, dates, etc.) |
| `version` | number | yes | Version number. Incremented only when a user-visible content field (title, content, summary, page_type, status) actually changes; pure bookkeeping writes (link maintenance, same-co |
| `last_edit_source` | string |  | LastEditSource records who authored the CURRENT version: pipeline | agent | user | revert. Empty for legacy rows (treated as pipeline). When the version is superseded this value tr |
| `last_editor_id` | string |  | LastEditorID is the user id of the caller that produced the current version (empty for background pipeline writes). |
| `created_at` | string(time) | yes | Creation time |
| `updated_at` | string(time) | yes | Last update time |
| `deleted_at` | DeletedAt | yes | Soft delete |

---
### `getWikiIndex` — `GET /knowledgebase/:kb_id/wiki/index`

handler: `GetIndex` · `wiki_page.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `types` | string |  | Comma-separated page types (default: all content types) |
| `limit` | integer |  | Per-group window size, 1-200 (default 50) |
| `cursor` | string |  | Opaque offset cursor from previous response |

**Response** `WikiIndexResponse`:
| field | type | req | notes |
|---|---|---|---|
| `intro` | string | yes |  |
| `version` | number | yes |  |
| `groups` | WikiIndexGroup[] | yes |  |

---
### `getWikiGraph` — `GET /knowledgebase/:kb_id/wiki/graph`

handler: `GetGraph` · `wiki_page.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `mode` | string |  | overview (default) | ego |
| `center` | string |  | Center slug for ego mode |
| `depth` | integer |  | Ego BFS depth (1-3, default 1) |
| `types` | string |  | Comma-separated page_type allow-list |
| `limit` | integer |  | Max nodes to return (default 500, max 2000) |

**Response** `WikiGraphData`:
| field | type | req | notes |
|---|---|---|---|
| `nodes` | WikiGraphNode[] | yes |  |
| `edges` | WikiGraphEdge[] | yes |  |
| `meta` | WikiGraphMeta | yes |  |

---
### `getWikiStats` — `GET /knowledgebase/:kb_id/wiki/stats`

handler: `GetStats` · `wiki_page.go`

**Response** `WikiStats`:
| field | type | req | notes |
|---|---|---|---|
| `total_pages` | number | yes |  |
| `pages_by_type` | map[string]int64 | yes |  |
| `total_links` | number | yes |  |
| `orphan_count` | number | yes |  |
| `recent_updates` | *WikiPage[] | yes |  |
| `pending_tasks` | number | yes |  |
| `pending_issues` | number | yes |  |
| `is_active` | boolean | yes |  |

---
### `searchWikiPages` — `GET /knowledgebase/:kb_id/wiki/search`

handler: `SearchPages` · `wiki_page.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `q` | string | yes | Search query |
| `limit` | integer |  | Max results (default 10) |

**Response**: `WikiPage[]`
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes | Unique identifier (UUID) |
| `tenant_id` | number | yes | Workspace ID for multi-workspace isolation |
| `knowledge_base_id` | string | yes | Knowledge base this page belongs to |
| `slug` | string | yes | URL-friendly slug for addressing, e.g. "entity/acme-corp", "concept/rag" Unique within a knowledge base |
| `title` | string | yes | Human-readable title |
| `page_type` | string | yes | Page type: summary, entity, concept, index, synthesis, comparison |
| `status` | string | yes | Page status: draft, published, archived |
| `content` | string | yes | Full markdown content |
| `summary` | string | yes | One-line summary for index listing |
| `aliases` | StringArray | yes | Alternate names, abbreviations, acronyms or translated names |
| `parent_slug` | string |  | ParentSlug optionally points at the wiki page that should act as this page's semantic parent in the directory tree. The parent may be empty when the page is grouped only by FolderI |
| `folder_id` | string |  | FolderID is the single source of truth for where this page sits in the directory tree — a reference to wiki_folders.id ("" = wiki root). The CategoryPath / WikiPath / Depth fields  |
| `category_path` | StringArray |  | CategoryPath is the directory breadcrumb that groups this page in the wiki browser, e.g. ["AI", "LLM Apps", "RAG"]. Derived cache of the folder chain identified by FolderID. |
| `wiki_path` | string |  | WikiPath is a normalized, sortable path derived from page_type, category_path, and title. It keeps large directory listings cheap to sort. |
| `depth` | number |  | Depth is len(CategoryPath), cached for filtering / display. |
| `sort_order` | number |  | SortOrder allows generated or manually edited pages to control sibling ordering before falling back to title. |
| `source_refs` | StringArray | yes | References to source knowledge IDs that contributed to this page. Format matches the legacy "<knowledge_id>|<doc_title>" convention used across the ingest pipeline, so retract / di |
| `chunk_refs` | StringArray | yes | ChunkRefs records the specific source-document chunks this page was built from — one UUID per cited chunk. Populated during ingest from the chunk-citation pass; refreshed wholesale |
| `in_links` | StringArray | yes | Slugs of pages that link TO this page (backlinks) |
| `out_links` | StringArray | yes | Slugs of pages this page links to (outbound links) |
| `page_metadata` | JSON | yes | Arbitrary metadata (tags, categories, dates, etc.) |
| `version` | number | yes | Version number. Incremented only when a user-visible content field (title, content, summary, page_type, status) actually changes; pure bookkeeping writes (link maintenance, same-co |
| `last_edit_source` | string |  | LastEditSource records who authored the CURRENT version: pipeline | agent | user | revert. Empty for legacy rows (treated as pipeline). When the version is superseded this value tr |
| `last_editor_id` | string |  | LastEditorID is the user id of the caller that produced the current version (empty for background pipeline writes). |
| `created_at` | string(time) | yes | Creation time |
| `updated_at` | string(time) | yes | Last update time |
| `deleted_at` | DeletedAt | yes | Soft delete |

---
### `listWikiIssues` — `GET /knowledgebase/:kb_id/wiki/issues`

handler: `ListIssues` · `wiki_page.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `slug` | string |  | Filter by page slug |
| `status` | string |  | Filter by status (pending, ignored, resolved) |

**Response**: `WikiPageIssue[]`
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes |  |
| `tenant_id` | number | yes |  |
| `knowledge_base_id` | string | yes |  |
| `slug` | string | yes |  |
| `issue_type` | string | yes |  |
| `description` | string | yes |  |
| `suspected_knowledge_ids` | StringArray | yes |  |
| `status` | string | yes |  |
| `reported_by` | string | yes |  |
| `created_at` | string(time) | yes |  |
| `updated_at` | string(time) | yes |  |
| `deleted_at` | DeletedAt | yes |  |

---
### `updateWikiIssueStatus` — `PUT /knowledgebase/:kb_id/wiki/issues/:issue_id/status`

handler: `UpdateIssueStatus` · `wiki_page.go`

**Body** (inline struct):
| field | type | req | notes |
|---|---|---|---|
| `status` | string | yes |  |

**Response**:
```json
{ message: string }
```
---
### `rebuildWikiLinks` — `POST /knowledgebase/:kb_id/wiki/rebuild-links`

handler: `RebuildLinks` · `wiki_page.go`

**Response**:
```json
{ message: string }
```
---

## Referenced types

#### `WikiFolderNode`
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes |  |
| `tenant_id` | number | yes |  |
| `knowledge_base_id` | string | yes |  |
| `parent_id` | string | yes |  |
| `name` | string | yes |  |
| `path` | string | yes |  |
| `depth` | number | yes |  |
| `sort_order` | number | yes |  |
| `created_at` | string(time) | yes |  |
| `updated_at` | string(time) | yes |  |
| `deleted_at` | DeletedAt | yes |  |
| `page_count` | number | yes |  |
| `has_children` | boolean | yes |  |

#### `WikiGraphEdge`
| field | type | req | notes |
|---|---|---|---|
| `source` | string | yes |  |
| `target` | string | yes |  |

#### `WikiGraphMeta`
| field | type | req | notes |
|---|---|---|---|
| `mode` | string | yes |  |
| `total` | number | yes |  |
| `returned` | number | yes |  |
| `truncated` | boolean | yes |  |
| `center` | string |  |  |
| `depth` | number |  |  |
| `familiar_count` | number |  | FamiliarCount is how many returned nodes are lit up for this person. |

#### `WikiGraphNode`
| field | type | req | notes |
|---|---|---|---|
| `slug` | string | yes |  |
| `title` | string | yes |  |
| `page_type` | string | yes |  |
| `link_count` | number | yes | Number of inbound + outbound links |
| `familiar` | boolean |  | Familiar is true when this page was built from a document this person keeps citing in answers. It is a personal overlay, not a property of the page: two people looking at the same  |

#### `WikiIndexGroup`
| field | type | req | notes |
|---|---|---|---|
| `type` | string | yes |  |
| `total` | number | yes |  |
| `items` | WikiIndexEntry[] | yes |  |
| `next_cursor` | string |  |  |
