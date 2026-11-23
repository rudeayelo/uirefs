# Task: recreate <NAME> as an interactive demo

First read `../COMMON.md` (the rules; it is in the parent directory).

Reference files in this directory: `<name>.mp4` (<duration> s, <WxH>), `<name>-sheet.png` (12 frames in reading order), `<name>-frame.png` (one still). Extract more frames with ffmpeg; crop and upscale to read small text.

What it is: <one paragraph of measurable facts: layout, colors with hex values, type, sizes in px, text strings, counts>.

Interactions:
- Hover: <...>
- Press and drag: <...>
- Keyboard: <...>
- Touch: <...>
- Timings and easing you can measure: <...>

Not visible in the recording (decide, and say in the report what you chose): <hover or press, trigger, end states>.

Responsive: 320 to 1440 px. At 293x320 the piece must still be usable and nothing may overlap.

Time budget: finish within about 40 minutes. Read at most 15 images in total (contact sheets and crops, never frame dumps).

Output: `out/<name>.html`. When done, write `out/REPORT.md` (what you built, how you verified it with real results, what differs from the reference) and reply with 3 lines.
