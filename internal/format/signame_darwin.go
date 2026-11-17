//go:build darwin

package format

import "strconv"

// sysSigname is macOS's sys_signame.
var sysSigname = [...]string{
	"", "hup", "int", "quit", "ill", "trap", "abrt", "emt", "fpe", "kill", "bus", "segv", "sys",
	"pipe", "alrm", "term", "urg", "stop", "tstp", "cont", "chld", "ttin", "ttou", "io", "xcpu",
	"xfsz", "vtalrm", "prof", "winch", "info", "usr1", "usr2",
}

// sig2name is tmux's sig2name where the system has sys_signame: its name, else the number.
func sig2name(sig int) string {
	if sig > 0 && sig < len(sysSigname) {
		return sysSigname[sig]
	}
	return strconv.Itoa(sig)
}
