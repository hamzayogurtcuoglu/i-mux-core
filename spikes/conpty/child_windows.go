package main

import (
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"time"

	"golang.org/x/sys/windows"
)

// childEnv names the mode the spike binary runs in when it is a pseudo console's program.
const childEnv = "CONPTY_SPIKE_CHILD"

// Markers around what a child prints, so the parent can find it in ConPTY's output.
const (
	beginMark = "<<BEGIN>>"
	endMark   = "<<END>>"
)

// runChild runs the child mode named by childEnv, if any, and reports whether it did; the
// caller then exits.
func runChild() bool {
	mode := os.Getenv(childEnv)
	if mode == "" {
		return false
	}
	switch {
	case len(mode) > 5 && mode[:5] == "emit:":
		b, _ := hex.DecodeString(mode[5:])
		os.Stdout.WriteString(beginMark)
		os.Stdout.Write(b)
		os.Stdout.WriteString(endMark + "\r\n")
		time.Sleep(500 * time.Millisecond)
	case mode == "echo-input":
		echoInput()
	case mode == "tree":
		cmd := exec.Command(os.Args[0])
		cmd.Env = append(os.Environ(), childEnv+"=sleep")
		if err := cmd.Start(); err != nil {
			fmt.Printf("ERROR:%v\r\n", err)
			os.Exit(1)
		}
		fmt.Printf("GRANDCHILD:%d\r\n", cmd.Process.Pid)
		time.Sleep(60 * time.Second)
	case mode == "sleep":
		time.Sleep(60 * time.Second)
	case mode == "size":
		reportSize()
	case len(mode) > 5 && mode[:5] == "exit:":
		n, _ := strconv.Atoi(mode[5:])
		os.Exit(n)
	}
	return true
}

// echoInput puts the console input in VT mode and prints, in hex, every chunk it reads.
func echoInput() {
	in := windows.Handle(os.Stdin.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(in, &mode); err == nil {
		mode &^= windows.ENABLE_LINE_INPUT | windows.ENABLE_ECHO_INPUT | windows.ENABLE_PROCESSED_INPUT
		mode |= windows.ENABLE_VIRTUAL_TERMINAL_INPUT
		windows.SetConsoleMode(in, mode)
	}
	os.Stdout.WriteString("READY\r\n")
	buf := make([]byte, 256)
	for {
		n, err := os.Stdin.Read(buf)
		if n > 0 {
			fmt.Printf("IN:%s\r\n", hex.EncodeToString(buf[:n]))
		}
		if err != nil {
			return
		}
	}
}

// reportSize prints the console's size whenever it changes.
func reportSize() {
	out := windows.Handle(os.Stdout.Fd())
	last := ""
	for range 400 {
		var info windows.ConsoleScreenBufferInfo
		if err := windows.GetConsoleScreenBufferInfo(out, &info); err == nil {
			w := info.Window
			s := fmt.Sprintf("SIZE:%dx%d", w.Right-w.Left+1, w.Bottom-w.Top+1)
			if s != last {
				fmt.Print(s + "\r\n")
				last = s
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
}
