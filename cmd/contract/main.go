// Command contract extracts what i-mux uses of tmux from a record made by scripts/contract.sh,
// and compares it with the contract (TASKS.md C0.3).
//
//	contract extract [-o contract/recorded.json] [-source ../i-mux] <record dir>
//	contract check [-recorded contract/recorded.json] [-expected contract/expected.json]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/contract"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: contract extract|check …")
		return 2
	}
	switch args[0] {
	case "extract":
		fs := flag.NewFlagSet("extract", flag.ContinueOnError)
		fs.SetOutput(stderr)
		out := fs.String("o", "contract/recorded.json", "output file")
		src := fs.String("source", "", "i-mux's source tree, scanned for the commands it builds")
		if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 1 {
			fmt.Fprintln(stderr, "usage: contract extract [-o file] <record dir>")
			return 2
		}
		rec, err := contract.ExtractDir(fs.Arg(0))
		if err != nil {
			fmt.Fprintln(stderr, "contract:", err)
			return 1
		}
		if *src != "" {
			scan, err := contract.ScanSource(*src)
			if err != nil {
				fmt.Fprintln(stderr, "contract:", err)
				return 1
			}
			rec.AddSource(scan)
		}
		if err := writeJSON(*out, rec); err != nil {
			fmt.Fprintln(stderr, "contract:", err)
			return 1
		}
		fmt.Fprintf(stdout, "contract: %d calls and %d configs → %s\n", rec.Calls, rec.Configs, *out)
		return 0
	case "check":
		fs := flag.NewFlagSet("check", flag.ContinueOnError)
		fs.SetOutput(stderr)
		recPath := fs.String("recorded", "contract/recorded.json", "recorded contract")
		expPath := fs.String("expected", "contract/expected.json", "expected contract")
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		var rec contract.Recorded
		var exp contract.Report
		if err := readJSON(*recPath, &rec); err != nil {
			fmt.Fprintln(stderr, "contract:", err)
			return 1
		}
		if err := readJSON(*expPath, &exp); err != nil {
			fmt.Fprintln(stderr, "contract:", err)
			return 1
		}
		missing, unused := contract.Diff(&rec, &exp)
		for _, m := range missing {
			fmt.Fprintln(stdout, "missing:", m)
		}
		for _, u := range unused {
			fmt.Fprintln(stdout, "unused: ", u)
		}
		for _, r := range []*contract.Report{rec.Imux, rec.Harness, rec.Source} {
			if r == nil {
				continue
			}
			for _, u := range r.Unknown.Sorted() {
				fmt.Fprintln(stdout, "unknown:", u)
			}
		}
		fmt.Fprintf(stdout, "contract: %d missing, %d unused\n", len(missing), len(unused))
		if len(missing) > 0 {
			return 1
		}
		return 0
	}
	fmt.Fprintln(stderr, "contract: unknown subcommand", args[0])
	return 2
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}
