# Notes for coding agents working on i-mux-core

Read [README.md](README.md) first, then [ARCHITECTURE.md](ARCHITECTURE.md),
[COMPATIBILITY.md](COMPATIBILITY.md) and [TASKS.md](TASKS.md). Work through TASKS.md in order, and
finish every task listed under a task's "Depends on" before starting it.

## Never break i-mux

- **Don't touch i-mux before Phase 7.** Do not change the i-mux repository (`../i-mux`) before
  then. Read it as much as you like: it is the source of truth for what the engine must do.
- **From Phase 7, keep tmux the default.** Every change to i-mux sits behind the engine switch,
  and tmux stays the default on macOS and Linux. i-mux's own `make ci` must keep passing with
  tmux, exactly as before.
- **Never install over the owner's i-mux.** Don't overwrite the i-mux the owner is running
  (`~/.local/bin/imux`). In the i-mux repository, install builds only with `make install-local`.
- **Leave the owner's servers alone.** Never kill or attach to the owner's running tmux or i-mux
  servers. Tests use their own socket names and their own temporary `HOME`.

## How to work

- **Docs first.** If a task changes a decision, update ARCHITECTURE.md, COMPATIBILITY.md or
  TASKS.md before writing its code, and commit that change (`docs: …`). Then commit the code
  (`feat: …`, `fix: …`, `test: …`). Mark the task `[x]` in TASKS.md in the same commit as its code.
- **One task, one commit,** where possible. Write commit messages in English.
- **No AI co-author lines.** Never add `Co-Authored-By:` trailers, for Claude or any other AI
  agent. The owner is the only author.
- **No other product names.** Do not name other terminal-workspace products in code, comments,
  docs or commits. You may name tmux and terminal emulators (Windows Terminal, iTerm2, Ghostty,
  kitty, WezTerm, …).
- **No pushing or tagging.** The owner does that.
- **Run `make ci` before each commit.** It runs lint, unit tests and the differential tests on
  whichever systems are available here. Say which checks could not run, for example
  Windows-only tests on a Mac.
- **Check tmux, don't guess.** When tmux's behaviour is unclear, write a differential test against
  real tmux and record the answer in COMPATIBILITY.md.

## Tests

- **Synthetic fixtures only.** Never read the real `~/.claude`, `~/.gemini`, `~/.copilot`,
  `~/.codex`, `~/.imux` or `~/.tmux.conf`. Every test points `HOME` and the socket directory at
  temp dirs.
- **No real agent CLIs.** Never start Claude Code, Codex, Copilot, Antigravity or OpenCode with a
  real prompt. For full-screen programs, use the fake TUI in `testdata/` (task C1.9) and the fake
  agents in `../i-mux/e2e/fakeagents`.
- **Regression tests.** Every bug fix comes with a test that failed before the fix.
- **No bare sleeps.** Where you can, wait for a condition with a timeout rather than sleeping
  for a fixed time.

## Code

- **Go and tooling.** Use the same Go version as i-mux (its `go.mod`). Format with gofumpt. Lint
  with golangci-lint, starting from i-mux's `.golangci.yml`.
- **Platform code.** Put it in files with build tags (`_unix.go`, `_windows.go`), behind the
  small interfaces in ARCHITECTURE.md §6. Core packages have no `runtime.GOOS` switches.
- **Dependencies.** Keep them few, permissively licensed (MIT, BSD, Apache-2.0 or ISC) and
  pinned. Justify each new one in its commit message.
- **tmux's license.** tmux is ISC-licensed. Reading its source to understand behaviour is fine;
  if you port code from it, keep its copyright notice in that file.
- **Security.** The server accepts connections only from its own user, on every system
  (ARCHITECTURE.md §7).
