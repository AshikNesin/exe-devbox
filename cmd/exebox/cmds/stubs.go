package cmds

import (
	"fmt"

	"github.com/ashiknesin/exebox/internal/output"
	"github.com/spf13/cobra"
)

// stub returns a command that prints a "not implemented yet" message.
// Used for planned-but-unbuilt subcommands so the CLI compiles and the
// help tree lists them.
func newNginxCmd() *cobra.Command {
	return stub("nginx", "Manage the exebox-managed nginx config (show/edit/reload)")
}

func stub(use, short string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			msg := fmt.Sprintf("%q not implemented yet", use)
			output.Global.Err("%s", msg)
			output.Global.Print(output.Result{OK: false, Exit: 1, Message: msg})
			return fmt.Errorf("%s", msg)
		},
	}
}