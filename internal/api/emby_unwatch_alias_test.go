package api

import (
	"doriyamedia/internal/models"
	"testing"
)

func TestEmbyPostDeletePlayedAlias(t *testing.T) {
	s, ts := newTestServer(t)
	token := embyLoginTest(t, ts)
	fid := seedProgressFile(t, s.Store)
	f, err := s.Store.FileByID(fid)
	if err != nil {
		t.Fatal(err)
	}
	id := embyID(f.ItemID)
	for _, prefix := range []string{"", "/EmBy", "/MediaBrowser"} {
		if err := s.Store.SaveProgress(&models.Progress{UserID: 1, FileID: fid, ItemID: f.ItemID, Position: 100, Duration: 100}); err != nil {
			t.Fatal(err)
		}
		path := prefix + "/Users/1/PlayedItems/" + id + "/Delete"
		if code, _ := embyRequest(t, ts.URL, "POST", path, "", nil); code != 401 {
			t.Fatalf("anonymous: %d", code)
		}
		if code, _ := embyRequest(t, ts.URL, "POST", path+"?UserId=999", token, nil); code != 403 {
			t.Fatalf("cross-user query: %d", code)
		}
		if code, _ := embyRequest(t, ts.URL, "POST", prefix+"/Users/999/PlayedItems/"+id+"/Delete", token, nil); code != 403 {
			t.Fatalf("cross-user path: %d", code)
		}
		p, err := s.Store.ProgressForFile(1, fid)
		if err != nil || p.Position != 100 {
			t.Fatalf("unauthorized mutation: %+v %v", p, err)
		}
		for i := 0; i < 2; i++ {
			code, raw := embyRequest(t, ts.URL, "POST", path, token, nil)
			if code != 200 || embyObject(t, raw)["Played"] != false || embyObject(t, raw)["PlaybackPositionTicks"] != float64(0) {
				t.Fatalf("unwatch: %d %s", code, raw)
			}
		}
		if _, err := s.Store.ProgressForFile(1, fid); err == nil {
			t.Fatal("progress was not deleted")
		}
	}
	// A plain POST without /Delete still marks watched, not unwatched.
	if err := s.Store.UpdateFileProbe(fid, `{"format":{"duration":"100"}}`); err != nil {
		t.Fatal(err)
	}
	code, raw := embyRequest(t, ts.URL, "POST", "/Users/1/PlayedItems/"+id, token, nil)
	if code != 200 || embyObject(t, raw)["Played"] != true {
		t.Fatalf("plain POST changed behavior: %d %s", code, raw)
	}
	if code, _ := embyRequest(t, ts.URL, "POST", "/Users/1/PlayedItems/999999/Delete", token, nil); code != 404 {
		t.Fatalf("missing item: %d", code)
	}
}
