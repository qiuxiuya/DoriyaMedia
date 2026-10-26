package api

import (
	"encoding/json"
	"testing"
)

func TestEmbyUserScopedDetailCollections(t *testing.T) {
	s, ts := newTestServer(t)
	token := embyLoginTest(t, ts)
	fid := seedProgressFile(t, s.Store)
	f, err := s.Store.FileByID(fid)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"SpecialFeatures", "LocalTrailers", "Intros"} {
		path := "/emby/Users/1/Items/" + embyID(f.ItemID) + "/" + name
		code, raw := embyRequest(t, ts.URL, "GET", path, token, nil)
		if code != 200 {
			t.Fatalf("%s: %d %s", path, code, raw)
		}
		if name != "Intros" {
			var items []any
			if err := json.Unmarshal(raw, &items); err != nil || items == nil {
				t.Fatalf("expected array %s: %s", name, raw)
			}
		} else if embyObject(t, raw)["TotalRecordCount"] != float64(0) {
			t.Fatalf("intros: %s", raw)
		}
		if code, _ := embyRequest(t, ts.URL, "GET", path, "", nil); code != 401 {
			t.Fatalf("anonymous %s: %d", name, code)
		}
		path = "/emby/Users/999/Items/" + embyID(f.ItemID) + "/" + name
		if code, _ := embyRequest(t, ts.URL, "GET", path, token, nil); code != 403 {
			t.Fatalf("cross user %s: %d", name, code)
		}
	}
}
