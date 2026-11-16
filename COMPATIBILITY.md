# Compatibility contract

This is the tmux behaviour i-mux uses and i-mux-core must reproduce. A difference from tmux on
anything listed here is a bug, unless §13 records it as a known difference.

**Source.** Recorded on 2026-11-15 (task C0.3) from i-mux 0.62.0, on tmux 3.8:
1. `scripts/contract.sh` ran i-mux's whole e2e suite (all tests passed) with a recording `tmux`
   first on `PATH` (`cmd/tmuxrec`): 8637 tmux invocations and 96 generated `tmux.conf` files,
   every `%if` branch included.
2. It scanned i-mux's Go source for the commands it builds (`tmux.Cmd{…}` literals, the flags
   appended to them, and command strings), which found `delete-buffer -b`, the one command the
   e2e suite does not run.
3. `cmd/contract` turned both into `contract/recorded.json`. `contract/expected.json` is the
   machine-readable form of this file, and a test checks that this file names everything in it.

**Keeping it complete.** `make contract-record` records again (slow: it runs i-mux's e2e suite);
`make contract` lists anything recorded that `contract/expected.json` lacks, and fails if there
is any. When i-mux starts using something new, add it here and to `contract/expected.json`.

**Marks.** In the tables, **†** marks what only i-mux's e2e harness uses (the tests' own tmux
calls, needed to run i-mux's e2e suite on i-mux-core in Phase 6), and **‡** what was found only
in i-mux's source.

**Reference version.** The reference is **tmux 3.8**, the newest behaviour i-mux handles (i-mux
has code for 3.7's and 3.8's changes, such as a double click's second press and display-message's
strftime) and the version i-mux's e2e suite passes on: on tmux 3.6, `TestPaneKeys` and
`TestSidebarDoubleClick` fail. i-mux also supports tmux 3.2 to 3.7, but i-mux-core only needs to
behave like 3.8 and report itself as 3.8 (ARCHITECTURE.md E9).

---

## 1. Command line

| Form | Meaning |
| --- | --- |
| `-L <name>` | Socket name in the socket directory (i-mux uses `imux`, or `$IMUX_TMUX_SOCKET`) |
| `-S <path>` | Full socket path. i-mux does not use it; the differential tests do |
| `-f <file>` | Config file loaded when the server starts |
| `-u` | UTF-8 client; i-mux always passes it. Output keeps tabs and non-ASCII as they are |
| `-V` | Print the version (E9). i-mux reads it to choose its code paths |
| `cmd args ; cmd args` | Several commands in one invocation, as separate argv words. A word `;` ends a command; a word ending in `;` ends the command after it, without the `;`; a word ending in `\;` is a literal `;` |
| exit code | 0 on success, 1 when any command failed. Errors go to stderr |

### Error messages i-mux classifies

These substrings must appear, in lower case, in stderr for these cases:

| Case | Must contain |
| --- | --- |
| No server | `no server running on <socket path>` (also matched: `error connecting to`, `server exited`, `lost server`) ‡ |
| No session | `can't find session: <name>` |
| No pane or window | `can't find pane: <id>` / `can't find window: <id>` |
| No client | `can't find client: <name>` / `no current client` |

‡ tmux 3.8 prints `no server running on <socket path>` when the socket file exists but nothing
listens (a server that exited leaves its socket), and `error connecting to <socket path> (No such
file or directory)` when there is no socket file. Both exit 1, and i-mux-core prints the same
in the same cases (`TestNoServer` in internal/difftest).

`display-message` is the exception: with `-t` naming a pane or window that does not exist, or
`-c` naming no client, it does not fail; it expands its format with no pane (`#{pane_id}` is
empty) and exits 0. A session that does not exist (`-t =name`) is still an error. (Checked on
tmux 3.8 by `testdata/diff/sample.txt`.)

## 2. Targets

| Form | Where i-mux uses it |
| --- | --- |
| `%N` | Pane id: almost every `-t` and `-s` |
| `@N` | Window id: `set -w -t`, `select-window`, `kill-window`, `select-layout` |
| `=name` | Session with exactly this name (no prefix match): `has-session`, `attach-session`, `switch-client`, `kill-session` |
| `=name:index` | A window of a session by index, exactly: `new-window -t` |
| `=` | In a mouse binding: the pane, window or status position under the mouse (`select-pane -t=`, `if -F -t=`, `switch-client -t =`, `run-shell -t =`) |
| `/dev/ttysN` (a tty path) | A client by name (`client_name`): `-c` of display-message, display-popup, display-menu and switch-client; `-t` of command-prompt and detach-client |
| (none) | The current client's session, window and pane; when there is no client, the most recently used session |
| `name` † | Session by name, prefix match allowed (`switch-client -t proj`) |
| `:index` † | A window of the current session by index (`list-panes -t :1`) |
| `{last}` † | The last pane (`display-message -p -t '{last}'`) |

## 3. Commands and flags

Every flag listed must work as in tmux 3.8. Aliases i-mux uses are in brackets. No other
command or flag is used.

| Command | Flags i-mux uses | Notes |
| --- | --- | --- |
| `attach-session` | `-t` | `-t =name` |
| `bind-key` [`bind`] | `-n -T` | Key, then a command string or a `{ … }` block |
| `capture-pane` | `-p -J -S -t`, `-e` † | `-J` joins wrapped lines; `-S -N` starts N lines into the history |
| `command-prompt` | `-I -p -t` | `-I` initial text (may be empty or a format), `-p` prompt; the template's `%%` is replaced |
| `copy-mode` | `-H` | Enters copy mode without the position indicator |
| `delete-buffer` ‡ | `-b` | |
| `detach-client` | `-t`, `-a` † | `-t` takes a tty path |
| `display-menu` | `-M -O -T -c -t -x -y` + items | Items: `name key command`; `""` is a separator; a name starting with `-` is disabled. `-x -y` may be formats (`#{e|+:#{popup_pane_left},N}`) |
| `display-message` | `-p -c -d -t` + message | `-p` prints; `-d 0` stays until a key, `-d 1` replaces a message at once, `-d N` milliseconds. The message goes through strftime |
| `display-popup` | `-E -c -d -h -t -w -x -y` + shell command | `-E` closes on exit; `-w -h` cells or `N%`; `-x -y` positions (`C`, or numbers); `-d` start directory |
| `has-session` | `-t` | Exit code only |
| `if-shell` [`if`] | `-F -t` + condition + then [+ else] | `-F` evaluates a format; then and else are strings or `{ … }` blocks; `-t =` in mouse bindings |
| `kill-pane` | `-t` | |
| `kill-server` | | |
| `kill-session` | `-t` | |
| `kill-window` | `-t` | |
| `list-buffers` † | `-F` | |
| `list-clients` | `-F` | |
| `list-keys` † | `-T` | |
| `list-panes` | `-a -s -t -F` | |
| `list-sessions` | `-F` | |
| `list-windows` † | `-F -t` | |
| `load-buffer` | `-b` + path | |
| `new-session` | `-d -s -c -P -F`, `-x -y` † + shell command | `-P -F '#{pane_id}'` prints the new pane. Without `-d`, attaches |
| `new-window` | `-a -c -t -P -F` + shell command | |
| `paste-buffer` | `-b -d -p -t` | `-p` bracketed paste when the pane asked for it |
| `refresh-client` † | `-S` | Redraws the status line |
| `rename-window` | `-t` + name | |
| `resize-pane` | `-t -x -L -R -U -D`, `-Z` † | In tmux 3.8, `-L -R -U -D` take their count as an optional value (`-R 2`) |
| `respawn-pane` | `-k -c -t` + shell command | |
| `run-shell` [`run`] | `-b -C -d -t` + command | `-C` runs a tmux command; `-d` delays, in seconds with fractions (`-d0.3`, `-d 0.5`), with or without a command |
| `select-layout` | `-t` + layout | Restores a saved `#{window_layout}`, of either version (see below) |
| `send-prefix` | | tmux's default `prefix C-b` binding runs it; i-mux itself never calls it |
| `select-pane` | `-t` | |
| `select-window` | `-t` | |
| `send-keys` [`send`] | `-t -M -X`, `-l` † + keys | `-M` forwards the mouse event; `-X` runs a copy-mode command |
| `set-buffer` | `-b -w` + data | `-w` also sets the terminal clipboard (OSC 52) |
| `set-hook` | `-g` + hook + command | |
| `set-option` [`set`] | `-g -s -w -p -t -u -q -a -F` | Includes `set -as` (append, server), `set -gF` and arrays `name[N]` |
| `show-buffer` † | | |
| `show-options` | `-g -p -q -t -v` and combinations (`-gqv`, `-pqv`) | |
| `source-file` | `-q` + path | `-q` ignores a missing file |
| `split-window` | `-h -v -b -f -d -l -c -t -P -F` + shell command | `-l N` cells; `-f` full width or height; `-b` before |
| `swap-pane` | `-d -s -t` | |
| `switch-client` | `-c -t -T` | `-T table` switches key table; `-t =` the clicked session; `-t =name` |
| `unbind-key` | `-q -T` | |

### Checked against tmux 3.8 (C3.6)

`testdata/diff/lists.txt`, `windows.txt` and `panes.txt` run the session, window and pane
commands with every flag above and their errors, and `TestLayoutScenarios` replays C2.5's
layout scenarios through the commands. What they showed:

- The lists keep tmux's orders: sessions by name, windows by index, panes in window order
  (`-O` and `-r` sort as tmux's `sort.c`); `list-windows` and `list-panes` give `#{line}` the
  number of lines, `list-sessions` and `list-clients` the line's own number.
- `-c` is a format, relative to the client's directory; `-n` and `-s` are formats too, and a
  name has its control characters encoded (`rename-window` included).
- `new-window -a` and `-b` move the windows in the way up by one; `kill-window -a` renumbers
  the session when `renumber-windows` is on.
- `capture-pane -p` always ends with one newline; `-S` and `-E` are formats, and a bad number
  is the screen's edge; `-a` with no alternate screen is an error unless `-q`.
- `resize-pane -x` and `-y` take `N%` up to 1000%; `-L -R -U -D` take their count as their
  value, the argument, or 1.
- `respawn-pane` without `-k` refuses a live pane (`pane s:0.1 still active`), and keeps the
  pane's command and directory unless given new ones.
- A window takes `pane-border-status` from its first pane, and every window follows a change.

Not yet: the command client's own variables in lists (`#{client_name}` of an unattached
client is `client-PID` in tmux, which nothing compares); `switch-client -T` (key tables,
C3.8); `capture-pane` without `-p` (paste buffers, C4.9); `select-layout`'s named layouts and
`-E -n -o -p`; `resize-pane -M -T`; `resize-window -a -A`; `respawn-pane -E`;
`new-window -E -k -S`. These are not in i-mux's contract and are refused.

### Names and titles (C3.7)

Checked against tmux 3.8 in `testdata/diff/names.txt`:

- A new window is named from its command as tmux's `default_window_name` does (`exec ` and
  leading dashes dropped, the first word, a path's base name, control characters encoded), and
  with `-n` keeps that name (`automatic-rename` off).
- With `automatic-rename` on, the name is `automatic-rename-format` expanded for the active
  pane, worked out when that pane writes, becomes active or `automatic-rename` is set, and at
  most every half second. Setting an option of the pane (`@imux_agent`) is not such a change;
  i-mux sets `automatic-rename` again after it. Right after a window is made, the name races
  with its program's start in both engines (`sh` before `exec`), so tests wait for it.
- `#{pane_current_command}` and `#{pane_current_path}` come from the PTY's foreground process
  (E11) each time a format asks.
- OSC 0 and 2 set the pane's title unless `allow-set-title` is off; `select-pane -T` sets it
  from a format. `ESC k … ST` renames the window only with `allow-rename` on, and turns
  `automatic-rename` off (an empty name gives it back).

### Key tables and bindings (C3.8)

Checked against tmux 3.8 in `testdata/diff/keys.txt`:

- **Tables.** A binding lives in a named table (`prefix`, `root`, a custom one such as
  i-mux's `imux-resize`), keyed by the key code without its flags. tmux 3.8's whole
  default set — every table, `copy-mode`, `copy-mode-vi` and `move` included, mouse keys
  and all — is loaded verbatim from its `key-bindings.c` when the server starts, so
  `list-keys` shows what tmux shows. The commands those bindings run are only parsed:
  running one that is not implemented yet fails (and shows on the message line with
  C4.5), which is why `bind-key` also takes `-N` (note) and `-r` (repeat) — tmux's own
  defaults use them.
- **The prefix.** `prefix` and `prefix2` (session options, defaults `C-b` and none) switch
  a client's table to `prefix`; a second press of the prefix key is looked up there (the
  default `C-b` binding runs `send-prefix`, which sends the prefix key to the pane).
  `prefix-timeout` (server, default 0: off) drops a client back to its default table after
  that long in `prefix`.
- **Dispatch.** A key is looked up in the client's table, then as `Any`, then (unless the
  table already is the default) in the default table (the session's `key-table` option,
  default `root`) — so a binding for one key does not hide the default table's. A key
  bound nowhere that reached the default table goes to the pane; one that fell out of
  another table (`prefix`, a `switch-client -T` table) is swallowed, not sent. `repeat-time`
  (session, default 500) and `initial-repeat-time` (default 0) keep a repeat (`-r`)
  binding live for that many milliseconds: presses within it run the binding again instead
  of the pane.
- **`switch-client -T`** puts the client (the `-c` one, else the current) in a table for
  one key: a bound key runs and, unless the binding repeats or re-enters, the client is
  back in its default table; an unbound key is looked up in the default table as above.
  A table that does not exist is `table <name> doesn't exist`; with no current client,
  `no current client`.
- **`unbind-key`** removes a binding (`-n`: `root`, `-T`: that table, else `prefix`);
  a table that has no bindings left and never had tmux defaults goes away entirely.
  With `-q` an error has no message: it stops the rest of the command's group, but the
  client exits 0 (tmux sets the exit status only with an error message, for any command). `bind-key key` with no command only
  sets the note and repeat flag of an existing binding.
- **`list-keys`** prints with tmux's own template: `bind-key ` (`-r` or two spaces when
  any listed binding repeats) `-T <table padded> <key escaped and padded> <command>`;
  tables in name order, bindings by key code; `list-keys -T <table>` lists one table,
  `list-keys <key>` filters. The harness's `list-keys -T` (†) is what i-mux's e2e suite
  uses.
- **A binding's commands** run for the client that pressed the key: its formats
  (`#{client_name}`) and `switch-client` without `-c` are that client's.

### run-shell and if-shell (C3.10)

Checked against tmux 3.8 in `testdata/diff/shell.txt`:

- A command waits for its job (the rest of the list, and the client, wait) unless `-b`.
  `-d` waits that many seconds (fractions too) before running; with no command it only
  waits. What `run-shell -C` and `if-shell` pick runs right after them, before the next
  command, with the same current target.
- Output: each line of the job's stdout (and stderr with `-E`; else stderr is dropped), then
  `'<cmd>' returned N` or `'<cmd>' terminated by signal N`. A command-line client gets them
  on its stdout and exits with the job's status (128 + N for a signal). With `-b`, `-t`, or a
  client that is attached (a key binding), they go to a pane's view mode in tmux, which comes
  with copy mode (C4.8); until then they are dropped.
- The command is a format first; `run-shell`'s further arguments are `#{1}`, `#{2}`, …;
  `-c` is a format. Jobs run in `/bin/sh -c` with the server's environment, `TERM` (the
  `default-terminal`), `TERM_PROGRAM`, `TERM_PROGRAM_VERSION`, `COLORTERM` (none of these
  four while the config loads), `TMUX` (with the target's session) and `PWD`; no
  `TMUX_PANE`.
- `if-shell -F` is true unless the expanded format is empty or starts with `0`; a branch
  missing is nothing to run; a branch that does not parse is the command's error.
- The argv form (X1) is the engine's and is checked by `internal/server`'s tests.

### Layout strings

tmux 3.8's `#{window_layout}` is JSON, version 2:
`{"V":2,"L":{"t":"h","w":80,"h":24,"x":0,"y":0,"c":[…]}}`, where `t` is `h` (side by side),
`v` (stacked) or `p` (a pane), and a pane has `"a":true` (active) or `"l":N` (its place in the
last-panes list), `"i"` (its index) and `"I":"%id"`. Older tmux wrote version 1,
`csum,WxH,X,Y{…}` (`[ ]` for stacked cells, the pane id after a pane's offsets), and tmux 3.8
still writes it for control clients. A layout i-mux saved with an older tmux is version 1, so
`select-layout` takes both, as tmux 3.8 does. With version 2 it also restores the active pane and
the last-panes list, and assigns panes by `"i"`; with version 1 it assigns them in tree order.
A window with a size from `resize-window` goes back to that size after `select-layout`.
(Checked on tmux 3.8 by the scenarios in `testdata/layout`, see TASKS.md C2.5.)

### Copy-mode commands (via `send -X`)

`select-word`, `select-line`, `copy-pipe-and-cancel` (with no command: it copies to the buffer
and the clipboard). i-mux's double- and triple-click bindings run them after `copy-mode -H`.
Mouse selection and scrolling otherwise use tmux's default `copy-mode` and `copy-mode-vi`
bindings (§7); task C4.8 lists the commands those need.

## 4. Formats

### Variables

| Group | Variables |
| --- | --- |
| Pane | `pane_id pane_active pane_width pane_height pane_left pane_top pane_right pane_bottom pane_title pane_current_command pane_current_path pane_dead pane_dead_status pane_dead_signal pane_in_mode mouse_any_flag`, `pane_bg` † |
| Window | `window_id window_index window_active window_width window_layout window_zoomed_flag` |
| Session | `session_name session_path session_windows session_attached session_activity` |
| Client | `client_name client_tty client_width client_height client_session client_flags client_termname client_activity client_control_mode`, `client_key_table` † |
| Mouse | `mouse_pane mouse_status_range` |
| Popup | `popup_pane_left popup_pane_top` (menu placement) |
| Server | `pid start_time`, `socket_path` † |
| Buffer | `buffer_name` † |
| Options as variables † | `status status-position status-format[0] status-format[1] pane-border-format remain-on-exit` (read with `#{T:…}` and `#{E:…}`) |
| Aliases | `#I #W`; `##` is a literal `#` |
| User options | Any `#{@name}`, looked up pane → window → session → global; also bare as a condition (`#{?@imux_agent,…}`) |

i-mux's user options are `@imux_agent @imux_appearance @imux_auto_cwd @imux_browser @imux_c_*
@imux_collapsed @imux_commit_ask @imux_compare @imux_cwd @imux_done_at @imux_fallback
@imux_file @imux_file_clip @imux_file_name @imux_file_root @imux_flag @imux_fold @imux_git
@imux_hook @imux_hook_at @imux_keys @imux_label @imux_last @imux_lim_antigravity
@imux_lim_claude @imux_lim_codex @imux_lim_copilot @imux_lim_opencode @imux_loaded @imux_lock
@imux_moved @imux_name @imux_name_client @imux_name_key @imux_name_pane @imux_name_value
@imux_press @imux_remote @imux_rename @imux_rename_pane @imux_reveal @imux_role @imux_scm_arg
@imux_scm_cwd @imux_scm_sort @imux_scm_view @imux_seen @imux_sidebar_off @imux_sidebar_pid
@imux_skill @imux_skill_pane @imux_skill_root @imux_skill_text @imux_start @imux_state
@imux_state_at @imux_status @imux_sync @imux_tab @imux_tab_auto @imux_tab_state @imux_terminal
@imux_trail @imux_view @imux_vscode_ipc @imux_web_client @imux_web_pane @imux_web_url
@imux_wt_agent @imux_wt_branch @imux_wt_client @imux_wt_pane`. They are set per pane (`set -p`),
per window (`set -w`) and globally (`set -g`), with `-u` and `-q`, and `@imux_press` with `-gF`.
The engine doesn't treat them specially; the list is here so tests cover each scope.

### Modifiers

`#{?c,a,b}` (and `#{?c1,a,c2,b,c}` chains), `#{==:a,b}`, `#{!=:a,b}`, `#{||:a,b}`,
`#{&&:a,b}`, `#{m:pattern,s}`, `#{e|+:a,b}`, `#{e|<:a,b}`, `#{P:fmt}` (each pane),
`#{W:fmt,current}` (each window, with the current window's form), `#{p/N/:s}` (pad to N, with N
itself a format between `/`), `#{E:…}` † and `#{T:…}` †, `##`, and nested formats in every
position.

### Styles in formats

`#[…]` with `fg=` and `bg=` (colour names, `colourN`, `#rrggbb`, `default`, and formats such as
`#{@imux_c_bg}`), `bold nobold dim nodim reverse noreverse default`, `align=left align=right`,
`range=window|N range=user|X norange`.

### Time

Status formats and `display-message` go through strftime: i-mux uses `%H:%M` in `status-right`,
and doubles `%` in literal messages.

### The engine (C3.5)

`internal/format` ports tmux 3.8's `format.c`: every modifier above and the rest of tmux's
(`=` `p` `s` `R` `n` `w` `b` `d` `q` `l` `a` `c` `t` `N` `E` `T` `S` `W` `P` `L` `I` `e` and the
comparisons), with tmux's limits (100 nested expansions, `R` at 10000 repeats and 64 KiB,
widths at 10000, and an expansion that runs over 100 ms gives what it has so far). Checked
against tmux 3.8 in `testdata/diff/formats.txt` (over 750 expressions, every format in i-mux's
source and generated config among them):

- Variables are looked up as tmux does: an option first (server, pane, window, session, with
  their parents; `status-format[1]` takes an array key), then the variable, then what the
  command added, then the environment (`#{HOME}`).
- `%if` and `%elif` conditions are expanded when the file is parsed, before any of its commands
  run: a file that sets `@imux_loaded` and then tests it sees it unset.
- `show-options` prints through tmux's own template (`option_name`, `option_value`, …), and `-F`
  takes another; `set-option -F` expands the value for the target.
- Quirks kept: `#{s/x*/-/:ab}` is `a-b-` (no empty match before the first character); a key
  that does not end (`#{session_name`) ends the expansion; `#{e|/:1,0}` is
  `-9223372036854775808`, as C's conversion of infinity.
- `#{version}` is `3.8`, the tmux the engine follows (`imux-core -V` names both).
- A window named with `-n` has `automatic-rename` off, as tmux's `spawn.c` does.
- `pane_dead_signal` is the signal's number, as tmux on a system without `sys_signame` (Linux).

Not yet, with the task that brings each: `#(…)` gives nothing (C4.2's status jobs); `#{A:…}`
moves only in status formats (C4.2); `#{C:…}` gives `0` (copy mode, C4.8); `pane_fg` and
`pane_bg` read only the `fg=` and `bg=` of `window-style` and `window-active-style` (C4.1);
the alert flags (`window_activity_flag`, `#` `!` `~` in `window_flags`) are off (C4.5);
`display-message` without `-p` shows nothing (C4.5), and its `-a`, `-I` and `-j` are refused.
The fuzzy matches (`m/p`, `m/z`) and the option and environment loops (`O`, `V`) are not in
i-mux's contract and give nothing.

## 5. Options

### Options i-mux sets and reads

| Option | Scope | Values used |
| --- | --- | --- |
| `default-terminal` | server (`set -g`) | `tmux-256color`, `screen-256color` |
| `terminal-features` | server array, `set -as` | `,*:RGB`, `,*:extkeys` |
| `terminal-overrides` | server array, `set -as` | `,*:Sync=\E[?2026%?%p1%{1}%-%tl%eh%;` |
| `extended-keys` | server | `on` |
| `escape-time` | server | `10` |
| `focus-events` | server (`set -g`) | `on` |
| `history-limit` | global session | from config |
| `mouse` | global session | `on` / `off` |
| `pane-border-status` | global window | `top` |
| `pane-border-format` | global window | format |
| `automatic-rename-format` | global window | format |
| `status` | global session | `on`, `2` |
| `status-position` | global session | `top`, `bottom` |
| `status-format[0]`, `status-format[1]` | global session array | formats; `set -gu status-format[0]` restores the default |
| `status-right`, `status-right-length`, `status-left-length` | global session | |
| `status-style` | global session | style |
| `remain-on-exit` | pane (`set -p`) | `on`, `off` |
| `default-shell` | global session, read (`show -gqv`) | |
| `prefix` | global session, read (`show -gqv`) | |
| `set-clipboard` | server | tmux's default (`external`) |

### Styles (the theme, D68 in i-mux)

`message-style message-command-style mode-style pane-border-style pane-active-border-style
popup-style popup-border-style menu-style menu-selected-style menu-border-style window-style
window-active-style pane-colours`. They are set with `set -g` and unset with `set -gu`.
`pane-colours` is an array of `#rrggbb` colours (`pane-colours[N]`).

## 6. Hooks

`pane-died`, `pane-exited`, `pane-focus-in`, `after-split-window`, `after-kill-pane`,
`window-pane-changed`, `client-attached`, `client-light-theme`, `client-dark-theme`, all set with
`set-hook -g`. Each runs in the context of its event, so the formats in the hook command
(`#{pane_id}`, `#{pane_dead_status}`) expand to that pane.

Checked against tmux 3.8 in `testdata/diff/hooks.txt` (C3.9):

- **When.** An `after-` hook runs right after its command, before the next one, for the same
  client (its output is the client's). Other events queue their hooks, which run once the
  command list (or whatever fired them) is done, in the order the events fired. Commands a
  hook runs fire no hooks of their own.
- **Which `after-`.** Commands marked for it in tmux's table fire `after-<command>` with
  their target; `split-window`, `new-window`, `new-session`, `select-pane` and
  `select-window` fire theirs with what they made or selected (`select-pane` only when the
  active pane changes).
- **Context.** A hook's commands run with the event's target as the current target, and see
  `#{hook}` and `#{hook_<item>}`: `hook_pane`, `hook_window` (and `hook_window_name`),
  `hook_session`, `hook_client`, `hook_new_pane` and `hook_old_pane` (window-pane-changed),
  `hook_exit_status` or `hook_exit_signal` and `hook_exit_success` (pane-exited, pane-died),
  and for an `after-` hook `hook_arguments`, `hook_argument_N` and `hook_flag_X`.
- **Order of a pane change.** With `focus-events` on: `pane-focus-out` of the old pane,
  `pane-focus-in` of the new, then `window-pane-changed`.
- **Pane exits.** `pane-exited` when the pane goes; with `remain-on-exit` (`on`, or `failed`
  and a failure) the pane stays dead and `pane-died` fires instead. The line tmux writes at a
  dead pane's bottom comes with C3.12.
- `client-attached` fires for `attach-session`, not for a `new-session` that attaches.

Dead panes (C3.12, `testdata/diff/lifecycle.txt`): a pane `remain-on-exit` keeps gets
`remain-on-exit-format` (expanded for it, `#{t:pane_dead_time}` included) on a new line at its
bottom, after its last output, and its cursor is hidden; `respawn-pane` without `-k` restarts
it with its command and directory. The format's `#[…]` styles are drawn with C4.1; until then
its text alone is.
- `set-hook` is `set-option` for hooks (`-a` adds a command, `-u` removes them); `-R` runs a
  hook now; `show-hooks` lists them.

## 7. Keys

### Tables

`prefix`, `root`, and the custom table `imux-resize`. `switch-client -T` enters a table for one
key; a binding in it can enter it again. Any key not bound in it falls back to the root table and
leaves the table. i-mux binds nothing in `copy-mode` and `copy-mode-vi` but relies on their
defaults.

### Key names

Bound: letters and punctuation (`a b c h j k l q r v H J K L S - , ?`), `C-p C-v C-\ M-v`,
`Left Right Up Down`, `S-Left S-Right S-Up S-Down`, `Enter Escape`. Sent with `send-keys`:
`C-\ C-p C-v M-v Enter`, `[ ] g q` †, and literal text.

### Mouse key names

Bound by i-mux: `MouseDown1Pane SecondClick1Pane DoubleClick1Pane TripleClick1Pane
MouseDown1Status MouseUp1Status MouseDown3Border`.

tmux 3.8's defaults that i-mux keeps and relies on: `MouseDrag1Pane` (copy mode and selection),
`MouseDragEnd1Pane` in `copy-mode` and `copy-mode-vi` (copy), `WheelUpPane` and `WheelDownPane`
(scrolling, entering copy mode), `MouseDrag1Border` (resizing), and the other status clicks
(`MouseUp1StatusLeft`, `MouseUp1StatusRight`, `MouseUp1StatusDefault`).

### Extended keys

With `extended-keys on` and the client's `extkeys` feature, modified keys (`C-v`, `C-Enter`,
`S-Enter`) are told apart and reach the pane encoded the way the pane asked for (CSI u or
modifyOtherKeys).

## 8. UI surfaces

- **Status lines:**
  - `status 2` gives two lines, `status-format[0..1]` with `#[align]` and `#[range]`;
  - clicks map to `mouse_status_range` (`window|N` gives the window index, `user|X` gives `X`);
  - tmux 3.4+ rule: a range is one cell longer than drawn. i-mux works around that; match it;
  - `%H:%M` refreshes every `status-interval`.
- **Pane borders:** single-line borders; the active pane's in `pane-active-border-style`; the
  `pane-border-status top` line shows `pane-border-format`. With tmux 3.8's defaults, borders
  are `themelightgrey` and the active pane's `themegreen`, which a client gets as white and green
  until its terminal reports its theme; the window's unused parts and the area outside it are
  drawn with `fill-character` (`·` outside). `testdata/render` holds the screens tmux draws.
- **Messages:** `display-message` shows on the status line for `display-time`, or `-d N` ms;
  `-d 0` until a key; `-d 1` clears a message at once.
- **Popups:** `-w -h` (cells or percentages), `-x -y`, `-E` close on exit, `-d` start
  directory, `-c` client and `-t` pane, and focus and mouse inside the popup.
- **Menus:** keyboard (keys, arrows, Enter, Escape) and mouse (`-M`, `-O` stay open on release),
  disabled items, separators, title (`-T`), position (`-x -y` with formats such as
  `#{e|+:#{popup_pane_left},N}`).
- **Command prompt:** `-I` initial text, `-p` prompt, `-t` client; the template's `%%` is
  replaced.
- **Copy mode:** entering it (`copy-mode -H`); the mouse selection a double or triple click
  makes; `copy-pipe-and-cancel` to buffer and clipboard; wheel scrolling in and out of it, and
  `pane_in_mode`.

### Replies to programs

The engine answers a program's queries exactly as tmux 3.8 does, including how it names itself:
DA1 `ESC [ ? 1 ; 2 c`, DA2 `ESC [ > 84 ; 0 ; 0 c` and XTVERSION `ESC P > | tmux 3.8 ESC \`, so
programs that adapt to tmux adapt the same way. `testdata/vt/replies.txt` lists every query
checked, and `replies.out` tmux's answers; the difftest runs it in a pane on both engines.

## 9. Environment of panes

| Variable | Value |
| --- | --- |
| `TMUX` | `<socket path>,<server pid>,<session id number>`. i-mux checks that the first field's base name is its socket name to know it is inside |
| `TMUX_PANE` | `%N` (i-mux reads it in 13 places) |
| `TERM` | `default-terminal` |
| `COLORTERM` | passed from the client when set |

Every other variable comes from tmux's global environment: the environment of the client that
started the server. i-mux and its tests rely on it for `HOME`, `PATH`, `SHELL`, `IMUX_HOME`,
`IMUX_TMUX_SOCKET`, `IMUX_TMUX_RECORD`, `IMUX_LANG` and the agents' homes
(`CLAUDE_CONFIG_DIR`, `CODEX_HOME`, `COPILOT_HOME`). The same holds for `run-shell` and
`if-shell` jobs.

## 10. Config file features

- `%if "#{…}"` / `%endif` (i-mux's config uses `%if "#{!=:#{@imux_loaded},1}"`);
- `set`, `bind`, `set-hook`, `source-file -q`;
- `{ … }` blocks, nested, on one line or across lines;
- comments;
- single and double quotes with tmux's escape rules (`\\` inside double quotes; single quotes keep
  `\E` in `terminal-overrides` as written).

tmux 3.8's parser has quirks the engine keeps, checked with `source-file -nv` (which parses a
file without running it and prints each line's commands) by the files in `testdata/cmdlang`:

- a quote left open runs to the end of the file, newlines included, with no error;
- a command's line, in errors and in `-v` output, is the line it ends on: after a `\`
  continuation or a newline inside quotes, and after a comment the next line (a command
  followed by a comment shares its group with the next line's commands);
- `~` is expanded at the start of a word and inside double quotes, not inside single quotes;
  `$VAR` and `${VAR}` are expanded when the file is parsed, so `NAME=value` lines in the same
  file do not reach later `$NAME`;
- the default command aliases (`split-pane`, `splitp`, `info`, …) are expanded when a file is
  parsed.

The user's `~/.imux/tmux.local.conf` is sourced last. With i-mux-core, a command it does not
know prints one warning and the rest of the file still loads.

Checked against tmux 3.8 in `testdata/diff/config.txt` (C3.11), with i-mux's generated config
and its reload (`unbind-key -q` for each key it bound, then `source-file`): the options, hooks
and every key table come out as tmux's. `source-file`:

- a relative path is relative to the client's directory (not the sourcing file's), and is a
  glob; `-F` expands it as a format; `-q` leaves out a path that matches nothing, else
  `No such file or directory: <path>`; the other paths still load;
- a file that does not parse runs none of its commands; its error (`<file>:<line>: …`) is
  printed after the other files' commands run, on stdout, and the client exits 1;
- an error while a sourced command runs is the message alone (the file and line are only for
  a config loaded with no client, which collects them);
- `#{current_file}` is the file; files nest up to 50 deep (`too many nested files`);
- `-n` parses only.

Without `-f`, i-mux-core loads no file, where tmux reads its default paths: i-mux always gives
`-f`. The startup config's errors are kept for the view mode of the first client (C4.8).

## 11. Attach behaviour

- `attach-session -t =name` from a terminal takes it over until detach.
- Two clients on one session see the same session. i-mux does not set `window-size`, so tmux
  3.8's default `latest` applies: the window follows the size of the client that used it last.
- Detach (`detach-client`, or the session ending) restores the terminal fully: modes, alternate
  screen, cursor, mouse and paste reporting off.

## 12. Extensions

Behaviour i-mux-core has and tmux does not. i-mux uses an extension only with i-mux-core, and only
where it needs it.

| # | Extension | Why |
| --- | --- | --- |
| X1 | **The argv form of `run-shell` and `if-shell`** (E14). A command whose text, after leading spaces, starts with `["` and is a JSON array of strings runs that argv directly, with no shell: the JSON is parsed before formats are expanded, then each element is expanded on its own, so values never need quoting. `{"argv": [...], "quiet": true}` does the same and also discards the output and the exit status, as `>/dev/null 2>&1 \|\| true` does. Otherwise output and exit status are handled as for a shell command, and for `if-shell` the exit status picks the branch. On every system the engine implements and tests it; i-mux uses it on Windows, where there is no POSIX shell | i-mux's bindings and hooks run `'<imux>' … >/dev/null 2>&1 \|\| true`, a POSIX line |

A shell command that is not in the argv form runs in `/bin/sh -c` on macOS and Linux, as in
tmux, and in `%COMSPEC% /d /s /c` on Windows (E14).

## 13. Known differences

| # | Difference | Why |
| --- | --- | --- |
| K1 | Windows: `pane_current_path` is the last OSC 7 / OSC 9;9 path, else the start directory | Reading another process's cwd is unreliable on Windows |
| K2 | Windows: `pane_dead_signal` is always empty | There are no signals |
| K3 | No control mode | Non-goal |
| K4 | Killing a pane (`kill-pane`, `kill-window`, `kill-session`, `respawn-pane -k`) sends SIGHUP and then, after a grace period, SIGKILL to the pane's process group and its terminal's foreground group. tmux only closes the PTY, so a program that ignores SIGHUP outlives its pane | An agent left running after its pane is gone keeps working unseen; tested by `internal/pty`'s kill test |
| K5 | A program's OSC 52 query (`ESC ] 52 ; c ; ?`) gets no answer. tmux answers it with the newest paste buffer when `set-clipboard` is `on`; with i-mux's setting, tmux's default `external`, tmux ignores OSC 52 from programs altogether, as the engine does | A program must not read the clipboard through the engine (ARCHITECTURE.md §7) |

| K6 | Floating panes (tmux 3.8's `new-pane`) do not exist; `select-layout` refuses a version 2 layout with a floating pane (`"z"`): `floating panes are not supported by imux-core` | i-mux does not use them; tested by `internal/layout`'s parse tests |
| K7 | `select-layout` with text that is not JSON at all reports `invalid json`; tmux names the position of the syntax error | Only broken strings differ; i-mux restores the strings tmux wrote; tested by `internal/layout`'s parse tests |

| K8 | Windows: a pane's program that asks for mouse reporting (1000, 1002, 1003, 1006) is not seen asking: ConPTY consumes the request. So mouse events are not forwarded into that pane, where tmux would forward them | ConPTY's own behaviour (E12); shown by the C0.7 spike's evidence (`spikes/conpty/EVIDENCE-26100.md`, `-20348.md`) |
| K9 | Windows 11 24H2 and later: an OSC 8 hyperlink from a pane's program carries an `id=` ConPTY added | ConPTY's own behaviour (E12); shown by the C0.7 evidence |
| K10 | Windows builds older than 26100 (seen on 20348): ConPTY consumes a program's alternate screen request (1049) and drops underline styles, so the pane does not switch screens and its history fills with full-screen output | ConPTY's own behaviour on those builds (E12); shown by `spikes/conpty/EVIDENCE-20348.md` |

New rows are added only with a test showing the difference and a reason.
