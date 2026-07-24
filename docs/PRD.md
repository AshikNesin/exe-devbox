# PRD — `exe-devbox`

> A Go CLI that automates the "multi-project dev behind Caddy + portless on an
> exe.dev VM" workflow documented in
> `~/Learn/multi-project-caddy-portless-exedev.md`.
>
> Status: **Draft**. Owner: nesintechnologies@gmail.com (VM `nesins-devbox`).

---

## 1. Problem

The manual setup in the reference doc is long and error-prone: install Node
LTS + portless + Caddy, install one shared portless daemon on `:8888`, write a
Host-rewriting Caddyfile on `:8080`, point exe.dev's main proxy at Caddy,
figure out the VM name/port, and — for every project — add a public subdomain
that needs (a) a DNS CNAME to `<vm>.exe.xyz` and (b) an exe.dev domain
registration. Doing this by hand for each new project is the friction we're
removing.

`exe-devbox` (binary name **`devbox`**) automates two things:

1. **One-time VM bring-up** (`devbox setup`) — install deps, manage Caddy
   config at `~/.exe-devbox/caddy`, and discover the VM's identity.
2. **Per-project domain onboarding** (`devbox new --domain …`) — figure out the
   DNS provider of the target domain, give the user the easiest possible path
   to add the CNAME (Domain Connect if supported, else manual instructions),
   then emit the exe.dev command/link to register the domain for this VM.

---

## 2. Goals / Non-goals

### Goals
- **G1.** `devbox setup` gets a fresh VM from zero to a working
  Caddy-on-`:8080` + portless-on-`:8888` reverse-proxy stack, idempotent and
  re-runnable, with Caddy config managed under `~/.exe-devbox/caddy`.
- **G2.** `devbox setup` auto-discovers the VM's **name** and **default port**
  from the exe.dev reflection endpoint (`reflection.int.exe.xyz`) — no flags
  required for the common case.
- **G3.** `devbox new --domain <fqdn>` determines the authoritative DNS
  provider of the domain's apex and chooses the right CNAME-add strategy:
  - Cloudflare → Domain Connect flow (with a today-working direct-API fallback).
  - Any other Domain-Connect-supporting provider → Domain Connect apply URL.
  - Unknown / unsupported provider → clear manual instructions with exact
    record values.
- **G4.** Every `devbox new` run ends by emitting the **exe.dev suggest link**
  to register the domain for this VM (and the share-port link if the VM's
  default port isn't already pointing at Caddy).
- **G5.** Distinguish **on-VM automation** (can run directly) from **owner-only
  exe.dev commands** (need the owner's SSH key → surfaced as
  `https://exe.dev/suggest?command=…` click-to-run links).

### Non-goals (v1)
- Managing project dev servers themselves (`pnpm run dev`, HMR wiring). That
  stays in each project's launcher / the reference doc's `~/bin/devbox-start`.
  v1 may *document* it but not own it.
- TLS/cert automation — exe.dev terminates TLS; Caddy is a plain HTTP hop.
- A TUI/dashboard. Plain CLI output with color + copy-paste blocks.
- Windows/macOS as first-class targets (exe.dev VMs are Ubuntu). Linux only v1.

---

## 3. Context & key facts (verified on this VM)

| Fact | Value / source |
|---|---|
| Default port | `curl https://reflection.int.exe.xyz/default_port` → `{"default_port":8080}` |
| VM name | `reflection.int.exe.xyz/` → `{"name":"nesins-devbox"}` |
| Owner email | `reflection.int.exe.xyz/email` |
| **CNAME target** | **`<vm>.exe.xyz`** (resolves; e.g. `nesins-devbox.exe.xyz` → `161.210.92.5`). **NOT** `<vm>.exe.dev`, which does not resolve. |
| exe.dev domain add (owner-only) | `ssh exe.dev domain add <vm> <domain>` |
| exe.dev main proxy target (owner-only) | `ssh exe.dev share port <vm> <port>` |

> **⚠ Note:** the original request used `nesins-devbox.exe.dev` as the CNAME
> target. That hostname does **not** resolve. The correct target is
> `nesins-devbox.exe.xyz`. The CLI will always use `<vm>.exe.xyz`.

### Domain Connect reality check

- Domain Connect (DC) is an open standard. A host "supports DC" iff its apex
  publishes `TXT _domainconnect.<apex>` pointing at the provider's DC API.
- **Cloudflare supports DC, but only the *synchronous* flow, and only for
  service providers who have onboarded a signed template**
  (`syncPubKeyDomain` TXT + a cryptographic signature on every apply URL,
  which must be the last query param). See
  https://developers.cloudflare.com/dns/reference/domain-connect/ .
- Consequence: a standalone CLI **cannot mint a working one-click DC apply URL
  for Cloudflare** without first (a) publishing a template to the
  `Domain-Connect/Templates` repo and (b) emailing Cloudflare to onboard it.
- This PRD therefore treats *real* one-click DC as a **future/onboarding-gated**
  capability, and ships a **today-working fallback** for Cloudflare (direct API
  apply if the user provides a token) plus **manual** for everyone else.

---

## 4. Architecture recap (from reference doc)

```
https://<project>.<your-domain>          (browser)
   │  TLS by exe.dev edge
   ▼
exe.dev edge ──HTTP──► VM:Caddy (:8080)
                            │  rewrite Host: <name>.localhost
                            ▼
                        VM:portless (:8888, one shared daemon)
                            │  routes by *.localhost Host
                            ▼
                        <project> dev server (auto port)
```

- One **shared portless** daemon on `:8888` (HTTP, no TLS), systemd-managed.
- **Caddy** on `:8080` rewrites `Host` from the public name to
  `<project>.localhost` so portless can route.
- exe.dev's single main proxy port points at Caddy (`8080`). All custom
  domains ride that one port → must all go through Caddy.

`devbox setup` produces exactly this. `devbox new` adds the public-name
entries (DNS + exe.dev registration) and the Caddy `@<project> handle {…}`
block for a project.

---

## 5. CLI surface

```
devbox setup [--vm <name>] [--port <n>] [--caddy-port 8080] [--portless-port 8888]
devbox new --domain <fqdn> [--project <name>] [--to <backend>] [--public]
devbox status
devbox caddy (reload | edit | show)
devbox doctor
```

Global flags: `--config <dir>` (default `~/.exe-devbox`), `--json` (machine
output), `--yes` (skip confirmations), `-v/--verbose`.

---

## 6. Feature spec — `devbox setup`

### 6.1 What it does (in order)

1. **Discover VM identity** (unless `--vm`/`--port` given):
   - `GET https://reflection.int.exe.xyz/` → `name`.
   - `GET https://reflection.int.exe.xyz/default_port` → `default_port`.
   - Store in `~/.exe-devbox/config.json`.
   - Derive `cname-target = <name>.exe.xyz`.

2. **Install dependencies** (detect & skip if present, pin versions in config):
   - **Node.js (latest LTS)** — prefer NodeSource apt repo for system-wide
     `/usr/bin/node`; detect existing nvm install and reuse its node if
     newer. Resolve "latest LTS" from
     `https://nodejs.org/dist/index.json` (filter `lts` field).
   - **portless** (`portless.sh`) — `npm i -g portless` using the node above.
   - **Caddy** — official apt repo (`apt-get install -y caddy`). Reuse if
     `/usr/bin/caddy` exists.
   - Requires sudo; prompt via `sudo` unless running as root or `--yes`.

3. **Install the shared portless daemon on `:8888`** (HTTP, no TLS):
   - Stop/remove any prior `:443`/HTTPS portless service first (per ref doc).
   - `PORTLESS_PORT=8888 PORTLESS_HTTPS=0 portless service install --no-tls --port 8888`.
   - Enable + start the systemd unit. Verify `systemctl is-active portless`.
   - Append `export PORTLESS_PORT=8888 PORTLESS_HTTPS=0` to `~/.bashrc` if
     absent (idempotent marker).

4. **Manage Caddy config at `~/.exe-devbox/caddy`** (the user's requested
   location):
   - Authoritative Caddyfile: `~/.exe-devbox/caddy/Caddyfile`.
   - Symlink it to `/etc/caddy/Caddyfile` (so the system service picks it up),
     or generate `/etc/caddy/Caddyfile` from it on each reload. **Decision:
     symlink** — single source of truth, survives package upgrades.
   - Seed a minimal Caddyfile with global `auto_https off` + `admin off`
     (Caddy is a plain HTTP hop; exe.dev did TLS) and a catch-all `404`.
   - The `Caddyfile` is **generated/edited by the CLI**; hand-edits are
     preserved in an `import ~/.exe-devbox/caddy/*.local.caddy` glob.
   - Use `systemctl restart caddy` (NOT `reload` — `admin off` disables the
     control endpoint).

5. **Write a caddy-reload helper** `~/.exe-devbox/bin/devbox-caddy-reload`
  (`caddy validate` then `systemctl restart caddy`) and add `~/.exe-devbox/bin`
  to PATH in `~/.bashrc` (idempotent).

6. **Point exe.dev at Caddy** — if `default_port != <caddy-port>`: emit the
   owner-only suggest link:
   `https://exe.dev/suggest?command=ssh%20exe.dev%20share%20port%20<vm>%20<caddy-port>`
   (can't run it — needs owner key).

7. **`devbox doctor` checks** at the end: node/npm/portless/caddy on PATH;
   `systemctl is-active caddy portless`; ports `:8080`/`:8888` listening;
   reflection port == caddy port; print a ✅/❌ table.

### 6.2 `~/.exe-devbox` layout

```
~/.exe-devbox/
├── config.json            # vm name, ports, cname target, dep versions
├── caddy/
│   ├── Caddyfile          # CLI-managed (source of truth)
│   └── *.local.caddy      # user hand-edits (imported by Caddyfile)
├── bin/
│   └── devbox-caddy-reload
└── state/                 # logs, last-run, ids of added domains
```

### 6.3 Idempotency & re-runnability

- `setup` is fully idempotent: installing an already-present dep is a no-op;
  the Caddyfile is regenerated deterministically; systemd units are
  re-enabled not duplicated. This is a hard requirement — the user will run it
  repeatedly.

---

## 7. Feature spec — `devbox new --domain <fqdn>`

### 7.1 Inputs

```
devbox new --domain new-app.devbox.nesin.io \
           [--project new-app] \
           [--to portless]          # default: portless (= *.localhost route)
           [--public]               # also emit set-public suggest link
```

- `--domain` (required): the public FQDN to serve.
- `--project` (default: first label of `--domain`, e.g. `new-app`): the
  portless route name → `<project>.localhost`, and the Caddy matcher name.
- `--to`: target backend. `portless` (default) writes the
  `header_up Host <project>.localhost` block; `loopback:<port>` writes a
  direct `reverse_proxy 127.0.0.1:<port>` (for non-portless services like
  Shelley on `:9999`).

### 7.2 What it does (in order)

**Step A — Resolve DNS provider of the apex.**
- Apex = public suffix + 1 (use a public-suffix list; fall back to "last two
  labels" for unknown TLDs). For `new-app.devbox.nesin.io` → apex `nesin.io`.
- `dig NS <apex>` → nameservers.
- Classify:
  - NS contains `*.cloudflare.com` → **provider = cloudflare**.
  - else `dig TXT _domainconnect.<apex>`:
    - present → **provider = DC-capable**, record the DC API URL.
    - absent → **provider = manual**.

**Step B — Add the CNAME (strategy by provider).**

The CNAME to add is always:

| Field | Value |
|---|---|
| Name / host | `<domain>` relative to apex, i.e. `new-app.devbox` for `…nesin.io` |
| Type | `CNAME` |
| Target | `<vm>.exe.xyz` (e.g. `nesins-devbox.exe.xyz`) |
| TTL | `auto` / `300` |
| Proxy | DNS-only (off) — proxying adds TLS/CNAME-chain quirks |

- **Cloudflare:**
  1. Construct the **Domain Connect synchronous apply URL** for documentation
  / future use (format below). Print it, clearly marked as
  *"requires a one-time exe-devbox template onboarding with Cloudflare; not
  one-click yet"*.
  2. **Today-working path:** if `CLOUDFLARE_API_TOKEN` (or token in config) is
  set → resolve the zone for the apex via the Cloudflare API
  (`/zones?name=<apex>`), create/update the CNAME record directly, print
  success. If no token → fall through to manual instructions (Step C).
- **Other DC-capable provider:** print the provider's DC apply URL (best-effort
  template) AND manual instructions.
- **Manual / unknown:** go straight to Step C.

**Step C — Manual instructions (always shown unless fully automated).**
- Print a copy-paste block with the exact record (table above) plus a
  per-provider hint (Cloudflare dashboard path, Route53, Namecheap, etc.).
- Offer to `dig CNAME <domain>` in a poll loop until it resolves to the target
  (`--wait`), with a sensible timeout.

**Step D — Register the domain on exe.dev (owner-only → suggest link).**
- Emit:
  `https://exe.dev/suggest?command=ssh%20exe.dev%20domain%20add%20<vm>%20<domain>`
- If `--public`, also:
  `https://exe.dev/suggest?command=ssh%20exe.dev%20share%20set-public%20<vm>`
- These cannot be run by the CLI (owner SSH key) — they're click-to-run links.

**Step E — Add the Caddy route (on-VM, automated).**
- Append/merge a block into `~/.exe-devbox/caddy/Caddyfile`:
  ```caddy
  @<project> host <domain>
  handle @<project> {
      reverse_proxy 127.0.0.1:8888 {
          header_up Host <project>.localhost
      }
  }
  ```
  (or `reverse_proxy 127.0.0.1:<port>` for `--to loopback:<port>`.)
- `caddy validate` + `systemctl restart caddy`.
- Record the domain + project in `~/.exe-devbox/state/domains.json`.
- Print the local sanity command:
  `curl -s -H "Host: <domain>" http://127.0.0.1:<caddy-port>/ | head`.

**Step F — (optional) HMR hint.** If `--to portless`, print the env var to set
in the project launcher so Vite HMR works through the proxy (ref doc §4):
`VITE_HMR_URL=<domain>`. (v1 just prints it; doesn't write the launcher.)

### 7.3 Domain Connect apply URL format (reference)

Synchronous apply, provider-side:
```
https://<dcApiUrl>/v2/<apex>/settings/<providerId>/<serviceId>/apply
  ?host=<host-label>&pointsTo=<cname-target>&TTL=300
  &sig=<base64url-signature>&key=<key-selector>
```
- `<dcApiUrl>` for Cloudflare = `api.cloudflare.com/client/v4/dns/domainconnect`
  (from `TXT _domainconnect.<apex>`).
- `sig` must be **last**; verified against a public key in `TXT` at the
  template's `syncPubKeyDomain`. → unusable without onboarding. Hence the
  direct-API fallback in Step B.

---

## 8. Supporting commands

- **`devbox status`** — VM name, reflection port vs Caddy port, portless
  routes (`portless list`), registered domains from `state/domains.json`,
  systemd unit states.
- **`devbox caddy show | edit | reload`** — `edit` opens
  `~/.exe-devbox/caddy/Caddyfile`; `reload` = validate + restart.
- **`devbox doctor`** — the health-check table (also runs at end of `setup`).

---

## 9. Owner-only vs on-VM (the auth split)

| Action | Who | Mechanism |
|---|---|---|
| Install deps, systemd, Caddy, portless | CLI | sudo on the VM |
| Add DNS record (Cloudflare API) | CLI | user's `CLOUDFLARE_API_TOKEN` |
| `ssh exe.dev domain add` | **owner** | suggest link |
| `ssh exe.dev share port` | **owner** | suggest link |
| `ssh exe.dev share set-public` | **owner** | suggest link |

The CLI never silently fails on owner-only steps — it always prints the exact
suggest link so the user can apply it in one click.

---

## 10. Dependencies & tech choices (Go CLI)

- **Go 1.26**, single static binary, cobra or urfave/cli for flags/subcommands.
- No heavy deps; prefer stdlib + small libs:
  - `net/http` for reflection + Cloudflare API.
  - `github.com/mattn/go-shellwords` or manual for suggest-link encoding.
  - A public-suffix list lib (`golang.org/x/net/publicsuffix`) for apex
    detection.
  - Colored TTY output (`fatih/color`) — auto-disabled if not a TTY / `--json`.
- DNS lookups via the system resolver (`net.LookupNS`, `net.LookupTXT`) — no
  shell-out to `dig` required.
- Telemetry: **none**. Purely local.

---

## 11. Risks & open questions

| # | Risk / question | Lean |
|---|---|---|
| R1 | Cloudflare DC can't be one-click without template onboarding. | Ship direct-API fallback now; track real DC as a post-onboarding epic. |
| R2 | CNAME target was specified as `exe.dev` but resolves only as `exe.xyz`. | Use `exe.xyz`; confirm with user. |
| R3 | Node LTS install method: NodeSource apt vs nvm. | NodeSource for system `/usr/bin/node` (services need absolute path); reuse nvm if already newer. |
| R4 | Should `devbox new` also write project launchers / HMR env? | v1: print only. v2: opt-in `--wire`. |
| R5 | exe.dev suggest-link command encoding — confirm exact `ssh exe.dev …` syntax (`domain add <vm> <domain>` order, `share port <vm> <n>`). | Confirm against `exe.dev/docs.md` before shipping. |
| R6 | Public-suffix list staleness for apex detection. | Bundle `x/net/publicsuffix` (compiled-in DAT); accept `--apex` override. |
| R7 | Caddyfile merging on repeated `devbox new`. | Maintain sections delimited by `# BEGIN devbox:<project>` / `# END` markers; replace in place. |

---

## 12. Milestones

- **M1 — Skeleton:** Go module, cobra skeleton, `devbox doctor` + reflection
  discovery + `--json`. (validate the reflection/CLI ergonomics)
- **M2 — `devbox setup`:** dep install (node/portless/caddy), portless daemon,
  `~/.exe-devbox/caddy` management, share-port suggest link.
- **M3 — `devbox new`:** apex/provider detection, manual flow + suggest link +
  Caddy route merge. (no Cloudflare automation yet)
- **M4 — Cloudflare API path:** token-based CNAME apply; DC URL documented.
- **M5 — Polish:** `status`, `caddy` subcmds, `doctor` table, docs, install
  script (`go install` + a one-liner).

---

## 13. Out of scope / future

- Real one-click Domain Connect (needs template onboarding with Cloudflare).
- Managing project dev servers / HMR launcher wiring (beyond printing hints).
- Multi-VM / fleet management.
- GUI.
- Non-Linux hosts.
