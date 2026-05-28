// Package config parses Xensus's runtime configuration from environment
// variables. Every value Xensus needs at runtime comes from the environment;
// there is no config file by design (one less thing to forget when running
// behind systemd or a container runtime).
package config

import (
	"errors"
	"fmt"
	"os"
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
	return c, nil
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
