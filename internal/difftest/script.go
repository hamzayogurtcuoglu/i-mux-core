package difftest

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// A script is a list of steps, one per line of a testdata/diff/*.txt file:
//
//	# a comment
//	conf <file>                  the config file (-f), relative to the script; before any run
//	file <name> <file>           copy a file (relative to the script) into the run's
//	                             directory, the commands' working directory, as name
//	size WxH                     the size of sessions made with new-session (default 80x24)
//	normalize <regex> <repl>     rewrite later outputs (ids, pids, times, paths) on both engines
//	skip-if-engine <name> <why>  skip the script when the engine under test is <name>
//	run <command line>           run a command; compare stdout, stderr and exit code
//	wait [-t <target>] <format> == <value>
//	                             poll display-message -p until the format gives the value
//	capture <pane>               compare capture-pane -p -e of a pane
//	screen WxH                   attach a client of WxH in a PTY and compare what its terminal
//	                             shows once it has settled (screen.go)
//	client NAME WxH              attach a client of WxH that stays attached
//	resize NAME WxH              resize that client's terminal
//	screen NAME                  compare what that client's terminal shows
//
// Words are split as a POSIX shell does: '…' is literal, "…" takes \" \\ \$ and \` escapes, and
// \ escapes one character outside quotes. A lone ; is a word, as on tmux's command line.
type Script struct {
	Path  string
	Steps []Step
}

// Step is one line of a script.
type Step struct {
	Line int
	Kind string   // "run", "conf", "file", "size", "normalize", "skip-if-engine", "wait", "capture", "screen", "client", "resize"
	Args []string // the words after the kind
	Text string   // the line as written, for messages
}

var reSize = regexp.MustCompile(`^([0-9]+)x([0-9]+)$`)

// ParseScript reads a script file.
func ParseScript(path string) (*Script, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	s := &Script{Path: path}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	n := 0
	ran := false
	clients := map[string]bool{}
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		words, err := SplitWords(line)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, n, err)
		}
		st := Step{Line: n, Kind: words[0], Args: words[1:], Text: line}
		bad := func(format string, a ...any) error {
			return fmt.Errorf("%s:%d: %s: %s", path, n, st.Kind, fmt.Sprintf(format, a...))
		}
		switch st.Kind {
		case "run":
			if len(st.Args) == 0 {
				return nil, bad("no command")
			}
			ran = true
		case "conf":
			if len(st.Args) != 1 {
				return nil, bad("want one file")
			}
			if ran {
				return nil, bad("must come before the first run")
			}
		case "file":
			if len(st.Args) != 2 {
				return nil, bad("want a name and a file")
			}
		case "size":
			if len(st.Args) != 1 || !reSize.MatchString(st.Args[0]) {
				return nil, bad("want WxH")
			}
		case "screen":
			if len(st.Args) != 1 {
				return nil, bad("want WxH or a client")
			}
			if !reSize.MatchString(st.Args[0]) && !clients[st.Args[0]] {
				return nil, bad("no client %s", st.Args[0])
			}
		case "client", "resize":
			if len(st.Args) != 2 || !reSize.MatchString(st.Args[1]) {
				return nil, bad("want a name and WxH")
			}
			if st.Kind == "client" {
				clients[st.Args[0]] = true
			} else if !clients[st.Args[0]] {
				return nil, bad("no client %s", st.Args[0])
			}
		case "normalize":
			if len(st.Args) != 2 {
				return nil, bad("want a regular expression and a replacement")
			}
			if _, err := regexp.Compile(st.Args[0]); err != nil {
				return nil, bad("%v", err)
			}
		case "skip-if-engine":
			if len(st.Args) < 2 {
				return nil, bad("want an engine name and a reason")
			}
		case "wait":
			if _, _, _, err := parseWait(st.Args); err != nil {
				return nil, bad("%v", err)
			}
		case "capture":
			if len(st.Args) != 1 {
				return nil, bad("want one pane")
			}
		default:
			return nil, fmt.Errorf("%s:%d: unknown step %q", path, n, st.Kind)
		}
		s.Steps = append(s.Steps, st)
	}
	return s, sc.Err()
}

// parseWait splits "[-t target] <format> == <value>".
func parseWait(args []string) (target, format, value string, err error) {
	if len(args) >= 2 && args[0] == "-t" {
		target, args = args[1], args[2:]
	}
	if len(args) != 3 || args[1] != "==" {
		return "", "", "", fmt.Errorf("want [-t target] <format> == <value>")
	}
	return target, args[0], args[2], nil
}

func parseSize(s string) (w, h int) {
	m := reSize.FindStringSubmatch(s)
	w, _ = strconv.Atoi(m[1])
	h, _ = strconv.Atoi(m[2])
	return w, h
}

// SplitWords splits a line into words as a POSIX shell does, without expansions.
func SplitWords(line string) ([]string, error) {
	var words []string
	var cur strings.Builder
	inWord := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch c {
		case ' ', '\t':
			if inWord {
				words = append(words, cur.String())
				cur.Reset()
				inWord = false
			}
		case '\'':
			inWord = true
			j := strings.IndexByte(line[i+1:], '\'')
			if j < 0 {
				return nil, fmt.Errorf("unterminated '")
			}
			cur.WriteString(line[i+1 : i+1+j])
			i += j + 1
		case '"':
			inWord = true
			i++
			for ; i < len(line) && line[i] != '"'; i++ {
				if line[i] == '\\' && i+1 < len(line) && strings.IndexByte("\"\\$`", line[i+1]) >= 0 {
					i++
				}
				cur.WriteByte(line[i])
			}
			if i >= len(line) {
				return nil, fmt.Errorf(`unterminated "`)
			}
		case '\\':
			inWord = true
			if i+1 < len(line) {
				i++
				cur.WriteByte(line[i])
			}
		default:
			inWord = true
			cur.WriteByte(c)
		}
	}
	if inWord {
		words = append(words, cur.String())
	}
	if len(words) == 0 {
		return nil, fmt.Errorf("empty line")
	}
	return words, nil
}
