#!/bin/sh
# Measures ARCHITECTURE.md §10's performance targets under the pinned tmux and imux-core, one
# after the other on this machine (TASKS.md C6.4), and writes the table into BENCHMARKS.md
# between its "bench" markers. With an i-mux checkout, i-mux's startup is measured too.
#
#   scripts/bench.sh [path to i-mux] [bench flags…]   (default: ../i-mux, when it is there)
#
# Every run has its own socket, HOME and config under /tmp; nothing of the owner's is used. The
# script exits 1 when a target is missed (the table is written anyway).
set -eu

root=$(cd "$(dirname "$0")/.." && pwd)
cache="$root/.cache/bench"
mkdir -p "$cache"
imux_dir=""
if [ $# -gt 0 ] && [ "${1#-}" = "$1" ]; then
	imux_dir=$(cd "$1" && pwd)
	shift
elif [ -d "$root/../i-mux/e2e/fakeagents" ]; then
	imux_dir=$(cd "$root/../i-mux" && pwd)
fi

tmux=$("$root/scripts/build-tmux.sh")
# As released (the Makefile's build): stripped, which also makes it start faster.
(cd "$root" && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$cache/imux-core" ./cmd/imux-core)
(cd "$root" && CGO_ENABLED=0 go build -o "$cache/bench" ./cmd/bench)
set -- -tmux "$tmux" -core "$cache/imux-core" "$@"
if [ -n "$imux_dir" ]; then
	(cd "$imux_dir" && CGO_ENABLED=0 go build -trimpath -o "$cache/imux" ./cmd/imux)
	set -- -imux "$cache/imux" -fakes "$imux_dir/e2e/fakeagents" "$@"
fi

status=0
"$cache/bench" "$@" >"$cache/table.md" || status=$?
if [ "$status" -gt 1 ]; then
	echo "bench: the measurements did not run" >&2
	exit "$status"
fi

{
	echo "Measured by \`scripts/bench.sh\` on $(uname -sm), $(go version | cut -d' ' -f3), on $(date -u +%Y-%m-%d)."
	echo
	cat "$cache/table.md"
} >"$cache/section.md"
awk -v section="$cache/section.md" '
	/<!-- bench:start -->/ { print; while ((getline line < section) > 0) print line; skip = 1; next }
	/<!-- bench:end -->/ { skip = 0 }
	!skip { print }
' "$root/BENCHMARKS.md" >"$cache/BENCHMARKS.md"
mv "$cache/BENCHMARKS.md" "$root/BENCHMARKS.md"
cat "$cache/table.md"
exit "$status"
