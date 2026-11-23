# Reviewing a recreated demo

Do this yourself, in a visible browser tab (`Page.bringToFront`). A hidden tab pauses animations and makes a good demo look frozen.

1. **Serve** `out/` with a small static server that adds the target's sandbox headers (for uirefs: the `/demo` Content-Security-Policy, see the library's `guide`). Use a free port on 127.0.0.1 and stop the server when done.
2. **List the real controls** and their centre points from the DOM, for example `querySelectorAll('button,[role=radio],[role=slider],input')` with `getBoundingClientRect`. A click at a guessed coordinate does nothing and looks like a bug.
3. **Drive it with real input** through CDP `Input.dispatchMouseEvent` (move, press, move in steps, release), `Input.dispatchKeyEvent` and `mouseWheel`. Take a screenshot at the moments the animation changes (for example at 0.3 s and 3 s), tile 3 or 4 frames into one sheet with PIL and read the sheet once.
4. **Compare** with the reference still from the pack. Note what differs; do not fix style by feel.
5. **Check sizes:** the full width, and once the small panel size (about 293x320) inside the app after saving: open `#/r/<id>`, scroll the iframe into view (`scrollIntoView({block:'center'})`), screenshot it.
6. **Wheel and arrow keys** too, when the original scrolls or slides.
7. A typo found in `behavior` after saving: read the stored field, replace the span and send the whole field with `update`.

A wrap-up for a worker that keeps polishing: send "stop polishing, save out/<name>.html, write out/REPORT.md, reply in 3 lines". With Pi under Rex the message waits at a prompt (`s steer / q queue`); press `s`.
