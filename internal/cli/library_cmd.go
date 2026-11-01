package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"strings"

	"doriyamedia/internal/config"
	"doriyamedia/internal/db"
	"doriyamedia/internal/models"
	"doriyamedia/internal/scan"
)

// runLibrary manages media libraries.
func runLibrary(a *App, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法: doriyamedia library <list|add|update|rm|scan> ...")
	}
	switch args[0] {
	case "list", "ls":
		return libraryList(a, args[1:])
	case "add":
		return libraryAdd(a, args[1:])
	case "update", "edit":
		return libraryUpdate(a, args[1:])
	case "rm", "delete":
		return libraryRemove(a, args[1:])
	case "scan":
		return libraryScan(a, args[1:])
	default:
		return fmt.Errorf("未知子命令: library %s", args[0])
	}
}

func libraryList(a *App, args []string) error {
	fs := newFlagSet(a, "library")
	jsonOut := fs.Bool("json", a.JSON, "以 JSON 输出")
	if err := fs.Parse(args); err != nil {
		return err
	}
	a.JSON = *jsonOut
	return a.withStore(func(cfg *config.Config, store *db.Store) error {
		libs, err := store.Libraries()
		if err != nil {
			return err
		}
		// The file browser keeps a hidden scratch library per source; it is not a
		// library the owner made, so it is not listed.
		visible := make([]models.Library, 0, len(libs))
		for _, lib := range libs {
			if !strings.HasPrefix(lib.Name, "__browse__") {
				visible = append(visible, lib)
			}
		}
		type row struct {
			ID        int64    `json:"id"`
			Name      string   `json:"name"`
			Type      string   `json:"type"`
			Path      string   `json:"path"`
			Sources   []string `json:"sources"`
			Providers string   `json:"providers"`
		}
		rows := make([]row, 0, len(visible))
		for _, lib := range visible {
			var paths []string
			for _, ls := range lib.Sources {
				src, err := store.SourceByID(ls.SourceID)
				label := fmt.Sprintf("源%d:%s", ls.SourceID, ls.Path)
				if err == nil {
					label = fmt.Sprintf("%s:%s", src.Name, ls.Path)
				}
				paths = append(paths, label)
			}
			if len(paths) == 0 {
				paths = []string{lib.Path}
			}
			rows = append(rows, row{
				ID: lib.ID, Name: lib.Name, Type: lib.Type, Path: lib.Path,
				Sources: paths, Providers: lib.Providers,
			})
		}
		return a.emit(rows, func() {
			if len(rows) == 0 {
				fmt.Fprintln(a.Out, "还没有媒体库。用 doriyamedia library add 创建。")
				return
			}
			out := make([][]string, 0, len(rows))
			for _, r := range rows {
				out = append(out, []string{fmt.Sprint(r.ID), r.Name, r.Type, strings.Join(r.Sources, ", ")})
			}
			a.table([]string{"ID", "名称", "类型", "扫描位置"}, out)
		})
	})
}

type libraryFlags struct {
	fs       *flag.FlagSet
	name     *string
	typ      *string
	sourceID *int64
	path     *string
	provider *string
	excludes *string
	yes      *bool
}

func newLibraryFlags(a *App, cmd string) *libraryFlags {
	fs := newFlagSet(a, cmd)
	return &libraryFlags{
		fs:       fs,
		name:     fs.String("name", "", "媒体库名称"),
		typ:      fs.String("type", "", "类型: movie|series"),
		sourceID: fs.Int64("source-id", 0, "存储源 ID"),
		path:     fs.String("path", "/", "存储源内的路径"),
		provider: fs.String("providers", "", "刮削顺序，逗号分隔，如 nfo,bgm,tmdb"),
		excludes: fs.String("exclude-keywords", "", "文件名排除词，逗号分隔"),
	}
}

// providersJSON normalises the --providers text into the JSON array the column
// stores. Empty keeps the server's default.
func providersJSON(text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", nil
	}
	var list []string
	for _, item := range strings.Split(text, ",") {
		if item = strings.TrimSpace(item); item != "" {
			list = append(list, item)
		}
	}
	raw, err := json.Marshal(list)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// keywordsJSON is the same for an exclusion list.
func keywordsJSON(text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", nil
	}
	var list []string
	for _, item := range strings.FieldsFunc(text, func(r rune) bool {
		return r == ',' || r == '，' || r == '\n'
	}) {
		if item = strings.TrimSpace(item); item != "" {
			list = append(list, item)
		}
	}
	if err := scan.ValidateExcludes(list); err != nil {
		return "", err
	}
	raw, err := json.Marshal(list)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func libraryAdd(a *App, args []string) error {
	f := newLibraryFlags(a, "library")
	if err := f.fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*f.name) == "" {
		return fmt.Errorf("需要 --name")
	}
	if *f.sourceID <= 0 {
		return fmt.Errorf("需要 --source-id（先用 doriyamedia source list 查看）")
	}
	providers, err := providersJSON(*f.provider)
	if err != nil {
		return err
	}
	excludes, err := keywordsJSON(*f.excludes)
	if err != nil {
		return err
	}
	return a.withStore(func(cfg *config.Config, store *db.Store) error {
		if _, err := store.SourceByID(*f.sourceID); err != nil {
			return fmt.Errorf("存储源 %d 不存在", *f.sourceID)
		}
		lib := &models.Library{
			Name:     strings.TrimSpace(*f.name),
			Type:     *f.typ,
			SourceID: *f.sourceID,
			Path:     *f.path,
		}
		if lib.Type != "movie" && lib.Type != "series" {
			lib.Type = "movie"
		}
		if strings.TrimSpace(lib.Path) == "" {
			lib.Path = "/"
		}
		if providers != "" {
			lib.Providers = providers
		} else {
			lib.Providers = `["nfo","bgm","tmdb"]`
		}
		if excludes != "" {
			lib.ExcludeMode = "custom"
			lib.Excludes = excludes
		}
		if err := store.CreateLibrary(lib); err != nil {
			return err
		}
		if a.JSON {
			return a.emit(lib, nil)
		}
		fmt.Fprintf(a.Out, "已创建媒体库 %s（ID %d，%s，源 %d:%s）\n",
			lib.Name, lib.ID, lib.Type, lib.SourceID, lib.Path)
		fmt.Fprintf(a.Out, "下一步: doriyamedia scan --id %d\n", lib.ID)
		return nil
	})
}

func libraryUpdate(a *App, args []string) error {
	f := newLibraryFlags(a, "library")
	id := f.fs.Int64("id", 0, "媒体库 ID")
	target := f.fs.String("target", "", "媒体库名称（代替 --id）")
	if err := f.fs.Parse(args); err != nil {
		return err
	}
	providers, err := providersJSON(*f.provider)
	if err != nil {
		return err
	}
	excludes, err := keywordsJSON(*f.excludes)
	if err != nil {
		return err
	}
	return a.withStore(func(cfg *config.Config, store *db.Store) error {
		lib, err := resolveLibrary(store, *id, *target)
		if err != nil {
			return err
		}
		if strings.TrimSpace(*f.name) != "" {
			lib.Name = strings.TrimSpace(*f.name)
		}
		if *f.typ == "movie" || *f.typ == "series" {
			lib.Type = *f.typ
		}
		if *f.sourceID > 0 {
			if _, err := store.SourceByID(*f.sourceID); err != nil {
				return fmt.Errorf("存储源 %d 不存在", *f.sourceID)
			}
			lib.SourceID = *f.sourceID
			lib.Path = *f.path
			if err := store.SetLibrarySources(lib.ID, []models.LibrarySource{
				{LibraryID: lib.ID, SourceID: *f.sourceID, Path: *f.path},
			}); err != nil {
				return err
			}
		}
		if providers != "" {
			lib.Providers = providers
		}
		if excludes != "" {
			lib.ExcludeMode = "custom"
			lib.Excludes = excludes
		}
		if err := store.UpdateLibrary(lib); err != nil {
			return err
		}
		if a.JSON {
			return a.emit(lib, nil)
		}
		fmt.Fprintf(a.Out, "已更新媒体库 %s（ID %d）\n", lib.Name, lib.ID)
		return nil
	})
}

func libraryRemove(a *App, args []string) error {
	fs := newFlagSet(a, "library")
	id := fs.Int64("id", 0, "媒体库 ID")
	name := fs.String("name", "", "媒体库名称（代替 --id）")
	yes := fs.Bool("yes", false, "跳过确认")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return a.withStore(func(cfg *config.Config, store *db.Store) error {
		lib, err := resolveLibrary(store, *id, *name)
		if err != nil {
			return err
		}
		if !*yes {
			return fmt.Errorf("删除媒体库 %q 会连同其条目与播放进度一起删除；确认请加 --yes", lib.Name)
		}
		if err := store.DeleteLibrary(lib.ID); err != nil {
			return err
		}
		if a.JSON {
			return a.emit(map[string]any{"ok": true, "deleted": lib.ID}, nil)
		}
		fmt.Fprintf(a.Out, "已删除媒体库 %s（ID %d）\n", lib.Name, lib.ID)
		return nil
	})
}

// libraryScan is the same blocking scan as `doriyamedia scan`, reached from the
// noun-first spelling someone may type after `library list`. A background
// variant would be a lie: the scan lives in this process, so it would die when
// the command returns.
func libraryScan(a *App, args []string) error {
	return runScan(a, args)
}
