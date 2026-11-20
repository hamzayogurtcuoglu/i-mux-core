//go:build windows

package commands

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// The shells on Windows (E14, TASKS.md C8.7): run-shell and if-shell run shell commands in
// cmd.exe /d /s /c, as tmux's /bin/sh -c; panes and popups start PowerShell (pwsh, else
// Windows PowerShell) or cmd.exe when default-shell names none.

// JobShell is %COMSPEC%: cmd.exe, always there and quick to start.
func JobShell() string {
	if c := os.Getenv("COMSPEC"); c != "" && filepath.IsAbs(c) {
		return c
	}
	return filepath.Join(systemRoot(), "System32", "cmd.exe")
}

// DefaultPath is the server's real Windows PATH when a command-line client
// and its session give no PATH (paneEnv). It keeps Git and the user's tool
// folders; /usr/bin:/bin is not a Windows path.
func DefaultPath() string {
	if p := os.Getenv("PATH"); p != "" {
		return p
	}
	return strings.Join([]string{
		filepath.Join(systemRoot(), "System32"),
		filepath.Join(systemRoot(), "System32", "WindowsPowerShell", "v1.0"),
	}, string(os.PathListSeparator))
}

func systemRoot() string {
	if r := os.Getenv("SystemRoot"); r != "" {
		return r
	}
	return `C:\Windows`
}

// DefaultShell is the shell when default-shell and SHELL give none that can run: pwsh if
// installed, else Windows PowerShell, else cmd.exe.
func DefaultShell() string {
	if p, err := exec.LookPath("pwsh.exe"); err == nil {
		if abs, err := filepath.Abs(p); err == nil {
			return abs
		}
	}
	ps := filepath.Join(systemRoot(), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	if isExecutable(ps) == nil {
		return ps
	}
	return JobShell()
}

// kind is which family of shell sh is, by its file name.
func kind(sh string) string {
	switch strings.TrimSuffix(strings.ToLower(filepath.Base(sh)), ".exe") {
	case "cmd":
		return "cmd"
	case "powershell", "pwsh":
		return "powershell"
	}
	return ""
}

// ShellCommand is how shell sh runs one command line. cmd.exe gets it after /d /s /c, as the
// command line itself: /s takes everything between the outer quotes as it is, which Windows'
// quoting of an argument would change. PowerShell gets it after -Command; another shell, after
// -c, as on Unix.
func ShellCommand(sh, cmd string) (argv []string, cmdline string) {
	name := filepath.Base(sh)
	switch kind(sh) {
	case "cmd":
		return []string{name, "/d", "/s", "/c", cmd}, `"` + sh + `" /d /s /c "` + cmd + `"`
	case "powershell":
		return []string{name, "-NoLogo", "-Command", cmd}, ""
	}
	return []string{name, "-c", cmd}, ""
}

// loginArgv starts sh interactive: Windows shells have no login form.
func loginArgv(sh string) []string {
	if kind(sh) == "powershell" {
		return []string{filepath.Base(sh), "-NoLogo"}
	}
	return []string{filepath.Base(sh)}
}

// isExecutable is the check that sh can run: a file whose extension PATHEXT names.
func isExecutable(p string) error {
	fi, err := os.Stat(p)
	if err != nil {
		return err
	}
	if fi.IsDir() {
		return errors.New("not executable")
	}
	exts := os.Getenv("PATHEXT")
	if exts == "" {
		exts = ".COM;.EXE;.BAT;.CMD"
	}
	ext := strings.ToLower(filepath.Ext(p))
	for _, e := range strings.Split(strings.ToLower(exts), ";") {
		if e != "" && e == ext {
			return nil
		}
	}
	return errors.New("not executable")
}
