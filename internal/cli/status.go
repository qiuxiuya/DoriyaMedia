package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"doriyamedia/internal/config"
	"doriyamedia/internal/db"
	"doriyamedia/internal/netproxy"
)

// statusReport is what `status --json` prints.
type statusReport struct {
	Version      string   `json:"version"`
	ConfigPath   string   `json:"config_path"`
	Port         int      `json:"port"`
	Listen       string   `json:"listen"`
	DataDir      string   `json:"data_dir"`
	DBPath       string   `json:"db_path"`
	DBSize       int64    `json:"db_size"`
	Sources      int      `json:"sources"`
	Libraries    int      `json:"libraries"`
	Items        int      `json:"items"`
	Files        int      `json:"files"`
	Users        int      `json:"users"`
	Admins       int      `json:"admins"`
	FFmpeg       string   `json:"ffmpeg"`
	FFProbe      string   `json:"ffprobe"`
	FFmpegOK     bool     `json:"ffmpeg_ok"`
	FFProbeOK    bool     `json:"ffprobe_ok"`
	TMDBKey      bool     `json:"tmdb_key"`
	BangumiToken bool     `json:"bangumi_token"`
	MailEnabled  bool     `json:"mail_enabled"`
	EmbyEnabled  bool     `json:"emby_enabled"`
	Excludes     []string `json:"exclude_keywords,omitempty"`
}

func runStatus(a *App, args []string) error {
	fs := newFlagSet(a, "status")
	jsonOut := fs.Bool("json", a.JSON, "以 JSON 输出")
	if err := fs.Parse(args); err != nil {
		return err
	}
	a.JSON = *jsonOut

	cfg, err := a.loadConfig()
	if err != nil {
		return err
	}
	store, err := a.openStore(cfg)
	if err != nil {
		return err
	}
	defer store.DB.Close()

	// The listen address is not in the file (see config.Config.Listen), so the
	// report has to take it from where the server would: the environment first,
	// then the default.
	if v := strings.TrimSpace(os.Getenv("DORIYAMEDIA_LISTEN")); v != "" {
		if host, err := config.NormalizeListen(v); err == nil {
			cfg.Listen = host
		}
	}
	if os.Getenv("DORIYAMEDIA_LOCAL") == "1" {
		cfg.Listen = "127.0.0.1"
	}
	rep := statusReport{
		Version:      a.Version,
		ConfigPath:   a.ConfigPath,
		Port:         cfg.Port,
		Listen:       cfg.Listen,
		DataDir:      cfg.Abs(),
		DBPath:       cfg.DBPath(),
		DBSize:       fileSize(cfg.DBPath()),
		FFmpeg:       cfg.FFmpeg,
		FFProbe:      cfg.FFProbe,
		FFmpegOK:     binaryExists(cfg.FFmpeg),
		FFProbeOK:    binaryExists(cfg.FFProbe),
		TMDBKey:      strings.TrimSpace(cfg.TMDB.APIKey) != "",
		BangumiToken: strings.TrimSpace(cfg.BGM.Token) != "",
		MailEnabled:  cfg.MailEnabled(),
		EmbyEnabled:  cfg.Emby.Enabled,
		Excludes:     cfg.ExcludeKeywords,
	}
	rep.Sources, _ = intCount(store, `SELECT COUNT(*) FROM sources`)
	rep.Libraries, _ = intCount(store, `SELECT COUNT(*) FROM libraries WHERE name NOT LIKE '__browse__%'`)
	rep.Items, _ = intCount(store, `SELECT COUNT(*) FROM items`)
	rep.Files, _ = intCount(store, `SELECT COUNT(*) FROM files`)
	rep.Users, _ = intCount(store, `SELECT COUNT(*) FROM users`)
	rep.Admins, _ = intCount(store, `SELECT COUNT(*) FROM users WHERE role='admin' AND status='active'`)

	return a.emit(rep, func() {
		fmt.Fprintf(a.Out, "DoriyaMedia %s\n\n", rep.Version)
		fmt.Fprintf(a.Out, "  配置       %s\n", rep.ConfigPath)
		fmt.Fprintf(a.Out, "  数据目录   %s\n", rep.DataDir)
		fmt.Fprintf(a.Out, "  数据库     %s (%s)\n", rep.DBPath, humanSize(rep.DBSize))
		fmt.Fprintf(a.Out, "  监听地址   %s\n", cfg.ListenAddr())
		fmt.Fprintf(a.Out, "\n  存储源 %d    媒体库 %d    条目 %d    文件 %d\n",
			rep.Sources, rep.Libraries, rep.Items, rep.Files)
		fmt.Fprintf(a.Out, "  账号 %d（可用管理员 %d）\n", rep.Users, rep.Admins)
		fmt.Fprintf(a.Out, "\n  ffmpeg     %s %s\n", rep.FFmpeg, markOK(rep.FFmpegOK))
		fmt.Fprintf(a.Out, "  ffprobe    %s %s\n", rep.FFProbe, markOK(rep.FFProbeOK))
		fmt.Fprintf(a.Out, "  TMDB key   %s\n", markOK(rep.TMDBKey))
		fmt.Fprintf(a.Out, "  Bangumi    %s\n", markOK(rep.BangumiToken))
		fmt.Fprintf(a.Out, "  邮件       %s\n", markOK(rep.MailEnabled))
		fmt.Fprintf(a.Out, "  Emby       %s\n", markOK(rep.EmbyEnabled))
	})
}

// doctorCheck is one line of `doctor` output.
type doctorCheck struct {
	Name string `json:"name"`
	OK   bool   `json:"ok"`
	Info string `json:"info,omitempty"`
	Hint string `json:"hint,omitempty"`
}

func runDoctor(a *App, args []string) error {
	fs := newFlagSet(a, "doctor")
	jsonOut := fs.Bool("json", a.JSON, "以 JSON 输出")
	proxy := fs.String("proxy", "", "额外检查一个代理（socks5:// 或 http://）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	a.JSON = *jsonOut

	cfg, err := a.loadConfig()
	if err != nil {
		return err
	}
	var checks []doctorCheck
	add := func(name string, ok bool, info, hint string) {
		checks = append(checks, doctorCheck{Name: name, OK: ok, Info: info, Hint: hint})
	}

	add("配置文件", true, a.ConfigPath, "")
	if info, ok := dirWritable(cfg.Abs()); ok {
		add("数据目录", true, info, "")
	} else {
		add("数据目录", false, info, "确认目录存在且当前用户可写")
	}
	if info, ok := binaryVersion(cfg.FFmpeg); ok {
		add("ffmpeg", true, info, "")
	} else {
		add("ffmpeg", false, cfg.FFmpeg, "安装 ffmpeg，或在 config.json 里把 ffmpeg 指向完整路径")
	}
	if info, ok := binaryVersion(cfg.FFProbe); ok {
		add("ffprobe", true, info, "")
	} else {
		add("ffprobe", false, cfg.FFProbe, "安装 ffmpeg（含 ffprobe），或修正 config.json 的 ffprobe 路径")
	}

	store, derr := a.openStore(cfg)
	if derr != nil {
		add("数据库", false, derr.Error(), "检查数据目录权限与磁盘空间")
	} else {
		defer store.DB.Close()
		n, err := countRows(store.DB, `SELECT COUNT(*) FROM sqlite_master WHERE type='table'`)
		if err != nil {
			add("数据库", false, err.Error(), "")
		} else {
			add("数据库", true, fmt.Sprintf("%s（%d 张表，%s）", cfg.DBPath(), n, humanSize(fileSize(cfg.DBPath()))), "")
		}
		if admins, err := countRows(store.DB, `SELECT COUNT(*) FROM users WHERE role='admin' AND status='active'`); err == nil {
			if admins == 0 {
				add("管理员账号", false, "没有可用管理员", "打开网页完成初始化，或用 doriyamedia user add --admin")
			} else {
				add("管理员账号", true, fmt.Sprintf("%d 个", admins), "")
			}
		}
	}

	// A missing TMDB key is not a failure: the README says so explicitly, and a
	// library without posters still plays. It is reported as a note instead of
	// a red cross, so `doctor`'s exit code only reflects real breakage.
	optionalNotes := []string{}
	if strings.TrimSpace(cfg.TMDB.APIKey) == "" {
		optionalNotes = append(optionalNotes, "TMDB 未配置 API key：仍可播放，但没有海报和简介")
	}

	if *proxy != "" {
		spec, perr := netproxy.Parse(*proxy)
		if perr != nil {
			add("代理", false, perr.Error(), "格式应为 socks5://host:port 或 http://host:port")
		} else {
			c := netproxy.CheckProxy(spec, 20*time.Second)
			add("代理", c.OK, c.Message, "")
		}
	}

	okAll := true
	for _, c := range checks {
		if !c.OK {
			okAll = false
		}
	}
	if err := a.emit(map[string]any{"ok": okAll, "checks": checks, "notes": optionalNotes}, func() {
		for _, c := range checks {
			fmt.Fprintf(a.Out, "  %s %-10s %s\n", markOK(c.OK), c.Name, c.Info)
			if !c.OK && c.Hint != "" {
				fmt.Fprintf(a.Out, "     └ 建议: %s\n", c.Hint)
			}
		}
		for _, note := range optionalNotes {
			fmt.Fprintf(a.Out, "  · %s\n", note)
		}
		if !okAll {
			fmt.Fprintln(a.Out, "\n存在未通过项。")
		} else {
			fmt.Fprintln(a.Out, "\n全部通过。")
		}
	}); err != nil {
		return err
	}
	if !okAll {
		// A failed check must be visible to a script, not only to a person
		// reading the output. In JSON mode the report on stdout already carries
		// ok:false, so the exit code is set without a second message that would
		// interleave with the JSON.
		if a.JSON {
			return errSilentFailure
		}
		return fmt.Errorf("自检未通过")
	}
	return nil
}

// errSilentFailure asks Main to exit non-zero without printing anything: the
// command already reported the failure in its own output.
var errSilentFailure = errors.New("")

func markOK(ok bool) string {
	if ok {
		return "✔"
	}
	return "✘"
}

// intCount runs a COUNT query and narrows it to int.
func intCount(store *db.Store, query string) (int, error) {
	n, err := countRows(store.DB, query)
	return int(n), err
}

// binaryExists reports whether a command or path can be executed.
func binaryExists(path string) bool {
	if strings.ContainsRune(path, os.PathSeparator) {
		st, err := os.Stat(path)
		return err == nil && !st.IsDir()
	}
	_, err := exec.LookPath(path)
	return err == nil
}

// binaryVersion returns the first line of `-version` output, which is enough to
// tell a working install from a broken one and to report which build is used.
func binaryVersion(path string) (string, bool) {
	if !binaryExists(path) {
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "-version").Output()
	if err != nil {
		return "", false
	}
	line := strings.SplitN(string(out), "\n", 2)[0]
	return strings.TrimSpace(line), true
}

// dirWritable checks that a directory exists (creating it) and is writable.
func dirWritable(dir string) (string, bool) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err.Error(), false
	}
	f, err := os.CreateTemp(dir, ".write-test-*")
	if err != nil {
		return err.Error(), false
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return dir, true
}
