// Package config parses Xensus's runtime configuration from environment
// variables. Every value Xensus needs at runtime comes from the environment;
// there is no config file by design (one less thing to forget when running
// behind systemd or a container runtime).
package config

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
)

// Config holds the parsed environment-variable configuration. Fields with
// the OIDC prefix are read in Slice 2 but only validated as required in
// Slice 3, where the auth code starts to use them.
type Config struct {
	Listen           string
	DataDir          string
	OIDCClientID     string
	OIDCClientSecret string
	OIDCRedirectURL  string
	SessionKey       string
	TrustProxy       bool
	Access           Access
}

// Read-access surfaces that XENSUS_STEWARD_ONLY can lock to stewards. Each
// names a group of read routes — its list, detail, CSV, and any nested
// reads. Writes always require a steward regardless of this setting.
const (
	SurfacePersons  = "persons"
	SurfaceSystems  = "systems"
	SurfaceStewards = "stewards"
	SurfaceAudit    = "audit"
)

// accessSurfaces is the closed set of names XENSUS_STEWARD_ONLY accepts,
// in the order they read most naturally for logging.
var accessSurfaces = []string{SurfacePersons, SurfaceSystems, SurfaceStewards, SurfaceAudit}

// Access records which read surfaces are restricted to stewards. Its zero
// value restricts nothing: every surface is open to any signed-in user,
// which is the documented default. A surface absent from the set is open.
type Access struct {
	stewardOnly map[string]bool
}

// StewardOnly reports whether reads of the named surface require a steward.
func (a Access) StewardOnly(surface string) bool {
	return a.stewardOnly[surface]
}

// Restricted lists the restricted surfaces in declaration order, so startup
// logging can show the effective policy at a glance.
func (a Access) Restricted() []string {
	var out []string
	for _, s := range accessSurfaces {
		if a.stewardOnly[s] {
			out = append(out, s)
		}
	}
	return out
}

// parseAccess reads XENSUS_STEWARD_ONLY: a comma-separated list of read
// surfaces to lock to stewards. Unset or empty restricts nothing. An
// unrecognized name is a hard error rather than a silent skip — a typo
// like "audt" would otherwise leave audit world-readable to the tenant
// when the operator believed they had restricted it, so we refuse to start.
func parseAccess(raw string) (Access, error) {
	a := Access{stewardOnly: make(map[string]bool)}
	for _, field := range strings.Split(raw, ",") {
		name := strings.ToLower(strings.TrimSpace(field))
		if name == "" {
			continue
		}
		if !slices.Contains(accessSurfaces, name) {
			return Access{}, fmt.Errorf("XENSUS_STEWARD_ONLY: unknown surface %q (valid: %s)", name, strings.Join(accessSurfaces, ", "))
		}
		a.stewardOnly[name] = true
	}
	return a, nil
}

// FromEnv reads the XENSUS_* environment variables and returns a Config.
// It validates the values that Slice 2 needs (listen address + data
// directory); OIDC validation happens once the auth package can use it.
func FromEnv() (*Config, error) {
	c := &Config{
		Listen:           envOrDefault("XENSUS_LISTEN", ":8080"),
		DataDir:          os.Getenv("XENSUS_DATA_DIR"),
		OIDCClientID:     os.Getenv("XENSUS_OIDC_CLIENT_ID"),
		OIDCClientSecret: os.Getenv("XENSUS_OIDC_CLIENT_SECRET"),
		OIDCRedirectURL:  os.Getenv("XENSUS_OIDC_REDIRECT_URL"),
		SessionKey:       os.Getenv("XENSUS_SESSION_KEY"),
		TrustProxy:       parseBool(os.Getenv("XENSUS_TRUST_PROXY")),
	}
	if c.DataDir == "" {
		return nil, errors.New("XENSUS_DATA_DIR must be set (the directory where xensus stores its SQLite database)")
	}
	if !strings.Contains(c.Listen, ":") {
		return nil, fmt.Errorf("XENSUS_LISTEN %q is not a valid listen address; want host:port or :port", c.Listen)
	}
	access, err := parseAccess(os.Getenv("XENSUS_STEWARD_ONLY"))
	if err != nil {
		return nil, err
	}
	c.Access = access
	return c, nil
}

// RequireOIDC returns an error listing every OIDC env var that is unset.
// Slice 2 reads the values lazily; Slice 3a calls this at the moment
// the auth package actually needs them.
func (c *Config) RequireOIDC() error {
	var missing []string
	if c.OIDCClientID == "" {
		missing = append(missing, "XENSUS_OIDC_CLIENT_ID")
	}
	if c.OIDCClientSecret == "" {
		missing = append(missing, "XENSUS_OIDC_CLIENT_SECRET")
	}
	if c.OIDCRedirectURL == "" {
		missing = append(missing, "XENSUS_OIDC_REDIRECT_URL")
	}
	if len(missing) > 0 {
		return fmt.Errorf("OIDC env vars required but unset: %s", strings.Join(missing, ", "))
	}
	return nil
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func parseBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
