#!/bin/sh
# Runs i-mux's e2e suite on i-mux-core (TASKS.md C6.2): imux-core is built and linked as
# "tmux" first on PATH, where i-mux's harness and i-mux look for tmux; i-mux (../i-mux,
# unchanged) is built for the suite; the results become PARITY.md.
#
#   scripts/imux-e2e.sh [path to i-mux]      (default: ../i-mux)
#
# The suite runs on its own sockets and temporary homes, as it always does; nothing is
# written to the i-mux repository. A failing suite still produces PARITY.md, and the script
# then exits with the suite's status.
set -eu

root=$(cd "$(dirname "$0")/.." && pwd)
imux=$(cd "${1:-$root/../i-mux}" && pwd)
cache="$root/.cache/imux-e2e"
bin="$cache/bin"

rm -rf "$cache"
mkdir -p "$bin"
(cd "$root" && CGO_ENABLED=0 go build -o "$cache/imux-core" ./cmd/imux-core)
ln -s "$cache/imux-core" "$bin/tmux"
(cd "$imux" && CGO_ENABLED=0 go build -trimpath -o "$cache/imux" ./cmd/imux)

echo "imux-e2e: running i-mux's e2e suite on imux-core (log: $cache/e2e.json)"
status=0
(cd "$imux" && PATH="$bin:$PATH" IMUX_E2E_BIN="$cache/imux" \
	go test -tags e2e -count=1 -json -timeout 60m ./e2e/... >"$cache/e2e.json" 2>"$cache/e2e.stderr") || status=$?
echo "imux-e2e: the suite exited with $status"

commit=$(git -C "$imux" rev-parse --short HEAD 2>/dev/null || echo unknown)
(cd "$root" && go run ./cmd/parity -commit "$commit" -notes parity/notes.txt "$cache/e2e.json" >PARITY.md)
echo "imux-e2e: PARITY.md written"
exit "$status"
