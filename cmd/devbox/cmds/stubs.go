package cmds

import (
	"fmt"

	"github.com/ashiknesin/exe-devbox/internal/output"
	"github.com/spf13/cobra"
)

// stubs for commands implemented in later milestones. They print a
// "not implemented yet" message and exit 1 so the CLI compiles end-to-end
// during early milestones.

func newStatusCmd() *cobra.Command {
	return stub("status", "Show VM identity, ports, portless routes, and registered domains")
}

func newNginxCmd() *cobra.Command {
	return stub("nginx", "Manage the devbox-managed nginx config (show/edit/reload)")
}

func stub(use, short string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			output.Global.Print(output.Result{OK: false, Exit: 1, Message: fmt.Sprintf("%q not implemented yet", use)})
			return fmt.Errorf("%q not implemented yet", use)
		},
	}
}
