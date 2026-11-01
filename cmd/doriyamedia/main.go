package main

import (
	"os"

	"doriyamedia/internal/cli"
)

// version 由构建时注入：
//
//	go build -ldflags "-X main.version=v1.2.3" ./cmd/doriyamedia
var version = "dev"

// main hands off to the CLI. A bare `doriyamedia` (no arguments, or only the
// old -config/-port flags) still starts the HTTP server exactly as before; the
// subcommands are an addition, not a replacement.
func main() {
	os.Exit(cli.Main(os.Args[1:], version))
}
