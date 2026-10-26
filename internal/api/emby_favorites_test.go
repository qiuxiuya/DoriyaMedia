package api

import (
	"doriyamedia/internal/auth"
	"doriyamedia/internal/models"
	"testing"
	"time"
)

func TestEmbyFavoritesPersistAndIsolate(t *testing.T) {
	s, ts := newTestServer(t)
	token := embyLoginTest(t, ts)
	fid := seedProgressFile(t, s.Store)
	file, err := s.Store.FileByID(fid)
	if err != nil {
		t.Fatal(err)
	}
	id := embyID(file.ItemID)
	if err := s.Store.SaveProgress(&models.Progress{UserID: 1, FileID: fid, ItemID: file.ItemID, Position: 25, Duration: 100}); err != nil {
		t.Fatal(err)
	}
	hash, salt := auth.HashPassword("viewer-pass")
	u := &models.User{Name: "favorite-viewer", PassHash: hash, Salt: salt, Status: models.StatusActive}
	if err := s.Store.CreateUser(u); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.CreateSession("favorite-viewer-session", u.ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	for _, prefix := range []string{"", "/EmBy", "/MediaBrowser"} {
		path := prefix + "/Users/1/FavoriteItems/" + id
		if code, _ := embyRequest(t, ts.URL, "POST", path, "", nil); code != 401 {
			t.Fatalf("anonymous %d", code)
		}
		if code, _ := embyRequest(t, ts.URL, "POST", path, "favorite-viewer-session", nil); code != 403 {
			t.Fatalf("cross-user %d", code)
		}
		if code, _ := embyRequest(t, ts.URL, "POST", path+"?UserId=999", token, nil); code != 403 {
			t.Fatalf("cross-user query %d", code)
		}
		for n := 0; n < 2; n++ {
			code, raw := embyRequest(t, ts.URL, "POST", path, token, nil)
			obj := embyObject(t, raw)
			if code != 200 || obj["IsFavorite"] != true || obj["PlaybackPositionTicks"] != float64(25*embyTicksPerSecond) {
				t.Fatalf("favorite %d %s", code, raw)
			}
		}
		var count int
		if err := s.Store.DB.QueryRow("SELECT COUNT(*) FROM emby_favorites WHERE user_id=1 AND item_id=?", file.ItemID).Scan(&count); err != nil || count != 1 {
			t.Fatalf("not persisted: %d %v", count, err)
		}
		code, raw := embyRequest(t, ts.URL, "GET", prefix+"/Users/1/Items/"+id, token, nil)
		if code != 200 || embyObject(t, raw)["UserData"].(map[string]any)["IsFavorite"] != true {
			t.Fatalf("detail %d %s", code, raw)
		}
		for _, filter := range []string{"IsFavorite=true", "Filters=IsFavorite", "Filters=IsFavoriteOrLikes"} {
			code, raw = embyRequest(t, ts.URL, "GET", prefix+"/Users/1/Items?Recursive=true&"+filter, token, nil)
			if code != 200 || embyObject(t, raw)["TotalRecordCount"] != float64(1) {
				t.Fatalf("filter %s %d %s", filter, code, raw)
			}
		}
		code, raw = embyRequest(t, ts.URL, "GET", prefix+"/Items?Recursive=true&IsFavorite=true", "favorite-viewer-session", nil)
		if code != 200 || embyObject(t, raw)["TotalRecordCount"] != float64(0) {
			t.Fatalf("favorite leaked %d %s", code, raw)
		}
		for _, method := range []string{"DELETE", "POST"} {
			target := path
			if method == "POST" {
				target += "/Delete"
				if code, _ := embyRequest(t, ts.URL, "POST", path, token, nil); code != 200 {
					t.Fatal(code)
				}
			}
			for n := 0; n < 2; n++ {
				code, raw = embyRequest(t, ts.URL, method, target, token, nil)
				if code != 200 || embyObject(t, raw)["IsFavorite"] != false {
					t.Fatalf("unfavorite %d %s", code, raw)
				}
			}
		}
		p, err := s.Store.ProgressForFile(1, fid)
		if err != nil || p.Position != 25 {
			t.Fatalf("favorite changed progress: %+v %v", p, err)
		}
	}
	if code, _ := embyRequest(t, ts.URL, "POST", "/Users/1/FavoriteItems/999999", token, nil); code != 404 {
		t.Fatal(code)
	}
	// Folders can be favorited without media files.
	item, _ := s.Store.ItemByID(file.ItemID)
	show := &models.Item{LibraryID: item.LibraryID, Type: "show", Name: "Favorite show"}
	if err := s.Store.InsertItem(show); err != nil {
		t.Fatal(err)
	}
	code, raw := embyRequest(t, ts.URL, "POST", "/Users/1/FavoriteItems/"+embyID(show.ID), token, nil)
	if code != 200 || embyObject(t, raw)["IsFavorite"] != true {
		t.Fatalf("show %d %s", code, raw)
	}
}
