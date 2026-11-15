# i-mux-core architecture

This is the design of i-mux-core. [COMPATIBILITY.md](COMPATIBILITY.md) says *what* it must do (the
tmux behaviour i-mux uses); this file says *how*. [TASKS.md](TASKS.md) turns both into work.

Decisions are numbered (E1, E2, …) and listed in §12 so tasks and commits can refer to them.

---

## 1. Goals and non-goals

### Goals

- **Run i-mux unchanged.** Every tmux command, flag, format, option, hook and key that i-mux uses
  works on i-mux-core with the same results, on macOS and Linux first and then on Windows.
- **One codebase for three systems.** The platform-specific code is small and sits behind
  interfaces (§6).
- **Correct terminal emulation for agent TUIs.** Claude Code, Codex, Copilot, Antigravity and
  OpenCode are full-screen Node and Go programs. They use the alternate screen, synchronized
  output, bracketed paste, mouse reporting, extended keys, true colour, OSC 8 hyperlinks,
  OSC 52 clipboard and wide characters. These must all render and behave as they do under tmux.
- **Fast, and idle when idle.** No polling while nothing happens, input latency like tmux's,
  output throughput close to tmux's (§10).
- **Secure by default.** Only the user who started the server can talk to it, and nothing ever
  listens on the network (§7).

### Non-goals (v1)

- **Not a general tmux replacement.** Commands i-mux does not use are not implemented. An
  unknown command fails with `unknown command: <name>`, the way tmux reports one.
- **No control mode** (`-C`, `-CC`). i-mux only reads `#{client_control_mode}`, which is always 0.
- **No tmux plugins and no `~/.tmux.conf`.** The server reads only the `-f` file it is given,
  plus whatever that file sources.
- **No wire compatibility with real tmux.** A real tmux client cannot attach to i-mux-core, and
  the other way round.
- **No full copy mode.** It has the subset i-mux's bindings and mouse selection use, plus
  scrolling (COMPATIBILITY.md §8).
- **None of these:** `choose-tree`, `clock-mode`, server access lists, `pipe-pane`, `link-window`,
  window groups.

---

## 2. Process model

One binary, `imux-core` (`imux-core.exe` on Windows), plays three roles, as tmux does:

| Role | How it is started | What it does |
| --- | --- | --- |
| **Command client** | `imux-core -L imux [-f conf] [-u] cmd args [; cmd …]` | Connects to the server, sends the commands and prints their output. Its exit code is the result. |
| **Attached client** | `imux-core -L imux attach-session -t =name` (or `new-session` without `-d`) | Puts the terminal into raw mode, then forwards input to the server and draws what the server sends, until it detaches or the session ends. |
| **Server** | Started by a client when there is no server and the command needs one (`new-session`) | Owns every session, window, pane, PTY, option, binding and hook. Runs in the background, detached from any terminal. |

**Server start (E1):**
1. The client runs itself again as `imux-core __server -L <name> [-f conf]`, fully detached:
   `setsid`, stdio on the null device, and on Windows `DETACHED_PROCESS |
   CREATE_NEW_PROCESS_GROUP`.
2. It waits up to 5 s for the socket to accept a connection.
3. The server loads the `-f` config before it accepts its first command, as tmux does.

**Server end:** the server exits when its last session closes (tmux's `exit-empty on`). On exit
it removes its socket.

---

## 3. Packages

```
cmd/imux-core/            main: argv → client or server
internal/
  cli/                    argv parsing (-L -S -f -u -V -N …), the command client, the attached client
  ipc/                    framing, messages, protocol version, peer checks (§5, §7)
  server/                 the event loop, client registry, command dispatch, server lifetime
  model/                  Server, Session, Window, Pane, Client: the tree and its ids ($1 @1 %1)
  layout/                 layout tree: split, resize, swap, zoom, kill, layout strings (select-layout)
  pty/                    PTY interface; pty_unix.go, pty_windows.go (ConPTY)
  vt/                     terminal emulator: parser, screen, cells, scrollback, modes, replies
  render/                 composes a client's frame (panes, borders, status, overlays) and diffs it
  termout/                writes frames to the client's terminal: capabilities, colours, Sync
  input/                  parses the client terminal's input: keys, mouse, paste, focus, reports
  keys/                   key names, key tables, bindings, prefix handling, escape-time
  cmdlang/                tmux's command syntax: tokens, quotes, ; and {}, %if/%else/%endif
  commands/               one file per command (new_session.go, split_window.go, …)
  target/                 target resolution: -t =name, %pane, @window, $session, "=" (mouse)
  options/                option tables, scopes (server/session/window/pane), user options, arrays
  format/                 the format engine: #{var}, #{?c,a,b}, modifiers, #[style]
  style/                  style parsing, ranges, colours
  status/                 status lines: status-format[N], alignment, ranges, click mapping
  overlay/                popups, menus, messages, the command prompt
  copymode/               the copy-mode subset
  buffers/                paste buffers, OSC 52 clipboard
  hooks/                  hook tables and firing
  job/                    run-shell and if-shell: the shell abstraction, -b, -d, -C
  platform/               paths, socket directory, process info (current command and path), shell, console
  difftest/               the differential harness (tests only)
  version/                the engine's version and the tmux version it implements (E9)
testdata/
  vt/                     byte-stream fixtures and their expected screens
  faketui/                a small full-screen program for tests (task C1.9)
  diff/                   differential test scripts
```

Every package has a `doc.go` that says what it owns. Only `pty`, `platform`, `ipc` and the
console part of `cli` have platform files.

---

## 4. Data model and concurrency

### 4.1 The tree

```
Server
 ├─ options (server, global session, global window, global pane), key tables, hooks, buffers
 ├─ Clients  (attached clients: their terminal, size, session, key table, overlays)
 └─ Sessions ($id, name, cwd, options, windows in order, last window)
     └─ Windows (@id, index, name, options, layout tree, active pane, zoom)
         └─ Panes (%id, options, PTY, process, vt.Screen, title, cwd, dead/exit status)
```

- **Ids** are server-wide counters (`$0`, `@0`, `%0` …), never reused while the server runs, as
  in tmux.
- **Window indexes** start at `base-index` (0 by default) and are per session.

### 4.2 One owner (E2)

A single goroutine, the **server loop**, owns the tree, the options, the bindings, the hooks and
the clients. Everything that changes them is an event on its channel:

- a client message;
- pane output that has been parsed;
- a pane process exiting;
- a timer (`display-message -d`, `run-shell -d`, status refresh);
- a job finishing (`run-shell`, `if-shell`).

This keeps tmux's ordering guarantees (commands joined with `;` run in order and see each other's
effects), and the tree needs no locks.

### 4.3 Panes

Each pane has a **reader goroutine** that reads its PTY and feeds the bytes to the pane's
`vt.Screen` under the pane's own mutex. It then sends a coalesced "pane changed" notice to the
loop; at most one notice is pending per pane.

The loop reads screen state, for formats and rendering, under the same mutex. Replies the
emulator must send to the program (DA, DSR, OSC 10/11 colour queries) are written back to the
PTY by the reader goroutine.

A pane whose program exits:
- stays as a dead pane when `remain-on-exit` is on, with `pane_dead`, `pane_dead_status` and
  `pane_dead_signal` set, and fires `pane-died`;
- otherwise closes, and fires `pane-exited`.

### 4.4 Rendering

Each attached client has a renderer. When anything it shows changes, the client is marked dirty.
A per-client ticker (at most every 16 ms, and right away after input) then:

1. **Composes** the frame from:
   - the status lines;
   - every visible pane's screen, at its offset;
   - the pane borders and their `pane-border-status` lines;
   - the overlays: popup, menu, message and prompt, on top.
2. **Diffs** it against the last frame sent, cell by cell.
3. **Writes** the changes with the fewest escape sequences the terminal supports (`termout`).
4. **Wraps** the update in synchronized output (DEC mode 2026) when the terminal supports it, and
   ends it with the cursor where the active pane, or the overlay, wants it.

---

## 5. IPC protocol (E3)

- **Transport:**
  - macOS and Linux: Unix domain socket.
  - Windows: decided in spike C0.7. Either AF_UNIX, which Windows 10 1803+ and Go support, or a
    named pipe. Whichever is chosen must give peer identity (§7).
- **Socket path:** `<socket dir>/<name>`, where `name` comes from `-L` (default `default`) and
  `-S <path>` gives a full path. The socket dir is:
  - Unix: `$TMUX_TMPDIR` or `/tmp`, then `imux-core-<uid>`;
  - Windows: `%LOCALAPPDATA%\i-mux-core\sockets`.
- **Framing:** `[type u8][length u32 big-endian][payload]`, with a 1 MiB cap per frame. A frame
  over the cap closes the connection.
- **Payloads:** control messages are JSON, and terminal data (input and output) is raw bytes.
- **Messages:**

  | Message | Direction | Content |
  | --- | --- | --- |
  | `Hello` | client → server | protocol version, client version, argv, cwd, the environment i-mux needs (`TERM`, `COLORTERM`, `LANG`/`LC_*`, `TERM_PROGRAM`, `PATH`, `HOME`, `IMUX_*`, `-u`), terminal size, whether stdin/stdout are terminals, client pid |
  | `Command` | client → server | the command list (already split into argv lists, or the raw string for `source-file`-style input) |
  | `Result` | server → client | stdout, stderr, exit code |
  | `Attach` | server → client | the client switches to attached mode |
  | `Output` | server → client | terminal bytes to write |
  | `Input` | client → server | terminal bytes read |
  | `Resize` | client → server | new terminal size |
  | `Detach`, `Exit` | server → client | reason, exit code; the client restores its terminal and exits |
  | `Suspend` | server → client | Unix only: the client stops itself (SIGTSTP) |

- **Version check:** a protocol version mismatch is refused with an error that i-mux classifies
  as "no server", so a newer `imux-core` never talks to an old server.

---

## 6. Platform layer (E4)

Core packages see only these interfaces. Implementations live in `_unix.go` and `_windows.go`
files.

```go
// pty
type PTY interface {
    Start(spec Spec) (Proc, error) // argv, env, cwd, size
}
type Proc interface {
    io.ReadWriter
    Resize(cols, rows int) error
    Wait() (ExitStatus, error)  // code, signal (Unix), or NTSTATUS (Windows)
    Kill() error                // the whole process tree (process group / job object)
    Pid() int
    Close() error
}

// platform
type ProcInfo interface {
    ForegroundCommand(p Proc) string // #{pane_current_command}
    CurrentPath(p Proc) string       // #{pane_current_path}
}
type Shell interface {
    Command(line string) (argv []string) // for run-shell, if-shell, default-command
}
type Console interface {                 // the attached client's own terminal
    MakeRaw() (restore func(), err error)
    Size() (cols, rows int, err error)
    Resizes() <-chan struct{}            // SIGWINCH, or Windows buffer-size events
}
type Listener interface { /* Listen, Dial, PeerUser */ }
```

### Unix

- **PTY:** `openpty` (via `github.com/creack/pty` or `golang.org/x/sys/unix`, decided in C0.6).
  The child runs `setsid`, takes the PTY as its controlling terminal and gets its own process
  group.
- **Foreground command and path:**
  - Linux: `tcgetpgrp`, then `/proc/<pgrp>/comm` and `/proc/<pgrp>/cwd`;
  - macOS: `tcgetpgrp`, then `proc_pidinfo` / `sysctl` (`KERN_PROC_PID`), as tmux's osdep does.
- **Shell:** `default-shell` (or `$SHELL`, else `/bin/sh`). `run-shell` uses `/bin/sh -c`, as tmux
  does.

### Windows (Phase 8)

- **PTY:** ConPTY (`CreatePseudoConsole`), with the child in a **job object** so `Kill` ends its
  whole tree. The spike (C0.7) settles:
  - the minimum Windows build;
  - which ConPTY flags are available (inherit cursor, resize behaviour, passthrough on newer
    builds);
  - how ConPTY's re-rendering affects the emulator.
- **Foreground command:** the newest live descendant of the pane's process in the job, by image
  name without `.exe`.
- **Current path:** the last OSC 7 or OSC 9;9 the program reported, else the pane's start
  directory. Reading another process's cwd on Windows is unreliable, so this is a documented
  difference in COMPATIBILITY.md.
- **Console:** `SetConsoleMode` with `ENABLE_VIRTUAL_TERMINAL_INPUT` on stdin and
  `ENABLE_VIRTUAL_TERMINAL_PROCESSING` + `DISABLE_NEWLINE_AUTO_RETURN` on stdout. Resizes come
  from `WINDOW_BUFFER_SIZE_EVENT`.
- **Shell:** for `run-shell` and `if-shell`, decided in C0.8.

---

## 7. Security (E5)

The server runs every command it receives as its user, so **only that user may connect**.

**Unix:**
- The socket dir is created `0700` and must be owned by the user, as tmux checks: wrong owner or
  mode means refuse to start, with a clear error. The socket itself is `0600`.
- Every connection's peer is checked: `SO_PEERCRED` on Linux, `getpeereid` /
  `LOCAL_PEERCRED` on macOS. Another uid is closed at once, without a reply.

**Windows:**
- The socket or pipe is created with a DACL that allows only the current user's SID (and
  SYSTEM), and gets no inherited ACEs.
- Every connection's client process token SID is checked against the server's. A named pipe also
  sets `PIPE_REJECT_REMOTE_CLIENTS`.

**Everywhere:**
- No TCP, UDP or other network listener, ever.
- Frame size caps (§5), and a cap on pending output per client (a client that does not read is
  detached rather than growing memory without limit).
- `run-shell` and `if-shell` run only what the user's own config or own clients sent, which is
  the same trust model as tmux.
- OSC 52 from a pane reaches the user's terminal only when `set-clipboard` allows it. Pane
  programs cannot read the clipboard through the engine.
- Escape sequences from a pane are parsed by the emulator and never passed raw to the client's
  terminal. `allow-passthrough` is off and not implemented in v1.

---

## 8. Terminal emulation (vt)

The emulator is the most important piece for i-mux's users. It must match what tmux shows for
the same bytes.

- **Parser:** a DEC-style state machine (ground, escape, CSI, OSC, DCS, SOS/PM/APC), UTF-8 aware,
  that tolerates malformed input the way xterm does.
- **Cells:**
  - a grapheme cluster (string) and its width (0, 1 or 2);
  - foreground, background and underline colour: default, 16, 256 or RGB;
  - attributes: bold, dim, italic, underline styles (single, double, curly, dotted, dashed),
    blink, reverse, invisible, strikethrough, overline;
  - an OSC 8 hyperlink id.
- **Width:** a single width function shared by the emulator, the renderer and the formats
  (`#{p/N/…}` padding, status line widths). It matches tmux's results for the test fixtures
  (East Asian Wide, emoji presentation, ZWJ sequences, variation selectors). Differences are
  listed in COMPATIBILITY.md.
- **Screen:**
  - main and alternate screens; scroll region; origin mode; auto-wrap with the wrap flag kept
    per line, which `capture-pane -J` needs; tab stops; insert mode; saved cursor (DECSC/DECRC
    and 1048/1049);
  - cursor style (DECSCUSR), cursor visibility, the window title (OSC 0/2) and the cwd (OSC 7).
- **Scrollback:** a ring of lines up to `history-limit`. Lines leave the top only from the main
  screen, as in tmux.
- **Modes the renderer and input path read:**
  - application cursor keys and keypad;
  - bracketed paste (2004), focus events (1004);
  - mouse reporting (1000, 1002, 1003; encodings 1005, 1006);
  - synchronized output (2026);
  - extended keys (`CSI > 4 ; N m` and the `CSI u` forms tmux supports);
  - DEC 2031 (colour scheme reports).
- **Replies:**
  - DA1, DA2, DSR (cursor position and status);
  - DECRQM for the modes above;
  - OSC 10, 11 and 4 colour queries, answered from the client terminal's colours when known,
    else defaults.
- **Synchronized output from a pane (2026):** while a pane holds the mode, the renderer keeps
  showing that pane's last complete frame, with a timeout (as tmux does).

Spike C0.5 decides whether to build on an existing Go emulator package or write our own. In
either case the code lives in `internal/vt` and we own its behaviour.

---

## 9. Command language, options, formats

- **`cmdlang` parses tmux's syntax:**
  - commands split by `;` (an argument ending in `\;` is a literal `;`);
  - `{ … }` blocks as single arguments;
  - single quotes, double quotes, `\` escapes, `~` and `$VAR` expansion where tmux does them;
  - comments;
  - `%if "<format>"` / `%elif` / `%else` / `%endif`, evaluated with the format engine.

  Command-line argv from `imux-core cmd args ; cmd2` goes through the same splitting.
- **Commands** are a table: name, alias (`set` for `set-option`, `run` for `run-shell`, `if` for
  `if-shell`, `send` for `send-keys` …), a getopt-style flag spec, and the target kinds they take.
  Each command returns stdout, stderr and an exit code.
- **Options:**
  - a typed table (string, number, flag, choice, style, colour, array, key) per scope: server
    `-s`, session `-g`/`-t`, window `-w`, pane `-p`;
  - inheritance: pane → window → global window, and session → global session;
  - user options (`@name`) of any scope, holding strings;
  - `-a` appends, `-u` unsets, `-q` stays quiet, `-F` expands formats first, `-o` sets only if
    unset;
  - array options (`status-format[N]`, `terminal-features`, `terminal-overrides`).
- **Formats** (`format`):
  - `#{name}` variables with the scope chain (pane, window, session, client, server);
  - `#{@user}` options;
  - conditionals `#{?cond,a,b}`;
  - comparisons `==` `!=` `<` `>` `<=` `>=`, logic `||` `&&` `!`;
  - `m:` glob match, `e|op:` arithmetic (`+ - * /`, integer and `f` float);
  - loops `P:` (panes), `W:` (windows), `S:` (sessions);
  - padding `p/N/…`, `=N`/`=-N` truncation, `l:` literal, `T:`/`E:` expansion;
  - `##` for a literal `#`;
  - `#[…]` styles, which pass through to the status renderer;
  - short aliases `#S #I #W #P #D #T #H`.

  The exact list i-mux uses is in COMPATIBILITY.md §4. The engine has a nesting limit and a time
  limit per expansion.
- **Hooks:** named hook arrays, run in the context (client, session, window, pane) of the event
  that fired them, as tmux does for `after-*` and the event hooks.

---

## 10. Performance targets

Measured by benchmarks in Phase 6, each against tmux on the same machine:

| What | Target |
| --- | --- |
| Idle CPU, 10 sessions × 4 panes, nothing happening | ~0%: no wakeups beyond the status clock and timers |
| Latency added from a key to the pane | ≤ tmux + 2 ms (p99) |
| Throughput: `cat` of a 100 MB file into a pane | ≤ 1.5× tmux's time |
| One command round trip (`display-message -p '#{pane_id}'`) | ≤ tmux + 1 ms (p50) |
| Memory per pane with `history-limit 50000`, full | ≤ 2× tmux's |
| i-mux startup to first frame (`imux` in a project) | no slower than on tmux |

---

## 11. Testing strategy

1. **Unit tests** in every package.
2. **VT fixtures:** `testdata/vt/*.in` byte streams with `*.screen` expected results (cells,
   attributes, cursor, modes). They are hand-written or generated by scripts in the repository;
   none are recorded from a real agent.
3. **Differential tests** (`difftest`): a script of commands runs against real tmux (≥ 3.4) and
   against i-mux-core, each on its own temporary socket with the same config, terminal size and
   environment. The harness compares:
   - every command's stdout, stderr (normalized for ids where needed) and exit code;
   - `capture-pane -p -e` of chosen panes;
   - the full rendered frame of a headless attached client: the client's output is fed into a
     `vt.Screen` and compared cell by cell.

   tmux's version is pinned (3.8, built from its release tarball by `scripts/build-tmux.sh`),
   in CI and locally (`make tmux`).
4. **i-mux's e2e suite** (`../i-mux/e2e`, 105 tests) run with `IMUX_TMUX_BIN=imux-core`. This
   is the final measure of parity.
5. **Windows CI** (`windows-latest`) runs the unit tests, the VT fixtures and an engine e2e
   suite through ConPTY. tmux does not exist there, so the differential tests run on macOS and
   Linux only.
6. **Fuzzing:** `go test -fuzz` for the VT parser, `cmdlang`, `format` and the IPC decoder.
   Fuzz corpora are committed.

---

## 12. Decisions

| # | Decision | Why |
| --- | --- | --- |
| E1 | One binary that is client, attached client and server; the server is started by the client | Same model as tmux, which i-mux's code assumes (`new-session -d` then `attach-session`) |
| E2 | One goroutine owns the server state; panes parse output in their own goroutines | tmux's ordering semantics without locks on the tree; parsing does not block the loop |
| E3 | A versioned, length-prefixed protocol, JSON control and raw data, with a frame cap | Simple, debuggable, safe against oversized frames |
| E4 | Platform code only behind the PTY, ProcInfo, Shell, Console and Listener interfaces | Keeps "the same on three systems" true everywhere else |
| E5 | Only the server's own user may connect; never a network listener | The server runs commands as the user |
| E6 | i-mux-core speaks tmux's command language for the subset i-mux uses | i-mux runs on it with `IMUX_TMUX_BIN`, with no code changes, and stays able to fall back to tmux |
| E7 | Differences from tmux are bugs unless written in COMPATIBILITY.md | Parity is measurable; nothing drifts silently |
| E8 | Windows work starts after parity on macOS and Linux (Phase 8) | The hard part, the emulator and the command surface, is proven where tmux can check it |
| E9 | `imux-core -V` prints `imux-core X.Y.Z (tmux 3.8 compatible)`; `#{version}` is `3.8` | i-mux gates features on tmux's version; the engine declares the level it implements. 3.8, not 3.6: i-mux handles 3.7's and 3.8's changes, and its e2e suite passes on 3.8 but not on 3.6 (decided by the owner on 2026-11-15) |
