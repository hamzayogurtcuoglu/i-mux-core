// Ported in part from tmux 3.8's prompt.c, prompt-history.c and status.c:
//
// Copyright (c) 2026 Nicholas Marriott <nicholas.marriott@gmail.com>
//
// Permission to use, copy, modify, and distribute this software for any
// purpose with or without fee is hereby granted, provided that the above
// copyright notice and this permission notice appear in all copies.
//
// THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
// WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
// MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
// ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
// WHATSOEVER RESULTING FROM LOSS OF MIND, USE, DATA OR PROFITS, WHETHER
// IN AN ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING
// OUT OF OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.

package status

import (
	"maps"
	"strings"
	"unicode/utf8"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/format"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/keys"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/options"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/style"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// Prompt flags (tmux's PROMPT_*).
const (
	PromptSingle     = 0x1
	PromptNumeric    = 0x2
	PromptKey        = 0x10
	PromptQuoteNext  = 0x40
	PromptBSpaceExit = 0x80
	PromptNoFreeze   = 0x100
	PromptCommand    = 0x200 // vi command mode
)

// Prompt types (tmux's PROMPT_TYPE_*).
const (
	PromptTypeCommand = iota
	PromptTypeSearch
	promptNTypes
)

// PromptTypeFromString is prompt_type: -1 for an unknown type.
func PromptTypeFromString(s string) int {
	switch s {
	case "command":
		return PromptTypeCommand
	case "search":
		return PromptTypeSearch
	}
	return -1
}

func promptTypeString(t int) string {
	switch t {
	case PromptTypeCommand:
		return "command"
	case PromptTypeSearch:
		return "search"
	}
	return "unknown"
}

// History is the prompts' history (prompt-history.c), by prompt type.
type History struct {
	list [promptNTypes][]string
}

// add is prompt_add_history.
func (h *History) add(line string, t, limit int) {
	l := h.list[t]
	if len(l) > 0 && l[len(l)-1] == line {
		return
	}
	l = append(l, line)
	if len(l) > limit {
		l = l[len(l)-limit:]
	}
	h.list[t] = l
}

// up is prompt_up_history.
func (h *History) up(idx *[promptNTypes]int, t int) (string, bool) {
	l := h.list[t]
	if len(l) == 0 || idx[t] == len(l) {
		return "", false
	}
	idx[t]++
	return l[len(l)-idx[t]], true
}

// down is prompt_down_history.
func (h *History) down(idx *[promptNTypes]int, t int) string {
	l := h.list[t]
	if len(l) == 0 || idx[t] == 0 {
		return ""
	}
	idx[t]--
	if idx[t] == 0 {
		return ""
	}
	return l[len(l)-idx[t]]
}

// pchar is one character of the prompt's input (a utf8_data): its bytes and width.
type pchar struct {
	s string
	w int
}

func toChars(s string) []pchar {
	var out []pchar
	for len(s) > 0 {
		r, n := utf8.DecodeRuneInString(s)
		w := 1
		if r >= 0x80 && n > 1 {
			w = max(vt.RuneWidth(r), 0)
		}
		out = append(out, pchar{s[:n], w})
		s = s[n:]
	}
	return out
}

func fromChars(cs []pchar) string {
	var b strings.Builder
	for _, c := range cs {
		b.WriteString(c.s)
	}
	return b.String()
}

func charsWidth(cs []pchar) int {
	w := 0
	for _, c := range cs {
		w += c.w
	}
	return w
}

// PromptKeyResult is prompt_key's result (tmux's prompt_key_result).
type PromptKeyResult int

// Results of a key.
const (
	PromptKeyHandled PromptKeyResult = iota
	PromptKeyClose
	PromptKeyNotHandled
	PromptKeyMove
)

// Prompt is a client's command prompt (struct prompt): the prompt text, the input being edited
// and where the cursor is in it.
type Prompt struct {
	Text  string // the prompt (a format)
	Flags int
	Type  int
	// Input is called when the prompt is done: with the input, or nil when it was cancelled.
	// It reports whether the prompt closes (it may ask for another input with Update).
	Input func(s *string) bool

	buffer []pchar
	index  int
	closed bool
	hindex [promptNTypes]int
	copied []pchar

	vi             bool
	wordSeparators string
	history        *History
	historyLimit   int
	pasteTop       func() []byte
}

// PromptOptions are what a prompt takes from the options when it is made
// (prompt_set_options).
type PromptOptions struct {
	Vi             bool   // status-keys vi
	WordSeparators string // word-separators
	History        *History
	HistoryLimit   int           // prompt-history-limit
	PasteTop       func() []byte // the top paste buffer, for C-y
}

// NewPrompt is prompt_create: input (already expanded) is the starting input.
func NewPrompt(text, input string, flags, typ int, o PromptOptions, done func(*string) bool) *Prompt {
	pr := &Prompt{
		Text: text, Flags: flags, Type: typ, Input: done,
		vi: o.Vi, wordSeparators: o.WordSeparators, history: o.History,
		historyLimit: o.HistoryLimit, pasteTop: o.PasteTop,
	}
	if pr.history == nil {
		pr.history = &History{}
	}
	pr.buffer = toChars(input)
	pr.index = len(pr.buffer)
	return pr
}

// Update is prompt_update: a new prompt and input, for the next of several prompts.
func (pr *Prompt) Update(text, input string) {
	pr.Text = text
	pr.buffer = toChars(input)
	pr.index = len(pr.buffer)
	pr.hindex = [promptNTypes]int{}
	pr.closed = false
}

// Closed is prompt_closed.
func (pr *Prompt) Closed() bool { return pr.closed }

// done is prompt_done.
func (pr *Prompt) done(s *string) PromptKeyResult {
	if pr.Input(s) {
		pr.closed = true
		return PromptKeyClose
	}
	return PromptKeyHandled
}

func inList(ws string, c pchar) bool {
	return len(c.s) == 1 && c.w == 1 && strings.Contains(ws, c.s)
}

func isSpace(c pchar) bool { return c.s == " " }

// at is the character at i, or an empty one past the end (the C buffer's terminator).
func (pr *Prompt) at(i int) pchar {
	if i >= 0 && i < len(pr.buffer) {
		return pr.buffer[i]
	}
	return pchar{}
}

// keypadKey is prompt_keypad_key.
func keypadKey(k keys.Key) keys.Key {
	if k&keys.MaskModifiers != 0 {
		return k
	}
	switch k {
	case keys.KPSlash:
		return '/'
	case keys.KPStar:
		return '*'
	case keys.KPMinus:
		return '-'
	case keys.KPSeven:
		return '7'
	case keys.KPEight:
		return '8'
	case keys.KPNine:
		return '9'
	case keys.KPPlus:
		return '+'
	case keys.KPFour:
		return '4'
	case keys.KPFive:
		return '5'
	case keys.KPSix:
		return '6'
	case keys.KPOne:
		return '1'
	case keys.KPTwo:
		return '2'
	case keys.KPThree:
		return '3'
	case keys.KPEnter:
		return '\r'
	case keys.KPZero:
		return '0'
	case keys.KPPeriod:
		return '.'
	}
	return k
}

// translateKey is prompt_translate_key: a vi key as an emacs one. 0 drops the key, 1
// processes it, 2 appends it.
func (pr *Prompt) translateKey(k keys.Key) (keys.Key, int) {
	ctrl := func(c byte) keys.Key { return keys.Key(c) | keys.Ctrl }
	if pr.Flags&PromptCommand == 0 {
		switch k {
		case ctrl('a'), ctrl('c'), ctrl('e'), ctrl('g'), ctrl('h'), '\t', ctrl('k'), ctrl('n'),
			ctrl('p'), ctrl('t'), ctrl('u'), ctrl('v'), ctrl('w'), ctrl('y'), '\n', '\r',
			keys.Left | keys.Ctrl, keys.Right | keys.Ctrl, keys.BSpace, keys.DC, keys.Down,
			keys.End, keys.Home, keys.Left, keys.Right, keys.Up:
			return k, 1
		case keys.C0ESC, ctrl('['):
			pr.Flags |= PromptCommand
			if pr.index != 0 {
				pr.index--
			}
			return k, 0
		}
		return k, 2
	}
	switch k {
	case keys.BSpace:
		return keys.Left, 1
	case 'A', 'I', 'C', 's', 'a':
		pr.Flags &^= PromptCommand
	case 'S':
		pr.Flags &^= PromptCommand
		return ctrl('u'), 1
	case 'i':
		pr.Flags &^= PromptCommand
		return k, 0
	case keys.C0ESC, ctrl('['):
		return k, 0
	}
	switch k {
	case 'A', '$':
		return keys.End, 1
	case 'I', '0', '^':
		return keys.Home, 1
	case 'C', 'D':
		return ctrl('k'), 1
	case keys.BSpace, 'X':
		return keys.BSpace, 1
	case 'b':
		return 'b' | keys.Meta, 1
	case 'B':
		return 'B' | keys.VI, 1
	case 'd':
		return ctrl('u'), 1
	case 'e':
		return 'e' | keys.VI, 1
	case 'E':
		return 'E' | keys.VI, 1
	case 'w':
		return 'w' | keys.VI, 1
	case 'W':
		return 'W' | keys.VI, 1
	case 'p':
		return ctrl('y'), 1
	case 'q':
		return ctrl('c'), 1
	case 's', keys.DC, 'x':
		return keys.DC, 1
	case keys.Down, 'j':
		return keys.Down, 1
	case keys.Left, 'h':
		return keys.Left, 1
	case 'a', keys.Right, 'l':
		return keys.Right, 1
	case keys.Up, 'k':
		return keys.Up, 1
	case ctrl('h'), ctrl('c'), '\n', '\r':
		return k, 1
	}
	return k, 0
}

// paste is prompt_paste: what C-w cut, else the top paste buffer up to its first control
// character, inserted at the cursor.
func (pr *Prompt) paste() bool {
	var ins []pchar
	if pr.copied != nil {
		ins = pr.copied
	} else {
		if pr.pasteTop == nil {
			return false
		}
		data := pr.pasteTop()
		if data == nil {
			return false
		}
		for _, c := range toChars(string(data)) {
			if len(c.s) == 1 && (c.s[0] <= 31 || c.s[0] >= 127) {
				break
			}
			ins = append(ins, c)
		}
	}
	if len(ins) != 0 {
		buf := append([]pchar(nil), pr.buffer[:pr.index]...)
		buf = append(buf, ins...)
		pr.buffer = append(buf, pr.buffer[pr.index:]...)
		pr.index += len(ins)
	}
	return true
}

// forwardWord is prompt_forward_word.
func (pr *Prompt) forwardWord(vi bool, seps string) {
	size := len(pr.buffer)
	idx := pr.index
	if !vi {
		for idx != size && isSpace(pr.at(idx)) {
			idx++
		}
	}
	if idx == size {
		pr.index = idx
		return
	}
	wordIsSeps := inList(seps, pr.at(idx)) && !isSpace(pr.at(idx))
	for {
		idx++
		if isSpace(pr.at(idx)) {
			if vi {
				for idx != size && isSpace(pr.at(idx)) {
					idx++
				}
			}
			break
		}
		if idx == size || wordIsSeps != inList(seps, pr.at(idx)) {
			break
		}
	}
	pr.index = idx
}

// endWord is prompt_end_word.
func (pr *Prompt) endWord(seps string) {
	size := len(pr.buffer)
	idx := pr.index
	if idx == size {
		return
	}
	for {
		idx++
		if idx == size {
			pr.index = idx
			return
		}
		if !isSpace(pr.at(idx)) {
			break
		}
	}
	wordIsSeps := inList(seps, pr.at(idx))
	for {
		idx++
		if idx == size || isSpace(pr.at(idx)) || wordIsSeps != inList(seps, pr.at(idx)) {
			break
		}
	}
	pr.index = idx - 1
}

// backwardWord is prompt_backward_word.
func (pr *Prompt) backwardWord(seps string) {
	idx := pr.index
	for idx != 0 {
		idx--
		if !isSpace(pr.at(idx)) {
			break
		}
	}
	wordIsSeps := inList(seps, pr.at(idx))
	for idx != 0 {
		idx--
		if isSpace(pr.at(idx)) || wordIsSeps != inList(seps, pr.at(idx)) {
			idx++
			break
		}
	}
	pr.index = idx
}

// Key is prompt_key: what a key does to the prompt.
func (pr *Prompt) Key(k keys.Key) PromptKeyResult {
	pr.closed = false
	ctrl := func(c byte) keys.Key { return keys.Key(c) | keys.Ctrl }
	if pr.Flags&PromptKey != 0 {
		s := keys.Name(k, false)
		if !pr.Input(&s) {
			pr.closed = true
		}
		return PromptKeyClose
	}
	size := len(pr.buffer)
	k &^= keys.MaskFlags
	k = keypadKey(k)

	if pr.Flags&PromptNumeric != 0 {
		if k >= '0' && k <= '9' {
			return pr.appendKey(k)
		}
		s := fromChars(pr.buffer)
		if !pr.Input(&s) {
			pr.closed = true
		}
		return PromptKeyNotHandled
	}
	if pr.Flags&(PromptSingle|PromptQuoteNext) != 0 {
		switch {
		case k&keys.MaskKey == keys.BSpace:
			k = 0x7f
		case k&keys.MaskKey > 0x7f:
			if !k.IsUnicode() {
				return PromptKeyHandled
			}
			k &= keys.MaskKey
		case k&keys.Ctrl != 0:
			k &= 0x1f
		default:
			k &= keys.MaskKey
		}
		pr.Flags &^= PromptQuoteNext
		return pr.appendKey(k)
	}
	if pr.vi {
		nk, what := pr.translateKey(k)
		switch what {
		case 1:
			k = nk
		case 2:
			return pr.appendKey(nk)
		default:
			return PromptKeyHandled
		}
	}

	switch k {
	case keys.Left, ctrl('b'):
		if pr.index > 0 {
			pr.index--
		}
	case keys.Right, ctrl('f'):
		if pr.index < size {
			pr.index++
		}
	case keys.Home, ctrl('a'):
		pr.index = 0
	case keys.End, ctrl('e'):
		pr.index = size
	case '\t':
		// Completion is not part of the engine yet.
	case keys.BSpace, ctrl('h'):
		if pr.Flags&PromptBSpaceExit != 0 && size == 0 {
			return pr.done(nil)
		}
		if pr.index != 0 {
			pr.buffer = append(pr.buffer[:pr.index-1], pr.buffer[pr.index:]...)
			pr.index--
		}
	case keys.DC, ctrl('d'):
		if pr.index != size {
			pr.buffer = append(pr.buffer[:pr.index], pr.buffer[pr.index+1:]...)
		}
	case ctrl('u'):
		pr.buffer = nil
		pr.index = 0
	case ctrl('k'):
		if pr.index < size {
			pr.buffer = pr.buffer[:pr.index]
		}
	case ctrl('w'):
		idx := pr.index
		for idx != 0 {
			idx--
			if !isSpace(pr.at(idx)) {
				break
			}
		}
		wordIsSeps := inList(pr.wordSeparators, pr.at(idx))
		for idx != 0 {
			idx--
			if isSpace(pr.at(idx)) || wordIsSeps != inList(pr.wordSeparators, pr.at(idx)) {
				idx++
				break
			}
		}
		pr.copied = append([]pchar(nil), pr.buffer[idx:pr.index]...)
		pr.buffer = append(pr.buffer[:idx], pr.buffer[pr.index:]...)
		pr.index = idx
	case keys.Right | keys.Ctrl, 'f' | keys.Meta:
		pr.forwardWord(false, pr.wordSeparators)
	case 'E' | keys.VI:
		pr.endWord("")
	case 'e' | keys.VI:
		pr.endWord(pr.wordSeparators)
	case 'W' | keys.VI:
		pr.forwardWord(true, "")
	case 'w' | keys.VI:
		pr.forwardWord(true, pr.wordSeparators)
	case 'B' | keys.VI:
		pr.backwardWord("")
	case keys.Left | keys.Ctrl, 'b' | keys.Meta:
		pr.backwardWord(pr.wordSeparators)
	case keys.Up, ctrl('p'):
		if s, ok := pr.history.up(&pr.hindex, pr.Type); ok {
			pr.buffer = toChars(s)
			pr.index = len(pr.buffer)
		}
	case keys.Down, ctrl('n'):
		pr.buffer = toChars(pr.history.down(&pr.hindex, pr.Type))
		pr.index = len(pr.buffer)
	case ctrl('y'):
		pr.paste()
	case ctrl('t'):
		idx := pr.index
		if idx < size {
			idx++
		}
		if idx >= 2 {
			pr.buffer[idx-2], pr.buffer[idx-1] = pr.buffer[idx-1], pr.buffer[idx-2]
			pr.index = idx
		}
	case '\r', '\n':
		s := fromChars(pr.buffer)
		if s != "" {
			pr.history.add(s, pr.Type, pr.historyLimit)
		}
		return pr.done(&s)
	case keys.C0ESC, ctrl('['), ctrl('c'), ctrl('g'):
		return pr.done(nil)
	case ctrl('v'):
		pr.Flags |= PromptQuoteNext
	default:
		return pr.appendKey(k)
	}
	return PromptKeyHandled
}

// appendKey is prompt_key's append_key: the key's character inserted at the cursor.
func (pr *Prompt) appendKey(k keys.Key) PromptKeyResult {
	var c pchar
	switch {
	case k <= 0x7f:
		c = pchar{string(rune(k)), 1}
		if k <= 0x1f || k == 0x7f {
			c.w = 2
		}
	case k.IsUnicode():
		r := k.Rune()
		c = pchar{string(r), max(vt.RuneWidth(r), 0)}
	default:
		return PromptKeyHandled
	}
	buf := append([]pchar(nil), pr.buffer[:pr.index]...)
	buf = append(buf, c)
	pr.buffer = append(buf, pr.buffer[pr.index:]...)
	pr.index++
	if pr.Flags&PromptSingle != 0 {
		if len(pr.buffer) != 1 {
			pr.closed = true
			return PromptKeyClose
		}
		s := fromChars(pr.buffer)
		return pr.done(&s)
	}
	return PromptKeyHandled
}

// promptFlagsString is prompt_flags_to_string.
func promptFlagsString(flags int) string {
	var out []string
	for _, f := range []struct {
		bit  int
		name string
	}{
		{PromptSingle, "SINGLE"},
		{PromptNumeric, "NUMERIC"},
		{PromptKey, "KEY"},
		{PromptQuoteNext, "QUOTENEXT"},
		{PromptBSpaceExit, "BSPACE_EXIT"},
		{PromptNoFreeze, "NOFREEZE"},
		{PromptCommand, "COMMANDMODE"},
	} {
		if flags&f.bit != 0 {
			out = append(out, f.name)
		}
	}
	return strings.Join(out, ",")
}

// PromptLines is status_prompt_redraw with prompt_draw: the status lines (at least one) with
// the prompt over its line, and the cursor's column. ctx is the prompt's target's context.
func PromptLines(ctx *format.Context, oo *options.Options, sx int, lines []Line, pr *Prompt) ([][]vt.Cell, int) {
	if sx <= 0 {
		return nil, 0
	}
	out := make([][]vt.Cell, max(len(lines), 1))
	for i := range out {
		row := make([]vt.Cell, sx)
		for x := range row {
			row[x] = vt.DefaultCell
		}
		if i < len(lines) {
			copy(row, lines[i].Cells)
		}
		out[i] = row
	}
	line := 0
	if len(lines) > 0 {
		line = min(int(oo.GetNumber("message-line")), len(lines)-1)
	}
	ax, aw := messageArea(oo, sx)

	// prompt_format_tree and prompt_expand1.
	fc := *ctx
	fc.Values = maps.Clone(ctx.Values)
	if fc.Values == nil {
		fc.Values = map[string]string{}
	}
	fc.Values["prompt_input"] = fromChars(pr.buffer)
	fc.Values["prompt_flags"] = promptFlagsString(pr.Flags)
	fc.Values["prompt_type"] = promptTypeString(pr.Type)
	fc.Values["command_prompt"] = "0"
	name := "message-style"
	if pr.Flags&PromptCommand != 0 {
		fc.Values["command_prompt"] = "1"
		name = "message-command-style"
	}
	// prompt_effective_style: the style made with no format, then with this one's formats.
	base := vt.DefaultCell
	sy0 := style.Default()
	if sy0.Parse(vt.DefaultCell, oo.GetString(name)) == nil {
		sy0.Add(&base)
	}
	sy := style.FromCell(base)
	if sy.Parse(vt.DefaultCell, fc.ExpandTime(oo.GetString(name))) != nil {
		sy = style.FromCell(base)
	}
	fc.Values["message"] = fc.ExpandTime(pr.Text)
	expanded := fc.ExpandTime(oo.GetString("message-format"))

	// prompt_layout.
	labelWidth := min(format.Width(expanded), aw)
	pcursor := charsWidth(pr.buffer[:pr.index])
	pwidth := charsWidth(pr.buffer)
	if pr.Flags&PromptQuoteNext != 0 {
		pwidth++
	}
	avail := aw - labelWidth
	offset, width, cursorX := 0, 0, labelWidth
	if avail > 0 {
		if pcursor >= avail {
			offset, width = pcursor-avail+1, avail
		} else {
			width = pwidth
		}
		width = min(width, avail)
		cursorX = labelWidth + pcursor - offset
	}
	contentWidth := min(labelWidth+width, aw)
	contentX := ax
	switch sy.Align {
	case style.AlignCentre, style.AlignAbsoluteCentre:
		contentX = ax + (aw-contentWidth)/2
	case style.AlignRight:
		contentX = ax + aw - contentWidth
	}
	inputX := contentX + labelWidth
	cursorX += contentX

	row := out[line]
	if sy.Fill != 8 {
		fill := vt.DefaultCell
		fill.Bg = sy.Fill
		for x := ax; x < ax+aw && x < sx; x++ {
			row[x] = fill
		}
	}
	gc := sy.Cell
	if contentWidth != 0 {
		if labelWidth != 0 {
			DrawFormat(row[contentX:], gc, labelWidth, expanded, nil, false)
		}
		x, w := inputX, 0
		put := func(c pchar) bool {
			if w < offset {
				w += c.w
				return true
			}
			if w >= offset+width {
				return false
			}
			w += c.w
			if w > offset+width {
				return false
			}
			cell := gc
			if len(c.s) == 1 && (c.s[0] <= 0x1f || c.s[0] == 0x7f) {
				ch := c.s[0] | 0x40
				if c.s[0] == 0x7f {
					ch = '?'
				}
				for _, s := range []string{"^", string(ch)} {
					if x < sx {
						cell.Data, cell.Width = s, 1
						row[x] = cell
					}
					x++
				}
				return true
			}
			if x < sx {
				cell.Data, cell.Width = c.s, uint8(c.w)
				row[x] = cell
				for k := 1; k < c.w && x+k < sx; k++ {
					pad := cell
					pad.Data, pad.Width, pad.Flags = "", 0, vt.FlagPadding
					row[x+k] = pad
				}
			}
			x += c.w
			return true
		}
		quote := func() bool {
			if pr.Flags&PromptQuoteNext != 0 && pcursor >= offset && x == inputX+pcursor-offset {
				return put(pchar{"^", 1})
			}
			return true
		}
		for _, c := range pr.buffer {
			if !quote() || !put(c) {
				break
			}
		}
		quote()
	}
	return out, cursorX
}
