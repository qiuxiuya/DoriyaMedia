package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"doriyamedia/internal/auth"
	"doriyamedia/internal/media"
	"doriyamedia/internal/models"
)

func embyRequest(t *testing.T, base, method, path, token string, body any) (int, []byte) {
	t.Helper()
	var input io.Reader
	if body != nil {
		b, e := json.Marshal(body)
		if e != nil {
			t.Fatal(e)
		}
		input = bytes.NewReader(b)
	}
	req, e := http.NewRequest(method, base+path, input)
	if e != nil {
		t.Fatal(e)
	}
	if token != "" {
		req.Header.Set("X-Emby-Token", token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer resp.Body.Close()
	raw, e := io.ReadAll(resp.Body)
	if e != nil {
		t.Fatal(e)
	}
	return resp.StatusCode, raw
}
func embyObject(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if e := json.Unmarshal(raw, &out); e != nil {
		t.Fatalf("JSON: %v: %s", e, raw)
	}
	return out
}
func embyLoginTest(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	setupAdmin(t, ts.URL)
	code, raw := embyRequest(t, ts.URL, "POST", "/emby/Users/AuthenticateByName", "", map[string]string{"Username": "admin", "Pw": "admin-pass"})
	if code != 200 {
		t.Fatalf("login %d: %s", code, raw)
	}
	return embyObject(t, raw)["AccessToken"].(string)
}
func TestEmbyDiscoveryAuthenticationAndRevocation(t *testing.T) {
	s, ts := newTestServer(t)
	var serverID string
	for _, prefix := range []string{"", "/emby", "/EmBy", "/mediabrowser"} {
		code, raw := embyRequest(t, ts.URL, "GET", prefix+"/sYsTeM/InFo/PuBlIc", "", nil)
		if code != 200 {
			t.Fatalf("discovery %d: %s", code, raw)
		}
		id := embyObject(t, raw)["Id"].(string)
		if serverID != "" && serverID != id {
			t.Fatal("server ID changed across prefixes")
		}
		serverID = id
	}
	token := embyLoginTest(t, ts)
	for _, headers := range []map[string]string{{"X-Emby-Token": token}, {"X-MediaBrowser-Token": token}, {"Authorization": "Bearer " + token}, {"Authorization": `MediaBrowser Client="Test", Token="` + token + `"`}, {"X-Emby-Authorization": `Emby Token="` + token + `"`}} {
		req, _ := http.NewRequest("GET", ts.URL+"/Users/Me", nil)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || embyObject(t, raw)["Name"] != "admin" {
			t.Fatalf("header auth %v: %d %s", headers, resp.StatusCode, raw)
		}
	}
	for _, key := range []string{"api_key", "X-Emby-Token"} {
		code, raw := embyRequest(t, ts.URL, "GET", "/emby/Users/Me?"+key+"="+token, "", nil)
		if code != 200 {
			t.Fatalf("query auth %d %s", code, raw)
		}
	}
	if code, _ := embyRequest(t, ts.URL, "GET", "/emby/Items", "invalid", nil); code != 401 {
		t.Fatalf("bad token: %d", code)
	}
	if code, _ := embyRequest(t, ts.URL, "GET", "/emby/Users/9999/Items", token, nil); code != 403 {
		t.Fatalf("cross-user: %d", code)
	}
	if code, raw := embyRequest(t, ts.URL, "POST", "/emby/Users/AuthenticateByName", "", map[string]string{"Username": "admin", "Pw": "wrong"}); code != 401 {
		t.Fatalf("wrong password %d %s", code, raw)
	}
	if code, _ := embyRequest(t, ts.URL, "POST", "/emby/Sessions/Logout", token, nil); code != 204 {
		t.Fatalf("logout: %d", code)
	}
	if code, _ := embyRequest(t, ts.URL, "GET", "/emby/Users/Me", token, nil); code != 401 {
		t.Fatalf("revoked token: %d", code)
	}
	if err := s.Store.CreateSession("expired", 1, time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if code, _ := embyRequest(t, ts.URL, "GET", "/emby/Users/Me", "expired", nil); code != 401 {
		t.Fatalf("expired token: %d", code)
	}
}
func TestEmbyAccountPolicy(t *testing.T) {
	s, ts := newTestServer(t)
	token := embyLoginTest(t, ts)
	u, _ := s.Store.UserByName("admin")
	if err := s.Store.UpdateUserStatus(u.ID, models.StatusDisabled); err != nil {
		t.Fatal(err)
	}
	if code, _ := embyRequest(t, ts.URL, "GET", "/emby/Users/Me", token, nil); code != 401 {
		t.Fatalf("disabled token %d", code)
	}
	if code, _ := embyRequest(t, ts.URL, "POST", "/emby/Users/AuthenticateByName", "", map[string]string{"Username": "admin", "Pw": "admin-pass"}); code != 403 {
		t.Fatalf("disabled login %d", code)
	}
	if err := s.Store.UpdateUserStatus(u.ID, models.StatusActive); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 11; i++ {
		embyRequest(t, ts.URL, "POST", "/Users/AuthenticateByName", "", map[string]string{"Username": "admin", "Pw": "bad"})
	}
	if code, _ := embyRequest(t, ts.URL, "POST", "/Users/AuthenticateByName", "", map[string]string{"Username": "admin", "Pw": "admin-pass"}); code != 429 {
		t.Fatalf("limiter %d", code)
	}
}
func TestEmbyLibraryPlaybackAndProgress(t *testing.T) {
	s, ts := newTestServer(t)
	token := embyLoginTest(t, ts)
	fileID := seedProgressFile(t, s.Store)
	f, _ := s.Store.FileByID(fileID)
	it, _ := s.Store.ItemByID(f.ItemID)
	probe := `{"streams":[{"index":0,"codec_type":"video","codec_name":"h264","width":1920,"height":1080},{"index":1,"codec_type":"audio","codec_name":"aac","channels":2}],"format":{"duration":"100","format_name":"matroska"}}`
	if e := s.Store.UpdateFileProbe(fileID, probe); e != nil {
		t.Fatal(e)
	}
	root := t.TempDir()
	if e := os.WriteFile(filepath.Join(root, "film.mkv"), []byte("0123456789"), 0600); e != nil {
		t.Fatal(e)
	}
	cfg, _ := json.Marshal(models.SourceConfig{Root: root})
	src, _ := s.Store.SourceByID(f.SourceID)
	src.Config = string(cfg)
	if e := s.Store.UpdateSource(src); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(root, "film.en.srt"), []byte("1\n00:00:01,000 --> 00:00:02,000\nHello\n"), 0600); e != nil {
		t.Fatal(e)
	}
	sub := &models.Subtitle{FileID: fileID, Path: "/film.en.srt", Lang: "eng", Format: "subrip", Convertible: true}
	if e := s.Store.UpsertSubtitle(sub); e != nil {
		t.Fatal(e)
	}
	s.Engine = media.NewEngine(s.Store, s.Cfg)
	t.Cleanup(s.Engine.CloseSessions)
	id := embyID(it.ID)
	fid := embyID(fileID)
	code, raw := embyRequest(t, ts.URL, "GET", "/emby/Users/1/Views", token, nil)
	if code != 200 {
		t.Fatalf("views %d %s", code, raw)
	}
	views := embyObject(t, raw)["Items"].([]any)
	if len(views) != 1 {
		t.Fatalf("views %v", views)
	}
	libID := views[0].(map[string]any)["Id"].(string)
	code, raw = embyRequest(t, ts.URL, "GET", "/emby/Users/1/Items?ParentId="+libID+"&Limit=1", token, nil)
	if code != 200 {
		t.Fatalf("items %d %s", code, raw)
	}
	page := embyObject(t, raw)
	if page["TotalRecordCount"] != float64(1) || len(page["Items"].([]any)) != 1 {
		t.Fatalf("items %s", raw)
	}
	for _, path := range []string{"/Items/" + id, "/Users/1/Items/" + id} {
		code, raw = embyRequest(t, ts.URL, "GET", path, token, nil)
		if code != 200 || embyObject(t, raw)["Type"] != "Movie" {
			t.Fatalf("detail %d %s", code, raw)
		}
	}
	code, raw = embyRequest(t, ts.URL, "POST", "/emby/Items/"+id+"/PlaybackInfo", token, map[string]string{"MediaSourceId": fid, "UserId": "1"})
	if code != 200 {
		t.Fatalf("playback %d %s", code, raw)
	}
	sources := embyObject(t, raw)["MediaSources"].([]any)
	ms := sources[0].(map[string]any)
	if ms["RunTimeTicks"] != float64(100*embyTicksPerSecond) || ms["Id"] != fid || ms["SupportsTranscoding"] != false {
		t.Fatalf("media source %v", ms)
	}
	streamURL := ms["DirectStreamUrl"].(string)
	req, _ := http.NewRequest("GET", ts.URL+streamURL, nil)
	req.Header.Set("Range", "bytes=2-5")
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 206 || string(data) != "2345" {
		t.Fatalf("range %d %s", resp.StatusCode, data)
	}
	req, _ = http.NewRequest("HEAD", ts.URL+streamURL, nil)
	resp, e = http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("HEAD %d", resp.StatusCode)
	}
	var subURL string
	for _, v := range ms["MediaStreams"].([]any) {
		st := v.(map[string]any)
		if st["Type"] == "Subtitle" {
			subURL, _ = st["DeliveryUrl"].(string)
		}
	}
	if subURL == "" {
		t.Fatal("missing subtitle URL")
	}
	code, raw = embyRequest(t, ts.URL, "GET", subURL, "", nil)
	if code != 200 || !bytes.Contains(raw, []byte("WEBVTT")) || !bytes.Contains(raw, []byte("Hello")) {
		t.Fatalf("subtitle %d %s", code, raw)
	}
	body := map[string]any{"ItemId": id, "MediaSourceId": fid, "PositionTicks": 40 * embyTicksPerSecond}
	for _, p := range []string{"/Sessions/Playing", "/Sessions/Playing/Progress", "/Sessions/Playing/Stopped"} {
		code, raw = embyRequest(t, ts.URL, "POST", p, token, body)
		if code != 204 || len(raw) != 0 {
			t.Fatalf("progress %d %s", code, raw)
		}
	}
	progress, e := s.Store.ProgressForFile(1, fileID)
	if e != nil || progress.Position != 40 || progress.Duration != 100 {
		t.Fatalf("progress %+v %v", progress, e)
	}
	code, raw = embyRequest(t, ts.URL, "GET", "/Users/1/Items/Resume", token, nil)
	if code != 200 || embyObject(t, raw)["TotalRecordCount"] != float64(1) {
		t.Fatalf("resume %d %s", code, raw)
	}
	hash, salt := auth.HashPassword("viewer-pass")
	other := &models.User{Name: "viewer", PassHash: hash, Salt: salt, Status: models.StatusActive}
	if e := s.Store.CreateUser(other); e != nil {
		t.Fatal(e)
	}
	if e := s.Store.CreateSession("viewer-token", other.ID, time.Now().Add(time.Hour)); e != nil {
		t.Fatal(e)
	}
	code, raw = embyRequest(t, ts.URL, "GET", "/Users/"+embyID(other.ID)+"/Items/Resume", "viewer-token", nil)
	if code != 200 || embyObject(t, raw)["TotalRecordCount"] != float64(0) {
		t.Fatalf("isolated resume %d %s", code, raw)
	}
	for _, pos := range []int64{-1, 101*embyTicksPerSecond + 1} {
		body["PositionTicks"] = pos
		code, raw = embyRequest(t, ts.URL, "POST", "/Sessions/Playing/Progress", token, body)
		if code != 400 {
			t.Fatalf("invalid progress %d %s", code, raw)
		}
	}
	otherItem := &models.Item{Name: "Other film", Type: "movie", LibraryID: it.LibraryID}
	if e := s.Store.InsertItem(otherItem); e != nil {
		t.Fatal(e)
	}
	code, raw = embyRequest(t, ts.URL, "GET", "/Videos/"+embyID(otherItem.ID)+"/stream?MediaSourceId="+fid, token, nil)
	if code != 404 {
		t.Fatalf("cross-item source %d %s", code, raw)
	}
	code, raw = embyRequest(t, ts.URL, "GET", "/Items?Recursive=true&SearchTerm="+url.QueryEscape("' OR 1=1 --"), token, nil)
	if code != 200 || embyObject(t, raw)["TotalRecordCount"] != float64(0) {
		t.Fatalf("search binding %d %s", code, raw)
	}
	code, raw = embyRequest(t, ts.URL, "GET", "/api/bootstrap", "", nil)
	if code != 200 || !strings.Contains(string(raw), "setup_needed") {
		t.Fatalf("native routes %d %s", code, raw)
	}
}
func TestEmbySeriesHierarchy(t *testing.T) {
	s, ts := newTestServer(t)
	token := embyLoginTest(t, ts)
	fid := seedProgressFile(t, s.Store)
	file, _ := s.Store.FileByID(fid)
	film, _ := s.Store.ItemByID(file.ItemID)
	lib := &models.Library{Name: "TV", Type: "series", SourceID: file.SourceID, Providers: "[]"}
	if e := s.Store.CreateLibrary(lib); e != nil {
		t.Fatal(e)
	}
	show := &models.Item{LibraryID: lib.ID, Type: "show", Name: "Test Show"}
	if e := s.Store.InsertItem(show); e != nil {
		t.Fatal(e)
	}
	season := &models.Item{LibraryID: lib.ID, ParentID: &show.ID, Type: "season", Name: "Season 1", Season: 1}
	if e := s.Store.InsertItem(season); e != nil {
		t.Fatal(e)
	}
	for n := 1; n <= 3; n++ {
		ep := &models.Item{LibraryID: lib.ID, ParentID: &season.ID, Type: "episode", Name: "Episode", Season: 1, Episode: n}
		if e := s.Store.InsertItem(ep); e != nil {
			t.Fatal(e)
		}
	}
	for _, p := range []string{"/Shows/" + embyID(show.ID) + "/Seasons", "/Shows/" + embyID(show.ID) + "/Episodes?SeasonId=" + embyID(season.ID), "/Items?ParentId=lib-" + embyID(film.LibraryID) + "&Limit=0"} {
		code, raw := embyRequest(t, ts.URL, "GET", p, token, nil)
		if code != 200 {
			t.Fatalf("hierarchy %d %s", code, raw)
		}
		out := embyObject(t, raw)
		items := out["Items"].([]any)
		if strings.Contains(p, "/Episodes") {
			if len(items) != 3 {
				t.Fatalf("episodes %s", raw)
			}
			ep := items[0].(map[string]any)
			if ep["SeriesId"] != embyID(show.ID) || ep["SeasonId"] != embyID(season.ID) || ep["IndexNumber"] != float64(1) {
				t.Fatalf("hierarchy fields %v", ep)
			}
		} else if strings.Contains(p, "Limit=0") {
			if len(items) != 0 || out["TotalRecordCount"] != float64(1) {
				t.Fatalf("count-only %s", raw)
			}
		} else if len(items) != 1 {
			t.Fatalf("seasons %s", raw)
		}
	}
}
