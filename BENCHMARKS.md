# Benchmarks

Baselines for i-mux-core's performance, to compare later changes against. The targets are in
[ARCHITECTURE.md §10](ARCHITECTURE.md); Phase 6 measures them all against tmux.

All numbers were taken on the owner's Apple M-series Mac (darwin/arm64), Go 1.27, on
2026-11-15. Run them with `make bench`, or one at a time as shown.

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

## Fuzzing

`make fuzz` runs every fuzz target briefly; the nightly workflow (`.github/workflows/nightly.yml`)
runs each for 10 minutes: `FuzzParser` and `FuzzTerminal` (internal/vt), `FuzzParse`
(internal/cmdlang), `FuzzWalk` (internal/format), `FuzzReader` (internal/ipc) and
`FuzzLayoutParse` (internal/layout).
