# Xensus

Identity registry for Microsoft 365 tenants. Hand out canonical, never-reused person IDs across HR, contractor, vendor, and account systems — without trying to be the master of any of them.

> **Status:** under construction. Not yet ready for use. See [the plan](https://github.com/excelano/xensus) for development progress.

## What it does

Xensus is a self-hosted registry that:

- Assigns permanent `X-000123`-style IDs to people, never reused
- Records associations between people and source systems (HR, Active Directory, FieldGlass, ServiceNow, etc.) with the foreign identifier in each system
- Maintains the full assertion history via an immutable audit log
- Authenticates via your own Microsoft 365 / Entra tenant

It does **not**:

- Sync data from source systems — that's integration work
- Enforce uniqueness or deduplicate — the registry records what stewards assert, doesn't second-guess them
- Replace your HR system, AD, FieldGlass, or any source system

The goal is to be the smallest possible thing that solves the "we have people in five places and no single source of truth" problem without trying to *be* the source of truth for any one of those places.

## Install

Coming with the v0.0.1 release.

## Configuration

Coming with the v0.3 release.

## License

MIT. See [LICENSE](LICENSE).

## Security

See [SECURITY.md](SECURITY.md) for vulnerability reporting and the data Xensus stores.
