# Xensus

Identity registry for Microsoft 365 tenants. Hand out canonical, never-reused person IDs across HR, contractor, vendor, and account systems — without trying to be the master of any of them.

Most organizations have people in five places and a single source of truth in none of them. HR tracks employees and some contractors. FieldGlass tracks staffing-vendor contractors but not all of them. Consultants are tracked nowhere. MSP-managed staff need system access but live in no corporate registry. Five years of trying to anoint one existing system as the master usually fails, because no source system's actual job is to track "everyone in the organization's orbit" — each owner backs out the moment scope exceeds their mandate. Xensus is the smallest thing that fills that gap: a registry whose only job is to assign a permanent ID to each person and record where that person shows up.

## What it does

Xensus is a self-hosted registry that:

- Assigns permanent `X-000123`-style IDs to people, never reused
- Records associations between people and source systems (HR, Active Directory, FieldGlass, ServiceNow, and so on), with the foreign identifier each system uses
- Maintains the full assertion history in an immutable audit log
- Authenticates users against your own Microsoft 365 / Entra tenant

It does **not**:

- Sync data from source systems — that's integration work, and it's deliberately out of scope
- Enforce uniqueness or deduplicate — the registry records what stewards assert and doesn't second-guess them
- Replace your HR system, AD, FieldGlass, or any source system

The goal is to be the smallest possible thing that solves the "we have people in five places and no single source of truth" problem without trying to *be* the source of truth for any one of those places.

## How it works

Xensus is built on one principle: stewards assert, and Xensus records. It is permissive by design. It does not try to decide who is real or merge records that look alike; it keeps a faithful, audited account of what the people responsible for the registry have stated. That makes it useful in exactly the situation where stricter tools fail — when the truth is genuinely messy and lives across systems that disagree.

Every person gets a permanent integer ID, serialized everywhere as `X-000123`. IDs are never reused: people are immortal in the registry, there is no delete, and the underlying key is `AUTOINCREMENT` so a retired ID can never come back attached to someone else. An association links a person to a system and carries the foreign identifier that system knows them by — an employee number in Workday, a `sAMAccountName` in Active Directory, a worker ID in FieldGlass. Duplicate associations are allowed on purpose: if a steward records the same person in the same system twice, that is recorded rather than rejected, because the registry's job is to capture assertions, not to overrule them.

Systems and stewards are never hard-deleted either, only deactivated reversibly. A system can be *disabled* (a toggle that drops it from the active list but keeps its history reachable) and re-enabled later. A steward can be *demoted* and, if needed, promoted again. Associations are the one thing that is truly removed — but the removal itself is written to the audit log with the foreign identifier intact, so the trail survives even when the join row does not.

Every write — create, rename, associate, remove, disable, promote — is recorded in an immutable audit log within the same database transaction as the change itself, stamped with the acting user's identity. Nothing mutates the registry without leaving a record of who did it and when.

Authentication runs entirely through your own Microsoft Entra tenant over OpenID Connect; Xensus never calls Microsoft Graph and reads nothing beyond the sign-in token. The first person to sign in to a fresh deployment binds it to their tenant permanently and becomes its first steward. That binding is one-way: tokens from any other tenant are rejected before any session is created. From then on, stewards can make changes and any signed-in member of the tenant can read the registry — though you can lock individual read surfaces to stewards too (see [Restricting reads to stewards](#restricting-reads-to-stewards)).

## A first session

A new deployment starts empty. The first steward signs in at the deployment's URL, which hands off to the Microsoft sign-in page; on return, Xensus binds the tenant and records the bootstrap in the audit log. The Persons page greets the first steward with a short welcome panel that explains the two-step setup and then clears itself for good once any data exists.

The natural order is systems first, then people. On the **Systems** page, a steward adds the source systems worth tracking — Workday, Active Directory, FieldGlass — each by name. On the **Persons** page, adding a person mints the next permanent ID; the name is optional, because sometimes all you have at first is "whoever owns employee #12345." Opening a person's detail page, the steward links them to each system they appear in and records the foreign identifier for that system. The person now reads at a glance as one identity spanning Workday, AD, and FieldGlass, with the foreign IDs that tie them together.

From there it is a working registry. The search box on each list filters by name. A person or system detail page shows its full audit history on demand. The **Audit** page is a filterable, newest-first timeline of every change across the registry, narrowable by entity type, actor, and date range. Any list exports to CSV, and a steward can pull the entire registry — persons, systems, associations, stewards, and the audit log — as a single zip of CSVs from the footer link or `GET /api/v1/export`.

Stewardship passes by invitation. A steward promotes a colleague by UPN; the invitation waits in a pending queue and is claimed automatically the first time that person signs in, at which point they become a steward and both events land in the audit log. A steward cannot remove themselves — the registry insists on a successor — so a deployment can never be left with no one able to maintain it.

## Install

Xensus ships a single static binary (pure Go, no CGO) for Linux, macOS, and Windows on amd64 and arm64.

### Quick install (Linux, macOS)

```sh
curl -fsSL https://raw.githubusercontent.com/excelano/xensus/main/install.sh | sh
```

The script resolves the latest release, downloads the binary for your platform, verifies its SHA-256 against the published `checksums.txt`, and installs it to `/usr/local/bin` (falling back to `~/.local/bin` if that isn't writable). Pin a specific version with `XENSUS_VERSION=v1.0.0` or change the target with `XENSUS_INSTALL_DIR=/opt/bin`.

### Debian / Ubuntu (.deb)

Download the `.deb` for your architecture from the [latest release](https://github.com/excelano/xensus/releases/latest) and install it:

```sh
sudo dpkg -i xensus_1.0.0_linux_amd64.deb
```

This installs the binary to `/usr/bin/xensus` and the documentation under `/usr/share/doc/xensus/`.

### Manual

Download the tarball for your platform from the [releases page](https://github.com/excelano/xensus/releases), verify it against `checksums.txt`, and extract the binary onto your `PATH`:

```sh
sha256sum -c checksums.txt --ignore-missing
tar -xzf xensus_1.0.0_linux_amd64.tar.gz xensus
sudo install -m 0755 xensus /usr/local/bin/xensus
```

### From source

Requires Go 1.22 or newer:

```sh
go install github.com/excelano/xensus@latest
```

Confirm any install with `xensus --version`.

## Configuration

Xensus is configured entirely through environment variables — there is no config file, which means one less thing to forget when running under `systemd` or a container runtime. Before the server can do anything beyond serving `/health`, it needs a Microsoft Entra app registration and a handful of variables.

### Register an Entra application

Each Xensus deployment uses its own Entra app registration, owned by the tenant it serves. There is no shared, Excelano-published registration, so your tenant administrators keep full control over consent, conditional access, and revocation.

1. In the [Microsoft Entra admin center](https://entra.microsoft.com), go to **Applications → App registrations → New registration**.
2. Give it a name (for example, `Xensus`). Under **Supported account types**, choose **Accounts in this organizational directory only** (single tenant).
3. Under **Redirect URI**, select **Web** and enter your deployment's callback URL: `https://xensus.example.com/auth/callback`. This must exactly match `XENSUS_OIDC_REDIRECT_URL`.
4. Register the application, then copy the **Application (client) ID** from the overview page — this is `XENSUS_OIDC_CLIENT_ID`.
5. Go to **Certificates & secrets → New client secret**, create one, and copy its **Value** immediately (it is shown only once) — this is `XENSUS_OIDC_CLIENT_SECRET`.
6. No API permissions are required. Xensus uses only the standard OpenID Connect scopes (`openid`, `profile`, `email`) and does not call Microsoft Graph, so there is nothing to consent to beyond sign-in.

### Environment variables

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `XENSUS_DATA_DIR` | Yes | — | Directory for the SQLite database. Xensus creates `xensus.sqlite` (plus its WAL sidecar files) inside it. |
| `XENSUS_OIDC_CLIENT_ID` | For sign-in | — | The Entra application (client) ID. |
| `XENSUS_OIDC_CLIENT_SECRET` | For sign-in | — | The Entra client secret value. |
| `XENSUS_OIDC_REDIRECT_URL` | For sign-in | — | The callback URL, matching the redirect URI registered in Entra (e.g. `https://xensus.example.com/auth/callback`). |
| `XENSUS_LISTEN` | No | `:8080` | Listen address, as `host:port` or `:port`. Bind to `127.0.0.1:8080` when running behind a reverse proxy. |
| `XENSUS_SESSION_KEY` | Recommended | random per start | Base64-encoded 32-byte key for encrypting session cookies. If unset, Xensus generates one at startup and warns — every active session is then invalidated on restart. |
| `XENSUS_TRUST_PROXY` | No | `false` | Set to `true` behind a TLS-terminating reverse proxy so Xensus honors `X-Forwarded-Proto` when deciding whether to mark cookies `Secure`. |
| `XENSUS_STEWARD_ONLY` | No | all open | Comma-separated list of read surfaces to restrict to stewards. See below. |

Without the three OIDC variables the process still starts and answers `/health`, but every sign-in and registry route is disabled — set them before the deployment is of any use.

### Generating a session key

```sh
openssl rand -base64 32
```

Set the result as `XENSUS_SESSION_KEY` and keep it stable across restarts so users stay signed in. Treat it like a secret: anyone who has it can forge session cookies.

### Restricting reads to stewards

By default any signed-in member of the bound tenant can read the registry, while only stewards can change it. If a surface should be readable only by stewards, name it in `XENSUS_STEWARD_ONLY` as a comma-separated list drawn from `persons`, `systems`, `stewards`, and `audit`. For example, `XENSUS_STEWARD_ONLY=audit,stewards` keeps the people and systems directories open to the tenant but hides the audit timeline and the steward roster from non-stewards. The same policy gates both the web pages and the corresponding API routes, and the navigation hides links a given user cannot follow. An unrecognized surface name is a hard startup error rather than a silent no-op, so a typo can't quietly leave something exposed. Writes always require a steward regardless of this setting, and the full-registry export is always steward-only.

## Running Xensus

### As a systemd service

Create a dedicated system user and data directory:

```sh
sudo useradd --system --home /var/lib/xensus --shell /usr/sbin/nologin xensus
sudo mkdir -p /var/lib/xensus /etc/xensus
sudo chown xensus:xensus /var/lib/xensus
```

Put the configuration in `/etc/xensus/xensus.env` (readable only by root, since it holds the client secret and session key — `sudo chmod 600 /etc/xensus/xensus.env`):

```sh
XENSUS_LISTEN=127.0.0.1:8080
XENSUS_DATA_DIR=/var/lib/xensus
XENSUS_OIDC_CLIENT_ID=00000000-0000-0000-0000-000000000000
XENSUS_OIDC_CLIENT_SECRET=your-client-secret
XENSUS_OIDC_REDIRECT_URL=https://xensus.example.com/auth/callback
XENSUS_SESSION_KEY=base64-32-byte-key
XENSUS_TRUST_PROXY=true
```

Write `/etc/systemd/system/xensus.service`:

```ini
[Unit]
Description=Xensus identity registry
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/bin/xensus
EnvironmentFile=/etc/xensus/xensus.env
User=xensus
Group=xensus
Restart=on-failure

# Hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=/var/lib/xensus

[Install]
WantedBy=multi-user.target
```

Adjust `ExecStart` to `/usr/local/bin/xensus` if you installed via the curl-pipe script rather than the `.deb`. Then enable and start it:

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now xensus
sudo journalctl -u xensus -f
```

Xensus logs to standard error as structured JSON, one line per request (method, path, status, duration, and a per-request `request_id` that is also returned in the `X-Request-Id` response header), which `journalctl` captures directly.

### Behind a reverse proxy

Run Xensus bound to localhost and let a reverse proxy terminate TLS. Point `XENSUS_OIDC_REDIRECT_URL` at the public HTTPS URL, set `XENSUS_TRUST_PROXY=true`, and make sure the proxy forwards `X-Forwarded-Proto` so session cookies are marked `Secure`.

nginx:

```nginx
server {
    listen 443 ssl;
    server_name xensus.example.com;
    # ssl_certificate / ssl_certificate_key ...

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-For $remote_addr;
    }
}
```

Caddy handles TLS and the forwarded headers for you:

```caddy
xensus.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

## The REST API

Everything the web UI does is also available under `/api/v1/`. Web requests carry an encrypted session cookie from sign-in; API requests authenticate with a Microsoft Entra bearer token for the Xensus app registration in the `Authorization: Bearer <token>` header. Both paths resolve to the same user identity and obey the same steward rules. Person IDs are accepted as either `X-000123` or the bare integer `123`, and always returned in the `X-000123` form.

Reads are open to any signed-in tenant member (unless restricted via `XENSUS_STEWARD_ONLY`); writes require a steward.

| Method | Path | Access | Purpose |
| --- | --- | --- | --- |
| `GET` | `/api/v1/me` | Any user | The caller's own identity and steward status |
| `GET` | `/api/v1/persons` | Read | List/search persons (`?q=`) |
| `GET` | `/api/v1/persons/{id}` | Read | A person and their associations |
| `GET` | `/api/v1/persons.csv` | Read | Persons as CSV |
| `POST` | `/api/v1/persons` | Steward | Create a person |
| `PATCH` | `/api/v1/persons/{id}` | Steward | Rename a person |
| `GET` | `/api/v1/persons/{id}/associations` | Read | A person's system associations |
| `POST` | `/api/v1/persons/{id}/associations` | Steward | Link a person to a system |
| `DELETE` | `/api/v1/persons/{id}/associations/{aid}` | Steward | Remove an association (audited) |
| `GET` | `/api/v1/systems` | Read | List/search active systems |
| `GET` | `/api/v1/systems/disabled` | Read | List disabled systems |
| `GET` | `/api/v1/systems/{id}` | Read | A system |
| `GET` | `/api/v1/systems.csv` | Read | Active systems as CSV |
| `POST` | `/api/v1/systems` | Steward | Create a system |
| `PATCH` | `/api/v1/systems/{id}` | Steward | Rename a system |
| `POST` | `/api/v1/systems/{id}/disable` | Steward | Disable a system |
| `POST` | `/api/v1/systems/{id}/enable` | Steward | Re-enable a system |
| `GET` | `/api/v1/audit` | Read | Filterable audit timeline |
| `GET` | `/api/v1/audit.csv` | Read | Audit log as CSV |
| `GET` | `/api/v1/stewards` | Read | Current stewards and pending invitations |
| `POST` | `/api/v1/stewards` | Steward | Invite a steward by UPN |
| `DELETE` | `/api/v1/stewards/{id}` | Steward | Demote a steward (never yourself) |
| `DELETE` | `/api/v1/stewards/pending/{id}` | Steward | Cancel a pending invitation |
| `GET` | `/api/v1/export` | Steward | Whole registry as a zip of CSVs |

The `/api/v1/` surface is stable as of v1.0.

## Data and backups

All state lives in one SQLite database at `$XENSUS_DATA_DIR/xensus.sqlite`. Because Xensus runs in WAL mode, you will also see `xensus.sqlite-wal` and `xensus.sqlite-shm` alongside it; back up the set together. The simplest backup is an online snapshot that needs no downtime:

```sh
sqlite3 /var/lib/xensus/xensus.sqlite ".backup '/var/backups/xensus-$(date +%F).sqlite'"
```

Or stop the service and copy the file directly. To restore, stop Xensus, replace `xensus.sqlite` (and remove any stale `-wal`/`-shm` sidecars), and start it again. There is nothing else to back up — the tenant binding, stewards, registry, and full audit history are all in that one file.

## License

MIT. See [LICENSE](LICENSE).

## Security

See [SECURITY.md](SECURITY.md) for vulnerability reporting and the data Xensus stores.
