package format

import (
	"os"
	"strings"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/buffers"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// Context is tmux's format tree: what a format's variables are looked up in.
type Context struct {
	Tree   *model.Tree
	Server *Server
	// Client is the client the format is for (tmux's ft->c), or nil.
	Client *Client
	// ItemClient is the client the command came from (ft->client), or nil.
	ItemClient *Client
	// Clients are the attached clients, for #{L:…}.
	Clients []*Client

	S  *model.Session
	WL *model.Winlink
	W  *model.Window
	P  *model.Pane
	// Type is what the format was made for: a pane, window, session or none.
	Type Type

	// Values are variables a command adds (format_add): loop_index and the like.
	Values map[string]string
	// Environ looks a name up in the session's environment, then the global one.
	Environ func(s *model.Session, name string) (string, bool)
	// Now is the time strftime and the time modifiers use; nil is time.Now.
	Now func() time.Time
	// Mouse is the mouse event of the command's key, if any.
	Mouse *Mouse
	// Buffer is the paste buffer the buffer_* variables give (format_defaults_paste_buffer).
	Buffer *buffers.Buffer
}

// Type is format_type.
type Type int

// Format types.
const (
	TypeUnknown Type = iota
	TypeSession
	TypeWindow
	TypePane
)

// Server is what the server variables give.
type Server struct {
	Pid        int
	Start      time.Time
	SocketPath string
	Version    string
	// ConfigFiles are the configuration files loaded, for #{config_files}.
	ConfigFiles []string
}

// Client is a client as formats see it.
type Client struct {
	Name     string // its terminal (#{client_name})
	TTY      string // #{client_tty}
	Session  *model.Session
	Last     *model.Session // the session it showed before (#{client_last_session})
	Activity time.Time
	Created  time.Time
	Pid      int
	// Flags are client_flags: attached, focused, control-mode, read-only, UTF-8, ….
	Flags      []string
	Control    bool
	ReadOnly   bool
	UTF8       bool
	Width      int
	Height     int
	Started    bool // its terminal is started (TTY_STARTED): it has a size
	TermName   string
	TermType   string
	Features   string // #{client_termfeatures}
	KeyTable   string
	Prefix     bool // its key table is not the default one
	Colours    int  // 2, 8, 16, 256 or 16777216
	CellWidth  int
	CellHeight int
	Theme      string
	Discarded  int
	Written    int
	UID        int
	User       string
	// HasCapability and HasFeature answer #{I/c:…} and #{I/f:…}; Env answers #{I/e:…}.
	HasCapability func(name string) bool
	HasFeature    func(name string) bool
	Env           map[string]string
}

// Mouse is a mouse event's place, for the mouse_* variables.
type Mouse struct {
	Valid bool
	// X and Y are the position in the pane under the mouse, or on the status line (HasXY).
	X, Y        int
	HasXY       bool
	Pane        *model.Pane
	StatusLine  int    // -1 when not on a status line
	StatusRange string // the range under the mouse, as #{mouse_status_range} gives it
	HasRange    bool
}

func (c *Context) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// Defaults is format_defaults: the client, session, winlink and pane the format is for. A
// missing session is the client's, a missing winlink the session's current, a missing pane the
// window's active one.
func (c *Context) Defaults(cl *Client, s *model.Session, wl *model.Winlink, p *model.Pane) {
	switch {
	case p != nil:
		c.Type = TypePane
	case wl != nil:
		c.Type = TypeWindow
	case s != nil:
		c.Type = TypeSession
	default:
		c.Type = TypeUnknown
	}
	if s == nil && cl != nil {
		s = cl.Session
	}
	if wl == nil && s != nil {
		wl = s.Current
	}
	if p == nil && wl != nil {
		p = wl.Window.Active
	}
	if cl != nil {
		if c.S == nil {
			c.S = cl.Session
		}
		c.Client = cl
	}
	if s != nil {
		c.S = s
	}
	if wl != nil {
		if c.W == nil {
			c.W = wl.Window
		}
		c.WL = wl
	}
	if p != nil {
		if c.W == nil {
			c.W = p.Window
		}
		c.P = p
	}
}

// job is #(…): its output. Running commands in formats comes with the status line (task
// C4.2); until then it is empty, as with tmux's FORMAT_NOJOBS.
func (c *Context) job(string) string { return "" }

// clientQuery is #{I/c:…}, #{I/f:…} and #{I/e:…}.
func (c *Context) clientQuery(modifiers uint64, name string) string {
	cl := c.Client
	if cl == nil || !cl.Started {
		return ""
	}
	value := ""
	if modifiers&modClientTermcap != 0 {
		value = boolString(cl.HasCapability != nil && cl.HasCapability(name))
	}
	if modifiers&modClientTermfeat != 0 {
		value = boolString(cl.HasFeature != nil && cl.HasFeature(name))
	}
	if modifiers&modClientEnviron != 0 {
		value = cl.Env[name]
	}
	return value
}

// colour is #{c:…}: a colour as six hex digits (or with f or b as the client's escape
// sequence, which needs a client terminal and is empty here).
func (c *Context) colour(modifiers uint64, s string) string {
	if modifiers&(modColourEscFg|modColourEscBg) != 0 {
		if strings.EqualFold(s, "none") {
			return "\033[0m"
		}
		return ""
	}
	col := vt.ColorFromString(s)
	if col == -1 {
		return ""
	}
	rgb := vt.ColorForceRGB(col)
	if rgb == -1 {
		return ""
	}
	r, g, b := vt.ColorSplitRGB(rgb)
	const hex = "0123456789abcdef"
	out := []byte{hex[r>>4], hex[r&15], hex[g>>4], hex[g&15], hex[b>>4], hex[b&15]}
	return string(out)
}

// host is #{host}: the host name.
func host() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	return h
}

// ParseWindowName is tmux's parse_window_name: a command line made a window name ("exec " and
// leading dashes dropped, the first word, its base name when it is a path).
func ParseWindowName(in string) string {
	name := in
	name = strings.TrimPrefix(name, "\"")
	if i := strings.IndexByte(name, '"'); i >= 0 {
		name = name[:i]
	}
	name = strings.TrimPrefix(name, "exec ")
	name = strings.TrimLeft(name, " -")
	if i := strings.IndexByte(name, ' '); i >= 0 {
		name = name[:i]
	}
	for len(name) > 1 {
		ch := name[len(name)-1]
		if isAlnum(ch) || isPunct(ch) {
			break
		}
		name = name[:len(name)-1]
	}
	if strings.HasPrefix(name, "/") {
		name = basename(name)
	}
	clean, ok := vt.CleanName(name, false)
	if !ok {
		return ""
	}
	return clean
}

func isAlnum(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// basename is basename(3).
func basename(s string) string {
	if s == "" {
		return "."
	}
	for len(s) > 1 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	if s == "/" {
		return s
	}
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		return s[i+1:]
	}
	return s
}
