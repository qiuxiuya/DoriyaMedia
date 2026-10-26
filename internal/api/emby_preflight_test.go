package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"doriyamedia/internal/media"
)

func TestEmbyOfficialDetailPreflight(t *testing.T) {
	s, ts := newTestServer(t)
	token := embyLoginTest(t, ts)
	fid := seedProgressFile(t, s.Store)
	f, err := s.Store.FileByID(fid)
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.Store.SourceByID(f.SourceID)
	if err != nil {
		t.Fatal(err)
	}
	src.Type = "unavailable-test-driver"
	if err := s.Store.UpdateSource(src); err != nil {
		t.Fatal(err)
	}
	s.Engine = media.NewEngine(s.Store, s.Cfg)
	t.Cleanup(s.Engine.CloseSessions)
	code, raw := embyRequest(t, ts.URL, "GET", "/emby/System/Endpoint", token, nil)
	if code != 200 {
		t.Fatalf("endpoint: %d %s", code, raw)
	}
	endpoint := embyObject(t, raw)
	if _, ok := endpoint["IsInNetwork"].(bool); !ok {
		t.Fatalf("endpoint contract: %s", raw)
	}
	id := embyID(f.ItemID)
	for _, tc := range []struct {
		path string
		body any
	}{
		{"/emby/Items/" + id + "/PlaybackInfo?UserId=1&IsPlayback=false", map[string]any{"DeviceProfile": map[string]any{}}},
		{"/emby/Items/" + id + "/PlaybackInfo", map[string]any{"IsPlayback": false, "UserId": "1"}},
	} {
		code, raw = embyRequest(t, ts.URL, "POST", tc.path, token, tc.body)
		if code != 200 {
			t.Fatalf("metadata preflight must not call unavailable driver: %d %s", code, raw)
		}
		sources := embyObject(t, raw)["MediaSources"].([]any)
		if len(sources) != 1 || sources[0].(map[string]any)["Id"] != embyID(fid) {
			t.Fatalf("preflight metadata: %s", raw)
		}
	}
	for _, tc := range []struct {
		query  string
		status int
	}{
		{"UserId=2&IsPlayback=false", 403},
		{"IsPlayback=false&MediaSourceId=999999", 404},
		{"IsPlayback=invalid", 400},
		{"IsPlayback=true", 502},
		{"", 502},
	} {
		code, raw = embyRequest(t, ts.URL, "POST", "/emby/Items/"+id+"/PlaybackInfo?"+tc.query, token, map[string]any{"DeviceProfile": map[string]any{}})
		if code != tc.status {
			t.Fatalf("%s: %d %s, want %d", tc.query, code, raw, tc.status)
		}
	}
}

func TestEmbyEndpointIgnoresForwardedHeaders(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/System/Endpoint", nil)
	req.RemoteAddr = "203.0.113.10:1234"
	req.Header.Set("X-Forwarded-For", "127.0.0.1")
	w := httptest.NewRecorder()
	s.embyEndpoint(w, req, nil)
	obj := embyObject(t, w.Body.Bytes())
	if obj["IsLocal"] != false || obj["IsInNetwork"] != false {
		t.Fatalf("spoofed network: %s", w.Body.String())
	}
}
