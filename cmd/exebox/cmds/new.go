package cmds

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ashiknesin/exebox/internal/cloudflare"
	"github.com/ashiknesin/exebox/internal/config"
	"github.com/ashiknesin/exebox/internal/dns"
	"github.com/ashiknesin/exebox/internal/exeapi"
	"github.com/ashiknesin/exebox/internal/nginx"
	"github.com/ashiknesin/exebox/internal/notify"
	"github.com/ashiknesin/exebox/internal/output"
	"github.com/ashiknesin/exebox/internal/portless"
	"github.com/ashiknesin/exebox/internal/reflection"
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
  1. detects the DNS provider of the apex (Cloudflare / manual)
  2. adds the CNAME -> <vm>.exe.xyz (Cloudflare API if you have a token, else
     prints exact manual instructions)
  3. prints the exe.dev suggest link to register the domain (owner key needed)
  4. writes the nginx server block and reloads nginx

Usage modes:
  exebox new -d myapp.example.com            explicit domain
  exebox new groot                            derive <project>.<default-domain>
  exebox new                                  interactive: pick from ~/Code projects

If --domain is omitted, the FQDN is derived as <project>.<default-domain>
where <default-domain> is set during 'exebox setup --default-domain'.
Multiple domains: repeat --domain (shares one backend).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 && project == "" {
				project = args[0]
			}
			opts := newOpts{domains: domains, project: project, to: to, public: public, wait: wait}
			resolved, err := opts.resolve(domains)
			if err != nil {
				output.Global.Err("%s", err)
				output.Global.Print(output.Result{OK: false, Exit: 1, Message: err.Error()})
				return err
			}
			res := runNew(cmd.Context(), resolved)
			output.Global.Print(output.Result{OK: res.ok, Exit: exitCode(res.ok), Data: res.report, Message: res.errMsg})
			if !res.ok {
				return fmt.Errorf("new did not complete: %s", res.errMsg)
			}
			return nil
		},
	}
	cmd.Flags().StringArrayVarP(&domains, "domain", "d", nil, "public FQDN to serve (repeat for multi-domain; default: <project>.<default-domain>)")
	cmd.Flags().StringVar(&project, "project", "", "project/route name (default: first label of domain, or picked interactively)")
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

// resolve fills in missing domain/project via defaults + interactive prompts.
// At least one of --domain, --project, or args[0] must be provided (or stdin
// must be a TTY for the interactive picker). Returns resolved opts.
func (o *newOpts) resolve(flagsDomains []string) (newOpts, error) {
	out := output.Global

	// Normalize what we have so far.
	o.domains = flagsDomains
	for i := range o.domains {
		o.domains[i] = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(o.domains[i])), ".")
	}
	if o.to == "" {
		o.to = "portless"
	}
	if o.to != "portless" && !strings.HasPrefix(o.to, "loopback:") {
		return *o, fmt.Errorf("--to must be 'portless' or 'loopback:<port>', got %q", o.to)
	}

	// Case 1: domains given — derive project from first domain (original behavior).
	if len(o.domains) > 0 {
		if o.project == "" {
			first := o.domains[0]
			if i := strings.IndexByte(first, '.'); i > 0 {
				o.project = first[:i]
			} else {
				o.project = first
			}
		}
		return *o, nil
	}

	// Case 2: project given (positional arg or --project) but no domain.
	// Derive FQDN from config's default_domain.
	if o.project != "" {
		cfg, _ := loadConfig()
		if cfg.DefaultDomain == "" {
			return *o, fmt.Errorf("no --domain given and no default domain set; run: exebox setup --default-domain <apex>")
		}
		o.domains = []string{o.project + "." + cfg.DefaultDomain}
		out.Info("derived domain: %s", o.domains[0])
		return *o, nil
	}

	// Case 3: nothing given — interactive project picker (needs a TTY).
	if out.JSON || !output.IsStdinTerminal() {
		return *o, fmt.Errorf("--domain or --project required (interactive mode needs a TTY)")
	}
	picked, err := pickProjectInteractive()
	if err != nil {
		return *o, err
	}
	o.project = picked

	cfg, _ := loadConfig()
	if cfg.DefaultDomain == "" {
		return *o, fmt.Errorf("no default domain set; run: exebox setup --default-domain <apex>")
	}
	o.domains = []string{o.project + "." + cfg.DefaultDomain}
	out.OK("project: %s  domain: %s", o.project, o.domains[0])
	return *o, nil
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
	Action string `json:"action"` // "created" | "updated" | "manual"
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
		nginxPort = defaultNginxPort // fallback when reflection + config both lack a port
	}
	portlessPort := cfg.PortlessPort
	if portlessPort == 0 {
		portlessPort = portless.DaemonPort
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

	// STEP D: exe.dev domain registration.
	// If $EXE_API_TOKEN is set (and scoped to domain add), we register
	// automatically via the HTTPS API. Otherwise we fall back to manual:
	// printing the shell command to paste at https://exe.dev/shell.
	out.Heading("exe.dev registration")
	api := exeapi.New(cfg.APIToken)
	for _, d := range opts.domains {
		shell := domainAdd(id.Name, d)
		report.SuggestLinks = append(report.SuggestLinks, suggestReport{Kind: "domain-add", Shell: shell})
		if api != nil && api.Available() {
			out.Step("registering %s via exe.dev API ...", d)
			resp, err := domainAddRetry(ctx, api, id.Name, d)
			if err != nil {
				out.Warn("API registration failed: %s", err)
				out.Info("fall back — open https://exe.dev/shell and run:")
				out.Block(shell)
			} else {
				out.OK("registered %s with exe.dev", d)
				if resp != "" {
					out.Info("%s", resp)
				}
			}
		} else {
			out.Info("register %s — open https://exe.dev/shell and run:", d)
			out.Block(shell)
		}
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
	spec := nginx.ProjectSpec{Domains: opts.domains, Project: opts.project, Backend: opts.to, PortlessPort: portlessPort}
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

	// Push notification: domain wired up.
	n := notify.Default()
	if n.Available() {
		url := fmt.Sprintf("https://%s/", opts.domains[0])
		msg := fmt.Sprintf("%s -> %s on %s", strings.Join(opts.domains, ", "), opts.project, id.Name)
		if err := n.SendWithURL(ctx, "🌐 Domain added", msg, url); err != nil {
			out.Warn("notify: %s", err)
		}
	}

	return newResult{report: report, ok: true}
}

func writeFile(path, content string, mode os.FileMode) error {
	return os.WriteFile(path, []byte(content), mode)
}

// loadConfig reads the persisted config (best-effort).
func loadConfig() (config.File, error) {
	p, err := paths()
	if err != nil {
		return config.File{}, err
	}
	return p.Load()
}

// pickProjectInteractive scans ~/Code for directories containing a package.json
// with a dev script, filters out projects already registered in exebox state,
// and prompts the user to pick one. Returns the chosen project name.
func pickProjectInteractive() (string, error) {
	out := output.Global

	p, err := paths()
	if err != nil {
		return "", err
	}
	registered := map[string]bool{}
	domains, _ := p.LoadDomains()
	for _, d := range domains {
		registered[d.Project] = true
	}

	projects, err := discoverCodeProjects()
	if err != nil {
		return "", fmt.Errorf("scan ~/Code: %w", err)
	}

	var available []string
	for _, name := range projects {
		if !registered[name] {
			available = append(available, name)
		}
	}

	if len(available) == 0 {
		if len(projects) == 0 {
			return "", fmt.Errorf("no projects with package.json found in ~/Code")
		}
		return "", fmt.Errorf("all projects in ~/Code are already registered")
	}

	out.Heading("pick a project")
	for i, name := range available {
		out.Line(fmt.Sprintf("  %d. %s", i+1, name))
	}
	fmt.Fprint(out.ErrW, "pick [1-"+fmt.Sprint(len(available))+"]: ")

	var input string
	fmt.Fscanln(os.Stdin, &input)

	for i, name := range available {
		if input == fmt.Sprint(i+1) || strings.EqualFold(input, name) {
			return name, nil
		}
	}
	return "", fmt.Errorf("invalid selection: %q", input)
}

// discoverCodeProjects returns directory names under ~/Code that contain a
// package.json with a dev script, sorted alphabetically.
func discoverCodeProjects() ([]string, error) {
	// Avoid encoding/json import in new.go by checking for the dev script
	// with a lightweight string match — avoids a heavy dependency for a UX nicety.
	entries, err := os.ReadDir(filepath.Join(homeDir(), "Code"))
	if err != nil {
		return nil, err // ~/Code doesn't exist is fine — caller handles empty
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if hasDevScript(filepath.Join(homeDir(), "Code", name, "package.json")) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, nil
}

// hasDevScript checks if package.json contains a "dev" script entry.
// Uses a lightweight string scan to avoid pulling in encoding/json here.
func hasDevScript(pkgPath string) bool {
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "\"dev\"")
}

// homeDir returns the user's home directory or "" on error.
func homeDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return h
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

// domainAddRetry registers a domain via the exe.dev API, retrying on transient
// failures (DNS propagation) with a 5s delay up to 1 minute. Permanent errors
// (bad token, permission denied) are returned immediately without retrying.
func domainAddRetry(ctx context.Context, api *exeapi.Client, vm, domain string) (string, error) {
	const (
		interval = 5 * time.Second
		maxWait  = 1 * time.Minute
	)
	deadline := time.Now().Add(maxWait)
	var lastErr error
	for attempt := 1; ; attempt++ {
		resp, err := api.DomainAdd(vm, domain)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if !exeapi.Retryable(err) {
			return "", err // auth/permission — don't retry
		}
		if !time.Now().Before(deadline) {
			return "", fmt.Errorf("domain add failed after %s: %w", maxWait, lastErr)
		}
		out := output.Global
		if attempt == 1 {
			out.Step("waiting for DNS to propagate, retrying in %s ...", interval)
		} else {
			out.Info("retry %d in %s (%s elapsed) ...", attempt, interval, time.Until(deadline).Truncate(time.Second))
		}
		select {
		case <-time.After(interval):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
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
