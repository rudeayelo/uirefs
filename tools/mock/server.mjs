/* Mock of the uirefs JSON API (docs/api.md) for the web UI. Node, standard library only.
   Serves web/ at /, the endpoints from fixtures.json, generated SVG tiles and self-contained demos.
   Usage: node tools/mock/server.mjs [--port N]   (PORT env also works; 0 picks a free port) */

import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const HERE = path.dirname(fileURLToPath(import.meta.url));
const WEB = path.resolve(HERE, "../../web");
const FIXTURES = path.join(HERE, "fixtures.json");

/* A short grayscale clip, generated once with ffmpeg and kept inline so the mock needs no tools. */
const CLIP = Buffer.from("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", "base64");

/* Same header as internal/api (DemoCSP). */
const DEMO_CSP = "sandbox allow-scripts allow-pointer-lock; default-src 'none'; " +
  "script-src 'unsafe-inline' 'unsafe-eval' https: blob:; style-src 'unsafe-inline' https:; " +
  "img-src data: blob: https:; font-src data: https:; media-src data: blob: https:; " +
  "connect-src https: data: blob:; worker-src blob:; form-action 'none'";

const MIME = {
  ".html": "text/html; charset=utf-8", ".css": "text/css; charset=utf-8", ".js": "text/javascript; charset=utf-8",
  ".json": "application/json; charset=utf-8", ".woff2": "font/woff2", ".svg": "image/svg+xml"
};

/* ---------- fixtures, kept in memory so DELETE can remove them ---------- */
const db = JSON.parse(await readFile(FIXTURES, "utf8"));
let references = db.references;

const listItem = r => {
  const images = r.assets.filter(a => a.kind === "image");
  const primary = images.find(a => a.role === "primary") || images[0];
  const kinds = ["image", "demo", "clip"].filter(k => r.assets.some(a => a.kind === k));
  return { id: r.id, type: r.type, title: r.title, description: r.description, tags: r.tags, source: r.source,
    thumb: primary ? primary.url : null, kinds, created_at: r.created_at };
};
const detail = r => ({ ...listItem(r), behavior: r.behavior, locator: r.locator ?? null, assets: r.assets });

/* ---------- tiny deterministic randomness, for tiles and demos ---------- */
const hash = s => { let h = 2166136261; for (const ch of String(s)) { h ^= ch.codePointAt(0); h = Math.imul(h, 16777619); } return h >>> 0; };
const rng = seed => () => ((seed = (Math.imul(seed, 1664525) + 1013904223) >>> 0) / 4294967296);

/* ---------- a neutral tile drawn from the title ---------- */
function tile(r, a) {
  const rand = rng(hash(r.title + a.id));
  const grays = ["#eaeae9", "#dfdfdd", "#d4d4d2", "#c9c9c7", "#bfbfbd"];
  const phone = String(a.viewport || "").startsWith("390");
  const W = 800, H = 500;
  let body = "";
  for (let i = 0; i < 4; i++) {
    const fill = grays[Math.floor(rand() * grays.length)];
    const cx = 90 + rand() * (W - 180), cy = 70 + rand() * (H - 140), rad = 40 + rand() * 130;
    body += rand() < .5
      ? `<circle cx="${cx.toFixed(0)}" cy="${cy.toFixed(0)}" r="${rad.toFixed(0)}" fill="${fill}"/>`
      : `<rect x="${(cx - rad).toFixed(0)}" y="${(cy - rad * .7).toFixed(0)}" width="${(rad * 2).toFixed(0)}" height="${(rad * 1.4).toFixed(0)}" rx="18" fill="${fill}"/>`;
  }
  const frame = phone
    ? `<rect x="300" y="40" width="200" height="420" rx="26" fill="#f7f7f6" stroke="#c6c6c5" stroke-width="2"/>`
    : `<rect x="60" y="40" width="680" height="420" rx="14" fill="#f7f7f6" stroke="#c6c6c5" stroke-width="2"/>
       <line x1="60" y1="96" x2="740" y2="96" stroke="#dededc" stroke-width="2"/>
       <circle cx="88" cy="68" r="6" fill="#d4d4d2"/><circle cx="110" cy="68" r="6" fill="#d4d4d2"/><circle cx="132" cy="68" r="6" fill="#d4d4d2"/>`;
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${W} ${H}" width="${W}" height="${H}">
    <rect width="${W}" height="${H}" fill="#f1f1f0"/>
    <g opacity=".85">${body}</g>${frame}</svg>`;
  return Buffer.from(svg);
}

/* ---------- a few small self-contained demos ---------- */
const shell = (css, html, js = "") => `<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<style>html,body{height:100%;margin:0}body{display:grid;place-items:center;background:#f7f7f6;color:#141414;
font:14px ui-sans-serif,system-ui,sans-serif;-webkit-font-smoothing:antialiased}${css}</style></head>
<body>${html}${js ? `<script>${js}</script>` : ""}</body></html>`;

const DEMOS = [
  // 1. a button that fills on hover
  () => shell(`
    .b{height:40px;padding:0 18px;border:0;border-radius:10px;background:#fff;color:#141414;font:inherit;font-weight:620;
       box-shadow:0 0 0 1px #e7e7e6,0 1px 2px rgba(0,0,0,.05);cursor:pointer;transition:background .2s,color .2s,box-shadow .2s,transform .2s}
    .b:hover{background:#141414;color:#fff;box-shadow:0 0 0 1px #141414}
    .b:active{transform:scale(.97)}`, `<button class="b">Save changes</button>`),
  // 2. the sticker peel
  () => shell(`
    .stage{width:180px;height:180px;perspective:600px}
    .st{width:100%;height:100%;border-radius:18px;background:#e9e9e8;box-shadow:0 0 0 1px #dededc,0 8px 20px -12px rgba(0,0,0,.4);
        display:grid;place-items:center;cursor:pointer;transform-origin:0 0;transition:transform .3s cubic-bezier(.22,1,.36,1)}
    .st span{font-size:44px;opacity:.25;transition:opacity .3s}
    .st.peel{transform:rotate(-10deg) skewX(6deg) scale(.94);border-radius:18px 40px 18px 18px;
        box-shadow:6px 6px 0 -1px #f7f7f6,10px 10px 18px -8px rgba(0,0,0,.35)}
    .st.peel span{opacity:1}`,
    `<div class="stage"><div class="st" id="s"><span>◍</span></div></div>`,
    `const s=document.getElementById("s");s.addEventListener("click",()=>s.classList.toggle("peel"));`),
  // 3. a marquee that pauses on hover
  () => shell(`
    .m{width:100%;overflow:hidden;-webkit-mask:linear-gradient(90deg,transparent,#000 48px,#000 calc(100% - 48px),transparent)}
    .t{display:flex;gap:32px;width:max-content;animation:run 22s linear infinite}
    .m:hover .t{animation-play-state:paused}
    .t span{font-size:26px;letter-spacing:-.02em;white-space:nowrap}
    @keyframes run{to{transform:translateX(-50%)}}`,
    `<div class="m"><div class="t"><span>Component · Site · Animation · Identity · Resource ·</span><span>Component · Site · Animation · Identity · Resource ·</span></div></div>`)
];

/* demos that fit the reference: a component gets the button, an animation the peel or the marquee */
const demoFor = (id, type) => {
  const i = type === "component" ? 0 : type === "animation" ? 1 + (hash(id) % 2) : hash(id) % DEMOS.length;
  return DEMOS[i]();
};

/* ---------- responses ---------- */
const json = (res, code, body) => {
  const buf = Buffer.from(JSON.stringify(body));
  res.writeHead(code, { "content-type": "application/json; charset=utf-8", "content-length": buf.length, "cache-control": "no-store" });
  res.end(buf);
};
const fail = (res, code, message) => json(res, code, { error: message });

function sendBuffer(res, req, buf, type) {
  const range = req.headers.range && /^bytes=(\d*)-(\d*)$/.exec(req.headers.range);
  if (range) {
    const start = range[1] ? Number(range[1]) : 0;
    const end = Math.min(range[2] ? Number(range[2]) : buf.length - 1, buf.length - 1);
    if (start < buf.length && end >= start) {
      res.writeHead(206, { "content-type": type, "content-length": end - start + 1, "accept-ranges": "bytes",
        "content-range": `bytes ${start}-${end}/${buf.length}`, "cache-control": "no-store" });
      res.end(buf.subarray(start, end + 1));
      return;
    }
  }
  res.writeHead(200, { "content-type": type, "content-length": buf.length, "accept-ranges": "bytes", "cache-control": "no-store" });
  res.end(buf);
}

/* ---------- routes ---------- */
async function serveStatic(res, req, pathname) {
  const rel = pathname === "/" ? "index.html" : decodeURIComponent(pathname).replace(/^\/+/, "");
  const file = path.resolve(WEB, rel);
  if (file !== WEB && !file.startsWith(WEB + path.sep)) return fail(res, 403, "forbidden");
  const type = MIME[path.extname(file)];
  if (!type) return fail(res, 404, "not found");
  try {
    const buf = await readFile(file);
    res.writeHead(200, { "content-type": type, "content-length": buf.length, "cache-control": "no-store" });
    res.end(buf);
  } catch {
    fail(res, 404, "not found");
  }
}

function api(req, res, url) {
  const p = url.pathname;

  if (p === "/api/references" && req.method === "GET") {
    const type = url.searchParams.get("type") || "";
    const tags = url.searchParams.getAll("tag").filter(Boolean);
    const q = (url.searchParams.get("q") || "").trim().toLowerCase();
    const limit = Number(url.searchParams.get("limit") || 60);
    const offset = Number(url.searchParams.get("offset") || 0);

    let rows = references.filter(r =>
      (!type || r.type === type) && tags.every(t => r.tags.includes(t)) &&
      (!q || [r.title, r.description, r.behavior, r.tags.join(" ")].join(" ").toLowerCase().includes(q)));
    // newest first; with q, a title hit outranks a body hit
    rows = rows.map(r => ({ r, title: q && r.title.toLowerCase().includes(q) ? 1 : 0 }))
      .sort((a, b) => b.title - a.title || String(b.r.created_at).localeCompare(String(a.r.created_at)))
      .map(x => x.r);

    return json(res, 200, { items: rows.slice(offset, offset + limit).map(listItem), total: rows.length });
  }

  if (p === "/api/tags" && req.method === "GET") {
    const type = url.searchParams.get("type") || "";
    const counts = new Map();
    for (const r of references) {
      if (type && r.type !== type) continue;
      for (const t of r.tags) counts.set(t, (counts.get(t) || 0) + 1);
    }
    const tags = [...counts].map(([name, count]) => ({ name, count }))
      .sort((a, b) => b.count - a.count || a.name.localeCompare(b.name));
    return json(res, 200, { tags });
  }

  const m = p.match(/^\/api\/references\/([^/]+)$/);
  if (m) {
    const id = decodeURIComponent(m[1]);
    const i = references.findIndex(r => r.id === id);
    if (i < 0) return fail(res, 404, "reference not found");
    if (req.method === "GET") return json(res, 200, detail(references[i]));
    if (req.method === "DELETE") { references.splice(i, 1); res.writeHead(204); return res.end(); }
    return fail(res, 405, "method not allowed");
  }

  if (p.startsWith("/api/")) return fail(res, 404, "not found");
  return false;
}

const port = Number(process.argv.includes("--port") ? process.argv[process.argv.indexOf("--port") + 1] : process.env.PORT || 0);

const server = createServer(async (req, res) => {
  const url = new URL(req.url, "http://127.0.0.1");
  try {
    if (url.pathname.startsWith("/api/")) { if (api(req, res, url) === false) fail(res, 404, "not found"); return; }

    const asset = url.pathname.match(/^\/assets\/([^/]+)$/);
    if (asset) {
      const id = decodeURIComponent(asset[1]);
      const ref = references.find(r => r.assets.some(a => a.id === id));
      const a = ref && ref.assets.find(x => x.id === id);
      if (!a) return fail(res, 404, "asset not found");
      return sendBuffer(res, req, a.kind === "clip" ? CLIP : tile(ref, a), a.mime || "application/octet-stream");
    }

    const demo = url.pathname.match(/^\/demo\/([^/]+)$/);
    if (demo) {
      const id = decodeURIComponent(demo[1]);
      const ref = references.find(r => r.assets.some(a => a.id === id && a.kind === "demo"));
      if (!ref) return fail(res, 404, "demo not found");
      const buf = Buffer.from(demoFor(id, ref.type));
      res.writeHead(200, { "content-type": "text/html; charset=utf-8", "content-length": buf.length,
        "cache-control": "no-store", "content-security-policy": DEMO_CSP });
      return res.end(buf);
    }

    return await serveStatic(res, req, url.pathname);
  } catch (err) {
    if (!res.headersSent) fail(res, 500, String(err && err.message || err));
    else res.end();
  }
});

server.listen(port, "127.0.0.1", () => {
  console.log(`mock uirefs listening on http://127.0.0.1:${server.address().port}`);
});
