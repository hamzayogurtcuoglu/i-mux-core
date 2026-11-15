package contract

import "strings"

// Global is a tmux command line's global flags and the words after them.
type Global struct {
	Socket     string // -L
	SocketPath string // -S
	Config     string // -f
	UTF8       bool   // -u
	Version    bool   // -V
	Flags      []string
	Rest       []string // the command words
}

// ParseGlobal reads tmux's global flags (getopt "2c:CDdf:lL:NqS:T:uUvV") from args.
func ParseGlobal(args []string) Global {
	var g Global
	i := 0
	for ; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			i++
			break
		}
		if len(a) < 2 || a[0] != '-' {
			break
		}
		for j := 1; j < len(a); j++ {
			c := a[j]
			if strings.IndexByte("cfLST", c) >= 0 {
				v := a[j+1:]
				if v == "" && i+1 < len(args) {
					i++
					v = args[i]
				}
				g.Flags = append(g.Flags, "-"+string(c))
				switch c {
				case 'f':
					g.Config = v
				case 'L':
					g.Socket = v
				case 'S':
					g.SocketPath = v
				}
				break
			}
			g.Flags = append(g.Flags, "-"+string(c))
			switch c {
			case 'u':
				g.UTF8 = true
			case 'V':
				g.Version = true
			}
		}
	}
	g.Rest = args[i:]
	return g
}
