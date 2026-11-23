package contract

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/cmdlang"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/format"
)

// HarnessCaller is the command name of i-mux's e2e test binary: calls from it are the test
// harness's own, not i-mux's.
const HarnessCaller = "e2e.test"

// ExtractDir reads a record directory (calls.jsonl and files/) written by tmuxrec.
func ExtractDir(dir string) (*Recorded, error) {
	imux, harness := NewReport(), NewReport()
	f, err := os.Open(filepath.Join(dir, "calls.jsonl"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	rec := &Recorded{From: "i-mux's e2e suite run with tmuxrec"}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		var c Call
		if err := json.Unmarshal(sc.Bytes(), &c); err != nil {
			return nil, fmt.Errorf("calls.jsonl: %w", err)
		}
		rec.Calls++
		r := imux
		if c.Caller == HarnessCaller {
			r = harness
		}
		r.AddCall(c.Argv)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	files, _ := filepath.Glob(filepath.Join(dir, "files", "*.conf"))
	for _, p := range files {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		rec.Configs++
		imux.AddConfig(string(b))
	}
	rec.Imux = imux
	rec.Harness = harness.Without(imux)
	return rec, nil
}

// AddCall records one tmux command line (without the program name).
func (r *Report) AddCall(argv []string) {
	g := ParseGlobal(argv)
	for _, f := range g.Flags {
		r.CLI.Add(f)
	}
	for _, words := range cmdlang.SplitArgv(g.Rest) {
		args := make([]cmdlang.Arg, len(words))
		for i, w := range words {
			args[i] = cmdlang.Arg{String: w}
		}
		r.addCommand(args, 0)
	}
}

// AddConfig records a config file, every %if branch included.
func (r *Report) AddConfig(text string) {
	r.addSource(text, 0)
}

func (r *Report) addSource(text string, depth int) {
	cmds, err := cmdlang.Parse(text, cmdlang.Options{
		AllBranches: true,
		Cond: func(f string) bool {
			r.addFormat(f)
			return false
		},
	})
	if err != nil {
		r.Unknown.Add("parse error: " + err.Error())
		return
	}
	for _, c := range cmds {
		r.addCommand(c.Args, depth)
	}
}

const maxNesting = 20

// addCommands records an argument that is itself commands: a { } block or a command string.
func (r *Report) addCommands(a cmdlang.Arg, depth int) {
	if a.Block {
		for _, c := range a.Commands {
			r.addCommand(c.Args, depth)
		}
		return
	}
	r.addSource(a.String, depth)
}

// addArgvCommands records commands given as several words (bind-key x send a \; send b).
func (r *Report) addArgvCommands(args []cmdlang.Arg, depth int) {
	if len(args) == 1 {
		r.addCommands(args[0], depth)
		return
	}
	var words []string
	for _, a := range args {
		if a.Block {
			words = append(words, cmdlang.Format(a.Commands))
		} else {
			words = append(words, a.String)
		}
	}
	for _, cmd := range cmdlang.SplitArgv(words) {
		w := make([]cmdlang.Arg, len(cmd))
		for i, s := range cmd {
			w[i] = cmdlang.Arg{String: s}
		}
		r.addCommand(w, depth)
	}
}

func str(a cmdlang.Arg) string {
	if a.Block {
		return cmdlang.Format(a.Commands)
	}
	return a.String
}

var (
	reNumber  = regexp.MustCompile(`^-?[0-9]+$`)
	rePercent = regexp.MustCompile(`^[0-9]+%$`)
	reDecimal = regexp.MustCompile(`^[0-9]*\.[0-9]+$`)
	reIndex   = regexp.MustCompile(`\[[0-9]+\]$`)
	reTime    = regexp.MustCompile(`%[A-Za-z%]`)
	// reVariable matches what a format can look up: a variable, an @option or an option.
	reVariable = regexp.MustCompile(`^[@A-Za-z_][A-Za-z0-9_-]*(\[[0-9]+\])?$`)
)

// shape says what kind of value v is, without the value itself.
func shape(v string) string {
	switch {
	case v == "":
		return "empty"
	case strings.Contains(v, "#{"):
		return "fmt"
	case reNumber.MatchString(v):
		if strings.HasPrefix(v, "-") {
			return "-N"
		}
		return "N"
	case rePercent.MatchString(v):
		return "N%"
	case reDecimal.MatchString(v):
		return "N.N"
	}
	return "str"
}

// sourceTargets are the commands whose -s is a target.
var sourceTargets = map[string]bool{
	"swap-pane": true, "swap-window": true, "join-pane": true, "move-pane": true,
	"move-window": true, "link-window": true, "break-pane": true, "detach-client": true,
}

// clientFlags are the commands whose -c is a client.
var clientFlags = map[string]bool{
	"display-message": true, "display-popup": true, "display-menu": true, "switch-client": true,
	"confirm-before": true,
}

func (r *Report) addCommand(args []cmdlang.Arg, depth int) {
	if depth > maxNesting || len(args) == 0 {
		return
	}
	name := str(args[0])
	spec, ok := lookupCommand(name)
	if !ok {
		r.Unknown.Add("command: " + name)
		return
	}
	cmd := spec.Name
	r.Commands.Add(cmd, "")
	if name != cmd {
		r.Commands.Add(cmd, "alias "+name)
	}
	words := make([]string, len(args)-1)
	for i, a := range args[1:] {
		words[i] = str(a)
	}
	flags, rest, bad := parseFlags(spec.Flags, words)
	restArgs := args[len(args)-len(rest):]
	for _, b := range bad {
		r.Unknown.Add("flag: " + cmd + " " + b)
	}
	has := map[byte]string{}
	for _, f := range flags {
		r.Commands.Add(cmd, "-"+string(f.Name))
		has[f.Name] = f.Value
		_, value, _ := takesValue(spec.Flags, f.Name)
		if !value {
			continue
		}
		r.Values.Add(cmd+" -"+string(f.Name), shape(f.Value))
		r.addFormat(f.Value)
		switch {
		case f.Name == 't':
			r.Targets.Add(targetForm(f.Value))
		case f.Name == 's' && sourceTargets[cmd]:
			r.Targets.Add(targetForm(f.Value))
		case f.Name == 'c' && clientFlags[cmd]:
			r.Targets.Add("client " + clientForm(f.Value))
		}
	}
	_, global := has['g']

	switch cmd {
	case "set-option", "set-window-option", "show-options", "show-window-options":
		if len(rest) == 0 {
			return
		}
		opt := rest[0]
		r.addOption(cmd, opt, has, global)
		if len(rest) > 1 {
			r.addOptionValue(reIndex.ReplaceAllString(opt, ""), rest[1])
		}
		return
	case "set-hook", "show-hooks":
		if len(rest) == 0 {
			return
		}
		hook := reIndex.ReplaceAllString(rest[0], "")
		r.Hooks.Add(hook, "")
		for _, f := range flags {
			r.Hooks.Add(hook, "-"+string(f.Name))
		}
		if reIndex.MatchString(rest[0]) {
			r.Hooks.Add(hook, "[N]")
		}
		if !tmuxHooks[hook] {
			r.Unknown.Add("hook: " + hook)
		}
		if len(restArgs) > 1 {
			r.addCommands(restArgs[1], depth+1)
		}
		return
	case "bind-key", "unbind-key":
		table := "prefix"
		if t, ok := has['T']; ok {
			table = t
		} else if _, ok := has['n']; ok {
			table = "root"
		}
		r.Tables.Add(table)
		if len(rest) > 0 {
			r.Keys.Add(rest[0])
		}
		if cmd == "bind-key" && len(restArgs) > 1 {
			r.addArgvCommands(restArgs[1:], depth+1)
		}
		return
	case "switch-client":
		if t, ok := has['T']; ok {
			r.Tables.Add(t)
		}
	case "send-keys":
		_, literal := has['l']
		_, copyCmd := has['X']
		switch {
		case copyCmd && len(rest) > 0:
			r.CopyCommands.Add(rest[0])
			for _, a := range rest[1:] {
				r.addFormat(a)
			}
		case literal:
		default:
			for _, k := range rest {
				if isKeyName(k) {
					r.SentKeys.Add(k)
				} else {
					r.SentKeys.Add("(text)")
				}
			}
		}
		return
	case "if-shell":
		if len(rest) > 0 {
			r.addFormat(rest[0]) // a format with -F; tmux expands a shell condition too
		}
		for _, a := range restArgs[min(1, len(restArgs)):] {
			r.addCommands(a, depth+1)
		}
		return
	case "run-shell":
		if _, ok := has['C']; ok {
			for _, a := range restArgs {
				r.addCommands(a, depth+1)
			}
			return
		}
	case "confirm-before":
		for _, a := range restArgs {
			r.addCommands(a, depth+1)
		}
		return
	case "command-prompt":
		for _, a := range restArgs {
			// %% and %1 are filled in when the prompt is answered.
			r.addSource(strings.NewReplacer("%%%", "x", "%%", "x", "%1", "x").Replace(str(a)), depth+1)
		}
		return
	case "display-menu":
		for i := 0; i < len(restArgs); i++ {
			item := str(restArgs[i])
			if item == "" || item == "-" {
				r.Commands.Add(cmd, "item separator")
				continue
			}
			if strings.HasPrefix(item, "-") {
				r.Commands.Add(cmd, "item disabled")
			}
			r.addFormat(item)
			if i+2 < len(restArgs) {
				r.addCommands(restArgs[i+2], depth+1)
			}
			i += 2
		}
		return
	case "source-file":
		return
	}
	for _, a := range rest {
		r.addFormat(a)
	}
}

// addOption records an option name with the scope it is set or read in.
func (r *Report) addOption(cmd, opt string, has map[byte]string, global bool) {
	base := reIndex.ReplaceAllString(opt, "")
	array := base != opt
	_, s := has['s']
	_, w := has['w']
	_, p := has['p']
	if cmd == "set-window-option" || cmd == "show-window-options" {
		w = true
	}
	scope := ""
	switch {
	case s:
		scope = "server"
	case p:
		scope = "pane"
	case w:
		scope = "window"
	case strings.HasPrefix(base, "@"):
		scope = "session"
	default:
		scope = tmuxOptions[base]
		if scope == "" {
			scope = engineOptions[base]
		}
		switch scope {
		case "":
			r.Unknown.Add("option: " + base)
			scope = "?"
		case "window|pane":
			scope = "window"
		}
	}
	if global && scope != "server" {
		scope = "global " + scope
	}
	verb := "set"
	if strings.HasPrefix(cmd, "show") {
		verb = "show"
	}
	entry := verb + " " + scope
	if array {
		entry += " [N]"
	}
	for _, f := range "aquoF" {
		if _, ok := has[byte(f)]; ok && verb == "set" {
			r.addFlagNote(base, "-"+string(f))
		}
	}
	if strings.HasPrefix(base, "@") {
		r.UserOptions.Add(userOptionName(base), entry)
		return
	}
	r.Options.Add(base, entry)
}

func (r *Report) addFlagNote(opt, flag string) {
	if strings.HasPrefix(opt, "@") {
		r.UserOptions.Add(userOptionName(opt), flag)
		return
	}
	r.Options.Add(opt, flag)
}

// userOptionName folds families of user options: i-mux's theme colours (@imux_c_*) and
// per-client options keyed by a client name.
func userOptionName(opt string) string {
	if strings.HasPrefix(opt, "@imux_c_") {
		return "@imux_c_*"
	}
	return opt
}

// isStyleOption reports whether opt's value is a style.
func isStyleOption(opt string) bool {
	return strings.HasSuffix(opt, "-style")
}

func (r *Report) addOptionValue(opt, value string) {
	switch {
	case opt == "pane-colours":
		r.Styles.Add("pane-colours=" + colourForm(value))
	case isStyleOption(opt):
		r.addStyle(value)
	}
	r.addFormat(value)
	if strings.Contains(opt, "format") || strings.HasPrefix(opt, "status-") || opt == "set-titles-string" {
		for _, m := range reTime.FindAllString(value, -1) {
			r.Strftime.Add(m)
		}
	}
}

// addFormat records the variables, modifiers, aliases, styles and jobs in s.
func (r *Report) addFormat(s string) {
	if !strings.Contains(s, "#") {
		return
	}
	format.Walk(s, func(kind, text string, args []string) {
		switch kind {
		case format.NodeVariable:
			if !reVariable.MatchString(text) {
				return // not a name: a regular expression in Go source, say
			}
			if strings.HasPrefix(text, "@") {
				r.Variables.Add(userOptionName(text))
				r.UserOptions.Add(userOptionName(text), "read in a format")
				return
			}
			r.Variables.Add(text)
		case format.NodeModifier:
			r.Modifiers.Add(modifierName(text, args))
		case format.NodeAlias:
			r.Aliases.Add(text)
		case format.NodeStyle:
			r.addStyle(text)
		case format.NodeJob:
			r.Modifiers.Add("#()")
		}
	})
}

// modifierName names a modifier with the arguments that change what it does.
func modifierName(name string, args []string) string {
	switch name {
	case "e":
		if len(args) == 0 {
			return "e"
		}
		n := "e|" + args[0]
		if len(args) > 1 {
			n += "|" + args[1]
		}
		return n
	case "p", "=":
		if len(args) == 0 {
			return name
		}
		a := args[0]
		switch {
		case strings.Contains(a, "#{"):
			return name + "/fmt/"
		case strings.HasPrefix(a, "-"):
			return name + "-N"
		}
		if len(args) > 1 {
			return name + "/N/marker"
		}
		return name + "N"
	case "m", "t", "q", "S", "W", "P", "L", "N", "C":
		if len(args) > 0 {
			return name + "/" + args[0]
		}
	}
	return name
}

// addStyle records the keywords of a style: "fg=red,bold" gives "fg=colour" and "bold".
func (r *Report) addStyle(s string) {
	for _, tok := range strings.FieldsFunc(s, func(c rune) bool { return c == ',' || c == ' ' }) {
		key, value, ok := strings.Cut(tok, "=")
		if !ok {
			r.Styles.Add(tok)
			continue
		}
		switch key {
		case "fg", "bg", "us", "fill":
			if value != "" { // "" is a string cut short in Go source: "#[fg=" + c
				r.Styles.Add(key + "=" + colourForm(value))
			}
		case "range":
			kind, _, _ := strings.Cut(value, "|")
			r.Styles.Add("range=" + kind)
		default:
			r.Styles.Add(key + "=" + value)
		}
	}
}

var reColourN = regexp.MustCompile(`^colou?r[0-9]+$`)

func colourForm(v string) string {
	switch {
	case strings.Contains(v, "#{"):
		return "fmt"
	case strings.HasPrefix(v, "#"):
		return "#rrggbb"
	case reColourN.MatchString(v):
		return "colourN"
	case v == "default" || v == "terminal":
		return v
	}
	return "name"
}

var (
	reSessionWindow = regexp.MustCompile(`^=?[^:.]*:[^.]*$`)
	reFull          = regexp.MustCompile(`^=?[^:.]*:[^.]*\.[^.]*$`)
)

// targetForm says which form a target takes: %N, @N, $N, =name, name:index …
func targetForm(v string) string {
	switch {
	case v == "":
		return "empty"
	case v == "=":
		return "= (mouse)"
	case strings.Contains(v, "#{"):
		return "fmt"
	case strings.HasPrefix(v, "{") && strings.HasSuffix(v, "}"):
		return v
	case len(v) > 1 && strings.IndexByte("%@$", v[0]) >= 0 && reNumber.MatchString(v[1:]):
		return string(v[0]) + "N"
	case strings.HasPrefix(v, "/dev/"):
		return "tty"
	case reFull.MatchString(v):
		if strings.HasPrefix(v, "=") {
			return "=session:window.pane"
		}
		return "session:window.pane"
	case reSessionWindow.MatchString(v):
		if strings.HasPrefix(v, "=") {
			return "=session:window"
		}
		return "session:window"
	case strings.HasPrefix(v, "="):
		return "=name"
	}
	return "name"
}

func clientForm(v string) string {
	switch {
	case strings.HasPrefix(v, "/dev/"):
		return "tty"
	case strings.Contains(v, "#{"):
		return "fmt"
	case v == "":
		return "empty"
	}
	return "name"
}

var reKey = regexp.MustCompile(`^((C|M|S)-)*([A-Za-z][A-Za-z0-9]*|[^A-Za-z0-9 ]|[0-9])$`)

// isKeyName reports whether s looks like a tmux key name rather than text to type.
func isKeyName(s string) bool {
	if !reKey.MatchString(s) {
		return false
	}
	base := s
	for len(base) > 2 && (strings.HasPrefix(base, "C-") || strings.HasPrefix(base, "M-") || strings.HasPrefix(base, "S-")) {
		base = base[2:]
	}
	if len(base) == 1 {
		return true
	}
	return keyNames[base] || strings.HasPrefix(base, "F") && reNumber.MatchString(base[1:]) ||
		strings.HasPrefix(base, "Mouse") || strings.HasPrefix(base, "Wheel") ||
		strings.HasPrefix(base, "KP")
}

var keyNames = map[string]bool{
	"Enter": true, "Escape": true, "Tab": true, "BTab": true, "Space": true, "BSpace": true,
	"Up": true, "Down": true, "Left": true, "Right": true, "Home": true, "End": true,
	"PageUp": true, "PageDown": true, "PgUp": true, "PgDn": true, "NPage": true, "PPage": true,
	"IC": true, "DC": true, "Insert": true, "Delete": true, "Any": true,
}
