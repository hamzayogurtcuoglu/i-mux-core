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

## Issues

| Terminal | What you did | tmux | i-mux-core |
| --- | --- | --- | --- |
| | | | |

## Sign-off

- [ ] Every check passes in the terminals above, or the issues have become tasks.

Signed off by: ______________  Date: __________
