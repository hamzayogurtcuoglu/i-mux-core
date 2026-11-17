package commands

import (
	"slices"
	"testing"
	"time"
)

// send-keys: targets, key names, text, -l, -H and -N; a key with no encoding in the pane goes
// as its name's text (TASKS.md C2.11).
func TestSendKeys(t *testing.T) {
	h := newFakeHost()
	for _, s := range []string{"a", "b"} {
		if r := run(h, "new-session", "-d", "-s", s, "cat"); r.Failed {
			t.Fatalf("new-session: %+v", r)
		}
	}
	for _, tc := range []struct {
		args []string
		sent []string
		err  string
	}{
		{[]string{"-t", "%1", "C-v", "Enter"}, []string{"%1 C-v", "%1 Enter"}, ""},
		{[]string{"-t", "a", "x"}, []string{"%0 x"}, ""},
		{[]string{"-t", "a:0.0", "-l", "Up"}, []string{"%0 U", "%0 p"}, ""},
		{[]string{"-t", "b:", "-H", "41", "zz"}, []string{"%1 A"}, ""},
		{[]string{"-t", "=b:", "-N", "2", "q"}, []string{"%1 q", "%1 q"}, ""},
		{[]string{"-t", "%1", "C-BSpace"}, []string{"%1 C-BSpace", "%1 C", "%1 -", "%1 B", "%1 S", "%1 p", "%1 a", "%1 c", "%1 e"}, ""},
		{[]string{"-t", "%1", "héllo"}, []string{"%1 h", "%1 é", "%1 l", "%1 l", "%1 o"}, ""},
		{[]string{"x"}, []string{"%0 x"}, ""}, // the best session: the first made
		{[]string{"-t", "%9", "x"}, nil, "can't find pane: %9"},
		{[]string{"-t", "nope", "x"}, nil, "can't find pane: nope"},
		{[]string{"-t", "nope:", "x"}, nil, "can't find session: nope"},
		{[]string{"-t", "a:7", "x"}, nil, "can't find window: 7"},
		{[]string{"-t", "a:0.3", "x"}, nil, "can't find pane: 3"},
		// -M with no mouse event: a command line has none.
		{[]string{"-M"}, nil, "no mouse target"},
	} {
		h.sent = nil
		r := run(h, append([]string{"send-keys"}, tc.args...)...)
		if tc.err != "" {
			if !r.Failed || r.Stderr != tc.err+"\n" {
				t.Errorf("%q: %+v, want %q", tc.args, r, tc.err)
			}
			continue
		}
		if r.Failed || !slices.Equal(h.sent, tc.sent) {
			t.Errorf("%q: sent %q (%+v), want %q", tc.args, h.sent, r, tc.sent)
		}
	}
}

// switch-client moves the client given with -c, else the most recently active one; with no
// client attached there is no current client.
func TestSwitchClient(t *testing.T) {
	h := newFakeHost()
	for _, s := range []string{"a", "b"} {
		if r := run(h, "new-session", "-d", "-s", s, "cat"); r.Failed {
			t.Fatalf("new-session: %+v", r)
		}
	}
	if r := run(h, "switch-client", "-t", "a"); !r.Failed || r.Stderr != "no current client\n" {
		t.Errorf("no client: %+v", r)
	}
	a := h.tree.SessionByName("a")
	h.clients = []*ClientInfo{
		{Name: "/dev/pts/1", Session: a, Activity: time.Unix(1, 0)},
		{Name: "/dev/pts/2", Session: a, Activity: time.Unix(2, 0)},
	}
	for _, args := range [][]string{{"-t", "b"}, {"-c", "/dev/pts/1", "-t", "=b"}, {"-c", "pts/1", "-t", "a"}} {
		if r := run(h, append([]string{"switch-client"}, args...)...); r.Failed {
			t.Errorf("%q: %+v", args, r)
		}
	}
	if want := []string{"/dev/pts/2 b", "/dev/pts/1 b", "/dev/pts/1 a"}; !slices.Equal(h.switched, want) {
		t.Errorf("switched %q, want %q", h.switched, want)
	}
	for _, tc := range []struct {
		args []string
		err  string
	}{
		{[]string{"-c", "/dev/pts/9", "-t", "a"}, "can't find client: /dev/pts/9"},
		{[]string{"-t", "nope"}, "can't find session: nope"},
		{[]string{"-n"}, "switch-client -n: not implemented by imux-core yet"},
	} {
		if r := run(h, append([]string{"switch-client"}, tc.args...)...); !r.Failed || r.Stderr != tc.err+"\n" {
			t.Errorf("%q: %+v, want %q", tc.args, r, tc.err)
		}
	}
}

func TestRefreshClient(t *testing.T) {
	h := newFakeHost()
	if r := run(h, "new-session", "-d", "-s", "a", "cat"); r.Failed {
		t.Fatalf("new-session: %+v", r)
	}
	if r := run(h, "refresh-client", "-S"); !r.Failed || r.Stderr != "no current client\n" {
		t.Errorf("no client: %+v", r)
	}
	h.clients = []*ClientInfo{{Name: "/dev/pts/1", Session: h.tree.SessionByName("a")}}
	for _, args := range [][]string{{"-S"}, {}, {"-S", "-t", "/dev/pts/1"}} {
		if r := run(h, append([]string{"refresh-client"}, args...)...); r.Failed {
			t.Errorf("%q: %+v", args, r)
		}
	}
	if want := []string{"/dev/pts/1 true", "/dev/pts/1 false", "/dev/pts/1 true"}; !slices.Equal(h.refreshed, want) {
		t.Errorf("refreshed %q, want %q", h.refreshed, want)
	}
	if r := run(h, "refresh-client", "-l"); !r.Failed || r.Stderr != "refresh-client -l: not implemented by imux-core yet\n" {
		t.Errorf("-l: %+v", r)
	}
}
