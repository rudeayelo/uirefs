---
name: ui-reference-ingestion
description: "Use when saving UI references with a demo into a library."
version: 1.1.0
author: uesteibar
license: MIT
metadata:
  hermes:
    tags: [design, references, mcp, demos]
    related_skills: [ui-reference-recreation, ui-reference-search]
---

# Ingest UI references with an interactive demo

## When to Use

Load it when the owner gives links, tweets or sites to save as references, or asks to add a demo to saved ones. It adds a component, site, animation or identity to the uirefs library (MCP) so an agent can later search it, open the demo and implement it. The library's `guide` tool has the five types and the tag vocabulary: call it before the first `add`. This skill is the procedure to get the material.

## Rules from the owner

- **Ask when it is unclear what to save.** A link to a site that also has a component library (or templates) can mean the page or the components. If you cannot tell, ask before ingesting: "the site as a reference, or its components?" Do not guess and do not save both. Once the owner says "the components", save components only, not the page.
- **An interactive demo is the point.** Get the real one if the source serves it; otherwise recreate it. A static image or a clip is the last fallback, and the description then says why.
- **Do not break access control:** no forged logins, no API calls the page does not make, no paid downloads. Gated code is recreated from the public preview and tagged `recreated`.
- **Collections have no cap.** Save what the owner asks for. If they say "the components" of a big library and not which, ask whether they want all of them, a selection or a number. Classify by what you would copy.
- Write `behavior` from frames and DOM you actually looked at. Say what you could not see instead of guessing.

## Procedure per source

1. **Open it and find the pieces.** Page: browser at 1440 wide for desktop images; if the shared browser will not resize, use headless `chromium --headless=new --window-size=1440,900 --hide-scrollbars --virtual-time-budget=12000 --screenshot=...` (add `--use-gl=swiftshader --enable-unsafe-swiftshader` for WebGL). X post: fetch it through a public mirror (see the usage guide).
2. **Find the code, in this order** (commands in `references/demo-extraction.md`):
   1. a registry file (`/r/<name>.json` with the files inline);
   2. a "copy code" button: its URL is in the site's JS chunks;
   3. the public repo of an open-source library;
   4. the page saved as one HTML file;
   5. nothing usable: recreate.
3. **Make one self-contained HTML file.** React/TS goes through `templates/bundle-component.mjs` (Tailwind or shadcn code: `templates/bundle-tailwind-component.mjs`, see `references/demo-extraction.md`); plain HTML is used as is. The demo runs in a sandboxed iframe with an opaque origin: no cookies, `localStorage` or access to `parent`; scripts, styles, fonts and images from https are fine.
4. **Recreate when the code is gated** (paid component, WebGL scene, video-only): follow `ui-reference-recreation` (preview, reference pack, brief, workers, review), or write it yourself, and compare the result with the frames in a browser with real input. If a recreation does not converge, say so in the description and save without a demo.
5. **A clip of a live component:** when the source has no video, see the CDP frame-loop recipe in `references/demo-extraction.md`.
6. **Save.** New reference: one `add` with images, demo and clip. Existing reference without a demo: `update` with `assets`.
7. **Look at it once in the app** (`#/r/<id>`, tab in front) and run one `search` with a natural sentence to see that it is found.

**Resources.** For a `resource` (library, icon set, font, texture pack, template kit), capture where to get it (package name or download URL), the licence, the formats and the counts in `description`/`behavior`. Build the demo as a gallery or specimen using the library's own assets or package when the licence allows; otherwise save images only.

## Pitfalls

- **A hidden tab pauses animations.** Components that wait for `document.hidden` or IntersectionObserver stay invisible in an unfocused tab. Bring the tab to front (`Page.bringToFront`) and wait a few seconds before judging.
- **No clipboard read** for "copy code": find the URL in the JS chunks and fetch it.
- **`npm install` can fail on native postinstall scripts** (for example `sharp`): use `--ignore-scripts`.
- **A page saved as one file** keeps analytics and preload scripts: drop them, inline the stylesheet and fonts, keep images as https URLs.
- **`update` replaces `tags` and `description` whole.** Read the reference first and send the old value plus your addition.
- A network that fails TLS verification (a self-signed certificate on a CDN host) is a blocker: report it, do not switch verification off.
