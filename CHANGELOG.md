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
- VT fixtures whose expected screens come from the pinned tmux (`make vt-fixtures`).
- OSC and DCS, ported from tmux 3.8: titles and the title stack, OSC 7 paths, OSC 8 hyperlinks
  (kept per cell and given back by `capture-pane -e`), OSC 4/10/11/12 colours and queries with
  their resets, OSC 52 under `set-clipboard`, OSC 133 prompt marks, OSC 9;4 progress, and DECRQSS.
- Replies to programs as tmux 3.8 gives them (DA1, DA2, XTVERSION, DSR, DECRQM, window sizes,
  colour queries, DECRQSS, the theme report), checked against tmux by a probe.
- `testdata/faketui`, a stand-in for agent CLIs in tests: a full-screen frame in synchronized
  output, key, mouse and paste echo, resizing, streaming, title, cwd and copy, and exits.
- History and `capture-pane` ranges checked against tmux with 5000 lines and wrapped lines, and
  a memory benchmark for a full 50000-line history.
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
