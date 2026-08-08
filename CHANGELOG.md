# Changelog

All notable changes to devbox are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [v0.5.7] - 2025-08-01

### Added
- `devbox add` is now an alias for `devbox new`.

## [v0.5.6] - 2025-08-01

### Added
- `devbox dev` now infers the project name from the nearest `package.json` — reads the `"name"` field (falls back to directory name). Works from anywhere in the project tree, so `devbox dev` just works without arguments when run inside a project.

## [v0.5.5] - 2025-08-01

### Fixed
- All `devbox` commands failed silently (exit 1, no output) on error because `main.go` assumed cobra would print errors, but `SilenceErrors` was enabled. The actual error message is now printed to stderr.

## [v0.5.4] - 2025-08-01

### Fixed
- Domain registration retry was not triggering on DNS propagation failures because the exe.dev API returns HTTP 200 with the error embedded in the JSON body. devbox now parses the response body and retries application-level errors (e.g. "DNS does not point to ...") every 5s for up to 1 minute.

## [v0.5.3] - 2025-08-01

### Added
- `devbox dev` now shows the public proxy URL in its startup output, as a clickable terminal hyperlink.

## [v0.5.2] - 2025-08-01

### Fixed
- `devbox new` now retries exe.dev domain registration on transient failures (DNS propagation) every 5s for up to 1 minute before failing. Auth/permission errors (401/403) fail immediately without retrying.

## [v0.5.1] - 2025-08-01

### Fixed
- Shell completion is now installed for **both** bash and zsh during `devbox setup`. Previously only the detected login shell (`$SHELL`) was patched, so zsh users on a bash-default VM got no completion. Scripts are now generated for every supported shell and the rc file is patched wherever it exists.

## [v0.5.0] - 2025-08-01

### Added
- `devbox setup --default-domain <apex>` — set a default domain root (e.g. `nesin.io`) so `devbox new <project>` derives `<project>.nesin.io` automatically. Persisted in config.
- Interactive project picker for `devbox new` — run with no args to pick from unregistered projects in `~/Code` that have a `dev` script.

### Changed
- `devbox new` now accepts a positional project arg: `devbox new groot`.
- `--domain` is no longer required on `devbox new` — it's derived from `--default-domain` + project name when omitted.

## [v0.4.0] - 2025-08-01

### Added
- `CHANGELOG.md` following the [Keep a Changelog](https://keepachangelog.com/) format.
- GitHub Actions release workflow (`.github/workflows/release.yml`) — push a `v*.*.*` tag and CI builds + publishes a GitHub Release with the binary as an asset.

### Changed
- `make release` now just tags + pushes; CI handles building and publishing.
- Download URLs (install.sh, `devbox update`) now use standard GitHub Release asset URLs.

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
- `devbox update` command for self-updating to the latest GitHub release.
- Shell completion auto-installation during `devbox setup`.
- Spinner output for long-running setup steps.
- `devbox set-token` to store an exe.dev API token for auto domain registration.
- exe.dev HTTPS API integration (`internal/exeapi`) — calls `domain add` directly with a scoped token.

### Changed
- Config directory renamed from `~/.exe-devbox` to `~/.devbox` (auto-migrated).
- Caddy-to-nginx handoff during setup (stops/disables Caddy if squatting on the port).

## [v0.1.0] - 2025-07-30

### Added
- Initial release.
- `devbox setup` — installs node, portless, nginx; configures the reverse proxy stack.
- `devbox new` — onboards a project domain (DNS detection, CNAME strategy, exe.dev registration, nginx route).
- `devbox dev` — launches a dev server with HMR-aware env (`VITE_HMR_URL`).
- `devbox status` — shows VM identity, proxy state, domain liveness.
- `devbox doctor` — health checks (reflection, deps, services, ports).
- `--json` machine-readable output for all commands.