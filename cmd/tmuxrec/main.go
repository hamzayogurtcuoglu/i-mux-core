// Command tmuxrec records tmux invocations. Installed as "tmux" in a directory put first on PATH,
// it appends each invocation to a log and then execs the real tmux, so the program under test
// sees no difference. scripts/contract.sh uses it to record which tmux commands i-mux runs
// (TASKS.md C0.3).
//
// It reads its settings from tmuxrec.json next to itself:
//
//	{"tmux": "/path/to/real/tmux", "out": "/path/to/record/dir"}
//
// Each invocation appends one JSON line to <out>/calls.jsonl. Config files given with -f, and
// files given to source-file, are copied into <out>/files/ so they outlive the tests that wrote
// them.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/contract"
)

func main() {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "tmuxrec:", err)
		os.Exit(1)
	}
	cfg, err := contract.LoadShimConfig(filepath.Join(filepath.Dir(exe), "tmuxrec.json"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "tmuxrec:", err)
		os.Exit(1)
	}
	if err := contract.Record(cfg, os.Args[1:], os.Getppid()); err != nil {
		// Recording must never change what the caller sees: say it and carry on.
		fmt.Fprintln(os.Stderr, "tmuxrec: record:", err)
	}
	err = execTmux(cfg.Tmux, os.Args[1:])
	fmt.Fprintln(os.Stderr, "tmuxrec: exec:", err)
	os.Exit(1)
}
