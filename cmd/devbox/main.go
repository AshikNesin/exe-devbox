// Command devbox automates multi-project dev behind nginx + portless on an
// exe.dev VM. See docs/PRD.md.
package main

import (
	"fmt"
	"os"

	"github.com/ashiknesin/exe-devbox/cmd/devbox/cmds"
)

// Version is set via -ldflags at build time.
var Version = "dev"

func main() {
	root := cmds.NewRoot(Version)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	cmds.MaybeInstallAlias()
}
