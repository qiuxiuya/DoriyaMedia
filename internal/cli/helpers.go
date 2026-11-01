package cli

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"doriyamedia/internal/config"
	"doriyamedia/internal/db"
	"doriyamedia/internal/models"
)

// loadConfig reads (and normalises) the config file, creating it with defaults
// when it does not exist — the same thing the server does on first start.
func (a *App) loadConfig() (*config.Config, error) {
	cfg, err := config.Load(a.ConfigPath)
	if err != nil {
		return nil, fmt.Errorf("读取配置 %s 失败: %w", a.ConfigPath, err)
	}
	return cfg, nil
}

// openStore opens the database the config points at. The caller owns closing it.
func (a *App) openStore(cfg *config.Config) (*db.Store, error) {
	if err := os.MkdirAll(cfg.Abs(), 0o755); err != nil {
		return nil, fmt.Errorf("创建数据目录 %s 失败: %w", cfg.Abs(), err)
	}
	store, err := db.Open(cfg.DBPath())
	if err != nil {
		return nil, fmt.Errorf("打开数据库 %s 失败: %w", cfg.DBPath(), err)
	}
	return store, nil
}

// withStore is the common "load config, open db, run fn" shape.
func (a *App) withStore(fn func(cfg *config.Config, store *db.Store) error) error {
	cfg, err := a.loadConfig()
	if err != nil {
		return err
	}
	store, err := a.openStore(cfg)
	if err != nil {
		return err
	}
	defer store.DB.Close()
	return fn(cfg, store)
}

// table renders rows with aligned columns. Callers pass a header row first.
func (a *App) table(headers []string, rows [][]string) {
	w := tabwriter.NewWriter(a.Out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, strings.Join(headers, "\t"))
	for _, r := range rows {
		fmt.Fprintln(w, strings.Join(r, "\t"))
	}
	_ = w.Flush()
}

// countRows runs a COUNT(*) style query and returns the number.
func countRows(db *sql.DB, query string, args ...any) (int64, error) {
	var n int64
	err := db.QueryRow(query, args...).Scan(&n)
	return n, err
}

// resolveLibrary finds one library by numeric ID or by exact name. Exactly one
// of id/name must be set; a name that matches nothing or several rows is an
// error, because a scan against the wrong library is destructive.
func resolveLibrary(store *db.Store, id int64, name string) (*models.Library, error) {
	if id > 0 {
		lib, err := store.LibraryByID(id)
		if err != nil {
			return nil, fmt.Errorf("媒体库 %d 不存在", id)
		}
		return lib, nil
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("需要 --id 或 --name 指定媒体库")
	}
	libs, err := store.Libraries()
	if err != nil {
		return nil, err
	}
	var matches []models.Library
	for _, lib := range libs {
		if strings.EqualFold(lib.Name, name) {
			matches = append(matches, lib)
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("没有名为 %q 的媒体库", name)
	case 1:
		return &matches[0], nil
	default:
		return nil, fmt.Errorf("有 %d 个同名媒体库 %q，请改用 --id", len(matches), name)
	}
}

// resolveSource finds one source by ID or exact name.
func resolveSource(store *db.Store, id int64, name string) (*models.Source, error) {
	if id > 0 {
		src, err := store.SourceByID(id)
		if err != nil {
			return nil, fmt.Errorf("存储源 %d 不存在", id)
		}
		return src, nil
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("需要 --id 或 --name 指定存储源")
	}
	srcs, err := store.Sources()
	if err != nil {
		return nil, err
	}
	var matches []models.Source
	for _, src := range srcs {
		if strings.EqualFold(src.Name, name) {
			matches = append(matches, src)
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("没有名为 %q 的存储源", name)
	case 1:
		return &matches[0], nil
	default:
		return nil, fmt.Errorf("有 %d 个同名存储源 %q，请改用 --id", len(matches), name)
	}
}

// parseID accepts a decimal id from a flag or positional argument.
func parseID(raw string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("无效的 ID: %q", raw)
	}
	return id, nil
}

// humanSize renders a byte count the way the UI does.
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// humanTime renders a timestamp as a local date-time, or "-" when unset.
func humanTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format("2006-01-02 15:04")
}

// fileSize returns the size of a file, or 0 when it does not exist.
func fileSize(path string) int64 {
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return st.Size()
}

// relToWorkingDir shortens a path for display when it sits under the cwd.
func relToWorkingDir(path string) string {
	wd, err := os.Getwd()
	if err != nil {
		return path
	}
	rel, err := filepath.Rel(wd, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return path
	}
	return rel
}
