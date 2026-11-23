## Rules (all tasks)

- Work only inside your task directory (a scratch directory, not a git repo). Output goes to `out/`. Touch nothing outside it. Do not use the target service's real port.
- The deliverable is ONE self-contained HTML file. It will be shown in an iframe with this header (replace with the target's real policy; for uirefs, take it from the library's `guide`): `Content-Security-Policy: sandbox allow-scripts; default-src 'none'; script-src 'unsafe-inline' https:; style-src 'unsafe-inline' https:; img-src data: blob: https:; font-src data: https:; media-src data: blob:; connect-src 'none'; form-action 'none'`. That means: no local files, no cookies or `localStorage`, no access to `parent`. Inline scripts and styles, data: images and https libraries or fonts are allowed. Prefer plain HTML, CSS, JS and canvas.
- It must be interactive and look like the reference, not a static picture. It fills its window and is responsive from 320 to 1440 px.
- You can read images. A contact sheet is a set of frames in reading order. Write down measurable facts (timing, easing, sizes, colors) before you code.
- Verify for real: serve `out/` with a tiny local server that adds the header above, open it in headless Chromium at 1280x720 and 390x844, drive it with real mouse, touch and keyboard events, take screenshots over time and read them, and check the console for errors. If animations look frozen the tab is probably hidden: bring it to front. Stop everything you start.
- Write the code yourself from the frames. Do not fetch the original's code.
- When done, write `out/REPORT.md` and reply with 3 lines.
