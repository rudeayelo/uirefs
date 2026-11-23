# JSON API for the web UI

Contract between the Go service (`core` task) and the web UI (`web/` task). Base path `/api`. JSON, UTF-8, no auth. Error body: `{"error": "message"}` with the right status.

Type: `component` | `site` | `animation` | `identity` | `resource`.

## Objects

**Asset**

```json
{ "id": "a_3f9", "kind": "image", "role": "primary", "mime": "image/png",
  "url": "/assets/a_3f9", "viewport": "1440x900", "caption": "Hover state" }
```

- `kind`: `image` | `demo` | `clip`. `role` is free text, `primary` by default.
- `url` for `image` and `clip` is `/assets/<id>` (clips support HTTP range requests). For `demo` it is `/demo/<id>`, which serves the sandboxed HTML (design section 5). `viewport` and `caption` may be `null`.

**Source**: `{ "id": "s_12", "url": "https://x.com/...", "kind": "tweet", "title": "..." }`. `kind`: `web` | `tweet`.

**Reference (list item)**

```json
{ "id": "r_81", "type": "animation", "title": "Sticker peel",
  "description": "...", "tags": ["click", "sticker"],
  "source": { "id": "s_12", "url": "...", "kind": "tweet", "title": "..." },
  "thumb": "/assets/a_3f9",
  "kinds": ["image", "demo", "clip"],
  "created_at": "2026-11-21T11:49:00Z" }
```

- `thumb` is the URL of the primary image, or `null` when the reference has no image (an animation with only a clip or a demo).
- `kinds` lists which asset kinds exist, so the grid can show a small marker without loading details.

**Reference (detail)** is the list item plus `behavior` (string), `locator` (string or null) and `assets` (array of Asset).

## Endpoints

| Method and path | Response |
|---|---|
| `GET /api/references?type=&tag=&q=&limit=&offset=` | `{ "items": [Reference list item], "total": 42 }`. `tag` may repeat (all must match). `q` is the search text. Without `q`, newest first. Default `limit` 60. |
| `GET /api/references/<id>` | Reference detail. 404 if missing. |
| `DELETE /api/references/<id>` | 204. Permanent. The source disappears when it has no references left. 404 if missing. |
| `GET /api/tags?type=` | `{ "tags": [{ "name": "click", "count": 7 }] }`, most used first. |
| `GET /assets/<id>` | The file, with its MIME type. |
| `GET /demo/<id>` | The demo HTML with the sandbox CSP header. |

The only mutating endpoint is `DELETE`; there is no create or update over this API (agents use MCP, `POST /mcp`, which needs `Content-Type: application/json`).
