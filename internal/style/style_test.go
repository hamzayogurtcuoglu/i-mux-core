package style

import (
	"testing"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// Styles parse and print as tmux's style_parse and style_tostring do.
func TestParseAndString(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", "default"},
		{"default", "default"},
		{"fg=red,bg=colour235", "fg=red,bg=colour235"},
		{"bg=#112233 bold", "bg=#112233,bright"},
		{"fg=red bold nobold", "fg=red"},
		{"reverse,underscore italics", "underscore,reverse,italics"},
		{"align=right range=window|3", "range=window|3,align=right"},
		{"range=user|imux:tab-is-long-name", "range=user|imux:tab-is-lon"}, // 15 bytes, as tmux's range_string
		{"range=pane|%4 list=focus", "list=focus,range=pane|%4"},
		{"range=session|$2 fill=blue", "range=session|$2,fill=blue"},
		{"range=left norange", "default"},
		{"push-default,fg=green", "push-default,fg=green"},
		{"dim=40%", "dim=40%"},
		{"width=10 pad=2", "width=10,pad=2"},
		{"width=50%", "width=50%"},
		{"us=yellow double-underscore", "us=yellow,double-underscore"},
		{"fg=themegreen", "fg=themegreen"},
		{"link=https://x", "link=https://x"},
		{"none", "default"},
		{"noattr", "noattr"},
		{"fg=red,default", "default"},
		{"BG=Blue BOLD", "bg=blue,bright"},
	} {
		sy := Default()
		if err := sy.Parse(vt.DefaultCell, tc.in); err != nil {
			t.Errorf("Parse(%q): %v", tc.in, err)
			continue
		}
		if got := sy.String(); got != tc.want {
			t.Errorf("Parse(%q).String() = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// A bad style leaves the style as it was.
func TestParseErrors(t *testing.T) {
	for _, in := range []string{
		"fg=nope", "bogus", "align=middle", "list=x", "range=window", "range=pane|4",
		"range=session|x", "range=left|x", "dim=200", "width=x", "xg=red", "nofoo", "range=user|",
	} {
		sy := Default()
		_ = sy.Parse(vt.DefaultCell, "fg=red")
		if err := sy.Parse(vt.DefaultCell, "bold "+in); err == nil {
			t.Errorf("Parse(%q) worked: %s", in, sy.String())
		}
		if sy.String() != "fg=red" {
			t.Errorf("Parse(%q) changed the style to %s", in, sy.String())
		}
		if Valid(in) {
			t.Errorf("Valid(%q)", in)
		}
	}
}

// Add lays a style's colours and attributes over a cell.
func TestAdd(t *testing.T) {
	gc := vt.DefaultCell
	gc.Fg = 1
	sy := Default()
	_ = sy.Parse(vt.DefaultCell, "bg=blue,bold")
	sy.Add(&gc)
	if gc.Fg != 1 || gc.Bg != 4 || gc.Attr != vt.AttrBright {
		t.Errorf("%+v", gc)
	}
}

func TestAttributes(t *testing.T) {
	if a, ok := AttributesFromString("bold,dim|italics"); !ok || AttributesToString(a) != "bright,dim,italics" {
		t.Errorf("%v %q", ok, AttributesToString(a))
	}
	for _, bad := range []string{"", ",bold", "bold,", "nothing"} {
		if _, ok := AttributesFromString(bad); ok {
			t.Errorf("%q", bad)
		}
	}
}
