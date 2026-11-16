package format

import (
	"strings"
	"testing"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
)

// testTree is a session "s" with windows 0 (two panes) and 3, and options at each scope.
func testTree(t *testing.T) (*model.Tree, *Context) {
	t.Helper()
	tree := model.NewTree()
	now := time.Date(2026, 11, 16, 9, 5, 7, 0, time.Local)
	s, err := tree.NewSession("s", "/work/project", now)
	if err != nil {
		t.Fatal(err)
	}
	wl, err := tree.NewWindow(s, -1, 0, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	wl.Window.Name = "code"
	p0 := tree.NewPane(wl.Window, nil, false, 80, 24)
	p1, err := tree.SplitPane(p0, model.SplitOptions{Horizontal: true, Size: -1})
	if err != nil {
		t.Fatal(err)
	}
	wl3, err := tree.NewWindow(s, 3, 0, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	wl3.Window.Name = "logs"
	tree.NewPane(wl3.Window, nil, false, 80, 24)
	s.SelectWindow(wl)
	tree.Options.Server.SetString("@g", false, "global")
	s.Options.SetString("@s", false, "session")
	wl.Window.Options.SetString("@w", false, "window")
	p1.Options.SetString("@p", false, "pane,with#comma")
	ctx := &Context{Tree: tree, Now: func() time.Time { return now }}
	ctx.Defaults(nil, s, wl, p1)
	return tree, ctx
}

func TestExpand(t *testing.T) {
	_, ctx := testTree(t)
	for _, tc := range []struct{ in, want string }{
		{"#{session_name}:#{window_index}.#{pane_index}", "s:0.1"},
		{"#S #I #W #P #D", "s 0 code 1 %1"},
		{"##S #, #} ###{session_name}", "#S , } #s"},
		{"#[fg=red]#I#[default] #[range=window|#{window_index}]x", "#[fg=red]0#[default] #[range=window|0]x"},
		{"#{@g}/#{@s}/#{@w}/#{@p}", "global/session/window/pane,with#comma"},
		{"#{?@p,yes,no} #{?@nothing,yes,no} #{?,a,b}", "yes no b"},
		{"#{?@nothing,a,@s,b,c} #{?@nothing,a,@n,b}", "b "},
		{"#{==:#{@s},session} #{!=:a,a} #{||:0,,1} #{&&:1,0} #{!:} #{!!:x}", "1 0 1 0 1 1"},
		{"#{m:s*,session} #{m/r:^ses,session} #{m/i:S*,s} #{m:[!a]?,ab}", "1 1 1 0"},
		{"#{e|+:#{pane_width},1} #{e|/|f|3:1,3} #{e|%:7,4} #{e|<:1,2} #{e|+:x,1}", "40 0.333 3 1 "},
		{"#{p5:session_name}| #{p-5:session_name}| #{=2:@s} #{=-2:@s} #{=/2/…:@s}", "s    |     s| se on se…"},
		{"#{s/s/S/:@s} #{s/(e)(s)/\\2\\1/:@s} #{R:ab,3} #{n:@s} #{w:@s}", "SeSSion ssesion ababab 7 7"},
		{"#{b:session_path} #{d:session_path} #{q:@p} #{q/s:@w} #{l:#{x}}", "project /work pane,with\\#comma 'window' #{x}"},
		{"#{W:#{window_index}#{?window_active,*,} } #{W:#I,[#I]}", "0* 3  [0]3"},
		{"#{P:#{pane_id},(#{pane_id})} #{S:#{session_name}}", "%0(%1) s"},
		{"#{N:code} #{N:none} #{N/s:s} #{a:65} #{c:red}", "1 0 1 A 800000"},
		{"#{window_flags} #{pane_flags} #{window_panes} #{session_windows}", "* * 2 2"},
		{"#{session_name", ""},
		{"a#{session_name}b#{", "asb"},
		{"#{t/f/%H#:%M:session_created}", "09:05"},
	} {
		if got := ctx.Expand(tc.in); got != tc.want {
			t.Errorf("Expand(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if got := ctx.ExpandTime("%H:%M %%d %Y-%m-%d #{session_name}"); got != "09:05 %d 2026-11-16 s" {
		t.Errorf("ExpandTime: %q", got)
	}
}

// Nesting stops at tmux's loop limit, repeats at their size limit, and an expansion that
// runs too long gives nothing.
func TestLimits(t *testing.T) {
	_, ctx := testTree(t)
	ctx.Tree.Options.Server.SetString("@loop", false, "x#{E:@loop}")
	got := ctx.Expand("#{E:@loop}")
	if n := strings.Count(got, "x"); n == 0 || n > loopLimit {
		t.Errorf("loop: %d x", n)
	}
	if got := ctx.Expand("#{R:#{R:abcdefgh,10000},2}"); got != "" {
		t.Errorf("repeat size: %d bytes", len(got))
	}
	if got := ctx.Expand("#{R:a,10001}"); got != "" {
		t.Errorf("repeat count: %d bytes", len(got))
	}
	ctx.Tree.Options.Server.SetString("@twice", false, "#{E:@twice}#{E:@twice}")
	start := time.Now()
	ctx.Expand("#{E:@twice}")
	if d := time.Since(start); d > 10*TimeLimit {
		t.Errorf("an exponential expansion took %v", d)
	}
}

func TestStrftime(t *testing.T) {
	tm := time.Date(2026, 1, 4, 15, 4, 5, 0, time.UTC)
	for f, want := range map[string]string{
		"%H:%M:%S":             "15:04:05",
		"%a %A %b %B %h":       "Sun Sunday Jan January Jan",
		"%d %e %j %m %y %Y %C": "04  4 004 01 26 2026 20",
		"%I %l %p %P %k":       "03  3 PM pm 15",
		"%u %w %U %W %V %G %g": "7 0 01 00 01 2026 26",
		"%D|%F|%R|%T|%c":       "01/04/26|2026-01-04|15:04|15:04:05|Sun Jan  4 15:04:05 2026",
		"%-d %_m %05Y %^a %#b": "4  1 02026 SUN JAN",
		"%% %Q %n%t":           "% %Q \n\t",
		"%z %Z":                "+0000 UTC",
		"%":                    "%",
	} {
		if got := Strftime(f, tm); got != want {
			t.Errorf("Strftime(%q) = %q, want %q", f, got, want)
		}
	}
}

func TestFnmatch(t *testing.T) {
	for _, tc := range []struct {
		p, s string
		want bool
	}{
		{"*", "", true},
		{"*", "a/b", true},
		{"imux:*", "imux:tab", true},
		{"a?c", "abc", true},
		{"a?c", "ac", false},
		{"[a-c]x", "bx", true},
		{"[!a-c]x", "bx", false},
		{"[^a]", "b", true},
		{"\\*", "*", true},
		{"\\*", "a", false},
		{"[", "[", true},
		{"[]]", "]", true},
		{"[[:digit:]]*", "5x", true},
		{"*.go", "a.go", true},
		{"*.go", "a.got", false},
	} {
		if got := Fnmatch(tc.p, tc.s, false); got != tc.want {
			t.Errorf("Fnmatch(%q, %q) = %v", tc.p, tc.s, got)
		}
	}
	if !Fnmatch("ABC*", "abcd", true) {
		t.Error("case folding")
	}
}

func TestRegsub(t *testing.T) {
	for _, tc := range []struct{ pattern, with, text, want string }{
		{"a", "b", "banana", "bbnbnb"},
		{"^b", "B", "bab", "Bab"},
		{"(n)(a)", "\\2\\1", "banana", "baanan"},
		{"x*", "-", "ab", "a-b-"}, // as tmux's regsub: no empty match before the first character
		{"", "x", "abc", "abc"},
		{"a", "b", "", ""},
	} {
		got, ok := Regsub(tc.pattern, tc.with, tc.text, false)
		if !ok || got != tc.want {
			t.Errorf("Regsub(%q, %q, %q) = %q, %v; want %q", tc.pattern, tc.with, tc.text, got, ok, tc.want)
		}
	}
	if _, ok := Regsub("(", "x", "a", false); ok {
		t.Error("a bad pattern compiled")
	}
}

func TestWidthAndTrim(t *testing.T) {
	for _, tc := range []struct {
		s           string
		width       int
		left, right string // trimmed to 3
	}{
		{"abcdef", 6, "abc", "def"},
		{"#[fg=red]abcdef#[default]", 6, "#[fg=red]abc", "#[fg=red]def#[default]"},
		{"日本語x", 7, "日", "語x"},
		{"a##b", 3, "a##b", "a##b"},
	} {
		if got := Width(tc.s); got != tc.width {
			t.Errorf("Width(%q) = %d, want %d", tc.s, got, tc.width)
		}
		if got := TrimLeft(tc.s, 3); got != tc.left {
			t.Errorf("TrimLeft(%q) = %q, want %q", tc.s, got, tc.left)
		}
		if got := TrimRight(tc.s, 3); got != tc.right {
			t.Errorf("TrimRight(%q) = %q, want %q", tc.s, got, tc.right)
		}
	}
}

func TestParseWindowName(t *testing.T) {
	for in, want := range map[string]string{
		"-bash": "bash", "exec cat": "cat", "/usr/bin/vim file": "vim", `"a b"`: "a", "top  ": "top",
		// Control characters are not valid in a name (utf8_isvalid), which is then empty.
		"a\tb": "", `"exec cat" -u`: "cat",
	} {
		if got := ParseWindowName(in); got != want {
			t.Errorf("ParseWindowName(%q) = %q, want %q", in, got, want)
		}
	}
}

// Any format expands without a panic, within the time limit.
func FuzzExpand(f *testing.F) {
	for _, s := range []string{
		"#{session_name}", "#{?pane_active,#{=3:@p},#{p/#{e|+:#{pane_width},1}/:}}", "#[fg=red]#I",
		"#{W:#{P:#{pane_id}},x}", "#{s/(a)/\\1\\1/:@s}", "#{R:#{R:ab,99},99}", "#{t/f/%H:session_created}",
		"#{e|/|f|-3:1,0}", "#{m/r:(,x}", "##[##{", "#{=/-5/…;p-9;q/h:@p}", "#(echo)", "%H#{T:@s}",
	} {
		f.Add(s)
	}
	_, ctx := testTree(&testing.T{})
	f.Fuzz(func(t *testing.T, s string) {
		start := time.Now()
		ctx.Expand(s)
		ctx.ExpandTime(s)
		if d := time.Since(start); d > 20*TimeLimit {
			t.Errorf("%q took %v", s, d)
		}
	})
}
