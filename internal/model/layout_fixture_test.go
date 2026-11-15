package model

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/layout"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/layout/layoutfixture"
)

// The layout scenarios (testdata/layout/*.txt) replayed on the tree, against what tmux 3.8
// gave for them (the .golden files, written by cmd/layoutfixture): panes, layout strings of
// both versions, zoom and window sizes after every step (TASKS.md C2.5).

func TestLayoutFixtures(t *testing.T) {
	files, err := filepath.Glob("../../testdata/layout/*.txt")
	if err != nil || len(files) == 0 {
		t.Fatalf("no scenarios: %v", err)
	}
	for _, f := range files {
		t.Run(strings.TrimSuffix(filepath.Base(f), ".txt"), func(t *testing.T) {
			steps, err := layoutfixture.Read(f)
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(strings.TrimSuffix(f, ".txt") + ".golden")
			if err != nil {
				t.Fatal(err)
			}
			states, err := replay(steps)
			if err != nil {
				t.Fatal(err)
			}
			got := layoutfixture.Format(states)
			if got != string(want) {
				t.Error(firstDifference(string(want), got))
			}
		})
	}
}

func firstDifference(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	step := ""
	for i := 0; i < len(w) || i < len(g); i++ {
		var a, b string
		if i < len(w) {
			a = w[i]
		}
		if i < len(g) {
			b = g[i]
		}
		if strings.HasPrefix(a, "> ") {
			step = a
		}
		if a != b {
			return fmt.Sprintf("after %q (line %d):\n  tmux:      %s\n  imux-core: %s", step, i+1, a, b)
		}
	}
	return "no difference"
}

// sizeArg reads -l, -x or -y: N, or N% of curval, as tmux's args_percentage.
func sizeArg(v string, curval int) (int, error) {
	if p, ok := strings.CutSuffix(v, "%"); ok {
		n, err := strconv.Atoi(p)
		return curval * n / 100, err
	}
	return strconv.Atoi(v)
}

func replay(steps []layoutfixture.Step) ([]layoutfixture.State, error) {
	tr := NewTree()
	var w *Window
	pane := func(ref string) (*Pane, error) {
		id, err := strconv.Atoi(strings.TrimPrefix(ref, "%"))
		if err != nil || tr.Pane(id) == nil {
			return nil, fmt.Errorf("can't find pane: %s", ref)
		}
		return tr.Pane(id), nil
	}
	var states []layoutfixture.State
	for _, st := range steps {
		a := st.Words
		var err error
		switch a[0] {
		case "new":
			x, y, _ := layoutfixture.Size(a[1])
			s, _ := tr.NewSession("s", "/", time.Now())
			wl, _ := tr.NewWindow(s, -1, 0, x, y)
			s.SelectWindow(wl)
			w = wl.Window
			tr.NewPane(w, nil, false, x, y)
		case "border":
			tr.SetBorderStatus(w, map[string]layout.Status{
				"off": layout.StatusOff, "top": layout.StatusTop, "bottom": layout.StatusBottom,
			}[a[1]])
		case "split":
			var p *Pane
			if p, err = pane(a[1]); err != nil {
				break
			}
			o := SplitOptions{Size: -1}
			size := ""
			for i := 2; i < len(a); i++ {
				switch a[i] {
				case "-h":
					o.Horizontal = true
				case "-b":
					o.Before = true
				case "-f":
					o.Full = true
				case "-d":
					o.Detached = true
				case "-l":
					i++
					size = a[i]
				}
			}
			if size != "" {
				cur := p.Sy
				switch {
				case o.Full && o.Horizontal:
					cur = w.Sx
				case o.Full:
					cur = w.Sy
				case o.Horizontal:
					cur = p.Sx
				}
				if o.Size, err = sizeArg(size, cur); err != nil {
					break
				}
			}
			_, err = tr.SplitPane(p, o)
		case "kill":
			var p *Pane
			if p, err = pane(a[1]); err == nil {
				tr.RemovePane(p)
			}
		case "resize":
			var p *Pane
			if p, err = pane(a[1]); err != nil {
				break
			}
			var n int
			switch a[2] {
			case "-x":
				n, err = sizeArg(a[3], w.Sx)
				tr.ResizePaneTo(p, true, n)
			case "-y":
				n, err = sizeArg(a[3], w.Sy)
				tr.ResizePaneTo(p, false, n)
			case "-L", "-R", "-U", "-D":
				n, err = strconv.Atoi(a[3])
				if a[2] == "-L" || a[2] == "-U" {
					n = -n
				}
				tr.ResizePane(p, a[2] == "-L" || a[2] == "-R", n)
			}
		case "zoom":
			var p *Pane
			if p, err = pane(a[1]); err == nil && !tr.Unzoom(w) {
				tr.Zoom(p)
			}
		case "select":
			var p *Pane
			if p, err = pane(a[1]); err == nil {
				tr.SetActivePane(w, p)
			}
		case "swap":
			var src, dst *Pane
			if src, err = pane(a[1]); err != nil {
				break
			}
			if dst, err = pane(a[2]); err != nil {
				break
			}
			tr.SwapPanes(src, dst, len(a) > 3 && a[3] == "-d")
		case "window":
			x, y, _ := layoutfixture.Size(a[1])
			tr.ResizeWindowManual(w, x, y)
		case "layout":
			var s string
			if s, err = layoutfixture.Resolve(a[1], states); err != nil {
				return nil, err
			}
			if err = tr.SelectLayout(w, s); err != nil {
				err = fmt.Errorf("%v: %s", err, s)
			}
		default:
			return nil, fmt.Errorf("line %d: unknown step", st.Line)
		}
		s := layoutfixture.State{Step: st.Text}
		if err != nil {
			s.Error = err.Error()
		}
		zoomed := 0
		if w.Zoomed() {
			zoomed = 1
		}
		s.Window = fmt.Sprintf("%dx%d zoomed=%d", w.Sx, w.Sy, zoomed)
		for i, p := range w.Panes {
			active := 0
			if p == w.Active {
				active = 1
			}
			s.Panes = append(s.Panes, fmt.Sprintf("%%%d %d %d %d %d %d %d", p.ID, p.Xoff, p.Yoff, p.Sx, p.Sy, i, active))
		}
		s.Layout = w.LayoutString(false, false, 0)
		s.Visible = w.LayoutString(true, false, 0)
		s.V1 = w.LayoutString(false, true, 0)
		states = append(states, s)
	}
	return states, nil
}
