# Getting a demo out of a source

## Registry file (shadcn style)

A component page that shows `npx shadcn@latest add "https://<site>/r/<name>.json"` serves its source for anyone:

```
curl -s -A 'Mozilla/5.0' https://<site>/r/<name>.json -o <name>.json
python3 -I - <<'EOF'
import json; d=json.load(open('<name>.json'))
print(d.get('dependencies'), [(f['path'], len(f['content'])) for f in d['files']])
EOF
```

Write each `files[].content` to disk under its basename, then bundle (below). Check the exported name: most registry components use `export default`, so import the default.

## "Copy code" buttons

1. List the button labels and `aria-label`/`title` in the page (`document.querySelectorAll('main li button')`).
2. Download the page's `/_next/static/chunks/*.js` (every `<script src>` from the HTML) and grep for the label text: the click handler holds the fetch URL template, for example `` `/motion-src/code/${id}.txt` ``.
3. The id is not always the visible slug: it can be the element's DOM id (`db-<slug>`). Read `li.id`, then `curl` the URL. A 404 that returns the whole 40 KB site shell means a wrong id.

The result may already be a complete single-file HTML (inline CSS and JS plus a Google Fonts link): save it as the demo unchanged.

## Bundling React/TS into one HTML

One scratch directory per toolchain, not inside the repo:

```
mkdir demotool && cd demotool && npm init -y >/dev/null
npm install --no-audit --no-fund --ignore-scripts esbuild react react-dom   # add motion etc. when the component imports it
node bundle-component.mjs <srcdir> <ExportName> '<propsJSON>' out.html '<bg>'   # template in templates/
```

The template bundles with `--format=iife --minify`, inlines JS and CSS, mounts the component in a full-height stage. Typical size 230 to 320 KB with React. Props worth setting: dark theme when the source stage is dark, `fill` for scene-like components that need the container's box, `autoplay`.

## Bundling a Tailwind or shadcn component

`templates/bundle-tailwind-component.mjs` bundles one `.tsx` file with esbuild into one HTML file. It loads Tailwind v4 from the https browser build and uses the project's own `globals.css` for theme tokens.

```
cd <toolchain dir>   # npm install --no-audit --no-fund --ignore-scripts esbuild react react-dom motion clsx tailwind-merge lucide-react
GLOBALS_CSS=<repo>/app/globals.css CN_SHIM=<dir>/utils.ts node bundle-tailwind-component.mjs <file.tsx> default '{}' out.html '#ffffff' 0
```

- `CN_SHIM` is a small file that exports `cn()` (`clsx` + `twMerge`). It replaces the `@/lib/utils` import. Other `@/...` imports need the same alias or an inlined copy.
- Pass `default` or the named export. A component that fills its parent (canvas, physics) needs a sized wrapper through its props: a zero-size root renders nothing.
- Without the project's `globals.css`, shadcn tokens (`bg-muted`, `border-border`) resolve to nothing and the demo looks unstyled.
- Tailwind from the CDN and web fonts need network: say so in the description.

## Saving a normal page as one file

Fetch the HTML, inline the stylesheet (`<style>`), rewrite `url(*.woff2)` to base64 data URIs, drop `<script src>`, preloads, icons, canonical and preconnect links, keep `<img src=https://...>`. Check the size and that no local path remains.

## Recording a live component without a video

In the browser tool: bring the tab to front, find the element rectangle, then loop `Page.captureScreenshot` with a `clip` (`scale` 1) for 8 to 10 seconds into numbered jpegs (about 8 fps is what the daemon sustains). Then:

```
ffmpeg -framerate <measured fps> -i %03d.jpg -vf "scale=trunc(iw/2)*2:trunc(ih/2)*2" -pix_fmt yuv420p -c:v libx264 -crf 24 clip.mp4
ffmpeg -framerate <fps> -i %03d.jpg -vf "select='not(mod(n,6))',scale=240:-1,tile=7x2" -frames:v 1 sheet.png   # contact sheet to read with vision
```

Use the measured frame count over elapsed time as the framerate so playback speed is right. Clips of a few tens of KB are enough.

## Reading a clip you cannot play

Contact sheet at 16 frames, then a second sheet at 1 to 3 fps over only the interesting seconds with `drawtext=text='%{pts\:hms}'` timestamps. Cross-check every time you state in `behavior`; re-read the exact seconds when a claim about timing or counts was a guess.
