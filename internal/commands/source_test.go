package commands

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/cmdlang"
)

// The config files in testdata/cmdlang, parsed as `source-file -nv` parses them, against what
// tmux 3.8 printed for them (the .golden files, written by cmd/parsefixture): each line's
// commands as built, and the errors with their file and line (TASKS.md C3.1).

// fixtureHome stands for the temporary HOME of cmd/parsefixture, written $HOME in the files.
const fixtureHome = "/tmp/imux-pf-home"

// fixtureEnv is cmd/parsefixture's environment.
var fixtureEnv = map[string]string{
	"PATH": "/usr/bin:/bin", "SHELL": "/bin/sh", "TERM": "xterm-256color", "LANG": "C.UTF-8",
	"FOO": "bar", "EMPTY": "", "SPACED": "a b", "HOME": fixtureHome, "TMUX_TMPDIR": fixtureHome,
}

// sourceVerbose is what `source-file -nv` prints for src.
func sourceVerbose(src string) string {
	var out strings.Builder
	code := 0
	opts := cmdlang.Options{
		Env: func(name string) (string, bool) {
			v, ok := fixtureEnv[name]
			return v, ok
		},
		Home: func(user string) (string, bool) {
			if user == "" {
				return fixtureHome, true
			}
			return "", false
		},
		// tmux expands the condition as a format; the files use literal ones.
		Cond: func(format string) bool { return format != "" && format != "0" },
	}
	cmds, err := cmdlang.Parse(src, opts)
	if err != nil {
		var pe *cmdlang.Error
		if errors.As(err, &pe) {
			fmt.Fprintf(&out, "<conf>:%d: %s\n", pe.Line, pe.Msg)
		} else {
			fmt.Fprintf(&out, "<conf>: %v\n", err)
		}
		code = 1
	} else {
		b := &Builder{File: "<conf>", Aliases: DefaultAliases, Parse: opts, Verbose: func(s string) {
			out.WriteString(s + "\n")
		}}
		if _, err := b.Build(cmds); err != nil {
			out.WriteString(err.Error() + "\n")
			code = 1
		}
	}
	text := strings.ReplaceAll(out.String(), fixtureHome, "$HOME")
	return fmt.Sprintf("stdout:\n%sstderr:\nexit %d\n", text, code)
}

func TestSourceFixtures(t *testing.T) {
	files, err := filepath.Glob("../../testdata/cmdlang/*.conf")
	if err != nil || len(files) == 0 {
		t.Fatalf("no files: %v", err)
	}
	// IMUX_CMDLANG_EXTRA names another directory of .conf and .golden files, such as the
	// configs i-mux generated in the contract run (C0.3), recorded with parsefixture -dir.
	if dir := os.Getenv("IMUX_CMDLANG_EXTRA"); dir != "" {
		more, _ := filepath.Glob(filepath.Join(dir, "*.conf"))
		files = append(files, more...)
	}
	for _, f := range files {
		t.Run(strings.TrimSuffix(filepath.Base(f), ".conf"), func(t *testing.T) {
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(strings.TrimSuffix(f, ".conf") + ".golden")
			if err != nil {
				t.Fatal(err)
			}
			if got := sourceVerbose(string(src)); got != string(want) {
				t.Errorf("tmux:\n%s\nimux-core:\n%s", want, got)
			}
		})
	}
}

// A config warns once for each command it uses that the engine does not implement yet, and
// the rest of it still loads (COMPATIBILITY.md §10).
func TestConfigWarnsOnce(t *testing.T) {
	h := newFakeHost()
	causes := RunConfig(h, "/l.conf", "clock-mode\nset -g @a 1\nclock-mode\nset -g @b 2\n", cmdlang.Options{})
	if len(causes) != 1 || causes[0] != "/l.conf:1: clock-mode: not implemented by imux-core yet" {
		t.Errorf("causes %q", causes)
	}
	if h.tree.Options.Session.GetString("@a") != "1" || h.tree.Options.Session.GetString("@b") != "2" {
		t.Error("the rest of the config did not load")
	}
}
