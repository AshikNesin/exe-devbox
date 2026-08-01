# Changelog

All notable changes to exebox are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [v0.5.3] - 2025-08-01

### Added
- `exebox dev` now shows the public proxy URL in its startup output, as a clickable terminal hyperlink.

## [v0.5.2] - 2025-08-01

### Fixed
- `exebox new` now retries exe.dev domain registration on transient failures (DNS propagation) every 5s for up to 1 minute before failing. Auth/permission errors (401/403) fail immediately without retrying.

## [v0.5.1] - 2025-08-01

### Fixed
- Shell completion is now installed for **both** bash and zsh during `exebox setup`. Previously only the detected login shell (`$SHELL`) was patched, so zsh users on a bash-default VM got no completion. Scripts are now generated for every supported shell and the rc file is patched wherever it exists.

## [v0.5.0] - 2025-08-01

### Added
- `exebox setup --default-domain <apex>` — set a default domain root (e.g. `nesin.io`) so `exebox new <project>` derives `<project>.nesin.io` automatically. Persisted in config.
- Interactive project picker for `exebox new` — run with no args to pick from unregistered projects in `~/Code` that have a `dev` script.

### Changed
- `exebox new` now accepts a positional project arg: `exebox new groot`.
- `--domain` is no longer required on `exebox new` — it's derived from `--default-domain` + project name when omitted.

## [v0.4.0] - 2025-08-01

### Added
- `CHANGELOG.md` following the [Keep a Changelog](https://keepachangelog.com/) format.
- GitHub Actions release workflow (`.github/workflows/release.yml`) — push a `v*.*.*` tag and CI builds + publishes a GitHub Release with the binary as an asset.

### Changed
- `make release` now just tags + pushes; CI handles building and publishing.
- Download URLs (install.sh, `exebox update`) now use standard GitHub Release asset URLs.

### Removed
- `scripts/release-upload.py` — the Contents API upload script is replaced by CI.
- `releases` branch approach — binaries are now proper release assets, not committed to a branch.

## [v0.3.0] - 2025-08-01

### Added
- Push notifications via the exe.dev notify integration — get a device notification when a domain is added, a dev server starts, or setup completes. Automatically detected; silent no-op if the integration isn't attached.

### Removed
- Domain Connect dead code — the detection logic (`TXT _domainconnect` lookup), `ProviderDomainConnect`, `DCAPI` field, and helper functions were never functional (required provider template onboarding) and have been removed. DNS provider detection is now Cloudflare or manual.
- Dead code: `dns.Record` type, `system.Run`/`RunBash`/`NeedRoot`, `config.File.NginxPortFromReflection`, `output.Mode.Plain`/`Yellow`, and the `_ = system.Run` import hack.
- 14MB of compiled binaries committed to git (`releases/v0.1.0/`, `releases/v0.2.0/`).

### Changed
- Release binaries now live on a dedicated `releases` branch instead of `main`, keeping the main tree clean.
- PRD milestones updated to reflect shipped status.

## [v0.2.0] - 2025-07-31

### Added
- `exebox update` command for self-updating to the latest GitHub release.
- Shell completion auto-installation during `exebox setup`.
- Spinner output for long-running setup steps.
- `exebox set-token` to store an exe.dev API token for auto domain registration.
- exe.dev HTTPS API integration (`internal/exeapi`) — calls `domain add` directly with a scoped token.

### Changed
- Config directory renamed from `~/.exe-devbox` to `~/.exebox` (auto-migrated).
- Caddy-to-nginx handoff during setup (stops/disables Caddy if squatting on the port).

## [v0.1.0] - 2025-07-30

### Added
- Initial release.
- `exebox setup` — installs node, portless, nginx; configures the reverse proxy stack.
- `exebox new` — onboards a project domain (DNS detection, CNAME strategy, exe.dev registration, nginx route).
- `exebox dev` — launches a dev server with HMR-aware env (`VITE_HMR_URL`).
- `exebox status` — shows VM identity, proxy state, domain liveness.
- `exebox doctor` — health checks (reflection, deps, services, ports).
- `--json` machine-readable output for all commands.