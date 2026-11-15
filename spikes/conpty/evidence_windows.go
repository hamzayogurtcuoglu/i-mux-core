package main

import (
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Row is one line of an evidence table.
type Row struct {
	What, Result string
}

// Report is the spike's evidence, written as Markdown.
type Report struct {
	Build   string
	Flags   []Row
	Output  map[string][]Row // per pseudo console flag set
	Input   map[string][]Row
	Process []Row
	IPC     []Row
}

func (r *Report) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# ConPTY and IPC evidence (C0.7)\n\nWindows build: %s\n\n", r.Build)
	table := func(title string, rows []Row) {
		fmt.Fprintf(&b, "## %s\n\n| What | Result |\n| --- | --- |\n", title)
		for _, row := range rows {
			fmt.Fprintf(&b, "| %s | %s |\n", row.What, strings.ReplaceAll(row.Result, "|", `\|`))
		}
		b.WriteString("\n")
	}
	table("Pseudo console flags", r.Flags)
	for _, name := range sortedKeys(r.Output) {
		table("Output sequences, flags "+name, r.Output[name])
	}
	for _, name := range sortedKeys(r.Input) {
		table("Input sequences, flags "+name, r.Input[name])
	}
	table("Processes", r.Process)
	table("IPC", r.IPC)
	return b.String()
}

func sortedKeys(m map[string][]Row) []string {
	var k []string
	for name := range m {
		k = append(k, name)
	}
	for i := range k {
		for j := i + 1; j < len(k); j++ {
			if k[j] < k[i] {
				k[i], k[j] = k[j], k[i]
			}
		}
	}
	return k
}

func childCmdline(mode string) (string, []string) {
	exe, _ := os.Executable()
	env := append(os.Environ(), childEnv+"="+mode)
	return windows.EscapeArg(exe), env
}

// outputProbes are sequences a program writes; the question is what ConPTY sends on.
var outputProbes = []struct{ name, seq string }{
	{"alternate screen 1049 on", "\x1b[?1049h"},
	{"alternate screen 1049 off", "\x1b[?1049l"},
	{"synchronized output 2026", "\x1b[?2026h"},
	{"mouse 1000/1002/1003", "\x1b[?1000h\x1b[?1002h\x1b[?1003h"},
	{"mouse SGR 1006", "\x1b[?1006h"},
	{"bracketed paste 2004", "\x1b[?2004h"},
	{"focus events 1004", "\x1b[?1004h"},
	{"OSC 0 title", "\x1b]0;spike-title\x07"},
	{"OSC 8 hyperlink", "\x1b]8;;https://example.com/\x1b\\link\x1b]8;;\x1b\\"},
	{"OSC 52 clipboard", "\x1b]52;c;aGVsbG8=\x07"},
	{"OSC 7 cwd", "\x1b]7;file://host/tmp\x07"},
	{"OSC 9;9 cwd", "\x1b]9;9;C:\\Users\x07"},
	{"kitty keys CSI > 1 u", "\x1b[>1u"},
	{"modifyOtherKeys CSI > 4 ; 2 m", "\x1b[>4;2m"},
	{"true colour 38;2", "\x1b[38;2;1;2;3mX\x1b[0m"},
	{"underline style 4:3", "\x1b[4:3mU\x1b[0m"},
	{"wide characters", "日本🙂"},
	{"DA1 query", "\x1b[c"},
}

// classify says how want reached out: as it is, changed, or not at all.
func classify(out []byte, want string) string {
	if strings.Contains(string(out), want) {
		return "passed through"
	}
	i := strings.Index(string(out), beginMark)
	j := strings.Index(string(out), endMark)
	if i >= 0 && j > i {
		between := string(out[i+len(beginMark) : j])
		if between == "" {
			return "dropped"
		}
		return "rewritten: " + strconv.Quote(trim(between, 120))
	}
	return "not seen: output " + strconv.Quote(trim(string(out), 120))
}

func trim(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func probeOutput(flags uint32) []Row {
	var rows []Row
	for _, p := range outputProbes {
		cmd, env := childCmdline("emit:" + hex.EncodeToString([]byte(p.seq)))
		c, err := StartConsole(cmd, 80, 24, flags, env)
		if err != nil {
			rows = append(rows, Row{p.name, "error: " + err.Error()})
			continue
		}
		out, err := c.WaitOutput(endMark, 10*time.Second)
		res := classify(out, p.seq)
		if err != nil {
			res += " (" + err.Error() + ")"
		}
		rows = append(rows, Row{p.name, res})
		c.Close()
	}
	return rows
}

// inputProbes are sequences a terminal sends; the question is what the program reads.
var inputProbes = []struct{ name, seq string }{
	{"text", "abc"},
	{"Up", "\x1b[A"},
	{"CSI u (C-a)", "\x1b[97;5u"},
	{"modifyOtherKeys (C-Enter)", "\x1b[27;5;13~"},
	{"bracketed paste", "\x1b[200~paste\x1b[201~"},
	{"SGR mouse press", "\x1b[<0;10;5M"},
	{"focus in", "\x1b[I"},
}

func probeInput(flags uint32) []Row {
	var rows []Row
	for _, p := range inputProbes {
		cmd, env := childCmdline("echo-input")
		c, err := StartConsole(cmd, 80, 24, flags, env)
		if err != nil {
			rows = append(rows, Row{p.name, "error: " + err.Error()})
			continue
		}
		if _, err := c.WaitOutput("READY", 10*time.Second); err != nil {
			rows = append(rows, Row{p.name, "child not ready: " + err.Error()})
			c.Close()
			continue
		}
		before := len(c.Output())
		if err := c.Write([]byte(p.seq)); err != nil {
			rows = append(rows, Row{p.name, "write: " + err.Error()})
			c.Close()
			continue
		}
		time.Sleep(500 * time.Millisecond) // the program prints what it read, if anything
		got := ""
		for _, line := range strings.Split(string(c.Output()[before:]), "\n") {
			if k := strings.Index(line, "IN:"); k >= 0 {
				b, _ := hex.DecodeString(strings.TrimSpace(line[k+3:]))
				got += string(b)
			}
		}
		switch got {
		case p.seq:
			rows = append(rows, Row{p.name, "passed through"})
		case "":
			rows = append(rows, Row{p.name, "nothing read"})
		default:
			rows = append(rows, Row{p.name, "read as " + strconv.Quote(got)})
		}
		c.Close()
	}
	return rows
}

func probeFlags() []Row {
	var rows []Row
	for _, f := range []struct {
		name  string
		flags uint32
	}{
		{"none", 0},
		{"INHERIT_CURSOR", flagInheritCursor},
		{"RESIZE_QUIRK", flagResizeQuirk},
		{"WIN32_INPUT_MODE", flagWin32InputMode},
		{"PASSTHROUGH_MODE", flagPassthroughMode},
	} {
		cmd, env := childCmdline("exit:0")
		c, err := StartConsole(cmd, 80, 24, f.flags, env)
		if err != nil {
			rows = append(rows, Row{f.name, "refused: " + err.Error()})
			continue
		}
		c.Close()
		rows = append(rows, Row{f.name, "accepted"})
	}
	return rows
}

func probeProcesses() []Row {
	var rows []Row
	// Shells.
	for _, sh := range []struct{ name, cmd, want string }{
		{"cmd.exe", `cmd.exe /d /c echo spike-cmd`, "spike-cmd"},
		{"powershell.exe", `powershell.exe -NoLogo -NoProfile -Command "Write-Output spike-ps"`, "spike-ps"},
		{"pwsh.exe", `pwsh.exe -NoLogo -NoProfile -Command "Write-Output spike-pwsh"`, "spike-pwsh"},
	} {
		want := sh.want
		c, err := StartConsole(sh.cmd, 80, 24, 0, nil)
		if err != nil {
			rows = append(rows, Row{sh.name, "error: " + err.Error()})
			continue
		}
		_, err = c.WaitOutput(want, 20*time.Second)
		code, werr := c.Wait(20 * time.Second)
		res := fmt.Sprintf("printed, exit %d", code)
		if err != nil {
			res = err.Error()
		} else if werr != nil {
			res = "printed; wait: " + werr.Error()
		}
		rows = append(rows, Row{sh.name, res})
		c.Close()
	}
	// Exit codes.
	cmd, env := childCmdline("exit:7")
	if c, err := StartConsole(cmd, 80, 24, 0, env); err == nil {
		code, err := c.Wait(10 * time.Second)
		rows = append(rows, Row{"exit code 7", fmt.Sprintf("%d %v", code, errString(err))})
		c.Close()
	}
	// Resize.
	cmd, env = childCmdline("size")
	if c, err := StartConsole(cmd, 80, 24, 0, env); err == nil {
		_, err1 := c.WaitOutput("SIZE:80x24", 10*time.Second)
		rerr := c.Resize(100, 30)
		_, err2 := c.WaitOutput("SIZE:100x30", 10*time.Second)
		rows = append(rows, Row{"resize 80x24 to 100x30", fmt.Sprintf("start %s, resize %s, seen %s", errString(err1), errString(rerr), errString(err2))})
		c.Close()
	}
	// Killing the tree.
	cmd, env = childCmdline("tree")
	if c, err := StartConsole(cmd, 80, 24, 0, env); err == nil {
		out, err := c.WaitOutput("\r\n", 10*time.Second)
		pid := 0
		if k := strings.Index(string(out), "GRANDCHILD:"); k >= 0 {
			pid, _ = strconv.Atoi(strings.TrimSpace(strings.SplitN(string(out[k+11:]), "\r", 2)[0]))
		}
		res := "no grandchild: " + errString(err)
		if pid != 0 {
			kerr := c.KillTree()
			alive := processAlive(uint32(pid), 5*time.Second)
			res = fmt.Sprintf("TerminateJobObject %s; grandchild alive after: %v", errString(kerr), alive)
		}
		rows = append(rows, Row{"kill the whole tree (job object)", res})
		c.Close()
	}
	return rows
}

func errString(err error) string {
	if err == nil {
		return "ok"
	}
	return err.Error()
}

// processAlive reports whether pid still runs after waiting up to timeout for it to end.
func processAlive(pid uint32, timeout time.Duration) bool {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, pid)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	ev, _ := windows.WaitForSingleObject(h, uint32(timeout.Milliseconds()))
	return ev != windows.WAIT_OBJECT_0
}

// currentUserSID returns this process's user SID.
func currentUserSID() (*windows.SID, error) {
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	return u.User.Sid.Copy()
}

// processUserSID returns the user SID of process pid.
func processUserSID(pid uint32) (*windows.SID, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(h)
	var tok windows.Token
	if err := windows.OpenProcessToken(h, windows.TOKEN_QUERY, &tok); err != nil {
		return nil, err
	}
	defer tok.Close()
	u, err := tok.GetTokenUser()
	if err != nil {
		return nil, err
	}
	return u.User.Sid.Copy()
}

func probeIPC() []Row {
	var rows []Row
	me, err := currentUserSID()
	if err != nil {
		return []Row{{"current user", "error: " + err.Error()}}
	}
	sddl := "D:P(A;;GA;;;" + me.String() + ")(A;;GA;;;SY)"
	rows = append(rows, Row{"DACL", "`" + sddl + "` (protected: no inherited ACEs)"})
	rows = append(rows, namedPipeProbe(sddl, me)...)
	rows = append(rows, unixSocketProbe(me)...)
	return rows
}

func namedPipeProbe(sddl string, me *windows.SID) []Row {
	var rows []Row
	name := `\\.\pipe\imux-core-spike-` + strconv.Itoa(os.Getpid())
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return []Row{{"named pipe: DACL", "error: " + err.Error()}}
	}
	sa := &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	n16, _ := windows.UTF16PtrFromString(name)
	srv, err := windows.CreateNamedPipe(n16,
		windows.PIPE_ACCESS_DUPLEX|windows.FILE_FLAG_FIRST_PIPE_INSTANCE,
		windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_WAIT|windows.PIPE_REJECT_REMOTE_CLIENTS,
		1, 4096, 4096, 0, sa)
	if err != nil {
		return []Row{{"named pipe: create with DACL and PIPE_REJECT_REMOTE_CLIENTS", "error: " + err.Error()}}
	}
	rows = append(rows, Row{"named pipe: create with DACL and PIPE_REJECT_REMOTE_CLIENTS", "ok"})
	// The DACL as read back.
	if info, err := windows.GetSecurityInfo(srv, windows.SE_KERNEL_OBJECT, windows.DACL_SECURITY_INFORMATION); err == nil {
		rows = append(rows, Row{"named pipe: DACL read back", "`" + info.String() + "`"})
	}
	connected := make(chan error, 1)
	go func() { connected <- windows.ConnectNamedPipe(srv, nil) }()
	cli, err := windows.CreateFile(n16, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		windows.CloseHandle(srv)
		return append(rows, Row{"named pipe: connect", "error: " + err.Error()})
	}
	if err := <-connected; err != nil && err != windows.ERROR_PIPE_CONNECTED {
		rows = append(rows, Row{"named pipe: accept", "error: " + err.Error()})
	}
	var pid uint32
	if err := windows.GetNamedPipeClientProcessId(srv, &pid); err != nil {
		rows = append(rows, Row{"named pipe: client pid", "error: " + err.Error()})
	} else {
		sid, err := processUserSID(pid)
		rows = append(rows, Row{"named pipe: client pid → token SID", fmt.Sprintf("pid ok (ours: %v); SID equal to ours: %v %s", pid == uint32(os.Getpid()), err == nil && windows.EqualSid(sid, me), errString(err))})
	}
	// The server dies: what does the client see?
	windows.CloseHandle(srv)
	buf := make([]byte, 1)
	var n uint32
	err = windows.ReadFile(cli, buf, &n, nil)
	rows = append(rows, Row{"named pipe: client read after the server closed", errString(err)})
	windows.CloseHandle(cli)
	return rows
}

// sioAFUnixGetPeerPid is SIO_AF_UNIX_GETPEERPID: _WSAIOR(IOC_VENDOR, 256).
const sioAFUnixGetPeerPid = 0x58000100

func unixSocketProbe(me *windows.SID) []Row {
	var rows []Row
	dir, err := os.MkdirTemp("", "imux-spike-")
	if err != nil {
		return []Row{{"AF_UNIX: temp dir", "error: " + err.Error()}}
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "s")
	ln, err := net.Listen("unix", path)
	if err != nil {
		return []Row{{"AF_UNIX: listen", "error: " + err.Error()}}
	}
	rows = append(rows, Row{"AF_UNIX: listen", "ok (" + strconv.Itoa(len(path)) + "-byte path)"})
	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			accepted <- nil
			return
		}
		accepted <- c
	}()
	cli, err := net.Dial("unix", path)
	if err != nil {
		ln.Close()
		return append(rows, Row{"AF_UNIX: dial", "error: " + err.Error()})
	}
	srv := <-accepted
	if srv == nil {
		ln.Close()
		cli.Close()
		return append(rows, Row{"AF_UNIX: accept", "failed"})
	}
	// The peer's pid, then its token's SID.
	raw, err := srv.(*net.UnixConn).SyscallConn()
	var pid uint32
	var ierr error
	if err == nil {
		err = raw.Control(func(fd uintptr) {
			var ret uint32
			ierr = windows.WSAIoctl(windows.Handle(fd), sioAFUnixGetPeerPid, nil, 0,
				(*byte)(unsafe.Pointer(&pid)), uint32(unsafe.Sizeof(pid)), &ret, nil, 0)
		})
	}
	switch {
	case err != nil:
		rows = append(rows, Row{"AF_UNIX: SIO_AF_UNIX_GETPEERPID", "error: " + err.Error()})
	case ierr != nil:
		rows = append(rows, Row{"AF_UNIX: SIO_AF_UNIX_GETPEERPID", "error: " + ierr.Error()})
	default:
		sid, err := processUserSID(pid)
		rows = append(rows, Row{"AF_UNIX: peer pid → token SID", fmt.Sprintf("pid ours: %v; SID equal to ours: %v %s", pid == uint32(os.Getpid()), err == nil && windows.EqualSid(sid, me), errString(err))})
	}
	// The server dies.
	srv.Close()
	ln.Close()
	cli.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, err = cli.Read(make([]byte, 1))
	if err == io.EOF {
		rows = append(rows, Row{"AF_UNIX: client read after the server closed", "EOF"})
	} else {
		rows = append(rows, Row{"AF_UNIX: client read after the server closed", errString(err)})
	}
	cli.Close()
	// The socket file's permissions are the directory's and the file's ACLs.
	if info, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION); err == nil {
		rows = append(rows, Row{"AF_UNIX: socket file DACL (inherited from %TEMP%)", "`" + info.String() + "`"})
	} else {
		rows = append(rows, Row{"AF_UNIX: socket file DACL", "error: " + err.Error()})
	}
	return rows
}

// Evidence runs every probe.
func Evidence() *Report {
	r := &Report{Build: windowsBuild(), Output: map[string][]Row{}, Input: map[string][]Row{}}
	r.Flags = probeFlags()
	r.Output["none"] = probeOutput(0)
	r.Input["none"] = probeInput(0)
	if c, err := StartConsole(`cmd.exe /d /c exit 0`, 80, 24, flagPassthroughMode, nil); err == nil {
		c.Close()
		r.Output["PASSTHROUGH_MODE"] = probeOutput(flagPassthroughMode)
		r.Input["PASSTHROUGH_MODE"] = probeInput(flagPassthroughMode)
	}
	r.Process = probeProcesses()
	r.IPC = probeIPC()
	return r
}
