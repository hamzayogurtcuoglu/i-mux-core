// Command parsefixture records how real tmux parses config files (TASKS.md C3.1): for each
// testdata/cmdlang/<name>.conf it runs `source-file -nv` on the pinned tmux, which parses the
// file without running it and prints each line's commands, and saves the output as
// <name>.golden. internal/commands parses the same files and compares.
//
//	parsefixture -tmux .cache/tmux/3.8/bin/tmux [-check] [files…]
//
// The file is read in a fixed environment (Env below), with HOME a temporary directory written
// as $HOME in the output, and the file's own path written as <conf>.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Env is the environment the files are parsed in, besides HOME and TMUX_TMPDIR. The tests
// use the same.
var Env = []string{"PATH=/usr/bin:/bin", "SHELL=/bin/sh", "TERM=xterm-256color", "LANG=C.UTF-8", "FOO=bar", "EMPTY=", "SPACED=a b"}

func main() {
	tmux := flag.String("tmux", ".cache/tmux/3.8/bin/tmux", "the pinned tmux")
	check := flag.Bool("check", false, "compare with the saved .golden files instead of writing them")
	dir := flag.String("dir", "testdata/cmdlang", "the files' directory")
	flag.Parse()
	files := flag.Args()
	if len(files) == 0 {
		var err error
		if files, err = filepath.Glob(filepath.Join(*dir, "*.conf")); err != nil || len(files) == 0 {
			fail("no files")
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
		got, err := parse(bin, f)
		if err != nil {
			fail(f + ": " + err.Error())
		}
		golden := strings.TrimSuffix(f, ".conf") + ".golden"
		if *check {
			if want, err := os.ReadFile(golden); err != nil || string(want) != got {
				fmt.Fprintf(os.Stderr, "parsefixture: %s differs from what tmux gives\n", golden)
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

func parse(bin, file string) (string, error) {
	dir, err := os.MkdirTemp("/tmp", "imux-pf-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	if d, err := filepath.EvalSymlinks(dir); err == nil {
		dir = d
	}
	src, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	conf := filepath.Join(dir, "c.conf")
	if err := os.WriteFile(conf, src, 0o600); err != nil {
		return "", err
	}
	cmd := exec.Command(bin, "-u", "-S", filepath.Join(dir, "s"), "-f", os.DevNull,
		"start-server", ";", "source-file", "-nv", conf)
	cmd.Env = append([]string{"HOME=" + dir, "TMUX_TMPDIR=" + dir}, Env...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err = cmd.Run()
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		return "", err
	}
	clean := func(s string) string {
		s = strings.ReplaceAll(s, conf, "<conf>")
		return strings.ReplaceAll(s, dir, "$HOME")
	}
	return fmt.Sprintf("stdout:\n%sstderr:\n%sexit %d\n", clean(out.String()), clean(errb.String()), code), nil
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "parsefixture:", msg)
	os.Exit(1)
}
