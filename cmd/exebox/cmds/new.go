package cmds

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/ashiknesin/exebox/internal/cloudflare"
	"github.com/ashiknesin/exebox/internal/config"
	"github.com/ashiknesin/exebox/internal/dns"
	"github.com/ashiknesin/exebox/internal/nginx"
	"github.com/ashiknesin/exebox/internal/output"
	"github.com/ashiknesin/exebox/internal/reflection"
	"github.com/ashiknesin/exebox/internal/system"
	"github.com/spf13/cobra"
)

func newNewCmd() *cobra.Command {
	var (
		domains  []string
		project  string
		to       string
		public   bool
		wait     bool
	)
	cmd := &cobra.Command{
		Use:   "new",
		Short: "Onboard a new project domain (DNS + exe.dev + nginx route)",
		Long: `Wire up a public domain to this VM. For each domain it:
  1. detects the DNS provider of the apex (Cloudflare / Domain Connect / manual)
  2. adds the CNAME -> <vm>.exe.xyz (Cloudflare API if you have a token, else
     prints exact manual instructions), with a one-click Domain Connect link
     where supported
  3. prints the exe.dev suggest link to register the domain (owner key needed)
  4. writes the nginx server block and reloads nginx

Multiple domains sharing one backend: repeat --domain (e.g. Shelley on two
hostnames). Backend: --to portless (default) or --to loopback:<port>.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := newOpts{domains: domains, project: project, to: to, public: public, wait: wait}
			if err := opts.defaults(); err != nil {
				output.Global.Print(output.Result{OK: false, Exit: 1, Message: err.Error()})
				return err
			}
			res := runNew(cmd.Context(), opts)
			output.Global.Print(output.Result{OK: res.ok, Exit: exitCode(res.ok), Data: res.report, Message: res.errMsg})
			if !res.ok {
				return fmt.Errorf("new did not complete: %s", res.errMsg)
			}
			return nil
		},
	}
	cmd.Flags().StringArrayVarP(&domains, "domain", "d", nil, "public FQDN to serve (repeat for multi-domain)")
	cmd.MarkFlagRequired("domain")
	cmd.Flags().StringVar(&project, "project", "", "project/route name (default: first label of first domain)")
	cmd.Flags().StringVar(&to, "to", "portless", "backend: 'portless' (default) or 'loopback:<port>'")
	cmd.Flags().BoolVar(&public, "public", false, "also emit share set-public suggest link")
	cmd.Flags().BoolVar(&wait, "wait", false, "poll DNS until the CNAME resolves to the target")
	return cmd
}

type newOpts struct {
	domains []string
	project string
	to      string
	public  bool
	wait    bool
}

func (o *newOpts) defaults() error {
	if len(o.domains) == 0 {
		return fmt.Errorf("--domain is required")
	}
	for i := range o.domains {
		o.domains[i] = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(o.domains[i])), ".")
	}
	if o.project == "" {
		// first label of first domain
		first := o.domains[0]
		if i := strings.IndexByte(first, '.'); i > 0 {
			o.project = first[:i]
		} else {
			o.project = first
		}
	}
	if o.to == "" {
		o.to = "portless"
	}
	if o.to != "portless" && !strings.HasPrefix(o.to, "loopback:") {
		return fmt.Errorf("--to must be 'portless' or 'loopback:<port>', got %q", o.to)
	}
	return nil
}

type newReport struct {
	VM        reflection.Identity `json:"vm"`
	Project   string              `json:"project"`
	Domains   []string            `json:"domains"`
	Backend   string              `json:"backend"`
	DNS       []dns.Result        `json:"dns"`
	Records   []recordReport      `json:"records"`
	NginxConf string              `json:"nginx_conf"`
	SuggestLinks []suggestReport  `json:"suggest_links,omitempty"`
}

type recordReport struct {
	Domain string `json:"domain"`
	Action string `json:"action"` // "created" | "updated" | "manual" | "dc-link"
	Detail string `json:"detail"`
}

type suggestReport struct {
	Kind string `json:"kind"` // "domain-add" | "set-public"
	Suggest string `json:"suggest"`
	Shell   string `json:"shell,omitempty"`
}

type newResult struct {
	report newReport
	ok     bool
	errMsg string
}

func runNew(ctx context.Context, opts newOpts) newResult {
	out := output.Global
	p, err := paths()
	if err != nil {
		return newResult{ok: false, errMsg: err.Error()}
	}
	if err := p.EnsureDirs(); err != nil {
		return newResult{ok: false, errMsg: err.Error()}
	}

	// identity: prefer saved config, else reflection.
	cfg, _ := p.Load()
	id := reflection.Identity{Name: cfg.VMName, Email: cfg.Email, DefaultPort: cfg.DefaultPort}
	if id.Name == "" {
		rc := reflection.New()
		discovered, err := rc.Discover(ctx)
		if err != nil {
			return newResult{ok: false, errMsg: "reflection: " + err.Error()}
		}
		id = discovered
	}
	nginxPort := cfg.NginxPort
	if nginxPort == 0 {
		nginxPort = id.DefaultPort
	}
	if nginxPort == 0 {
		nginxPort = 8080
	}

	report := newReport{
		VM:      id,
		Project: opts.project,
		Domains: opts.domains,
		Backend: opts.to,
	}

	out.Heading(fmt.Sprintf("onboard %s -> %s", strings.Join(opts.domains, ", "), opts.project))
	out.Info("VM: %s  cname target: %s", id.Name, id.CNAME())

	// STEP A+B: DNS provider detection + CNAME strategy per domain.
	cf := cloudflare.New()
	for _, d := range opts.domains {
		r, err := dns.Investigate(d)
		if err != nil {
			return newResult{report: report, ok: false, errMsg: fmt.Sprintf("dns %s: %s", d, err)}
		}
		report.DNS = append(report.DNS, r)
		rec := recordReport{Domain: d}

		out.Step("dns: %s  apex=%s  provider=%s", d, r.Apex, r.Provider)

		// Cloudflare Domain Connect link (informational; not one-click without onboarding)
		if r.IsCloudflare() && r.DCAPI != "" {
			out.Info("Domain Connect (Cloudflare, needs template onboarding): %s/v2/%s/settings/...", r.DCAPI, r.Apex)
		}

		switch {
		case r.IsCloudflare() && cf != nil && cf.Available():
			// direct API apply (token mode or exe.dev proxy mode)
			target := id.CNAME()
			id2, created, err := cf.UpsertCNAME(ctx, r.Apex, d, target)
			if err != nil {
				return newResult{report: report, ok: false, errMsg: fmt.Sprintf("cloudflare %s: %s", d, err)}
			}
			rec.Action = ternary(created, "created", "updated")
			rec.Detail = fmt.Sprintf("CNAME %s -> %s (id %s, via %s)", d, target, id2, cf.Mode())
			out.OK("CNAME %s %s -> %s (via %s)", ternary(created, "created", "updated"), d, target, cf.Mode())
		default:
			// manual (also the fallback when no CF token)
			rec.Action = "manual"
			target := id.CNAME()
			rec.Detail = fmt.Sprintf("CNAME %s -> %s", d, target)
			out.Warn("add this CNAME manually at your DNS provider:")
			out.Block(manualRecordBlock(d, r, target))
		}
		report.Records = append(report.Records, rec)
	}

	// optional DNS wait
	if opts.wait {
		for _, d := range opts.domains {
			target := id.CNAME()
			out.Step("waiting for %s to resolve to %s ...", d, target)
			if err := waitCNAME(ctx, d, target, 5*time.Minute); err != nil {
				out.Warn("(continuing) %s", err)
			} else {
				out.OK("%s resolves to %s", d, target)
			}
		}
	}

	// STEP D: exe.dev suggest links (owner-only).
	out.Heading("exe.dev registration (owner key required)")
	for _, d := range opts.domains {
		suggest, shell := domainAdd(id.Name, d)
		report.SuggestLinks = append(report.SuggestLinks, suggestReport{Kind: "domain-add", Suggest: suggest, Shell: shell})
		out.Info("register %s:", d)
		out.Line("  " + suggest)
		out.Info("(if the link isn't supported, run in the exe.dev shell:)")
		out.Block(shell)
	}
	if opts.public {
		link := suggestSetPublic(id.Name)
		report.SuggestLinks = append(report.SuggestLinks, suggestReport{Kind: "set-public", Suggest: link})
		out.Info("make VM public:")
		out.Line("  " + link)
	}

	// STEP E: nginx route (on-VM, automated).
	out.Heading("nginx route")
	confPath := p.ProjectConf(opts.project)
	spec := nginx.ProjectSpec{Domains: opts.domains, Project: opts.project, Backend: opts.to}
	conf := nginx.ProjectConf(nginxPort, spec)
	if err := writeFile(confPath, conf, 0o644); err != nil {
		return newResult{report: report, ok: false, errMsg: "write nginx conf: " + err.Error()}
	}
	report.NginxConf = confPath
	out.OK("wrote %s", confPath)

	if _, err := nginx.Reload(); err != nil {
		return newResult{report: report, ok: false, errMsg: "nginx reload: " + err.Error()}
	}
	out.OK("nginx reloaded")

	// record in state
	domains, _ := p.LoadDomains()
	domains = config.UpsertDomain(domains, config.Domain{
		Domain: strings.Join(opts.domains, ","), // primary join for multi-domain
		Project: opts.project, Backend: opts.to,
		Apex: report.DNS[0].Apex, Public: opts.public,
		AddedAt: time.Now().UTC().Format(time.RFC3339),
	})
	_ = p.SaveDomains(domains)

	// STEP F: HMR hint for portless backends.
	if opts.to == "portless" {
		out.Heading("HMR hint")
		out.Info("set in your project launcher so Vite HMR works through the proxy:")
		for _, d := range opts.domains {
			out.Block("export VITE_HMR_URL=" + d)
		}
	}

	// local sanity command
	out.Heading("verify (on-VM)")
	out.Block(fmt.Sprintf("curl -s -H \"Host: %s\" http://127.0.0.1:%d/ | head", opts.domains[0], nginxPort))

	_ = system.Run // keep import
	return newResult{report: report, ok: true}
}

func writeFile(path, content string, mode os.FileMode) error {
	return os.WriteFile(path, []byte(content), mode)
}

// manualRecordBlock renders the copy-paste manual instructions for non-API providers.
func manualRecordBlock(domain string, r dns.Result, target string) string {
	return fmt.Sprintf(`Type:  CNAME
Name:  %s        (relative to %s)
Target: %s
TTL:   300 / auto
Proxy: DNS-only (off)`, r.HostLabel, r.Apex, target)
}

func ternary(b bool, t, f string) string {
	if b {
		return t
	}
	return f
}

// waitCNAME polls until domain resolves (via CNAME chain) to target.
func waitCNAME(ctx context.Context, domain, target string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		cname, err := net.LookupCNAME(domain)
		if err == nil && strings.TrimSuffix(cname, ".") == target {
			return nil
		}
		time.Sleep(5 * time.Second)
	}
	return fmt.Errorf("timeout waiting for %s -> %s", domain, target)
}
