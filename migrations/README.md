# Migrations

This directory holds Xensus's append-only schema evolution. The migration runner in `store/store.go` reads every file matching `NNNN_*.sql` (where `NNNN` is a four-digit zero-padded version number), sorts them numerically, and applies any whose version is greater than the value stored in `config.schema_version`. After each successful application the runner writes the new version back to `config.schema_version`.

The rules are simple. Never edit a migration that has already shipped. Never reorder filenames. Never delete a migration. To change the schema, add a new file: `0002_add_thing.sql`, `0003_rename_column.sql`, and so on.

`modernc.org/sqlite`'s DDL isn't fully transactional the way Postgres DDL is, so a migration that fails midway can leave the database in a partially-applied state. Keep each migration small and recover-friendly: prefer multiple narrow migrations over one sprawling one. The initial migration (`0001_initial.sql`) is necessarily large because it lays down the entire V1 schema in a single shot; everything that follows should be smaller.

To inspect a running Xensus database directly, point the `sqlite3` CLI at `$XENSUS_DATA_DIR/xensus.sqlite` — the schema is plain SQL and small enough to read in one sitting.
