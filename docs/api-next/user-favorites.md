# User Favorites API

Frontend module: `frontend-next/lib/api/user-favorites.ts` — see [README](README.md) for envelope, auth headers, and transport conventions.

---

### `listFavorites` — `GET /user/favorites`

handler: `ListFavorites` · `user_resource_favorite.go`

**Query**:
| name | type | req | notes |
|---|---|---|---|
| `type` | string | yes | Resource type (kb | agent) |

**Response**:
```json
{ success: true, data: list }
```
---
### `addFavorite` — `POST /user/favorites`

handler: `AddFavorite` · `user_resource_favorite.go`

**Body** `AddFavoriteRequest`:
| field | type | req | notes |
|---|---|---|---|
| `type` | string |  |  |
| `id` | string |  |  |

**Response**:
```json
{ success: true }
```
---
### `removeFavorite` — `DELETE /user/favorites/:type/:id`

handler: `RemoveFavorite` · `user_resource_favorite.go`

**Response**:
```json
{ success: true }
```
---