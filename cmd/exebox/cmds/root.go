// Package cmds holds the cobra command tree for the exebox CLI.
package cmds

import (
	"github.com/ashiknesin/exebox/internal/config"
	"github.com/ashiknesin/exebox/internal/output"
	"github.com/spf13/cobra"
)

// globalFlags are the shared --config/--json/--yes/-v values, populated by the
// root PersistentPreRun and read by subcommands via gflags().
type globalFlags struct {
	ConfigDir string
	JSON      bool
	Yes       bool
	Verbose   bool
}

var gflags globalFlags

// NewRoot builds the root command and wires subcommands.
func NewRoot(version string) *cobra.Command {
	root := &cobra.Command{
		Use:           "exebox",
		Short:         "Manage multi-project dev behind nginx + portless on an exe.dev VM",
		Long: `exebox automates bringing up an exe.dev VM for multi-project dev: it installs
nginx + portless, wires per-project public subdomains, and tells exe.dev to
route them here.

See ` + "`exebox <cmd> --help`" + ` and docs/PRD.md.`,
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			output.Global.JSON = gflags.JSON
			if gflags.JSON {
				output.Global.Color = false
			}
			if gflags.Verbose {
				// verbose currently only re-enables color in weird combos; placeholder.
			}
			return nil
		},
	}

	root.PersistentFlags().StringVar(&gflags.ConfigDir, "config", "", "exebox config dir (default ~/.exebox)")
	root.PersistentFlags().BoolVar(&gflags.JSON, "json", false, "machine-readable JSON output")
	root.PersistentFlags().BoolVar(&gflags.Yes, "yes", false, "skip confirmation prompts")
	root.PersistentFlags().BoolVarP(&gflags.Verbose, "verbose", "v", false, "verbose output")

	root.AddCommand(newDoctorCmd())
	root.AddCommand(newStatusCmd())
	root.AddCommand(newSetupCmd())
	root.AddCommand(newNewCmd())
	root.AddCommand(newDevCmd())
	root.AddCommand(newNginxCmd())
	root.AddCommand(newSetTokenCmd())
	return root
}

// paths resolves the config Paths for this invocation (honoring --config).
func paths() (config.Paths, error) {
	p, err := config.New(gflags.ConfigDir)
	if err != nil {
		return config.Paths{}, err
	}
	return p, nil
}
