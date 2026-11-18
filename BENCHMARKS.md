# Benchmarks

Baselines for i-mux-core's performance, to compare later changes against. The targets are in
[ARCHITECTURE.md §10](ARCHITECTURE.md); Phase 6 measures them all against tmux.

All numbers were taken on the owner's Apple M-series Mac (darwin/arm64), Go 1.27, on
2026-11-15. Run them with `make bench`, or one at a time as shown.

## Against tmux: ARCHITECTURE.md §10's targets (C6.4)

`scripts/bench.sh` measures each target under the pinned tmux 3.8 and i-mux-core, one after the
other on the same machine (`cmd/bench`; ARCHITECTURE.md §10 says how each is measured), and
writes this table. CI runs it on its Linux and macOS runners and keeps BENCHMARKS.md as an
artifact.

<!-- bench:start -->
Measured by `scripts/bench.sh` on Darwin arm64, go1.27.3, on 2026-11-18.

| What | tmux 3.8 | i-mux-core | Target | Met |
| --- | --- | --- | --- | --- |
| Idle CPU (10 sessions × 4 panes, one client, 10 s) | 0.00% | 0.00% | ≤ 0.5% of a core | yes |
| Key to screen latency (p99 of 300 keys, in turns) | 0.47 ms | 1.02 ms | ≤ tmux + 2 ms | yes |
| Throughput (100 MB into a 200×50 pane) | 4.18 s | 1.63 s | ≤ 1.5× tmux | yes |
| Command round trip (p50 of 200) | 5.29 ms | 5.04 ms | ≤ tmux + 1 ms | yes |
| Program start alone (-V, p50 of 200) | 4.79 ms | 4.59 ms | none (shows the round trip's share) | — |
| Memory per pane (history-limit 50000, filled) | 25.1 MiB | 32.3 MiB | ≤ 2× tmux | yes |
| i-mux startup to the sidebar (median of 5, in turns) | 253.95 ms | 240.09 ms | ≤ tmux + 10% | yes |
<!-- bench:end -->

### On the CI runners

CI run 44204870252 (commit `d0f14ce`), every target met on both runners. On the Linux runner
i-mux's startup is at the edge of its target, and missed it in most of the runs before this
one: TASKS.md C6.4 says why and what is left to decide.

GitHub's ubuntu-latest runner, measured by `scripts/bench.sh` on Linux x86_64, go1.27.0 on 2026-11-18:

| What | tmux 3.8 | i-mux-core | Target | Met |
| --- | --- | --- | --- | --- |
| Idle CPU (10 sessions × 4 panes, one client, 10 s) | 0.00% | 0.00% | ≤ 0.5% of a core | yes |
| Key to screen latency (p99 of 300 keys, in turns) | 0.29 ms | 0.69 ms | ≤ tmux + 2 ms | yes |
| Throughput (100 MB into a 200×50 pane) | 2.81 s | 1.62 s | ≤ 1.5× tmux | yes |
| Command round trip (p50 of 200) | 1.15 ms | 1.42 ms | ≤ tmux + 1 ms | yes |
| Program start alone (-V, p50 of 200) | 0.80 ms | 1.13 ms | none (shows the round trip's share) | — |
| Memory per pane (history-limit 50000, filled) | 21.6 MiB | 28.7 MiB | ≤ 2× tmux | yes |
| i-mux startup to the sidebar (median of 5, in turns) | 63.49 ms | 69.60 ms | ≤ tmux + 10% | yes |

GitHub's macos-latest runner, measured by `scripts/bench.sh` on Darwin arm64, go1.27.0 on 2026-11-18:

| What | tmux 3.8 | i-mux-core | Target | Met |
| --- | --- | --- | --- | --- |
| Idle CPU (10 sessions × 4 panes, one client, 10 s) | 0.00% | 0.00% | ≤ 0.5% of a core | yes |
| Key to screen latency (p99 of 300 keys, in turns) | 1.06 ms | 2.63 ms | ≤ tmux + 2 ms | yes |
| Throughput (100 MB into a 200×50 pane) | 8.01 s | 3.28 s | ≤ 1.5× tmux | yes |
| Command round trip (p50 of 200) | 6.28 ms | 5.01 ms | ≤ tmux + 1 ms | yes |
| Program start alone (-V, p50 of 200) | 5.86 ms | 6.83 ms | none (shows the round trip's share) | — |
| Memory per pane (history-limit 50000, filled) | 25.2 MiB | 32.1 MiB | ≤ 2× tmux | yes |
| i-mux startup to the sidebar (median of 5, in turns) | 382.01 ms | 392.33 ms | ≤ tmux + 10% | yes |

## Emulator (C1.13)

`go test -run '^$' -bench 'Terminal' -benchtime 3x ./internal/vt/`: the whole emulator (parser
and screen) on a 200×50 screen with a 2000-line history, fed 8 MiB in 32 KiB writes.

| Stream | Throughput | Allocations |
| --- | --- | --- |
| Plain text lines (`BenchmarkTerminalPlain`) | 58.7 MB/s | 13.8 per KB |
| Colour-heavy: RGB, 256-colour, bold, curly underline (`BenchmarkTerminalColour`) | 30.5 MB/s | 42.8 per KB |
| Cursor-heavy: moves, erasing, clears, insert and delete (`BenchmarkTerminalCursor`) | 14.5 MB/s | 47.4 per KB |
| Mixed, as agent TUIs write (`BenchmarkTerminalMixed`) | 13.7 MB/s | 218.4 per KB |

The parser alone (`BenchmarkParserMixed`) runs at 180.9 MB/s.

### Against tmux: 100 MB of mixed output

The same deterministic 100 MB mixed stream as the emulator spike (C0.5, ARCHITECTURE.md §8),
with `IMUX_VT_FIXTURE=<file> go test -run TestTerminalFixtureThroughput -v ./internal/vt/`:

| | Time | Throughput |
| --- | --- | --- |
| i-mux-core's emulator | 4.33 s | 24.2 MB/s |
| tmux 3.8, end to end through a pty (`cat` in a pane) | 10.1 s | 10.4 MB/s |

The emulator takes 0.43× tmux's time, well under the target of 1.5×. The comparison is not like
for like (tmux's figure includes the pty and its event loop); Phase 6 measures both end to end.

## History memory (C1.10)

`go test -run '^$' -bench HistoryMemory -benchtime 3x ./internal/vt/`: a full 50000-line
history of 80-column lines, a third of them coloured.

| | Memory |
| --- | --- |
| i-mux-core | 23.4 MiB of Go heap for 50024 lines (527 bytes a line) |
| tmux 3.8, the same output in one more pane | 16.0 MiB more server RSS (46643 lines kept) |

The target is at most 2× tmux's.

## Renderer (C2.8)

`go test -run '^$' -bench . -benchmem ./internal/render/`, on Linux (WSL2, Intel i7-13800H),
Go 1.27, on 2026-11-16: an 80×24 client with two panes side by side.

| | Time | Written | Allocated |
| --- | --- | --- | --- |
| One changed cell (`BenchmarkOneChangedCell`) | 60 µs a frame | 19 bytes a frame | 650 B |
| A whole frame of a busy screen (`BenchmarkWholeFrame`) | 261 µs | | 99 KB |

## Fuzzing

`make fuzz` runs every fuzz target briefly; the nightly workflow (`.github/workflows/nightly.yml`)
runs each for 10 minutes: `FuzzParser` and `FuzzTerminal` (internal/vt), `FuzzParse`
(internal/cmdlang), `FuzzWalk` (internal/format), `FuzzReader` (internal/ipc),
`FuzzLayoutParse` (internal/layout), `FuzzParseTerminfo` (internal/termout) and `FuzzInput`
(internal/input).
