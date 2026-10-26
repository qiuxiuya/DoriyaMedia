package api

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"doriyamedia/internal/models"
)

type embyLatestFixture struct {
	s                                          *Server
	ts                                         *httptest.Server
	token                                      string
	anime, movies, hidden                      *models.Library
	a, b, a1, a2, b1, ae1, ae2, ae3, be1, film *models.Item
}

func newEmbyLatestFixture(t *testing.T) embyLatestFixture {
	t.Helper()
	s, ts := newTestServer(t)
	f := embyLatestFixture{s: s, ts: ts, token: embyLoginTest(t, ts)}
	fid := seedProgressFile(t, s.Store)
	file, err := s.Store.FileByID(fid)
	if err != nil {
		t.Fatal(err)
	}
	f.film, err = s.Store.ItemByID(file.ItemID)
	if err != nil {
		t.Fatal(err)
	}
	f.movies, err = s.Store.LibraryByID(f.film.LibraryID)
	if err != nil {
		t.Fatal(err)
	}
	makeLibrary := func(name, typ string) *models.Library {
		t.Helper()
		lib := &models.Library{Name: name, Type: typ, SourceID: f.movies.SourceID, Path: name}
		if err := s.Store.CreateLibrary(lib); err != nil {
			t.Fatal(err)
		}
		return lib
	}
	f.anime = makeLibrary("Anime", "series")
	f.hidden = makeLibrary("__browse__scratch", "series")
	other := makeLibrary("Music", "music")
	add := func(lib *models.Library, typ, name string, parent *models.Item, season, episode, minute int) *models.Item {
		t.Helper()
		it := &models.Item{LibraryID: lib.ID, Type: typ, Name: name, Season: season, Episode: episode}
		if parent != nil {
			it.ParentID = &parent.ID
		}
		if err := s.Store.InsertItem(it); err != nil {
			t.Fatal(err)
		}
		date := time.Date(2025, 1, 1, 0, minute, 0, 0, time.UTC)
		if _, err := s.Store.DB.Exec("UPDATE items SET added_at=? WHERE id=?", date, it.ID); err != nil {
			t.Fatal(err)
		}
		return it
	}
	// Containers deliberately sort opposite to the newest matching episodes.
	f.a = add(f.anime, "show", "Alpha", nil, 0, 0, 1)
	f.b = add(f.anime, "show", "Beta", nil, 0, 0, 55)
	f.a1 = add(f.anime, "season", "Alpha season 1", f.a, 1, 0, 2)
	f.a2 = add(f.anime, "season", "Alpha season 2", f.a, 2, 0, 59)
	f.b1 = add(f.anime, "season", "Beta season 1", f.b, 1, 0, 56)
	f.ae1 = add(f.anime, "episode", "Alpha old episode", f.a1, 1, 1, 10)
	f.be1 = add(f.anime, "episode", "Beta episode", f.b1, 1, 1, 20)
	f.ae2 = add(f.anime, "episode", "Alpha newest episode", f.a2, 2, 1, 40)
	f.ae3 = add(f.anime, "episode", "Alpha second newest episode", f.a2, 2, 2, 30)
	// Direct show parents are supported alongside the scanner's season hierarchy.
	add(f.hidden, "episode", "Hidden episode", add(f.hidden, "show", "Hidden show", nil, 0, 0, 57), 1, 1, 58)
	add(other, "movie", "Not a supported library", nil, 0, 0, 58)
	if _, err := s.Store.DB.Exec("UPDATE items SET added_at=? WHERE id=?", time.Date(2025, 1, 1, 0, 25, 0, 0, time.UTC), f.film.ID); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f embyLatestFixture) latest(t *testing.T, query string) []map[string]any {
	t.Helper()
	code, raw := embyRequest(t, f.ts.URL, "GET", "/emby/Users/1/Items/Latest"+query, f.token, nil)
	if code != 200 {
		t.Fatalf("latest %d: %s", code, raw)
	}
	var out []map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("latest array: %v: %s", err, raw)
	}
	if out == nil {
		t.Fatalf("latest must be array: %s", raw)
	}
	return out
}

func embyLatestWantIDs(t *testing.T, got []map[string]any, want ...*models.Item) {
	t.Helper()
	ids, expected := []string{}, []string{}
	for _, it := range got {
		ids = append(ids, it["Id"].(string))
	}
	for _, it := range want {
		expected = append(expected, embyID(it.ID))
	}
	if !reflect.DeepEqual(ids, expected) {
		t.Fatalf("latest IDs = %v, want %v; DTOs: %v", ids, expected, got)
	}
}

func TestEmbyLatestGroupsMatchingLeavesBeforePagination(t *testing.T) {
	f := newEmbyLatestFixture(t)
	parent := "?ParentId=lib-" + embyID(f.anime.ID)
	for _, flag := range []string{"", "&GroupItems=true", "&GroupItems=TRUE"} {
		got := f.latest(t, parent+flag)
		embyLatestWantIDs(t, got, f.a, f.b)
		if got[0]["Type"] != "Series" || got[0]["ChildCount"] != float64(3) || got[1]["ChildCount"] != float64(1) {
			t.Fatalf("grouped DTO: %v", got)
		}
	}
	embyLatestWantIDs(t, f.latest(t, parent+"&StartIndex=1&Limit=1"), f.b)
	embyLatestWantIDs(t, f.latest(t, parent+"&Limit=1"), f.a)
	embyLatestWantIDs(t, f.latest(t, parent+"&StartIndex=2&Limit=1"))
	embyLatestWantIDs(t, f.latest(t, parent+"&Limit=0"))
	// A newly added episode refreshes an old show ahead of more recent containers.
	if _, err := f.s.Store.DB.Exec("UPDATE items SET added_at=? WHERE id=?", time.Date(2025, 1, 1, 0, 45, 0, 0, time.UTC), f.be1.ID); err != nil {
		t.Fatal(err)
	}
	embyLatestWantIDs(t, f.latest(t, parent), f.b, f.a)
}

func TestEmbyLatestUngroupedAndEpisodeFilter(t *testing.T) {
	f := newEmbyLatestFixture(t)
	parent := "?ParentId=lib-" + embyID(f.anime.ID)
	got := f.latest(t, parent+"&GroupItems=false")
	embyLatestWantIDs(t, got, f.ae2, f.ae3, f.be1, f.ae1)
	for _, dto := range got {
		if dto["Type"] != "Episode" {
			t.Fatalf("container leaked into ungrouped latest: %v", dto)
		}
	}
	embyLatestWantIDs(t, f.latest(t, parent+"&GroupItems=FALSE&StartIndex=1&Limit=2"), f.ae3, f.be1)
	embyLatestWantIDs(t, f.latest(t, parent+"&IncludeItemTypes=Episode"), f.a, f.b)
	embyLatestWantIDs(t, f.latest(t, parent+"&IncludeItemTypes=Episode&GroupItems=false"), f.ae2, f.ae3, f.be1, f.ae1)
	embyLatestWantIDs(t, f.latest(t, parent+"&IncludeItemTypes=Movie"))
	embyLatestWantIDs(t, f.latest(t, parent+"&IncludeItemTypes=Series,Season,Episode"), f.a, f.b)
	embyLatestWantIDs(t, f.latest(t, parent+"&SearchTerm=Beta"), f.b)
	embyLatestWantIDs(t, f.latest(t, parent+"&Ids="+embyID(f.be1.ID)), f.b)
	got = f.latest(t, "?ParentId="+embyID(f.a2.ID))
	embyLatestWantIDs(t, got, f.a)
	if got[0]["ChildCount"] != float64(2) {
		t.Fatalf("season scope lost: %v", got)
	}
	embyLatestWantIDs(t, f.latest(t, parent+"&Season=1"), f.b, f.a)
}

func TestEmbyLatestUserFiltersApplyToLeavesBeforeGrouping(t *testing.T) {
	f := newEmbyLatestFixture(t)
	parent := "?ParentId=lib-" + embyID(f.anime.ID)
	for i, it := range []*models.Item{f.ae2, f.ae3} {
		fid, err := f.s.Store.UpsertFile(&models.MediaFile{ItemID: it.ID, SourceID: f.anime.SourceID, Path: fmt.Sprintf("/alpha-%d.mkv", i)})
		if err != nil {
			t.Fatal(err)
		}
		if err := f.s.Store.SaveProgress(&models.Progress{UserID: 1, FileID: fid, ItemID: it.ID, Position: 100, Duration: 100}); err != nil {
			t.Fatal(err)
		}
	}
	got := f.latest(t, parent+"&IsPlayed=false&IncludeItemTypes=Episode")
	embyLatestWantIDs(t, got, f.b, f.a)
	if got[1]["ChildCount"] != float64(1) {
		t.Fatalf("played episodes counted in unplayed group: %v", got)
	}
	got = f.latest(t, parent+"&IsPlayed=true")
	embyLatestWantIDs(t, got, f.a)
	if got[0]["ChildCount"] != float64(2) {
		t.Fatalf("played count: %v", got)
	}
	embyLatestWantIDs(t, f.latest(t, parent+"&IsPlayed=false&GroupItems=false"), f.be1, f.ae1)
	embyLatestWantIDs(t, f.latest(t, parent+"&IsPlayed=false&Limit=1"), f.b)
	other := &models.User{Name: "other", PassHash: "unused", Role: models.RoleUser, Status: models.StatusActive}
	if err := f.s.Store.CreateUser(other); err != nil {
		t.Fatal(err)
	}
	fid, err := f.s.Store.UpsertFile(&models.MediaFile{ItemID: f.be1.ID, SourceID: f.anime.SourceID, Path: "/beta.mkv"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.Store.SaveProgress(&models.Progress{UserID: other.ID, FileID: fid, ItemID: f.be1.ID, Position: 100, Duration: 100}); err != nil {
		t.Fatal(err)
	}
	embyLatestWantIDs(t, f.latest(t, parent+"&IsPlayed=false&Limit=1"), f.b)
	if err := f.s.Store.SaveProgress(&models.Progress{UserID: 1, FileID: fid, ItemID: f.be1.ID, Position: 50, Duration: 100}); err != nil {
		t.Fatal(err)
	}
	embyLatestWantIDs(t, f.latest(t, parent+"&Filters=IsResumable"), f.b)
	// Both scanner-style season parents and direct show parents must group.
	if _, err := f.s.Store.DB.Exec("UPDATE items SET parent_id=? WHERE id=?", f.b.ID, f.be1.ID); err != nil {
		t.Fatal(err)
	}
	embyLatestWantIDs(t, f.latest(t, parent+"&Filters=IsResumable"), f.b)
}

func TestEmbyLatestVisibilityMovieLibrariesAndOrdinaryLists(t *testing.T) {
	f := newEmbyLatestFixture(t)
	embyLatestWantIDs(t, f.latest(t, ""), f.a, f.film, f.b)
	embyLatestWantIDs(t, f.latest(t, "?GroupItems=false"), f.ae2, f.ae3, f.film, f.be1, f.ae1)
	embyLatestWantIDs(t, f.latest(t, "?ParentId=lib-"+embyID(f.movies.ID)), f.film)
	embyLatestWantIDs(t, f.latest(t, "?ParentId=lib-"+embyID(f.hidden.ID)))
	embyLatestWantIDs(t, f.latest(t, "?IncludeItemTypes=Movie"), f.film)
	if code, _ := embyRequest(t, f.ts.URL, "GET", "/emby/Users/9999/Items/Latest", f.token, nil); code != 403 {
		t.Fatalf("cross-user latest status = %d", code)
	}
	if code, _ := embyRequest(t, f.ts.URL, "GET", "/emby/Users/1/Items/Latest", "", nil); code != 401 {
		t.Fatalf("unauthenticated latest status = %d", code)
	}
	// The latest-only branch must not group normal Items or episode listings.
	for _, tc := range []struct {
		path string
		want []*models.Item
	}{
		{"/emby/Users/1/Items?ParentId=lib-" + embyID(f.anime.ID) + "&GroupItems=true", []*models.Item{f.b, f.a}},
		{"/emby/Users/1/Items?ParentId=lib-" + embyID(f.anime.ID) + "&Recursive=true&IncludeItemTypes=Episode&GroupItems=true", []*models.Item{f.ae2, f.ae3, f.be1, f.ae1}},
		{fmt.Sprintf("/emby/Shows/%d/Episodes?GroupItems=true", f.a.ID), []*models.Item{f.ae1, f.ae2, f.ae3}},
	} {
		code, raw := embyRequest(t, f.ts.URL, "GET", tc.path, f.token, nil)
		if code != 200 {
			t.Fatalf("ordinary listing %d: %s", code, raw)
		}
		var page struct {
			Items            []map[string]any
			TotalRecordCount int
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			t.Fatal(err)
		}
		embyLatestWantIDs(t, page.Items, tc.want...)
		if page.TotalRecordCount != len(tc.want) {
			t.Fatalf("ordinary count changed: %s", raw)
		}
	}
}
