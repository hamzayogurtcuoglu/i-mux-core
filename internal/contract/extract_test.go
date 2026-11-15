package contract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// writeRecord writes a synthetic record directory, as tmuxrec would.
func writeRecord(t *testing.T, calls []Call, configs ...string) string {
	t.Helper()
	dir := t.TempDir()
	var b strings.Builder
	for _, c := range calls {
		j, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(append(j, '\n'))
	}
	if err := os.WriteFile(filepath.Join(dir, "calls.jsonl"), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "files"), 0o700); err != nil {
		t.Fatal(err)
	}
	for i, c := range configs {
		p := filepath.Join(dir, "files", string(rune('a'+i))+".conf")
		if err := os.WriteFile(p, []byte(c), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const syntheticConf = `
set -g mouse on
set -as terminal-features ',*:RGB'
set -g status-format[0] '#[align=left range=window|#{window_index}]#{W:#I,#[bold]#S} %H:%M'
set -g pane-border-style 'fg=colour8,bg=default'
%if "#{>=:#{version},3.4}"
set -gw pane-border-status top
%endif
set-hook -g pane-died 'run -b "imux __died #{pane_id} #{pane_dead_status}"'
bind -n MouseDown1Status { switch-client -t = ; select-window -t = }
bind -T imux-resize l { resize-pane -R 2 ; switch-client -T imux-resize }
bind x display-menu -T Title -x '#{e|+:#{popup_pane_left},2}' Copy c 'send -X copy-pipe-and-cancel' '' -Gone g ''
unbind -q -T copy-mode-vi y
`

func TestExtract(t *testing.T) {
	dir := writeRecord(t, []Call{
		{Argv: []string{"-L", "imux", "-f", "/x.conf", "-u", "new-session", "-d", "-s", "p", "-P", "-F", "#{pane_id}", ";", "set", "-p", "-t", "%1", "@imux_role", "agent"}, Caller: "imux"},
		{Argv: []string{"-L", "imux", "-u", "split-window", "-h", "-l", "30%", "-t", "%1", "sh"}, Caller: "imux"},
		{Argv: []string{"-L", "imux", "-u", "send-keys", "-t", "=p:0.1", "C-c", "hello world", "Enter"}, Caller: "imux"},
		{Argv: []string{"-L", "imux", "-u", "display-message", "-p", "-c", "/dev/ttys001", "#{?#{==:#{@imux_c_bg},x},a,b}"}, Caller: "imux"},
		{Argv: []string{"-L", "imux", "-u", "show-options", "-gqv", "prefix"}, Caller: "imux"},
		{Argv: []string{"-L", "imux", "-u", "bogus-command"}, Caller: "imux"},
		{Argv: []string{"-L", "imux", "capture-pane", "-p", "-t", "%3"}, Caller: HarnessCaller},
		{Argv: []string{"-L", "imux", "new-session", "-d", "-s", "p"}, Caller: HarnessCaller},
	}, syntheticConf)
	rec, err := ExtractDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Calls != 8 || rec.Configs != 1 {
		t.Fatalf("calls %d configs %d", rec.Calls, rec.Configs)
	}
	r := rec.Imux
	has := func(what string, s Set, items ...string) {
		t.Helper()
		for _, i := range items {
			if !s[i] {
				t.Errorf("%s: missing %q in %q", what, i, s.Sorted())
			}
		}
	}
	has("cli", r.CLI, "-L", "-f", "-u")
	has("new-session", r.Commands["new-session"], "-d", "-s", "-P", "-F")
	has("set-option", r.Commands["set-option"], "alias set", "-p", "-t", "-g", "-a", "-s")
	has("split-window", r.Commands["split-window"], "-h", "-l", "-t")
	has("split -l", r.Values["split-window -l"], "N%")
	has("variables", r.Variables, "pane_id", "window_index", "version", "pane_dead_status", "popup_pane_left", "@imux_c_*")
	has("modifiers", r.Modifiers, "W", ">=", "e|+", "==")
	has("aliases", r.Aliases, "#I", "#S")
	has("styles", r.Styles, "align=left", "range=window", "bold", "fg=colourN", "bg=default")
	has("strftime", r.Strftime, "%H", "%M")
	has("mouse option", r.Options["mouse"], "set global session")
	has("terminal-features", r.Options["terminal-features"], "set server", "-a")
	has("status-format", r.Options["status-format"], "set global session [N]")
	has("pane-border-status", r.Options["pane-border-status"], "set global window")
	has("prefix", r.Options["prefix"], "show global session")
	has("user option", r.UserOptions["@imux_role"], "set pane")
	has("hooks", r.Hooks["pane-died"], "-g")
	has("tables", r.Tables, "root", "imux-resize", "prefix", "copy-mode-vi")
	has("keys", r.Keys, "MouseDown1Status", "l", "x", "y")
	has("sent keys", r.SentKeys, "C-c", "Enter", "(text)")
	has("copy commands", r.CopyCommands, "copy-pipe-and-cancel")
	has("targets", r.Targets, "%N", "= (mouse)", "=session:window.pane", "client tty")
	has("menu", r.Commands["display-menu"], "-T", "-x", "item separator", "item disabled")
	has("run-shell from a hook", r.Commands["run-shell"], "-b")
	has("unknown", r.Unknown, "command: bogus-command")
	if r.Commands["capture-pane"] != nil {
		t.Errorf("the harness's capture-pane counted as i-mux's")
	}
	has("harness", rec.Harness.Commands["capture-pane"], "-p", "-t")
	if rec.Harness.Commands["new-session"] != nil {
		t.Errorf("harness_only repeats what i-mux uses: %v", rec.Harness.Commands["new-session"])
	}
}

func TestDiff(t *testing.T) {
	rec := &Recorded{Imux: NewReport(), Harness: NewReport()}
	rec.Imux.Commands.Add("new-session", "-d")
	rec.Imux.Variables.Add("pane_id")
	rec.Harness.Commands.Add("capture-pane", "-p")
	exp := NewReport()
	exp.Commands.Add("new-session", "-d")
	exp.Commands.Add("kill-server", "")
	missing, unused := Diff(rec, exp)
	want := []string{"command: capture-pane", "command: capture-pane -p", "variable: pane_id"}
	if !reflect.DeepEqual(missing, want) {
		t.Errorf("missing = %q, want %q", missing, want)
	}
	if !reflect.DeepEqual(unused, []string{"command: kill-server"}) {
		t.Errorf("unused = %q", unused)
	}
}

func TestRecord(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "tmux.conf")
	if err := os.WriteFile(conf, []byte("set -g mouse on\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := ShimConfig{Tmux: "/bin/false", Out: filepath.Join(dir, "rec")}
	if err := Record(cfg, []string{"-L", "x", "-f", conf, "new", ";", "source-file", "-q", conf, filepath.Join(dir, "missing.conf")}, os.Getpid()); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(cfg.Out, "calls.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var c Call
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	if len(c.Files) != 1 || c.Files[conf] == "" {
		t.Fatalf("files = %v", c.Files)
	}
	// The caller is named where ps(1) can name processes (not on Windows).
	if c.Caller == "" && commandName(os.Getpid()) != "" {
		t.Fatal("no caller")
	}
	copied, err := os.ReadFile(filepath.Join(cfg.Out, "files", c.Files[conf]))
	if err != nil || string(copied) != "set -g mouse on\n" {
		t.Fatalf("copy = %q, %v", copied, err)
	}
}

func TestTargetForm(t *testing.T) {
	for v, want := range map[string]string{
		"%12": "%N", "@3": "@N", "$0": "$N", "=proj": "=name", "proj": "name",
		"proj:1": "session:window", "=proj:1.2": "=session:window.pane", "=": "= (mouse)",
		"#{pane_id}": "fmt", "{mouse}": "{mouse}", "/dev/ttys003": "tty",
	} {
		if got := targetForm(v); got != want {
			t.Errorf("targetForm(%q) = %q, want %q", v, got, want)
		}
	}
}

func TestLookupCommand(t *testing.T) {
	for in, want := range map[string]string{
		"set": "set-option", "send": "send-keys", "new-session": "new-session", "display": "display-message",
		"splitw": "split-window", "run": "run-shell", "if": "if-shell", "list-p": "list-panes",
	} {
		c, ok := lookupCommand(in)
		if !ok || c.Name != want {
			t.Errorf("lookupCommand(%q) = %q, %v; want %q", in, c.Name, ok, want)
		}
	}
	if _, ok := lookupCommand("list"); ok {
		t.Error(`"list" is ambiguous`)
	}
}

func TestParseFlags(t *testing.T) {
	spec := "D::L::t:x:Z"
	flags, args, bad := parseFlags(spec, []string{"-D", "5", "-L", "-t", "%1", "-Zq", "rest"})
	want := []Flag{{Name: 'D', Value: "5"}, {Name: 'L'}, {Name: 't', Value: "%1"}, {Name: 'Z'}}
	if !reflect.DeepEqual(flags, want) || !reflect.DeepEqual(args, []string{"rest"}) || !reflect.DeepEqual(bad, []string{"-q"}) {
		t.Fatalf("got %+v %q %q", flags, args, bad)
	}
	flags, args, _ = parseFlags(spec, []string{"-D3", "-x", "-1", "--", "-t"})
	want = []Flag{{Name: 'D', Value: "3"}, {Name: 'x', Value: "-1"}}
	if !reflect.DeepEqual(flags, want) || !reflect.DeepEqual(args, []string{"-t"}) {
		t.Fatalf("got %+v %q", flags, args)
	}
}

func TestScanSource(t *testing.T) {
	dir := t.TempDir()
	src := `package x

import "tmux"

const moved = "set-option -gu @imux_moved"

func f(client, w string) {
	c := tmux.Cmd{"display-popup"}
	if client != "" {
		c = append(c, "-c", client)
	}
	_ = append(c, "-E", "-w", w, "prog")
	_ = tmux.Cmd{"send", "-t", "%1", "#{pane_id}"}
	d := "display-menu"
	d += " -T " + w + " -x 0"
	_ = "kill-pane -t " + client
	_ = "not a command"
}
`
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	// Tests are not scanned.
	if err := os.WriteFile(filepath.Join(dir, "x_test.go"), []byte("package x\nvar _ = Cmd{\"kill-server\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := ScanSource(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{
		"display-popup": {"-E", "-c", "-w"},
		"send-keys":     {"-t", "alias send"},
		"display-menu":  {"-T", "-x"},
		"kill-pane":     {"-t"},
		"set-option":    {"-g", "-u"},
	}
	for cmd, flags := range want {
		if got := r.Commands[cmd].Sorted(); !reflect.DeepEqual(got, flags) {
			t.Errorf("%s: %q, want %q", cmd, got, flags)
		}
	}
	if r.Commands["kill-server"] != nil {
		t.Error("a test file was scanned")
	}
	if !r.Variables["pane_id"] {
		t.Error("format in a Cmd literal not seen")
	}
}
