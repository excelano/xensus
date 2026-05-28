package auth

import (
	"crypto/rand"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

// Login starts the OIDC code flow: it generates state + nonce, stores
// them in the short-lived login-state cookie, and redirects the browser
// to the Microsoft sign-in page. The optional ?return_to= query is
// preserved through the round-trip so the user lands where they tried
// to go.
func (a *Authenticator) Login(w http.ResponseWriter, r *http.Request) {
	state, err := randomToken()
	if err != nil {
		slog.Error("login: generate state", "err", err)
		http.Error(w, "login init failed", http.StatusInternalServerError)
		return
	}
	nonce, err := randomToken()
	if err != nil {
		slog.Error("login: generate nonce", "err", err)
		http.Error(w, "login init failed", http.StatusInternalServerError)
		return
	}
	returnTo := r.URL.Query().Get("return_to")
	if returnTo == "" || !strings.HasPrefix(returnTo, "/") || strings.HasPrefix(returnTo, "//") {
		returnTo = "/"
	}

	if err := a.sessions.WriteLoginState(w, r, loginData{
		State:    state,
		Nonce:    nonce,
		ReturnTo: returnTo,
		Exp:      time.Now().Add(loginMaxAge).Unix(),
	}); err != nil {
		slog.Error("login: write state cookie", "err", err)
		http.Error(w, "login init failed", http.StatusInternalServerError)
		return
	}

	url := a.oauth2.AuthCodeURL(state, oidc.Nonce(nonce))
	http.Redirect(w, r, url, http.StatusFound)
}

// Callback handles the OIDC redirect back from Microsoft. State and
// nonce are verified before code exchange; the ID token is verified
// against the discovered signing keys; the tid claim is matched against
// the bound tenant BEFORE any session cookie is written. The session
// itself is set by completeSignIn so that downstream logic is unit-
// testable without going through the network.
func (a *Authenticator) Callback(w http.ResponseWriter, r *http.Request) {
	ls, err := a.sessions.ReadLoginState(r)
	if err != nil || ls == nil {
		http.Error(w, "missing or invalid login state — start over at /auth/login", http.StatusBadRequest)
		return
	}
	a.sessions.ClearLoginState(w, r)

	if r.URL.Query().Get("state") != ls.State {
		http.Error(w, "state mismatch", http.StatusBadRequest)
		return
	}
	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		desc := r.URL.Query().Get("error_description")
		slog.Warn("oidc error from provider", "error", errMsg, "description", desc)
		http.Error(w, "sign-in failed: "+errMsg, http.StatusBadRequest)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}

	tok, err := a.oauth2.Exchange(r.Context(), code)
	if err != nil {
		slog.Warn("oauth2 code exchange failed", "err", err)
		http.Error(w, "code exchange failed", http.StatusBadRequest)
		return
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok || rawID == "" {
		http.Error(w, "no id_token in token response", http.StatusBadRequest)
		return
	}
	claims, err := a.verifier.Verify(r.Context(), rawID)
	if err != nil {
		slog.Warn("id token verification failed", "err", err)
		http.Error(w, "id token verification failed", http.StatusUnauthorized)
		return
	}

	a.completeSignIn(w, r, claims, ls)
}

// completeSignIn applies the invariant checks that don't require the
// network and, on success, writes the session and redirects. Split out
// so that the tenant-mismatch path is unit-testable without spinning
// up an OIDC provider or oauth2 token endpoint.
func (a *Authenticator) completeSignIn(w http.ResponseWriter, r *http.Request, c Claims, ls *loginData) {
	if c.Nonce != ls.Nonce {
		slog.Warn("nonce mismatch", "upn", c.UPN)
		http.Error(w, "nonce mismatch", http.StatusUnauthorized)
		return
	}
	if c.TID != a.tenantID {
		slog.Warn("tenant mismatch — refusing sign-in",
			"got_tid", c.TID, "want_tid", a.tenantID, "upn", c.UPN)
		http.Error(w, "this Xensus deployment is bound to a different tenant", http.StatusForbidden)
		return
	}

	if err := a.sessions.WriteSession(w, r, sessionData{
		OID: c.OID,
		UPN: c.UPN,
		TID: c.TID,
		Exp: time.Now().Add(sessionMaxAge).Unix(),
	}); err != nil {
		slog.Error("session write failed", "err", err)
		http.Error(w, "session write failed", http.StatusInternalServerError)
		return
	}
	slog.Info("sign-in", "upn", c.UPN, "oid", c.OID)
	http.Redirect(w, r, ls.ReturnTo, http.StatusFound)
}

// Logout clears the session cookie and redirects to /. It is POST-only
// to avoid trivial CSRF-driven logouts (an attacker linking to a GET
// could log a user out from across the web).
func (a *Authenticator) Logout(w http.ResponseWriter, r *http.Request) {
	a.sessions.ClearSession(w, r)
	http.Redirect(w, r, "/", http.StatusFound)
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
