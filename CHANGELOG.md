# Changelog

All notable changes to i-mux-core are listed here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- The repository skeleton: `imux-core -V`, the package layout, and `make ci`.
- CI on Linux, macOS and Windows, and the pinned tmux 3.8 (`make tmux`) the differential tests
  compare against.
- The contract recorded from i-mux (`make contract-record`, `make contract`): COMPATIBILITY.md now
  lists exactly what i-mux uses of tmux 3.8, with `contract/expected.json` as its data.
- The differential test harness (`make difftest`): scripts in `testdata/diff` run against tmux
  and i-mux-core, and every command's output, exit code and captured screen is compared.
- The terminal emulator's parser, ported from tmux 3.8's `input.c` (E10), with tmux's ground timer
  for strings that never end.
- PTYs on macOS and Linux, and a pane's foreground command and working directory, without cgo
  (E11).
- The emulator's screen, ported from tmux 3.8 (`grid.c`, `grid-view.c`, `screen-write.c`,
  `utf8-combined.c`): cells, wide characters, combining, tabs, wrapping, history and
  `capture-pane`, with tmux's widths from the pinned utf8proc 2.12.0.
- VT fixtures whose expected screens come from the pinned tmux (`make vt-fixtures`): every
  sequence of tasks C1.4 to C1.7, wide characters, wrapping, history and resizing; CI checks that
  tmux still gives them.
- OSC and DCS, ported from tmux 3.8: titles and the title stack, OSC 7 paths, OSC 8 hyperlinks
  (kept per cell and given back by `capture-pane -e`), OSC 4/10/11/12 colours and queries with
  their resets, OSC 52 under `set-clipboard`, OSC 133 prompt marks, OSC 9;4 progress, and DECRQSS.
- Replies to programs as tmux 3.8 gives them (DA1, DA2, XTVERSION, DSR, DECRQM, window sizes,
  colour queries, DECRQSS, the theme report), checked against tmux by a probe on the emulator
  and in a pane on both engines.
- `testdata/faketui`, a stand-in for agent CLIs in tests: a full-screen frame in synchronized
  output, key, mouse and paste echo, resizing, streaming, title, cwd and copy, and exits.
- History and `capture-pane` ranges checked against tmux with 5000 lines and wrapped lines, and
  a memory benchmark for a full 50000-line history.
- Resizing a pane's screen as tmux does: lines reflow to the new width on the main screen and
  move between the screen and the history for a new height, with the cursor following them.
- Emulator benchmarks and `BENCHMARKS.md` (100 MB of mixed output in 0.43× tmux's time), a fuzz
  target for the whole emulator, and a nightly CI job that fuzzes every target for 10 minutes.
- `internal/ipc`: the client-server protocol's frames and messages, the protocol version check,
  and the output queue that detaches a client that does not keep up.
- The server's socket: a `0700` directory per user, checked before use, a `0600` socket, and only
  the owner's connections accepted.
- The server's tree of sessions, windows and panes, with tmux's ids, names and indexes, and its
  loop: one goroutine owns the tree, pane readers feed the screens, and the server ends with its
  last session.
- `internal/pty`: programs on PTYs with resizing, exit codes and signals, and a kill that ends
  programs which ignore SIGHUP (K4).
- Cursor movement, erasing, line/character editing, scroll regions, tab stops, cursor save/restore,
  insert/origin/wrap modes, alignment test and resets in `internal/vt`, verified against tmux 3.8
  fixtures.
- SGR colours and text attributes in `internal/vt` (ANSI 8/16, 256-colour and RGB in `;` and `:`
  forms, underline styles `4:0`–`4:5`, underline colours 58/59, and resets), verified against
  tmux 3.8 `capture-pane -e` fixtures.
- Terminal modes (`DECSET`/`DECRST` for cursor, mouse, bracketed paste, focus, synchronized output,
  theme updates, and alternate screen 47/1047/1049), `DECRQM` replies, and alternate-screen switching
  in `internal/vt`.
- The renderer: an attached client is drawn as tmux 3.8 draws it (panes, borders and their
  colours, the window's fill, cursor and mouse modes), with only what changed written, inside
  synchronized output when the terminal has it; a pane in a synchronized update keeps its last
  frame. `make render-fixtures` records what tmux draws for `testdata/render`, and the tests
  compare.
- The differential harness's `screen WxH` step: a client attached in a PTY, its terminal's
  screen compared with tmux's once it settles, and a difference shown side by side.
- Input from attached clients, read as tmux 3.8 reads it: key names (`internal/keys`), the key
  sequences of the client's terminal with escape-time, mouse reports, bracketed paste and focus
  reports passed to the active pane, and the terminal's replies (features, colours, theme)
  used; theme reports fire the client events.
- Keys into panes, encoded as tmux 3.8 encodes them for each pane's modes (application keys,
  extended keys in both formats); `send-keys` with key names, text, `-l`, `-H` and `-N`;
  `set-option` for `extended-keys` and `extended-keys-format`; keys typed in an attached client
  reach its active pane.
- Several clients: windows sized by `window-size` (tmux's `latest` by default) less the status
  lines, `switch-client -t`, sessions for attaching preferring unattached ones as tmux does, and
  difftest clients that stay attached (`client`, `resize`, `screen NAME`).
- Target resolution (`internal/target`), ported from tmux 3.8's `cmd-find.c` and `window.c`:
  ids, exact (`=`), prefix and glob names, window and pane tokens, positions and directions,
  `TMUX_PANE` for commands run inside a pane, a command list's current target, and basic
  `new-window`, `split-window`, `select-window` and `select-pane`.
- Options: tmux 3.8's option table, generated from its source, with scopes, inheritance,
  arrays and user options; `set-option`, `set-window-option`, `show-options` and
  `show-window-options` as tmux has them.
