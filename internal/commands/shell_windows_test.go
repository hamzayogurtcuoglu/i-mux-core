//go:build windows

package commands

import (
	"slices"
	"testing"
)

func TestShellCommandWindows(t *testing.T) {
	t.Setenv("PATH", `C:\Windows\System32;C:\Program Files\Git\cmd`)
	if got := DefaultPath(); got != `C:\Windows\System32;C:\Program Files\Git\cmd` {
		t.Errorf("DefaultPath = %q", got)
	}
	argv, line := ShellCommand(`C:\Windows\System32\cmd.exe`, `echo "a b"& exit 2`)
	if !slices.Equal(argv, []string{"cmd.exe", "/d", "/s", "/c", `echo "a b"& exit 2`}) ||
		line != `"C:\Windows\System32\cmd.exe" /d /s /c "echo "a b"& exit 2"` {
		t.Errorf("cmd.exe: %q %q", argv, line)
	}
	argv, line = ShellCommand(`C:\Program Files\PowerShell\7\pwsh.exe`, "Get-Date")
	if !slices.Equal(argv, []string{"pwsh.exe", "-NoLogo", "-Command", "Get-Date"}) || line != "" {
		t.Errorf("pwsh: %q %q", argv, line)
	}
	if got := loginArgv(`C:\x\powershell.exe`); !slices.Equal(got, []string{"powershell.exe", "-NoLogo"}) {
		t.Errorf("login: %q", got)
	}
	if sh := DefaultShell(); !CheckShell(sh) {
		t.Errorf("the default shell %q does not pass CheckShell", sh)
	}
	if !CheckShell(JobShell()) || CheckShell(`C:\Windows`) || CheckShell("cmd.exe") {
		t.Error("CheckShell")
	}
}
