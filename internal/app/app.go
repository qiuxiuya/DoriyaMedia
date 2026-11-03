// Package app wires the service together and runs the HTTP server. It lives
// outside cmd/ so the CLI's `serve` subcommand and the bare `doriyamedia`
// invocation start exactly the same process.
package app

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"

	"doriyamedia/internal/api"
	"doriyamedia/internal/config"
	"doriyamedia/internal/db"
	"doriyamedia/internal/media"
	"doriyamedia/internal/metadata"
	"doriyamedia/internal/models"
	"doriyamedia/internal/scan"
	"doriyamedia/internal/site"
	"doriyamedia/internal/source"
	"doriyamedia/internal/web"
)

// Serve loads the configuration and blocks serving HTTP until the process is
// stopped. A listen address or port argument overrides the config file when it
// is not empty / greater than zero.
func Serve(cfgPath string, version string, listen string, port int) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if port > 0 {
		cfg.Port = port
	}
	if listen != "" {
		host, err := config.NormalizeListen(listen)
		if err != nil {
			return err
		}
		cfg.Listen = host
	}
	if err := os.MkdirAll(cfg.ImagesDir(), 0o755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	store, err := db.Open(cfg.DBPath())
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}

	engine := media.NewEngine(store, cfg)
	// Adopt packaged output a previous run left behind (and drop anything that
	// never finished), so replaying a file does not package it again.
	engine.LoadCache()
	manager := metadata.NewManager(store, cfg)
	scanner := &scan.Scanner{
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
	// Probe is the slowest step of starting playback on a cloud drive. Warm the
	// cache in the background after each scan so opening a file is already
	// decided; bounded and rate-limited so it never competes with playback.
	scanner.OnScanned = func(lib *models.Library) {
		if !cfg.Playback.PreProbeAfterScan {
			return
		}
		files, err := store.FilesWithoutProbe(lib.ID, cfg.Playback.PreProbeMaxFiles)
		if err != nil {
			log.Printf("pre-probe: list files for library %d failed: %v", lib.ID, err)
			return
		}
		engine.PreProbeFiles(files)
	}
	scanner.OnScannedFiles = func(lib *models.Library, files []models.MediaFile) {
		if !cfg.Playback.PreProbeAfterScan {
			return
		}
		var pending []*models.MediaFile
		for i := range files {
			if files[i].Probe == "" {
				pending = append(pending, &files[i])
				if cfg.Playback.PreProbeMaxFiles > 0 && len(pending) >= cfg.Playback.PreProbeMaxFiles {
					break
				}
			}
		}
		engine.PreProbeFiles(pending)
	}

	srv := api.New(store, cfg, scanner, manager, engine, web.Dist(), version)
	srv.ConfigPath = cfgPath
	siteStore, err := site.Open(cfg.SitePath())
	if err != nil {
		return fmt.Errorf("open site content: %w", err)
	}
	srv.Site = siteStore
	srv.BangumiSync.Start()

	// DORIYAMEDIA_LOCAL is the older way to say "loopback only". It still wins
	// over the config file, because whoever set it did so on this run's
	// environment and meant it — but it is no longer the only way, so a
	// `listen` setting is what a config-file deployment uses.
	if os.Getenv("DORIYAMEDIA_LOCAL") == "1" {
		cfg.Listen = "127.0.0.1"
	}
	addr := cfg.ListenAddr()
	log.Printf("===========================================")
	log.Printf("  DoriyaMedia %s  →  http://%s", version, displayAddr(addr))
	if !cfg.ListenIsLocal() {
		log.Printf("  监听所有网卡时，请确认防火墙只放行你需要的网段；只想本机访问就用 -listen 127.0.0.1 启动")
	}
	log.Printf("  首次使用请先打开网页完成管理员初始化")
	log.Printf("===========================================")
	httpServer := &http.Server{Addr: addr, Handler: srv.Handler()}
	return httpServer.ListenAndServe()
}

// displayAddr rewrites a wildcard bind address into one the owner can actually
// type: "0.0.0.0" and "::" are instructions to the kernel, not addresses a
// browser accepts, and printing them leaves the reader with a URL that fails.
func displayAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if host == "" {
		return net.JoinHostPort("127.0.0.1", port)
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		return net.JoinHostPort("127.0.0.1", port)
	}
	return addr
}
