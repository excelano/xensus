// Xensus — identity registry for Microsoft 365 tenants.
//
// main.go is just the entrypoint: flag handling, slog setup, and the
// handoff to Run in server.go. Everything else lives in subpackages
// (config, store, core, auth, api, web) per the plan at
// /home/anderix/.claude/plans/lucky-crunching-dawn.md.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/excelano/xensus/config"
)

// version is stamped by goreleaser at build time via -X main.version=<tag>.
// Local `go build` leaves it as the default below.
var version = "(devel)"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--version", "-v", "version":
			fmt.Println("xensus " + version)
			return
		case "--help", "-h", "help":
			printHelp()
			return
		}
	}

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))
	slog.Info("xensus starting", "version", version)

	cfg, err := config.FromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, "xensus: "+err.Error())
		os.Exit(2)
	}

	if err := Run(context.Background(), cfg); err != nil {
		slog.Error("xensus exiting with error", "err", err)
		os.Exit(1)
	}
}

func printHelp() {
	fmt.Println("xensus — identity registry for Microsoft 365")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  xensus              start the HTTP server")
	fmt.Println("  xensus --version    print the version and exit")
	fmt.Println("  xensus --help       print this help and exit")
	fmt.Println()
	fmt.Println("Environment variables:")
	fmt.Println("  XENSUS_LISTEN              listen address (default :8080)")
	fmt.Println("  XENSUS_DATA_DIR            directory for the SQLite database (required)")
	fmt.Println("  XENSUS_OIDC_CLIENT_ID      Microsoft Entra app client ID")
	fmt.Println("  XENSUS_OIDC_CLIENT_SECRET  Microsoft Entra app client secret")
	fmt.Println("  XENSUS_OIDC_REDIRECT_URL   OIDC callback URL (e.g. https://xensus.example.com/auth/callback)")
	fmt.Println("  XENSUS_SESSION_KEY         base64 32-byte key for encrypted session cookies")
	fmt.Println("  XENSUS_TRUST_PROXY         set to true behind a TLS-terminating proxy")
	fmt.Println()
	fmt.Println("See https://github.com/excelano/xensus for setup details.")
}
