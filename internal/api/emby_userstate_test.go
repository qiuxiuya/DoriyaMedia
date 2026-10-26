package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"doriyamedia/internal/models"
)

func TestEmbyWatchedAndNextUp(t *testing.T) {
	s, ts := newTestServer(t)
	token := embyLoginTest(t, ts)
	fid := seedProgressFile(t, s.Store)
	f, _ := s.Store.FileByID(fid)
	film, _ := s.Store.ItemByID(f.ItemID)
	probe := `{"streams":[{"index":0,"codec_type":"video","codec_name":"h264"}],"format":{"duration":"100"}}`
	if err := s.Store.UpdateFileProbe(fid, probe); err != nil {
		t.Fatal(err)
	}
	id := embyID(film.ID)
	code, raw := embyRequest(t, ts.URL, "POST", "/Users/1/PlayedItems/"+id, token, nil)
	if code != 200 || embyObject(t, raw)["Played"] != true {
		t.Fatalf("mark watched %d %s", code, raw)
	}
	code, raw = embyRequest(t, ts.URL, "GET", "/Items?Recursive=true&IsPlayed=true", token, nil)
	if code != 200 || embyObject(t, raw)["TotalRecordCount"] != float64(1) {
		t.Fatalf("played filter %d %s", code, raw)
	}
	code, raw = embyRequest(t, ts.URL, "POST", "/Sessions/Playing/Stopped", token, map[string]any{"ItemId": id, "MediaSourceId": embyID(fid)})
	if code != 204 {
		t.Fatalf("missing position %d %s", code, raw)
	}
	p, err := s.Store.ProgressForFile(1, fid)
	if err != nil || p.Position != 100 {
		t.Fatalf("stop reset position: %+v %v", p, err)
	}
	code, raw = embyRequest(t, ts.URL, "DELETE", "/Users/1/PlayedItems/"+id, token, nil)
	if code != 200 || embyObject(t, raw)["Played"] != false {
		t.Fatalf("clear watched %d %s", code, raw)
	}
	lib := &models.Library{Name: "TV", Type: "series", SourceID: f.SourceID, Providers: "[]"}
	if err := s.Store.CreateLibrary(lib); err != nil {
		t.Fatal(err)
	}
	show := &models.Item{LibraryID: lib.ID, Type: "show", Name: "Series"}
	if err := s.Store.InsertItem(show); err != nil {
		t.Fatal(err)
	}
	season := &models.Item{LibraryID: lib.ID, Type: "season", Name: "Season", ParentID: &show.ID, Season: 1}
	if err := s.Store.InsertItem(season); err != nil {
		t.Fatal(err)
	}
	episodeIDs := []int64{}
	fileIDs := []int64{}
	for n := 1; n <= 3; n++ {
		ep := &models.Item{LibraryID: lib.ID, Type: "episode", Name: "Episode", ParentID: &season.ID, Season: 1, Episode: n}
		if err := s.Store.InsertItem(ep); err != nil {
			t.Fatal(err)
		}
		fileID, err := s.Store.UpsertFile(&models.MediaFile{ItemID: ep.ID, SourceID: f.SourceID, Path: "/episode-" + embyID(int64(n)) + ".mp4"})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Store.UpdateFileProbe(fileID, probe); err != nil {
			t.Fatal(err)
		}
		episodeIDs = append(episodeIDs, ep.ID)
		fileIDs = append(fileIDs, fileID)
	}
	checkNext := func(want int64) {
		t.Helper()
		code, raw := embyRequest(t, ts.URL, "GET", "/Shows/NextUp?UserId=1", token, nil)
		if code != 200 {
			t.Fatalf("next up %d %s", code, raw)
		}
		items := embyObject(t, raw)["Items"].([]any)
		if want == 0 {
			if len(items) != 0 {
				t.Fatalf("unexpected next up %s", raw)
			}
			return
		}
		if len(items) != 1 || items[0].(map[string]any)["Id"] != embyID(want) {
			t.Fatalf("next up expected %d: %s", want, raw)
		}
	}
	checkNext(0)
	if err := s.Store.SaveProgress(&models.Progress{UserID: 1, FileID: fileIDs[0], ItemID: episodeIDs[0], Position: 100, Duration: 100}); err != nil {
		t.Fatal(err)
	}
	checkNext(episodeIDs[1])
	code, raw = embyRequest(t, ts.URL, "POST", "/Users/1/PlayedItems/"+embyID(episodeIDs[1]), token, nil)
	if code != 200 {
		t.Fatalf("episode watched %d %s", code, raw)
	}
	checkNext(episodeIDs[2])
	code, raw = embyRequest(t, ts.URL, "GET", "/Users/1/Items/Latest?ParentId=lib-"+embyID(lib.ID)+"&IncludeItemTypes=Episode&GroupItems=false", token, nil)
	var latest []map[string]any
	if code != 200 {
		t.Fatalf("latest %d %s", code, raw)
	}
	if err := jsonUnmarshal(string(raw), &latest); err != nil || len(latest) != 3 {
		t.Fatalf("latest episode list: %s %v", raw, err)
	}
	hidden := &models.Library{Name: "__browse__-test", Type: "movie", SourceID: f.SourceID, Providers: "[]"}
	if err := s.Store.CreateLibrary(hidden); err != nil {
		t.Fatal(err)
	}
	hiddenItem := &models.Item{LibraryID: hidden.ID, Type: "movie", Name: "Internal"}
	if err := s.Store.InsertItem(hiddenItem); err != nil {
		t.Fatal(err)
	}
	code, raw = embyRequest(t, ts.URL, "GET", "/Items?Recursive=true&SearchTerm=Internal", token, nil)
	if code != 200 || embyObject(t, raw)["TotalRecordCount"] != float64(0) {
		t.Fatalf("hidden items %d %s", code, raw)
	}
	code, raw = embyRequest(t, ts.URL, "GET", "/Users/1/Views", token, nil)
	if code != 200 || len(embyObject(t, raw)["Items"].([]any)) != 2 {
		t.Fatalf("hidden view %d %s", code, raw)
	}
	for _, path := range []string{"/Items?UserId=999", "/Shows/NextUp?UserId=999"} {
		code, raw = embyRequest(t, ts.URL, "GET", path, token, nil)
		if code != 403 {
			t.Fatalf("query user isolation %d %s", code, raw)
		}
	}
	code, raw = embyRequest(t, ts.URL, "POST", "/Items/"+id+"/PlaybackInfo", token, map[string]string{"UserId": "999"})
	if code != 403 {
		t.Fatalf("body user isolation %d %s", code, raw)
	}
}

func TestEmbyPreservesFrontendLibraryNavigation(t *testing.T) {
	s, ts := newTestServer(t)
	s.WebFS = fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("native frontend")}}
	for _, path := range []string{"/", "/library/1", "/library/1/", "/item/1", "/settings"} {
		code, raw := embyRequest(t, ts.URL, "GET", path, "", nil)
		if code != 200 || string(raw) != "native frontend" {
			t.Fatalf("frontend %s: %d %s", path, code, raw)
		}
	}
	code, _ := embyRequest(t, ts.URL, "GET", "/Library/MediaFolders", "", nil)
	if code != 401 {
		t.Fatalf("library API not secured: %d", code)
	}
	req := httptest.NewRequest(http.MethodGet, "/EmBy/SyStEm/InFo/PuBlIc/", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("trailing slash %d", rec.Code)
	}
}
