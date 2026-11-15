package contract

import (
	"reflect"
	"testing"
)

func TestParseGlobal(t *testing.T) {
	g := ParseGlobal([]string{"-L", "imux", "-f/tmp/c.conf", "-u", "new-session", "-d", "-s", "x"})
	if g.Socket != "imux" || g.Config != "/tmp/c.conf" || !g.UTF8 {
		t.Fatalf("got %+v", g)
	}
	if want := []string{"new-session", "-d", "-s", "x"}; !reflect.DeepEqual(g.Rest, want) {
		t.Fatalf("Rest = %q, want %q", g.Rest, want)
	}
	if g := ParseGlobal([]string{"-uV"}); !g.UTF8 || !g.Version || len(g.Rest) != 0 {
		t.Fatalf("got %+v", g)
	}
}
