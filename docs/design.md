# uirefs: design

## 1. What it is

A small private service, for example at `https://uirefs.example.com`. No login and no token: the private network is the access control.

- Agents read and write it through an **MCP server**.
- The owner browses and deletes through a **web UI**.
- One entry is one **reference**: a component, a whole site, an animation, an identity or a resource. Not one link.

Main use: an agent that designs or builds UI can **find a reference**, then **fetch everything it needs to implement it and verify the result**.

## 2. Scope of version 1

In:

- Sources: X posts and web pages.
- Five types: `component`, `site`, `animation`, `identity`, `resource` (something you install or download and use: illustrations, icons, fonts, textures, templates).
- Assets per reference: images, self-contained HTML demos, short clips (stored as is).
- Keyword search (always on) and semantic search (optional, see section 6).
- MCP tools (add, update, delete, search, get, list_tags).
- Web UI (grid, filters, search, detail with the demo and clip inline, delete).
- A usage guide embedded in the binary and served by the MCP, that tells agents how to decompose a link into references.

Out:

- Anything that processes video or images inside the app. The app stores and serves files. Seeing a clip (for example a frame contact sheet) is the agent's job, and the guide carries the recipe.
- Authentication, users, tokens.
- Sharing, collections, nested folders, browser extension, Shiori import.
- Trash or undo. Delete is permanent.
- Paid or gated content: gated code is recreated from the public preview, never unlocked.

## 3. Data model

**Source**: `id`, `url`, `kind` (`web` | `tweet`), `title`, `added_at`. A tweet that links to a page creates two sources.

**Reference**

- `id`, `type` (`component` | `site` | `animation` | `identity` | `resource`), `title`.
- `description`: what it is and why it is good. Written in English by the agent.
- `behavior`: what an implementer must see to say "this matches". Observable facts only (for example "peels from the top-left corner on click, about 300 ms ease-out, reveals the icon below"). This field is what makes verification possible.
- `tags`: from a short vocabulary per type (section 7). `concept:` tags are allowed.
- `source_id`, `locator` (section of the page, or timestamp of the clip).
- `assets[]`, `embedding` (only when semantic search is on), `created_at`.

**Asset**: `kind` (`image` | `demo` | `clip`), `role` (free text, `primary` by default), `viewport`, `caption`.

- `demo`: a self-contained HTML file. This is the "code" of the reference.
- `clip`: mp4/webm/gif, stored without processing.

Types: `component`, `site`, `animation`, `identity`, `resource`. `title` and `description` are
required. A reference may hold any number of images, demos and clips; there is no
size limit.

## 4. MCP server

Streamable HTTP at `https://uirefs.example.com/mcp`. No auth.

1. `search(query, type?, tags?, limit=10)`: compact results with id, type, title, description, tags, source URL, thumbnail (inline image).
2. `get(id)`: everything needed to implement and verify: metadata, `description`, `behavior`, images inline, `demo_url` with the first demo's HTML source inline, `clip_url`, source URL and locator.
3. `add(type, title, description, behavior, tags, source_url, assets[])`: assets by file path (same host), URL, or base64. Any size is accepted; the file must be an allowed image, video or HTML type, which decides the served `Content-Type`.
4. `update(id, fields)`.
5. `delete(id)`: permanent. Removes the files. Removes the source when it has no references left.
6. `list_tags(type?)`: vocabulary and counts.
7. `guide()`: returns the full usage guide (section 7), embedded in the binary.

The `initialize` response carries short `instructions` (what the library is, when to search it, when to save, and "call `guide` before the first `add`"). The descriptions of `add` and `search` repeat the key rule. So a client that ignores `instructions` still sees the pointer to `guide`.

Queries are in English. The calling agent translates.

Verification flow (written into the guide):

1. `search`, pick the closest, `get` it. Read `behavior`. Implement.
2. Open `demo_url` and its own build side by side at the same viewport as the stored image.
3. For animations, compare against the demo, or against its own frame sheet made from `clip_url`.
4. Report which `behavior` facts it checked.

## 5. Web UI

For the owner only. Light theme, monochrome, minimal, app-like.

- Grid with thumbnail and title. Filter by type and tag. Search box.
- **Detail view: images, the demo running inline in the same page, the clip with controls**, description, behavior, tags, link to the source.
- Delete from grid and detail, with one confirmation.
- No counters, no onboarding text, no instruction labels.

**Demos run inline, from the same origin.** Demo HTML is untrusted, and with no auth any script on the UI origin could call the delete endpoint. So the demo is served at `/demo/<id>` with a response header `Content-Security-Policy: sandbox allow-scripts allow-pointer-lock; default-src 'none'; script-src 'unsafe-inline' 'unsafe-eval' https: blob:; style-src 'unsafe-inline' https:; img-src data: blob: https:; font-src data: https:; media-src data: blob: https:; connect-src https: data: blob:; worker-src blob:; form-action 'none'`.

- `sandbox` without `allow-same-origin` is the boundary: the demo gets an opaque origin, so it cannot read or call the app, and has no cookies, no `localStorage` and no `parent`.
- Scripts, styles, fonts, images, media and `fetch` over https work, so real demos render.
- The detail page embeds it with `<iframe src="/demo/<id>" sandbox="allow-scripts">`. No second hostname.
- Guards for a no-auth service on a browser machine: the JSON API only has GET and DELETE and sends no CORS headers, so another site cannot call it; `POST /mcp` needs `Content-Type: application/json` and refuses a cross-origin `Origin`; any `Host` that is not `localhost`, an IP or a name in `UIREFS_PUBLIC_URL` / `UIREFS_ALLOWED_HOSTS` gets a 403 (DNS rebinding); the UI sends `frame-ancestors 'self'`.

**Look and feel.** Minimalist, in plain ES modules with no build step and tokens in `web/app.css`, with a self-hosted font:

- Light theme, monochrome, gray accent. Page `#f7f7f6`, surface `#ffffff`, ink `#141414`, lines `#e7e7e6`, font Allo (variable woff2), 14 px, the same easing curves.
- No magenta.
- Grid of cards of one size, calm hover, detail as a panel that slides over the grid with a light scrim and never reflows it. On the phone it is a bottom sheet with its own URL.
- No counters, no numbering, no explanatory text, no icon-only meaning. Short motion, respects `prefers-reduced-motion`.

## 6. Language, packaging and embeddings

**Go, one static binary.** Verified with Go 1.27.3:

- `modernc.org/sqlite` (pure Go) with FTS5 works. A test binary is **6.4 MB**, statically linked, and used 11 MB of memory. It cross-compiles for macOS arm64 (6.2 MB) with `CGO_ENABLED=0`.
- The web UI is plain HTML/CSS/JS embedded into the binary with `go:embed`. No build step.
- Data lives in one directory (`--data`, default `./data`): the SQLite file and the asset files.
- Commands: `uirefs serve --port N --data DIR`, `uirefs reindex`, `uirefs version`. Starts in milliseconds, so an agent can start and stop it on demand. Stop is a clean exit on SIGTERM.
- Runs as a `systemd --user` service (Linux) or a `launchd` agent (macOS); the installer sets it up.
- Considered and not chosen: Bun `--compile` (works, but the binary is 104 MB and in-process ONNX failed to compile because of a native `sharp` dependency), Node (needs Node and `node_modules`), Rust (also fine, slower to build and iterate).

**Embeddings are optional.**

- Default: off. Search is FTS5 keyword search with ranking over title, description, behavior and tags. Agents write English and use the shared tag vocabulary, so this already works for exact words and tags.
- On: set `UIREFS_EMBED_URL` to any OpenAI-compatible `/v1/embeddings` endpoint. The binary stores one vector per reference and ranks by cosine similarity (brute force in Go, fine up to tens of thousands of rows), fused with FTS5 by reciprocal rank fusion. `uirefs reindex` embeds everything again after you turn it on or change the model.
- The repo includes a small optional sidecar (about 30 lines of Node, `@huggingface/transformers`) that serves that endpoint with the model below. It runs only when you want it. The Go binary never depends on it.

**Model for the sidecar: `Xenova/all-MiniLM-L6-v2`, int8.** English only, which is enough because the agent translates.

Measured (Node process, CPU, 2026-11-21; an idle Node process already uses about 90 MB):

| Model | On disk | Process RSS after load and use | Top-1 on an 8-query English test |
|---|---|---|---|
| all-MiniLM-L6-v2 (int8) | 23 MB | 174 MB | 8/8 |
| bge-small-en-v1.5 (int8) | 34 MB | 196 MB | 8/8 |
| gte-small (int8) | 34 MB | 208 MB | 8/8 |
| bge-m3 (int8, multilingual) | 560 MB | 1070 MB | 8/8 |

Limits: the test is tiny and I wrote both documents and queries, so it cannot rank the small models against each other. It shows they all load, run on CPU, and handle English paraphrases. MiniLM truncates input at 256 tokens, which is enough for a title plus description plus behavior. If real data shows weak ranking, move to `bge-small-en-v1.5` (+11 MB disk, +22 MB RAM). Rerun the comparison after 30 or more references.

## 7. Usage guide, embedded in the binary

The guide ships inside the binary (`go:embed`, a Markdown file in the repo) and is served by the MCP: as `instructions`, as the `guide` tool and as the resource `uirefs://guide`. Version and behavior stay in one place, and any agent that connects gets it with no skill to install or sync. No separate skill is needed. If an agent proves to miss the trigger ("save this link"), add a 5-line skill later that only says "use the uirefs MCP".

The guide is self-contained (no reference to other skills). It teaches an agent to:

1. Ingest the source. X post: `https://api.fxtwitter.com/<user>/status/<id>` (`tweet.text`, `tweet.media.videos[].url`, and the quoted or linked page). Web page: the agent's browser.
2. Classify by what you would copy: a component, a whole site, a motion, a style. If the value is the concept, put it in the description and a `concept:` tag.
3. For each reference: images at fixed viewports (desktop 1440, mobile 390), a self-contained demo when the element is interactive, the clip for tweets with video, `description` in English, and `behavior`.
4. For a clip, build its own frame contact sheet with ffmpeg to see it (`fps=16/<duration>,scale=360:-1,drawtext=...,tile=4x4`). That sheet stays on the agent's side, it is not uploaded.
5. Mark demos the agent wrote itself with a `recreated` tag.
6. Use only tags from `list_tags` unless nothing fits. Never break authentication or a paywall; recreate gated code from the public preview.
7. The verification flow of section 4.

Tag vocabulary per type (starting point):

- `component`: button, input, toggle, card, hero, nav, pricing, form, footer, modal, block (combination of components).
- `site`: portfolio, product, editorial, docs, dashboard.
- `animation`: scroll, hover, click, loop, shader, transition, text, 3d.
- `identity`: logo, type, palette, illustration, brand-guidelines.
- `resource`: icons, illustrations, fonts, textures, templates, library.
- Cross-type: `concept:<free text>`, `recreated`.

## 8. Acceptance list

1. `go build` with `CGO_ENABLED=0` produces one binary under 30 MB. Copied alone to an empty directory, it starts with `uirefs serve` and works with no other file.
2. `search("pricing with annual toggle")` finds the right reference with embeddings off (keyword) and on.
3. `get(id)` of a component with a demo returns images inline, `demo_url`, the demo source and `behavior`.
4. The detail page shows the demo running inline. A test demo that calls `fetch` to the app's delete endpoint, or reads `window.parent`, fails.
5. `add` of an animation with only an image succeeds. So does a component with no image, a site without `source_url`, a file over 6 MB, a role other than `primary`, and a second demo or clip. A text file named `.png` is still rejected by the MIME check.
6. `delete` removes the reference from search and the grid, and its files from disk.
7. The web UI shows type filter, tag filter, search, detail and delete at 390 and 1280 px.
8. With `UIREFS_EMBED_URL` unset, the process uses under 50 MB and starts in under a second. After a restart, data and routes are still there.
9. A fresh agent session, with only the MCP connected, adds example links and the result has sensible types and tags.
10. A fresh agent session implements one stored component from `get` output alone and reports the checked `behavior` facts.
11. A fresh agent session with only the MCP connected (no skill) saves a link correctly after reading `instructions` and `guide`.
12. `add` of a resource with no image, no demo and no `source_url` succeeds.
