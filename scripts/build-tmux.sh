#!/bin/sh
# Builds the pinned tmux the differential tests compare against, from its
# release tarball checked against its SHA-256, into a cache directory, and
# prints the path of the tmux binary. A second run reuses the build.
#
#   scripts/build-tmux.sh [cache dir]     (default: .cache/tmux)
#
# Needs a C compiler, make, libevent and ncurses (on macOS: brew install
# libevent ncurses utf8proc; on Linux: libevent-dev libncurses-dev
# libutf8proc-dev bison pkg-config).
set -eu

TMUX_VERSION=3.8
TMUX_SHA256=e79c699c7e949dccd0a4a125e17b8d1261e311b16979dbc8eb34542f3966d82e
URL="https://github.com/tmux/tmux/releases/download/$TMUX_VERSION/tmux-$TMUX_VERSION.tar.gz"

root=$(cd "$(dirname "$0")/.." && pwd)
cache=${1:-"$root/.cache/tmux"}
prefix="$cache/$TMUX_VERSION"
bin="$prefix/bin/tmux"

if [ -x "$bin" ] && "$bin" -V | grep -qx "tmux $TMUX_VERSION"; then
	echo "$bin"
	exit 0
fi

mkdir -p "$cache"
work=$(mktemp -d "$cache/build.XXXXXX")
trap 'rm -rf "$work"' EXIT

tarball="$work/tmux.tar.gz"
curl -sSLf -o "$tarball" "$URL"
if command -v sha256sum >/dev/null; then
	sum=$(sha256sum "$tarball" | cut -d' ' -f1)
else
	sum=$(shasum -a 256 "$tarball" | cut -d' ' -f1)
fi
if [ "$sum" != "$TMUX_SHA256" ]; then
	echo "build-tmux: SHA-256 mismatch for $URL: got $sum, want $TMUX_SHA256" >&2
	exit 1
fi

tar -xzf "$tarball" -C "$work"
cd "$work/tmux-$TMUX_VERSION"

cflags=${CFLAGS:-}
ldflags=${LDFLAGS:-}
if command -v brew >/dev/null; then
	for f in libevent ncurses utf8proc; do
		p=$(brew --prefix "$f" 2>/dev/null || true)
		if [ -n "$p" ] && [ -d "$p/include" ]; then
			cflags="$cflags -I$p/include"
			ldflags="$ldflags -L$p/lib"
		fi
	done
	# Without pkg-config, configure needs utf8proc's flags spelled out.
	if ! command -v pkg-config >/dev/null; then
		p=$(brew --prefix utf8proc)
		LIBUTF8PROC_CFLAGS="-I$p/include"
		LIBUTF8PROC_LIBS="-L$p/lib -lutf8proc"
		export LIBUTF8PROC_CFLAGS LIBUTF8PROC_LIBS
	fi
fi

# utf8proc on every system, so wide characters and emoji get the same widths on
# macOS and Linux (Homebrew builds tmux with it too). jemalloc only changes
# memory allocation, and configure on macOS asks for a choice.
CFLAGS="$cflags" CPPFLAGS="$cflags" LDFLAGS="$ldflags" ./configure --prefix="$prefix" --enable-utf8proc --disable-jemalloc >"$work/configure.log" 2>&1 || {
	cat "$work/configure.log" >&2
	exit 1
}
make -j4 >"$work/make.log" 2>&1 || {
	tail -50 "$work/make.log" >&2
	exit 1
}
make install >/dev/null

if ! "$bin" -V | grep -qx "tmux $TMUX_VERSION"; then
	echo "build-tmux: built $("$bin" -V), want tmux $TMUX_VERSION" >&2
	exit 1
fi
echo "$bin"
