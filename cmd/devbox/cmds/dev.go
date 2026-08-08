package cmds

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ashiknesin/exe-devbox/internal/notify"
	"github.com/ashiknesin/exe-devbox/internal/output"
	"github.com/ashiknesin/exe-devbox/internal/portless"
	"github.com/spf13/cobra"
)

func newDevCmd() *cobra.Command {
	var (
		dir     string
		runner  string // pnpm/npm
		nodaemon bool
	)
	cmd := &cobra.Command{
		Use:   "dev [project]",
		Short: "Launch a project's dev server with correct HMR env (wss://<public-host>:443)",
		Long: `Start a portless project's dev server with the environment Vite HMR needs to
work through exe.dev + nginx. Without this, Vite computes its HMR WebSocket
target from portless's injected PORTLESS_URL (ws://<project>.localhost:8888),
which the browser can't reach and is blocked as mixed content.

Looks up <project> in devbox state (devbox new) to get its public domain, sets
VITE_HMR_URL=<public-domain> + PORTLESS_PORT/HOST, kills any leftover dev
tree so the portless route name is free, then runs the dev script.

Default runner is pnpm (override with --runner npm). Use --dir to point at a
project not in ~/Code/<project>.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			project := ""
			if len(args) > 0 {
				project = args[0]
			}
			opts := devOpts{project: project, dir: dir, runner: runner, foreground: nodaemon}
			res := runDev(opts)
			output.Global.Print(output.Result{OK: res.ok, Exit: exitCode(res.ok), Message: res.msg})
			if !res.ok {
				return fmt.Errorf("dev: %s", res.msg)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (default ~/Code/<project>)")
	cmd.Flags().StringVar(&runner, "runner", "pnpm", "dev script runner: pnpm|npm")
	cmd.Flags().BoolVar(&nodaemon, "foreground", false, "run in foreground (default: detached background process)")
	return cmd
}

type devOpts struct {
	project   string
	dir       string
	runner    string
	foreground bool
}

type devResult struct {
	ok  bool
	msg string
}

func runDev(o devOpts) devResult {
	out := output.Global

	// Resolve project + its public domain from state.
	p, err := paths()
	if err != nil {
		return devResult{false, err.Error()}
	}
	// Infer project from cwd: walk up to find the nearest package.json and
	// use its directory name as the project name.
	if o.project == "" {
		o.project = inferProjectFromCWD()
	}
	if o.project == "" {
		// default to the sole portless project if unambiguous
		domains, _ := p.LoadDomains()
		var portlessProjects []string
		for _, d := range domains {
			if d.Backend == "portless" {
				portlessProjects = appendUnique(portlessProjects, d.Project)
			}
		}
		if len(portlessProjects) == 1 {
			o.project = portlessProjects[0]
		} else {
			return devResult{false, "specify a project: devbox dev <project>"}
		}
	}

	// Find the project's first registered public domain (for VITE_HMR_URL).
	hmrHost, backend, err := lookupProjectDomain(o.project)
	if err != nil {
		return devResult{false, err.Error()}
	}
	if backend != "portless" {
		return devResult{false, fmt.Sprintf("project %q is a %s backend; dev only manages portless projects", o.project, backend)}
	}

	// Resolve project dir.
	if o.dir == "" {
		home, _ := os.UserHomeDir()
		o.dir = filepath.Join(home, "Code", o.project)
	}
	if fi, err := os.Stat(o.dir); err != nil || !fi.IsDir() {
		return devResult{false, fmt.Sprintf("project dir not found: %s", o.dir)}
	}
	if _, err := os.Stat(filepath.Join(o.dir, "package.json")); err != nil {
		return devResult{false, fmt.Sprintf("no package.json in %s", o.dir)}
	}

	// Ensure the portless daemon is up (dev servers register on it).
	if !portless.ServiceActive() {
		out.Warn("portless daemon not active; start it with: devbox setup")
	}

	out.Step("starting %s dev (HMR -> wss://%s:443)", o.project, hmrHost)

	// Kill any leftover dev tree so the *.localhost route name frees up.
	killed := killPortlessProject(o.project)
	if killed > 0 {
		out.Info("killed %d leftover process(es) holding the %s.localhost route", killed, o.project)
		time.Sleep(1500 * time.Millisecond)
	}

	// Resolve the node bin dir (nvm) so the dev server + its child processes
	// (which spawn pnpm/node for prisma, etc.) can find them. On this VM node/pnpm
	// live under ~/.nvm and aren't on a non-login PATH.
	nodeBin := resolveNodeBin()

	runner := o.runner
	if resolved, err := lookPathWith(runner, nodeBin); err == nil {
		runner = resolved
	} else {
		// fall back across the common runners
		for _, alt := range []string{"pnpm", "npm"} {
			if resolved, err := lookPathWith(alt, nodeBin); err == nil {
				runner = resolved
				break
			}
		}
	}

	devCmd := exec.Command(runner, "run", "dev")
	devCmd.Dir = o.dir
	// HMR-aware env: the whole point of this command.
	devCmd.Env = append(os.Environ(),
		fmt.Sprintf("VITE_HMR_URL=%s", hmrHost),
		"PORTLESS_PORT="+fmt.Sprint(portless.DaemonPort),
		"PORTLESS_HTTPS=0",
		"NODE_ENV=development",
	)
	if nodeBin != "" {
		devCmd.Env = appendEnvPath(devCmd.Env, nodeBin)
	}

	if o.foreground {
		devCmd.Stdout = os.Stdout
		devCmd.Stderr = os.Stderr
		devCmd.Stdin = os.Stdin
		out.Info("foreground mode; Ctrl-C to stop")
		if err := devCmd.Run(); err != nil {
			return devResult{false, fmt.Sprintf("dev exited: %s", err)}
		}
		return devResult{ok: true}
	}

	// Detached background process (new session, logs to a file).
	logPath := filepath.Join(p.State, o.project+"-dev.log")
	logf, err := os.Create(logPath)
	if err != nil {
		return devResult{false, fmt.Sprintf("open log %s: %s", logPath, err)}
	}
	devCmd.Stdout = logf
	devCmd.Stderr = logf
	devCmd.Stdin = nil
	devCmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // detach into own session+group
	if err := devCmd.Start(); err != nil {
		logf.Close()
		return devResult{false, fmt.Sprintf("start: %s", err)}
	}
	// Re-parent so it survives this CLI exit; we don't wait on it.
	_ = devCmd.Process.Release()

	url := "https://" + hmrHost + "/"
	out.OK("%s dev started (pid %d, runner=%s)", o.project, devCmd.Process.Pid, runner)
	out.Info("public URL: %s", hyperlink(url, url))
	out.Info("HMR target: wss://%s:443", hmrHost)
	out.Info("log: %s", logPath)
	out.Info("wait ~15-20s, then: devbox status")

	// Push notification: dev server is up (fire-and-forget, with a grace
	// period so the server has time to bind).
	go func() {
		n := notify.Default()
		if !n.Available() {
			return
		}
		time.Sleep(10 * time.Second)
		url := fmt.Sprintf("https://%s/", hmrHost)
		msg := fmt.Sprintf("%s dev server is up on %s", o.project, hmrHost)
		_ = n.SendWithURL(context.Background(), "🚀 Dev server up", msg, url)
	}()

	return devResult{ok: true}
}

// inferProjectFromCWD returns the project name from the nearest ancestor
// containing a package.json — reads its "name" field, falling back to the
// directory name. Returns "" if no package.json is found.
func inferProjectFromCWD() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	for dir := cwd; dir != "/" && dir != home && strings.HasPrefix(dir, home); {
		if name := readPackageName(dir); name != "" {
			return name
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// readPackageName reads the "name" field from <dir>/package.json, falling
// back to the directory base name if the field is missing/unparseable.
func readPackageName(dir string) string {
	if _, err := os.Stat(filepath.Join(dir, "package.json")); err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return filepath.Base(dir)
	}
	var pkg struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(data, &pkg) == nil && pkg.Name != "" {
		return pkg.Name
	}
	return filepath.Base(dir)
}

// lookupProjectDomain returns the first public domain + backend for a project
// from devbox state.
func lookupProjectDomain(project string) (domain, backend string, err error) {
	p, _ := paths()
	domains, _ := p.LoadDomains()
	for _, d := range domains {
		if d.Project == project {
			first := strings.TrimSpace(strings.Split(d.Domain, ",")[0])
			return first, d.Backend, nil
		}
	}
	return "", "", fmt.Errorf("project %q not found in devbox state (run: devbox new -d <fqdn> --project %s)", project, project)
}

// killPortlessProject kills any process running `portless run --name <project>`
// so its route name frees up. Returns the count killed.
func killPortlessProject(project string) int {
	// `pkill -f` with a trailing space to avoid matching the pattern itself /
	// substring collisions (e.g. "groot" vs "groot-two").
	pattern := fmt.Sprintf("portless run --name %s ", project)
	// count first (before pkill kills them), then kill.
	out, _ := exec.Command("pgrep", "-f", pattern).Output()
	n := len(strings.Fields(string(out)))
	if n > 0 {
		_ = exec.Command("pkill", "-f", pattern).Run()
	}
	return n
}

func appendUnique(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

// resolveNodeBin finds the nvm node bin dir (e.g.
// ~/.nvm/versions/node/v24.18.0/bin) if present, else "". We pick the newest
// installed version. This matters because node/pnpm/portless are NOT on a
// non-login PATH on this VM.
func resolveNodeBin() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	versionsDir := filepath.Join(home, ".nvm", "versions", "node")
	entries, err := os.ReadDir(versionsDir)
	if err != nil {
		return ""
	}
	var newest string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		bin := filepath.Join(versionsDir, e.Name(), "bin")
		if _, err := os.Stat(filepath.Join(bin, "node")); err != nil {
			continue
		}
		if e.Name() > newest { // lexicographic works for v24.x.y ordering
			newest = e.Name()
		}
	}
	if newest == "" {
		return ""
	}
	return filepath.Join(versionsDir, newest, "bin")
}

// appendEnvPath prepends dir to PATH in a PATH=... env slice (idempotent).
func appendEnvPath(env []string, dir string) []string {
	var path string
	others := make([]string, 0, len(env))
	for _, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			path = strings.TrimPrefix(kv, "PATH=")
		} else {
			others = append(others, kv)
		}
	}
	if path == "" {
		path = os.Getenv("PATH")
	}
	if !strings.Contains(":"+path+":", ":"+dir+":") {
		path = dir + ":" + path
	}
	return append(others, "PATH="+path)
}

// lookPathWith finds name on PATH augmented with extraDir (if set).
func lookPathWith(name, extraDir string) (string, error) {
	if extraDir != "" {
		candidate := filepath.Join(extraDir, name)
		if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
			return candidate, nil
		}
	}
	return exec.LookPath(name)
}

// hyperlink wraps text in an OSC 8 terminal hyperlink (clickable in modern
// terminals). Returns plain text when not a TTY.
func hyperlink(url, text string) string {
	if !output.Global.Color {
		return text
	}
	return "\033]8;;" + url + "\033\\" + text + "\033]8;;\033\\"
}