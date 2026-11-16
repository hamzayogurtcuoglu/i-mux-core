//go:build difftest

package difftest

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Keys into panes (TASKS.md C2.11): each key is sent with send-keys to the fake TUI on both
// engines, and what reaches the program is compared, in normal and application modes, with
// extended keys off and on (modes 1 and 2, both formats).

// sentKeys are the keys of COMPATIBILITY.md §7 and more of each kind.
var sentKeys = []string{
	"a", "b", "c", "h", "j", "k", "l", "q", "r", "v", "H", "J", "K", "L", "S", "-", ",", "?", "[", "]", "g",
	"C-p", "C-v", "C-\\", "M-v", "Left", "Right", "Up", "Down", "S-Left", "S-Right", "S-Up", "S-Down",
	"Enter", "Escape", "C-Enter", "S-Enter", "M-Enter", "Tab", "BTab", "C-Tab", "S-Tab", "BSpace",
	"M-BSpace", "C-BSpace", "Space", "C-Space", "C-a", "C-M-a", "M-a", "M-A", "C-S-a", "C-1", "C-2",
	"C-/", "C-?", "C-@", "C-[", "C-Up", "M-Left", "C-S-Right", "F1", "F5", "C-F5", "S-F1", "M-F12",
	"Home", "End", "C-Home", "PPage", "NPage", "IC", "DC", "C-DC", "KP5", "KPEnter", "KP/", "é",
	"C-é", "M-é", "日",
}

// keyVariants are the pane modes and server options the keys are sent with.
var keyVariants = []struct {
	name, conf, init string
}{
	{"normal", "", ""},
	{"application", "", `\x1b[?1h\x1b=`},
	{"extended off, mode 2 asked", "", `\x1b[>4;2m`},
	{"extended on, mode 1", "set -s extended-keys on\n", `\x1b[>4;1m`},
	{"extended on, mode 2", "set -s extended-keys on\n", `\x1b[>4;2m`},
	{"extended on, mode 2, csi-u", "set -s extended-keys on\nset -s extended-keys-format csi-u\n", `\x1b[>4;2m`},
	{"extended always", "set -s extended-keys always\n", ``},
}

// marker follows each key so that its end is known: no key here sends it.
const marker = "§"

// sendKeysRun sends every key on one engine for one variant and returns what the pane read for
// each, in hex.
func sendKeysRun(t *testing.T, eng *Binary, tui, conf, init string) []string {
	t.Helper()
	env, err := NewEnv()
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(env.Dir)
	defer eng.Stop(env)
	if conf != "" {
		env.Config = filepath.Join(env.Dir, "keys.conf")
		if err := os.WriteFile(env.Config, []byte(conf), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	out := filepath.Join(env.Dir, "in")
	ctx := context.Background()
	cmd := "exec " + tui + " -mode keys -out " + out + " -init '" + init + "'"
	if r := eng.Run(ctx, env, []string{"new-session", "-d", "-x", "80", "-y", "24", cmd}); r.Code != 0 {
		t.Fatalf("%s: new-session: %+v", eng.Name(), r)
	}
	read := func() string {
		b, _ := os.ReadFile(out)
		return strings.ReplaceAll(string(b), "\n", "")
	}
	deadline := time.Now().Add(WaitTimeout)
	for {
		if _, err := os.Stat(out); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: the fake TUI did not start", eng.Name())
		}
		time.Sleep(5 * time.Millisecond)
	}
	markerHex := "c2a7"
	var got []string
	seen := 0
	for _, k := range sentKeys {
		if r := eng.Run(ctx, env, []string{"send-keys", "-t", "%0", k, marker}); r.Code != 0 {
			t.Fatalf("%s: send-keys %s: %+v", eng.Name(), k, r)
		}
		deadline := time.Now().Add(5 * time.Second)
		all := read()
		for !strings.HasSuffix(all, markerHex) || len(all) <= seen {
			if time.Now().After(deadline) {
				t.Fatalf("%s: %s: no marker after %q", eng.Name(), k, all[seen:])
			}
			time.Sleep(2 * time.Millisecond)
			all = read()
		}
		got = append(got, strings.TrimSuffix(all[seen:], markerHex))
		seen = len(all)
	}
	return got
}

func TestKeysIntoPanes(t *testing.T) {
	ref, core := tmuxEngine(t), coreEngine(t)
	dir := t.TempDir()
	tui := filepath.Join(dir, "faketui")
	if out, err := exec.Command("go", "build", "-o", tui, "../../testdata/faketui").CombinedOutput(); err != nil {
		t.Fatalf("faketui: %v: %s", err, out)
	}
	for _, v := range keyVariants {
		t.Run(v.name, func(t *testing.T) {
			a := sendKeysRun(t, ref, tui, v.conf, v.init)
			b := sendKeysRun(t, core, tui, v.conf, v.init)
			var log strings.Builder
			for i, k := range sentKeys {
				if a[i] != b[i] {
					t.Errorf("%s: tmux sends %s, imux-core %s", k, a[i], b[i])
				}
				log.WriteString(k + "=" + a[i] + " ")
			}
			t.Log(strconv.Quote(log.String()))
		})
	}
}
