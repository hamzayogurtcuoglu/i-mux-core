// Command vtfixture creates the expected screens of the VT fixtures (TASKS.md C1.12) from real
// tmux: for each testdata/vt/<name>[@WxH].in it runs a tmux pane of that size (80x24 if none),
// writes the bytes into it unchanged, and saves what tmux shows as <name>[@WxH].screen.
//
//	vtfixture -tmux .cache/tmux/3.8/bin/tmux [-check] [files…]
//
// With -check it compares instead of writing, and fails on any difference: CI uses it to show
// that the fixtures are still what tmux gives.
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

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt/vtfixture"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "__write" {
		os.Exit(vtfixture.Helper(os.Args[2:]))
	}
	tmux := flag.String("tmux", ".cache/tmux/3.8/bin/tmux", "the pinned tmux")
	check := flag.Bool("check", false, "compare with the saved screens instead of writing them")
	dir := flag.String("dir", "testdata/vt", "the fixtures' directory")
	flag.Parse()
	files := flag.Args()
	if len(files) == 0 {
		if !*check {
			for name, data := range vtfixture.Specs {
				p := filepath.Join(*dir, name)
				if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
					fmt.Fprintln(os.Stderr, "vtfixture:", err)
					os.Exit(1)
				}
			}
		}
		var err error
		files, err = filepath.Glob(filepath.Join(*dir, "*.in"))
		if err != nil || len(files) == 0 {
			fmt.Fprintln(os.Stderr, "vtfixture: no fixtures")
			os.Exit(1)
		}
	}
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "vtfixture:", err)
		os.Exit(1)
	}
	tmuxPath, err := exec.LookPath(*tmux)
	if err == nil {
		tmuxPath, err = filepath.Abs(tmuxPath)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "vtfixture: tmux:", err)
		os.Exit(1)
	}
	failed := 0
	for _, in := range files {
		got, err := vtfixture.FromTmux(tmuxPath, self, in, 20*time.Second)
		if err != nil {
			fmt.Fprintf(os.Stderr, "vtfixture: %s: %v\n", in, err)
			failed++
			continue
		}
		out := strings.TrimSuffix(in, ".in") + ".screen"
		if *check {
			want, err := os.ReadFile(out)
			if err != nil || !bytes.Equal(want, []byte(got)) {
				fmt.Fprintf(os.Stderr, "vtfixture: %s differs from tmux\n", out)
				failed++
			}
			continue
		}
		if err := os.WriteFile(out, []byte(got), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "vtfixture:", err)
			failed++
		}
	}
	if failed > 0 {
		os.Exit(1)
	}
}
