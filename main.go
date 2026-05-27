// Xensus — identity registry for Microsoft 365 tenants.
//
// Slice 1: prints --version and exits. The server, storage, and auth layers
// are added in subsequent slices per /home/anderix/.claude/plans/.
package main

import (
	"fmt"
	"os"
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
	fmt.Fprintln(os.Stderr, "xensus: server not yet implemented; run with --version or --help")
	os.Exit(1)
}

func printHelp() {
	fmt.Println("xensus — identity registry for Microsoft 365")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  xensus [--version | --help]")
	fmt.Println()
	fmt.Println("The server, storage, and auth layers are not yet implemented.")
	fmt.Println("See https://github.com/excelano/xensus for development status.")
}
