#!/bin/sh
# Records the tmux calls i-mux makes (TASKS.md C0.3): it runs i-mux's e2e suite
# (../i-mux, unchanged) with a recording shim installed as "tmux" first on
# PATH, then extracts contract/recorded.json from the record.
#
#   scripts/contract.sh [path to i-mux]      (default: ../i-mux)
#
# The shim execs the pinned tmux 3.8 (scripts/build-tmux.sh). The e2e suite
# runs on its own sockets and temporary homes, as it always does; nothing is
# written to the i-mux repository. Failing e2e tests are reported but do not
# stop the extraction: their calls are recorded too.
set -eu

root=$(cd "$(dirname "$0")/.." && pwd)
imux=$(cd "${1:-$root/../i-mux}" && pwd)
cache="$root/.cache/contract"
record="$cache/record"
shim="$cache/shim"

tmux_bin=$("$root/scripts/build-tmux.sh")

rm -rf "$record" "$shim"
mkdir -p "$record" "$shim"
(cd "$root" && CGO_ENABLED=0 go build -o "$shim/tmux" ./cmd/tmuxrec)
printf '{"tmux": "%s", "out": "%s"}\n' "$tmux_bin" "$record" >"$shim/tmuxrec.json"
(cd "$imux" && CGO_ENABLED=0 go build -trimpath -o "$cache/imux" ./cmd/imux)

echo "contract: running i-mux's e2e suite with the recorder (log: $cache/e2e.log)"
status=0
(cd "$imux" && PATH="$shim:$PATH" IMUX_E2E_BIN="$cache/imux" \
	go test -tags e2e -count=1 -timeout 40m ./e2e/... >"$cache/e2e.log" 2>&1) || status=$?
grep -E '^(--- FAIL|FAIL|ok)' "$cache/e2e.log" || true
if [ "$status" -ne 0 ]; then
	echo "contract: the e2e suite failed (exit $status); its calls were recorded anyway"
fi
echo "contract: $(wc -l <"$record/calls.jsonl") calls recorded"

(cd "$root" && go run ./cmd/contract extract -o contract/recorded.json -source "$imux" "$record")
