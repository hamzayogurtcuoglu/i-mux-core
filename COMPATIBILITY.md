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
  `pane-border-status top` line shows `pane-border-format`.
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
