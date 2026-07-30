package cmds

import (
	"context"
	"fmt"

	"github.com/ashiknesin/exebox/internal/output"
	"github.com/ashiknesin/exebox/internal/reflection"
	"github.com/ashiknesin/exebox/internal/system"
	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run health checks (reflection, deps, services, ports)",
		Long: `Run a battery of checks and print a ✓/✗ table: VM identity from
reflection, binaries on PATH (node/npm/portless/nginx), systemd units,
and whether :8080/:8888 are listening. Exits non-zero if any check fails.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			report, ok := runDoctor(cmd.Context())
			output.Global.Print(output.Result{
				OK:   ok,
				Exit: exitCode(ok),
				Data: report,
			})
			if !ok {
				return fmt.Errorf("some checks failed")
			}
			return nil
		},
	}
	return cmd
}

// doctorReport is the JSON payload for `exebox doctor`.
type doctorReport struct {
	VM        *reflection.Identity `json:"vm,omitempty"`
	Checks    []system.Check       `json:"checks"`
	AllPass   bool                 `json:"all_pass"`
}

func runDoctor(ctx context.Context) (doctorReport, bool) {
	p, _ := paths()
	report := doctorReport{}

	// 1. reflection identity (best-effort; not fatal for doctor).
	rc := reflection.New()
	id, err := rc.Discover(ctx)
	if err != nil {
		report.Checks = append(report.Checks, system.Check{Name: "reflection", Pass: false, Detail: err.Error()})
	} else {
		report.VM = &id
		report.Checks = append(report.Checks, system.Check{Name: "reflection", Pass: true,
			Detail: fmt.Sprintf("%s (port %d, cname %s)", id.Name, id.DefaultPort, id.CNAME())})
	}

	// 2. binaries on PATH.
	for _, b := range []string{"node", "npm", "portless", "nginx"} {
		report.Checks = append(report.Checks, system.BinaryOnPATH(b))
	}

	// 3. systemd units.
	for _, u := range []string{"portless", "nginx"} {
		report.Checks = append(report.Checks, system.UnitActive(u))
	}

	// 4. ports.
	for _, port := range []int{8080, 8888} {
		report.Checks = append(report.Checks, system.PortListening(port))
	}

	// 5. reflection port vs configured nginx port.
	if cfg, _ := p.Load(); cfg.NginxPort != 0 && id.DefaultPort != 0 {
		match := cfg.NginxPort == id.DefaultPort
		report.Checks = append(report.Checks, system.Check{
			Name:   "reflection port == nginx port",
			Pass:   match,
			Detail: fmt.Sprintf("reflection=%d nginx=%d", id.DefaultPort, cfg.NginxPort),
		})
	}

	// tally.
	allPass := true
	for _, c := range report.Checks {
		if !c.Pass {
			allPass = false
		}
	}
	report.AllPass = allPass

	renderDoctor(report)
	return report, allPass
}

func renderDoctor(r doctorReport) {
	out := output.Global
	out.Heading("exebox doctor")
	if r.VM != nil {
		out.Info("VM: %s  port: %d  cname: %s", r.VM.Name, r.VM.DefaultPort, r.VM.CNAME())
		if r.VM.Email != "" {
			out.Info("owner: %s", r.VM.Email)
		}
	}
	for _, c := range r.Checks {
		var mark string
		if c.Pass {
			mark = out.Green("✓")
		} else {
			mark = out.Red("✗")
		}
		out.Line(fmt.Sprintf("  %s %-34s %s", mark, c.Name, c.Detail))
	}
	if r.AllPass {
		out.OK("all checks passed")
	} else {
		out.Err("some checks failed")
	}
}

func exitCode(ok bool) int {
	if ok {
		return 0
	}
	return 1
}
