---
name: ui-reference-recreation
description: "Use when rebuilding a gated UI piece from its public preview."
version: 2.0.0
author: uesteibar
license: MIT
metadata:
  hermes:
    tags: [design, references, demos, workers]
    related_skills: [ui-reference-ingestion]
---

# Recreate a UI piece from its public preview

## When to Use

Use it when you need a working, self-contained HTML demo of a component, interaction or animation and the real code is out of reach: paid or gated code, or a source that is only a video or screenshots. The result is an approximation written from frames, not a copy. It does not depend on any library; to save the result in the uirefs library, hand it to `ui-reference-ingestion`.

## Decide first

- Look for the real code before anything else (registry file, a "copy code" URL in the JS chunks, an open-source repo): see `ui-reference-ingestion`. Recreate only what stays gated.
- Check the target library for the same source and title first. Sites that sell one catalogue under two names are one source.
- Pick pieces a small HTML file can carry: CSS, SVG, canvas, spring physics. Raymarched or rigged 3D scenes and photographic scenes do not converge. Save those as an animation with the preview clip and a description that says why there is no demo.
- A site that bot-blocks your browser (security checkpoint, HTTP 429) is access control: do not evade it. Take code from a public repo, render the piece yourself for images, and say so in the description.

## Procedure

1. **Get the preview.** Gallery cards loop a public mp4. Grep the page HTML for `.mp4`; the CDN host is often another domain. Download with `curl -A 'Mozilla/5.0'`, then check the file really is video (`file`, `ffprobe`): a network filter can answer 200 with an HTML block page. If TLS verification fails for the host, stop and report. Turn it off for that host only when the owner says to, and then check the file again.
2. **Reference pack**, one scratch directory per piece (not a git repo; absolute paths in every handoff): `<name>.mp4`, a contact sheet `ffmpeg -i <name>.mp4 -vf "fps=12/<duration>,scale=480:-1,tile=3x4" -frames:v 1 <name>-sheet.png`, one or two stills (`-ss 1 -frames:v 1`), and a zoomed crop of the interesting seconds if needed. Read the sheet yourself and write down what is measurable: colors, text, sizes, timings. Put the sandbox rules of the target in a shared `COMMON.md` one level up (`templates/COMMON.md`).
3. **Brief** per piece, written with the file-write tool (`templates/BRIEF.md`): what it is in measurable facts; what happens on every interaction; the responsive range 320 to 1440 px and the small panel size (about 293x320); the output path `out/<name>.html`; no image files (CSS, SVG, canvas); "do not fetch the original's code"; a time budget of about 40 minutes (a worker otherwise polishes pixels for hours); at most about 15 images read in total, contact sheets and crops only (the model gateway rejects a request past 30 images or about 48 MB and the worker dies); a final reply of 3 lines plus `out/REPORT.md`.
4. **Workers.** One cheap-model window per piece, in parallel. A crashed worker usually left `out/<name>.html` on disk: review it before relaunching. A worker still polishing after the budget gets a wrap-up message (stop polishing, save, write the report, reply in 3 lines); then take what exists.
5. **Review it yourself.** The worker's report is not evidence. Serve `out/` with a static server that sends the target's sandbox headers, open it in a visible tab, drive it with real input and compare with the reference still. Recipe and checks: `references/review.md`.
6. **Hand off.** Give the demo, the preview clip and the reference sheet to the library flow. Tag it `recreated`. The description says it is a recreation from the public preview and why (code paid or gated). `behavior` holds what you saw, plus one sentence for every text, number or name you had to invent and a list of the paths you did not trigger (touch, error states, second click).
7. **Clean up.** Close the worker window (use the ID column of `rex window ls`) and stop the static server.

## Pitfalls

- A demo that looks right at 780 px can overlap at 293x320. Open it once at the real panel size in the app.
- Two tools can use different temp roots (`execute_code` and `terminal`). Write absolute paths in specs and check where a file landed.
- Do not describe a behavior you did not trigger.
