# PRD — `exebox`

> A Go CLI that automates the "multi-project dev behind nginx + portless on an
> exe.dev VM" workflow documented in
> `~/Learn/multi-project-caddy-portless-exedev.md`.
>
> Status: **M1–M3 shipped; live on `nesins-devbox`.** Caddy fully removed;
> nginx + portless in production. M4 (Cloudflare API automation) next.
>
> Owner: nesintechnologies@gmail.com (VM `nesins-devbox`).

---

## 1. Problem

The manual setup in the reference doc is long and error-prone: install Node
LTS + portless + nginx, install one shared portless daemon on `:8888`, write a
Host-rewriting reverse-proxy config on `:8080`, point exe.dev's main proxy at
nginx, figure out the VM name/port, and — for every project — add a public
subdomain that needs (a) a DNS CNAME to `<vm>.exe.xyz` and (b) an exe.dev
domain registration. Doing this by hand for each new project is the friction
we're removing.

`exebox` (binary name **`exebox`**) automates two things:

1. **One-time VM bring-up** (`exebox setup`) — install deps, manage nginx
   config at `~/.exebox/nginx`, and discover the VM's identity.
2. **Per-project domain onboarding** (`exebox new --domain …`) — figure out the
   DNS provider of the target domain, give the user the easiest possible path
   to add the CNAME (Domain Connect if supported, else manual instructions),
   then emit the exe.dev command/link to register the domain for this VM.

---

## 2. Goals / Non-goals

### Goals
- **G1.** `exebox setup` gets a fresh VM from zero to a working
  nginx-on-`:8080` + portless-on-`:8888` reverse-proxy stack, idempotent and
  re-runnable, with nginx config managed under `~/.exebox/nginx`.
- **G2.** `exebox setup` auto-discovers the VM's **name** and **default port**
  from the exe.dev reflection endpoint (`reflection.int.exe.xyz`) — no flags
  required for the common case.
- **G3.** `exebox new --domain <fqdn>` determines the authoritative DNS
  provider of the domain's apex and chooses the right CNAME-add strategy:
  - Cloudflare → Domain Connect flow (with a today-working direct-API fallback).
  - Any other Domain-Connect-supporting provider → Domain Connect apply URL.
  - Unknown / unsupported provider → clear manual instructions with exact
    record values.
- **G4.** Every `exebox new` run ends by emitting the **exe.dev suggest link**
  to register the domain for this VM (and the share-port link if the VM's
  default port isn't already pointing at nginx).
- **G5.** Distinguish **on-VM automation** (can run directly) from **owner-only
  exe.dev commands** (need the owner's SSH key → surfaced as
  `https://exe.dev/suggest?command=…` click-to-run links).

### Non-goals (v1)
- Managing project dev servers themselves (`pnpm run dev`, HMR wiring). That
  stays in each project's launcher / the reference doc's `~/bin/exebox-start`.
  v1 may *document* it but not own it.
- TLS/cert automation — exe.dev terminates TLS; nginx is a plain HTTP hop.
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

### Why nginx (not Caddy)

We started on Caddy (see `~/Learn/multi-project-caddy-portless-exedev.md`) and
**migrated to nginx**, now live in production. Rationale: exe.dev terminates
TLS so Caddy's headline feature (automatic HTTPS) was turned off
(`auto_https off`), leaving only plain HTTP reverse-proxying + a `Host` header
rewrite — which nginx does natively. nginx is preinstalled on the VM
(`/usr/sbin/nginx`) and ubiquitous. We keep **portless** as the per-project
route layer so existing `portless run` dev scripts are untouched.

> **Migration verified (2026-07-24):** Caddy `apt-get purge`'d; nginx serves the
> live Shelley domains on `:8080` with byte-equivalent behavior (HTTP 200,
> same `307` auth redirect through exe.dev). `exebox doctor` all-green.

**One nginx quirk worth knowing:** `systemctl reload nginx` can return just
before workers finish loading new config, so a request fired immediately after
adding a server block may hit a stale `404`. `exebox`'s `nginx.Reload` retries
+ settles 200ms to close that window (see `internal/nginx/conf.go`).

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

## 4. Architecture recap

```
https://<project>.<your-domain>          (browser)
   │  TLS by exe.dev edge
   ▼
exe.dev edge ──HTTP──► VM:nginx (:8080)
                            │  rewrite Host: <name>.localhost
                            ▼
                        VM:portless (:8888, one shared daemon)
                            │  routes by *.localhost Host
                            ▼
                        <project> dev server (auto port)
```

- One **shared portless** daemon on `:8888` (HTTP, no TLS), systemd-managed.
- **nginx** on `:8080` rewrites `Host` from the public name to
  `<project>.localhost` so portless can route.
- exe.dev's single main proxy port points at nginx (`8080`). All custom
  domains ride that one port → must all go through nginx.

`exebox setup` produces exactly this. `exebox new` adds the public-name
entries (DNS + exe.dev registration) and the nginx server block for a project.

---

## 5. CLI surface

```
exebox setup [--vm <name>] [--nginx-port 8080] [--portless-port 8888] [--yes]
exebox new -d <fqdn> [-d <fqdn> ...] [--project <name>] [--to portless|loopback:<port>] [--public] [--wait]
exebox dev [project] [--dir <path>] [--runner pnpm|npm] [--foreground]
exebox status [--no-probe]
exebox nginx (reload | edit | show)
exebox doctor
```

Global flags: `--config <dir>` (default `~/.exebox`), `--json` (machine
output), `--yes` (skip confirmations), `-v/--verbose`.

---

## 6. Feature spec — `exebox setup`

### 6.1 What it does (in order)

1. **Discover VM identity** (unless `--vm`/`--port` given):
   - `GET https://reflection.int.exe.xyz/` → `name`.
   - `GET https://reflection.int.exe.xyz/default_port` → `default_port`.
   - Store in `~/.exebox/config.json`.
   - Derive `cname-target = <name>.exe.xyz`.

2. **Install dependencies** (detect & skip if present, pin versions in config):
   - **Node.js (latest LTS)** — prefer NodeSource apt repo for system-wide
     `/usr/bin/node`; detect existing nvm install and reuse its node if
     newer. Resolve "latest LTS" from
     `https://nodejs.org/dist/index.json` (filter `lts` field).
   - **portless** (`portless.sh`) — `sudo npm i -g portless` (NodeSource node
     installs globals into `/usr/lib/node_modules`, which needs root).
   - **nginx** — `apt-get install -y nginx`. Reuse if `/usr/sbin/nginx`
     exists (it is already present on this VM).
   - Requires sudo; prompt via `sudo` unless running as root or `--yes`.

3. **Install the shared portless daemon on `:8888`** (HTTP, no TLS):
   - Stop/remove any prior `:443`/HTTPS portless service first (per ref doc).
   - `PORTLESS_PORT=8888 PORTLESS_HTTPS=0 portless service install --no-tls --port 8888`.
   - Enable + start the systemd unit. Verify `systemctl is-active portless`.
   - Append `export PORTLESS_PORT=8888 PORTLESS_HTTPS=0` to `~/.bashrc` if
     absent (idempotent marker).

4. **Manage nginx config at `~/.exebox/nginx`** (the user's requested
   location) via a one-time **include shim**:
   - Write `/etc/nginx/conf.d/exebox.conf` (sudo, once) containing a single
     resolved include:
     `include /home/<user>/.exebox/nginx/conf.d/*.conf;`
     (home expanded at write time — nginx doesn't expand `~`).
   - All subsequent per-project server blocks live in
     `~/.exebox/nginx/conf.d/<project>.conf` — **no sudo needed to add or
     edit** them.
   - Seed a base file `~/.exebox/nginx/conf.d/00-exebox-base.conf` with:
     - a `map $http_upgrade $connection_upgrade` block (for WS/HMR upgrades),
     - a catch-all `server { listen <nginx-port> default_server; return 404; }`.
   - Each per-project server block `listen`s on `<nginx-port>` and uses
     `server_name <domain>`. Multiple domains → one block via space-separated
     `server_name`.
   - Reload semantics: **`sudo nginx -t && sudo systemctl reload nginx`**.
     `nginx -t` must run as root — non-root fails on `/run/nginx.pid`
     permission even when config is valid. Reload (not restart) works; it can
     return just before workers load new config, so `exebox` retries + settles
     (see "Why nginx" above).

5. **Write an nginx-reload helper** `~/.exebox/bin/exebox-nginx-reload`
  (`nginx -t` then `systemctl reload nginx`) and add `~/.exebox/bin`
  to PATH in `~/.bashrc` (idempotent).

6. **Point exe.dev at nginx** — if `default_port != <nginx-port>`: emit the
   owner-only suggest link:
   `https://exe.dev/suggest?command=share+port+<vm>+<nginx-port>`
   (lobby-command form, not `ssh exe.dev …`; can't run it — needs owner key).

   **Caddy handoff (done on this VM):** if another service (e.g. Caddy from the
   reference-doc era) is squatting on `<nginx-port>`, setup detects it via
   `sudo ss -tlnp` (process names need root), prompts (or proceeds with
   `--yes`), and stops+disables the unit so nginx can take over.

7. **`exebox doctor` checks** at the end: node/npm/portless/nginx on PATH;
   `systemctl is-active nginx portless`; ports `:8080`/`:8888` listening;
   reflection port == nginx port; print a ✅/❌ table.

### 6.2 `~/.exebox` layout

```
~/.exebox/
├── config.json            # vm name, ports, cname target, dep versions
├── nginx/
│   └── conf.d/
│       ├── 00-exebox-base.conf   # CLI-managed base (WS map + catch-all)
│       └── <project>.conf        # one per project, user-editable
├── bin/
│   └── exebox-nginx-reload
└── state/                 # logs, last-run, ids of added domains
```

### 6.3 Idempotency & re-runnability

- `setup` is fully idempotent: installing an already-present dep is a no-op;
  the base conf is regenerated deterministically; the include shim is written
  once (re-written identically if present); systemd units are re-enabled not
  duplicated. This is a hard requirement — the user will run it repeatedly.

---

## 7. Feature spec — `exebox new --domain <fqdn>`

### 7.1 Inputs

```
exebox new -d new-app.devbox.nesin.io \
           [-d <more-domains>...]   # repeat for multi-domain backends
           [--project new-app] \
           [--to portless]          # default: portless (= *.localhost route)
           [--public]               # also emit set-public suggest link
           [--wait]                 # poll DNS until CNAME resolves
```

- `-d/--domain` (required, repeatable): the public FQDN(s) to serve. Multiple
  domains share one backend (e.g. Shelley on two hostnames).
- `--project` (default: first label of the first domain, e.g. `new-app`): the
  portless route name → `<project>.localhost`, and the nginx conf filename.
- `--to`: target backend. `portless` (default) writes the
  `proxy_set_header Host <project>.localhost` block; `loopback:<port>` writes
  a direct `proxy_pass http://127.0.0.1:<port>` (for non-portless services
  like Shelley on `:9999`).

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
  *"requires a one-time exebox template onboarding with Cloudflare; not
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
Suggest links take the **lobby command** (e.g. `domain add <vm> <domain>`),
*not* the `ssh exe.dev …` form — see https://exe.dev/docs/suggest-links.md .
Note: `domain add` is **not** in the documented suggestable set (only
`share port` / `share set-public` / `share add` / `resize` / `ls` are), so we
emit both a suggest link *and* a fallback `ssh exe.dev …` shell command.
- Emit: `https://exe.dev/suggest?command=domain+add+<vm>+<domain>`
- Fallback (paste at https://exe.dev/shell): `ssh exe.dev domain add <vm> <domain>`
- If `--public`, also: `https://exe.dev/suggest?command=share+set-public+<vm>`
- These cannot be run by the CLI (owner SSH key) — they're click-to-run links.

**Step E — Add the nginx route (on-VM, automated).**
- Write `~/.exebox/nginx/conf.d/<project>.conf`:
  ```nginx
  # managed by exebox — project: new-app
  server {
      listen 8080;
      server_name new-app.devbox.nesin.io;

      location / {
          proxy_pass http://127.0.0.1:8888;
          proxy_set_header Host new-app.localhost;
          proxy_set_header X-Real-IP $remote_addr;
          proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
          proxy_set_header X-Forwarded-Proto $scheme;

          # WebSocket / Vite HMR upgrade
          proxy_http_version 1.1;
          proxy_set_header Upgrade $http_upgrade;
          proxy_set_header Connection $connection_upgrade;
          proxy_read_timeout 86400;
      }
  }
  ```
  For `--to loopback:<port>`: `proxy_pass http://127.0.0.1:<port>;` and
  `proxy_set_header Host $host;` instead of the `.localhost` rewrite.
- `nginx -t && systemctl reload nginx`.
- Record the domain + project in `~/.exebox/state/domains.json`.
- Print the local sanity command:
  `curl -s -H "Host: <domain>" http://127.0.0.1:<nginx-port>/ | head`.

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

- **`exebox status [--no-probe]`** — VM name, proxy port (nginx + portless),
  registered domains from `state/domains.json`, and a **live HTTP probe** per
  domain through nginx (`● live [HTTP 200]` / `○ no nginx route` /
  `○ backend not running`), plus `portless list`. `--no-probe` skips the HTTP
  checks for an instant view.
- **`exebox dev [project]`** — launch a portless project's dev server with the
  environment Vite HMR needs. See §8a below.
- **`exebox nginx show | edit | reload`** — `show` prints
  `~/.exebox/nginx/conf.d/*`; `edit` opens `<project>.conf`; `reload` =
  `nginx -t` + `systemctl reload nginx`.
- **`exebox doctor`** — the health-check table (also runs at end of `setup`).

### 8a. Vite HMR behind exe.dev + nginx (`exebox dev`)

**Symptom:** the app loads over HTTPS, but the console shows
`WebSocket connection to 'ws://groot.localhost:8888/?token=…' failed` and
`[vite] failed to connect to websocket`.

**Root cause (NOT nginx):** nginx proxies WebSocket upgrades correctly
(`map $http_upgrade $connection_upgrade` + `Upgrade`/`Connection` headers).
The break is that Vite computes its own HMR WebSocket target from the
`PORTLESS_URL` portless injects (`http://<project>.localhost:8888`), so the
browser is told to open `ws://<project>.localhost:8888`. That URL is doubly
broken: `<project>.localhost` resolves to **the user's laptop** (unreachable
from the browser), and `ws://` from an `https://` page is **mixed content**
(blocked before it tries).

**Fix:** override the HMR target to the public `wss://<host>:443` name. Both
groot and nesins-finance read `VITE_HMR_URL` (`packages/core/src/server.ts`):
```ts
if (process.env.VITE_HMR_URL) {
  wsConfig = { server: httpServer, protocol: "wss", host: process.env.VITE_HMR_URL, clientPort: 443 };
}
```
`exebox dev <project>` sets `VITE_HMR_URL=<public-domain>` (looked up from
state) plus `PORTLESS_PORT`/`PORTLESS_HTTPS=0`, prepends the **nvm bin dir**
to `PATH` (node/pnpm/portless aren't on a non-login PATH on this VM — without
it the dev server's child `pnpm` calls fail with `ENOENT`), kills any leftover
dev tree holding the `*.localhost` route, and runs `<runner> run dev`.

Boot log confirms it: `HMR configured for tunnel: wss://<host>:443`, and
`/@vite/client` then injects `socketProtocol="wss"`, `hmrPort=443`,
`socketHost="<public-host>"`. By default it detaches into its own session
(`--foreground` to keep it in the shell); logs to
`~/.exebox/state/<project>-dev.log`.

> **Lesson:** any dev server that self-computes a public/HMR URL from a
> loopback or `.localhost` address will hand the browser an unreachable
> (and mixed-content) WebSocket target when served through exe.dev + a proxy.
> Always launch portless dev servers via `exebox dev` (or set `VITE_HMR_URL`
> by hand) — `pnpm run dev` alone reproduces the bug.

---

## 9. Owner-only vs on-VM (the auth split)

| Action | Who | Mechanism |
|---|---|---|
| Install deps, systemd, nginx, portless | CLI | sudo on the VM |
| Add DNS record (Cloudflare API) | CLI | user's `CLOUDFLARE_API_TOKEN` |
| `ssh exe.dev domain add` | **owner** | suggest link |
| `ssh exe.dev share port` | **owner** | suggest link |
| `ssh exe.dev share set-public` | **owner** | suggest link |

The CLI never silently fails on owner-only steps — it always prints the exact
suggest link so the user can apply it in one click.

---

## 10. Dependencies & tech choices (Go CLI)

- **Go 1.26**, single static binary, **cobra** for flags/subcommands.
- Minimal deps; stdlib-first:
  - `net/http` for reflection + Cloudflare API.
  - `net/url.QueryEscape` for suggest-link encoding (no shellwords dep).
  - `golang.org/x/net/publicsuffix` for apex detection (the one external dep).
  - Colored TTY output via raw ANSI (no `fatih/color` dep), auto-disabled when
    not a TTY or `--json`.
- DNS lookups via the system resolver (`net.LookupNS`, `net.LookupTXT`) — no
  shell-out to `dig` required. Note: DC discovery TXT records are often
  scheme-less (`api.cloudflare.com/...`), so we accept any host/path token.
- Telemetry: **none**. Purely local.
- **Build/install:** `make install` builds to `~/.local/bin/exebox` and
  regenerates bash completion to `~/dotfiles/bash/exebox-completion.bash`
  (sourced from `~/.bashrc`).

---

## 11. Risks & open questions

| # | Risk / question | Lean |
|---|---|---|
| R1 | Cloudflare DC can't be one-click without template onboarding. | Ship direct-API fallback now; track real DC as a post-onboarding epic. |
| R2 | ✅ Resolved: CNAME target is `<vm>.exe.xyz` (`exe.dev` doesn't resolve). | Confirmed via cnames.md + reflection. |
| R3 | Node LTS install method: NodeSource apt vs nvm. | NodeSource for system `/usr/bin/node` (services need absolute path); reuse nvm if already newer. |
| R4 | Should `exebox new` also write project launchers / HMR env? | v1: print only. v2: opt-in `--wire`. |
| R5 | ✅ Resolved: suggest links take the **lobby command** (e.g. `share port <vm> <n>`), not `ssh exe.dev …`. `domain add` isn't in the suggestable set → emit suggest link + fallback shell command. | Confirmed against exe.dev/docs/suggest-links.md + cli-domain.md. |
| R6 | Public-suffix list staleness for apex detection. | Bundle `x/net/publicsuffix` (compiled-in DAT); accept `--apex` override. |
| R7 | ✅ Resolved: re-running `exebox new` rewrites `<project>.conf` wholesale (deterministic); `exebox setup` regenerates `00-exebox-base.conf`. State file upserts by FQDN, not duplicated. | Shipped. |
| R8 | ✅ Resolved: `/etc/nginx/conf.d/exebox.conf` resolves `$HOME` at write time; `exebox setup` rewrites it idempotently if the path changes. | Shipped. |
| R9 | **New (from cutover):** `nginx -t` must run as root (non-root fails on `/run/nginx.pid` perm). | Fixed: `nginx.Test()` uses `system.AsRoot`. |
| R10 | **New:** `npm i -g portless` needs sudo (NodeSource globals → `/usr/lib/node_modules`). | Fixed: `deps.InstallPortless` uses `sudo npm`. |
| R11 | **New:** `ss -tlnp` hides process names without root → Caddy handoff detection failed. | Fixed: `portOwner` runs `sudo ss` + trims quotes. |
| R12 | **New:** `systemctl reload nginx` has a stale-routing window after adding a server block. | Fixed: `nginx.Reload` retries + 200ms settle. |
| R13 | ✅ Resolved: `exebox dev <project>` launches portless dev servers with `VITE_HMR_URL=<public-host>` + nvm bin on PATH (resolves the `pnpm ENOENT` child-spawn failure + the broken `ws://<project>.localhost:8888` HMR target). | Shipped. |

---

## 12. Milestones

- ✅ **M1 — Skeleton:** Go module, cobra skeleton, `exebox doctor` + reflection
  discovery + `--json`. (shipped)
- ✅ **M2 — `exebox setup`:** dep install (node/portless/nginx), portless daemon,
  `~/.exebox/nginx` management + Caddy handoff, share-port suggest link.
  (shipped; used for the live Caddy→nginx cutover)
- ✅ **M3 — `exebox new`:** apex/provider detection, manual flow + suggest link +
  nginx server block write + reload. Multi-domain + loopback backends.
  (shipped; live for `devbox.nesin.io` + `devbox.ashiknesin.com` → Shelley)
- ⏳ **M4 — Cloudflare API path:** token-based CNAME apply; DC URL documented.
- ⏳ **M5 — Polish:** ✅ `status` + `dev` (HMR-aware dev launcher) shipped; `nginx`
  subcmds, `doctor` table, docs remaining.

---

## 13. Out of scope / future

- Real one-click Domain Connect (needs template onboarding with Cloudflare).
- Managing project dev servers / HMR launcher wiring (beyond printing hints).
- Multi-VM / fleet management.
- GUI.
- Non-Linux hosts.
