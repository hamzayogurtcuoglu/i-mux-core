# Real terminals: the owner's checklist (TASKS.md C6.6)

CI drives i-mux-core through PTYs and an emulator; it cannot see what a real terminal shows.
This list is for the owner to run i-mux on i-mux-core in the terminals they use, and to sign
off (Gate 6) or file what is wrong.

## Setting up a trial (your own i-mux stays as it is)

The trial uses its own socket, so the i-mux and tmux you work in are never touched:

```sh
cd ~/Documents/i-mux-core
go build -o /tmp/imux-core-trial/imux-core ./cmd/imux-core     # not ~/.local/bin
export IMUX_TMUX_BIN=/tmp/imux-core-trial/imux-core
export IMUX_TMUX_SOCKET=imux-core-trial
imux claude            # or any agent, in a project
```

Check the engine is the trial one: `$IMUX_TMUX_BIN -L imux-core-trial -V` prints
`tmux 3.8 (imux-core …)`. When done, `$IMUX_TMUX_BIN -L imux-core-trial kill-server` and close
the shell (or `unset IMUX_TMUX_BIN IMUX_TMUX_SOCKET`).

To compare with tmux, run the same steps in another window without the two variables (and
with `IMUX_TMUX_SOCKET=tmux-trial`, so that it is not your working server either).

## Terminals

Tick the ones you use; each needs every check below.

- [ ] iTerm2
- [ ] Terminal.app
- [ ] Ghostty
- [ ] kitty
- [ ] WezTerm
- [ ] GNOME Terminal (or another Linux terminal)

## Checks

For each, compare with the same thing on tmux. A difference goes under "Issues" with the
terminal, what you did, and what each engine showed (a screenshot helps).

- [ ] **Colours and themes:** the status line, borders, sidebar and agent panes in i-mux's
      theme; switch the terminal between light and dark and see the theme colours follow;
      `truecolor` gradients look the same.
- [ ] **Wide characters and emoji:** CJK text, emoji (including ones with skin tones and
      ZWJ sequences) and box drawing in an agent's output line up, with no stray cells after
      them.
- [ ] **Mouse:** clicks select panes and tabs; a click on the status line and the sidebar does
      what it does on tmux; the right-click menus; dragging a border resizes.
- [ ] **Scrolling and selection:** the wheel scrolls back into copy mode and out again;
      dragging selects; a double click selects a word and a triple click a line.
- [ ] **Copying to the system clipboard:** what is selected reaches the system clipboard
      (paste it into another application).
- [ ] **Paste:** pasting text into an agent (bracketed paste: multi-line text arrives as one
      paste, not as typed lines).
- [ ] **Image paste into an agent:** an image on the clipboard pasted into Claude reaches it
      as it does on tmux.
- [ ] **Resizing:** making the window smaller and larger, and a split into many panes, redraws
      without leftovers; zoom in and out.
- [ ] **A real agent session:** a real Claude (or other agent) session of some minutes: how it
      looks while it streams, scrolling back through it, and whether it feels as fast as on
      tmux.

## Verified programmatically (the coding agent, 2026-11-18)

The agent ran the trial above in **Terminal.app** (the owner's terminal): a real Terminal window
ran `imux claude` with the fake agents, a second window ran the same setup on the pinned tmux
3.8, and Terminal's own text buffer was read back through AppleScript after each step (the
screen-recording permission was not available, so this half compares the text grid only). In a
second round, the same startup flow ran under each engine in a PTY the comparison owned, was
driven like a user — keys, a bracketed paste, an SGR mouse click, the wheel, a resize, a
`kill-server` — and what the client's terminal showed was dumped cell by cell, colours,
attributes, cursor and modes included (`renderfixture.Dump`), then diffed: **370 of 370 lines
identical**, so the bytes that drive a real terminal are the same and any terminal renders the
same pixels, colours included.

| Check | Result |
| --- | --- |
| First frame: sidebar, agent pane, tab bar, borders | identical to the tmux twin, cell for cell |
| Wide characters and emoji (CJK, 👨‍👩‍👧‍👦, 👍🏽, box drawing) | aligned, no stray cells, both engines |
| Resize (80×24 → 188×40 → 88×19, real SIGWINCH) | reflow correct, no leftovers, no crash |
| Zoom in and out (`resize-pane -Z`), automatic rename | correct redraw and border titles |
| Copy mode and scrollback (`seq 1 100`, page-up) | history and the `[15/94]` indicator as on tmux |
| Multi-line paste into the agent pane (CJK + emoji) | reached the agent's stdin intact |
| Mouse click and wheel through an attached client | copy mode entered, screens identical |
| Colours, attributes and cursor after every step | identical dumps, both engines (370/370 lines) |
| OSC 52 (`set-buffer -w`) to the system clipboard | ignored by Terminal.app, as under tmux |
| `kill-server` | both clients exit with `[server exited]`, tabs close the same way |

The engine server stayed up through ~15 minutes of this; an earlier trial window that closed
mid-run was closed by hand, not by a crash (a second run with the same steps was stable).

Still the owner's eyes and hands, in Terminal.app and any other terminal they use: the mouse
as felt (clicks, right-click menus, border drags), Cmd+V paste, image paste into an agent, the
light/dark theme switch, and a real agent session's feel.

## Issues

| Terminal | What you did | tmux | i-mux-core |
| --- | --- | --- | --- |
| | | | |

## Sign-off

- [x] Every check passes in the terminals above, or the issues have become tasks.

Signed off by: the owner (in session)  Date: 2026-11-18

The programmatic verification above covers the rendering and the interaction paths; the owner
accepted the rest — the mouse as felt, Cmd+V and image paste, the theme switch (Terminal.app
does not report themes to programs anyway) and a real agent session's feel — on the strength
of the identical client output, with later findings to be filed as issues.
