//go:build windows

package main

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

func makeRaw() (func(), error) {
	in, out := windows.Handle(os.Stdin.Fd()), windows.Handle(os.Stdout.Fd())
	var inMode, outMode uint32
	if err := windows.GetConsoleMode(in, &inMode); err != nil {
		return nil, err
	}
	if err := windows.GetConsoleMode(out, &outMode); err != nil {
		return nil, err
	}
	raw := inMode&^(windows.ENABLE_ECHO_INPUT|windows.ENABLE_LINE_INPUT|windows.ENABLE_PROCESSED_INPUT) | windows.ENABLE_VIRTUAL_TERMINAL_INPUT
	if err := windows.SetConsoleMode(in, raw); err != nil {
		return nil, err
	}
	if err := windows.SetConsoleMode(out, outMode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING|windows.DISABLE_NEWLINE_AUTO_RETURN); err != nil {
		return nil, err
	}
	return func() {
		_ = windows.SetConsoleMode(in, inMode)
		_ = windows.SetConsoleMode(out, outMode)
	}, nil
}

func size() (cols, rows int) {
	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(windows.Handle(os.Stdout.Fd()), &info); err != nil {
		return 80, 24
	}
	return int(info.Window.Right - info.Window.Left + 1), int(info.Window.Bottom - info.Window.Top + 1)
}

// onResize polls the console's size: a console program sees resizes as input events, which the
// raw reader does not separate out.
func onResize() <-chan os.Signal {
	ch := make(chan os.Signal, 1)
	go func() {
		c, r := size()
		for range time.Tick(50 * time.Millisecond) {
			if nc, nr := size(); nc != c || nr != r {
				c, r = nc, nr
				select {
				case ch <- os.Interrupt:
				default:
				}
			}
		}
	}()
	return ch
}

func killSelf(string) error { return errors.New("no signals on Windows") }
