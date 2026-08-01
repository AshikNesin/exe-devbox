// Command exebox automates multi-project dev behind nginx + portless on an
// exe.dev VM. See docs/PRD.md.
package main

import (
	"fmt"
	"os"

	"github.com/ashiknesin/exebox/cmd/exebox/cmds"
)

// Version is set via -ldflags at build time.
var Version = "dev"

func main() {
	root := cmds.NewRoot(Version)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
