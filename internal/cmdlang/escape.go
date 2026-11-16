package cmdlang

import (
	"strings"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// Escape quotes an argument as tmux's args_escape does, so that it reads back the same.
func Escape(s string) string {
	if s == "" {
		return "''"
	}
	quote := byte(0)
	if strings.ContainsAny(s, " #';${}%") {
		quote = '"'
	} else if strings.ContainsAny(s, " \"") {
		quote = '\''
	}
	if s[0] != ' ' && len(s) == 1 && (quote != 0 || s[0] == '~') {
		return "\\" + s
	}
	escaped := vt.ArgVis(s, quote == '"')
	switch quote {
	case '\'':
		return "'" + escaped + "'"
	case '"':
		if strings.HasPrefix(escaped, "~") {
			return "\"\\" + escaped + "\""
		}
		return "\"" + escaped + "\""
	}
	if strings.HasPrefix(escaped, "~") {
		return "\\" + escaped
	}
	return escaped
}
