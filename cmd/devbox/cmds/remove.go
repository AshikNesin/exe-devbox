package cmds

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ashiknesin/exe-devbox/internal/cloudflare"
	"github.com/ashiknesin/exe-devbox/internal/config"
	"github.com/ashiknesin/exe-devbox/internal/dns"
	"github.com/ashiknesin/exe-devbox/internal/exeapi"
	"github.com/ashiknesin/exe-devbox/internal/nginx"
	"github.com/ashiknesin/exe-devbox/internal/notify"
	"github.com/ashiknesin/exe-devbox/internal/output"
	"github.com/ashiknesin/exe-devbox/internal/reflection"
	"github.com/spf13/cobra"
)

func newRemoveCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:     "remove",
		Aliases: []string{"rm", "delete"},
		Short:   "Tear down a project domain (DNS + exe.dev + nginx route + state)",
		Long: `Remove a project from devbox. For each domain it:
  1. deletes the DNS CNAME record (Cloudflare API if available, else prints
     manual instructions)
  2. unregisters the domain from exe.dev (API if token set, else prints the
     shell command to run)
  3. removes the nginx server block and reloads nginx
  4. removes the project from devbox state (~/.devbox/state/domains.json)

The codebase directory under ~/Code is NOT touched.

Usage:
  devbox remove <project-or-domain>   remove by project name or FQDN
  devbox remove                        interactive: pick from registered projects`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := ""
			if len(args) > 0 {
				key = args[0]
			}
			res := runRemove(cmd.Context(), key, yes)
			output.Global.Print(output.Result{OK: res.ok, Exit: exitCode(res.ok), Data: res.report, Message: res.errMsg})
			if !res.ok {
				return fmt.Errorf("remove did not complete: %s", res.errMsg)
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation prompt")
	return cmd
}

type removeReport struct {
	VM          reflection.Identity `json:"vm"`
	Project     string              `json:"project"`
	Domains     []string            `json:"domains"`
	DNS         []dns.Result        `json:"dns,omitempty"`
	Records     []removeRecord      `json:"records"`
	NginxConf   string              `json:"nginx_conf"`
	SuggestCmds []suggestReport     `json:"suggest_cmds,omitempty"`
}

type removeRecord struct {
	Domain string `json:"domain"`
	Action string `json:"action"` // "deleted" | "not-found" | "manual"
	Detail string `json:"detail"`
}

type removeResult struct {
	report removeReport
	ok     bool
	errMsg string
}

func runRemove(ctx context.Context, key string, skipConfirm bool) removeResult {
	out := output.Global
	p, err := paths()
	if err != nil {
		return removeResult{ok: false, errMsg: err.Error()}
	}

	// Load state + identity.
	cfg, _ := p.Load()
	id := reflection.Identity{Name: cfg.VMName, Email: cfg.Email, DefaultPort: cfg.DefaultPort}
	if id.Name == "" {
		rc := reflection.New()
		discovered, err := rc.Discover(ctx)
		if err != nil {
			return removeResult{ok: false, errMsg: "reflection: " + err.Error()}
		}
		id = discovered
	}

	domains, _ := p.LoadDomains()
	if len(domains) == 0 {
		return removeResult{ok: false, errMsg: "no projects registered — nothing to remove"}
	}

	// Resolve which project to remove.
	if key == "" {
		if out.JSON || !output.IsStdinTerminal() {
			return removeResult{ok: false, errMsg: "specify a project or domain (interactive mode needs a TTY)"}
		}
		picked, err := pickRegisteredProject(domains)
		if err != nil {
			return removeResult{ok: false, errMsg: err.Error()}
		}
		key = picked
	}

	entry := config.FindDomain(domains, key)
	if entry == nil {
		return removeResult{ok: false, errMsg: fmt.Sprintf("project or domain %q not found in devbox state (run: devbox status)", key)}
	}

	// Collect all FQDNs for this entry.
	fqdns := strings.Split(entry.Domain, ",")
	for i := range fqdns {
		fqdns[i] = strings.TrimSpace(fqdns[i])
	}

	// Confirm.
	out.Heading(fmt.Sprintf("remove %s", entry.Project))
	out.Info("domains: %s", strings.Join(fqdns, ", "))
	out.Warn("DNS records, exe.dev registration, and nginx routes will be removed.")
	out.Info("the project codebase under ~/Code will NOT be touched.")
	if !skipConfirm {
		if out.JSON || !output.IsStdinTerminal() {
			return removeResult{ok: false, errMsg: "confirmation required — pass --yes to skip (interactive prompt needs a TTY)"}
		}
		if !confirm(fmt.Sprintf("Remove %q?", entry.Project)) {
			return removeResult{ok: false, errMsg: "cancelled"}
		}
	}

	report := removeReport{
		VM:      id,
		Project: entry.Project,
		Domains: fqdns,
	}

	// STEP 1: DNS — delete CNAME records.
	out.Heading("DNS")
	cf := cloudflare.New()
	for _, fqdn := range fqdns {
		r, err := dns.Investigate(fqdn)
		if err != nil {
			out.Warn("dns lookup %s: %s (continuing)", fqdn, err)
			report.Records = append(report.Records, removeRecord{Domain: fqdn, Action: "manual", Detail: "dns lookup failed"})
			continue
		}
		report.DNS = append(report.DNS, r)
		rec := removeRecord{Domain: fqdn}

		switch {
		case r.IsCloudflare() && cf != nil && cf.Available():
			deleted, err := cf.DeleteCNAME(ctx, r.Apex, fqdn)
			if err != nil {
				out.Warn("cloudflare delete %s: %s (continuing)", fqdn, err)
				rec.Action = "manual"
				rec.Detail = fmt.Sprintf("delete failed: %s", err)
			} else if deleted {
				rec.Action = "deleted"
				rec.Detail = fmt.Sprintf("CNAME %s removed (via %s)", fqdn, cf.Mode())
				out.OK("deleted CNAME %s (via %s)", fqdn, cf.Mode())
			} else {
				rec.Action = "not-found"
				rec.Detail = fmt.Sprintf("no DNS record for %s", fqdn)
				out.Info("no DNS record for %s (already gone)", fqdn)
			}
		default:
			rec.Action = "manual"
			rec.Detail = fmt.Sprintf("delete CNAME %s at your DNS provider", fqdn)
			out.Warn("delete this CNAME manually at your DNS provider:")
			out.Block(fmt.Sprintf("Type:  CNAME\nName:  %s        (relative to %s)", r.HostLabel, r.Apex))
		}
		report.Records = append(report.Records, rec)
	}

	// STEP 2: exe.dev — unregister domains.
	out.Heading("exe.dev")
	api := exeapi.New(cfg.APIToken)
	for _, fqdn := range fqdns {
		shell := domainRemove(id.Name, fqdn)
		report.SuggestCmds = append(report.SuggestCmds, suggestReport{Kind: "domain-remove", Shell: shell})
		if api != nil && api.Available() {
			out.Step("unregistering %s via exe.dev API ...", fqdn)
			resp, err := api.DomainRemove(id.Name, fqdn)
			if err != nil {
				out.Warn("API removal failed: %s", err)
				out.Info("open https://exe.dev/shell and run:")
				out.Block(shell)
			} else {
				out.OK("unregistered %s from exe.dev", fqdn)
				if resp != "" {
					out.Info("%s", resp)
				}
			}
		} else {
			out.Info("unregister %s — open https://exe.dev/shell and run:", fqdn)
			out.Block(shell)
		}
	}

	// STEP 3: nginx — remove server block + reload.
	out.Heading("nginx")
	confPath := p.ProjectConf(entry.Project)
	if _, err := os.Stat(confPath); err == nil {
		if err := os.Remove(confPath); err != nil {
			return removeResult{report: report, ok: false, errMsg: "remove nginx conf: " + err.Error()}
		}
		report.NginxConf = confPath
		out.OK("removed %s", confPath)
	} else {
		out.Info("no nginx conf for %s (already gone)", entry.Project)
	}
	if _, err := nginx.Reload(); err != nil {
		out.Warn("nginx reload: %s (continuing)", err)
	}
	out.OK("nginx reloaded")

	// STEP 4: state — remove entry.
	domains, changed := config.RemoveDomain(domains, entry.Domain)
	if changed {
		_ = p.SaveDomains(domains)
		out.OK("removed %q from devbox state", entry.Project)
	}

	// Push notification.
	n := notify.Default()
	if n.Available() {
		msg := fmt.Sprintf("%s removed from %s", strings.Join(fqdns, ", "), id.Name)
		if err := n.Send(ctx, "🌐 Domain removed", msg); err != nil {
			out.Warn("notify: %s", err)
		}
	}

	return removeResult{report: report, ok: true}
}

// pickRegisteredProject shows the list of registered projects and prompts for
// one. Returns the project name.
func pickRegisteredProject(domains []config.Domain) (string, error) {
	out := output.Global
	out.Heading("pick a project to remove")
	for i, d := range domains {
		out.Line(fmt.Sprintf("  %d. %s  (%s)", i+1, d.Project, d.Domain))
	}
	fmt.Fprint(out.ErrW, "pick [1-"+fmt.Sprint(len(domains))+"]: ")

	var input string
	fmt.Fscanln(os.Stdin, &input)
	for i, d := range domains {
		if input == fmt.Sprint(i+1) || strings.EqualFold(input, d.Project) {
			return d.Project, nil
		}
	}
	return "", fmt.Errorf("invalid selection: %q", input)
}

// confirm is defined in setup.go and shared across commands.
