// Package cli implements the `doriyamedia` command line interface: a set of
// subcommands that operate on the same config file and database the server
// uses, so a headless deployment can be administered over SSH without opening
// the web UI.
//
// Every command that reports data supports --json, so a script can consume it.
// Mutating commands are the same operations the settings page performs; they
// exist for the case where the server is not running, or where a browser is not
// available.
package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

// Version is stamped by cmd/doriyamedia from the linker.
type Version string

// App holds what every subcommand needs: where the config lives, where output
// goes, and whether the caller asked for JSON.
type App struct {
	ConfigPath string
	Version    string
	Out        io.Writer
	Err        io.Writer
	JSON       bool
	// Quiet suppresses the human-readable progress chatter of long commands.
	Quiet bool
}

// command is one subcommand.
type command struct {
	name    string
	summary string
	usage   string
	// run gets the arguments after the command name.
	run func(a *App, args []string) error
}

// commands is the dispatch table. Order is the order shown in help. It is filled
// in init because help itself reads the table, and a package-level literal that
// mentions runHelp would be an initialisation cycle.
var commands []command

func init() {
	commands = []command{
		{"serve", "启动 HTTP 服务（默认命令）", "serve [-port N]", runServe},
		{"version", "打印版本", "version", runVersion},
		{"status", "概览：配置、数据库、媒体库与账号数量", "status [--json]", runStatus},
		{"doctor", "自检：ffmpeg/ffprobe、数据目录、数据库、代理", "doctor [--proxy URL] [--json]", runDoctor},
		{"config", "查看或修改 config.json", "config <show|get|set|path> ...", runConfig},
		{"source", "存储源管理", "source <list|add|update|rm|test> ...", runSource},
		{"library", "媒体库管理", "library <list|add|update|rm|scan> ...", runLibrary},
		{"scan", "扫描媒体库并等待结果", "scan [--id N|--name X] [--path /dir] [--json]", runScan},
		{"item", "条目浏览", "item <list|show> ...", runItem},
		{"user", "账号管理", "user <list|add|passwd|role|status|rm> ...", runUser},
		{"help", "显示帮助", "help [command]", runHelp},
	}
}

func findCommand(name string) (command, bool) {
	for _, c := range commands {
		if c.name == name {
			return c, true
		}
	}
	return command{}, false
}

// Main is the CLI entry point. It returns the process exit code. A bare
// invocation with no subcommand keeps the historical behaviour: start the
// server. Flags that used to be global (-config, -port, -version) still work in
// that position.
func Main(args []string, version string) int {
	app := &App{
		ConfigPath: envOr("DORIYAMEDIA_CONFIG", "config.json"),
		Version:    version,
		Out:        os.Stdout,
		Err:        os.Stderr,
	}

	// Global flags may appear before the subcommand. Parse only up to the first
	// non-flag argument so `-config x status` and `status -config x` both work.
	rest, err := parseGlobal(app, args)
	if err != nil {
		fmt.Fprintln(app.Err, "错误:", err)
		return 2
	}
	if len(rest) == 0 {
		// No subcommand: behave exactly like the old binary.
		if err := runServe(app, nil); err != nil {
			fmt.Fprintln(app.Err, "错误:", err)
			return 1
		}
		return 0
	}

	name := rest[0]
	cmd, ok := findCommand(name)
	if !ok {
		// Backwards compatibility: `doriyamedia -version` used to print the
		// version, and a stray positional argument used to be ignored.
		switch name {
		case "-version", "--version", "-v":
			fmt.Fprintln(app.Out, "doriyamedia", version)
			return 0
		}
		fmt.Fprintf(app.Err, "未知命令: %s\n\n", name)
		printHelp(app, app.Err, "")
		return 2
	}
	if err := cmd.run(app, rest[1:]); err != nil {
		// errSilentFailure means the command already reported the problem in
		// its own output (a JSON report with ok:false); only the exit code is
		// left to set. flag.ErrHelp is -h, which is not an error.
		if err != flag.ErrHelp && err != errSilentFailure {
			fmt.Fprintln(app.Err, "错误:", err)
		}
		return 1
	}
	return 0
}

// parseGlobal consumes leading global flags and returns the remaining args.
func parseGlobal(app *App, args []string) ([]string, error) {
	fs := flag.NewFlagSet("doriyamedia", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&app.ConfigPath, "config", app.ConfigPath, "配置文件路径")
	fs.BoolVar(&app.JSON, "json", app.JSON, "以 JSON 输出")
	fs.BoolVar(&app.Quiet, "quiet", app.Quiet, "减少输出")
	port := fs.Int("port", 0, "覆盖监听端口")
	showVersion := fs.Bool("version", false, "打印版本")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if *showVersion {
		fmt.Fprintln(app.Out, "doriyamedia", app.Version)
		os.Exit(0)
	}
	// A port override only matters to serve; hand it on through the environment
	// so `doriyamedia -port 8080` keeps working without a second parser.
	if *port > 0 {
		os.Setenv("DORIYAMEDIA_PORT", fmt.Sprint(*port))
	}
	return fs.Args(), nil
}

// newFlagSet builds a subcommand flag set that reports errors to the app. It
// also registers -config and -quiet, so `doriyamedia status -config x` works as
// well as `doriyamedia -config x status`: people type the flag where it reads
// best, and refusing one of the two positions is a needless trap.
func newFlagSet(app *App, name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(app.Err)
	fs.StringVar(&app.ConfigPath, "config", app.ConfigPath, "配置文件路径")
	fs.BoolVar(&app.Quiet, "quiet", app.Quiet, "减少输出")
	fs.Usage = func() {
		if c, ok := findCommand(name); ok {
			fmt.Fprintf(app.Err, "用法: doriyamedia %s\n\n", c.usage)
			fmt.Fprintf(app.Err, "%s\n", c.summary)
		}
	}
	return fs
}

// emit prints either JSON or a human rendering. fn is only called when JSON was
// not requested, so it may assume it can format freely. A nil fn means the
// command has nothing to add in human mode (the JSON is the whole output).
func (a *App) emit(v any, fn func()) error {
	if a.JSON || fn == nil {
		if !a.JSON {
			return nil
		}
		enc := json.NewEncoder(a.Out)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
	fn()
	return nil
}

func runVersion(a *App, args []string) error {
	if a.JSON {
		return a.emit(map[string]string{"version": a.Version}, nil)
	}
	fmt.Fprintln(a.Out, "doriyamedia", a.Version)
	return nil
}

func runHelp(a *App, args []string) error {
	topic := ""
	if len(args) > 0 {
		topic = args[0]
	}
	printHelp(a, a.Out, topic)
	return nil
}

func printHelp(a *App, w io.Writer, topic string) {
	if topic != "" {
		c, ok := findCommand(topic)
		if !ok {
			fmt.Fprintf(w, "未知命令: %s\n", topic)
			return
		}
		fmt.Fprintf(w, "用法: doriyamedia %s\n\n%s\n", c.usage, c.summary)
		return
	}
	fmt.Fprintf(w, "DoriyaMedia %s\n\n", a.Version)
	fmt.Fprintf(w, "用法: doriyamedia [全局选项] <命令> [参数]\n\n")
	fmt.Fprintf(w, "不带命令时等同于 `doriyamedia serve`，启动 HTTP 服务。\n\n")
	fmt.Fprintf(w, "全局选项:\n")
	fmt.Fprintf(w, "  -config PATH   配置文件路径（默认 config.json，或环境变量 DORIYAMEDIA_CONFIG）\n")
	fmt.Fprintf(w, "  -json          以 JSON 输出\n")
	fmt.Fprintf(w, "  -quiet         减少输出\n")
	fmt.Fprintf(w, "  -port N        覆盖监听端口（仅 serve）\n")
	fmt.Fprintf(w, "  -version       打印版本\n\n")
	fmt.Fprintf(w, "命令:\n")
	for _, c := range commands {
		fmt.Fprintf(w, "  %-9s %s\n", c.name, c.summary)
	}
	fmt.Fprintf(w, "\n每个命令都支持 -h 查看详细用法。\n")
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}
