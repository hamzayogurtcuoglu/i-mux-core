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

**Server start (E1):** only the commands tmux marks as starting a server do so
(`new-session`, `attach-session`, `start-server`, `list-commands`, `list-keys`); the others
report that there is no server.
1. The client takes the lock file `<socket>.lock`, as tmux does, so that two clients starting at
   once start one server: the one that waits for the lock tries to connect again afterwards. A
   socket that refuses connections (left by a server that is gone) is removed under the lock.
2. It runs itself again as `imux-core __server -S <socket> [-f conf]`, fully detached: `setsid`,
   stdio on the null device, and on Windows `DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP`. The
   server gets the client's environment, as tmux's forked server does.
3. The server loads the `-f` config, then listens, then says so on a pipe the client passed it
   (or writes why it could not start). The client waits up to 5 s for that, then connects and
   sends its command.

**Server end:** the server exits when it has no session and no client is connected (tmux's
`exit-empty on`), and on `kill-server` or SIGTERM. Like tmux, it leaves its socket behind: the
next client finds it refusing connections and reports `no server running on <socket>`, or
removes it when it starts a new server. (Checked on tmux 3.8.)

**Attaching (E15):** as in tmux, the client reads its own terminal's terminfo entry and sends
the capabilities to the server as `name=value` strings (tmux's `MSG_IDENTIFY_TERMINFO`), with
`TERM`, the size and the environment. The server builds the terminal's description from them as
tmux's `tty_term_create` does: `terminal-features`, `COLORTERM`, `terminal-overrides`, and the
VT100-like features. Everything the terminal is sent, the start and stop sequences included, goes
to the client in `Output` frames; the client only switches its terminal to raw mode before the
first one and back after the last, and prints the reason it detached.

---

## 3. Packages

```
cmd/imux-core/            main: argv → client or server
cmd/contract/             development tool: extracts and checks the contract (C0.3)
cmd/tmuxrec/              development tool: a recording tmux shim (C0.3)
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
  status/                 status lines: status-format[N] drawn with format_draw, ranges, messages
  copymode/               the copy-mode subset
  buffers/                paste buffers, OSC 52 clipboard
  hooks/                  hook tables and firing
  job/                    run-shell and if-shell: the shell abstraction, -b, -d, -C
  platform/               paths, socket directory, process info (current command and path), shell, console
  difftest/               the differential harness (tests only)
  version/                the engine's version and the tmux version it implements (E9)
  contract/               development tool: what i-mux uses of tmux (C0.3); the engine never imports it
contract/                 recorded.json (what i-mux was seen using) and expected.json (COMPATIBILITY.md as data)
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

A pane may be **in a mode** (tmux's `wp->modes`; the engine has copy mode, task C4.8). The mode
is `model.Pane.Mode`, a small interface: the screen to show, its key table and its name.
While a pane has one, the renderer draws the mode's screen instead of the program's (the
program goes on writing to its own), the client's keys go to the mode's key table
(`copy-mode`, or `copy-mode-vi` with `mode-keys vi`) as tmux's `server_client_key_callback`
picks it, and mouse keys are not sent to the program. `internal/copymode` keeps a copy of the
pane's screen and history taken on entry (`window_copy_clone_screen`) and a view of the pane's
size drawn from it, touched only in the loop.

### 4.4 Rendering

Each attached client has a renderer. When anything it shows changes, the client is marked dirty.
A per-client ticker (at most every 16 ms, and right away after input) then:

1. **Composes** the frame from:
   - the status lines;
   - every visible pane's screen, at its offset;
   - the pane borders and their `pane-border-status` lines;
   - the overlays: popup, menu, message and prompt, on top.

   There is no overlay package. An overlay's state and program live with its client in
   `server` (a popup's PTY and screen, its keys, its timer), which hands `render` what to draw
   (`render.Popup`, the message lines). The drawing goes where tmux's does: `render` draws a
   popup's box and screen (`popup.c`'s draw, `screen_write_box`), `status` draws messages over
   the status line with `format_draw`, as `status_message_redraw` does. The command prompt
   follows the same split. A menu is the window's, as in tmux 3.8 (`w->menu`): `server` keeps it
   by window, every client showing the window draws it over the panes and borders, in window
   coordinates, and the keys of a client showing that window go to it (after a popup's).
2. **Diffs** it against the last frame sent, cell by cell. The parts tmux draws whole when they
   change are written whole when anything in them changed: the status lines
   (`screen_redraw_draw_status`), a popup (`popup_draw_cb`) and the menu. Pane cells stay diffed:
   tmux passes a program's own writes through there.
3. **Writes** the changes with the fewest escape sequences the terminal supports (`termout`).
4. **Wraps** the update in synchronized output (DEC mode 2026) when the terminal supports it, and
   ends it with the cursor where the active pane, or the overlay, wants it.

The frame is what the client's terminal is to show, not how tmux gets there: tmux draws scenes
of spans and updates panes incrementally, the engine diffs frames, and the resulting screen
must be the same. So must the text of the parts drawn whole: i-mux's e2e suite reads a
client's output stream after a mark and looks for a popup's, menu's or status line's text in
it, which a cell diff would cut into the changed cells only (C6.3). The parts that decide that screen are ported: the border cells and
which pane colours them (`screen-redraw.c`, with tmux's z order of panes kept in the model),
the fill cells (`window-border.c`), a pane's colours (`tty_attributes`), and the writing of a run
of cells for the terminal's capabilities (`tty_draw_line`, `tty_colours`, the line drawing set).

---

## 5. IPC protocol (E3)

- **Transport:**
  - macOS and Linux: Unix domain socket.
  - Windows (E13): a named pipe, `\\.\pipe\imux-core-<user SID>\<name>`. The last element is
    the socket name, so `#{socket_path}`'s base name is `-L`'s name, as i-mux checks on every
    system. `-S <path>` names the pipe itself.
- **Socket path:** `<socket dir>/<name>`, where `name` comes from `-L` (default `default`) and
  `-S <path>` gives a full path. The socket dir is:
  - Unix: `$TMUX_TMPDIR` or `/tmp`, then `imux-core-<uid>`;
  - Windows: none; the pipe's name carries the user's SID (above).
- **Framing:** `[type u8][length u32 big-endian][payload]`, with a 1 MiB cap per frame. A frame
  over the cap closes the connection.
- **Payloads:** control messages are JSON, and terminal data (input and output) is raw bytes.
- **Messages:**

  | Message | Direction | Content |
  | --- | --- | --- |
  | `Hello` | client → server | protocol version, client version, argv, cwd, the client's whole environment, as tmux's client sends it (a session takes the `update-environment` variables from it, and clears those the client lacks; C3.13), `-u`, terminal size, whether stdin/stdout are terminals, client pid; for a terminal, its path (`#{client_name}`), its terminfo capabilities (E15) and its erase character (termios `VERASE`, which tmux reads as `BSpace`) |
  | `Command` | client → server | the command list (already split into argv lists, or the raw string for `source-file`-style input) |
  | `Result` | server → client | stdout, stderr, exit code |
  | `Attach` | server → client | the client switches to attached mode |
  | `Output` | server → client | terminal bytes to write |
  | `Input` | client → server | terminal bytes read |
  | `Resize` | client → server | new terminal size |
  | `Detach`, `Exit` | server → client | reason, exit code; the client restores its terminal and exits |
  | `Suspend` | server → client | Unix only: the client stops itself (SIGTSTP) |
  | `VersionError` | server → client | the server's protocol version, before it closes the connection |

- **Version check:** a protocol version mismatch is refused with an error that i-mux classifies
  as "no server", so a newer `imux-core` never talks to an old server. The server answers a
  `Hello` with another version by a `VersionError` frame holding its own version, and closes the
  connection; the client reports `error connecting to <socket path> (protocol version N, server
  has M)`, which i-mux reads as no server.
- **Back-pressure:** each attached client's output goes through a queue with a byte cap (4 MiB).
  A client that stays over the cap for more than a second is detached with the reason
  `too slow`, so a client that stops reading cannot make the server grow without limit (§7).

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
    Kill() error                // the whole process tree (process group / job object), K4
    Pid() int
    Fd() int                    // the PTY master, for ProcInfo
    Close() error
}

// platform
type ProcInfo interface {           // given the pane's PTY master (Proc.Fd)
    ForegroundCommand(fd int) string // #{pane_current_command}
    CurrentPath(fd int) string       // #{pane_current_path}
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

- **PTY (E11):** opened with `golang.org/x/sys/unix` directly: `/dev/ptmx`, then
  `TIOCSPTLCK`/`TIOCGPTN` on Linux and `TIOCPTYGRANT`/`TIOCPTYUNLK`/`TIOCPTYGNAME` on macOS. The
  child runs `setsid`, takes the PTY as its controlling terminal and so leads its own process
  group.
- **Foreground command and path (E11),** as tmux's osdep files do, without cgo:
  - Linux: `tcgetpgrp`, then the first word of `/proc/<pgrp>/cmdline` and the link
    `/proc/<pgrp>/cwd` (else the session leader's, found with `TIOCGSID`);
  - macOS: `tcgetpgrp`, then `proc_pidinfo` with `PROC_PIDT_SHORTBSDINFO` (`pbsi_comm`) and
    `PROC_PIDVNODEPATHINFO` (`pvi_cdir`). `proc_pidinfo` is called in libSystem through an
    assembly trampoline and `syscall.syscall6`, the way `golang.org/x/sys/unix` calls libSystem.
  - The name then goes through tmux's `parse_window_name` (C3.7).
- **Shell:** `default-shell` (or `$SHELL`, else `/bin/sh`). `run-shell` uses `/bin/sh -c`, as tmux
  does.

### Windows (Phase 8)

- **PTY (E12):** ConPTY (`CreatePseudoConsole`) with no flags, and the child in a **job object**
  so `Kill` ends its whole tree. The spike's evidence (C0.7) on build 26100
  (`spikes/conpty/EVIDENCE-26100.md`):
  - every flag is accepted, and `PSEUDOCONSOLE_PASSTHROUGH_MODE` changes nothing on build
    26100, so none is used;
  - passed through to the emulator: 2026, 2004, 1004, OSC 0, 7, 9;9 and 52, CSI u,
    modifyOtherKeys, RGB and underline styles (with `ESC [ 0 m` written `ESC [ m`), wide
    characters; keys, pastes and SGR mouse input reach the program unchanged;
  - consumed by ConPTY: a program's mouse mode requests (1000, 1002, 1003, 1006), so the pane's
    screen never learns them (COMPATIBILITY.md K8), and the DA1 query, which ConPTY answers;
  - changed by ConPTY: OSC 8 gets an `id=` (K9); `ESC [ ? 1049 l` comes out as a repaint; and
    each console starts with a title (OSC 0, the program's path) and `ESC [ ? 25 h`, which the
    emulator takes as any program's own;
  - processes: `cmd.exe`, `powershell.exe` and `pwsh.exe` run; exit codes and resizes arrive;
    `TerminateJobObject` ends grandchildren too.
  - on build 20348 (`spikes/conpty/EVIDENCE-20348.md`) ConPTY also consumes the alternate
    screen request (1049) and drops underline styles (K10), adds no OSC 8 `id=`, and passes
    focus events in;
  - **Minimum Windows:** Windows 11 24H2 (build 26100), the oldest build where the evidence
    shows what a pane's program writes reaching the emulator as on macOS and Linux (but for K8
    and K9). Older builds from Windows 10 1809 (17763, where `CreatePseudoConsole` appears) run,
    with K10's losses. CI gathers the evidence on 26100 (`windows-latest`) and 20348
    (`windows-2022`) at every push.
- **Foreground command:** the newest live descendant of the pane's process in the job, by image
  name without `.exe`.
- **Current path:** the last OSC 7 or OSC 9;9 the program reported, else the pane's start
  directory. Reading another process's cwd on Windows is unreliable, so this is a documented
  difference in COMPATIBILITY.md.
- **Console:** `SetConsoleMode` with `ENABLE_VIRTUAL_TERMINAL_INPUT` on stdin and
  `ENABLE_VIRTUAL_TERMINAL_PROCESSING` + `DISABLE_NEWLINE_AUTO_RETURN` on stdout. Resizes come
  from `WINDOW_BUFFER_SIZE_EVENT`.
- **Shell (E14):**
  - a shell command given to `run-shell` or `if-shell` runs as `%COMSPEC% /d /s /c "<command>"`
    (`cmd.exe`), the Windows counterpart of tmux's `/bin/sh -c`: always there, quick to start,
    and with one quoting rule;
  - a pane's program given as one string runs in `default-shell`, which defaults to `pwsh.exe`
    when it is installed, else `powershell.exe` (C8.7); given as several arguments, it runs
    without a shell, as tmux does on every system;
  - i-mux's own calls use the **argv form** of `run-shell` and `if-shell` (COMPATIBILITY.md X1),
    which needs no shell and no quoting at all.

---

## 7. Security (E5)

The server runs every command it receives as its user, so **only that user may connect**.

**Unix:**
- The socket dir (`$TMUX_TMPDIR` or `/tmp`, resolved, then `imux-core-<uid>`) is created
  `0700`. It must be a directory owned by the user with no group or other access; otherwise the
  client refuses to use it, with tmux's messages (`couldn't create directory …`, `… is not a
  directory`, `directory … has unsafe permissions`). This is stricter than tmux, which refuses
  only other users' access (o+rwx) and allows the group's. The socket itself is `0600`.
- Every connection's peer is checked: `SO_PEERCRED` on Linux, `LOCAL_PEERCRED` on macOS.
  Another uid, root included, is closed at once, without a reply. tmux's default server access
  also lets root in; the engine has no `server-access` (a non-goal), so only the owner gets in.

**Windows (E13):**
- The pipe is created with a protected DACL that allows only the current user's SID and SYSTEM
  (no inherited ACEs) and with `PIPE_REJECT_REMOTE_CLIENTS`.
- Every connection's client process (`GetNamedPipeClientProcessId`) has its token SID checked
  against the server's; another is closed at once.
- A client whose server dies reads "The pipe has been ended", which it reports as a lost
  server.

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
  (`#{p/N/…}` padding, status line widths). It is tmux's with utf8proc: tmux's own default
  width table (regional indicators 1, skin tone modifiers and some emoji 2), then
  `utf8proc_charwidth`, with private use characters 1. Its table is generated
  (`scripts/gen-widths.sh`) from **utf8proc 2.12.0**, the same pinned version
  `scripts/build-tmux.sh` builds and links into the reference tmux on every system, so widths
  agree everywhere. Combining (ZWJ sequences, variation selectors, skin tones, regional
  indicator pairs, Hangul jamo) follows tmux's `screen_write_combine` and `utf8-combined.c`.
  Differences are listed in COMPATIBILITY.md.
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

**Decision E10 (spike C0.5): we write our own,** porting tmux 3.8's `input.c` (the parser and
the sequences), `screen-write.c` and `grid.c` (the screen, wrapping, scrollback and reflow), so
that the same bytes give the same screen as under tmux, malformed input included. Ported files
keep tmux's ISC copyright notice. The evidence, measured on 2026-11-15 on an Apple M-series Mac
with a deterministic 100 MB mixed stream (plain and coloured lines, cursor movement and erasing,
synchronized alternate-screen frames, wide characters and emoji, titles, OSC 8 links, scroll
regions, tabs) fed in 32 KiB writes to a 200×50 screen:

| | License, upkeep | Line wrap flag (`capture-pane -J`) | Reflow on resize | Scrollback | Underline styles, OSC 8 | 2026 held frames | Throughput | Allocated |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `github.com/charmbracelet/x/vt` v0.1.0 | MIT, maintained; pulls in 12 modules | no | no | yes, trailing blanks trimmed | yes, yes | mode tracked only | 4.3 MB/s | 7.2 GB |
| `github.com/hinshun/vt10x` (2022-03) | MIT, unmaintained since 2022 | no | no | no | no, no | no | 15.6 MB/s | 2.0 GB |
| Our port of tmux's `input.c` state machine (parser only, `internal/vt`) | ISC (tmux), ours | — | — | — | — | — | 176.5 MB/s | 0.1 MB per 8 MB |
| tmux 3.8 itself, end to end through a pty (reference) | | yes | yes | yes | yes, yes | yes | 10.4 MB/s | |

Neither package keeps the per-line wrap flag or reflows the way tmux does, and both would need
deep changes to match tmux's handling of malformed input, its replies and its quirks; the
faster one is unmaintained. A Go port of tmux's own state machine parses an order of magnitude
faster than either, which leaves the screen code room to stay within the ≤ 1.5× tmux target
(§10). The parser is the starting point of `internal/vt` (task C1.2).

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

Measured by `scripts/bench.sh` (`cmd/bench`, C6.4), each against tmux 3.8 on the same machine,
and recorded in `BENCHMARKS.md`. Most measures run one engine after the other; latency and
startup, whose outliers on a shared CI runner come from the machine rather than the engine,
take their samples in turns (one under tmux, then one under i-mux-core), so that a noisy
moment falls on both:

| What | How it is measured | Target |
| --- | --- | --- |
| Idle CPU | 10 sessions × 4 panes of `cat` and one attached client, nothing happening, for 10 s; the server's CPU time | ≤ 0.5% of a core: no wakeups beyond the status clock and timers |
| Latency from a key to the screen | A key typed into an attached client's terminal, echoed by the pane's program (`cat` in raw mode), until the echo reaches that terminal, at 50 ms between keys, 300 keys, in turns with tmux (p99) | ≤ tmux + 2 ms |
| Throughput | `cat` of a 100 MB file of 80-column lines, a third coloured, into a 200×50 pane an attached client shows, until `cat` ends | ≤ 1.5× tmux's time |
| One command round trip | `display-message -p '#{pane_id}'` from a new client process, 200 times (p50) | ≤ tmux + 1 ms |
| Memory per pane | The server's resident memory growth for each of 4 panes filled past `history-limit 50000` with that text | ≤ 2× tmux's |
| i-mux startup to its first frame | `imux claude` in a project in a 160×40 terminal, with i-mux's fake agents, until the sidebar shows, in turns with tmux (median of 5) | no slower than on tmux (within 10%, the measurement's noise) |

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

   Before the engine has the commands to build layouts, **render cases** (`testdata/render`) do
   the same for the renderer alone: `cmd/renderfixture` records the client screen tmux draws,
   and `internal/render`'s tests draw the same panes and compare what can be seen.

   tmux's version is pinned (3.8, built from its release tarball by `scripts/build-tmux.sh`),
   in CI and locally (`make tmux`).
4. **The engine's own end-to-end scenarios** (`e2e/`, `make e2e`, part of `make ci`): imux-core
   alone, each scenario with its own server, home and socket, driving attached clients in PTYs
   (difftest's headless client) and the fake TUI, and checking screens, panes and formats.
   They need no tmux, so they can run on all three systems.
5. **i-mux's e2e suite** (`../i-mux/e2e`, 105 tests) run with `IMUX_TMUX_BIN=imux-core`. This
   is the final measure of parity.
6. **Windows CI** (`windows-latest`) runs the unit tests, the VT fixtures and an engine e2e
   suite through ConPTY. tmux does not exist there, so the differential tests run on macOS and
   Linux only.
7. **Fuzzing:** `go test -fuzz` for the VT parser, `cmdlang`, `format` and the IPC decoder.
   Fuzz corpora are committed.
8. **Soak** (`make soak`, nightly for an hour on Linux and macOS): the server in the test's
   process, built with `-race`, with 4 sessions × 4 panes of the fake TUI streaming and
   redrawing, 2 clients attaching and detaching and a command every 100 ms. Goroutines, open
   files and heap are sampled; they must be as flat at the end as at the start.

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
| E9 | `imux-core -V` prints `tmux 3.8 (imux-core X.Y.Z)`; `#{version}` is `3.8` | i-mux gates features on tmux's version; the engine declares the level it implements. 3.8, not 3.6: i-mux handles 3.7's and 3.8's changes, and its e2e suite passes on 3.8 but not on 3.6 (decided by the owner on 2026-11-15). The line starts as tmux's does so that an unmodified i-mux reads it as 3.8 (its parser takes `tmux `, then `major.minor`, and keeps the rest); the part in brackets names the engine. The first form, `imux-core X.Y.Z (tmux 3.8 compatible)`, made i-mux stop before doing anything in 97 of its 108 e2e tests (C6.2, 2026-11-17) |
| E10 | `internal/vt` is our own port of tmux 3.8's `input.c`, `screen-write.c` and `grid.c`, not an existing Go emulator package | The candidates lack the line wrap flag and tmux's reflow, and parse 11 to 40 times slower than the port (§8) |
| E11 | Unix PTYs and process information through `golang.org/x/sys/unix` and, on macOS, libSystem's `proc_pidinfo` without cgo; no `github.com/creack/pty` | x/sys is needed anyway (`tcgetpgrp`, `ioctl`s, signals); opening a PTY is a few `ioctl`s per system; no cgo keeps cross-compiled release builds simple. Shown by C0.6's test on macOS and Linux |
| E12 | On Windows, panes run in ConPTY with no flags, each in a job object; the minimum supported is Windows 11 24H2 (build 26100), and builds from 17763 run with documented losses | The C0.7 evidence (`spikes/conpty/EVIDENCE-26100.md` and `-20348.md`): what passes, what ConPTY consumes or changes, and that the job object ends a whole tree (§6). Passthrough mode changes nothing. Mouse modes a pane's program asks for do not reach the emulator on either build (K8); on 20348 neither do the alternate screen and underline styles (K10) |
| E13 | On Windows, the server listens on a named pipe with a protected DACL (the user and SYSTEM), `PIPE_REJECT_REMOTE_CLIENTS`, and a token SID check of every client | The C0.7 evidence: the DACL, the remote client flag and the client's SID all work on a pipe. AF_UNIX gives the peer SID too, but its socket file's protection could not be shown (the spike could not read its DACL) and it has no remote client flag |
| E14 | On Windows, `run-shell` and `if-shell` shell commands run in `cmd.exe /d /s /c`; i-mux's own calls use an engine extension, the argv form (`["prog", "arg", …]`, formats expanded in each element, no shell), and pane programs go as separate arguments | i-mux builds POSIX lines (`'<imux>' … >/dev/null 2>&1 \|\| true`, `rm -f`) that no Windows shell runs; quoting them for PowerShell or `cmd.exe` would bring its own bugs, while an argv needs none. macOS and Linux stay unchanged (C9.3) |
| E15 | The client reads terminfo with our own reader of compiled terminfo files (the legacy and the 32-bit formats, with extended capabilities), from the places ncurses looks; parameters are expanded by our own `tparm`. A built-in `xterm-256color` description, generated from ncurses' entry, is used when there is no terminfo (Windows) | No cgo and no ncurses dependency; tmux itself only needs the capabilities as strings, which is what crosses the socket. The reader and `tparm` are checked against tmux's own view of the terminal (`show-messages -T`) |
