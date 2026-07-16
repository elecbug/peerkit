package main

import (
	"os"

	"github.com/k-p2plab/peerkit/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
