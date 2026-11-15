// Command layoutfixture records what real tmux does with the layout scenarios (TASKS.md C2.5):
// for each testdata/layout/<name>.txt it runs the steps on the pinned tmux and saves each
// step's panes and layout strings as <name>.golden.
//
//	layoutfixture -tmux .cache/tmux/3.8/bin/tmux [-check] [files…]
//
// With -check it compares instead of writing, and fails on any difference.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/layout/layoutfixture"
)

func main() {
	tmux := flag.String("tmux", ".cache/tmux/3.8/bin/tmux", "the pinned tmux")
	check := flag.Bool("check", false, "compare with the saved .golden files instead of writing them")
	dir := flag.String("dir", "testdata/layout", "the scenarios' directory")
	flag.Parse()
	files := flag.Args()
	if len(files) == 0 {
		var err error
		files, err = filepath.Glob(filepath.Join(*dir, "*.txt"))
		if err != nil || len(files) == 0 {
			fail("no scenarios")
		}
	}
	bin, err := exec.LookPath(*tmux)
	if err == nil {
		bin, err = filepath.Abs(bin)
	}
	if err != nil {
		fail(err.Error())
	}
	bad := 0
	for _, f := range files {
		steps, err := layoutfixture.Read(f)
		if err != nil {
			fail(err.Error())
		}
		states, err := layoutfixture.FromTmux(bin, steps)
		if err != nil {
			fail(f + ": " + err.Error())
		}
		got := layoutfixture.Format(states)
		golden := strings.TrimSuffix(f, ".txt") + ".golden"
		if *check {
			want, err := os.ReadFile(golden)
			if err != nil || string(want) != got {
				fmt.Fprintf(os.Stderr, "layoutfixture: %s differs from what tmux gives\n", golden)
				bad++
			}
			continue
		}
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			fail(err.Error())
		}
	}
	if bad > 0 {
		os.Exit(1)
	}
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "layoutfixture:", msg)
	os.Exit(1)
}
