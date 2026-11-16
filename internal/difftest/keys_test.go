//go:build difftest

package difftest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/input"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/pty"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/termout"
)

// keyCases are bytes a terminal sends for one key, with the name tmux gives it.
var keyCases = []struct{ in, name string }{
	{"a", "a"},
	{"A", "A"},
	{"é", "é"},
	{"\x01", "C-a"},
	{"\x1a", "C-z"},
	{"\x00", "C-Space"},
	{"\t", "Tab"},
	{"\r", "Enter"},
	{"\n", "C-j"},
	{"\x08", "C-h"},
	{"\x7f", "BSpace"},
	{"\x1c", "C-\\"},
	{"\x10", "C-p"},
	{"\x16", "C-v"},
	{"\x1b", "Escape"},
	{"\x1ba", "M-a"},
	{"\x1bv", "M-v"},
	{"\x1b\x01", "C-M-a"},
	{"\x1b\x7f", "M-BSpace"},
	{"\x1b[", "M-["},
	{"\x1b[A", "Up"},
	{"\x1bOA", "Up"},
	{"\x1b[D", "Left"},
	{"\x1b[1;2A", "S-Up"},
	{"\x1b[1;3A", "M-Up"},
	{"\x1b[1;5A", "C-Up"},
	{"\x1b[1;6D", "C-S-Left"},
	{"\x1b[1;2C", "S-Right"},
	{"\x1b\x1b[A", "M-Up"},
	{"\x1b[H", "Home"},
	{"\x1b[F", "End"},
	{"\x1b[1;5H", "C-Home"},
	{"\x1b[2~", "IC"},
	{"\x1b[3~", "DC"},
	{"\x1b[5~", "PPage"},
	{"\x1b[6~", "NPage"},
	{"\x1b[3;5~", "C-DC"},
	{"\x1b[5;2~", "S-PPage"},
	{"\x1b[Z", "BTab"},
	{"\x1bOP", "F1"},
	{"\x1b[15~", "F5"},
	{"\x1b[24~", "F12"},
	{"\x1b[1;2P", "S-F1"},
	{"\x1b[15;5~", "C-F5"},
	{"\x1b[24;3~", "M-F12"},
	{"\x1bOo", "KP/"},
	{"\x1bOM", "KPEnter"},
	{"\x1b[13;2u", "S-Enter"},
	{"\x1b[13;5u", "C-Enter"},
	{"\x1b[27;5;118~", "C-v"},
	{"\x1b[27;2;13~", "S-Enter"},
	{"\x1b[118;5u", "C-v"},
	{"\x1b[97;3u", "M-a"},
	{"\x1b[9;2u", "BTab"},
	{"\x1b[127;5u", "C-BSpace"},
	{"\x1b[233;5u", "C-é"},
}

// TestKeyNamesAgainstTmux checks the key table against tmux 3.8: every name is bound in tmux's
// root table to set an option, the bytes are typed into an attached client, and the binding
// that fires is the key tmux read. internal/input must read the same key.
func TestKeyNamesAgainstTmux(t *testing.T) {
	ref := tmuxEngine(t)
	env, err := NewEnv()
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(env.Dir)
	defer ref.Stop(env)
	var conf strings.Builder
	for i, c := range keyCases {
		fmt.Fprintf(&conf, "bind -n '%s' set -g @k %d\n", c.name, i)
	}
	env.Config = filepath.Join(env.Dir, "keys.conf")
	if err := os.WriteFile(env.Config, []byte(conf.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if r := ref.Run(ctx, env, []string{"new-session", "-d", "-x", "80", "-y", "24", "exec cat"}); r.Code != 0 {
		t.Fatalf("new-session: %+v", r)
	}
	vars := append(append([]string(nil), env.Vars...), "TERMINFO="+ClientTerminfo)
	p, err := pty.New().Start(pty.Spec{
		Path: ref.Path, Argv: []string{"tmux", "-S", env.Socket, "-f", env.Config, "-u", "attach"},
		Env: vars, Dir: env.Dir, Cols: 80, Rows: 24,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Kill(); _, _ = p.Wait(); _ = p.Close() }()
	go func() {
		b := make([]byte, 4096)
		for {
			if _, err := p.Read(b); err != nil {
				return
			}
		}
	}()
	option := func() string {
		r := ref.Run(ctx, env, []string{"show", "-gqv", "@k"})
		return strings.TrimSpace(r.Stdout)
	}
	deadline := time.Now().Add(WaitTimeout)
	for ref.Run(ctx, env, []string{"list-clients"}).Stdout == "" {
		if time.Now().After(deadline) {
			t.Fatal("the client did not attach")
		}
		time.Sleep(20 * time.Millisecond)
	}

	ours := ourParser(t)
	for _, c := range keyCases {
		ref.Run(ctx, env, []string{"set", "-g", "@k", "none"})
		if _, err := p.Write([]byte(c.in)); err != nil {
			t.Fatal(err)
		}
		got := "none"
		deadline := time.Now().Add(2 * time.Second)
		for got == "none" && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
			got = option()
		}
		// A name bound twice keeps its last binding: compare names, not cases.
		name := "nothing bound"
		if n, err := strconv.Atoi(got); err == nil {
			name = keyCases[n].name
		}
		if name != c.name {
			t.Errorf("%q: tmux read %s, the table says %s", c.in, name, c.name)
		}
		if name := ours(c.in); name != c.name {
			t.Errorf("%q: imux-core reads %q, tmux %q", c.in, name, c.name)
		}
	}
}

// ourParser reads keys as an xterm-256color client of i-mux-core would, its erase DEL.
func ourParser(t *testing.T) func(string) string {
	b, err := os.ReadFile("../../testdata/term/xterm-256color.json")
	if err != nil {
		t.Fatal(err)
	}
	var rec struct{ Caps []string }
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatal(err)
	}
	caps := rec.Caps
	term, err := termout.NewTerm("xterm-256color", caps, &termout.Features{}, termout.DefaultOptions)
	if err != nil {
		t.Fatal(err)
	}
	return func(in string) string {
		p := input.New(term, 0x7f)
		p.StopWaiting()
		evs := p.Feed([]byte(in))
		if _, ok := p.Delay(); ok {
			evs = append(evs, p.Expire()...)
		}
		var names []string
		for _, ev := range evs {
			names = append(names, ev.Key.String())
		}
		return strings.Join(names, " ")
	}
}
