package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"doriyamedia/internal/config"
	"doriyamedia/internal/db"
	"doriyamedia/internal/models"
)

func TestSourceListsRespectBrowserAndAdminPermissions(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "sources.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.DB.Close() })
	for _, role := range []string{models.RoleAdmin, models.RoleUser} {
		user := &models.User{Name: role, Role: role}
		if err := store.CreateUser(user); err != nil {
			t.Fatal(err)
		}
		if err := store.CreateSession(role, user.ID, time.Now().Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	src := &models.Source{Name: "private storage", Type: "webdav", Config: `{"webdav_pass":"test-secret"}`}
	if err := store.CreateSource(src); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	server := &Server{Store: store, Cfg: cfg}
	handler := server.Handler()

	for _, mode := range []string{config.FileBrowserOff, config.FileBrowserAdmin, config.FileBrowserAll} {
		cfg.FileBrowser = mode
		for _, token := range []string{"", models.RoleUser, models.RoleAdmin} {
			for _, path := range []string{"/api/sources", "/api/admin/sources"} {
				t.Run(mode+"/"+token+path, func(t *testing.T) {
					wantStatus := http.StatusOK
					if token == "" {
						wantStatus = http.StatusUnauthorized
					} else if path == "/api/admin/sources" {
						if token != models.RoleAdmin {
							wantStatus = http.StatusForbidden
						}
					} else if mode == config.FileBrowserOff || (mode == config.FileBrowserAdmin && token != models.RoleAdmin) {
						wantStatus = http.StatusForbidden
					}
					req := httptest.NewRequest(http.MethodGet, path, nil)
					if token != "" {
						req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
					}
					rec := httptest.NewRecorder()
					handler.ServeHTTP(rec, req)
					if rec.Code != wantStatus {
						t.Fatalf("status = %d, want %d: %s", rec.Code, wantStatus, rec.Body.String())
					}
					if wantStatus != http.StatusOK {
						return
					}
					var rows []sourceDTO
					if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
						t.Fatal(err)
					}
					if len(rows) != 1 || rows[0].ID != src.ID {
						t.Fatalf("unexpected sources: %+v", rows)
					}
					wantPass := "test-secret"
					if token != models.RoleAdmin {
						wantPass = maskedSecret
					}
					if rows[0].Config.WebDAVPass != wantPass {
						t.Fatal("source credential visibility does not match user role")
					}
				})
			}
		}
	}

	// Turning the browser off must still block raw directories and playback,
	// including for the administrator who can manage sources in settings.
	cfg.FileBrowser = config.FileBrowserOff
	for _, path := range []string{"/api/browse/1", "/api/browseplay/1"} {
		t.Run("off/admin"+path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.AddCookie(&http.Cookie{Name: cookieName, Value: models.RoleAdmin})
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403: %s", rec.Code, rec.Body.String())
			}
		})
	}
}
