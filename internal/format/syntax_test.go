package format

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestSkip(t *testing.T) {
	tests := []struct {
		s, end string
		want   int
	}{
		{"a,b", ",", 1},
		{"#{a,b},c", ",", 6},
		{"a#,b,c", ",", 4},
		{"#{?x,#{y},z}}", "}", 11},
		{"abc", ",", -1},
		{"#{a:b}:c", ":", 6},
	}
	for _, tt := range tests {
		if got := Skip(tt.s, tt.end); got != tt.want {
			t.Errorf("Skip(%q, %q) = %d, want %d", tt.s, tt.end, got, tt.want)
		}
	}
}

func TestParseModifiers(t *testing.T) {
	tests := []struct {
		key  string
		mods []Modifier
		rest string
	}{
		{"pane_id", nil, "pane_id"},
		{"?pane_active,a,b", nil, "?pane_active,a,b"},
		{"==:a,b", []Modifier{{Name: "=="}}, "a,b"},
		{"!=:a,b", []Modifier{{Name: "!="}}, "a,b"},
		{"||:a,b", []Modifier{{Name: "||"}}, "a,b"},
		{"!:a", []Modifier{{Name: "!"}}, "a"},
		{"m:*x*,s", []Modifier{{Name: "m"}}, "*x*,s"},
		{"m/ri:x,s", []Modifier{{Name: "m", Args: []string{"ri"}}}, "x,s"},
		{"e|+:1,2", []Modifier{{Name: "e", Args: []string{"+"}}}, "1,2"},
		{"e|*|f|2:1,2", []Modifier{{Name: "e", Args: []string{"*", "f", "2"}}}, "1,2"},
		{"p10:x", []Modifier{{Name: "p", Args: []string{"10"}}}, "x"},
		{"p-10:x", []Modifier{{Name: "p", Args: []string{"-10"}}}, "x"},
		{"p/#{e|+:#{pane_width},1}/:", []Modifier{{Name: "p", Args: []string{"#{e|+:#{pane_width},1}"}}}, ""},
		{"=10:x", []Modifier{{Name: "=", Args: []string{"10"}}}, "x"},
		{"=/10/...:x", []Modifier{{Name: "=", Args: []string{"10", "..."}}}, "x"},
		{"=-5:x", []Modifier{{Name: "=", Args: []string{"-5"}}}, "x"},
		{"s/a/b/:x", []Modifier{{Name: "s", Args: []string{"a", "b"}}}, "x"},
		{"P:#{pane_id} ", []Modifier{{Name: "P"}}, "#{pane_id} "},
		{"W:a,b", []Modifier{{Name: "W"}}, "a,b"},
		{"l:#{x}", []Modifier{{Name: "l"}}, "#{x}"},
		{"T;=10:x", []Modifier{{Name: "T"}, {Name: "=", Args: []string{"10"}}}, "x"},
		{"t/p:x", []Modifier{{Name: "t", Args: []string{"p"}}}, "x"},
		{"@imux_x", nil, "@imux_x"},
		{"bogus:x", nil, "bogus:x"},
	}
	for _, tt := range tests {
		mods, rest := ParseModifiers(tt.key)
		if !reflect.DeepEqual(mods, tt.mods) || rest != tt.rest {
			t.Errorf("ParseModifiers(%q) = %+v, %q; want %+v, %q", tt.key, mods, rest, tt.mods, tt.rest)
		}
	}
}

func TestChoose(t *testing.T) {
	l, r, ok := Choose("#{a,b},#{c}")
	if !ok || l != "#{a,b}" || r != "#{c}" {
		t.Fatalf("got %q %q %v", l, r, ok)
	}
	if _, _, ok := Choose("abc"); ok {
		t.Fatal("ok without a comma")
	}
}

func collect(s string) []string {
	var out []string
	Walk(s, func(kind, text string, args []string) {
		e := kind + ":" + text
		if len(args) > 0 {
			e += "/" + strings.Join(args, "/")
		}
		out = append(out, e)
	})
	sort.Strings(out)
	return out
}

func TestWalk(t *testing.T) {
	tests := []struct {
		s    string
		want []string
	}{
		{"#{pane_id}", []string{"variable:pane_id"}},
		{"#S:#I.#P ##S", []string{"alias:#I", "alias:#P", "alias:#S"}},
		{"#{?pane_active,#[fg=red]x,y}", []string{"style:fg=red", "variable:pane_active"}},
		{"#{?#{==:#{@a},1},yes,no}", []string{"modifier:==", "variable:@a"}},
		{"#{?a,1,b,2,3}", []string{"variable:a", "variable:b"}},
		{"#{e|+:#{pane_width},1}", []string{"modifier:e/+", "variable:pane_width"}},
		{"#{P:#{pane_id} }", []string{"modifier:P", "variable:pane_id"}},
		{"#{W:#{window_name},#[bold]#{window_name}}", []string{"modifier:W", "style:bold", "variable:window_name", "variable:window_name"}},
		{"#{p/#{e|+:#{pane_width},1}/:}", []string{"modifier:e/+", "modifier:p/#{e|+:#{pane_width},1}", "variable:pane_width"}},
		{"#{l:#{x}}", []string{"modifier:l"}},
		{"#{=10:pane_title}", []string{"modifier:=/10", "variable:pane_title"}},
		{"#(echo #{pane_id})", []string{"job:echo #{pane_id}", "variable:pane_id"}},
		{"#[range=window|#{window_index}]", []string{"style:range=window|#{window_index}", "variable:window_index"}},
		{"#{&&:#{a},#{b},#{c}}", []string{"modifier:&&", "variable:a", "variable:b", "variable:c"}},
		{"%H:%M #{t:start_time}", []string{"modifier:t", "variable:start_time"}},
		{"#[fg=red]#S", []string{"alias:#S", "style:fg=red"}},
		{"#{unterminated", nil},
	}
	for _, tt := range tests {
		if got := collect(tt.s); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Walk(%q) = %q, want %q", tt.s, got, tt.want)
		}
	}
}

func FuzzWalk(f *testing.F) {
	for _, s := range []string{"#{pane_id}", "#{?a,#{b},c}", "#[fg=red]", "#(x)", "#{p/#{e|+:1,2}/:x}", "##{", "#{s/a/b/:x}"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		Walk(s, func(string, string, []string) {})
		ParseModifiers(s)
	})
}
