package contract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/cmdlang"
)

// ShimConfig is tmuxrec.json.
type ShimConfig struct {
	Tmux string `json:"tmux"` // the real tmux
	Out  string `json:"out"`  // the record directory
}

// LoadShimConfig reads a tmuxrec.json.
func LoadShimConfig(path string) (ShimConfig, error) {
	var c ShimConfig
	b, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("%s: %w", path, err)
	}
	if c.Tmux == "" || c.Out == "" {
		return c, fmt.Errorf("%s: tmux and out must be set", path)
	}
	return c, nil
}

// Call is one recorded tmux invocation: a line of calls.jsonl.
type Call struct {
	Argv   []string          `json:"argv"`
	Caller string            `json:"caller,omitempty"` // the parent process's command name
	Time   string            `json:"time,omitempty"`
	Files  map[string]string `json:"files,omitempty"` // path given → copy under files/
}

// Record appends args to cfg.Out/calls.jsonl and copies the config files they name.
func Record(cfg ShimConfig, args []string, ppid int) error {
	c := Call{
		Argv:   args,
		Caller: commandName(ppid),
		Time:   time.Now().UTC().Format(time.RFC3339Nano),
	}
	for _, p := range filesNamed(args) {
		name, err := copyFile(cfg.Out, p)
		if err != nil {
			continue // a missing file (source-file -q) is not an error here
		}
		if c.Files == nil {
			c.Files = map[string]string{}
		}
		c.Files[p] = name
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.Out, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(cfg.Out, "calls.jsonl"), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	// One write per line: O_APPEND keeps concurrent writers' lines whole.
	_, err = f.Write(append(b, '\n'))
	return err
}

// filesNamed returns the -f config file and the files given to source-file in args.
func filesNamed(args []string) []string {
	var files []string
	g := ParseGlobal(args)
	if g.Config != "" {
		files = append(files, g.Config)
	}
	for _, cmd := range cmdlang.SplitArgv(g.Rest) {
		if len(cmd) == 0 || (cmd[0] != "source-file" && cmd[0] != "source") {
			continue
		}
		for _, a := range cmd[1:] {
			if !strings.HasPrefix(a, "-") {
				files = append(files, a)
			}
		}
	}
	return files
}

// copyFile copies path into out/files/, named by its content's hash, and returns that name.
func copyFile(out, path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	name := hex.EncodeToString(sum[:8]) + ".conf"
	dir := filepath.Join(out, "files")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, name)
	if _, err := os.Stat(dst); err == nil {
		return name, nil
	}
	tmp := dst + ".tmp" + strconv.Itoa(os.Getpid())
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return "", err
	}
	return name, os.Rename(tmp, dst)
}

// commandName returns the base name of process pid's command, or "".
func commandName(pid int) string {
	out, err := exec.Command("ps", "-o", "comm=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return ""
	}
	return filepath.Base(strings.TrimSpace(string(out)))
}
