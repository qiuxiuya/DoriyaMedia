package metadata

import (
	"path/filepath"
	"strings"
	"testing"

	"doriyamedia/internal/config"
	"doriyamedia/internal/db"
	"doriyamedia/internal/models"
)

func TestRefreshItemRecordsMissingFilesAndDriverErrors(t *testing.T) {
	dir := t.TempDir()
	store, err := db.Open(filepath.Join(dir, "refresh.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.DB.Close()
	src := &models.Source{Name: "test", Type: "local", Config: `{}`}
	if err := store.CreateSource(src); err != nil {
		t.Fatal(err)
	}
	lib := &models.Library{Name: "test", Type: "series", SourceID: src.ID, Path: "/", Providers: `["nfo"]`}
	if err := store.CreateLibrary(lib); err != nil {
		t.Fatal(err)
	}
	show := &models.Item{LibraryID: lib.ID, Type: "show", Name: "test show"}
	if err := store.InsertItem(show); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(store, config.Defaults())
	if err := mgr.RefreshItem(show.ID, 0); err != nil {
		t.Fatalf("missing file must not block online identification: %v", err)
	}
	issues, err := store.ScanIssues(lib.ID, "")
	if err != nil || len(issues) != 1 || issues[0].Kind != models.ScanIssueScrape || !strings.Contains(issues[0].Detail, "文件关联异常") {
		t.Fatalf("nil refresh result hid the unresolved file association: %+v, %v", issues, err)
	}

	src.Type = "invalid"
	if err := store.UpdateSource(src); err != nil {
		t.Fatal(err)
	}
	refreshErr := mgr.RefreshItem(show.ID, 0)
	if refreshErr == nil {
		t.Fatal("invalid driver must fail")
	}
	issues, err = store.ScanIssues(lib.ID, "")
	if err != nil || len(issues) != 1 || !strings.Contains(issues[0].Detail, refreshErr.Error()) {
		t.Fatalf("manual refresh failure was not persisted: %+v, %v", issues, err)
	}
}
