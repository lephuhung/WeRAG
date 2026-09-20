# Organizations & Sharing API

Frontend module: `frontend-next/lib/api/organizations.ts` — see [README](README.md) for envelope, auth headers, and transport conventions.

---

### `createOrganization` — `POST /organizations`

handler: `CreateOrganization` · `organization.go`

**Body** `CreateOrganizationRequest`:
| field | type | req | notes |
|---|---|---|---|
| `name` | string | yes |  |
| `description` | string |  |  |
| `avatar` | string |  |  |
| `invite_code_validity_days` | number |  |  |
| `member_limit` | number |  |  |

**Response**:
```json
{ success: true, data: h.toOrgResponse(ctx }
```
---
### `getOrganization` — `GET /organizations/:id`

handler: `GetOrganization` · `organization.go`

**Response**:
```json
{ success: true, data: h.toOrgResponse(ctx }
```
---
### `listMyOrganizations` — `GET /organizations`

handler: `ListMyOrganizations` · `organization.go`

**Response** `ListOrganizationsResponse`:
| field | type | req | notes |
|---|---|---|---|
| `organizations` | OrganizationResponse[] | yes |  |
| `total` | number | yes |  |
| `resource_counts` | ResourceCountsByOrgResponse |  |  |

---
### `updateOrganization` — `PUT /organizations/:id`

handler: `UpdateOrganization` · `organization.go`

**Body** `UpdateOrganizationRequest`:
| field | type | req | notes |
|---|---|---|---|
| `name` | string |  |  |
| `description` | string |  |  |
| `avatar` | string |  |  |
| `require_approval` | boolean |  |  |
| `searchable` | boolean |  |  |
| `invite_code_validity_days` | number |  |  |
| `member_limit` | number |  |  |

**Response**:
```json
{ success: true, data: h.toOrgResponse(ctx }
```
---
### `deleteOrganization` — `DELETE /organizations/:id`

handler: `DeleteOrganization` · `organization.go`

**Response**:
```json
{ success: true, message: string }
```
---
### `joinOrganization` — `POST /organizations/join`

handler: `JoinByInviteCode` · `organization.go`

**Body** `JoinOrganizationRequest`:
| field | type | req | notes |
|---|---|---|---|
| `invite_code` | string | yes |  |

**Response**:
```json
{ success: true, data: h.toOrgResponse(ctx }
```
---
### `submitJoinRequest` — `POST /organizations/join-request`

handler: `SubmitJoinRequest` · `organization.go`

**Body** `SubmitJoinRequestRequest`:
| field | type | req | notes |
|---|---|---|---|
| `invite_code` | string | yes |  |
| `message` | string |  |  |
| `role` | OrgMemberRole |  |  |

**Response**:
```json
{ success: true, data: types.OrganizationJoinRequest }
```
`data` is `OrganizationJoinRequest`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes | Unique identifier |
| `organization_id` | string | yes | Organization ID |
| `user_id` | string | yes | User ID of the requester |
| `tenant_id` | number | yes | Tenant ID of the requester |
| `request_type` | JoinRequestType | yes | Type of request: 'join' for new member, 'upgrade' for role upgrade |
| `prev_role` | OrgMemberRole | yes | Previous role before upgrade (only for upgrade requests) |
| `requested_role` | OrgMemberRole | yes | Role requested by the applicant (admin/editor/viewer) |
| `status` | JoinRequestStatus | yes | Status of the request |
| `message` | string | yes | Optional message from the requester |
| `reviewed_by` | string | yes | User ID of the admin who reviewed the request |
| `reviewed_at` | string(time) | yes | Time when the request was reviewed |
| `review_message` | string | yes | Optional message from the reviewer |
| `created_at` | string(time) | yes | Creation time |
| `updated_at` | string(time) | yes | Last updated time |
| `organization` | Organization |  | Associations (not stored in database) |
| `user` | User |  |  |
| `reviewer` | User |  |  |

---
### `previewOrganization` — `GET /organizations/preview/:code`

handler: `PreviewByInviteCode` · `organization.go`

**Response**:
```json
{ success: true, data: {id, name, description, avatar, member_count, share_count, agent_share_count, is_already_member, require_approval, created_at} }
```
---
### `searchSearchableOrganizations` — `GET /organizations/search`

handler: `SearchOrganizations` · `organization.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `q` | string |  | search keyword (space name or description) |
| `limit` | integer |  | max number to return |

**Response**:
```json
{ success: true, data: SearchableOrganizationItem[], total: number }
```
---
### `joinOrganizationById` — `POST /organizations/join-by-id`

handler: `JoinByOrganizationID` · `organization.go`

**Body** `JoinByOrganizationIDRequest`:
| field | type | req | notes |
|---|---|---|---|
| `organization_id` | string | yes |  |
| `message` | string |  |  |
| `role` | OrgMemberRole |  |  |

**Response**:
```json
{ success: true, data: h.toOrgResponse(ctx }
```
---
### `leaveOrganization` — `POST /organizations/:id/leave`

handler: `LeaveOrganization` · `organization.go`

**Response**:
```json
{ success: true, message: string }
```
---
### `requestRoleUpgrade` — `POST /organizations/:id/request-upgrade`

handler: `RequestRoleUpgrade` · `organization.go`

**Body** `RequestRoleUpgradeRequest`:
| field | type | req | notes |
|---|---|---|---|
| `requested_role` | OrgMemberRole | yes |  |
| `message` | string |  |  |

**Response**:
```json
{ success: true, data: types.OrganizationJoinRequest }
```
`data` is `OrganizationJoinRequest`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes | Unique identifier |
| `organization_id` | string | yes | Organization ID |
| `user_id` | string | yes | User ID of the requester |
| `tenant_id` | number | yes | Tenant ID of the requester |
| `request_type` | JoinRequestType | yes | Type of request: 'join' for new member, 'upgrade' for role upgrade |
| `prev_role` | OrgMemberRole | yes | Previous role before upgrade (only for upgrade requests) |
| `requested_role` | OrgMemberRole | yes | Role requested by the applicant (admin/editor/viewer) |
| `status` | JoinRequestStatus | yes | Status of the request |
| `message` | string | yes | Optional message from the requester |
| `reviewed_by` | string | yes | User ID of the admin who reviewed the request |
| `reviewed_at` | string(time) | yes | Time when the request was reviewed |
| `review_message` | string | yes | Optional message from the reviewer |
| `created_at` | string(time) | yes | Creation time |
| `updated_at` | string(time) | yes | Last updated time |
| `organization` | Organization |  | Associations (not stored in database) |
| `user` | User |  |  |
| `reviewer` | User |  |  |

---
### `generateInviteCode` — `POST /organizations/:id/invite-code`

handler: `GenerateInviteCode` · `organization.go`

**Response**:
```json
{ success: true, data: {invite_code} }
```
---
### `listOrganizationMembers` — `GET /organizations/:id/members`

handler: `ListMembers` · `tenant_org.go`

**Response** `ListMembersResponse`:
| field | type | req | notes |
|---|---|---|---|
| `members` | OrganizationMemberResponse[] | yes |  |
| `total` | number | yes |  |

---
### `updateOrgMemberRole` — `PUT /organizations/:id/members/:tenant_id`

handler: `UpdateMemberRole` · `tenant_org.go`

**Body** `UpdateMemberRoleRequest`:
| field | type | req | notes |
|---|---|---|---|
| `role` | OrgMemberRole | yes |  |

**Response**:
```json
{ success: true }
```
---
### `removeOrgMember` — `DELETE /organizations/:id/members/:tenant_id`

handler: `RemoveMember` · `tenant_org.go`

**Response**:
```json
{ success: true }
```
---
### `listJoinRequests` — `GET /organizations/:id/join-requests`

handler: `ListJoinRequests` · `organization.go`

**Response**:
```json
{ success: true, data: ListJoinRequestsResponse }
```
`data` is `ListJoinRequestsResponse`:
| field | type | req | notes |
|---|---|---|---|
| `requests` | JoinRequestResponse[] | yes |  |
| `total` | number | yes |  |

---
### `reviewJoinRequest` — `PUT /organizations/:id/join-requests/:request_id/review`

handler: `ReviewJoinRequest` · `organization.go`

**Body** `ReviewJoinRequestRequest`:
| field | type | req | notes |
|---|---|---|---|
| `approved` | boolean |  |  |
| `message` | string |  |  |
| `role` | OrgMemberRole |  |  |

**Response**:
```json
{ success: true, message: string }
```
---
### `shareKnowledgeBase` — `POST /knowledge-bases/:id/shares`

handler: `ShareKnowledgeBase` · `organization.go`

**Body** `ShareKnowledgeBaseRequest`:
| field | type | req | notes |
|---|---|---|---|
| `organization_id` | string | yes |  |
| `permission` | OrgMemberRole | yes |  |

**Response**:
```json
{ success: true, data: types.KnowledgeBaseShare }
```
`data` is `KnowledgeBaseShare`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes | Unique identifier |
| `knowledge_base_id` | string | yes | Knowledge base ID being shared |
| `organization_id` | string | yes | Organization ID receiving the share |
| `shared_by_user_id` | string | yes | User ID who shared the knowledge base |
| `source_tenant_id` | number | yes | Original tenant ID of the knowledge base (for cross-tenant embedding model access) |
| `permission` | OrgMemberRole | yes | Permission level (admin/editor/viewer) |
| `created_at` | string(time) | yes | Creation time |
| `updated_at` | string(time) | yes | Last updated time |
| `deleted_at` | DeletedAt | yes | Deletion time (soft delete) |
| `knowledge_base` | KnowledgeBase |  | Associations (not stored in database) |
| `organization` | Organization |  |  |

---
### `listKBShares` — `GET /knowledge-bases/:id/shares`

handler: `ListKBShares` · `organization.go`

**Response** `ListSharesResponse`:
| field | type | req | notes |
|---|---|---|---|
| `shares` | KnowledgeBaseShareResponse[] | yes |  |
| `total` | number | yes |  |

---
### `updateSharePermission` — `PUT /knowledge-bases/:id/shares/:share_id`

handler: `UpdateSharePermission` · `organization.go`

**Body** `UpdateSharePermissionRequest`:
| field | type | req | notes |
|---|---|---|---|
| `permission` | OrgMemberRole | yes |  |

**Response**:
```json
{ success: true, message: string }
```
---
### `removeShare` — `DELETE /knowledge-bases/:id/shares/:share_id`

handler: `RemoveShare` · `organization.go`

**Response**:
```json
{ success: true, message: string }
```
---
### `listSharedKnowledgeBases` — `GET /shared-knowledge-bases`

handler: `ListSharedKnowledgeBases` · `organization.go`

**Response**:
```json
{ success: true, data: rows, total: len(rows) }
```
---
### `listOrganizationSharedKnowledgeBases` — `GET /organizations/:id/shared-knowledge-bases`

handler: `ListOrganizationSharedKnowledgeBases` · `organization.go`

**Response**:
```json
{ success: true, data: rows, total: len(rows) }
```
---
### `listOrgShares` — `GET /organizations/:id/shares`

handler: `ListOrgShares` · `organization.go`

**Response** `ListSharesResponse`:
| field | type | req | notes |
|---|---|---|---|
| `shares` | KnowledgeBaseShareResponse[] | yes |  |
| `total` | number | yes |  |

---
### `shareAgent` — `POST /agents/:id/shares`

handler: `ShareAgent` · `organization.go`

**Body** `ShareKnowledgeBaseRequest`:
| field | type | req | notes |
|---|---|---|---|
| `organization_id` | string | yes |  |
| `permission` | OrgMemberRole | yes |  |

**Response**:
```json
{ success: true, data: types.AgentShare }
```
`data` is `AgentShare`:
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes |  |
| `agent_id` | string | yes |  |
| `organization_id` | string | yes |  |
| `shared_by_user_id` | string | yes |  |
| `source_tenant_id` | number | yes |  |
| `permission` | OrgMemberRole | yes |  |
| `created_at` | string(time) | yes |  |
| `updated_at` | string(time) | yes |  |
| `deleted_at` | DeletedAt | yes |  |
| `agent` | CustomAgent |  |  |
| `organization` | Organization |  |  |

---
### `listAgentShares` — `GET /agents/:id/shares`

handler: `ListAgentShares` · `organization.go`

**Response**:
```json
{ success: true, data: {shares, total} }
```
---
### `removeAgentShare` — `DELETE /agents/:id/shares/:share_id`

handler: `RemoveAgentShare` · `organization.go`

**Response**:
```json
{ success: true, message: string }
```
---
### `listSharedAgents` — `GET /shared-agents`

handler: `ListSharedAgents` · `organization.go`

**Response**:
```json
{ success: true, data: list, total: len(list) }
```
---
### `listOrganizationSharedAgents` — `GET /organizations/:id/shared-agents`

handler: `ListOrganizationSharedAgents` · `organization.go`

**Response**:
```json
{ success: true, data: list, total: len(list) }
```
---
### `setSharedAgentDisabledByMe` — `POST /shared-agents/disabled`

handler: `SetSharedAgentDisabledByMe` · `organization.go`

**Body** `SetSharedAgentDisabledByMeRequest`:
| field | type | req | notes |
|---|---|---|---|
| `agent_id` | string | yes |  |
| `disabled` | boolean |  |  |

**Response**:
```json
{ success: true }
```
---
### `listOrgAgentShares` — `GET /organizations/:id/agent-shares`

handler: `ListOrgAgentShares` · `organization.go`

**Response**:
```json
{ success: true, data: {shares, total} }
```
---
### `searchTenantsForInvite` — `GET /organizations/:id/search-tenants`

handler: `SearchTenantsForInvite` · `organization.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `q` | string | yes | Exact workspace ID |

**Response**:
```json
{ success: true, data: candidates }
```
---
### `inviteMember` — `POST /organizations/:id/invite`

handler: `InviteMember` · `organization.go`

**Body** `InviteMemberRequest`:
| field | type | req | notes |
|---|---|---|---|
| `tenant_id` | number |  | TenantID is the workspace to enrol as an org member. Preferred field. |
| `representative_user_id` | string |  | RepresentativeUserID is accepted for compatibility and ignored: a direct add attaches no user of the enrolled workspace, since the inviter must not choose whose details the roster  |
| `user_id` | string |  | UserID is retained for backward compatibility. When set without TenantID, the handler resolves the user's TenantID. |
| `role` | OrgMemberRole | yes |  |

**Response**:
```json
{ success: true, message: string }
```
---

## Referenced types

#### `CustomAgent`
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

#### `JoinRequestResponse`
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes |  |
| `user_id` | string | yes |  |
| `username` | string | yes |  |
| `email` | string | yes |  |
| `message` | string | yes |  |
| `request_type` | string | yes |  |
| `prev_role` | string | yes |  |
| `requested_role` | string | yes |  |
| `status` | string | yes |  |
| `created_at` | string(time) | yes |  |
| `reviewed_at` | string(time) |  |  |

#### `KnowledgeBase`
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes | Unique identifier of the knowledge base |
| `name` | string | yes | Name of the knowledge base |
| `type` | string | yes | Type of the knowledge base (document, faq, etc.) |
| `is_temporary` | boolean | yes | Whether this knowledge base is temporary (ephemeral) and should be hidden from UI |
| `description` | string | yes | Description of the knowledge base |
| `tenant_id` | number | yes | Workspace ID |
| `creator_id` | string | yes | CreatorID records the user ID of whoever originally created the KB. Used by the workspace-level RBAC middleware to let Contributors edit their own KBs without granting them access  |
| `visibility` | KBVisibility | yes | Visibility controls the read/search scope of this knowledge base. See KBVisibility constants. Rows predating migration 000107 read back as 'tenant' via the column default. |
| `org_id` | number |  | OrgID binds the knowledge base to a tenant org when Visibility is 'org'. Must reference a tenant_orgs row of the same tenant; nil for all other visibilities. |
| `chunking_config` | ChunkingConfig | yes | Chunking configuration |
| `image_processing_config` | ImageProcessingConfig | yes | Image processing configuration |
| `embedding_model_id` | string | yes | ID of the embedding model |
| `summary_model_id` | string | yes | Summary model ID |
| `vlm_config` | VLMConfig | yes | VLM config |
| `asr_config` | ASRConfig | yes | ASR config (Automatic Speech Recognition) |
| `storage_provider_config` | StorageProviderConfig | yes | Storage provider config (new): only stores provider selection; credentials from workspace StorageEngineConfig |
| `storage_backend_id` | string |  | StorageBackendID binds this KB to one concrete storage instance. The legacy provider field remains readable during migration only. |
| `storage_config` | StorageConfig | yes | Deprecated: legacy COS config column. Kept for backward compatibility with old data. |
| `vector_store_id` | string |  | VectorStoreID references the VectorStore this knowledge base is bound to. When nil, the KB falls back to the workspace's effective engines derived from the RETRIEVE_DRIVER environm |
| `extract_config` | ExtractConfig | yes | Extract config |
| `faq_config` | FAQConfig | yes | FAQConfig stores FAQ specific configuration such as indexing strategy |
| `question_generation_config` | QuestionGenerationConfig | yes | QuestionGenerationConfig stores question generation configuration for document knowledge bases |
| `auto_tag_config` | AutoTagConfig | yes | AutoTagConfig controls asynchronous association of existing tags after parsing. |
| `profile_config` | KnowledgeBaseProfileConfig | yes | ProfileConfig controls automatic generation of the knowledge-base description from per-document profiles (document knowledge bases only). |
| `generated_profile` | KnowledgeBaseProfile |  | GeneratedProfile is the machine-generated description: a gist, merged topics, typical questions and the aggregate snapshot they came from. It never overwrites the user-authored Des |
| `wiki_config` | WikiConfig | yes | WikiConfig stores wiki-specific configuration (only for wiki type knowledge bases) |
| `indexing_strategy` | IndexingStrategy | yes | IndexingStrategy controls which indexing pipelines are active for this knowledge base. Pipelines: vector search, keyword search, wiki generation, knowledge graph extraction. |
| `is_pinned` | boolean | yes | IsPinned and PinnedAt are computed per-caller from user_kb_pins (see migration 000050). They used to be stored on the row itself, which made pinning a workspace-wide ordering decis |
| `pinned_at` | string(time) | yes | PinnedAt records when the current caller pinned this knowledge base; nil when they have not. |
| `created_at` | string(time) | yes | Creation time of the knowledge base |
| `updated_at` | string(time) | yes | Last updated time of the knowledge base |
| `deleted_at` | DeletedAt | yes | Deletion time of the knowledge base |
| `knowledge_count` | number | yes | Knowledge count (not stored in database, calculated on query) |
| `chunk_count` | number | yes | Chunk count (not stored in database, calculated on query) |
| `is_processing` | boolean | yes | IsProcessing indicates if there is a processing import task (for FAQ type knowledge bases) |
| `processing_count` | number | yes | ProcessingCount indicates the number of knowledge items being processed (for document type knowledge bases) |
| `share_count` | number | yes | ShareCount indicates the number of organizations this knowledge base is shared with (not stored in database) |
| `creator_name` | string |  | CreatorName is the display name (username/email) of the user referenced by CreatorID. It is batch-filled by list handlers only and never persisted; empty means the creator could not be resolved (deleted user, legacy rows with empty CreatorID, etc.). The frontend uses it for the mine-vs-workspace badge on cards. |

#### `KnowledgeBaseShareResponse`
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes |  |
| `knowledge_base_id` | string | yes |  |
| `knowledge_base_name` | string | yes |  |
| `knowledge_base_type` | string | yes |  |
| `knowledge_count` | number | yes |  |
| `chunk_count` | number | yes |  |
| `organization_id` | string | yes |  |
| `organization_name` | string | yes |  |
| `shared_by_user_id` | string | yes |  |
| `shared_by_username` | string | yes |  |
| `source_tenant_id` | number | yes |  |
| `permission` | string | yes |  |
| `my_role_in_org` | string | yes |  |
| `my_permission` | string | yes |  |
| `created_at` | string(time) | yes |  |
| `require_approval` | boolean | yes |  |

#### `Organization`
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes | Unique identifier of the organization |
| `name` | string | yes | Name of the organization |
| `description` | string | yes | Description of the organization |
| `avatar` | string | yes | Avatar URL for display in list and settings |
| `owner_id` | string | yes | User ID of the organization owner |
| `owner_tenant_id` | number | yes | OwnerTenantID is the tenant the owner belonged to when the organization was created. Plan 3 (#1303) treats this tenant as the org's "owning tenant": its membership row in organizat |
| `invite_code` | string | yes | Unique invitation code for joining the organization |
| `invite_code_expires_at` | string(time) | yes | When the current invite code expires; nil means no expiry |
| `invite_code_validity_days` | number | yes | Invite link validity in days: 0=never, 1/7/30 |
| `require_approval` | boolean | yes | Whether joining requires admin approval |
| `searchable` | boolean | yes | Whether the space is open for search (discoverable; non-members can search and join by org ID) |
| `member_limit` | number | yes | Max members allowed; 0 means no limit |
| `created_at` | string(time) | yes | Creation time |
| `updated_at` | string(time) | yes | Last updated time |
| `deleted_at` | DeletedAt | yes | Deletion time (soft delete) |
| `owner` | User |  | Associations (not stored in database) |
| `members` | OrganizationTenantMember[] |  |  |
| `shares` | KnowledgeBaseShare[] |  |  |

#### `OrganizationMemberResponse`
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes |  |
| `user_id` | string | yes |  |
| `representative_user_id` | string | yes |  |
| `username` | string | yes |  |
| `email` | string | yes |  |
| `avatar` | string | yes |  |
| `role` | string | yes |  |
| `tenant_id` | number | yes |  |
| `tenant_name` | string |  |  |
| `joined_at` | string(time) | yes |  |

#### `OrganizationResponse`
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes |  |
| `name` | string | yes |  |
| `description` | string | yes |  |
| `avatar` | string |  |  |
| `owner_id` | string | yes |  |
| `owner_tenant_id` | number | yes | OwnerTenantID is the persisted owner workspace of the organization (Plan 3, migration 000046). Frontend uses this to identify the "owner row" in the workspace-keyed members list —  |
| `invite_code` | string |  |  |
| `invite_code_expires_at` | string(time) |  |  |
| `invite_code_validity_days` | number | yes |  |
| `require_approval` | boolean | yes |  |
| `searchable` | boolean | yes |  |
| `member_limit` | number | yes |  |
| `member_count` | number | yes |  |
| `share_count` | number | yes |  |
| `agent_share_count` | number | yes |  |
| `pending_join_request_count` | number | yes |  |
| `is_owner` | boolean | yes |  |
| `my_role` | string |  |  |
| `has_pending_upgrade` | boolean | yes |  |
| `created_at` | string(time) | yes |  |
| `updated_at` | string(time) | yes |  |

#### `ResourceCountsByOrgResponse`
| field | type | req | notes |
|---|---|---|---|
| `by_organization` | map[string]int | yes |  |
| `by_organization` | map[string]int | yes |  |

#### `User`
| field | type | req | notes |
|---|---|---|---|
| `id` | string | yes | Unique identifier of the user |
| `username` | string | yes | Username of the user |
| `email` | string | yes | Email address of the user |
| `avatar` | string | yes | Avatar URL of the user |
| `tenant_id` | number | yes | Workspace ID that the user belongs to |
| `is_active` | boolean | yes | Whether the user is active |
| `can_access_all_tenants` | boolean | yes | Whether the user can access all workspaces (cross-workspace access) |
| `is_system_admin` | boolean | yes | Whether the user is a system administrator (independent of workspace roles) |
| `preferences` | UserPreferences | yes | Per-user UI/feature preferences. Stored as JSON (jsonb on Postgres, TEXT on SQLite) via the driver.Valuer / sql.Scanner methods on UserPreferences. |
| `created_at` | string(time) | yes | Creation time of the user |
| `updated_at` | string(time) | yes | Last updated time of the user |
| `deleted_at` | DeletedAt | yes | Deletion time of the user |
| `tenant` | Tenant |  | Association relationship, not stored in the database |
