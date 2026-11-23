// usage: node bundle-tailwind-component.mjs <file.tsx> <ExportName|default> '<propsJSON>' <out.html> [bg] [dark=0|1]
// env GLOBALS_CSS=<path>  project's globals.css (Tailwind v4 theme tokens), optional but needed for shadcn-style classes
// env CN_SHIM=<path>      file that exports cn(); aliases the "@/lib/utils" import
// Run from a directory that has node_modules (esbuild, react, react-dom, the component's own deps).
import { build } from "esbuild"; import fs from "node:fs"; import path from "node:path";
const [file, comp, props = "{}", out, bg = "#ffffff", dark = "0"] = process.argv.slice(2);
const dir = path.dirname(path.resolve(file)); const base = path.basename(file, ".tsx");
const entry = path.join(dir, "_entry_" + base + ".tsx");
const importLine = comp === "default" ? `import Comp from "./${base}";` : `import { ${comp} as Comp } from "./${base}";`;
fs.writeFileSync(entry, `import React from "react"; import { createRoot } from "react-dom/client"; ${importLine}
createRoot(document.getElementById("root")!).render(<Comp {...(${props})} />);`);
const alias = process.env.CN_SHIM ? { "@/lib/utils": path.resolve(process.env.CN_SHIM) } : {};
let js = "";
try {
  const r = await build({ entryPoints: [entry], bundle: true, minify: true, write: false, outdir: "out", format: "iife", jsx: "automatic",
    target: "es2020", define: { "process.env.NODE_ENV": '"production"' }, logLevel: "error", nodePaths: [path.resolve("node_modules")], alias });
  for (const f of r.outputFiles) js += f.text;
} finally { fs.unlinkSync(entry); }
const theme = process.env.GLOBALS_CSS ? fs.readFileSync(process.env.GLOBALS_CSS, "utf8").replace(/@import\s+["']tailwindcss["'];?/, "") : "";
const html = `<!doctype html><html${dark === "1" ? ' class="dark"' : ""}><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600&display=swap"><script src="https://cdn.jsdelivr.net/npm/@tailwindcss/browser@4"></script><style type="text/tailwindcss">${theme}</style><style>html,body{margin:0;height:100%;background:${bg};font-family:Inter,system-ui,sans-serif}#root{height:100%;display:grid;place-items:center;overflow:hidden}</style></head><body><div id="root"></div><script>${js.replace(/<\/script/g, "<\\/script")}</script></body></html>`;
fs.writeFileSync(out, html); console.log(out, Math.round(html.length / 1024) + " KB");
