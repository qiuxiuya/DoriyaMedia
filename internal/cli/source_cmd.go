package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"strings"
	"time"

	"doriyamedia/internal/config"
	"doriyamedia/internal/db"
	"doriyamedia/internal/models"
	"doriyamedia/internal/source"
)

// runSource manages storage sources. The flags mirror the fields of
// models.SourceConfig; only the ones relevant to the chosen type are used.
func runSource(a *App, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法: doriyamedia source <list|add|update|rm|test> ...")
	}
	switch args[0] {
	case "list", "ls":
		return sourceList(a, args[1:])
	case "add":
		return sourceAdd(a, args[1:])
	case "update", "edit":
		return sourceUpdate(a, args[1:])
	case "rm", "delete":
		return sourceRemove(a, args[1:])
	case "test":
		return sourceTest(a, args[1:])
	default:
		return fmt.Errorf("未知子命令: source %s", args[0])
	}
}

func sourceList(a *App, args []string) error {
	fs := newFlagSet(a, "source")
	jsonOut := fs.Bool("json", a.JSON, "以 JSON 输出")
	if err := fs.Parse(args); err != nil {
		return err
	}
	a.JSON = *jsonOut
	return a.withStore(func(cfg *config.Config, store *db.Store) error {
		srcs, err := store.Sources()
		if err != nil {
			return err
		}
		type row struct {
			ID      int64  `json:"id"`
			Name    string `json:"name"`
			Type    string `json:"type"`
			Summary string `json:"summary"`
		}
		rows := make([]row, 0, len(srcs))
		for _, src := range srcs {
			rows = append(rows, row{ID: src.ID, Name: src.Name, Type: src.Type, Summary: sourceSummary(src)})
		}
		return a.emit(rows, func() {
			if len(rows) == 0 {
				fmt.Fprintln(a.Out, "还没有存储源。用 doriyamedia source add 添加。")
				return
			}
			out := make([][]string, 0, len(rows))
			for _, r := range rows {
				out = append(out, []string{fmt.Sprint(r.ID), r.Name, r.Type, r.Summary})
			}
			a.table([]string{"ID", "名称", "类型", "地址/状态"}, out)
		})
	})
}

// sourceFlags is the shared parser for add and update. A pointer per field keeps
// "not passed" distinguishable from "passed empty", which is how update can
// change one field without clearing the rest.
type sourceFlags struct {
	fs   *flag.FlagSet
	name *string
	typ  *string

	root      *string
	webdavURL *string
	webdavUsr *string
	webdavPwd *string
	baseURL   *string
	token     *string
	cookie    *string
	codec     *string
	proxy     *string
}

func newSourceFlags(a *App, name string) *sourceFlags {
	fs := newFlagSet(a, name)
	return &sourceFlags{
		fs:        fs,
		name:      fs.String("name", "", "名称"),
		typ:       fs.String("type", "", "类型: local|webdav|openlist|bilibili"),
		root:      fs.String("root", "", "local: 本地根目录"),
		webdavURL: fs.String("webdav-url", "", "webdav: 地址"),
		webdavUsr: fs.String("webdav-user", "", "webdav: 用户名"),
		webdavPwd: fs.String("webdav-password", "", "webdav: 密码"),
		baseURL:   fs.String("base-url", "", "openlist: 地址"),
		token:     fs.String("token", "", "openlist: token"),
		cookie:    fs.String("bili-cookie", "", "bilibili: cookie"),
		codec:     fs.String("bili-codec", "", "bilibili: avc|hevc|av01|auto"),
		proxy:     fs.String("bili-proxy", "", "bilibili: 代理"),
	}
}

// changed reports whether the caller passed a flag, as opposed to leaving it at
// its zero value.
func (f *sourceFlags) changed(name string) bool {
	set := false
	f.fs.Visit(func(fl *flag.Flag) {
		if fl.Name == name {
			set = true
		}
	})
	return set
}

// apply merges the passed flags into base.
func (f *sourceFlags) apply(base models.SourceConfig) models.SourceConfig {
	if f.changed("root") {
		base.Root = *f.root
	}
	if f.changed("webdav-url") {
		base.WebDAVURL = *f.webdavURL
	}
	if f.changed("webdav-user") {
		base.WebDAVUser = *f.webdavUsr
	}
	if f.changed("webdav-password") {
		base.WebDAVPass = *f.webdavPwd
	}
	if f.changed("base-url") {
		base.BaseURL = *f.baseURL
	}
	if f.changed("token") {
		base.Token = *f.token
	}
	if f.changed("bili-cookie") {
		base.BiliCookie = *f.cookie
	}
	if f.changed("bili-codec") {
		base.BiliCodec = *f.codec
	}
	if f.changed("bili-proxy") {
		base.BiliProxy = *f.proxy
	}
	return base
}

func sourceAdd(a *App, args []string) error {
	f := newSourceFlags(a, "source")
	noTest := f.fs.Bool("no-test", false, "添加后不测试连接")
	if err := f.fs.Parse(args); err != nil {
		return err
	}
	if f.fs.NArg() > 0 {
		return fmt.Errorf("多余的参数: %v", f.fs.Args())
	}
	if strings.TrimSpace(*f.name) == "" {
		return fmt.Errorf("需要 --name")
	}
	typ := strings.TrimSpace(*f.typ)
	switch typ {
	case "local", "webdav", "openlist", "bilibili":
	case "":
		return fmt.Errorf("需要 --type（local|webdav|openlist|bilibili）")
	default:
		return fmt.Errorf("类型必须是 local/webdav/openlist/bilibili")
	}

	cfg := f.apply(models.SourceConfig{})
	if err := validateSourceConfig(typ, cfg); err != nil {
		return err
	}
	if !*noTest {
		// Proving the credentials before writing the row keeps a typo out of the
		// database, where the next library scan would fail minutes later.
		if _, err := testSource(cfg, typ, 30*time.Second); err != nil {
			return fmt.Errorf("连接测试失败: %w（确认无误可加 --no-test 直接保存）", err)
		}
	}

	return a.withStore(func(conf *config.Config, store *db.Store) error {
		raw, err := json.Marshal(cfg)
		if err != nil {
			return err
		}
		src := &models.Source{Name: strings.TrimSpace(*f.name), Type: typ, Config: string(raw)}
		if err := store.CreateSource(src); err != nil {
			return err
		}
		if a.JSON {
			return a.emit(src, nil)
		}
		fmt.Fprintf(a.Out, "已添加存储源 %s（ID %d，类型 %s）\n", src.Name, src.ID, src.Type)
		return nil
	})
}

func sourceUpdate(a *App, args []string) error {
	f := newSourceFlags(a, "source")
	id := f.fs.Int64("id", 0, "存储源 ID")
	name := f.fs.String("target", "", "存储源名称（代替 --id）")
	if err := f.fs.Parse(args); err != nil {
		return err
	}
	return a.withStore(func(conf *config.Config, store *db.Store) error {
		src, err := resolveSource(store, *id, *name)
		if err != nil {
			return err
		}
		var cfg models.SourceConfig
		if err := json.Unmarshal([]byte(src.Config), &cfg); err != nil {
			return fmt.Errorf("存储源配置损坏: %w", err)
		}
		cfg = f.apply(cfg)
		if err := validateSourceConfig(src.Type, cfg); err != nil {
			return err
		}
		if f.changed("name") && strings.TrimSpace(*f.name) != "" {
			src.Name = strings.TrimSpace(*f.name)
		}
		raw, err := json.Marshal(cfg)
		if err != nil {
			return err
		}
		src.Config = string(raw)
		if err := store.UpdateSource(src); err != nil {
			return err
		}
		if a.JSON {
			return a.emit(src, nil)
		}
		fmt.Fprintf(a.Out, "已更新存储源 %s（ID %d）\n", src.Name, src.ID)
		return nil
	})
}

func sourceRemove(a *App, args []string) error {
	fs := newFlagSet(a, "source")
	id := fs.Int64("id", 0, "存储源 ID")
	name := fs.String("name", "", "存储源名称（代替 --id）")
	yes := fs.Bool("yes", false, "跳过确认")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return a.withStore(func(conf *config.Config, store *db.Store) error {
		src, err := resolveSource(store, *id, *name)
		if err != nil {
			return err
		}
		libs, _ := store.Libraries()
		var affected []string
		for _, lib := range libs {
			for _, ls := range lib.Sources {
				if ls.SourceID == src.ID {
					affected = append(affected, lib.Name)
					break
				}
			}
		}
		if len(affected) > 0 && !*yes {
			return fmt.Errorf("存储源 %q 被媒体库使用：%s；确认删除请加 --yes（只扫描该源的媒体库会一并删除）",
				src.Name, strings.Join(affected, "、"))
		}
		if err := store.DeleteSource(src.ID); err != nil {
			return err
		}
		if a.JSON {
			return a.emit(map[string]any{"ok": true, "deleted": src.ID, "libraries": affected}, nil)
		}
		fmt.Fprintf(a.Out, "已删除存储源 %s（ID %d）\n", src.Name, src.ID)
		if len(affected) > 0 {
			fmt.Fprintf(a.Out, "一并删除的媒体库: %s\n", strings.Join(affected, "、"))
		}
		return nil
	})
}

func sourceTest(a *App, args []string) error {
	fs := newFlagSet(a, "source")
	id := fs.Int64("id", 0, "存储源 ID")
	name := fs.String("name", "", "存储源名称（代替 --id）")
	jsonOut := fs.Bool("json", a.JSON, "以 JSON 输出")
	timeout := fs.Duration("timeout", 30*time.Second, "超时时间")
	if err := fs.Parse(args); err != nil {
		return err
	}
	a.JSON = *jsonOut
	return a.withStore(func(conf *config.Config, store *db.Store) error {
		src, err := resolveSource(store, *id, *name)
		if err != nil {
			return err
		}
		var cfg models.SourceConfig
		if err := json.Unmarshal([]byte(src.Config), &cfg); err != nil {
			return fmt.Errorf("存储源配置损坏: %w", err)
		}
		start := time.Now()
		entries, err := testSource(cfg, src.Type, *timeout)
		elapsed := time.Since(start)
		if err != nil {
			if a.JSON {
				return a.emit(map[string]any{"ok": false, "error": err.Error()}, nil)
			}
			return fmt.Errorf("连接失败（%s）: %w", elapsed.Round(time.Millisecond), err)
		}
		if a.JSON {
			return a.emit(map[string]any{
				"ok": true, "count": len(entries),
				"elapsed_ms": elapsed.Milliseconds(),
			}, nil)
		}
		fmt.Fprintf(a.Out, "%s 连接正常：根目录 %d 项，用时 %s\n",
			src.Name, len(entries), elapsed.Round(time.Millisecond))
		for i, e := range entries {
			if i >= 10 {
				fmt.Fprintf(a.Out, "  … 还有 %d 项\n", len(entries)-i)
				break
			}
			if e.IsDir {
				fmt.Fprintf(a.Out, "  [目录] %s\n", e.Name)
			} else {
				fmt.Fprintf(a.Out, "  %s (%s)\n", e.Name, humanSize(e.Size))
			}
		}
		return nil
	})
}

// validateSourceConfig refuses a source whose required field for its type is
// empty, so the row cannot exist in a state that can never connect.
func validateSourceConfig(typ string, cfg models.SourceConfig) error {
	switch typ {
	case "local":
		if strings.TrimSpace(cfg.Root) == "" {
			return fmt.Errorf("local 类型需要 --root")
		}
	case "webdav":
		if strings.TrimSpace(cfg.WebDAVURL) == "" {
			return fmt.Errorf("webdav 类型需要 --webdav-url")
		}
	case "openlist":
		if strings.TrimSpace(cfg.BaseURL) == "" {
			return fmt.Errorf("openlist 类型需要 --base-url")
		}
	case "bilibili":
		if strings.TrimSpace(cfg.BiliCookie) == "" {
			return fmt.Errorf("bilibili 类型需要 --bili-cookie（或在网页里扫码登录后复制配置）")
		}
	}
	return nil
}

// sourceSummary is the one-line description shown in `source list`.
func sourceSummary(src models.Source) string {
	var cfg models.SourceConfig
	_ = json.Unmarshal([]byte(src.Config), &cfg)
	switch src.Type {
	case "local":
		return cfg.Root
	case "webdav":
		return cfg.WebDAVURL
	case "openlist":
		return cfg.BaseURL
	case "bilibili":
		if strings.TrimSpace(cfg.BiliCookie) == "" {
			return "(未登录)"
		}
		if codec := strings.TrimSpace(cfg.BiliCodec); codec != "" && codec != "auto" {
			return "(已登录，" + codec + ")"
		}
		return "(已登录)"
	default:
		return ""
	}
}

// testSource lists the source's root to prove the credentials work.
func testSource(cfg models.SourceConfig, typ string, timeout time.Duration) ([]source.Entry, error) {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	drv, err := source.NewDriver(&models.Source{Type: typ, Config: string(raw)})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return drv.List(ctx, "/")
}
