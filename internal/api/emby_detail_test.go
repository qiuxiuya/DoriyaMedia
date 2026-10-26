package api

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"doriyamedia/internal/models"
)

// Exercise the actual compatibility middleware, including prefix/case folding,
// rather than invoking the detail handlers directly. A deadline makes cursor
// regressions fail instead of leaving the suite hanging.
func embyDetailGet(t *testing.T, base, path, token string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, base+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Emby-Token", token)
	resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, raw
}

func TestEmbyDetailAncestors(t *testing.T) {
	s, ts := newTestServer(t)
	token := embyLoginTest(t, ts)
	fid := seedProgressFile(t, s.Store)
	f, err := s.Store.FileByID(fid)
	if err != nil {
		t.Fatal(err)
	}
	film, err := s.Store.ItemByID(f.ItemID)
	if err != nil {
		t.Fatal(err)
	}
	show := &models.Item{LibraryID: film.LibraryID, Type: "show", Name: "Series"}
	if err := s.Store.InsertItem(show); err != nil {
		t.Fatal(err)
	}
	season := &models.Item{LibraryID: film.LibraryID, ParentID: &show.ID, Type: "season", Name: "Season 1", Season: 1}
	if err := s.Store.InsertItem(season); err != nil {
		t.Fatal(err)
	}
	ep := &models.Item{LibraryID: film.LibraryID, ParentID: &season.ID, Type: "episode", Name: "Episode 1", Season: 1, Episode: 1}
	if err := s.Store.InsertItem(ep); err != nil {
		t.Fatal(err)
	}
	// All query results must be closed before serialization issues more queries.
	s.Store.DB.SetMaxOpenConns(1)
	for _, prefix := range []string{"", "/EmBy", "/MediaBrowser"} {
		for _, tc := range []struct {
			id   string
			want []string
		}{
			{embyID(ep.ID), []string{embyID(season.ID), embyID(show.ID), "lib-" + embyID(film.LibraryID)}},
			{embyID(film.ID), []string{"lib-" + embyID(film.LibraryID)}},
			{"lib-" + embyID(film.LibraryID), []string{}},
		} {
			path := prefix + "/Items/" + tc.id + "/Ancestors?UserId=1"
			code, raw := embyDetailGet(t, ts.URL, path, token)
			if code != http.StatusOK {
				t.Fatalf("%s: %d %s", path, code, raw)
			}
			var ancestors []map[string]any
			if err := json.Unmarshal(raw, &ancestors); err != nil {
				t.Fatal(err)
			}
			if ancestors == nil || len(ancestors) != len(tc.want) {
				t.Fatalf("%s: ancestors %s, want %v", path, raw, tc.want)
			}
			for i, id := range tc.want {
				if ancestors[i]["Id"] != id {
					t.Fatalf("%s: ancestor %d = %v, want %s", path, i, ancestors[i]["Id"], id)
				}
			}
		}
	}
	for _, tc := range []struct {
		path, token string
		status      int
	}{
		{"/emby/Items/999999/Ancestors", token, 404},
		{"/emby/Items/" + embyID(film.ID) + "/Ancestors", "", 401},
		{"/emby/Items/" + embyID(film.ID) + "/Ancestors?UserId=999", token, 403},
	} {
		if code, raw := embyDetailGet(t, ts.URL, tc.path, tc.token); code != tc.status {
			t.Fatalf("%s: %d %s, want %d", tc.path, code, raw, tc.status)
		}
	}
	// Corrupt parent data must terminate with a JSON error, not loop forever.
	if _, err := s.Store.DB.Exec("UPDATE items SET parent_id=? WHERE id=?", ep.ID, show.ID); err != nil {
		t.Fatal(err)
	}
	code, raw := embyDetailGet(t, ts.URL, "/emby/Items/"+embyID(ep.ID)+"/Ancestors", token)
	if code != http.StatusInternalServerError || !json.Valid(raw) {
		t.Fatalf("cyclic hierarchy: %d %s", code, raw)
	}
}

func TestEmbyDetailSupplementalRequests(t *testing.T) {
	s, ts := newTestServer(t)
	token := embyLoginTest(t, ts)
	fid := seedProgressFile(t, s.Store)
	f, err := s.Store.FileByID(fid)
	if err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"ThemeSongs", "ThemeVideos", "CriticReviews"} {
		path := "/emby/Items/" + embyID(f.ItemID) + "/" + endpoint
		code, raw := embyDetailGet(t, ts.URL, path, token)
		if code != http.StatusOK {
			t.Errorf("%s: %d %s", path, code, raw)
			continue
		}
		var result struct {
			Items            []any
			TotalRecordCount int
		}
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatal(err)
		}
		if result.Items == nil || len(result.Items) != 0 || result.TotalRecordCount != 0 {
			t.Fatalf("%s: %s", path, raw)
		}
	}
}

func TestEmbyDetailSerializationSingleConnection(t *testing.T) {
	s, ts := newTestServer(t)
	token := embyLoginTest(t, ts)
	fid := seedProgressFile(t, s.Store)
	f, err := s.Store.FileByID(fid)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Store.SaveProgress(&models.Progress{UserID: 1, FileID: fid, ItemID: f.ItemID, Position: 25, Duration: 100}); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.UpsertSubtitle(&models.Subtitle{FileID: fid, Path: "/film.en.srt", Lang: "eng", Format: "subrip", Convertible: true}); err != nil {
		t.Fatal(err)
	}
	s.Store.DB.SetMaxOpenConns(1)
	for _, path := range []string{"/emby/Items/" + embyID(f.ItemID), "/emby/Users/1/Items/" + embyID(f.ItemID), "/emby/Items?Recursive=true"} {
		code, raw := embyDetailGet(t, ts.URL, path, token)
		if code != http.StatusOK || !json.Valid(raw) {
			t.Fatalf("%s: %d %s", path, code, raw)
		}
		item := embyObject(t, raw)
		if items, ok := item["Items"].([]any); ok {
			if len(items) != 1 {
				t.Fatalf("%s: %s", path, raw)
			}
			item = items[0].(map[string]any)
		}
		if item["Id"] != embyID(f.ItemID) || item["MediaSourceCount"] != float64(1) {
			t.Fatalf("detail payload %s: %s", path, raw)
		}
		state, ok := item["UserData"].(map[string]any)
		if !ok || state["PlaybackPositionTicks"] != float64(25*embyTicksPerSecond) {
			t.Fatalf("detail progress %s: %s", path, raw)
		}
		streams, ok := item["MediaStreams"].([]any)
		if !ok || len(streams) == 0 {
			t.Fatalf("detail subtitles %s: %s", path, raw)
		}
	}
}
