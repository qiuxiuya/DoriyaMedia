package cli

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"doriyamedia/internal/config"
	"doriyamedia/internal/db"
	"doriyamedia/internal/models"
)

// runItem browses what a scan imported, without the web UI.
func runItem(a *App, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法: doriyamedia item <list|show> ...")
	}
	switch args[0] {
	case "list", "ls":
		return itemList(a, args[1:])
	case "show":
		return itemShow(a, args[1:])
	default:
		return fmt.Errorf("未知子命令: item %s", args[0])
	}
}

func itemList(a *App, args []string) error {
	fs := newFlagSet(a, "item")
	libID := fs.Int64("library-id", 0, "只看某个媒体库")
	libName := fs.String("library", "", "只看某个媒体库（名称）")
	typ := fs.String("type", "", "类型: movie|show|season|episode")
	search := fs.String("search", "", "按名称搜索")
	sortBy := fs.String("sort", "", "排序: name|year|rating（默认按加入时间）")
	limit := fs.Int("limit", 50, "最多显示多少条")
	jsonOut := fs.Bool("json", a.JSON, "以 JSON 输出")
	if err := fs.Parse(args); err != nil {
		return err
	}
	a.JSON = *jsonOut

	return a.withStore(func(cfg *config.Config, store *db.Store) error {
		libraryID := *libID
		if *libName != "" {
			lib, err := resolveLibrary(store, 0, *libName)
			if err != nil {
				return err
			}
			libraryID = lib.ID
		}
		var (
			items []models.Item
			total int
			err   error
		)
		if libraryID > 0 {
			if *typ == "" {
				// Without a type the library query would also return seasons and
				// episodes, which is not what "list the library" means. Roots
				// are what the grid on the home page shows.
				items, total, err = store.ItemsByLibrary(libraryID, "movie", *search, *sortBy, true, 0, *limit)
				if err == nil {
					var shows []models.Item
					var showsTotal int
					shows, showsTotal, err = store.ItemsByLibrary(libraryID, "show", *search, *sortBy, true, 0, *limit)
					items = append(items, shows...)
					total += showsTotal
				}
			} else {
				items, total, err = store.ItemsByLibrary(libraryID, *typ, *search, *sortBy, true, 0, *limit)
			}
		} else {
			// No library: list roots of every library, newest first. Reading the
			// per-library query and merging would lose the global ordering, so
			// this uses the recent-items path the home page uses.
			if *typ == "" {
				movies, merr := store.RecentItems("movie", *limit)
				shows, serr := store.RecentItems("show", *limit)
				if merr != nil {
					err = merr
				} else if serr != nil {
					err = serr
				}
				items = append(movies, shows...)
				sortItems(items)
				if len(items) > *limit {
					items = items[:*limit]
				}
			} else {
				items, err = store.RecentItems(*typ, *limit)
			}
			total = len(items)
		}
		if err != nil {
			return err
		}
		return a.emit(map[string]any{"total": total, "items": items}, func() {
			if len(items) == 0 {
				fmt.Fprintln(a.Out, "没有匹配的条目。")
				return
			}
			rows := make([][]string, 0, len(items))
			for _, it := range items {
				rows = append(rows, []string{
					strconv.FormatInt(it.ID, 10), it.Type, itemTitle(it), itemExtra(it),
				})
			}
			a.table([]string{"ID", "类型", "名称", "年份/季集"}, rows)
			if total > len(items) {
				fmt.Fprintf(a.Out, "\n共 %d 条，已显示前 %d 条（用 --limit 调整）\n", total, len(items))
			}
		})
	})
}

func itemShow(a *App, args []string) error {
	fs := newFlagSet(a, "item")
	jsonOut := fs.Bool("json", a.JSON, "以 JSON 输出")
	if err := fs.Parse(args); err != nil {
		return err
	}
	a.JSON = *jsonOut
	if fs.NArg() != 1 {
		return fmt.Errorf("用法: doriyamedia item show <ID>")
	}
	itemID, err := parseID(fs.Arg(0))
	if err != nil {
		return err
	}
	return a.withStore(func(cfg *config.Config, store *db.Store) error {
		it, err := store.ItemByID(itemID)
		if err != nil {
			return fmt.Errorf("条目 %d 不存在", itemID)
		}
		files, err := store.FilesByItem(itemID)
		if err != nil {
			return err
		}
		children, _ := store.ItemChildren(itemID)
		type fileRow struct {
			ID       int64  `json:"id"`
			Path     string `json:"path"`
			Size     int64  `json:"size"`
			SourceID int64  `json:"source_id"`
			Probed   bool   `json:"probed"`
		}
		fileRows := make([]fileRow, 0, len(files))
		for _, f := range files {
			fileRows = append(fileRows, fileRow{
				ID: f.ID, Path: f.Path, Size: f.Size, SourceID: f.SourceID, Probed: f.Probe != "",
			})
		}
		return a.emit(map[string]any{
			"item": it, "files": fileRows, "children": children,
		}, func() {
			fmt.Fprintf(a.Out, "%s\n", itemTitle(*it))
			fmt.Fprintf(a.Out, "  ID        %d\n", it.ID)
			fmt.Fprintf(a.Out, "  类型      %s\n", it.Type)
			fmt.Fprintf(a.Out, "  媒体库    %d\n", it.LibraryID)
			if it.Year > 0 {
				fmt.Fprintf(a.Out, "  年份      %d\n", it.Year)
			}
			if it.Season > 0 || it.Episode > 0 {
				fmt.Fprintf(a.Out, "  季/集     S%02dE%02d\n", it.Season, it.Episode)
			}
			if it.TMDBID > 0 {
				fmt.Fprintf(a.Out, "  TMDB      %d\n", it.TMDBID)
			}
			if it.BGMID > 0 {
				fmt.Fprintf(a.Out, "  Bangumi   %d\n", it.BGMID)
			}
			if it.Rating > 0 {
				fmt.Fprintf(a.Out, "  评分      %.1f\n", it.Rating)
			}
			fmt.Fprintf(a.Out, "  加入时间  %s\n", humanTime(it.AddedAt))
			if it.Overview != "" {
				fmt.Fprintf(a.Out, "  简介      %s\n", oneLine(it.Overview, 200))
			}
			if len(fileRows) > 0 {
				fmt.Fprintln(a.Out, "\n  文件:")
				for _, f := range fileRows {
					probe := "未探测"
					if f.Probed {
						probe = "已探测"
					}
					fmt.Fprintf(a.Out, "    #%d  %s  %s  [%s]\n", f.ID, humanSize(f.Size), f.Path, probe)
				}
			}
			if len(children) > 0 {
				fmt.Fprintf(a.Out, "\n  子条目 %d 个（doriyamedia item list --library-id %d）\n", len(children), it.LibraryID)
			}
		})
	})
}

// sortItems orders items newest first, matching the SQL queries' default.
func sortItems(items []models.Item) {
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].AddedAt.After(items[j].AddedAt)
	})
}

func itemTitle(it models.Item) string {
	name := it.Name
	if it.OriginalTitle != "" && it.OriginalTitle != it.Name {
		name += " / " + it.OriginalTitle
	}
	return name
}

func itemExtra(it models.Item) string {
	switch {
	case it.Type == "episode" && it.Episode > 0:
		return fmt.Sprintf("S%02dE%02d", it.Season, it.Episode)
	case it.Year > 0:
		return strconv.Itoa(it.Year)
	default:
		return ""
	}
}

// oneLine collapses whitespace and truncates for a terminal listing.
func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}
