package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"doriyamedia/internal/config"
	"doriyamedia/internal/db"
	"doriyamedia/internal/metadata"
	"doriyamedia/internal/models"
	"doriyamedia/internal/scan"
	"doriyamedia/internal/source"
)

// newScanner wires a Scanner exactly the way the server does, minus the HTTP
// server: identification (metadata enrichment) is part of a scan, so a scan
// started from the CLI produces the same library a scan from the settings page
// does. Pre-probing is deliberately left out — it is a playback cache warm-up,
// not part of the import, and it would keep the command alive for minutes.
func newScanner(cfg *config.Config, store *db.Store) *scan.Scanner {
	manager := metadata.NewManager(store, cfg)
	return &scan.Scanner{
		Store: store,
		Cfg:   cfg,
		OnIdentify: func(lib *models.Library, listings map[string][]source.Entry, report func(done, total int, msg string)) {
			manager.SeedListings(listings)
			manager.EnrichLibrary(lib, report)
		},
		OnIdentifyFiles: func(lib *models.Library, files []models.MediaFile, listings map[string][]source.Entry, report func(done, total int, msg string)) {
			manager.SeedListings(listings)
			manager.EnrichFiles(lib, files, report)
		},
	}
}

// runScan scans a library and blocks until it finishes, printing progress. This
// is the command an operator runs over SSH after adding files; it does not need
// the HTTP server, and it exits non-zero when the scan failed.
func runScan(a *App, args []string) error {
	fs := newFlagSet(a, "scan")
	id := fs.Int64("id", 0, "媒体库 ID")
	name := fs.String("name", "", "媒体库名称（代替 --id）")
	path := fs.String("path", "", "只扫描该源内目录（需配合 --source-id）")
	sourceID := fs.Int64("source-id", 0, "配合 --path 指定存储源")
	jsonOut := fs.Bool("json", a.JSON, "以 JSON 输出最终结果")
	timeout := fs.Duration("timeout", 2*time.Hour, "整体超时")
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

	lib, err := resolveLibrary(store, *id, *name)
	if err != nil {
		return err
	}
	if *path != "" && *sourceID <= 0 {
		return fmt.Errorf("--path 需要同时给出 --source-id")
	}

	sc := newScanner(cfg, store)
	if *path != "" {
		err = sc.ScanLibraryPath(lib, *sourceID, *path)
	} else {
		err = sc.ScanLibrary(lib)
	}
	if err != nil {
		return err
	}

	// Ctrl+C cancels the scan instead of killing the process mid-write.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		sc.Cancel(lib.ID)
	}()
	deadline := time.NewTimer(*timeout)
	defer deadline.Stop()

	if !a.JSON {
		fmt.Fprintf(a.Out, "开始扫描 %s（ID %d）…\n", lib.Name, lib.ID)
	}
	var lastSeq int64
	var lastPhase string
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	finish := func(st models.ScanState) error {
		if a.JSON {
			// The report goes out first; the exit code then reflects the scan,
			// so a script that only checks $? still learns the scan failed.
			if err := a.emit(st, nil); err != nil {
				return err
			}
			if st.Error != "" {
				return errSilentFailure
			}
			return nil
		}
		fmt.Fprintf(a.Out, "\n完成：%d 个视频文件\n", st.Total)
		if st.Error != "" {
			return fmt.Errorf("%s", st.Error)
		}
		return nil
	}

	for {
		select {
		case <-deadline.C:
			sc.Cancel(lib.ID)
			return fmt.Errorf("扫描超时（%s），已请求取消", *timeout)
		case <-ctx.Done():
			sc.Cancel(lib.ID)
			return fmt.Errorf("已取消")
		case <-ticker.C:
		}

		st := sc.State(lib.ID)
		if !a.JSON {
			// Print the live feed incrementally: a large scan is otherwise a
			// silent terminal for minutes.
			for _, ev := range st.Events {
				if ev.Seq <= lastSeq {
					continue
				}
				lastSeq = ev.Seq
				fmt.Fprintf(a.Out, "  [%s] %s\n", ev.Kind, ev.Text)
			}
			if st.Phase != lastPhase {
				lastPhase = st.Phase
				switch st.Phase {
				case "walk":
					fmt.Fprintln(a.Out, "阶段：遍历目录")
				case "organize":
					fmt.Fprintf(a.Out, "阶段：整理（%d 个文件）\n", st.Total)
				case "identify":
					fmt.Fprintln(a.Out, "阶段：刮削与识别")
				}
			}
			if st.Phase == "walk" && st.DirsDone > 0 {
				fmt.Fprintf(a.Out, "\r  已遍历 %d 个目录，发现 %d 个文件…", st.DirsDone, st.FilesFound)
			}
		}

		if !st.Running && st.Phase != "idle" && st.Phase != "" {
			return finish(st)
		}
		if st.Phase == "idle" {
			// The worker has not picked the job up yet.
			continue
		}
	}
}
