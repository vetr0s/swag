package main

import (
	"os"

	"github.com/vetr0s/swag/internal/cli"
)

// version is set for releases with -ldflags "-X main.version=...".
var version string

func main() {
	os.Exit(cli.Run(version, os.Args[1:], os.Stdout, os.Stderr))
}
