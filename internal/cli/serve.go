package cli

import (
	"fmt"
	"os"
	"strconv"

	"doriyamedia/internal/app"
)

// runServe starts the HTTP server. It never returns except on failure, which is
// the same contract the old main() had.
func runServe(a *App, args []string) error {
	fs := newFlagSet(a, "serve")
	port := fs.Int("port", 0, "覆盖监听端口")
	// listen is the host, not the address: the port already has its own flag,
	// and accepting "1.2.3.4:8080" here would give two ways to set one thing.
	// It reaches the app through the environment so a global -listen (written
	// before the subcommand) lands in the same place as a subcommand one.
	listen := fs.String("listen", "", "监听地址：0.0.0.0（默认，所有网卡）或 127.0.0.1 等")
	host := fs.String("host", "", "-listen 的别名")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *port <= 0 {
		// A global -port was handed over through the environment.
		if v, err := strconv.Atoi(os.Getenv("DORIYAMEDIA_PORT")); err == nil {
			*port = v
		}
	}
	if *listen == "" {
		*listen = *host
	}
	if *listen == "" {
		// Same hand-off for a global -listen.
		if v := os.Getenv("DORIYAMEDIA_LISTEN"); v != "" {
			*listen = v
		}
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("多余的参数: %v", fs.Args())
	}
	return app.Serve(a.ConfigPath, a.Version, *listen, *port)
}
