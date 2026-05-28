-- 0001_initial.sql -- Xensus V1 schema.
--
-- All tables that Xensus uses for its life as v1.* land in this migration.
-- Subsequent migrations are append-only narrow changes (rename a column,
-- add an index) and never edits to existing migration files.

CREATE TABLE config (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    tenant_id TEXT,
    schema_version INTEGER NOT NULL DEFAULT 0,
    bootstrapped_at TEXT
);
INSERT INTO config (id) VALUES (1);

CREATE TABLE persons (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    created_by TEXT NOT NULL,
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_by TEXT NOT NULL
);

-- Systems are disabled, never deleted: disabling drops a system from the
-- active set but keeps its row and full history for the permanent record.
CREATE TABLE systems (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    created_by TEXT NOT NULL,
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_by TEXT NOT NULL,
    disabled_at TEXT,
    disabled_by TEXT
);

-- Associations are hard-deleted, not soft-removed: a removed link's only
-- permanent trace lives in audit_log (association.create / association.remove).
-- Duplicates are allowed by design (a re-add is always a fresh row), so a
-- soft-removed row could never be resurrected — keeping it would be dead
-- weight on every query. No removed_at column, plain (not partial) indexes.
CREATE TABLE associations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    person_id INTEGER NOT NULL REFERENCES persons(id),
    system_id INTEGER NOT NULL REFERENCES systems(id),
    foreign_id TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    created_by TEXT NOT NULL
);

CREATE INDEX associations_person_idx ON associations(person_id);
CREATE INDEX associations_system_idx ON associations(system_id);

CREATE TABLE stewards (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_oid TEXT NOT NULL,
    user_upn TEXT NOT NULL,
    promoted_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    promoted_by TEXT NOT NULL,
    removed_at TEXT,
    removed_by TEXT
);

CREATE UNIQUE INDEX stewards_active_oid_idx ON stewards(user_oid) WHERE removed_at IS NULL;

CREATE TABLE pending_stewards (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_upn TEXT NOT NULL UNIQUE,
    invited_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    invited_by TEXT NOT NULL
);

CREATE TABLE audit_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    occurred_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    actor_oid TEXT NOT NULL,
    actor_upn TEXT NOT NULL,
    action TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    entity_id INTEGER,
    details TEXT
);

CREATE INDEX audit_log_entity_idx ON audit_log(entity_type, entity_id);
CREATE INDEX audit_log_actor_idx ON audit_log(actor_oid);
CREATE INDEX audit_log_occurred_idx ON audit_log(occurred_at);
