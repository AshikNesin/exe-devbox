package cmds

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ashiknesin/exebox/internal/output"
	"github.com/ashiknesin/exebox/internal/portless"
	"github.com/ashiknesin/exebox/internal/reflection"
	"github.com/ashiknesin/exebox/internal/system"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show VM identity, proxy state, registered domains (live), and portless routes",
		Long: `Print a live snapshot of what exebox has wired up on this VM:
  - VM identity (from reflection)
  - proxy ports: nginx (default port) + portless daemon
  - each registered domain probed through nginx (200 live / 5xx backend down / 404 no route)
  - portless routes (portless list)

Use --no-probe to skip the per-domain HTTP checks (offline/instant).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			noProbe, _ := cmd.Flags().GetBool("no-probe")
			report, ok := runStatus(cmd.Context(), noProbe)
			output.Global.Print(output.Result{OK: ok, Exit: exitCode(ok), Data: report})
			return nil // status never hard-fails; the report carries per-domain state
		},
	}
	cmd.Flags().BoolP("no-probe", "n", false, "skip live HTTP probing of registered domains")
	return cmd
}

// statusReport is the JSON payload for `exebox status`.
type statusReport struct {
	VM            *reflection.Identity `json:"vm,omitempty"`
	NginxPort     int                  `json:"nginx_port"`
	NginxActive   bool                 `json:"nginx_active"`
	PortlessPort  int                  `json:"portless_port"`
	PortlessActive bool                `json:"portless_active"`
	Domains       []domainStatus       `json:"domains"`
	Portless      string               `json:"portless_routes,omitempty"`
	AllLive       bool                 `json:"all_live"`
}

// domainStatus is one registered domain + its live HTTP probe result.
type domainStatus struct {
	Domain   string `json:"domain"`
	Project  string `json:"project,omitempty"`
	Backend  string `json:"backend,omitempty"`
	Probed   bool   `json:"probed"`     // false when --no-probe
	Live     bool   `json:"live"`
	HTTPCode int    `json:"http_code,omitempty"`
	Detail   string `json:"detail,omitempty"` // human hint for non-live domains
}

func runStatus(ctx context.Context, noProbe bool) (statusReport, bool) {
	p, _ := paths()

	r := statusReport{AllLive: true}

	// VM identity (best-effort).
	rc := reflection.New()
	if id, err := rc.Discover(ctx); err == nil {
		r.VM = &id
	}

	// Config (cached ports).
	cfg, _ := p.Load()
	r.NginxPort = cfg.NginxPort
	r.PortlessPort = cfg.PortlessPort
	if r.NginxPort == 0 && r.VM != nil {
		r.NginxPort = r.VM.DefaultPort
	}
	if r.PortlessPort == 0 {
		r.PortlessPort = portless.DaemonPort
	}

	// Service state.
	if c := system.UnitActive("nginx"); c.Pass {
		r.NginxActive = true
	}
	r.PortlessActive = portless.ServiceActive()

	// Registered domains + live probe.
	domains, _ := p.LoadDomains()
	for _, d := range domains {
		for _, fqdn := range strings.Split(d.Domain, ",") {
			fqdn = strings.TrimSpace(fqdn)
			ds := domainStatus{Domain: fqdn, Project: d.Project, Backend: d.Backend}
			if !noProbe && r.NginxPort != 0 {
				ds.Probed = true
				code := probeDomain(r.NginxPort, fqdn)
				ds.HTTPCode = code
				ds.Live, ds.Detail = classify(code, d.Backend)
				if !ds.Live {
					r.AllLive = false
				}
			}
			r.Domains = append(r.Domains, ds)
		}
	}
	if len(r.Domains) == 0 {
		r.AllLive = false // nothing wired up yet
	}

	// Portless routes (raw `portless list`).
	if list, err := portless.List(); err == nil {
		r.Portless = strings.TrimSpace(list)
	}

	renderStatus(r)
	return r, r.AllLive
}

// probeDomain hits nginx on localhost with the Host header set and returns the
// HTTP status code (0 on connection failure).
func probeDomain(nginxPort int, host string) int {
	url := fmt.Sprintf("http://127.0.0.1:%d/", nginxPort)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0
	}
	req.Host = host
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// classify turns an HTTP code into (live, humanHint).
//   - 2xx/3xx: live
//   - 502/504: backend down (nginx is fine; the dev server isn't running)
//   - 404:     no nginx route for this Host
//   - 0:       nginx not responding on the port
func classify(code int, backend string) (bool, string) {
	switch {
	case code == 0:
		return false, "nginx not responding"
	case code >= 200 && code < 400:
		return true, ""
	case code == 404:
		return false, "no nginx route"
	case code == 502 || code == 504:
		name := backend
		if name == "" || name == "portless" {
			name = "backend"
		}
		return false, name+" not running"
	default:
		return false, fmt.Sprintf("unexpected HTTP %d", code)
	}
}

func renderStatus(r statusReport) {
	out := output.Global
	out.Heading("exebox status")

	if r.VM != nil {
		out.Info("VM: %s  port: %d  cname: %s", r.VM.Name, r.VM.DefaultPort, r.VM.CNAME())
		if r.VM.Email != "" {
			out.Info("owner: %s", r.VM.Email)
		}
	} else {
		out.Info("VM: (reflection unavailable)")
	}

	// proxy line
	nginxMark := out.Green("✓")
	if !r.NginxActive {
		nginxMark = out.Red("✗")
	}
	portlessMark := out.Green("✓")
	if !r.PortlessActive {
		portlessMark = out.Red("✗")
	}
	out.Line(fmt.Sprintf("  proxy: nginx %s :%d   portless %s :%d",
		nginxMark, r.NginxPort, portlessMark, r.PortlessPort))

	// domains
	if len(r.Domains) == 0 {
		out.Info("domains: (none registered — run: exebox new -d <fqdn>)")
	} else {
		out.Line("")
		out.Line("  domains (live HTTP probe through nginx):")
		for _, d := range r.Domains {
			if !d.Probed {
				back := ""
				if d.Backend != "" {
					back = "  (" + d.Backend + ")"
				}
				out.Line(fmt.Sprintf("    %s %-32s %s%s", out.Cyan("-"), d.Domain, out.Bold("(not probed)")+back, ""))
				continue
			}
			mark := out.Green("●")
			label := "live"
			if !d.Live {
				mark = out.Red("○")
				label = d.Detail
				if label == "" {
					label = "not live"
				}
			}
			code := ""
			if d.HTTPCode != 0 {
				code = fmt.Sprintf(" [HTTP %d]", d.HTTPCode)
			}
			back := ""
			if d.Backend != "" {
				back = "  (" + d.Backend + ")"
			}
			out.Line(fmt.Sprintf("    %s %-32s %s%s%s", mark, d.Domain, out.Bold(label), code, back))
		}
	}

	// portless routes
	if r.Portless != "" {
		out.Line("")
		out.Line("  portless routes:")
		for _, line := range strings.Split(r.Portless, "\n") {
			if strings.TrimSpace(line) != "" {
				out.Line("    " + line)
			}
		}
	}

	// summary
	out.Line("")
	if len(r.Domains) == 0 {
		out.Warn("no domains registered yet")
	} else {
		probed, down := 0, 0
		for _, d := range r.Domains {
			if d.Probed {
				probed++
				if !d.Live {
					down++
				}
			}
		}
		if probed == 0 {
			out.Info("%d domain(s) registered (not probed)", len(r.Domains))
		} else if down == 0 {
			out.OK("all %d probed domain(s) live", probed)
		} else {
			out.Err("%d/%d domain(s) not live", down, probed)
		}
	}
}
