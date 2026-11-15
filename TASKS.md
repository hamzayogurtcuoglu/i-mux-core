# i-mux-core tasks

This is the full plan, phase by phase. Read [AGENTS.md](AGENTS.md) for how to work.
[ARCHITECTURE.md](ARCHITECTURE.md) has the design and [COMPATIBILITY.md](COMPATIBILITY.md) the contract.

## How to read a task

```
### [ ] C<phase>.<n> Title
- Goal:        why this task exists, in one or two sentences
- Details:     what to build, and the decisions already made
- Acceptance:  what must be true (and tested) for the task to be done
- Depends on:  tasks that must be done first
```

- **Done:** mark a task `[x]` in the same commit as its code.
- **USER:** the owner does it (manual checks on real machines, approvals).
- **Repository:** tasks in Phases 7 and 9 change the **i-mux** repository. They follow i-mux's
  own AGENTS.md: docs first, `make ci`, English commits, no co-author trailers, install only with
  `make install-local`.
- **Phase gates:** each phase ends with a gate. Do not start the next phase until the gate holds.

## Definition of done (every task)

1. The code is in, with tests that cover the acceptance criteria.
2. `make ci` passes. The commit message says which checks could not run here.
3. Docs are updated whenever behaviour, a decision or the contract changed: ARCHITECTURE.md,
   COMPATIBILITY.md, and CHANGELOG.md under "Unreleased".
4. No real agent CLI was started and no real user config was read.
5. i-mux on tmux is unaffected.

---

## Phase 0: Foundations

### [x] C0.1 Repository skeleton
- **Goal:** a buildable, lintable, testable repository from day one.
- **Details:**
  - Module `github.com/hamzayogurtcuoglu/i-mux-core`, with the same Go version as `../i-mux/go.mod`.
  - `cmd/imux-core/main.go`. `imux-core -V` prints `imux-core 0.0.0-dev (tmux 3.8 compatible)` (E9).
  - The package directories from ARCHITECTURE.md §3, each with a `doc.go` stating what it owns.
  - Makefile targets:
    - `build`, `test` (with `-race`), `lint`, `fmt`;
    - `difftest`, `fuzz` (short runs), `bench`;
    - `ci`, which runs lint, test and difftest.
  - `.golangci.yml`, starting from i-mux's.
  - `.gitignore`.
  - `CHANGELOG.md` with an "Unreleased" section.
  - `LICENSE`, the same license as i-mux.
- **Acceptance:**
  - `make ci` passes on macOS and Linux.
  - `imux-core -V` prints the version line.
  - `go vet` is clean with `GOOS=windows`.
- **Depends on:** none.

### [x] C0.2 CI on macOS, Linux and Windows
- **Goal:** every change is checked on all three systems from the start.
- **Details:**
  - A GitHub Actions workflow with a matrix of `ubuntu-latest`, `macos-latest` and `windows-latest`:
    build, vet and unit tests on each.
  - Lint runs on Linux for `GOOS=linux`, `GOOS=darwin` and `GOOS=windows`.
  - The difftest job runs on Linux and macOS against a **pinned tmux 3.8**, built on both by
    `scripts/build-tmux.sh` from the release tarball, checked against its SHA-256, configured
    with `--enable-utf8proc` (so widths agree on both systems) and cached. Homebrew is not used
    for tmux itself: it ships only the newest tmux, so it cannot be pinned. The script checks the
    built version and fails on a mismatch. `make tmux` builds the same pinned tmux locally, and
    `make difftest` uses it.
  - Go module and build caches are kept.
- **Acceptance:**
  - All jobs are green on an empty-but-real codebase.
  - A deliberately failing test turns the right job red.
  - *Not yet seen:* the workflow has not run on GitHub; it runs once the owner pushes. Check
    both points then.
- **Depends on:** C0.1.

### [x] C0.3 Record the contract from i-mux
- **Goal:** COMPATIBILITY.md lists exactly what i-mux uses: nothing missing, no guesses.
- **Details:**
  - `scripts/contract.sh` runs i-mux's e2e suite (`../i-mux`, `go test -tags e2e ./e2e/...`)
    with a recording shim, `cmd/tmuxrec`, installed as `tmux` first on `PATH`. The shim logs
    each invocation and its caller, then execs the pinned tmux. `IMUX_TMUX_RECORD` cannot be
    used: i-mux's e2e harness sets it to a temporary file per test and deletes it. This changes
    nothing in i-mux.
  - The shim also copies the generated `tmux.conf` files (`-f`, and files given to
    `source-file`) out of the test homes.
  - Calls from the e2e test binary itself are kept apart: they are the harness's, not i-mux's.
  - A scan of i-mux's Go source (`tmux.Cmd{…}` literals, the flags appended to them, and
    command strings) adds what the e2e suite does not run.
  - `cmd/contract` (a small Go tool) reads the records and configs and writes
    `contract/recorded.json`, holding:
    - every command and the flags it was given;
    - every format variable and modifier;
    - every option, with its scope;
    - every hook;
    - every key and table;
    - every target form.
  - The same tool compares that file with COMPATIBILITY.md (via a machine-readable block or a
    side file `contract/expected.json`) and prints what is missing or unused.
  - Resolve every "(check)" mark in COMPATIBILITY.md.
- **Acceptance:**
  - `make contract` reports zero items used by i-mux that are missing from the contract.
  - No "(check)" marks remain.
  - The tool has unit tests on a small synthetic record file.
- **Depends on:** C0.1. Needs tmux and i-mux's e2e prerequisites installed.

### [x] C0.4 Differential test harness
- **Goal:** one script runs against real tmux and against i-mux-core, and the results are
  compared.
- **Details:**
  - Package `internal/difftest`, with an engine abstraction (`tmux` binary, `imux-core` binary).
  - Each run has its own temp dir, its own socket (`-S`), and its own `HOME` and `TMUX_TMPDIR`.
  - The environment is fixed: `TERM=xterm-256color`, `LANG=C.UTF-8`, `TZ=UTC`, and a minimal
    `PATH`.
  - Script files go in `testdata/diff/*.txt`. Their format:
    - `run <tmux command line>` runs a command and compares stdout, stderr and exit code;
    - `conf <file>` sets the config;
    - `size WxH`;
    - `wait <format> == <value>` polls with a timeout;
    - `capture <pane>` compares `capture-pane -p -e`;
    - `normalize <regex> <replacement>`, for ids, pids, times and paths;
    - `skip-if-engine <name> <reason>`.
  - A readable diff on failure: the command, both outputs, and the script line.
  - `make difftest` skips with a clear message when tmux is not installed. CI never skips it.
  - Screen-frame comparison through a headless attached client comes later (C2.9).
- **Acceptance:**
  - The harness runs a sample script against tmux, and against a stub engine that echoes known
    answers.
  - It reports a planted difference correctly.
  - It has unit tests.
- **Depends on:** C0.1.

### [x] C0.5 Spike: the terminal emulator
- **Goal:** decide whether to build `internal/vt` on an existing Go package or write it.
- **Details:**
  - Time-box it.
  - Evaluate the maintained Go VT emulators available at the time; candidates include
    `github.com/charmbracelet/x/vt` and `github.com/hinshun/vt10x`. Check, for each:
    - license and maintenance;
    - coverage of ARCHITECTURE.md §8 (wrap flags, scrollback, underline styles, OSC 8, 2026,
      DECRQM, replies);
    - parse throughput on a 100 MB mixed fixture;
    - how easy it is to read cells and modes and to plug in our width function.
  - Writing our own is a valid answer. If a package is used, it is vendored or forked under
    `internal/vt` so we own its behaviour.
- **Acceptance:**
  - Decision **E10** is added to ARCHITECTURE.md §12, with a comparison table and benchmark
    numbers.
  - The spike code is deleted or moved into `internal/vt` as the starting point.
- **Depends on:** C0.1.

### [x] C0.6 Spike: Unix PTY and process information
- **Goal:** settle the Unix PTY library, and how `pane_current_command` / `pane_current_path`
  are read.
- **Details:**
  - Compare `github.com/creack/pty` with direct `golang.org/x/sys/unix` calls.
  - Prove on both macOS and Linux:
    - `setsid` and the controlling terminal;
    - process-group kill;
    - exit code and signal;
    - `tcgetpgrp`, and then the foreground process's name and cwd (Linux `/proc`, macOS
      `proc_pidinfo` / `sysctl`, without cgo if possible).
- **Acceptance:**
  - Decision **E11** is recorded.
  - A test on CI shows that a pane running `sh -c 'cd /tmp && exec sleep 5'` reports `sleep`
    and `/tmp` (resolved through symlinks) on both systems. Without `exec`, `sleep` runs in the
    shell's process group and the group's leader is the shell, so tmux reports the shell
    (`bash` for macOS's `/bin/sh`, checked on tmux 3.8); the test checks that too.
- **Depends on:** C0.2.

### [ ] C0.7 Spike: Windows ConPTY and IPC
- **Goal:** know the Windows ground before anything depends on it. It runs on GitHub's Windows
  runner; no Windows machine is needed.
- **Details:**
  - A small Go program in `spikes/conpty`:
    - create a pseudo console;
    - run `cmd.exe`, `powershell.exe` and a Go child that prints test bytes;
    - read, write and resize;
    - kill the whole tree with a job object.
  - Record which sequences ConPTY passes through, rewrites or drops: alternate screen, 2026,
    mouse modes, bracketed paste, OSC 8, OSC 52, OSC 7, CSI u / modifyOtherKeys, true colour,
    wide characters. Note which Windows builds and ConPTY flags change that.
  - IPC: compare AF_UNIX sockets with named pipes for Go on Windows:
    - a DACL limited to the current user's SID;
    - peer identity (client process token SID);
    - rejecting remote clients;
    - behaviour when the server dies.
  - Settle the minimum supported Windows version.
- **Acceptance:**
  - Decisions **E12** (ConPTY use and minimum Windows build) and **E13** (IPC transport and
    its security) are recorded with the evidence tables.
  - The spike's test runs on `windows-latest`.
- **Depends on:** C0.2.

### [ ] C0.8 Spike: shell commands on Windows
- **Goal:** decide what `run-shell`, `if-shell` and `default-shell` mean on Windows.
- **Details:**
  - i-mux's config and code build POSIX shell command lines today. Examples:
    `'<imux>' __menu --pane #{pane_id} >/dev/null 2>&1 || true` and `rm -f '<file>'`.
  - Options to weigh:
    - (a) the engine runs them with `pwsh` / `powershell.exe`, and i-mux generates
      PowerShell-quoted lines on Windows;
    - (b) `cmd.exe /d /s /c`;
    - (c) an engine extension: a `run-shell` argument that is an argv (JSON) run without any
      shell, which i-mux uses on every system for its own `imux …` calls.
  - Option (c) removes quoting bugs everywhere. If it is chosen, it is a documented extension
    in COMPATIBILITY.md, and i-mux uses it only with i-mux-core.
- **Acceptance:**
  - Decision **E14** is recorded, with what changes on the i-mux side; that work goes into
    C9.3.
- **Depends on:** C0.3.

**Gate 0:**
- CI is green on three systems.
- The contract is recorded with no open "(check)" marks.
- The differential harness works.
- E10 to E14 are decided.

---

## Phase 1: PTY and terminal emulator

### [ ] C1.1 Unix PTY
- **Goal:** panes can run programs on macOS and Linux.
- **Details:**
  - `internal/pty` implements the `PTY` and `Proc` interfaces (ARCHITECTURE.md §6) for Unix, per
    E11:
    - argv, env, cwd and size;
    - the child as session leader with the PTY as its controlling terminal;
    - `Resize` sends `TIOCSWINSZ`, which raises SIGWINCH in the child;
    - `Wait` returns code or signal;
    - `Kill` signals the process group: SIGHUP, then SIGKILL after a grace period, as tmux does
      on kill-pane.
  - No goroutine leak and no fd leak after `Close`.
- **Acceptance:** tests run `sh` scripts to check:
  - output is read;
  - input reaches the child;
  - a resize is seen by the child (`stty size`);
  - an exit code and a signal are reported;
  - kill ends a child that ignores SIGHUP;
  - the fd count returns to its start value.
- **Depends on:** C0.6.

### [ ] C1.2 VT parser
- **Goal:** turn bytes into well-defined actions, whatever the input.
- **Details:**
  - DEC ANSI state machine: ground, escape, escape intermediate, CSI entry/param/intermediate/
    ignore, OSC string, DCS entry/param/intermediate/passthrough/ignore, SOS/PM/APC string.
  - UTF-8 decoding with replacement characters for invalid sequences.
  - C1 controls in 7-bit and 8-bit form, as tmux handles them.
  - Parameter limits: 32 params, with sub-parameters (`:`).
  - String limits: OSC and DCS capped (e.g. 1 MiB); anything over is dropped safely.
  - The parser is split across `Write` calls at any byte boundary.
- **Acceptance:**
  - A table test for every state transition.
  - A fuzz target: no panics, bounded memory.
  - A split-at-every-byte test gives the same actions as a single write.
- **Depends on:** C0.5.

### [ ] C1.3 Screen, cells and width
- **Goal:** the data model every other part reads.
- **Details:**
  - Cells, lines (with a wrapped flag) and the grid, per ARCHITECTURE.md §8.
  - **One width function** for the whole program: it matches tmux's widths for the fixture set
    (CJK, emoji, ZWJ sequences, VS15/VS16, combining marks, regional indicators).
  - Grapheme clusters are stored per cell. The second cell of a wide character is a
    continuation cell.
  - Writing over half of a wide character clears both halves, as tmux does.
- **Acceptance:**
  - Unit tests for the width table.
  - Fixtures with wide characters and combining marks match tmux (via C1.12).
- **Depends on:** C1.2.

### [ ] C1.4 Cursor and editing sequences
- **Goal:** programs that draw full screens look right.
- **Details:**
  - Cursor movement: CUU, CUD, CUF, CUB, CNL, CPL, CHA, CUP/HVP, VPA, HPA, HPR, VPR.
  - Erasing and editing: ED (0/1/2/3), EL, ECH, ICH, DCH, IL, DL, REP.
  - Scrolling: DECSTBM scroll regions, SU, SD, IND, RI, NEL.
  - Tabs: HT, HTS, TBC, CHT, CBT.
  - Cursor save and modes: DECSC/DECRC, IRM, DECOM, DECAWM with the pending-wrap state, DECALN.
  - Resets: RIS, DECSTR.
  - BS, CR, LF/VT/FF, and LNM as tmux does.
- **Acceptance:** a fixture for each sequence, including edge cases (cursor at the margins,
  inside and outside the scroll region, pending wrap). All match tmux.
- **Depends on:** C1.3, C1.12 (the fixture tool can grow alongside).

### [ ] C1.5 SGR: colours and attributes
- **Goal:** colours and text styles exactly as programs set them.
- **Details:**
  - Every SGR attribute in ARCHITECTURE.md §8.
  - Colours: 30–37/90–97, 40–47/100–107, 38/48 with `;5;n` and `;2;r;g;b`, in both `;` and `:`
    forms, plus the `:2::r:g:b` colourspace form.
  - Underline styles `4:0`–`4:5`; underline colour 58/59.
  - 21 as double underline, as tmux does.
  - Resets: 0, 22–29 and 39/49/59.
- **Acceptance:** fixtures for every attribute and colour form match tmux's `capture-pane -e`.
- **Depends on:** C1.3.

### [ ] C1.6 Modes and the alternate screen
- **Goal:** the modes programs turn on are tracked and acted on.
- **Details:**
  - DECSET/DECRST for:
    - cursor and keypad: 1 (DECCKM), 6, 7, 12, 25, 66 (DECNKM), and 3 as tmux treats it;
    - screens: 47, 1047, 1048, 1049;
    - mouse: 1000, 1002, 1003, 1004, 1005, 1006;
    - 2004, 2026, 2031.
  - SM/RM 4 and 20.
  - Keypad mode: DECKPAM/DECKPNM.
  - DECRQM answers for each.
  - Modes are exposed through getters for the input path and the renderer.
  - 1049 saves and restores the cursor and clears the alternate screen, as tmux does.
- **Acceptance:**
  - Fixtures for alternate-screen switches with content on both screens.
  - DECRQM replies match tmux (via the probe in C1.8).
- **Depends on:** C1.4.

### [ ] C1.7 OSC and DCS
- **Goal:** titles, links, clipboard and colour queries work.
- **Details:**
  - Titles and cwd:
    - OSC 0/1/2 set the title (`pane_title`, `select-pane -T` overrides it);
    - OSC 7 sets the cwd (a `file://host/path` URI, percent-decoded);
    - OSC 9;9 sets the cwd (Windows Terminal style).
  - Links: OSC 8 hyperlinks, with ids and params, stored per cell.
  - Clipboard: OSC 52 set goes to a paste buffer, and to the client terminal per
    `set-clipboard`. OSC 52 read (`?`) is refused.
  - Colours: OSC 4/10/11/12 queries answered (from the client's colours when known), sets
    tracked; resets 104/110/111/112.
  - Other OSCs are ignored without breaking the parser (133 shell integration, for example).
  - DCS: tmux's handling of XTGETTCAP and DECRQSS for the ones tmux answers; everything else is
    ignored.
- **Acceptance:** fixtures and probe tests match tmux's titles, captured screens and replies.
- **Depends on:** C1.6.

### [ ] C1.8 Replies to the program
- **Goal:** programs that query the terminal get the answers they get under tmux.
- **Details:**
  - DA1, DA2, DA3 if tmux answers it.
  - DSR 5 and 6, DECXCPR.
  - XTVERSION (`CSI > q`).
  - Window-size queries (`CSI 14 t`, `16 t`, `18 t`) where tmux answers them.
  - The kitty keyboard query `CSI ? u`: answer exactly as tmux 3.8 does.
  - A **probe program** (`testdata/probe`) sends each query and prints the reply, hex-encoded.
    The difftest runs it in tmux and in i-mux-core.
- **Acceptance:** every probe reply is identical, or the difference is recorded in
  COMPATIBILITY.md §12 with a reason.
- **Depends on:** C1.7, C2.6 (for the difftest run; the unit part can come earlier).

### [ ] C1.9 Fake TUI for tests
- **Goal:** a stand-in for agent CLIs that exercises what they do to a terminal, without
  running one.
- **Details:**
  - `testdata/faketui`, a Go program with modes chosen by flag:
    - draw a full-screen frame in the alternate screen, with RGB colours, wide characters,
      emoji, box drawing and an OSC 8 link, wrapped in 2026;
    - echo every key it receives (raw bytes and decoded) at a fixed line;
    - echo mouse events;
    - echo bracketed paste;
    - redraw on resize;
    - stream N MB of mixed output;
    - set the title, set the cwd (OSC 7), copy (OSC 52);
    - exit with a given code or signal.
  - It builds for all three systems.
- **Acceptance:**
  - Each mode is covered by an engine test.
  - The program has no dependencies beyond the standard library and `x/sys`.
- **Depends on:** C0.1.

### [ ] C1.10 Scrollback and history
- **Goal:** history and `capture-pane` behave like tmux.
- **Details:**
  - A ring of lines up to `history-limit`. Only the main screen scrolls into history.
  - `clear-history` semantics for ED 3.
  - `capture-pane`:
    - `-S -N` and `-E`;
    - `-J` joins wrapped lines and keeps trailing spaces as tmux does;
    - `-e` emits escape sequences for attributes;
    - `-N` keeps trailing spaces;
    - `-p` prints.
  - Memory use is bounded: compact line storage, with no per-cell allocation for blank cells.
- **Acceptance:**
  - Difftest captures with history (`seq 1 5000` in a pane, then `capture-pane -p -J -S
    -2000`) match tmux.
  - A memory benchmark is recorded.
- **Depends on:** C1.4, C1.5.

### [ ] C1.11 Resize and reflow
- **Goal:** resizing a pane keeps its content the way tmux does.
- **Details:**
  - Width changes reflow wrapped lines, in the main screen and in the history, as tmux does.
  - Height changes move content into or out of the history, keeping the cursor line visible.
  - On the alternate screen, the content is cut or padded without reflow.
  - The cursor position follows its content.
- **Acceptance:** difftest scenarios (fill, wrap, resize narrower and wider, then capture with
  history) match tmux on both screens.
- **Depends on:** C1.10.

### [ ] C1.12 VT fixture suite
- **Goal:** one place where emulator behaviour is pinned to tmux's.
- **Details:**
  - Fixtures are `testdata/vt/<name>.in`: byte streams written by hand or generated by
    scripts, never recorded from agent CLIs.
  - `cmd/vtfixture` creates the `<name>.screen` expectations **from real tmux**: it runs a
    tmux pane sized as the fixture says, writes the bytes with a helper program, and saves
    `capture-pane -p -e -N` together with the cursor position, the cursor visibility and the
    modes (`#{cursor_x}`, `#{cursor_y}`, `#{cursor_flag}`, `#{alternate_on}`, …).
  - A unit test feeds every `.in` to `internal/vt` and compares the result with its `.screen`.
- **Acceptance:**
  - Fixtures cover every sequence in C1.4–C1.7, wide characters, and wrapping at the margins.
  - All pass.
  - Regenerating the fixtures from tmux gives no changes (checked in CI).
- **Depends on:** C1.3, C0.4.

### [ ] C1.13 Emulator performance and fuzzing
- **Goal:** the emulator is fast and does not crash on hostile input.
- **Details:**
  - Benchmarks of plain-text, colour-heavy and cursor-heavy streams, in MB/s and allocations
    per KB.
  - Fuzz targets for the parser and screen together.
  - A 10-minute fuzz run per target in a nightly CI job.
- **Acceptance:**
  - Baselines are recorded in `BENCHMARKS.md`.
  - The fuzz runs are clean.
  - Parse throughput is enough for ARCHITECTURE.md §10 (100 MB in well under tmux's time × 1.5).
- **Depends on:** C1.10.

**Gate 1:**
- Every VT fixture matches tmux.
- The probe replies match tmux.
- The fuzz runs are clean.
- Benchmarks are recorded.

---

## Phase 2: Server, client, panes

### [ ] C2.1 IPC protocol
- **Goal:** client and server talk safely and simply.
- **Details:**
  - `internal/ipc`, per ARCHITECTURE.md §5:
    - frame encoder and decoder, with the 1 MiB cap;
    - the message types;
    - the protocol version in `Hello`;
    - a version mismatch reported as an error that i-mux classifies as no server.
  - Back-pressure: a per-client output queue with a cap. A client that stays over it is
    detached with the reason `too slow`.
- **Acceptance:**
  - Round-trip tests for every message.
  - A fuzz target for the decoder.
  - Oversized frames close the connection.
  - A version mismatch gives the expected error.
- **Depends on:** C0.1.

### [ ] C2.2 Socket directory and peer checks (Unix)
- **Goal:** only the server's own user can connect (E5).
- **Details:**
  - The socket dir is `$TMUX_TMPDIR` or `/tmp`, then `imux-core-<uid>`:
    - created `0700`;
    - verified to be owned by the user, a directory and not group- or world-accessible (as tmux
      checks);
    - otherwise the server refuses, naming the problem.
  - The socket is `0600`.
  - Peer credentials: `SO_PEERCRED` on Linux, `getpeereid` on macOS. Any other uid is closed
    without a reply.
- **Acceptance:**
  - Tests: a wrong-mode dir is refused; the wrong owner is refused, tested with an injected
    check function; a peer with another uid is refused, tested by injecting credentials.
  - A security note is added to ARCHITECTURE.md §7 if anything changed.
- **Depends on:** C2.1.

### [ ] C2.3 Server start, lifetime and stale sockets
- **Goal:** `new-session -d` starts a server when there is none, as tmux does.
- **Details:**
  - `imux-core __server` is started by the client, detached (`setsid`, null stdio).
  - The client waits for the socket with a timeout.
  - The server loads `-f` before accepting commands.
  - The server exits when the last session closes, and removes its socket.
  - A socket left by a dead server (connection refused) is removed and a new server started, as
    tmux does.
  - Commands that need a server, when there is none, print `no server running on <path>` with
    exit code 1.
- **Acceptance:**
  - Tests: start; second client reuses the server; last session closes and the server exits; a
    stale socket is replaced; no-server errors match tmux (difftest).
- **Depends on:** C2.2, C2.4.

### [ ] C2.4 Model and server loop
- **Goal:** the tree of sessions, windows and panes, owned by one goroutine (E2).
- **Details:**
  - `internal/model`: ids (`$N @N %N`), names, indexes, creation and destruction, the
    last-used ordering for defaults.
  - `internal/server`: the event loop, with events from clients, panes, timers and jobs.
  - Pane reader goroutines feed `vt` under the pane's mutex and coalesce change notices.
  - A pane exit becomes an event in the loop.
- **Acceptance:**
  - Unit tests for every tree operation.
  - A race-detector test with 20 panes streaming while commands run.
  - No goroutine leak after the server shuts down.
- **Depends on:** C1.1, C1.3.

### [ ] C2.5 Layouts
- **Goal:** splits, resizes, swaps, zoom and saved layouts match tmux exactly.
- **Details:**
  - A layout tree of horizontal and vertical containers and pane leaves, with sizes in cells
    and one-cell borders.
  - Split options:
    - `-h` / `-v`;
    - `-b` (before);
    - `-f` (full width or height, at the window's root);
    - `-l N` and `-l N%`.
  - Closing a pane gives its space to a neighbour, by tmux's rules.
  - `resize-pane` with `-L -R -U -D N` and `-x` / `-y`; `-Z` zoom.
  - `swap-pane` with `-s -t -d`.
  - `#{window_layout}` strings, including tmux's checksum, and `select-layout <string>` to
    restore one. i-mux saves and restores these, so they must be byte-identical to tmux's.
  - Window resize distributes space as tmux does.
- **Acceptance:** difftest scenarios (the i-mux layouts: sidebar `-h -b -f -l 30`, agent splits,
  nested splits, kill in the middle, resize, swap, zoom, save and restore) produce identical
  `list-panes -F '#{pane_id} #{pane_left} #{pane_top} #{pane_width} #{pane_height}'` and
  `#{window_layout}` output.
- **Depends on:** C2.4.

### [ ] C2.6 The command client
- **Goal:** `imux-core [-L|-S] [-f] [-u] cmd ; cmd` works like tmux's command line.
- **Details:**
  - argv parsing:
    - global flags;
    - splitting at a lone `;`;
    - `\;` escapes;
    - `-V`.
  - `Hello`, `Command`, `Result`.
  - stdout, stderr and the exit code exactly as tmux prints them.
  - An empty command list behaves like tmux's (`new-session`).
  - Runs fine when stdin is not a terminal, which is how i-mux calls it.
- **Acceptance:** difftest of a set of basic commands (`list-sessions` with no server,
  `new-session -d -P -F`, `has-session`, `display-message -p`) is identical.
- **Depends on:** C2.3, C3.2 (for the command table: build a minimal one first and grow it).

### [ ] C2.7 The attached client (Unix)
- **Goal:** a terminal attaches, shows the session and gives it back clean.
- **Details:**
  - Console:
    - raw mode;
    - the size, and resize via SIGWINCH;
    - restore on every exit path (normal, detach, server lost, signal).
  - On attach, set up the terminal as tmux does:
    - smcup;
    - keypad, bracketed paste, focus events and mouse modes per the options;
    - extended keys enabled the way tmux 3.8 does for terminals with `extkeys`.
  - On detach, undo all of it.
  - Record the exact enter and leave sequences tmux sends (difftest through a PTY) and match
    them.
  - Terminal capabilities: from `TERM`'s terminfo where available, plus `terminal-features`
    (`RGB`, `extkeys`, `Sync`, …) and `terminal-overrides`, as tmux does.
  - The client's `TERM`, size and environment go into `Hello`.
- **Acceptance:**
  - An attach and detach cycle in a test PTY restores every mode (checked by feeding the client
    output to `vt` and comparing modes before and after).
  - The enter and leave sequences match tmux's.
- **Depends on:** C2.6.

### [ ] C2.8 Renderer
- **Goal:** each client sees its session drawn correctly, with minimal output.
- **Details:**
  - Compose the frame: panes at their offsets, borders, the status line area (filled in C4.2),
    and overlays (filled in C4.6–C4.10).
  - Diff against the last frame and write the fewest sequences:
    - cursor moves;
    - SGR changes;
    - runs of repeated characters;
    - clear-to-end-of-line.
  - Colours:
    - RGB when the client has `RGB`;
    - else the nearest of 256, as tmux maps them;
    - else 16.
  - Synchronized output (2026) around each frame when the client supports it (the i-mux
    config's `Sync` override).
  - The cursor goes to the active pane's cursor, or the overlay's, with its style and
    visibility.
  - A pane in its own 2026 update keeps its last complete frame until it ends or times out
    (tmux's timeout).
  - Frame rate: at most one frame per 16 ms per client, and right away after input.
- **Acceptance:**
  - Headless-client tests (C2.9) for a few layouts match tmux frame for frame.
  - A benchmark shows that one changed cell produces a small, bounded write.
- **Depends on:** C2.5, C2.7.

### [ ] C2.9 Headless client in the differential harness
- **Goal:** compare what users actually see, not only command output.
- **Details:**
  - `difftest` gets a `screen WxH` step:
    1. attach a client inside a PTY the harness owns;
    2. feed the client's output into a `vt.Screen`, wait for it to settle (no output for 100 ms
       and no pending 2026);
    3. compare the cells (characters, colours, attributes) and the cursor with tmux's,
       produced the same way.
  - A failure prints both screens side by side, with the differing cells marked.
- **Acceptance:**
  - The harness detects a planted one-cell difference.
  - The basic layout scripts pass.
- **Depends on:** C2.8, C0.4.

### [ ] C2.10 Input from the client terminal
- **Goal:** keys, mouse, paste, focus and terminal reports are understood like tmux does.
- **Details:**
  - Key parsing:
    - UTF-8 text;
    - C0 controls as `C-x`;
    - Escape and Meta, using `escape-time`;
    - CSI and SS3 function keys;
    - modified keys in xterm form, modifyOtherKeys and CSI u.
  - Mouse: SGR (1006), and X10/UTF-8 encodings when that is all the client sends.
  - Bracketed paste from the client passes to the pane as a paste.
  - Focus in/out reports go to the pane when `focus-events` is on and the pane asked for them.
  - Terminal replies (DA, DECRPM, OSC 10/11 colours, DEC 2031 theme reports) are separated
    from keys and used:
    - colours answer the panes' queries;
    - 2031 fires `client-light-theme` / `client-dark-theme`.
- **Acceptance:**
  - A table test of byte sequences to key names (the names tmux shows in `list-keys`).
  - Theme reports fire the hooks.
  - Paste and focus reach the fake TUI's echo correctly (engine test).
- **Depends on:** C2.7.

### [ ] C2.11 Keys into panes
- **Goal:** programs get keys encoded the way they asked for.
- **Details:**
  - Encode a key for a pane from its modes: application cursor and keypad, extended keys (what
    the pane enabled: `CSI > 4 ; 1/2 m` or nothing), bracketed paste wrapping.
  - `send-keys` with key names and literal strings, `-l` if used, and `-M` for mouse (C5.3).
- **Acceptance:** for every key in COMPATIBILITY.md §7, the fake TUI's echo under i-mux-core
  matches its echo under tmux (difftest), in normal and application modes and with extended
  keys on and off.
- **Depends on:** C2.10, C1.9.

### [ ] C2.12 Several clients, sizes, detach
- **Goal:** more than one terminal on a session works like tmux.
- **Details:**
  - `window-size` with tmux's default (`latest`), and whatever C0.3 found i-mux relies on.
  - `client_*` formats.
  - `detach-client`.
  - `kill-session` or the server ending makes every attached client exit cleanly with tmux's
    messages (`[exited]`, `[detached (from session …)]`).
  - `switch-client -t` moves a client to another session.
- **Acceptance:** difftest with two headless clients at different sizes matches tmux; detach
  and exit messages match.
- **Depends on:** C2.9.

**Gate 2:**
- Two headless clients attach, see shell panes and detach, and every frame matches tmux.
- No leaks under `-race`.

---

## Phase 3: Commands, options, formats

### [ ] C3.1 Command syntax parser
- **Goal:** the config and command strings i-mux writes parse exactly as tmux parses them.
- **Details:**
  - `internal/cmdlang`:
    - single and double quotes, with tmux's escapes in double quotes (`\"`, `\\`, `\$`,
      `\u`…);
    - `;` and `\;`;
    - `{ … }` blocks, nested and across lines;
    - `#` comments and `\` line continuation;
    - `~` and `$VAR` / `${VAR}` expansion where tmux does it;
    - `%if` / `%elif` / `%else` / `%endif` with formats.
  - Errors carry the file and line as tmux prints them.
- **Acceptance:**
  - Table tests.
  - A fuzz target.
  - The **generated i-mux tmux.conf files** captured in C0.3 parse with no error, and each one's
    commands are listed the same as tmux shows them (`list-keys`, `show-options -g`, compared
    with the difftest).
- **Depends on:** C0.3.

### [ ] C3.2 Command table and flags
- **Goal:** one place that defines every command's flags and targets.
- **Details:**
  - A table of name, alias, getopt-style flag spec, argument count, target kinds and handler.
  - Flags combine (`-gqv`); flags take values (`-t x`, `-tx`); `--` ends flags.
  - An unknown command or flag gives tmux's exact error text.
- **Acceptance:** table tests; difftest of error messages for bad commands and flags.
- **Depends on:** C3.1.

### [ ] C3.3 Targets
- **Goal:** `-t` and `-s` find what tmux finds.
- **Details:**
  - COMPATIBILITY.md §2: ids, `=name`, `name:index.pane`, `=` (the mouse), and the defaults
    with and without a current client.
  - Errors use tmux's text.
- **Acceptance:** difftest over every target form, including errors.
- **Depends on:** C3.2.

### [ ] C3.4 Options
- **Goal:** `set-option` and `show-options` behave like tmux for every option i-mux uses.
- **Details:**
  - The option table:
    - every option in COMPATIBILITY.md §5;
    - the options the default status line needs (`status-left`, `window-status-format`,
      `display-time`, `status-interval`, `base-index`, `default-shell`, `remain-on-exit`,
      `word-separators`, …);
    - tmux 3.8's defaults for each.
  - Scopes and inheritance; user options at every scope.
  - Flags: `-g -u -q -p -w -s -t -a -F -o`.
  - Arrays (`status-format[N]`, `terminal-features`, `terminal-overrides`).
  - `show-options` output formatting and quoting, and `-v`.
  - Changing an option redraws what depends on it.
- **Acceptance:** difftest of set and show for every option in the contract, in every scope i-mux
  uses, including unset and inheritance.
- **Depends on:** C3.3.

### [ ] C3.5 Format engine
- **Goal:** every format i-mux writes expands as it does in tmux.
- **Details:**
  - Variables from COMPATIBILITY.md §4, each tied to the model.
  - Every modifier listed, with nesting.
  - `#[…]` styles kept for the status renderer.
  - `%H:%M` strftime.
  - Limits on nesting depth and expansion size.
  - `display-message -p` as the main test entry point.
- **Acceptance:**
  - A table of at least 200 format expressions, including **every format string found in
    i-mux's source and generated config**, gives identical results under both engines
    (difftest), in sessions with several windows, panes and user options.
  - A fuzz target.
- **Depends on:** C3.4.

### [ ] C3.6 Session, window and pane commands
- **Goal:** the commands i-mux uses to build its workspace work like tmux.
- **Details:**
  - Implement every command in COMPATIBILITY.md §3 under "session", "window" and "pane":
    `new-session`, `attach-session`, `has-session`, `kill-session`, `list-sessions`,
    `new-window`, `select-window`, `rename-window`, `kill-window`, `list-windows`,
    `split-window`, `select-pane`, `swap-pane`, `kill-pane`, `respawn-pane`, `resize-pane`,
    `select-layout`, `list-panes`, `capture-pane`, `switch-client`, `detach-client`.
  - Implement every flag i-mux uses for each.
- **Acceptance:** a difftest script per command, covering every flag i-mux uses and the error
  cases.
- **Depends on:** C3.5, C2.5.

### [ ] C3.7 Names, titles, current command and path
- **Goal:** windows and borders show what tmux would show.
- **Details:**
  - `automatic-rename` with `automatic-rename-format`, refreshed as tmux does.
  - Pane titles from OSC 2 and `select-pane -T`.
  - `pane_current_command` and `pane_current_path` through `ProcInfo` (E11), refreshed on demand
    when a format asks.
- **Acceptance:** difftest: a pane running `sleep` in a directory shows the same window name,
  border text and formats under both engines.
- **Depends on:** C3.6.

### [ ] C3.8 Key tables and bindings
- **Goal:** the keys i-mux binds do what they do in tmux.
- **Details:**
  - `bind-key` with `-n` and `-T`, and commands or `{ … }` blocks; `unbind-key -q -T`.
  - The prefix key and the `prefix` table.
  - `switch-client -T` one-key tables: an unbound key leaves the table and is looked up in
    `root`.
  - tmux 3.8's default bindings for the keys i-mux leaves unbound (`list-keys` in the
    difftest).
  - Key name parsing and printing match tmux's.
  - `list-keys` is implemented for tests.
- **Acceptance:**
  - Difftest of `list-keys` after loading the i-mux config.
  - Engine tests drive keys through a headless client and check the effects, including the
    `imux-resize` table.
- **Depends on:** C3.6, C2.10.

### [ ] C3.9 Hooks
- **Goal:** i-mux's hooks fire at the same moments, with the same context.
- **Details:**
  - `set-hook -g` (and `-u` if C0.3 finds it).
  - Firing for every hook in COMPATIBILITY.md §6, with the context set so formats expand to the
    event's pane, window, session or client.
  - `after-<command>` hooks for the commands i-mux hooks.
- **Acceptance:** difftest: hooks that run `display-message -p`-style logging to a file give
  identical logs (order and expanded values) for scripted events: split, kill, focus change,
  pane exit, pane death with `remain-on-exit`, attach, theme report.
- **Depends on:** C3.8.

### [ ] C3.10 run-shell and if-shell
- **Goal:** i-mux's bindings run its commands as they do under tmux.
- **Details:**
  - `run-shell`:
    - formats are expanded first;
    - `-b` in the background;
    - `-d N` delays (fractional seconds);
    - `-C` runs a tmux command instead of a shell;
    - `-t` sets the target;
    - output and a non-zero exit of a foreground job show in the pane's view mode, as tmux does.
  - `if-shell`:
    - `-F` evaluates a format;
    - the shell form's exit code picks the branch;
    - the branches are commands or `{ … }` blocks.
  - Jobs get `TMUX` and `TMUX_PANE` as tmux sets them.
  - On Unix the shell is `/bin/sh -c`.
- **Acceptance:** difftest of each form, including timing order with `-d`, and the view-mode
  output of a failing foreground job.
- **Depends on:** C3.9.

### [ ] C3.11 Config loading
- **Goal:** the config is loaded at start and on reload, as tmux does.
- **Details:**
  - `-f <file>` is loaded at server start.
  - `source-file [-q] <path>` works, including nesting and relative paths.
  - Errors are collected and shown as tmux shows them, in the first client's view mode, with
    file and line.
  - **The user's `tmux.local.conf`:** an unknown command there prints one warning per command
    name and the rest still loads (COMPATIBILITY.md §10).
- **Acceptance:** i-mux's generated config, and a reload sequence like i-mux's (`Reload`:
  unbind the old keys, then `source-file`), give the same `list-keys` and `show-options` as
  tmux.
- **Depends on:** C3.10.

### [ ] C3.12 Pane lifecycle
- **Goal:** dead and exited panes behave like tmux.
- **Details:**
  - `remain-on-exit` keeps a dead pane, with tmux's "Pane is dead" line
    (`remain-on-exit-format`).
  - `pane_dead`, `pane_dead_status` and `pane_dead_signal`.
  - `respawn-pane -k`.
  - `pane-died` vs `pane-exited`.
  - The last pane of the last window closing ends the session and, if it was the last session,
    the server.
- **Acceptance:** difftest for exit codes 0 and 3, a kill by signal, respawn, and the last-pane
  case.
- **Depends on:** C3.9.

### [ ] C3.13 Environment of panes and jobs
- **Goal:** programs in panes see the environment they see under tmux.
- **Details:**
  - `TMUX` (`<socket>,<server pid>,<session id>`), `TMUX_PANE`, `TERM` from `default-terminal`.
  - The global environment, the session environment and `update-environment` from the client,
    as tmux does at session creation.
  - `default-shell` and `default-command`.
- **Acceptance:** difftest: `env | sort` in a new pane (normalized for pids and paths) is the
  same under both engines.
- **Depends on:** C3.6.

**Gate 3:**
- Every command, flag, format, option, hook and key in COMPATIBILITY.md has a passing difftest.

---

## Phase 4: UI surfaces

### [ ] C4.1 Styles
- **Goal:** `#[…]` and the `*-style` options draw as tmux draws them.
- **Details:**
  - Parse colours (names, `colourN`, `#rrggbb`, `default`, `terminal`) and attributes, with `no`
    forms.
  - `align=`, `range=` and `norange`, `list=` if needed, `fill=`.
  - Apply them to cells.
- **Acceptance:** unit tests, plus frame difftests in C4.2–C4.4.
- **Depends on:** C3.5, C2.8.

### [ ] C4.2 Status lines
- **Goal:** i-mux's tab bar and agent bar look and click exactly as on tmux.
- **Details:**
  - `status on|off|2..5`, `status-position top|bottom`, `status-format[N]`.
  - With `status-format[0]` unset, tmux's default status line, from `status-left`, the window
    list and `status-right`.
  - Alignment and width handling with the shared width function.
  - Ranges are recorded per client for clicks: `mouse_status_range` gives `window|N` → the
    index, and `user|X` → `X`.
  - Include tmux 3.4+'s "a range is one cell longer than drawn" behaviour, which i-mux works
    around.
  - `status-interval` refresh.
  - `status-style`.
- **Acceptance:** frame difftests for i-mux's status setups (tabs on and off, agent bar on and
  off, both) at several widths, with long names and wide characters, match tmux cell by cell.
  Click ranges match (C5.1 checks them by clicking).
- **Depends on:** C4.1.

### [ ] C4.3 Window list defaults
- **Goal:** the default status line's window list matches tmux.
- **Details:**
  - `window-status-format`, `window-status-current-format`, the separator and the styles, with
    tmux 3.8 defaults.
- **Acceptance:** frame difftest with 1, 3 and 12 windows, including the current and last-window
  markers.
- **Depends on:** C4.2.

### [ ] C4.4 Pane borders
- **Goal:** borders, the active border and border status lines look the same.
- **Details:**
  - Border cells and their joining characters, as tmux draws them (UTF-8 box drawing, or ACS
    when the client lacks UTF-8).
  - `pane-border-style` and `pane-active-border-style`, including tmux's half-highlighting of
    shared borders.
  - `pane-border-status top` with `pane-border-format`.
  - `pane-border-lines` default.
- **Acceptance:** frame difftests of the i-mux layouts (sidebar plus 1 to 4 agent panes, zoomed
  and not) match tmux.
- **Depends on:** C4.1.

### [ ] C4.5 Messages
- **Goal:** `display-message` shows like tmux.
- **Details:**
  - The message replaces the status line for `display-time`, or `-d N` ms; `-d 0` until a key.
  - `message-style`.
  - `-c` picks the client and `-t` the context.
  - `-p` prints instead of showing.
- **Acceptance:** frame difftests at the moment of display, and after expiry.
- **Depends on:** C4.2.

### [ ] C4.6 Popups
- **Goal:** i-mux's popups (menus, pickers, install and update windows, editors) work like tmux's.
- **Details:**
  - `display-popup` with every flag in COMPATIBILITY.md §3:
    - size and position: percentages, cells and formats, centred by default, clamped to the
      client;
    - border lines and title;
    - styles;
    - `-E` closes on exit;
    - `-d` cwd and `-e` env;
    - `-C` closes the open popup.
  - The popup runs its command in its own PTY and `vt.Screen`.
  - Keys go to the popup while it is open. Mouse events inside it go to its program, if it asked
    for them.
  - `popup_*` formats are available while it is open.
  - A popup closes when its client detaches.
- **Acceptance:**
  - Frame difftests of a popup running the fake TUI, at the sizes and positions i-mux uses.
  - Keys and mouse reach the fake TUI the same way under both engines.
- **Depends on:** C4.4, C2.11.

### [ ] C4.7 Menus
- **Goal:** `display-menu` looks and behaves like tmux's.
- **Details:**
  - Item parsing: name, key and command; `""` separators; `-` disabled items.
  - Title.
  - Position from `-x` / `-y`: numbers, formats, and tmux's position letters (`R P M W S C`)
    where i-mux uses them.
  - Keyboard: the item keys, Up/Down/Home/End, Enter and Escape.
  - Mouse:
    - `-M` (the menu takes the mouse at once);
    - `-O` (it stays open when the button is released);
    - clicking outside closes it.
  - The chosen command runs in the menu's context.
  - Styles: `menu-style`, `menu-selected-style`, `menu-border-style`.
- **Acceptance:** frame difftests of i-mux's menus (pane menu, agent menu, sessions), with and
  without `-M -O`, keyboard and mouse choice.
- **Depends on:** C4.6.

### [ ] C4.8 Copy mode (subset)
- **Goal:** scrolling back and selecting text work as i-mux's bindings expect.
- **Details:**
  - Enter it with `copy-mode`; `-H` (hide the position indicator) and `-e` (leave at the
    bottom), plus `-u` if used.
  - Scroll with keys and the wheel.
  - Selection by mouse drag; `select-word` and `select-line` (via `send -X`) using
    `word-separators`.
  - `copy-pipe-and-cancel` (no command: copy to a buffer and, per `set-clipboard`, the
    terminal), `cancel`.
  - The default `copy-mode` and `copy-mode-vi` tables for these actions.
  - `mode-style`, `pane_in_mode`.
  - The exact list of keys is settled by C0.3 and recorded in COMPATIBILITY.md §3.
- **Acceptance:** engine tests, and difftest of buffer contents, for the double- and
  triple-click bindings in i-mux's config and for a wheel scroll in and out.
- **Depends on:** C4.4, C1.10.

### [ ] C4.9 Paste buffers and clipboard
- **Goal:** pasting, and copying to the user's clipboard, work as in tmux.
- **Details:**
  - `set-buffer -b -w`, `load-buffer -b`, `paste-buffer -b -d -p -t`.
  - Automatic buffer names.
  - OSC 52 to the client terminal per `set-clipboard` (default `external`).
- **Acceptance:** difftest of buffer contents and pasted bytes (the fake TUI's echo), with and
  without bracketed paste.
- **Depends on:** C2.11.

### [ ] C4.10 Command prompt
- **Goal:** the rename-tab prompt works.
- **Details:**
  - `command-prompt -I <initial> -p <prompt> <template>`.
  - Line editing: arrows, Home/End, Backspace/Delete, `C-u`, `C-w`, `C-a`, `C-e`.
  - `%%%` substituted, with quoting as tmux does.
  - Escape cancels.
  - `message-command-style`.
- **Acceptance:** engine test driving i-mux's rename binding; the resulting `@imux_tab` matches
  tmux for names with spaces and quotes.
- **Depends on:** C4.5.

### [ ] C4.11 i-mux's configuration end to end
- **Goal:** the whole i-mux look renders identically.
- **Details:**
  - Load each generated config captured in C0.3.
  - Build i-mux's standard workspace layouts with the same commands i-mux runs (taken from the
    records).
  - Compare frames.
- **Acceptance:** frame difftests of at least: the sidebar with one agent; two splits; tabs on
  with three tabs; the agent bar; light and dark themes. All match tmux.
- **Depends on:** C4.2–C4.10.

**Gate 4:**
- i-mux's configuration loads.
- Every UI surface matches tmux frame for frame in the difftests.

---

## Phase 5: Mouse

### [ ] C5.1 Mouse events and key names
- **Goal:** mouse presses become the same key names, with the same targets, as in tmux.
- **Details:**
  - Hit-testing a mouse position: pane, border, status line (left, right, default area,
    range), popup or menu.
  - Key names: `MouseDown/Up/Drag/DragEnd<N><Where>`, `WheelUp/Down<Where>`, and
    `SecondClick`, `DoubleClick` and `TripleClick` with tmux's timing.
  - `mouse_*` formats.
  - `-t =` resolves to the place under the mouse.
  - tmux 3.8's behaviour for a second press after the pane under it changed (from 3.7 it is a
    new press); i-mux handles it with `@imux_moved`.
- **Acceptance:** engine tests that send SGR mouse bytes through a headless client and record
  which binding ran and what `=` resolved to, for every mouse key in COMPATIBILITY.md §7.
  Results match tmux (difftest, using bindings that log).
- **Depends on:** C3.8, C4.2.

### [ ] C5.2 Default mouse bindings
- **Goal:** the mouse does what tmux's defaults do where i-mux doesn't override it.
- **Details:**
  - tmux 3.8's default root mouse bindings:
    - selecting a pane on press;
    - resizing on border drag;
    - wheel to copy mode;
    - selecting a window on a status click;
    - the right-click menus, if C0.3 shows i-mux relies on them.
- **Acceptance:** `list-keys -T root` difftest; engine tests for each default.
- **Depends on:** C5.1.

### [ ] C5.3 Mouse into panes
- **Goal:** programs that asked for the mouse get it, encoded as they asked.
- **Details:**
  - `send -M` forwards the current event to the pane per its mouse mode (1000, 1002 or 1003) and
    encoding (1006 or 1005), with coordinates relative to the pane.
  - `mouse_any_flag`.
  - When the pane did not ask for the mouse, its events go nowhere, as in tmux.
- **Acceptance:** the fake TUI's mouse echo matches tmux's for clicks, drags and the wheel in
  each mode.
- **Depends on:** C5.1.

### [ ] C5.4 Border drag resizing
- **Goal:** dragging a border resizes panes like tmux.
- **Details:** `MouseDrag1Border` → `resize-pane -M`, with tmux's rules.
- **Acceptance:** difftest: pane sizes after the same drag are identical.
- **Depends on:** C5.2.

**Gate 5:**
- Every mouse binding in i-mux's config behaves as on tmux in engine tests and difftests.

---

## Phase 6: Parity with tmux on macOS and Linux

### [ ] C6.1 Engine end-to-end suite
- **Goal:** realistic scenarios tested against the engine alone, runnable on all three systems
  later.
- **Details:**
  - `e2e/` in this repo, using the fake TUI. Scenarios:
    - attach, detach and reattach;
    - splits and kills;
    - popups and menus;
    - copy and paste;
    - mouse;
    - two clients;
    - server restart after the last session.
  - Each scenario drives a headless client and asserts on screens and formats.
- **Acceptance:** at least 30 scenarios, green on macOS and Linux CI.
- **Depends on:** Gate 5.

### [ ] C6.2 Run i-mux's e2e suite on i-mux-core, without touching i-mux
- **Goal:** measure parity with the real thing.
- **Details:**
  - `scripts/imux-e2e.sh` builds `imux-core` and puts a directory first on `PATH` holding a link
    named `tmux` to it. i-mux's harness finds `tmux` on `PATH`, and i-mux runs `tmux` unless
    `IMUX_TMUX_BIN` says otherwise.
  - It then runs `../i-mux`'s e2e suite.
  - A Go test-JSON report goes into `PARITY.md`: a table of every i-mux e2e test with its status
    and, for failures, a short cause and the task that fixes it.
  - Add a CI job that checks out i-mux at a pinned commit and runs it. It is non-blocking until
    C6.3 is done.
- **Acceptance:** the script runs end to end and `PARITY.md` is generated.
- **Depends on:** C6.1.

### [ ] C6.3 Fix until every i-mux e2e test passes
- **Goal:** full parity.
- **Details:**
  - Work through `PARITY.md`.
  - Every fix comes with a difftest or engine test that reproduces the cause in this repo.
  - Do **not** change i-mux's tests to make them pass. If a test depends on tmux behaviour that
    COMPATIBILITY.md missed, add that behaviour to the contract first.
- **Acceptance:**
  - All i-mux e2e tests pass on i-mux-core (105 at the time of writing; whatever the pinned
    commit has).
  - The CI job from C6.2 becomes blocking.
- **Depends on:** C6.2.

### [ ] C6.4 Performance
- **Goal:** meet ARCHITECTURE.md §10.
- **Details:**
  - `scripts/bench.sh` measures each target under tmux and i-mux-core on the same machine and
    writes `BENCHMARKS.md`.
  - Profile and fix whatever misses its target.
- **Acceptance:** every target met on the macOS and Linux CI runners, with the numbers recorded.
- **Depends on:** C6.3.

### [ ] C6.5 Soak and leak tests
- **Goal:** it runs for days.
- **Details:**
  - A nightly job: 4 sessions × 4 panes of the fake TUI streaming and redrawing, 2 clients
    attaching and detaching, commands every 100 ms, for 1 hour, built with `-race`.
  - Track goroutines, fds and RSS over time.
- **Acceptance:** no races, flat goroutine and fd counts, and RSS stable within history limits.
- **Depends on:** C6.3.

### [ ] C6.6 Real terminals: USER
- **Goal:** confirm on real terminals what CI cannot see.
- **Details:**
  - A checklist in `CHECKLIST.md` for the owner to run i-mux on i-mux-core
    (`IMUX_TMUX_BIN=imux-core`) in iTerm2, Terminal.app, Ghostty, kitty, WezTerm and GNOME
    Terminal (or whichever they use).
  - Things to check:
    - colours and themes;
    - wide characters and emoji;
    - mouse, scrolling and selection;
    - copying to the system clipboard;
    - paste;
    - image paste into an agent;
    - resizing;
    - a real agent session's look and speed.
- **Acceptance:** the owner signs off, or files issues that become tasks.
- **Depends on:** C6.3.

**Gate 6:**
- 100% of i-mux's e2e tests pass on i-mux-core.
- The benchmarks meet their targets.
- The soak test is clean.
- The owner has signed off C6.6.

---

## Phase 7: Integration into i-mux (i-mux repository)

All tasks here change `../i-mux`. They go behind the engine switch, and tmux stays the default.
i-mux's `make ci` must pass with tmux exactly as before.

### [ ] C7.1 Docs first in i-mux
- **Goal:** i-mux's design records the engine before any code.
- **Details:**
  - In i-mux's ARCHITECTURE.md: a new decision (the next free D number) for "two engines: tmux
    (default) and i-mux-core (opt-in)", the switch, version handling, packaging and fallback.
  - In i-mux's TASKS.md: a milestone with C7.2–C7.7 as its tasks.
  - AGENTS.md's "No native Windows" rule stays until Phase 9.
- **Acceptance:** a `docs: …` commit in i-mux.
- **Depends on:** Gate 6.

### [ ] C7.2 i-mux's e2e harness honours `IMUX_TMUX_BIN`
- **Goal:** run i-mux's suite on either engine without PATH tricks.
- **Details:**
  - `e2e/harness_test.go` uses `IMUX_TMUX_BIN` when set, else `tmux` on `PATH`, as today.
  - `make e2e-core` builds or locates `imux-core` and runs the suite with it.
- **Acceptance:** `make e2e` is unchanged, and `make e2e-core` passes.
- **Depends on:** C7.1.

### [ ] C7.3 Engine detection and versions
- **Goal:** i-mux knows which engine it talks to, and its feature level.
- **Details:**
  - `ParseVersion` accepts `imux-core X.Y.Z (tmux 3.8 compatible)` and maps it to tmux 3.8
    features, plus an `Engine` field.
  - `imux doctor` shows the engine and its version.
  - `imux doctor` warns when an i-mux-core older than i-mux's minimum is found.
- **Acceptance:** unit tests; doctor e2e for both engines.
- **Depends on:** C7.2.

### [ ] C7.4 The engine switch
- **Goal:** users can choose the engine, and tmux remains the default.
- **Details:**
  - Config `[engine] backend = "tmux" | "core"`, with default `tmux` on macOS and Linux.
  - With `core`, i-mux uses the `imux-core` next to its own binary, else on `PATH`.
    `IMUX_TMUX_BIN` still overrides everything.
  - The two engines use different socket directories, so they never collide.
  - Switching while sessions run: i-mux says sessions must be closed first, and offers to do it.
  - Settings shows the choice as "Engine (beta)".
- **Acceptance:**
  - Unit tests for the resolution order.
  - e2e: with the default, i-mux behaves exactly as before (the whole suite); with `core`, the
    suite passes.
- **Depends on:** C7.3.

### [ ] C7.5 Packaging and updates
- **Goal:** users who opt in get the engine with no extra steps.
- **Details:**
  - GoReleaser builds `imux-core` for darwin and linux (amd64 and arm64) and ships it in the same
    archives and the Homebrew formula.
  - `make install-local` installs both binaries.
  - The self-update (`imux update`, Settings → Update) replaces both, checksum-verified, as one
    step. If one fails, neither changes.
  - A running i-mux-core server keeps using the old binary until its sessions end; the update
    message says so.
- **Acceptance:**
  - A goreleaser snapshot contains both.
  - An update e2e (with a local release fixture, as i-mux's update tests do) replaces both.
- **Depends on:** C7.4.

### [ ] C7.6 CI for both engines
- **Goal:** neither engine can regress unnoticed.
- **Details:**
  - i-mux's CI gets an `e2e-core` job: it checks out i-mux-core at the pinned version, builds
    it, and runs `make e2e-core` on Linux and macOS.
  - The job is blocking.
- **Acceptance:** green on both systems.
- **Depends on:** C7.5.

### [ ] C7.7 Beta release: USER
- **Goal:** real users try the engine on macOS and Linux.
- **Details:**
  - Release notes explain the switch and how to go back.
  - An issue template "Engine (beta)" asks for the terminal, OS, and `imux doctor` output.
- **Acceptance:** the owner approves and tags the release.
- **Depends on:** C7.6.

**Gate 7:**
- i-mux ships with both engines.
- tmux is still the default.
- CI runs the full e2e suite on both engines.

---

## Phase 8: i-mux-core on Windows

### [ ] C8.1 ConPTY
- **Goal:** panes run programs on Windows.
- **Details:**
  - `pty_windows.go` per E12:
    - a pseudo console per pane;
    - the child started with the attribute list, env block and cwd;
    - put into a job object (`JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`);
    - read and write through the pipes;
    - `Resize` via `ResizePseudoConsole`;
    - `Wait` returns the exit code;
    - `Kill` ends the job.
  - Handles are closed in the right order, so reads end and nothing leaks.
- **Acceptance:** on `windows-latest`:
  - `cmd.exe /c echo`, a `pwsh` script and the fake TUI run;
  - output is read and input reaches them;
  - a resize is seen;
  - exit codes are reported;
  - kill ends grandchildren;
  - the handle count returns to its start value.
- **Depends on:** Gate 6, C0.7.

### [ ] C8.2 ConPTY's output in the emulator
- **Goal:** what users see on Windows matches what they see on macOS and Linux.
- **Details:**
  - Run the VT fixture suite through ConPTY: a helper child writes each fixture's bytes, and
    i-mux-core's pane captures the result.
  - Compare with the Unix expectations.
  - Fix what can be fixed in the emulator or in the ConPTY flags. Record what cannot in
    COMPATIBILITY.md §12 (K rows), with the Windows build it was seen on.
- **Acceptance:** the fixture comparison runs in Windows CI, and every difference is either fixed
  or recorded.
- **Depends on:** C8.1.

### [ ] C8.3 IPC and security on Windows
- **Goal:** only the user can reach their server on Windows too (E5, E13).
- **Details:**
  - The listener per E13:
    - a DACL with only the user's SID and SYSTEM, no inheritance;
    - remote clients rejected;
    - every peer's token SID checked.
  - The socket or pipe directory is under `%LOCALAPPDATA%\i-mux-core`, with the same ACL.
  - Stale endpoint handling.
- **Acceptance:**
  - Tests of the ACL (read back and checked) and of the SID check (with an injected identity).
  - If CI allows creating a second local user, a test that it is refused.
  - A short security review written in ARCHITECTURE.md §7.
- **Depends on:** C8.1.

### [ ] C8.4 The server process on Windows
- **Goal:** the server starts, lives and ends correctly, with no stray windows.
- **Details:**
  - Detached start: no console, no flashing window, and a new process group.
  - Lifetime as on Unix.
  - Children are created without their own visible console windows.
  - Logging to `%LOCALAPPDATA%\i-mux-core\logs` when `IMUX_CORE_LOG` is set.
- **Acceptance:** the engine tests for start, reuse, last-session exit and stale endpoint pass on
  Windows.
- **Depends on:** C8.3.

### [ ] C8.5 The attached client on Windows
- **Goal:** attaching from Windows Terminal (and conhost) works and restores the console.
- **Details:**
  - Console modes per ARCHITECTURE.md §6; UTF-8 code pages (65001) set, then restored.
  - Resize events.
  - Ctrl+C reaches the pane as a key, without killing the client: handle console control events.
  - The console is restored on every exit path, including a crash (a deferred restore and the
    control handler).
- **Acceptance:** engine tests through a ConPTY-hosted client (the client runs inside a ConPTY
  the test owns, so its output can be read): attach, keys, resize, detach, and restored modes.
- **Depends on:** C8.4.

### [ ] C8.6 Process information on Windows
- **Goal:** window names and paths are useful on Windows.
- **Details:**
  - `pane_current_command`: the newest live process in the pane's job, by image name without
    `.exe`.
  - `pane_current_path`: the last OSC 7 / OSC 9;9 path, else the start directory (K1).
- **Acceptance:** tests with `pwsh` running `Start-Sleep` show `Start-Sleep`'s host or the
  expected name. Document exactly what is shown.
- **Depends on:** C8.1.

### [ ] C8.7 Shells and jobs on Windows
- **Goal:** `run-shell`, `if-shell` and the default shell work on Windows per E14.
- **Details:**
  - The decided shell or argv form.
  - `default-shell`: `pwsh` if installed, else `powershell.exe`, else `cmd.exe`, unless set.
  - `TMUX` and `TMUX_PANE` in jobs and panes, in a form i-mux parses on Windows (C9.3).
- **Acceptance:** engine tests of each `run-shell` and `if-shell` form on Windows.
- **Depends on:** C8.4, C0.8.

### [ ] C8.8 Engine e2e on Windows
- **Goal:** the engine's own scenarios pass on Windows.
- **Details:**
  - Run the C6.1 suite on `windows-latest`.
  - Scenarios that cannot apply are skipped with a reason, and each skip is listed in the
    suite's README.
- **Acceptance:** green on Windows CI, and blocking.
- **Depends on:** C8.5, C8.6, C8.7.

### [ ] C8.9 Windows Terminal check: USER
- **Goal:** confirm the real experience.
- **Details:** the CHECKLIST.md items in Windows Terminal on Windows 11, plus conhost on
  Windows 10 if that version is supported (E12).
- **Acceptance:** the owner signs off, or files issues.
- **Depends on:** C8.8.

**Gate 8:**
- The engine passes its own e2e suite on Windows.
- The owner has signed off C8.9.

---

## Phase 9: i-mux on Windows (i-mux repository)

### [ ] C9.1 Docs first
- **Goal:** i-mux's design allows native Windows, with i-mux-core as the engine there.
- **Details:**
  - i-mux's ARCHITECTURE.md: amend the "No native Windows" non-goal (D60) with a new decision.
  - i-mux's AGENTS.md: replace the rule. Windows-only code is allowed in build-tagged files
    behind small interfaces, and the Windows CI jobs are required.
  - i-mux's TASKS.md: a milestone for C9.2–C9.10.
- **Acceptance:** a `docs: …` commit in i-mux.
- **Depends on:** Gate 8.

### [ ] C9.2 i-mux builds for Windows
- **Goal:** `GOOS=windows go build ./...` and `go vet` succeed, with no behaviour change on
  macOS or Linux.
- **Details:** audit and replace the POSIX-only uses:
  - `syscall.Exec`: attach becomes "run the engine client as a child and wait, passing its exit
    code" on Windows;
  - `Setsid`;
  - signals (`SIGTERM`, `SIGHUP`, `SIGWINCH`);
  - `flock` (→ `LockFileEx`);
  - Unix file modes;
  - `/dev/null`;
  - symlink assumptions;
  - `$HOME` (→ `USERPROFILE`).
  
  Put each behind a small interface in build-tagged files.
- **Acceptance:**
  - The Windows build and vet are clean.
  - Lint runs for `GOOS=windows` in CI.
  - All existing tests pass on macOS and Linux.
- **Depends on:** C9.1.

### [ ] C9.3 Command strings without a POSIX shell
- **Goal:** every command line i-mux generates works on Windows.
- **Details:**
  - Change `UICommand`, `Silent`, `ShQuote` and `ShJoin`, the `run-shell` strings in the
    generated config, the popups' commands and every `sh -c` use per E14:
    - on Windows, the decided form;
    - on macOS and Linux, unchanged.
  - Parse `TMUX` and `TMUX_PANE` in their Windows form.
- **Acceptance:**
  - Unit tests of the generated config and commands for both platforms.
  - The macOS and Linux output is byte-identical to before (golden test).
- **Depends on:** C9.2.

### [ ] C9.4 Paths and agents' homes
- **Goal:** i-mux finds its own files and each agent's files on Windows.
- **Details:**
  - `~/.imux` under `%USERPROFILE%`.
  - Each agent's config and session paths on Windows (Claude Code, Codex, Copilot, Antigravity,
    OpenCode), each verified from that agent's own documentation and recorded in i-mux's
    ARCHITECTURE.md.
  - Path separators in session names, in git paths and in the sidebar.
- **Acceptance:** unit tests with synthetic Windows-style homes, using no real files.
- **Depends on:** C9.2.

### [ ] C9.5 Agents on Windows
- **Goal:** detecting, installing, starting, resuming and hooking each agent works.
- **Details:**
  - Binaries are found as npm's `.cmd` and `.ps1` shims or `.exe` files.
  - Install commands for Windows.
  - Hook installation into each agent's settings, using Windows paths and commands.
  - Resume commands.
- **Acceptance:** e2e on Windows CI with the fake agents, ported to Windows (Go or `.cmd`).
- **Depends on:** C9.3, C9.4.

### [ ] C9.6 Windows integrations
- **Goal:** the parts of i-mux that talk to the OS work on Windows.
- **Details:**
  - Opening URLs and files (`ShellExecute`).
  - Clipboard text and the image paste into agents (Win32 clipboard).
  - Notifications (a Windows toast).
  - The Nerd Font install: a per-user font, and the Windows Terminal profile set to it, with
    the user's consent.
  - The editor launch.
  - Git: Git for Windows is required, and `imux doctor` checks for it.
- **Acceptance:** unit tests with fakes, and Windows CI e2e where the OS allows.
- **Depends on:** C9.2.

### [ ] C9.7 Self-update on Windows
- **Goal:** Settings → Update i-mux works on Windows, with no installer run again.
- **Details:**
  - Download and verify both `imux.exe` and `imux-core.exe`.
  - Replace the running executables: rename the current one to `*.old`, write the new one,
    delete `*.old` on the next start.
  - Roll back if any step fails.
  - A running engine server keeps the old binary until its sessions end; Settings says so.
- **Acceptance:** an update e2e on Windows CI with a local release fixture: both replaced, the
  old files cleaned up on the next start, and the rollback tested.
- **Depends on:** C9.2.

### [ ] C9.8 Installer
- **Goal:** a Windows user installs i-mux once, from a familiar starting point.
- **Details:**
  - A per-user installer, needing no admin:
    - installs to `%LOCALAPPDATA%\Programs\i-mux`;
    - adds it to the user's `PATH`;
    - adds a Start menu entry that opens i-mux in Windows Terminal (falling back to conhost);
    - adds an uninstaller in Apps & features.
  - Build it on Linux CI: NSIS cross-builds there. No Windows build machine is needed.
  - Also publish:
    - an `install.ps1` one-liner for developers;
    - a winget manifest pointing at the installer.
  - Code signing is a USER decision (cost and identity check). Until then, the release notes
    explain the SmartScreen prompt.
- **Acceptance:** the installer is built in CI. A Windows CI job installs it silently, runs
  `imux doctor`, and uninstalls cleanly, with nothing left behind.
- **Depends on:** C9.7.

### [ ] C9.9 Windows CI for i-mux
- **Goal:** i-mux on Windows cannot regress unnoticed.
- **Details:** i-mux's CI gets a `windows` job: unit tests, plus the e2e suite on i-mux-core,
  with tests that cannot apply skipped and listed.
- **Acceptance:** green and blocking.
- **Depends on:** C9.5, C9.6.

### [ ] C9.10 Windows check: USER
- **Goal:** the full experience on a real Windows PC.
- **Details:** a checklist for the owner:
  - install from the installer;
  - open from the Start menu;
  - install an agent from i-mux;
  - sidebar, tabs, popups, menus and mouse;
  - image paste and notifications;
  - update from Settings;
  - uninstall.
- **Acceptance:** the owner signs off.
- **Depends on:** C9.9.

**Gate 9:**
- i-mux runs natively on Windows on i-mux-core.
- It installs once and updates from Settings.
- The owner has signed off.

---

## Phase 10: Rollout

### [ ] C10.1 Beta period on macOS and Linux
- **Goal:** gather real-world reports before any default changes.
- **Details:** track engine issues; every fix gets a difftest or engine test.
- **Acceptance:** at least two releases with the opt-in engine and no open serious engine bugs.
- **Depends on:** Gate 7.

### [ ] C10.2 Windows public release: USER
- **Goal:** announce Windows support.
- **Details:**
  - README and README.tr get a Windows section: installer, `install.ps1`, winget.
  - CHANGELOG.
  - Screenshots from Windows Terminal.
- **Acceptance:** the owner approves and tags.
- **Depends on:** Gate 9.

### [ ] C10.3 The default switch on macOS and Linux: USER
- **Goal:** one engine everywhere, so everything really is the same.
- **Details:** the criteria:
  - C10.1 is done;
  - every i-mux e2e test is green on both engines for the last two releases;
  - the benchmarks are met;
  - the owner approves.

  Then `backend = "core"` becomes the default, and existing users are told in the release notes
  how to go back.
- **Acceptance:** the owner approves and tags.
- **Depends on:** C10.1.

### [ ] C10.4 Keep the fallback
- **Goal:** users always have a way back.
- **Details:**
  - `backend = "tmux"` stays supported and tested in CI on macOS and Linux.
  - `imux doctor` suggests it when the engine fails to start.
  - Any decision to drop tmux support is a separate one, for the owner, much later.
- **Acceptance:** CI keeps both engines' e2e jobs.
- **Depends on:** C10.3.

---

## Dependency overview

```
Phase 0 ──► Phase 1 ──► Phase 2 ──► Phase 3 ──► Phase 4 ──► Phase 5 ──► Phase 6 (parity, macOS/Linux)
                                                                             │
                                          ┌──────────────────────────────────┼─────────────────┐
                                          ▼                                  ▼                 │
                                Phase 7 (i-mux: switch, tmux default)   Phase 8 (engine on Windows)
                                          │                                  │
                                          ▼                                  ▼
                                Phase 10.1 (beta)                     Phase 9 (i-mux on Windows)
                                          │                                  │
                                          ▼                                  ▼
                                Phase 10.3 (default switch)           Phase 10.2 (Windows release)
```

Phases 7 and 8 can run in parallel once Gate 6 holds.
