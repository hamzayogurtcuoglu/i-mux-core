// Command termfixture records how real tmux describes client terminals (TASKS.md C2.7): for
// each case in testdata/term/cases.txt it attaches a client to the pinned tmux in a PTY with
// that TERM (and COLORTERM, terminal-features and terminal-overrides), and saves what
// `show-messages -T` and #{client_termfeatures} show, with the terminfo capabilities
// internal/termout reads for the terminal, as testdata/term/<name>.json.
//
//	termfixture -tmux .cache/tmux/3.8/bin/tmux [-terminfo dir] [-check]
//
// Both tmux and the reader read terminfo from the same directory (TERMINFO): by default
// testdata/term/terminfo, entries copied from ncurses 6.6's database so that every system
// records the same.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/pty"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/termout"
)

// Case is one recorded terminal; the tests read the same files.
type Case struct {
	Name         string   `json:"name"`
	Term         string   `json:"term"`
	ColorTerm    string   `json:"colorterm,omitempty"`
	Features     []string `json:"features,omitempty"`  // appended to terminal-features
	Overrides    []string `json:"overrides,omitempty"` // appended to terminal-overrides
	Caps         []string `json:"caps"`                // what termout reads from terminfo
	Describe     []string `json:"describe"`            // show-messages -T, without its header
	Flags        string   `json:"flags"`               // the header's flags=
	TermFeatures string   `json:"termfeatures"`        // #{client_termfeatures}
}

func main() {
	tmux := flag.String("tmux", ".cache/tmux/3.8/bin/tmux", "the pinned tmux")
	terminfo := flag.String("terminfo", "testdata/term/terminfo", "the terminfo directory, read by tmux and by termout")
	check := flag.Bool("check", false, "compare with the saved files instead of writing them")
	dir := flag.String("dir", "testdata/term", "the cases' directory")
	flag.Parse()
	if abs, err := filepath.Abs(*terminfo); err == nil {
		*terminfo = abs
	}
	bin, err := exec.LookPath(*tmux)
	if err == nil {
		bin, err = filepath.Abs(bin)
	}
	if err != nil {
		fail(err)
	}
	cases, err := readCases(filepath.Join(*dir, "cases.txt"))
	if err != nil {
		fail(err)
	}
	bad := 0
	for _, c := range cases {
		if err := record(bin, *terminfo, &c); err != nil {
			fail(fmt.Errorf("%s: %w", c.Name, err))
		}
		b, _ := json.MarshalIndent(c, "", "  ")
		b = append(b, '\n')
		p := filepath.Join(*dir, c.Name+".json")
		if *check {
			if want, err := os.ReadFile(p); err != nil || !bytes.Equal(want, b) {
				fmt.Fprintf(os.Stderr, "termfixture: %s differs from what tmux gives\n", p)
				bad++
			}
			continue
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			fail(err)
		}
	}
	if bad > 0 {
		os.Exit(1)
	}
}

// readCases reads lines "name|TERM|COLORTERM|feature entries|override entries", the entries
// separated by spaces.
func readCases(p string) ([]Case, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Case
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "|")
		for len(parts) < 5 {
			parts = append(parts, "")
		}
		out = append(out, Case{
			Name: parts[0], Term: parts[1], ColorTerm: parts[2],
			Features: strings.Fields(parts[3]), Overrides: strings.Fields(parts[4]),
		})
	}
	return out, sc.Err()
}

func record(bin, terminfo string, c *Case) error {
	ti, err := termout.Lookup(c.Term, []string{terminfo})
	if err != nil {
		return err
	}
	c.Caps = ti.Capabilities()

	dir, err := os.MkdirTemp("/tmp", "imux-tf-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "s")
	env := []string{
		"HOME=" + dir, "TMUX_TMPDIR=" + dir, "PATH=/usr/bin:/bin", "SHELL=/bin/sh",
		"LANG=C.UTF-8", "TERM=" + c.Term, "TERMINFO=" + terminfo,
	}
	if c.ColorTerm != "" {
		env = append(env, "COLORTERM="+c.ColorTerm)
	}
	var conf strings.Builder
	for _, f := range c.Features {
		fmt.Fprintf(&conf, "set -as terminal-features ',%s'\n", f)
	}
	for _, o := range c.Overrides {
		fmt.Fprintf(&conf, "set -as terminal-overrides ',%s'\n", o)
	}
	confPath := filepath.Join(dir, "c.conf")
	if err := os.WriteFile(confPath, []byte(conf.String()), 0o600); err != nil {
		return err
	}
	run := func(args ...string) (string, error) {
		cmd := exec.Command(bin, append([]string{"-S", sock, "-f", confPath}, args...)...)
		cmd.Env = env
		var out, errb bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errb
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("%v: %s", args, errb.String())
		}
		return out.String(), nil
	}
	defer func() { _, _ = run("kill-server") }()
	if _, err := run("new-session", "-d", "-s", "a", "sleep 100000"); err != nil {
		return err
	}
	p, err := pty.New().Start(pty.Spec{
		Path: bin, Argv: []string{"tmux", "-u", "-S", sock, "attach", "-t", "a"},
		Env: env, Dir: dir, Cols: 80, Rows: 24,
	})
	if err != nil {
		return err
	}
	defer func() { _ = p.Kill(); _ = p.Close() }()
	go func() { // drain what tmux draws
		b := make([]byte, 65536)
		for {
			if _, err := p.Read(b); err != nil {
				return
			}
		}
	}()
	var client string
	deadline := time.Now().Add(10 * time.Second)
	for client == "" {
		out, _ := run("list-clients", "-F", "#{client_name}")
		client = strings.TrimSpace(out)
		if time.Now().After(deadline) {
			return errors.New("the client did not attach")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Let the client's terminal queries go unanswered past tmux's wait for them.
	time.Sleep(1500 * time.Millisecond)
	out, err := run("show-messages", "-T")
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "Terminal 0: ") {
		return fmt.Errorf("show-messages -T: %q", out)
	}
	_, c.Flags, _ = strings.Cut(lines[0], "flags=")
	c.Flags = strings.TrimSuffix(c.Flags, ":")
	c.Describe = lines[1:]
	out, err = run("display-message", "-p", "-c", client, "#{client_termfeatures}")
	if err != nil {
		return err
	}
	c.TermFeatures = strings.TrimSpace(out)
	return nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "termfixture:", err)
	os.Exit(1)
}
