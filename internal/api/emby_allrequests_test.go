package api

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEmbyLogsEveryRequest(t *testing.T) {
	s, _ := newTestServer(t)
	var logs bytes.Buffer
	old := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(old)
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/emby/System/Info/Public?IsPlayback=false&MediaSourceId=42&api_key=secretquery&Tag=secrettag&Password=secretpassword&RedirectUrl=https://private.example", 200},
		{"HEAD", "/emby/CustomCssJS/Scripts", 200},
		{"OPTIONS", "/emby/Items/1/PlaybackInfo", 204},
		{"GET", "/emby/Unknown/Endpoint", 404},
	} {
		logs.Reset()
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.Header.Set("Origin", "https://app.emby.media")
		req.Header.Set("Authorization", "Bearer secretheader")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Fatalf("response %d want %d: %s", w.Code, tc.status, w.Body.String())
		}
		output := logs.String()
		if strings.Count(output, "phase=start") != 1 || strings.Count(output, "phase=end") != 1 || !strings.Contains(output, "request=") {
			t.Fatalf("missing start/end: %s", output)
		}
		for _, secret := range []string{"secretquery", "secrettag", "secretpassword", "secretheader", "private.example", "api_key"} {
			if strings.Contains(output, secret) {
				t.Fatalf("leaked %s: %s", secret, output)
			}
		}
		if strings.Contains(tc.path, "IsPlayback") && (!strings.Contains(output, "isplayback=false") || !strings.Contains(output, "mediasourceid=42")) {
			t.Fatalf("missing useful options: %s", output)
		}
	}
}

func TestEmbyLogsBeforeDispatch(t *testing.T) {
	var logs bytes.Buffer
	old := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(old)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(logs.String(), "phase=start") || strings.Contains(logs.String(), "phase=end") {
			t.Fatalf("start not emitted before handler: %s", logs.String())
		}
		w.WriteHeader(204)
	})
	embyServeLogged(h, httptest.NewRecorder(), httptest.NewRequest("POST", "/items/1/playbackinfo", nil))
	if !strings.Contains(logs.String(), "status=204") {
		t.Fatalf("missing completion: %s", logs.String())
	}
}
