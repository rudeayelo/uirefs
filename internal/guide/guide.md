# uirefs — usage guide

uirefs is a private library of UI references. One entry is one **reference**:
a component, a whole site, an animation, an identity or a resource — never just a link.
Each reference carries images, self-contained HTML demos and optional
clips, plus two texts that make it useful to an implementing agent:

- **description** — what it is and why it is good, in English.
- **behavior** — observable facts only ("peels from the top-left corner on
  click, about 300 ms ease-out, reveals the icon below"). This is the field
  that lets you verify that a reimplementation matches.

## When to use it

- **Before building UI**: `search` with English keywords, pick the closest
  hit, `get` it, read `behavior`, implement, then verify (below).
- **When you find good UI**: save it with `add` so future agents can find it.

## Ingesting a source

- **X post**: fetch `https://api.fxtwitter.com/<user>/status/<id>`; use
  `tweet.text`, `tweet.media.videos[].url`, and the quoted or linked page.
  Store the original `https://x.com/...` URL as `source_url`.
- **Web page**: use your browser.
- **Page or components?** If a link could mean the page itself or the
  components the site offers, ask the owner which one before saving. If the
  owner wants the components of a large library and does not say which, ask
  how many. There is no cap on what you may save.

### Take the demo first

Every component and animation needs an interactive demo, and a site gets one
when it is feasible. Look for it in this order:

1. **The source offers the code.** A "copy code" or "show the code" button, a
   registry file (for example shadcn style `https://<site>/r/<name>.json`
   with the files inline), a `view-source` single file, or a
   `/code/<id>.txt` URL that the buttons fetch (read the page's JS to find
   it). Fetch it with `curl` and build one self-contained HTML file: bundle
   React or other sources with `esbuild --bundle --minify --format=iife` and
   inline the JS and CSS. Scripts and styles from https CDNs and Google Fonts
   are allowed; local files are not.
2. **The source is a normal page or site.** Save it as one HTML file: inline
   the CSS, inline the fonts as data URIs, keep images as https URLs, drop
   analytics and external scripts.
3. **Not possible** — no code, a canvas/WebGL/3D app that cannot be bundled,
   a video only: **reproduce** it. Write a small self-contained HTML demo from
   the frames, the clip and the page's behavior; tag it `recreated` and check
   it against the clip.
4. **Only when none of the above works**, save the reference without a demo
   and say why in `description`.

Never break authentication or a paywall; recreate gated code
from the public preview.

A reference you already saved without a demo can get one later with
`update(id, assets: [...])`.

## Types

Classify by what you would copy:

- `component` — a single piece: button, input, card, nav, hero.
- `site` — a whole site or page.
- `animation` — a motion: scroll, hover, click, transition.
- `identity` — a brand system: logo, palette, type.
- `resource` — a thing you install or download and use: an illustration library, icon set, font, texture pack, template kit. Save where to get it (package name or download URL), the licence, the formats, and what a user gets; show it with a demo when it can show itself.

If the value is the concept rather than the artifact, say so in
`description` and add a `concept:<short text>` tag.

## Assets

- Images at fixed viewports: desktop 1440, mobile 390.
- Self-contained HTML demos — the code of the reference. No local files.
  Scripts, styles, fonts, images, media and `fetch` are allowed over https;
  the demo runs sandboxed with an opaque origin: no cookies, no
  `localStorage`, no `parent`.
- Clips for tweets with video (mp4/webm/gif, stored as is), public preview
  only. A reference may hold several demos and clips.

Look at the demo before you save it, with the tab in front: a hidden tab
freezes many components (`document.hidden`).

To see a clip yourself, build a frame contact sheet on your own side with
ffmpeg (it stays with you, never upload it):

    ffmpeg -i clip.mp4 -vf "fps=16/<duration>,scale=360:-1,tile=4x4" sheet.png

Mark demos you wrote yourself with the `recreated` tag.

## Tags

Use only tags from `list_tags` unless nothing fits. Starting vocabulary:

- `component`: button, input, toggle, card, hero, nav, pricing, form,
  footer, modal, block
- `site`: portfolio, product, editorial, docs, dashboard
- `animation`: scroll, hover, click, loop, shader, transition, text, 3d
- `identity`: logo, type, palette, illustration, brand-guidelines
- `resource`: icons, illustrations, fonts, textures, templates, library
- cross-type: `concept:<free text>`, `recreated`

## Verification flow

1. `search`, pick the closest, `get` it. Read `behavior`. Implement.
2. Open `demo_url` and your own build side by side, at the same viewport as
   the stored image.
3. For animations without a demo, compare against a frame sheet you made
   from `clip_url`.
4. Report which `behavior` facts you checked.
