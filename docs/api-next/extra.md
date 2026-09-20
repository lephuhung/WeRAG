# Miscellaneous API

Frontend module: `frontend-next/lib/api/extra.ts` — see [README](README.md) for envelope, auth headers, and transport conventions.

---

### `getSuggestedQuestions` — `GET /agents`

_Auxiliary call: when `agentId` is omitted, the function first fetches the agent list and uses the first entry as the default agent._

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