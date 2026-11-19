#!/bin/sh
# Runs a package's tests on the Windows that hosts this WSL, through WSL's interop: the tests are
# built for Windows, copied to Windows' temporary folder, and run there (Phase 8).
#
#   scripts/wsl-windows-test.sh ./internal/pty [-test.v -test.run ConPTY ...]
#
# IMUX_FAKETUI and IMUX_CORE_BIN give the tests Windows builds of testdata/faketui and of
# imux-core, as Windows has no Go here.
set -eu

root=$(cd "$(dirname "$0")/.." && pwd)
command -v cmd.exe >/dev/null 2>&1 || PATH="$PATH:/mnt/c/Windows/System32"
wintemp=$(cd /mnt/c && cmd.exe /d /c 'echo %TEMP%' | tr -d '\r')
dir=$(wslpath -u "$wintemp")/imux-core-wintest
mkdir -p "$dir"

pkg=$1
shift
name=$(basename "$pkg")
cd "$root"
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c -o "$dir/$name.test.exe" "$pkg"
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o "$dir/faketui.exe" ./testdata/faketui
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o "$dir/imux-core.exe" ./cmd/imux-core

# The tests run in their package's folder, as go test runs them, to find testdata: Windows sees
# it as a UNC path, which Go handles (cmd.exe does not, so the tests give it a folder of its own).
cd "$root/$pkg"
status=0
IMUX_FAKETUI="$wintemp\\imux-core-wintest\\faketui.exe" IMUX_CORE_BIN="$wintemp\\imux-core-wintest\\imux-core.exe" \
	WSLENV=IMUX_FAKETUI:IMUX_CORE_BIN "$dir/$name.test.exe" "$@" >"$dir/$name.out" 2>&1 || status=$?
tr -d '\r' <"$dir/$name.out"
exit "$status"
