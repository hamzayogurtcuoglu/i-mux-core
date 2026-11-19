//go:build !darwin && !linux && !windows

package pty

// New returns this system's PTY implementation; Windows' ConPTY one comes with task C8.1.
func New() PTY { return unsupported{} }

type unsupported struct{}

func (unsupported) Start(Spec) (Proc, error) { return nil, ErrUnsupported }
