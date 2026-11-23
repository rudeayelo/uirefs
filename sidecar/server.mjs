/* uirefs embeddings sidecar: a local OpenAI-compatible /v1/embeddings endpoint.
   Xenova/all-MiniLM-L6-v2, int8 (q8), mean pooling, normalized. Node, one dependency.
   Usage: SIDECAR_PORT=48900 node sidecar/server.mjs   (see sidecar/README.md) */

import { createServer } from "node:http";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { env, pipeline } from "@huggingface/transformers";

const MODEL = "Xenova/all-MiniLM-L6-v2";
const PORT = Number(process.env.SIDECAR_PORT || 48900);
const HOST = "127.0.0.1";
const MAX_BODY = 1 << 20; // 1 MB: far more than a batch of reference texts

// Model cache: HF_HOME when set, else ./.cache next to this file.
env.cacheDir = process.env.HF_HOME ? path.resolve(process.env.HF_HOME) : path.join(path.dirname(fileURLToPath(import.meta.url)), ".cache");

const extractor = await pipeline("feature-extraction", MODEL, { dtype: "q8" });

const embeddingFor = async texts => {
  const out = await extractor(texts, { pooling: "mean", normalize: true });
  return out.tolist();
};

const send = (res, code, body) => {
  const buf = Buffer.from(JSON.stringify(body));
  res.writeHead(code, { "content-type": "application/json; charset=utf-8", "content-length": buf.length });
  res.end(buf);
};
const fail = (res, code, message) => send(res, code, { error: { message, type: "invalid_request_error" } });

function readBody(req) {
  return new Promise((resolve, reject) => {
    let size = 0;
    const chunks = [];
    req.on("data", c => {
      size += c.length;
      if (size > MAX_BODY) { reject(new Error("request body too large")); req.destroy(); return; }
      chunks.push(c);
    });
    req.on("end", () => resolve(Buffer.concat(chunks).toString("utf8")));
    req.on("error", reject);
  });
}

const server = createServer(async (req, res) => {
  const { pathname } = new URL(req.url, "http://127.0.0.1");
  if (pathname !== "/v1/embeddings" || req.method !== "POST") return fail(res, 404, "not found");

  let body;
  try {
    body = JSON.parse(await readBody(req));
  } catch (err) {
    return fail(res, 400, `invalid JSON body: ${err.message}`);
  }

  const input = body && body.input;
  const texts = typeof input === "string" ? [input] : Array.isArray(input) ? input : null;
  if (!texts || texts.length === 0 || !texts.every(t => typeof t === "string")) {
    return fail(res, 400, "input must be a string or an array of strings");
  }

  try {
    const vectors = await embeddingFor(texts);
    send(res, 200, {
      object: "list",
      data: vectors.map((embedding, index) => ({ object: "embedding", index, embedding })),
      model: (body && body.model) || MODEL,
      usage: { prompt_tokens: texts.reduce((n, t) => n + t.length / 4 | 0, 0), total_tokens: 0 }
    });
  } catch (err) {
    console.error("embedding failed:", err);
    fail(res, 500, `embedding failed: ${err.message}`);
  }
});

server.listen(PORT, HOST, async () => {
  // Warm up, so the first real request pays no model load.
  try {
    await embeddingFor(["warm up"]);
  } catch (err) {
    console.error("warm up failed:", err);
  }
  const rss = Math.round(process.memoryUsage().rss / 1024 / 1024);
  console.log(`embeddings sidecar on http://${HOST}:${PORT}/v1/embeddings (${MODEL}, q8, ${rss} MB RSS)`);
});

for (const sig of ["SIGINT", "SIGTERM"]) process.on(sig, () => server.close(() => process.exit(0)));
