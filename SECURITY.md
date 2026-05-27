# Security Policy

## Reporting a vulnerability

Please report suspected vulnerabilities privately through GitHub Security Advisories at https://github.com/excelano/xensus/security/advisories/new. If you would rather not use GitHub, email david.anderson@excelano.com instead. I aim to respond within seven days.

Please do not open public issues for security problems.

## Supported versions

The latest v1.x release receives security fixes. Older versions are not supported.

## What Xensus can access

Xensus is a self-hosted HTTP server that runs on infrastructure you control. It authenticates users against your own Microsoft Entra (Azure AD) app registration. The app registration requests only the OpenID Connect scopes `openid`, `profile`, and `email` — Xensus does not call the Microsoft Graph API, does not read mail or calendars, and cannot operate outside what your tenant exposes via the OIDC ID token. All identity data that lives in Xensus was put there by a steward signing in to your deployment; the registry never pulls data from any source system.

Tenant binding is one-way: once a Xensus deployment has been bound to a tenant (by the first successful sign-in), tokens from any other tenant are rejected before any session or context state is created. There is no operator-level cross-tenant access path.

## What Xensus stores

Xensus stores its state in a single SQLite database file at the configured data directory (default `/var/lib/xensus/xensus.db`). The database holds:

- The bound Microsoft Entra tenant ID
- Person records (X-IDs and the names stewards have entered)
- System records (the source systems stewards have defined)
- Associations between persons and systems, including the foreign identifiers stewards have entered
- Steward grants and the pending-steward invitation queue
- The full audit log of every write, including the acting user's object ID and UPN

Session cookies are encrypted with a server-side key (`XENSUS_SESSION_KEY`). The session payload contains the signed-in user's identity claims; it never contains the OAuth access token or refresh token.

Xensus does not phone home. The only outbound network calls are to Microsoft's identity endpoints (`login.microsoftonline.com` for OIDC discovery, JWKS, and the token exchange) during sign-in. There is no telemetry, no analytics, no remote logging.

## Self-hosting and Entra app registration

Each Xensus deployment uses its own Microsoft Entra app registration owned by the deploying tenant. There is no Excelano-published app registration that all deployments share. Tenant admins retain full control over consent, conditional access, and revocation. See the README for the registration walkthrough.

## Verifying releases

Every GitHub release includes a `checksums.txt` file listing SHA-256 hashes of all binary archives. Verify any download before running it:

    sha256sum xensus_1.0.0_linux_amd64.tar.gz
    # compare against the value in checksums.txt

Release artifacts are built by GitHub Actions from a tagged commit using the goreleaser configuration in this repo. The workflow and build configuration are public and auditable.
