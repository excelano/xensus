package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/excelano/xensus/core"
	"github.com/excelano/xensus/store"
)

func newTestSessionStore(t *testing.T) *SessionStore {
	t.Helper()
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		t.Fatal(err)
	}
	s, err := NewSessionStore(base64.StdEncoding.EncodeToString(key), false)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestCompleteSignIn_RejectsForeignTenant pins the Slice 3a invariant
// from the plan: a token whose tid does not match the bound tenant must
// be refused with 403 and no session cookie may be issued.
func TestCompleteSignIn_RejectsForeignTenant(t *testing.T) {
	a := &Authenticator{
		tenantID: "tenant-good",
		sessions: newTestSessionStore(t),
	}
	ls := &loginData{
		State:    "s",
		Nonce:    "n",
		ReturnTo: "/",
		Exp:      time.Now().Add(time.Minute).Unix(),
	}
	claims := Claims{
		OID:   "user-oid",
		UPN:   "evil@otherco.onmicrosoft.com",
		TID:   "tenant-evil",
		Nonce: "n",
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/auth/callback", nil)
	a.completeSignIn(w, r, claims, ls)

	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d (body=%q)", w.Code, w.Body.String())
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookieName && c.Value != "" {
			t.Fatalf("session cookie was set despite tenant mismatch: %+v", c)
		}
	}
}

func TestCompleteSignIn_RejectsNonceMismatch(t *testing.T) {
	a := &Authenticator{
		tenantID: "tenant-good",
		sessions: newTestSessionStore(t),
	}
	ls := &loginData{State: "s", Nonce: "expected", ReturnTo: "/", Exp: time.Now().Add(time.Minute).Unix()}
	claims := Claims{OID: "u", UPN: "user@good", TID: "tenant-good", Nonce: "wrong"}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/auth/callback", nil)
	a.completeSignIn(w, r, claims, ls)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookieName && c.Value != "" {
			t.Fatalf("session cookie set despite nonce mismatch")
		}
	}
}

func TestCompleteSignIn_HappyPath(t *testing.T) {
	a := &Authenticator{
		tenantID: "tenant-good",
		sessions: newTestSessionStore(t),
		db:       newTestDB(t),
	}
	ls := &loginData{State: "s", Nonce: "n", ReturnTo: "/dashboard", Exp: time.Now().Add(time.Minute).Unix()}
	claims := Claims{OID: "u-1", UPN: "user@good", TID: "tenant-good", Nonce: "n"}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/auth/callback", nil)
	a.completeSignIn(w, r, claims, ls)

	if w.Code != http.StatusFound {
		t.Fatalf("want 302, got %d (body=%q)", w.Code, w.Body.String())
	}
	if loc := w.Header().Get("Location"); loc != "/dashboard" {
		t.Errorf("want redirect to /dashboard, got %q", loc)
	}
	found := false
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookieName && c.Value != "" {
			found = true
		}
	}
	if !found {
		t.Errorf("session cookie not set on happy path")
	}
}

// TestCompleteSignIn_ClaimsPendingSteward proves the Slice 7 wiring: a user
// who was invited by UPN becomes an active steward the moment they sign in,
// without any extra action. The tenant is already bound, so this exercises
// the bound-mode claim path rather than bootstrap.
func TestCompleteSignIn_ClaimsPendingSteward(t *testing.T) {
	db := newTestDB(t)
	a := &Authenticator{
		tenantID: "tenant-good",
		sessions: newTestSessionStore(t),
		db:       db,
	}
	ctx := context.Background()

	// A steward invites a colleague by UPN.
	inviter := core.Actor{OID: "inviter-oid", UPN: "inviter@good"}
	if _, err := core.PromoteSteward(ctx, db, inviter, "colleague@good"); err != nil {
		t.Fatalf("seed invite: %v", err)
	}

	// The colleague signs in for the first time.
	ls := &loginData{State: "s", Nonce: "n", ReturnTo: "/", Exp: time.Now().Add(time.Minute).Unix()}
	claims := Claims{OID: "colleague-oid", UPN: "colleague@good", TID: "tenant-good", Nonce: "n"}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/auth/callback", nil)
	a.completeSignIn(w, r, claims, ls)

	if w.Code != http.StatusFound {
		t.Fatalf("want 302, got %d (body=%q)", w.Code, w.Body.String())
	}
	active, err := store.IsActiveSteward(ctx, db, "colleague-oid")
	if err != nil {
		t.Fatalf("is active: %v", err)
	}
	if !active {
		t.Error("colleague did not become a steward at sign-in")
	}
	if _, err := store.GetPendingStewardByUPN(ctx, db, "colleague@good"); err == nil {
		t.Error("pending invitation was not consumed at sign-in")
	}
}

func TestSessionStore_RoundTrip(t *testing.T) {
	s := newTestSessionStore(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)

	want := sessionData{OID: "u", UPN: "u@x", TID: "t", Exp: time.Now().Add(time.Hour).Unix()}
	if err := s.WriteSession(w, r, want); err != nil {
		t.Fatal(err)
	}

	// Replay the cookie into a fresh request.
	r2 := httptest.NewRequest("GET", "/", nil)
	for _, c := range w.Result().Cookies() {
		r2.AddCookie(c)
	}
	got, err := s.ReadSession(r2)
	if err != nil || got == nil {
		t.Fatalf("read session: err=%v got=%v", err, got)
	}
	if got.OID != want.OID || got.UPN != want.UPN || got.TID != want.TID {
		t.Errorf("round-trip mismatch: got %+v want %+v", got, want)
	}
}
