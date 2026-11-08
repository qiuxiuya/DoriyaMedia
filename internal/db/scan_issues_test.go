package db

import (
	"path/filepath"
	"strings"
	"testing"

	"doriyamedia/internal/models"
)

func issueTestStore(t *testing.T) (*Store, *models.Library, int64) {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "issues.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.DB.Close() })
	src := &models.Source{Name: "test", Type: "local", Config: `{}`}
	if err := s.CreateSource(src); err != nil {
		t.Fatal(err)
	}
	lib := &models.Library{Name: "test", Type: "series", SourceID: src.ID, Path: "/", Providers: `["nfo"]`}
	if err := s.CreateLibrary(lib); err != nil {
		t.Fatal(err)
	}
	return s, lib, src.ID
}

func issueTestItem(t *testing.T, s *Store, libID int64, typ, name string, parent *int64) *models.Item {
	t.Helper()
	it := &models.Item{LibraryID: libID, Type: typ, Name: name, ParentID: parent}
	if err := s.InsertItem(it); err != nil {
		t.Fatal(err)
	}
	return it
}

func issueTestFile(t *testing.T, s *Store, itemID, srcID int64, path string) {
	t.Helper()
	if _, err := s.UpsertFile(&models.MediaFile{ItemID: itemID, SourceID: srcID, Path: path}); err != nil {
		t.Fatal(err)
	}
}

func TestScrapeIssuesReportsEmptyAndUnidentifiedRoots(t *testing.T) {
	s, lib, srcID := issueTestStore(t)
	empty := issueTestItem(t, s, lib.ID, "show", "empty", nil)
	issueTestItem(t, s, lib.ID, "season", "season", &empty.ID)
	identifiedEmpty := issueTestItem(t, s, lib.ID, "show", "identified-empty", nil)
	identifiedEmpty.BGMID = 123
	if err := s.UpdateItemMeta(identifiedEmpty); err != nil {
		t.Fatal(err)
	}
	show := issueTestItem(t, s, lib.ID, "show", "unidentified", nil)
	season := issueTestItem(t, s, lib.ID, "season", "season", &show.ID)
	ep := issueTestItem(t, s, lib.ID, "episode", "episode", &season.ID)
	issueTestFile(t, s, ep.ID, srcID, "/show/01.mkv")
	issueTestFile(t, s, ep.ID, srcID, "/show/01-alt.mkv")
	identified := issueTestItem(t, s, lib.ID, "show", "identified", nil)
	identified.BGMID = 456
	if err := s.UpdateItemMeta(identified); err != nil {
		t.Fatal(err)
	}
	// Older imports can parent an episode directly to a show.
	direct := issueTestItem(t, s, lib.ID, "episode", "episode", &identified.ID)
	issueTestFile(t, s, direct.ID, srcID, "/identified/01.mkv")
	movie := issueTestItem(t, s, lib.ID, "movie", "empty-movie", nil)

	issues, err := s.ScrapeIssues(lib.ID, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 4 {
		t.Fatalf("expected 4 root issues, got %+v", issues)
	}
	byID := map[int64]models.ScanIssue{}
	for _, issue := range issues {
		if issue.Kind != models.ScanIssueScrape {
			t.Fatalf("file association is not type mismatch: %+v", issue)
		}
		byID[issue.ItemID] = issue
	}
	for _, id := range []int64{empty.ID, identifiedEmpty.ID, movie.ID} {
		issue, ok := byID[id]
		if !ok || issue.Path != "" || !strings.Contains(issue.Detail, "文件关联异常") {
			t.Fatalf("missing empty-root diagnostic for %d: %+v", id, issue)
		}
	}
	if issue := byID[show.ID]; issue.Path == "" || !strings.Contains(issue.Detail, "没有匹配到刮削结果") {
		t.Fatalf("wrong metadata failure diagnostic: %+v", issue)
	}
	if _, ok := byID[identified.ID]; ok {
		t.Fatal("identified playable show must not be reported")
	}
}

func TestScrapeIssuesScopeUsesLiteralPathAndSource(t *testing.T) {
	s, lib, srcID := issueTestStore(t)
	for _, path := range []string{"/show_1%/01.mkv", "/showX1anything/01.mkv", "/other/01.mkv"} {
		show := issueTestItem(t, s, lib.ID, "show", path, nil)
		ep := issueTestItem(t, s, lib.ID, "episode", "episode", &show.ID)
		issueTestFile(t, s, ep.ID, srcID, path)
	}
	issueTestItem(t, s, lib.ID, "show", "empty", nil)
	for _, scope := range []struct {
		path string
		n    int
	}{{"/show_1%", 1}, {"/", 3}} {
		issues, err := s.ScrapeIssues(lib.ID, srcID, scope.path)
		if err != nil || len(issues) != scope.n {
			t.Fatalf("scope %q: got %+v, err=%v", scope.path, issues, err)
		}
	}
	issues, err := s.ScrapeIssues(lib.ID, srcID+100, "/")
	if err != nil || len(issues) != 0 {
		t.Fatalf("wrong source leaked: %+v, err=%v", issues, err)
	}
}

func TestSyncItemScrapeIssueReconcilesOnlyRefreshedRoot(t *testing.T) {
	s, lib, srcID := issueTestStore(t)
	show := issueTestItem(t, s, lib.ID, "show", "show", nil)
	other := issueTestItem(t, s, lib.ID, "show", "other", nil)
	seed := []models.ScanIssue{
		{LibraryID: lib.ID, Kind: models.ScanIssueMismatch, Path: "/unnumbered.mkv"},
		{LibraryID: lib.ID, Kind: models.ScanIssueScrape, ItemID: other.ID, Detail: "keep"},
	}
	if err := s.ReplaceScanIssues(lib.ID, seed, 0, ""); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := s.SyncItemScrapeIssue(show.ID, "provider failed"); err != nil {
			t.Fatal(err)
		}
	}
	issues, err := s.ScanIssues(lib.ID, "")
	if err != nil || len(issues) != 3 {
		t.Fatalf("refresh must dedupe and preserve other issues: %+v, %v", issues, err)
	}
	for _, issue := range issues {
		if issue.ItemID == show.ID && (!strings.Contains(issue.Detail, "provider failed") || !strings.Contains(issue.Detail, "文件关联异常")) {
			t.Fatalf("missing failure context: %+v", issue)
		}
	}

	show.BGMID = 123
	if err := s.UpdateItemMeta(show); err != nil {
		t.Fatal(err)
	}
	if err := s.SyncItemScrapeIssue(show.ID, ""); err != nil {
		t.Fatal(err)
	}
	issues, _ = s.ScanIssues(lib.ID, "")
	if len(issues) != 3 {
		t.Fatal("matching metadata alone must not hide missing files")
	}
	ep := issueTestItem(t, s, lib.ID, "episode", "episode", &show.ID)
	issueTestFile(t, s, ep.ID, srcID, "/show/01.mkv")
	if err := s.SyncItemScrapeIssue(show.ID, ""); err != nil {
		t.Fatal(err)
	}
	issues, err = s.ScanIssues(lib.ID, "")
	if err != nil || len(issues) != 2 {
		t.Fatalf("repair should clear only this root: %+v, %v", issues, err)
	}
	for _, issue := range issues {
		if issue.ItemID == show.ID {
			t.Fatalf("repaired issue survived: %+v", issue)
		}
	}
}
