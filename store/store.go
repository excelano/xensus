// Package store is Xensus's storage layer. It owns the SQLite connection,
// applies migrations on open, and exposes typed helpers that always accept
// *sql.Tx — never *sql.DB — so that the core package retains exclusive
// control over transaction boundaries (and therefore over the audit
// invariant: every data change writes an audit row in the same Tx).
package store

import (
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"

	_ "modernc.org/sqlite"

	"github.com/excelano/xensus/migrations"
)

const dbFilename = "xensus.sqlite"

// Open creates dataDir if needed, opens the SQLite database inside it,
// configures the connection PRAGMAs, and applies any pending migrations.
// On return the database is ready for the rest of the program to use.
func Open(dataDir string) (*sql.DB, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir %q: %w", dataDir, err)
	}
	dbPath := filepath.Join(dataDir, dbFilename)

	// PRAGMAs are passed via the DSN so they apply to every connection in
	// the pool (modernc.org/sqlite resets them per-connection otherwise).
	dsn := "file:" + dbPath +
		"?_pragma=foreign_keys(1)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=busy_timeout(5000)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", dbPath, err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite %q: %w", dbPath, err)
	}
	if err := runMigrations(db, migrations.FS); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// ApplyMigrations applies any pending migrations from the embedded FS
// to an already-opened *sql.DB. Open calls this internally; the export
// exists so that tests can spin up an in-memory database with the full
// schema by calling sql.Open("sqlite", ":memory:") + ApplyMigrations.
func ApplyMigrations(db *sql.DB) error {
	return runMigrations(db, migrations.FS)
}

var migrationNameRE = regexp.MustCompile(`^(\d{4})_[^.]+\.sql$`)

type migration struct {
	version int
	name    string
	sql     string
}

func runMigrations(db *sql.DB, fs embed.FS) error {
	entries, err := fs.ReadDir(".")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}
	var migs []migration
	for _, e := range entries {
		m := migrationNameRE.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		v, err := strconv.Atoi(m[1])
		if err != nil {
			return fmt.Errorf("parse migration version from %q: %w", e.Name(), err)
		}
		b, err := fs.ReadFile(e.Name())
		if err != nil {
			return fmt.Errorf("read migration %s: %w", e.Name(), err)
		}
		migs = append(migs, migration{version: v, name: e.Name(), sql: string(b)})
	}
	sort.Slice(migs, func(i, j int) bool { return migs[i].version < migs[j].version })

	current, err := currentSchemaVersion(db)
	if err != nil {
		return fmt.Errorf("read schema_version: %w", err)
	}
	for _, m := range migs {
		if m.version <= current {
			continue
		}
		if _, err := db.Exec(m.sql); err != nil {
			return fmt.Errorf("apply migration %s: %w", m.name, err)
		}
		if _, err := db.Exec(`UPDATE config SET schema_version = ? WHERE id = 1`, m.version); err != nil {
			return fmt.Errorf("record schema_version after %s: %w", m.name, err)
		}
	}
	return nil
}

// currentSchemaVersion returns 0 before the very first migration has run
// (when the config table itself does not yet exist), and the persisted
// schema_version value afterwards.
func currentSchemaVersion(db *sql.DB) (int, error) {
	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'config'`,
	).Scan(&count); err != nil {
		return 0, err
	}
	if count == 0 {
		return 0, nil
	}
	var v int
	if err := db.QueryRow(`SELECT schema_version FROM config WHERE id = 1`).Scan(&v); err != nil {
		return 0, err
	}
	return v, nil
}
