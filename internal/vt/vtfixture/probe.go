package vtfixture

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// A probe is a list of queries a program sends, one per line of testdata/vt/replies.txt:
//
//	name<TAB>"Go-quoted bytes"
//
// Each is written in turn on one pane (so earlier entries can set up state), and what the
// terminal answers is recorded. replies.out holds tmux's answers, as name<TAB>"quoted reply".

// Entry is one probe line.
type Entry struct {
	Name  string
	Query string
}

// ReadEntries reads a probe or reply file.
func ReadEntries(path string) ([]Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Entry
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		n++
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, q, ok := strings.Cut(line, "\t")
		if !ok {
			return nil, fmt.Errorf("%s:%d: no tab", path, n)
		}
		v, err := strconv.Unquote(q)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %v", path, n, err)
		}
		out = append(out, Entry{Name: name, Query: v})
	}
	return out, sc.Err()
}

// FormatReplies writes replies (Query holding the reply) as a replies.out file.
func FormatReplies(replies []Entry) string {
	var b strings.Builder
	for _, r := range replies {
		b.WriteString(r.Name + "\t" + strconv.Quote(r.Query) + "\n")
	}
	return b.String()
}

// Barrier is the query after each entry: DSR 5, which tmux answers "ESC [ 0 n" only once it has
// dealt with everything before it.
const (
	Barrier      = "\x1b[5n"
	BarrierReply = "\x1b[0n"
)
