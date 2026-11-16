package target

import (
	"testing"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
)

// buildTree builds a tree with two sessions:
//   - "alpha" ($0, activity t=2): windows 0 "ed" (@0: %0), 2 "logs" (@1: %1, %2, %3, %4 in 2x2),
//     current=2, last=0; active pane in @1 is %1 (top-left), last pane is %4 (bottom-right).
//   - "beta" ($1, activity t=3): window 0 "work" (@2: %5).
//   - "bezel" ($2, activity t=1): window 0 "build" (@3: %6).
func buildTree(t *testing.T) *model.Tree {
	t.Helper()
	tr := model.NewTree()
	s0, err := tr.NewSession("alpha", "/", time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	wl0, err := tr.NewWindow(s0, 0, 0, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	wl0.Window.Name = "ed"
	tr.NewPane(wl0.Window, nil, false, 80, 24) // %0
	s0.SelectWindow(wl0)

	wl2, err := tr.NewWindow(s0, 2, 0, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	w1 := wl2.Window
	w1.Name = "logs"
	p1 := tr.NewPane(w1, nil, false, 80, 24) // %1
	p2, err := tr.SplitPane(p1, model.SplitOptions{Horizontal: true})
	if err != nil {
		t.Fatal(err)
	}
	p3, err := tr.SplitPane(p1, model.SplitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	p4, err := tr.SplitPane(p2, model.SplitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_ = p3
	tr.SetActivePane(w1, p4)
	tr.SetActivePane(w1, p1)
	s0.SelectWindow(wl2)

	s1, err := tr.NewSession("beta", "/", time.Unix(3, 0))
	if err != nil {
		t.Fatal(err)
	}
	wlB, _ := tr.NewWindow(s1, 0, 0, 80, 24)
	wlB.Window.Name = "work"
	tr.NewPane(wlB.Window, nil, false, 80, 24) // %5
	s1.SelectWindow(wlB)

	s2, err := tr.NewSession("bezel", "/", time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	wlZ, _ := tr.NewWindow(s2, 0, 0, 80, 24)
	wlZ.Window.Name = "build"
	tr.NewPane(wlZ.Window, nil, false, 80, 24) // %6
	s2.SelectWindow(wlZ)

	return tr
}

func TestFindTargets(t *testing.T) {
	tr := buildTree(t)
	s0 := tr.SessionByName("alpha")
	ctx := &Context{
		Tree: tr,
		Current: &State{
			S: s0, WL: s0.Current, Idx: s0.Current.Index,
			W: s0.Current.Window, P: s0.Current.Window.Active,
		},
	}

	for _, tc := range []struct {
		target string
		typ    Type
		flags  int
		wantS  string
		wantW  int
		wantP  int
		err    string
	}{
		{"", Pane, 0, "alpha", 1, 1, ""},
		{"$1", Session, 0, "beta", 2, 5, ""},
		{"=beta", Session, 0, "beta", 2, 5, ""},
		{"al", Session, 0, "alpha", 1, 1, ""},
		{"bez*", Session, 0, "bezel", 3, 6, ""},
		{"be", Session, 0, "", 0, 0, "can't find session: be"},
		{"=al", Session, 0, "", 0, 0, "can't find session: al"},
		{"@0", Window, 0, "alpha", 0, 0, ""},
		{"0", Window, 0, "alpha", 0, 0, ""},
		{"^", Window, 0, "alpha", 0, 0, ""},
		{"{start}", Window, 0, "alpha", 0, 0, ""},
		{":$", Window, 0, "alpha", 1, 1, ""},
		{"$", Window, 0, "", 0, 0, "can't find session: $"},
		{"{end}", Window, 0, "alpha", 1, 1, ""},
		{"!", Window, 0, "alpha", 0, 0, ""},
		{"{last}", Window, 0, "alpha", 0, 0, ""},
		{"+", Window, 0, "alpha", 0, 0, ""},
		{"-", Window, 0, "alpha", 0, 0, ""},
		{"lo", Window, 0, "alpha", 1, 1, ""},
		{"=logs", Window, 0, "alpha", 1, 1, ""},
		{"=lo", Window, 0, "", 0, 0, "can't find window: lo"},
		{"beta", Window, 0, "beta", 2, 5, ""}, // falls back to session
		{"%5", Pane, 0, "beta", 2, 5, ""},
		{"!", Pane, 0, "alpha", 1, 4, ""},
		{"{last}", Pane, 0, "alpha", 1, 4, ""},
		{"+", Pane, 0, "alpha", 1, 3, ""},
		{"-", Pane, 0, "alpha", 1, 4, ""},
		{"2", Pane, 0, "alpha", 1, 2, ""},
		{"{top-left}", Pane, 0, "alpha", 1, 1, ""},
		{"{top-right}", Pane, 0, "alpha", 1, 2, ""},
		{"{bottom-left}", Pane, 0, "alpha", 1, 3, ""},
		{"{bottom-right}", Pane, 0, "alpha", 1, 4, ""},
		{"{right-of}", Pane, 0, "alpha", 1, 2, ""},
		{"{down-of}", Pane, 0, "alpha", 1, 3, ""},
		{"{left-of}", Pane, 0, "alpha", 1, 2, ""}, // wraps around from left edge
		{"{up-of}", Pane, 0, "alpha", 1, 3, ""},   // wraps around from top edge
		{"alpha:0.0", Pane, 0, "alpha", 0, 0, ""},
		{"alpha:.2", Pane, 0, "alpha", 1, 2, ""},
		{":0", Pane, 0, "alpha", 0, 0, ""},
		{"ed", Pane, 0, "alpha", 0, 0, ""},  // falls back to window
		{"beta", Pane, 0, "beta", 2, 5, ""}, // falls back to session
		{"@", Pane, 0, "", 0, 0, "no current client"},
		{"=", Pane, 0, "", 0, 0, "no mouse target"},
		{"~", Pane, 0, "", 0, 0, "no marked target"},
		{"alpha:0.0", Window, WindowIndex, "", 0, 0, "can't specify pane here"},
	} {
		st, err := Find(ctx, tc.target, tc.typ, tc.flags)
		if tc.err != "" {
			if err == nil || err.Error() != tc.err {
				t.Errorf("Find(%q, %v): got %v, want error %q", tc.target, tc.typ, err, tc.err)
			}
			continue
		}
		if err != nil {
			t.Errorf("Find(%q, %v): unexpected error %v", tc.target, tc.typ, err)
			continue
		}
		if st.S.Name != tc.wantS || st.W.ID != tc.wantW || st.P.ID != tc.wantP {
			t.Errorf("Find(%q, %v): got (%s, @%d, %%%d), want (%s, @%d, %%%d)",
				tc.target, tc.typ, st.S.Name, st.W.ID, st.P.ID, tc.wantS, tc.wantW, tc.wantP)
		}
	}
}

func TestFromClientAndInside(t *testing.T) {
	tr := buildTree(t)
	// Without Current or Client or Inside, bestSession picks "beta" (activity t=3).
	st, err := Find(&Context{Tree: tr}, "", Pane, 0)
	if err != nil || st.S.Name != "beta" {
		t.Fatalf("best session: %+v %v", st, err)
	}
	// With Inside=%0 (in "alpha"), default target is "alpha" and its current window (@1).
	st, err = Find(&Context{Tree: tr, Inside: tr.Pane(0)}, "", Pane, 0)
	if err != nil || st.S.Name != "alpha" || st.W.ID != 1 {
		t.Fatalf("inside pane: %+v %v", st, err)
	}
	// With an attached Client on "bezel", default target and "@" are "bezel".
	s2 := tr.SessionByName("bezel")
	c := Client{Name: "/dev/pts/7", Session: s2, Activity: time.Unix(5, 0)}
	ctx := &Context{Tree: tr, Clients: []Client{c}, Client: &c}
	st, err = Find(ctx, "@", Pane, 0)
	if err != nil || st.S.Name != "bezel" {
		t.Fatalf("active client: %+v %v", st, err)
	}
	// Session lookup by client tty path (with and without /dev/ and trailing colon).
	for _, q := range []string{"/dev/pts/7", "pts/7", "pts/7:"} {
		st, err = Find(ctx, q, Session, 0)
		if err != nil || st.S.Name != "bezel" {
			t.Errorf("client session %q: %+v %v", q, st, err)
		}
	}
}
