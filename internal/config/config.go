// Package config owns the ~/.devbox directory layout: where files live,
// the persisted config.json, and the domains state file. Centralizing paths
// here means the rest of the CLI never hardcodes "~/.devbox".
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const DirName = ".devbox"

// legacyDirNames are pre-rename config dirs. We auto-migrate from any of them
// if the new dir doesn't exist yet (one-time, on first run after upgrading).
var legacyDirNames = []string{".exebox", ".exe-devbox"}

// Paths holds resolved filesystem locations for one config root.
type Paths struct {
	Root    string // ~/.devbox
	Config  string // config.json
	Bin     string // bin/
	Nginx   string // nginx/conf.d/
	State   string // state/
	Domains string // state/domains.json
}

// New resolves paths for the given root. If root is empty, uses ~/.devbox.
// On first use it auto-migrates a legacy ~/.exebox or ~/.exe-devbox dir if
// present.
func New(root string) (Paths, error) {
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Paths{}, err
		}
		root = filepath.Join(home, DirName)
		// One-time migration from a pre-rename dir.
		if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
			for _, legacy := range legacyDirNames {
				legacyPath := filepath.Join(home, legacy)
				if _, err := os.Stat(legacyPath); err == nil {
					if err := migrateLegacy(legacyPath, root); err == nil {
						migrated = true
						break
					}
				}
			}
		}
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return Paths{}, err
	}
	p := Paths{
		Root:    root,
		Config:  filepath.Join(root, "config.json"),
		Bin:     filepath.Join(root, "bin"),
		Nginx:   filepath.Join(root, "nginx", "conf.d"),
		State:   filepath.Join(root, "state"),
		Domains: filepath.Join(root, "state", "domains.json"),
	}
	return p, nil
}

// migrated records whether a legacy dir migration happened during New().
var migrated bool

// WasMigrated reports whether the last New() call migrated from a legacy dir.
func WasMigrated() bool { return migrated }

// migrateLegacy copies the legacy config dir tree to the new location. It
// renames the nginx base conf to match the new naming. Source is left intact.
func migrateLegacy(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		// Rename old base conf to match new naming.
		name := info.Name()
		if name == "00-exebox-base.conf" {
			target = filepath.Join(filepath.Dir(target), "00-devbox-base.conf")
		}
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}

// EnsureDirs creates the full directory tree (idempotent).
func (p Paths) EnsureDirs() error {
	for _, d := range []string{p.Root, p.Bin, p.Nginx, p.State} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", d, err)
		}
	}
	return nil
}

// File holds the persisted ~/.devbox/config.json. It's the cache of what
// devbox discovered/configured so subsequent runs don't need to re-discover.
type File struct {
	VMName        string `json:"vm_name,omitempty"`
	Email         string `json:"email,omitempty"`
	DefaultPort   int    `json:"default_port,omitempty"`
	NginxPort     int    `json:"nginx_port,omitempty"`
	PortlessPort  int    `json:"portless_port,omitempty"`
	CNAMETarget   string `json:"cname_target,omitempty"`
	APIToken      string `json:"api_token,omitempty"` // exe.dev HTTPS API token (scoped to domain add)
	DefaultDomain string `json:"default_domain,omitempty"` // e.g. nesin.io — used to derive FQDNs in devbox new
}

// Load reads config.json; returns a zero File (no error) if it doesn't exist yet.
func (p Paths) Load() (File, error) {
	b, err := os.ReadFile(p.Config)
	if errors.Is(err, os.ErrNotExist) {
		return File{}, nil
	}
	if err != nil {
		return File{}, err
	}
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		return File{}, fmt.Errorf("parse %s: %w", p.Config, err)
	}
	return f, nil
}

// Save writes config.json (pretty).
func (p Paths) Save(f File) error {
	if err := os.MkdirAll(filepath.Dir(p.Config), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(p.Config, b, 0o644)
}

// Domain is one row in state/domains.json: a public FQDN devbox wired up.
type Domain struct {
	Domain  string `json:"domain"`            // public FQDN, e.g. new-app.devbox.nesin.io
	Project string `json:"project"`           // portless route name -> <project>.localhost
	Backend string `json:"backend"`           // "portless" or "loopback:<port>"
	Apex    string `json:"apex,omitempty"`    // registered DNS apex, e.g. nesin.io
	Public  bool   `json:"public,omitempty"`  // whether set-public was requested
	AddedAt string `json:"added_at,omitempty"`
}

// LoadDomains reads state/domains.json; empty list if absent.
func (p Paths) LoadDomains() ([]Domain, error) {
	b, err := os.ReadFile(p.Domains)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var d []Domain
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, fmt.Errorf("parse %s: %w", p.Domains, err)
	}
	return d, nil
}

// SaveDomains writes state/domains.json.
func (p Paths) SaveDomains(d []Domain) error {
	if err := os.MkdirAll(filepath.Dir(p.Domains), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(p.Domains, b, 0o644)
}

// UpsertDomain adds or replaces a domain by FQDN, returns the new list.
func UpsertDomain(domains []Domain, d Domain) []Domain {
	for i, ex := range domains {
		if ex.Domain == d.Domain {
			domains[i] = d
			return domains
		}
	}
	return append(domains, d)
}

// ProjectConf returns the nginx conf.d path for a project.
func (p Paths) ProjectConf(project string) string {
	return filepath.Join(p.Nginx, project+".conf")
}

// BaseConf returns the CLI-managed nginx base config path.
func (p Paths) BaseConf() string {
	return filepath.Join(p.Nginx, "00-devbox-base.conf")
}
