# i-mux-core

i-mux-core is the terminal engine i-mux will run on: a terminal multiplexer written in Go that
runs the same way on **macOS, Linux and Windows**.

Today i-mux runs on tmux, and tmux does not run on Windows. i-mux-core takes over the part of
tmux's job that i-mux uses, with one codebase for all three systems:

- sessions, windows and panes;
- popups, menus and the status line;
- key bindings, hooks and formats;
- attaching and detaching.

It is **not** a general tmux replacement. It implements exactly the tmux behaviour i-mux relies
on, which is written down in [COMPATIBILITY.md](COMPATIBILITY.md). Tests run the same commands
against real tmux and against i-mux-core to check that both behave the same.

## The golden rule: i-mux must never break

i-mux is a released, working product. The plan is built so that nothing changes for its users
until the new engine has proved itself:

1. **Separate repository.** All engine work happens here, in `i-mux-core`. The i-mux repository
   is not touched until the integration phase (Phase 7 in [TASKS.md](TASKS.md)).
2. **Same command language.** i-mux-core speaks the subset of tmux's command line that i-mux
   uses (`-L socket -f conf -u cmd ; cmd …`). i-mux already lets its tmux binary be replaced with
   `IMUX_TMUX_BIN`, so it can run on the new engine without code changes for a trial.
3. **tmux stays the default** on macOS and Linux. The default only switches when every i-mux
   end-to-end test passes on i-mux-core and the owner approves the switch. tmux remains a
   supported fallback after that (`[engine] backend = "tmux"`).
4. **Differential tests.** The same scripts run against real tmux and against i-mux-core, and
   their outputs are compared. Any difference is a bug in i-mux-core unless COMPATIBILITY.md
   documents it.
5. **Windows comes last.** Windows work starts only once the engine matches tmux on macOS and
   Linux.

## Documents

| File | What it is |
| --- | --- |
| [AGENTS.md](AGENTS.md) | Rules for the coding agent building this. Read it first. |
| [ARCHITECTURE.md](ARCHITECTURE.md) | The design: processes, packages, data flow, platform layers and security. |
| [COMPATIBILITY.md](COMPATIBILITY.md) | The contract: every tmux command, flag, format, option, hook and key i-mux uses. |
| [TASKS.md](TASKS.md) | The plan: phases and tasks, each with its goal, details and acceptance criteria. |
| [BENCHMARKS.md](BENCHMARKS.md) | Performance baselines, measured against tmux. |

## Phases at a glance

| Phase | What | Ends when |
| --- | --- | --- |
| 0 | Repository, CI on three systems, the contract, the differential test harness, spikes | CI is green on macOS, Linux and Windows, and each spike's decision is written down |
| 1 | PTY layer and terminal emulator (VT parser, screen, scrollback) | Synthetic byte-stream fixtures render the same as on the reference terminal |
| 2 | Server, client, sessions, windows, panes, layouts, attach and detach, rendering, input | Two clients can attach to one session and both see panes running shells |
| 3 | Command language, options, formats, key tables, hooks, config files | Every command in COMPATIBILITY.md parses and the differential tests pass |
| 4 | UI surfaces: status lines, borders, messages, popups, menus, prompts, copy mode, buffers | i-mux's generated tmux.conf loads and renders like it does on tmux |
| 5 | Mouse | Every mouse key name i-mux binds behaves like tmux |
| 6 | Parity with tmux on macOS and Linux: i-mux's e2e suite on i-mux-core, performance | All i-mux e2e tests pass on i-mux-core, with no performance regression |
| 7 | Integration into i-mux behind a switch (changes in the i-mux repository) | i-mux runs on either engine, and tmux is still the default |
| 8 | i-mux-core on Windows (ConPTY, console client, IPC, processes) | The engine's own tests pass on Windows CI |
| 9 | i-mux on Windows: POSIX assumptions, installer, self-update | i-mux runs natively on Windows and updates from Settings |
| 10 | Rollout | Beta, then the default, with tmux kept as a fallback |

## Status

Planning: nothing is implemented yet. The plan was written on 2026-11-15.
