package termout

// codeType is a capability's type in tmux's table.
type codeType int

const (
	codeString codeType = iota + 1
	codeNumber
	codeFlag
)

type codeEntry struct {
	typ  codeType
	name string
}
