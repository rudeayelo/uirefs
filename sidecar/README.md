# Embeddings sidecar

An optional local `POST /v1/embeddings` endpoint for uirefs, in the OpenAI shape. It runs
`Xenova/all-MiniLM-L6-v2` (int8), mean pooling, normalized, 384 dimensions per text. Nothing in the
Go binary depends on it: with `UIREFS_EMBED_URL` unset, uirefs never loads or calls it, and when the
endpoint is down keyword search keeps answering (`embed query: … (keyword-only results)` in the log).

## Run

```sh
cd sidecar
npm install --ignore-scripts   # plain `npm install` can fail on `sharp`; this uses the WASM runtime. Not committed
node server.mjs      # or: npm start
```

It binds `127.0.0.1` only. The first run downloads about 23 MB of model files from the Hugging Face hub.

| Variable | Default | Meaning |
|---|---|---|
| `SIDECAR_PORT` | `48900` | Listen port. |
| `HF_HOME` | `sidecar/.cache` | Where the model is cached. |

Check it:

```sh
curl -s http://127.0.0.1:48900/v1/embeddings \
  -d '{"model":"Xenova/all-MiniLM-L6-v2","input":"a pricing table with an annual toggle"}' |
  python3 -c 'import json,sys,math; e=json.load(sys.stdin)["data"][0]["embedding"]; print(len(e), math.sqrt(sum(x*x for x in e)))'
```

`input` may be one string or an array of strings; the response is `{"data":[{"embedding":[…],"index":0}],…}`.

## Wire it into uirefs

```sh
UIREFS_EMBED_URL=http://127.0.0.1:48900/v1/embeddings ./uirefs serve --port 4880 --data ./data
UIREFS_EMBED_URL=http://127.0.0.1:48900/v1/embeddings ./uirefs reindex --data ./data
```

`uirefs reindex` embeds every reference again; run it once after turning embeddings on and again after
changing the endpoint or the model. Adds made while the sidecar runs are embedded on write.

## Footprint

Measured (2026-11-21, Node 26, this sidecar):

- 23 MB of model files on disk (`HF_HOME`), of which the int8 ONNX file is most of it.
- 169–188 MB RSS once the model is loaded and warm; the design note measured 174 MB.
- One short text takes about 2 ms, a batch of 16 (the reindex batch) about 22 ms, on CPU.

If npm blocks install scripts (`onnxruntime-node`'s postinstall, as this machine's npm does), there is
no native binding and `transformers.js` silently runs on its WASM backend. That is what the numbers
above measure. Allowing the script (`npm install-scripts approve onnxruntime-node` on npm 11, or an npm
config that does not block scripts) installs the native build; same vectors, less memory and CPU.

The design note for the whole feature is `docs/design.md` §6.
