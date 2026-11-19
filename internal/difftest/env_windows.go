//go:build windows

package difftest

import (
	"os"
	"path/filepath"
)

// PosixShell is the sh the scenarios' command lines run in on Windows: Git for Windows' (as on
// GitHub's Windows runners), with its cat, printf and seq beside it; IMUX_E2E_SH names another.
func PosixShell() string {
	if sh := os.Getenv("IMUX_E2E_SH"); sh != "" {
		return sh
	}
	return `C:\Program Files\Git\usr\bin\sh.exe`
}

// systemEnv is the part of FixedEnv that names the system's programs: Windows' own, and the sh
// and its tools as SHELL (which the server takes as default-shell) and on PATH.
func systemEnv() []string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	sh := PosixShell()
	path := filepath.Dir(sh) + ";" + filepath.Join(root, "System32") + ";" + root + ";" +
		filepath.Join(root, "System32", "WindowsPowerShell", "v1.0")
	env := []string{
		"PATH=" + path, "SHELL=" + sh, "SystemRoot=" + root, "COMSPEC=" + filepath.Join(root, "System32", "cmd.exe"),
		"PATHEXT=.COM;.EXE;.BAT;.CMD",
	}
	for _, k := range []string{"SystemDrive", "TEMP", "TMP", "USERNAME", "USERPROFILE", "LOCALAPPDATA", "APPDATA", "ProgramData"} {
		if v := os.Getenv(k); v != "" {
			env = append(env, k+"="+v)
		}
	}
	return env
}
