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
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *port <= 0 {
		// A global -port was handed over through the environment.
		if v, err := strconv.Atoi(os.Getenv("DORIYAMEDIA_PORT")); err == nil {
			*port = v
		}
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("多余的参数: %v", fs.Args())
	}
	return app.Serve(a.ConfigPath, a.Version, *port)
}
