#!/bin/sh
# Builds the pinned tmux the differential tests compare against, from its
# release tarball checked against its SHA-256, into a cache directory, and
# prints the path of the tmux binary. A second run reuses the build.
#
#   scripts/build-tmux.sh [cache dir]     (default: .cache/tmux)
#
# tmux is linked statically with a pinned utf8proc, also built from its checked
# tarball, so that character widths are the same on every system: the engine's
# width table comes from the same utf8proc (scripts/gen-widths.sh).
#
# Needs a C compiler, make, libevent and ncurses (on macOS: brew install
# libevent ncurses; on Linux: libevent-dev libncurses-dev bison pkg-config).
set -eu

TMUX_VERSION=3.8
TMUX_SHA256=e79c699c7e949dccd0a4a125e17b8d1261e311b16979dbc8eb34542f3966d82e
TMUX_URL="https://github.com/tmux/tmux/releases/download/$TMUX_VERSION/tmux-$TMUX_VERSION.tar.gz"
UTF8PROC_VERSION=2.12.0
UTF8PROC_SHA256=f564011d38b2888d583d510b08e69ffa15aa117155db1b9b49ef1dfe1fa25111
UTF8PROC_URL="https://github.com/JuliaStrings/utf8proc/archive/refs/tags/v$UTF8PROC_VERSION.tar.gz"

root=$(cd "$(dirname "$0")/.." && pwd)
cache=${1:-"$root/.cache/tmux"}
prefix="$cache/$TMUX_VERSION"
bin="$prefix/bin/tmux"
stamp="$prefix/.utf8proc-$UTF8PROC_VERSION"

if [ -x "$bin" ] && [ -f "$stamp" ] && "$bin" -V | grep -qx "tmux $TMUX_VERSION"; then
	echo "$bin"
	exit 0
fi

mkdir -p "$cache"
work=$(mktemp -d "$cache/build.XXXXXX")
trap 'rm -rf "$work"' EXIT

sha256() {
	if command -v sha256sum >/dev/null; then
		sha256sum "$1" | cut -d' ' -f1
	else
		shasum -a 256 "$1" | cut -d' ' -f1
	fi
}

# fetch <url> <sha256> <file>
fetch() {
	curl -sSLf -o "$3" "$1"
	sum=$(sha256 "$3")
	if [ "$sum" != "$2" ]; then
		echo "build-tmux: SHA-256 mismatch for $1: got $sum, want $2" >&2
		exit 1
	fi
}

# utf8proc, as a static library.
fetch "$UTF8PROC_URL" "$UTF8PROC_SHA256" "$work/utf8proc.tar.gz"
tar -xzf "$work/utf8proc.tar.gz" -C "$work"
u="$work/utf8proc-$UTF8PROC_VERSION"
(cd "$u" && make libutf8proc.a >"$work/utf8proc.log" 2>&1) || {
	cat "$work/utf8proc.log" >&2
	exit 1
}

# tmux.
fetch "$TMUX_URL" "$TMUX_SHA256" "$work/tmux.tar.gz"
tar -xzf "$work/tmux.tar.gz" -C "$work"
cd "$work/tmux-$TMUX_VERSION"

cflags=${CFLAGS:-}
ldflags=${LDFLAGS:-}
if command -v brew >/dev/null; then
	for f in libevent ncurses; do
		p=$(brew --prefix "$f" 2>/dev/null || true)
		if [ -n "$p" ] && [ -d "$p/include" ]; then
			cflags="$cflags -I$p/include"
			ldflags="$ldflags -L$p/lib"
		fi
	done
fi
LIBUTF8PROC_CFLAGS="-I$u"
LIBUTF8PROC_LIBS="$u/libutf8proc.a"
export LIBUTF8PROC_CFLAGS LIBUTF8PROC_LIBS

# jemalloc only changes memory allocation, and configure on macOS asks for a choice.
CFLAGS="$cflags -I$u" CPPFLAGS="$cflags -I$u" LDFLAGS="$ldflags" \
	./configure --prefix="$prefix" --enable-utf8proc --disable-jemalloc >"$work/configure.log" 2>&1 || {
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
touch "$stamp"
echo "$bin"
