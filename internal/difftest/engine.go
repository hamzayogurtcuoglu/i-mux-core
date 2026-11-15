package difftest

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"time"
)

// Result is what one command gave: its output and exit code.
type Result struct {
	Stdout string
	Stderr string
	Code   int
}

// Env is where an engine runs a script: its own directory, socket, home and environment.
type Env struct {
	Dir    string   // the run's temporary directory
	Socket string   // the server's socket path (-S)
	Config string   // the config file (-f), or ""
	Vars   []string // the whole environment of every command
}

// Engine runs tmux command lines: real tmux, i-mux-core, or a stub in tests.
type Engine interface {
	Name() string
	// Run runs one invocation with args (the command words, without -S, -f and -u).
	Run(ctx context.Context, env *Env, args []string) Result
	// Stop ends the engine's server for env, if it runs.
	Stop(env *Env)
}

// Binary is an engine that is a tmux-compatible program: tmux or imux-core.
type Binary struct {
	Label string // "tmux", "imux-core"
	Path  string
}

// NewBinary returns the engine for the program at path, made absolute, as commands run in the
// run's own directory.
func NewBinary(label, path string) (*Binary, error) {
	p, err := exec.LookPath(path)
	if err != nil {
		return nil, err
	}
	if p, err = filepath.Abs(p); err != nil {
		return nil, err
	}
	return &Binary{Label: label, Path: p}, nil
}

// Name implements Engine.
func (b *Binary) Name() string { return b.Label }

// Run implements Engine. Every invocation gets -S, -f and -u, as i-mux passes -L, -f and -u.
func (b *Binary) Run(ctx context.Context, env *Env, args []string) Result {
	argv := []string{"-S", env.Socket}
	if env.Config != "" {
		argv = append(argv, "-f", env.Config)
	}
	argv = append(append(argv, "-u"), args...)
	cmd := exec.CommandContext(ctx, b.Path, argv...)
	cmd.Dir = env.Dir
	cmd.Env = env.Vars
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	r := Result{Stdout: out.String(), Stderr: errb.String()}
	var ee *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee):
		r.Code = ee.ExitCode()
	default:
		r.Code = -1
		r.Stderr += "difftest: " + err.Error()
	}
	return r
}

// Stop implements Engine: kill-server on the run's own socket, never anyone else's.
func (b *Binary) Stop(env *Env) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, b.Path, "-S", env.Socket, "kill-server")
	cmd.Env = env.Vars
	_ = cmd.Run()
}

// Func is an engine made of a function, for testing the harness itself.
type Func struct {
	Label string
	Fn    func(args []string) Result
}

// Name implements Engine.
func (f *Func) Name() string { return f.Label }

// Run implements Engine.
func (f *Func) Run(_ context.Context, _ *Env, args []string) Result { return f.Fn(args) }

// Stop implements Engine.
func (f *Func) Stop(*Env) {}
