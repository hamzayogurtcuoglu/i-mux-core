# Compatibility contract

This is the tmux behaviour i-mux uses and i-mux-core must reproduce. A difference from tmux on
anything listed here is a bug, unless §12 records it as a known difference.

**Source.** This first version was drawn from the i-mux source (`internal/tmux/conf.go`,
`internal/tmux/tmux.go` and every `tmux.Cmd{…}` in `internal/`) on 2026-11-15.

**Keeping it complete.** Task C0.3 makes the list authoritative and keeps it up to date:
1. It runs i-mux's e2e suite with `IMUX_TMUX_RECORD`, which logs every tmux invocation i-mux
   makes.
2. It adds the generated `tmux.conf` and every command string i-mux builds.
3. It compares the result with this file. Anything missing is added here.

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
| `-S <path>` | Full socket path (implement it; tests use it) |
| `-f <file>` | Config file loaded when the server starts |
| `-u` | UTF-8 client; i-mux always passes it. Output keeps tabs and non-ASCII as they are |
| `-V` | Print the version (E9) |
| `cmd args ; cmd args` | Several commands in one invocation, as separate argv words (`;` alone). An argument ending in `\;` is a literal `;` |
| exit code | 0 on success, 1 when any command failed. Errors go to stderr |

### Error messages i-mux classifies

These substrings must appear, in lower case, in stderr for these cases:

| Case | Must contain |
| --- | --- |
| No server | `no server running on <socket path>` (also matched: `error connecting to`, `server exited`, `lost server`) |
| No session | `can't find session: <name>` |
| No pane or window | `can't find pane: <id>` / `can't find window: <id>` |
| No client | `can't find client: <name>` / `no current client` |

## 2. Targets

| Form | Meaning |
| --- | --- |
| `%N` | Pane id |
| `@N` | Window id |
| `$N` | Session id |
| `=name` | Session with exactly this name (no prefix match) |
| `name`, `name:index`, `name:index.pane` | Session, window, pane by name and index |
| `=` | In a mouse binding: the pane, window or status position under the mouse |
| (none) | The current client's session, window and pane; when there is no client, the most recently used session |
| `-c <client>` | Client by name (`client_name`, the tty path on Unix) |

## 3. Commands and flags

Every flag listed must work as in tmux 3.8. Aliases i-mux uses are in brackets.

| Command | Flags i-mux uses | Notes |
| --- | --- | --- |
| `new-session` | `-d -s -c -x -y -P -F` | `-P -F '#{pane_id}'` prints the new pane. Without `-d`, attaches |
| `attach-session` | `-t -u` | `-t =name` |
| `has-session` | `-t` | Exit code only |
| `kill-session` | `-t` | |
| `list-sessions` | `-F` | |
| `new-window` | `-a -c -n -t -P -F` | |
| `select-window` | `-t` | |
| `rename-window` | `-t` | |
| `kill-window` | `-t` | |
| `list-windows` | `-F -t` (check C0.3) | |
| `split-window` | `-h -v -b -f -d -l -c -t -P -F` | `-l N` cells; `-f` full width or height; `-b` before |
| `select-pane` | `-t -T -L -R -U -D` | `-T` sets the title; `-L/-R/-U/-D` from key bindings |
| `swap-pane` | `-d -s -t` | |
| `kill-pane` | `-t` | |
| `respawn-pane` | `-k -c -l -t` + command | `-l`? Check in C0.3 (tmux has no `-l` on respawn-pane; i-mux may pass it to another command) |
| `resize-pane` | `-t -x -L -R -U -D N`, `-Z` (check) | |
| `select-layout` | `-t <layout string>` | Restores a saved `#{window_layout}` |
| `list-panes` | `-a -s -t -F` | |
| `capture-pane` | `-p -J -S -N -t`, `-e` (difftest) | `-J` joins wrapped lines; `-S -N` starts N lines into the history |
| `send-keys` [`send`] | `-t`, `-M`, `-X <copy command>`, key names, literal strings | `-M` forwards the mouse event; `-X` runs a copy-mode command |
| `display-message` | `-p -d -c -t` + format | `-p` prints; `-d 0` stays until a key; `-d N` milliseconds |
| `display-popup` | `-E -B -b -s -S -T -L -Z -c -d -h -t -u -w -x -y -C` | Popup running a command; `-E` closes on exit. `-C` closes the open popup |
| `display-menu` | `-M -O -T -Z -c -t -x -y` + items | Items: `name key command`, `""` separator, `-name` disabled |
| `command-prompt` | `-I -p` + template | `%%%` replaced, quoted |
| `set-option` [`set`] | `-g -u -q -p -w -s -t -a -F -o` | Includes `set -as` (append, server) and arrays `name[N]` |
| `show-options` | `-g -q -v -p -w -s -t` and combinations (`-gqv`, `-pqv`, `-wqv`) | |
| `set-hook` | `-g` + hook name + command | |
| `bind-key` [`bind`] | `-n`, `-T table`, key, command or `{ … }` | |
| `unbind-key` | `-q -T` | |
| `switch-client` | `-c -t -T` | `-T table` switches key table; `-t =` the clicked session |
| `detach-client` | (none), `-t`/`-c` (check) | |
| `run-shell` [`run`] | `-b -C -d N -t -h` (check `-h`) | `-C` runs a tmux command; `-d` delays, seconds (fractions allowed) |
| `if-shell` [`if`] | `-F -t` + condition + then [+ else] | `-F` evaluates a format instead of a shell command |
| `source-file` | `-q` + path | `-q` ignores a missing file |
| `set-buffer` | `-b -w` | `-w` also sets the terminal clipboard (OSC 52) |
| `load-buffer` | `-b` + path | |
| `paste-buffer` | `-b -d -p -t` | `-p` bracketed paste when the pane asked for it |
| `copy-mode` | `-H` (+ `-e -u` check) | |

### Copy-mode commands (via `send -X`)

`select-word`, `select-line`, `copy-pipe-and-cancel` (with no command it copies to the buffer and
the clipboard), `cancel`, plus the scrolling and selection keys of the default `copy-mode` and
`copy-mode-vi` tables that mouse selection needs. Task C4.8 lists them exactly.

## 4. Formats

### Variables

| Group | Variables |
| --- | --- |
| Pane | `pane_id pane_index pane_active pane_width pane_height pane_left pane_top pane_right pane_bottom pane_title pane_current_command pane_current_path pane_dead pane_dead_status pane_dead_signal pane_in_mode pane_pid pane_start_command pane_tty mouse_any_flag` |
| Window | `window_id window_index window_name window_active window_width window_height window_layout window_zoomed_flag window_panes` |
| Session | `session_id session_name session_path session_windows session_attached session_activity` |
| Client | `client_name client_tty client_width client_height client_session client_flags client_termname client_activity client_control_mode` |
| Mouse | `mouse_pane mouse_status_range mouse_x mouse_y` |
| Popup | `popup_pane_left popup_pane_top` (and the rest of `popup_*` used for menu placement) |
| Server | `pid start_time version socket_path` |
| Aliases | `#S #I #W #P #D #T #H`, and `%H:%M` strftime in status formats |
| User options | Any `#{@name}`, looked up pane → window → session → global |

i-mux's user options include `@imux_role @imux_agent @imux_name @imux_label @imux_flag
@imux_status @imux_tab @imux_tab_auto @imux_tab_state @imux_state @imux_state_at @imux_cwd
@imux_start @imux_trail @imux_last @imux_view @imux_terminal @imux_seen @imux_reveal
@imux_press @imux_lock @imux_loaded @imux_sync @imux_hook @imux_hook_at @imux_fold
@imux_fallback @imux_done_at @imux_collapsed @imux_keys @imux_moved @imux_remote @imux_c_*`.
The engine doesn't treat them specially; the list is here so tests cover each scope they are
set in.

### Modifiers

`#{?c,a,b}`, `#{==:a,b}`, `#{!=:a,b}`, `#{||:a,b}`, `#{&&:a,b}`, `#{!:a}`, `#{m:pattern,s}`,
`#{e|+:a,b}` (and `- * /`, and the `f` float form), `#{P:fmt}` (each pane), `#{P:fmt,active}`,
`#{W:fmt,current}` (each window, with the current window's form), `#{p/N/:s}` (pad to N, with N
itself a format between `/`), `##`, nested formats in every position.

### Styles in formats

`#[fg=… bg=… bold nobold default align=left align=right range=window|N range=user|X range=left
range=right norange]`. Colours: names, `colourN`, `#rrggbb`, `default`, and the theme's option
references (`#{@imux_c_*}`).

## 5. Options

### Server and global options i-mux sets

| Option | Scope | Values used |
| --- | --- | --- |
| `default-terminal` | session (`-g`) | `tmux-256color`, `screen-256color` |
| `terminal-features` | server array, `set -as` | `,*:RGB`, `,*:extkeys` |
| `terminal-overrides` | server array, `set -as` | `,*:Sync=\E[?2026%?%p1%{1}%-%tl%eh%;` |
| `extended-keys` | server | `on` |
| `escape-time` | server | `10` |
| `history-limit` | session | from config |
| `mouse` | session | `on` / `off` |
| `focus-events` | server | `on` |
| `pane-border-status` | window | `top` |
| `pane-border-format` | window | format |
| `automatic-rename-format` | window | format |
| `status` | session | `on`, `2` |
| `status-position` | session | `top`, `bottom` |
| `status-format[0]`, `status-format[1]` | session array | formats; `set -gu status-format[0]` restores the default |
| `status-right`, `status-right-length`, `status-left-length` | session | |
| `remain-on-exit` | window/pane | `on`, `off` |
| `default-shell` | session | path |
| `prefix` | session | key (read by i-mux) |
| `set-clipboard` | server | tmux's default (`external`) |

### Styles (theme, D68 in i-mux)

`status-style message-style message-command-style mode-style pane-border-style
pane-active-border-style popup-style popup-border-style menu-style menu-selected-style
menu-border-style window-style window-active-style pane-colours`. They are set with `set -g` and
unset with `set -gu`.

## 6. Hooks

`pane-died`, `pane-exited`, `pane-focus-in`, `after-split-window`, `after-kill-pane`,
`window-pane-changed`, `client-attached`, `client-light-theme`, `client-dark-theme`,
`session-closed` (check C0.3). Each runs in the context of its event, so the formats in the hook
command (`#{pane_id}`, `#{pane_dead_status}`) expand to that pane.

## 7. Keys

### Tables

`prefix`, `root`, `copy-mode`, `copy-mode-vi`, and custom tables (`imux-resize`).
`switch-client -T` enters a table for one key. Any key not bound in it falls back to the root
table and leaves the table.

### Key names

Letters, digits and punctuation (`,` included); `C-x`, `M-x`, `C-M-x`, `S-x`; `Left Right Up
Down`, with `C-`, `M-`, `S-` and `C-M-`; `Enter Escape Tab BTab Space BSpace Home End PageUp
PageDown IC DC F1–F12`.

### Mouse key names

`MouseDown1Pane MouseUp1Pane MouseDrag1Pane MouseDragEnd1Pane SecondClick1Pane
DoubleClick1Pane TripleClick1Pane MouseDown3Border MouseDown1Status MouseUp1Status
MouseUp1StatusLeft MouseUp1StatusRight MouseUp1StatusDefault WheelUpPane WheelDownPane`, and
the defaults tmux binds to them.

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
  `-d 0` until a key.
- **Popups:** the size and position flags (`-w -h -x -y`, which accept percentages and formats),
  border lines (`-b`), styles (`-s -S`), title (`-T`), `-E` close on exit, `-d` cwd, `-e` env,
  and focus and mouse inside the popup.
- **Menus:** keyboard (keys, arrows, Enter, Escape) and mouse (`-M`, `-O` stay open on release),
  disabled items, separators, title, position (`-x -y` with formats such as
  `#{e|+:#{popup_pane_left},N}`).
- **Command prompt:** `-I` initial text, `-p` prompt; the template's `%%%` is replaced.
- **Copy mode:** entering it (`copy-mode -H`); the mouse selection a double or triple click
  makes; `copy-pipe-and-cancel` to buffer and clipboard; wheel scrolling in and out of it, and
  `pane_in_mode`.

## 9. Environment of panes

| Variable | Value |
| --- | --- |
| `TMUX` | `<socket path>,<server pid>,<session id number>` (i-mux checks it to know it is inside) |
| `TMUX_PANE` | `%N` (i-mux reads it in 13 places) |
| `TERM` | `default-terminal` |
| `COLORTERM` | passed from the client when set |

Plus the client's environment at session creation, as tmux's `update-environment` and global
environment do. C0.3 records exactly which variables i-mux's tests rely on.

## 10. Config file features

- `%if "#{…}"` / `%endif`;
- `set`, `bind`, `set-hook`, `source-file -q`;
- `{ … }` blocks across lines;
- `\` line continuation;
- comments;
- single and double quotes with tmux's escape rules.

The user's `~/.imux/tmux.local.conf` is sourced last. With i-mux-core, a command it does not
know prints one warning and the rest of the file still loads.

## 11. Attach behaviour

- `attach-session -t =name` from a terminal takes it over until detach.
- Two clients on one session see the same session at the smallest common size (tmux's default
  `window-size latest`: check in C0.3 which i-mux relies on).
- Detach (`detach-client`, or the session ending) restores the terminal fully: modes, alternate
  screen, cursor, mouse and paste reporting off.

## 12. Known differences

| # | Difference | Why |
| --- | --- | --- |
| K1 | Windows: `pane_current_path` is the last OSC 7 / OSC 9;9 path, else the start directory | Reading another process's cwd is unreliable on Windows |
| K2 | Windows: `pane_dead_signal` is always empty | There are no signals |
| K3 | No control mode | Non-goal |

New rows are added only with a test showing the difference and a reason.
