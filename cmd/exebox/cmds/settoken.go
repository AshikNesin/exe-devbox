package cmds

import (
	"github.com/ashiknesin/exebox/internal/output"
	"github.com/spf13/cobra"
)

func newSetTokenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set-token <token>",
		Short: "Store an exe.dev API token so exebox can auto-register domains",
		Long: `Stores an exe.dev HTTPS API token in ~/.exebox/config.json. exebox uses it
to call "domain add" automatically via the exe.dev HTTPS API, removing the
manual registration step from exebox new.

Create a token scoped to ONLY "domain add" by running this on your local
machine (where ssh exe.dev works):

  ssh exe.dev ssh-key generate-api-key --cmds="domain add" --exp=never --label=exebox

Then paste the resulting token here:

  exebox set-token exe0.eyJjbW...your-token...

The token is stored in plaintext in config.json. It can only add domains —
nothing else.
`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			token := args[0]
			p, err := paths()
			if err != nil {
				return err
			}
			if err := p.EnsureDirs(); err != nil {
				return err
			}
			cfg, _ := p.Load()
			cfg.APIToken = token
			if err := p.Save(cfg); err != nil {
				return err
			}
			output.Global.OK("API token saved to %s", p.Config)
			output.Global.Info("exebox new will now auto-register domains with exe.dev")
			return nil
		},
	}
	return cmd
}