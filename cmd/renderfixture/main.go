// Command renderfixture records what a client terminal shows on real tmux (TASKS.md C2.8): for
// each testdata/render/<name>.txt it makes the case's panes on the pinned tmux, attaches a
// client of the case's terminal and size in a PTY, feeds what tmux draws into the emulator and
// saves the screen as <name>.screen. internal/render's tests draw the same panes and compare.
//
//	renderfixture -tmux .cache/tmux/3.8/bin/tmux [-check] [cases…]
//
// With -check it compares instead of writing: CI uses it to show that the screens are still
// what tmux draws.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/render/renderfixture"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt/vtfixture"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "__write" {
		os.Exit(vtfixture.Helper(os.Args[2:]))
	}
	tmux := flag.String("tmux", ".cache/tmux/3.8/bin/tmux", "the pinned tmux")
	check := flag.Bool("check", false, "compare with the saved screens instead of writing them")
	dir := flag.String("dir", "testdata/render", "the cases' directory")
	termDir := flag.String("term", "testdata/term", "the client terminals' directory")
	terminfo := flag.String("terminfo", "testdata/term/terminfo", "the terminfo directory tmux reads")
	flag.Parse()
	cases := flag.Args()
	if len(cases) == 0 {
		var err error
		if cases, err = filepath.Glob(filepath.Join(*dir, "*.txt")); err != nil || len(cases) == 0 {
			fail(fmt.Errorf("no cases in %s", *dir))
		}
	}
	self, err := os.Executable()
	if err != nil {
		fail(err)
	}
	bin, err := exec.LookPath(*tmux)
	if err == nil {
		bin, err = filepath.Abs(bin)
	}
	if err != nil {
		fail(err)
	}
	if *terminfo, err = filepath.Abs(*terminfo); err != nil {
		fail(err)
	}
	failed := 0
	for _, p := range cases {
		c, err := renderfixture.ReadCase(p)
		if err != nil {
			fmt.Fprintln(os.Stderr, "renderfixture:", err)
			failed++
			continue
		}
		got, err := renderfixture.FromTmux(bin, self, *termDir, *terminfo, c, 20*time.Second)
		if err != nil {
			fmt.Fprintf(os.Stderr, "renderfixture: %s: %v\n", p, err)
			failed++
			continue
		}
		out := strings.TrimSuffix(p, ".txt") + ".screen"
		if *check {
			if want, err := os.ReadFile(out); err != nil || !bytes.Equal(want, []byte(got)) {
				fmt.Fprintf(os.Stderr, "renderfixture: %s differs from what tmux draws\n", out)
				failed++
			}
			continue
		}
		if err := os.WriteFile(out, []byte(got), 0o644); err != nil {
			fail(err)
		}
	}
	if failed > 0 {
		os.Exit(1)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "renderfixture:", err)
	os.Exit(1)
}
