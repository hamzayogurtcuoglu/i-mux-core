package contract

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// ScanSource reads i-mux's Go source under dir (tests excluded) for the tmux commands it builds,
// to catch what its e2e suite does not run:
//
//   - composite literals tmux.Cmd{"name", "-x", …}, and string literals that a variable holding
//     one is later appended (append(c, "-E", "-w", …));
//   - string literals that are tmux command lines ("set-option -gu @imux_moved").
//
// Only literal flags are seen: a flag held in a variable is not.
func ScanSource(dir string) (*Report, error) {
	r := NewReport()
	fset := token.NewFileSet()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "testdata", "e2e", ".git", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		scanFile(r, f)
		return nil
	})
	return r, err
}

// isCmdType reports whether e names the tmux.Cmd type (Cmd inside package tmux).
func isCmdType(e ast.Expr) bool {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name == "Cmd"
	case *ast.SelectorExpr:
		x, ok := t.X.(*ast.Ident)
		return ok && x.Name == "tmux" && t.Sel.Name == "Cmd"
	}
	return false
}

func stringLit(e ast.Expr) (string, bool) {
	l, ok := e.(*ast.BasicLit)
	if !ok || l.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(l.Value)
	return s, err == nil
}

var reFlagLit = regexp.MustCompile(`^-[A-Za-z]+$`)

func scanFile(r *Report, f *ast.File) {
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		scanFunc(r, fn.Body)
	}
	// String literals outside functions too (constants and variables).
	ast.Inspect(f, func(n ast.Node) bool {
		if g, ok := n.(*ast.GenDecl); ok {
			ast.Inspect(g, func(n ast.Node) bool {
				if s, ok := stringLit(asExpr(n)); ok {
					scanString(r, s)
				}
				return true
			})
			return false
		}
		return true
	})
}

func asExpr(n ast.Node) ast.Expr {
	if e, ok := n.(ast.Expr); ok {
		return e
	}
	return nil
}

// scanFunc finds tmux.Cmd literals in a function body and the flags appended to them.
func scanFunc(r *Report, body *ast.BlockStmt) {
	vars := map[string]string{} // variable → command name
	addFlags := func(cmd string, args []ast.Expr) {
		spec, ok := lookupCommand(cmd)
		if !ok {
			return
		}
		for _, a := range args {
			s, ok := stringLit(a)
			if !ok || !reFlagLit.MatchString(s) {
				continue
			}
			for _, c := range s[1:] {
				if has, _, _ := takesValue(spec.Flags, byte(c)); has {
					r.Commands.Add(spec.Name, "-"+string(c))
				}
			}
		}
	}
	cmdOf := func(e ast.Expr) (string, []ast.Expr, bool) {
		cl, ok := e.(*ast.CompositeLit)
		if !ok || !isCmdType(cl.Type) || len(cl.Elts) == 0 {
			return "", nil, false
		}
		name, ok := stringLit(cl.Elts[0])
		return name, cl.Elts[1:], ok
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			if n.Tok == token.ADD_ASSIGN && len(n.Lhs) == 1 && len(n.Rhs) == 1 {
				// c += " -E -w 90% -d " + …, on a c := "display-popup".
				if id, ok := n.Lhs[0].(*ast.Ident); ok {
					if name, ok := vars[id.Name]; ok {
						if spec, known := lookupCommand(name); known {
							for _, l := range literals(n.Rhs[0]) {
								addFlagWords(r, spec, strings.Fields(l))
							}
						}
					}
				}
			}
			for i, rhs := range n.Rhs {
				if i >= len(n.Lhs) {
					break
				}
				id, ok := n.Lhs[i].(*ast.Ident)
				if !ok {
					continue
				}
				if name, _, ok := cmdOf(rhs); ok {
					vars[id.Name] = name
				} else if s, ok := stringLit(rhs); ok {
					if spec, known := lookupCommand(s); known && spec.Name == s {
						vars[id.Name] = s // c := "display-popup"
					}
				}
			}
		case *ast.CompositeLit:
			if name, rest, ok := cmdOf(n); ok {
				if spec, known := lookupCommand(name); known {
					r.Commands.Add(spec.Name, "")
					if name != spec.Name {
						r.Commands.Add(spec.Name, "alias "+name)
					}
					addFlags(name, rest)
					for _, e := range rest {
						if s, ok := stringLit(e); ok && !reFlagLit.MatchString(s) {
							r.addFormat(s)
						}
					}
				}
			}
		case *ast.CallExpr:
			if id, ok := n.Fun.(*ast.Ident); ok && id.Name == "append" && len(n.Args) > 1 {
				if v, ok := n.Args[0].(*ast.Ident); ok {
					if name, ok := vars[v.Name]; ok {
						addFlags(name, n.Args[1:])
					}
				}
			}
		case *ast.BasicLit:
			if s, ok := stringLit(n); ok {
				scanString(r, s)
			}
		}
		return true
	})
}

// scanString records a string literal: the start of a command line, or a format.
func scanString(r *Report, s string) {
	scanCommandString(r, s)
	if strings.Contains(s, "#{") {
		r.addFormat(s)
	}
}

// scanCommandString records s when it starts a tmux command line: a full command name, a
// space, then arguments ("set-option -gu ", "display-popup -E -w 90%"). The string is often
// only the start of the command, joined to values at run time, so only the command, its literal
// flags and the formats in it are recorded.
func scanCommandString(r *Report, s string) {
	words := strings.Fields(s)
	if len(words) < 2 || !strings.HasPrefix(s, words[0]+" ") {
		return
	}
	spec, known := lookupCommand(words[0])
	if !known || spec.Name != words[0] {
		return
	}
	r.Commands.Add(spec.Name, "")
	addFlagWords(r, spec, words[1:])
	r.addFormat(s)
}

// addFlagWords records the words of ws that are flags of spec's command.
func addFlagWords(r *Report, spec commandSpec, ws []string) {
	for _, w := range ws {
		if !reFlagLit.MatchString(w) {
			continue
		}
		for _, c := range w[1:] {
			if has, _, _ := takesValue(spec.Flags, byte(c)); has {
				r.Commands.Add(spec.Name, "-"+string(c))
			}
		}
	}
}

// literals returns the string literals in e, a literal or a + of them.
func literals(e ast.Expr) []string {
	switch e := e.(type) {
	case *ast.BasicLit:
		if s, ok := stringLit(e); ok {
			return []string{s}
		}
	case *ast.BinaryExpr:
		if e.Op == token.ADD {
			return append(literals(e.X), literals(e.Y)...)
		}
	case *ast.ParenExpr:
		return literals(e.X)
	}
	return nil
}
