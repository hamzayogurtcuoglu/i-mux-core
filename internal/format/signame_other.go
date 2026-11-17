//go:build !darwin

package format

import "strconv"

// sig2name is tmux's sig2name on a system without sys_signame (Linux): the number.
func sig2name(sig int) string { return strconv.Itoa(sig) }
