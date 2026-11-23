// usage: node bundle-component.mjs <srcdir> <ExportName> '<propsJSON>' <out.html> [background]   (env WIDE=1 for scene components)
// Bundles a React component (default export) with its CSS into one self-contained HTML file.
import { build } from "esbuild"; import fs from "node:fs"; import path from "node:path";
const [dir, comp, props = "{}", out, bg = "#141414"] = process.argv.slice(2);
const file = fs.readdirSync(dir).find(f => f.toLowerCase().replace(/-/g, "") === comp.toLowerCase() + ".tsx");
if (!file) throw new Error(`no ${comp}.tsx in ${dir}`);
const entry = path.join(dir, "_entry.tsx");
fs.writeFileSync(entry, `import React from "react"; import { createRoot } from "react-dom/client"; import ${comp} from "./${file.replace(".tsx", "")}";
createRoot(document.getElementById("root")!).render(<${comp} {...(${props})} />);`);
const r = await build({ entryPoints: [entry], bundle: true, minify: true, write: false, outdir: "out", format: "iife", jsx: "automatic",
  target: "es2020", loader: { ".css": "css" }, define: { "process.env.NODE_ENV": '"production"' }, logLevel: "error" });
let js = "", css = "";
for (const f of r.outputFiles) { if (f.path.endsWith(".css")) css += f.text; else js += f.text; }
const fill = process.env.WIDE ? "width:100%;height:100%;" : "";
const html = `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>html,body{margin:0;height:100%;background:${bg}}#root{height:100%;display:grid;place-items:center;overflow:hidden}#root>*{max-width:100%;${fill}}${css}</style></head><body><div id="root"></div><script>${js.replace(/<\/script/g, "<\\/script")}</script></body></html>`;
fs.writeFileSync(out, html); console.log(out, Math.round(html.length / 1024) + " KB");
