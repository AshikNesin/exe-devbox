package cmds

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ashiknesin/exebox/internal/config"
	"github.com/ashiknesin/exebox/internal/deps"
	"github.com/ashiknesin/exebox/internal/nginx"
	"github.com/ashiknesin/exebox/internal/output"
	"github.com/ashiknesin/exebox/internal/portless"
	"github.com/ashiknesin/exebox/internal/reflection"
	"github.com/ashiknesin/exebox/internal/shell"
	"github.com/ashiknesin/exebox/internal/system"
	"github.com/spf13/cobra"
)

// defaultNginxPort is the last-resort fallback when neither --nginx-port nor
// the reflection default port is available.
const defaultNginxPort = 8000

func newSetupCmd() *cobra.Command {
	var vmName, nginxPortStr, portlessPortStr string
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Install deps, manage nginx config, discover VM identity",
		Long: `Bring a fresh exe.dev VM up to a working nginx + portless reverse-proxy
stack for multi-project dev. Idempotent and safe to re-run.

Steps:
  1. discover VM name + default port from reflection
  2. install node (LTS), portless, nginx (skip if present)
  3. ensure the shared portless daemon on :8888 (HTTP)
  4. write nginx config under ~/.exebox/nginx + the /etc include shim
  5. point exe.dev's proxy at nginx (prints a suggest link if not already)
  6. run doctor`,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := setupOpts{
				vmName:       vmName,
				nginxPortStr: nginxPortStr,
				portlessPort: parsePort(portlessPortStr, portless.DaemonPort),
			}
			res := runSetup(cmd.Context(), opts, cmd.Root())
			output.Global.Print(output.Result{OK: res.ok, Exit: exitCode(res.ok), Data: res.report, Message: res.errMsg})
			if !res.ok {
				return fmt.Errorf("setup did not complete: %s", res.errMsg)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&vmName, "vm", "", "VM name (default: auto-discovered from reflection)")
	cmd.Flags().StringVar(&nginxPortStr, "nginx-port", "", "port for nginx to listen on (default: reflection default port, or 8000)")
	cmd.Flags().StringVar(&portlessPortStr, "portless-port", "", "port for the portless daemon (default 8888)")
	return cmd
}

type setupOpts struct {
	vmName        string
	nginxPortStr  string // raw --nginx-port flag; resolved after reflection
	nginxPort     int    // resolved (flag > reflection default > fallback)
	portlessPort  int
}

type setupReport struct {
	VM            reflection.Identity `json:"vm"`
	NginxPort     int                 `json:"nginx_port"`
	PortlessPort  int                 `json:"portless_port"`
	NodeVersion   string              `json:"node_version,omitempty"`
	Steps         []setupStep         `json:"steps"`
	SuggestLink   string              `json:"suggest_link,omitempty"`
}

type setupStep struct {
	Name   string `json:"name"`
	Status string `json:"status"` // "ok" | "skip" | "fail"
	Detail string `json:"detail,omitempty"`
}

type setupResult struct {
	report  setupReport
	ok      bool
	errMsg  string
}

func runSetup(ctx context.Context, opts setupOpts, root *cobra.Command) setupResult {
	out := output.Global
	p, err := paths()
	if err != nil {
		return setupResult{ok: false, errMsg: err.Error()}
	}
	report := setupReport{PortlessPort: opts.portlessPort}
	step := func(name, status, detail string) {
		report.Steps = append(report.Steps, setupStep{name, status, detail})
	}

	// 1. discover identity
	s := out.Spinner("discovering VM identity")
	rc := reflection.New()
	id, err := rc.Discover(ctx)
	if err != nil {
		s.Fail("reflection: %s", err)
		return setupResult{report: report, ok: false, errMsg: "reflection: " + err.Error()}
	}
	if opts.vmName != "" {
		id.Name = opts.vmName // override wins; recompute cname target via Identity.CNAME()
	}
	report.VM = id
	step("reflection", "ok", fmt.Sprintf("%s port=%d cname=%s", id.Name, id.DefaultPort, id.CNAME()))
	s.OK("VM %s (default port %d, cname %s)", id.Name, id.DefaultPort, id.CNAME())

	// Resolve the nginx port now that we know the reflection default port.
	// Priority: --nginx-port flag > reflection default port > fallback 8000.
	opts.nginxPort = resolveNginxPort(opts.nginxPortStr, id.DefaultPort)
	report.NginxPort = opts.nginxPort
	out.Info("nginx will listen on :%d", opts.nginxPort)

	// ensure config dir (New() may have auto-migrated from ~/.exe-devbox)
	if config.WasMigrated() {
		out.OK("migrated config from ~/.exe-devbox -> ~/.exebox")
	}
	if err := p.EnsureDirs(); err != nil {
		return setupResult{report: report, ok: false, errMsg: err.Error()}
	}

	// 2. install deps
	s = out.Spinner("installing dependencies (node LTS, portless, nginx)")
	nodeV, err := deps.InstallNode(ctx, false)
	if err != nil {
		s.Fail("node: %s", err)
		step("node", "fail", err.Error())
		return setupResult{report: report, ok: false, errMsg: "node: " + err.Error()}
	}
	report.NodeVersion = nodeV
	step("node", cond(nodeV != "", "ok", "fail"), nodeV)
	s.OK("node %s", nodeV)

	if err := deps.InstallPortless(ctx); err != nil {
		out.Err("portless: %s", err)
		step("portless bin", "fail", err.Error())
		return setupResult{report: report, ok: false, errMsg: "portless: " + err.Error()}
	}
	step("portless bin", "ok", "installed")
	out.OK("portless installed")

	if err := deps.InstallNginx(ctx); err != nil {
		out.Err("nginx: %s", err)
		step("nginx", "fail", err.Error())
		return setupResult{report: report, ok: false, errMsg: "nginx: " + err.Error()}
	}
	step("nginx", "ok", "/usr/sbin/nginx")
	out.OK("nginx present")

	// 3. portless daemon
	s = out.Spinner("ensuring portless daemon on :%d (HTTP)", opts.portlessPort)
	if err := portless.EnsureDaemon(); err != nil {
		s.Fail("portless daemon: %s", err)
		step("portless daemon", "fail", err.Error())
		return setupResult{report: report, ok: false, errMsg: "portless daemon: " + err.Error()}
	}
	step("portless daemon", "ok", "active")
	s.OK("portless daemon active on :%d", opts.portlessPort)

	// 4. nginx config: handle Caddy squatting on the port, write shim + base
	s = out.Spinner("configuring nginx on :%d", opts.nginxPort)
	if err := ensureNginxPortFree(opts.nginxPort, gflags.Yes); err != nil {
		s.Fail("nginx port: %s", err)
		step("nginx port", "fail", err.Error())
		return setupResult{report: report, ok: false, errMsg: err.Error()}
	}
	if err := writeNginxConfigs(p, opts.nginxPort); err != nil {
		s.Fail("nginx config: %s", err)
		step("nginx config", "fail", err.Error())
		return setupResult{report: report, ok: false, errMsg: "nginx config: " + err.Error()}
	}
	step("nginx config", "ok", p.Nginx)
	s.OK("nginx config at %s", p.Nginx)

	// validate + enable + (re)start nginx
	out.Step("validating + restarting nginx")
	if out2, err := nginx.Test(); err != nil {
		out.Err("nginx -t: %s", string(out2))
		step("nginx -t", "fail", string(out2))
		return setupResult{report: report, ok: false, errMsg: "nginx -t: " + string(out2)}
	}
	if out2, err := system.AsRoot("systemctl", "enable", "--now", "nginx").CombinedOutput(); err != nil {
		out.Err("nginx enable: %s", string(out2))
		step("nginx enable", "fail", string(out2))
		return setupResult{report: report, ok: false, errMsg: "nginx enable: " + string(out2)}
	}
	if out2, err := system.AsRoot("systemctl", "restart", "nginx").CombinedOutput(); err != nil {
		out.Err("nginx restart: %s", string(out2))
		step("nginx restart", "fail", string(out2))
		return setupResult{report: report, ok: false, errMsg: "nginx restart: " + string(out2)}
	}
	step("nginx", "ok", "active")
	out.OK("nginx active on :%d", opts.nginxPort)

	// persist config
	cfg, _ := p.Load()
	cfg.VMName = id.Name
	cfg.Email = id.Email
	cfg.DefaultPort = id.DefaultPort
	cfg.NginxPort = opts.nginxPort
	cfg.PortlessPort = opts.portlessPort
	cfg.CNAMETarget = id.CNAME()
	_ = p.Save(cfg)

	// 5. share-port suggest link if reflection port != nginx port
	if id.DefaultPort != opts.nginxPort {
		link := suggestSharePort(id.Name, opts.nginxPort)
		report.SuggestLink = link
		out.Heading("action needed: point exe.dev at nginx")
		out.Info("exe.dev currently routes port %d to this VM; nginx is on %d.", id.DefaultPort, opts.nginxPort)
		out.Info("click to retarget (owner key required):")
		out.Line("  " + link)
	}

	out.OK("setup complete")

	// Auto-install shell completion (bash or zsh, detected automatically).
	if cr, err := shell.InstallCompletion(root); err != nil {
		out.Warn("shell completion: %s", err)
	} else if cr.AlreadyOK {
		out.OK("shell completion ready (%s)", cr.Shell)
	} else {
		out.OK("shell completion installed (%s)", cr.Shell)
		out.Info("open a new shell or: source ~/%s", filepath.Base(cr.RCFile))
	}

	// Subtle next-step hint: domain registration.
	// Only show if no API token is set yet (otherwise exebox new handles it
	// automatically and there's nothing for the user to do).
	if cfg.APIToken == "" {
		out.Heading("next steps")
		out.Info("wire a public domain to a project on this VM:")
		out.Block("exebox new -d myapp.example.com --project myapp")
		out.Info("to skip the manual domain registration step, set up an API token:")
		out.Block("exebox set-token --help")
	}

	return setupResult{report: report, ok: true}
}

// ensureNginxPortFree detects if something else (e.g. Caddy from the reference
// doc setup) is squatting on the nginx port and offers to stop+disable it.
func ensureNginxPortFree(port int, assumeYes bool) error {
	// Is our own nginx already there? Fine.
	if system.PortListening(port).Pass {
		owner := portOwner(port)
		// Could be nginx (good) or another service (needs handling).
		if owner == "nginx" {
			return nil // nginx already serving, nothing to do
		}
		// Caddy squatting (reference doc setup)? Stop + disable it.
		if owner == "caddy" {
			if !assumeYes && !confirm(fmt.Sprintf("Caddy is using :%d. Stop and disable Caddy so nginx can take over?", port)) {
				return fmt.Errorf("port :%d in use by caddy; declined to stop it", port)
			}
			if out, err := system.AsRoot("systemctl", "stop", "caddy").CombinedOutput(); err != nil {
				return fmt.Errorf("stop caddy: %w: %s", err, out)
			}
			_ = system.AsRoot("systemctl", "disable", "caddy").Run()
			output.Global.OK("stopped Caddy")
			return nil
		}
		return fmt.Errorf("port :%d in use by %q; stop it first", port, owner)
	}
	return nil
}

// portOwner returns the process name listening on port, or "". Needs root to
// see other users' process names via ss, so we run it through sudo.
func portOwner(port int) string {
	out, err := system.AsRoot("ss", "-tlnp").CombinedOutput()
	if err != nil {
		return ""
	}
	needle := fmt.Sprintf(":%d", port)
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, needle) {
			continue
		}
		if i := strings.Index(line, "users:(("); i >= 0 {
			rest := line[i+len("users:((") :]
			if comma := strings.IndexByte(rest, ','); comma > 0 {
				name := strings.Trim(rest[:comma], `"`)
				return name
			}
		}
		return "unknown"
	}
	return ""
}

// writeNginxConfigs writes the include shim (sudo, once) and the base conf.
func writeNginxConfigs(p config.Paths, port int) error {
	// base conf (user-owned, no sudo)
	base := nginx.BaseConf(port)
	if err := os.WriteFile(p.BaseConf(), []byte(base), 0o644); err != nil {
		return err
	}
	// include shim at /etc/nginx/conf.d/exebox.conf (root-owned). nginx doesn't
	// expand ~, so we resolve the absolute path here.
	shim := nginx.IncludeShim(p.Nginx)
	tmp, err := os.CreateTemp("", "exebox-shim-*.conf")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(shim); err != nil {
		return err
	}
	tmp.Close()
	target := "/etc/nginx/conf.d/exebox.conf"
	if out, err := system.AsRoot("cp", tmp.Name(), target).CombinedOutput(); err != nil {
		return fmt.Errorf("write %s: %w: %s", target, err, out)
	}
	if out, err := system.AsRoot("chmod", "644", target).CombinedOutput(); err != nil {
		return fmt.Errorf("chmod %s: %w: %s", target, err, out)
	}
	return nil
}

// --- small helpers ---

// resolveNginxPort picks the nginx listen port. Priority:
//  1. explicit flag (--nginx-port)
//  2. reflection default port (the port exe.dev already routes here)
//  3. fallback (defaultNginxPort)
func resolveNginxPort(flag string, reflectionPort int) int {
	if p := parsePort(flag, 0); p != 0 {
		return p
	}
	if reflectionPort != 0 {
		return reflectionPort
	}
	return defaultNginxPort
}

func parsePort(s string, def int) int {
	if s == "" {
		return def
	}
	p, err := strconv.Atoi(s)
	if err != nil || p < 1 || p > 65535 {
		return def
	}
	return p
}

func cond(b bool, t, f string) string {
	if b {
		return t
	}
	return f
}

func confirm(prompt string) bool {
	if gflags.Yes {
		return true // --yes skips prompts
	}
	if output.Global.JSON {
		return false // never auto-confirm in JSON mode without --yes
	}
	fmt.Fprintf(output.Global.ErrW, "%s [y/N]: ", prompt)
	var resp string
	fmt.Fscanln(os.Stdin, &resp)
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(resp)), "y")
}

// suggestLink is defined in link.go.

// keep imports honest
var _ = strconv.Atoi
