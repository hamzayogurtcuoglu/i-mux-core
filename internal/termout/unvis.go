package termout

// strunvis decodes s as tmux's compat strunvis (OpenBSD unvis.c) does: \\, \n \r \b \a \v \t
// \f \s \E, octal, \M- and \^ escapes. ok is false on a bad escape, where tmux keeps the text
// as written.
func strunvis(s string) (string, bool) {
	const (
		ground = iota
		start
		meta
		meta1
		ctrl
		octal2
		octal3
	)
	isOctal := func(c byte) bool { return c >= '0' && c <= '7' }
	var out []byte
	state := ground
	var cp byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch state {
		case ground:
			if c == '\\' {
				state = start
				cp = 0
				continue
			}
			out = append(out, c)
		case start:
			state = ground
			switch c {
			case '\\':
				out = append(out, '\\')
			case '0', '1', '2', '3', '4', '5', '6', '7':
				cp = c - '0'
				state = octal2
			case 'M':
				cp = 0o200
				state = meta
			case '^':
				state = ctrl
			case 'n':
				out = append(out, '\n')
			case 'r':
				out = append(out, '\r')
			case 'b':
				out = append(out, '\b')
			case 'a':
				out = append(out, '\a')
			case 'v':
				out = append(out, '\v')
			case 't':
				out = append(out, '\t')
			case 'f':
				out = append(out, '\f')
			case 's':
				out = append(out, ' ')
			case 'E':
				out = append(out, 0o33)
			case '\n', '$':
				// A hidden newline or marker: nothing.
			default:
				return s, false
			}
		case meta:
			switch c {
			case '-':
				state = meta1
			case '^':
				state = ctrl
			default:
				return s, false
			}
		case meta1:
			out = append(out, cp|c)
			state = ground
		case ctrl:
			if c == '?' {
				out = append(out, cp|0o177)
			} else {
				out = append(out, cp|(c&0o37))
			}
			state = ground
		case octal2:
			if isOctal(c) {
				cp = cp<<3 + (c - '0')
				state = octal3
				continue
			}
			out = append(out, cp)
			state = ground
			i-- // the character is read again
		case octal3:
			state = ground
			if isOctal(c) {
				out = append(out, cp<<3+(c-'0'))
				continue
			}
			out = append(out, cp)
			i--
		}
	}
	switch state {
	case octal2, octal3:
		out = append(out, cp)
	case ground:
	default:
		return s, false
	}
	return string(out), true
}
