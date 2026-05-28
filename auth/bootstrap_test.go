package auth

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/excelano/xensus/store"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// A modernc.org/sqlite ":memory:" database is per-connection, so a pool
	// that opens a second connection sees an empty schema. Pin the pool to a
	// single connection so every query hits the migrated database.
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if err := store.ApplyMigrations(db); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	return db
}

// TestCompleteSignIn_BootstrapsOnFirstSignIn proves the Slice 3b demo:
// against a fresh database, the first sign-in atomically binds the
// tenant, creates the steward, writes the bootstrap audit row, sets a
// session cookie, and updates the Authenticator's in-memory tenant ID.
func TestCompleteSignIn_BootstrapsOnFirstSignIn(t *testing.T) {
	db := newTestDB(t)
	a := &Authenticator{
		sessions: newTestSessionStore(t),
		db:       db,
		// tenantID intentionally empty — fresh deployment
	}
	ls := &loginData{
		State:    "s",
		Nonce:    "n",
		ReturnTo: "/",
		Exp:      time.Now().Add(time.Minute).Unix(),
	}
	claims := Claims{
		OID:   "first-steward-oid",
		UPN:   "first@bootstrap.onmicrosoft.com",
		TID:   "tenant-bootstrap",
		Nonce: "n",
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/auth/callback", nil)
	a.completeSignIn(w, r, claims, ls)

	if w.Code != http.StatusFound {
		t.Fatalf("want 302, got %d (body=%q)", w.Code, w.Body.String())
	}
	if got := a.getTenant(); got != claims.TID {
		t.Errorf("in-memory tenantID: got %q want %q", got, claims.TID)
	}
	var dbTID string
	if err := db.QueryRow(`SELECT tenant_id FROM config WHERE id=1`).Scan(&dbTID); err != nil {
		t.Fatal(err)
	}
	if dbTID != claims.TID {
		t.Errorf("db tenant_id: got %q want %q", dbTID, claims.TID)
	}
	var stewards int
	if err := db.QueryRow(`SELECT COUNT(*) FROM stewards WHERE user_oid=?`, claims.OID).Scan(&stewards); err != nil {
		t.Fatal(err)
	}
	if stewards != 1 {
		t.Errorf("steward count for first signer: got %d want 1", stewards)
	}
	var audit int
	if err := db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action='tenant.bootstrap'`).Scan(&audit); err != nil {
		t.Fatal(err)
	}
	if audit != 1 {
		t.Errorf("bootstrap audit count: got %d want 1", audit)
	}
	found := false
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookieName && c.Value != "" {
			found = true
		}
	}
	if !found {
		t.Errorf("session cookie not set after successful bootstrap")
	}
}

// TestCompleteSignIn_PostBootstrap_RejectsForeignTenant pins the
// "second-tenant user after bind → 403, no state mutated" invariant
// from the plan. After a successful bootstrap, a sign-in from a
// different tid must be refused and must not alter the steward table
// or the audit log.
func TestCompleteSignIn_PostBootstrap_RejectsForeignTenant(t *testing.T) {
	db := newTestDB(t)
	a := &Authenticator{
		sessions: newTestSessionStore(t),
		db:       db,
	}
	ls := &loginData{State: "s", Nonce: "n", ReturnTo: "/", Exp: time.Now().Add(time.Minute).Unix()}

	// First: bootstrap with tenant A.
	first := Claims{OID: "oid-a", UPN: "a@a.onmicrosoft.com", TID: "tenant-a", Nonce: "n"}
	w1 := httptest.NewRecorder()
	r1 := httptest.NewRequest("GET", "/auth/callback", nil)
	a.completeSignIn(w1, r1, first, ls)
	if w1.Code != http.StatusFound {
		t.Fatalf("bootstrap sign-in: want 302 got %d", w1.Code)
	}

	// Snapshot state for later comparison.
	var (
		stewardsBefore, auditBefore int
		tidBefore                   string
	)
	_ = db.QueryRow(`SELECT tenant_id FROM config WHERE id=1`).Scan(&tidBefore)
	_ = db.QueryRow(`SELECT COUNT(*) FROM stewards`).Scan(&stewardsBefore)
	_ = db.QueryRow(`SELECT COUNT(*) FROM audit_log`).Scan(&auditBefore)

	// Second: foreign-tenant attempt.
	intruder := Claims{OID: "oid-b", UPN: "b@b.onmicrosoft.com", TID: "tenant-b", Nonce: "n"}
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("GET", "/auth/callback", nil)
	a.completeSignIn(w2, r2, intruder, ls)

	if w2.Code != http.StatusForbidden {
		t.Fatalf("foreign-tenant sign-in: want 403 got %d (body=%q)", w2.Code, w2.Body.String())
	}
	for _, c := range w2.Result().Cookies() {
		if c.Name == sessionCookieName && c.Value != "" {
			t.Fatalf("session cookie set despite foreign-tenant rejection: %+v", c)
		}
	}

	var tidAfter string
	var stewardsAfter, auditAfter int
	_ = db.QueryRow(`SELECT tenant_id FROM config WHERE id=1`).Scan(&tidAfter)
	_ = db.QueryRow(`SELECT COUNT(*) FROM stewards`).Scan(&stewardsAfter)
	_ = db.QueryRow(`SELECT COUNT(*) FROM audit_log`).Scan(&auditAfter)
	if tidAfter != tidBefore {
		t.Errorf("tenant_id mutated by foreign sign-in: %q -> %q", tidBefore, tidAfter)
	}
	if stewardsAfter != stewardsBefore {
		t.Errorf("steward count changed: %d -> %d", stewardsBefore, stewardsAfter)
	}
	if auditAfter != auditBefore {
		t.Errorf("audit row count changed: %d -> %d", auditBefore, auditAfter)
	}
}
