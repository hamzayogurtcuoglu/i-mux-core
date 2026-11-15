//go:build !windows

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "conpty: this spike runs on Windows only")
	os.Exit(1)
}
