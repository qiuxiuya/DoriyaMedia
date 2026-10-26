package api

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestEmbyOptionalScriptsAndLiveTV(t *testing.T) {
	_, ts := newTestServer(t)
	token := embyLoginTest(t, ts)
	for _, prefix := range []string{"", "/EmBy", "/MediaBrowser"} {
		for _, method := range []string{"GET", "HEAD"} {
			req, err := http.NewRequest(method, ts.URL+prefix+"/CustomCssJS/Scripts", nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/javascript") || len(raw) != 0 {
				t.Fatalf("scripts %s %s: %d %v %s", method, prefix, resp.StatusCode, resp.Header, raw)
			}
		}
		code, raw := embyRequest(t, ts.URL, "GET", prefix+"/LiveTv/Channels?UserId=1&StartIndex=5", token, nil)
		if code != 200 {
			t.Fatalf("channels %s: %d %s", prefix, code, raw)
		}
		obj := embyObject(t, raw)
		items, ok := obj["Items"].([]any)
		if !ok || len(items) != 0 || obj["TotalRecordCount"] != float64(0) || obj["StartIndex"] != float64(5) {
			t.Fatalf("channels contract: %s", raw)
		}
		code, raw = embyRequest(t, ts.URL, "GET", prefix+"/LiveTv/Info", token, nil)
		if code != 200 || embyObject(t, raw)["IsEnabled"] != false {
			t.Fatalf("live info: %d %s", code, raw)
		}
		if code, _ := embyRequest(t, ts.URL, "GET", prefix+"/LiveTv/Channels", "", nil); code != 401 {
			t.Fatalf("anonymous channels: %d", code)
		}
		if code, _ := embyRequest(t, ts.URL, "GET", prefix+"/LiveTv/Channels?UserId=999", token, nil); code != 403 {
			t.Fatalf("cross user channels: %d", code)
		}
	}
	code, raw := embyRequest(t, ts.URL, "GET", "/Users/Me", token, nil)
	if code != 200 {
		t.Fatalf("user %d %s", code, raw)
	}
	policy := embyObject(t, raw)["Policy"].(map[string]any)
	if policy["EnableLiveTvAccess"] != false || policy["EnableLiveTvManagement"] != false {
		t.Fatalf("false capability: %s", raw)
	}
	// Do not turn unknown plugin operations into generic successful responses.
	if code, _ := embyRequest(t, ts.URL, "GET", "/emby/CustomCssJS/Unknown", "", nil); code != 404 {
		t.Fatalf("unknown plugin: %d", code)
	}
}
