package main

import (
	"fmt"
	"os"
)

func main() {
	if runChild() {
		os.Exit(0)
	}
	fmt.Print(Evidence().Markdown())
}
