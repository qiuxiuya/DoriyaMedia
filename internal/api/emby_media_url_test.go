package api

import (
	"crypto/tls"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestEmbyHTTPMediaSourcePath(t *testing.T) {
	s, ts := newTestServer(t)
	token := embyLoginTest(t, ts)
	fid := seedProgressFile(t, s.Store)
	f, err := s.Store.FileByID(fid)
	if err != nil {
		t.Fatal(err)
	}
	code, raw := embyRequest(t, ts.URL, "GET", "/Users/1/Items/"+embyID(f.ItemID), token, nil)
	if code != 200 {
		t.Fatalf("detail %d %s", code, raw)
	}
	source := embyObject(t, raw)["MediaSources"].([]any)[0].(map[string]any)
	full, err := url.Parse(source["Path"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if full.Scheme != "http" || full.Host == "" || !strings.HasPrefix(full.String(), ts.URL+"/emby/Videos/") || full.Query().Get("api_key") != token {
		t.Fatalf("HTTP Path not absolute/authenticated: %s", full.Redacted())
	}
	if direct := source["DirectStreamUrl"].(string); !strings.HasPrefix(direct, "/emby/Videos/") {
		t.Fatalf("relative DirectStreamUrl changed: %s", direct)
	}
	req := httptest.NewRequest("GET", "http://internal:5666/Items/1", nil)
	req.TLS = &tls.ConnectionState{}
	if got := s.embyAbsoluteMediaURL(req, "/emby/Videos/1/stream.mkv"); got != "https://internal:5666/emby/Videos/1/stream.mkv" {
		t.Fatalf("tls: %s", got)
	}
	s.Cfg.Users.BaseURL = "https://media.example/"
	if got := s.embyAbsoluteMediaURL(req, "/emby/Videos/1/stream.mkv"); got != "https://media.example/emby/Videos/1/stream.mkv" {
		t.Fatalf("public origin: %s", got)
	}
	if got := s.embyAbsoluteMediaURL(req, "https://cdn.example/video.m3u8"); got != "https://cdn.example/video.m3u8" {
		t.Fatalf("absolute remux: %s", got)
	}
}
