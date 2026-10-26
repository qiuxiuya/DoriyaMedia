package api

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEmbyUnmatchedDiagnostics(t *testing.T) {
	s, _ := newTestServer(t)
	var logs bytes.Buffer
	old := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(old)
	req := httptest.NewRequest("GET", "/emby/Items/secret-item/Images/Primary/0/private-tag/unknown?api_key=private-token", nil)
	req.Header.Set("Authorization", "Bearer private-header")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != 404 {
		t.Fatalf("expected missing route: %d", w.Code)
	}
	output := logs.String()
	if !strings.Contains(output, "/items/{redacted}/images/primary/") || !strings.Contains(output, "status=404") {
		t.Fatalf("no diagnostic path: %s", output)
	}
	for _, secret := range []string{"secret-item", "private-tag", "private-token", "private-header", "api_key"} {
		if strings.Contains(output, secret) {
			t.Fatalf("credential leaked: %s", output)
		}
	}
	if got := embyDiagnosticPath("/Items/123/ThemeMedia"); got != "/items/{redacted}/thememedia" {
		t.Fatalf("diagnostic %s", got)
	}
}

func TestEmbyOfficialSupplementalCollections(t *testing.T) {
	s, ts := newTestServer(t)
	token := embyLoginTest(t, ts)
	fid := seedProgressFile(t, s.Store)
	f, err := s.Store.FileByID(fid)
	if err != nil {
		t.Fatal(err)
	}
	id := embyID(f.ItemID)
	for _, prefix := range []string{"", "/EmBy", "/MediaBrowser"} {
		path := prefix + "/Items/" + id + "/ThemeMedia?UserId=1&InheritFromParent=true"
		code, raw := embyRequest(t, ts.URL, "GET", path, token, nil)
		if code != 200 {
			t.Fatalf("%s: %d %s", path, code, raw)
		}
		obj := embyObject(t, raw)
		for _, key := range []string{"ThemeVideosResult", "ThemeSongsResult"} {
			result, ok := obj[key].(map[string]any)
			if !ok {
				t.Fatalf("missing %s: %s", key, raw)
			}
			items, ok := result["Items"].([]any)
			if !ok || len(items) != 0 || result["TotalRecordCount"] != float64(0) {
				t.Fatalf("contract %s: %s", key, raw)
			}
		}
		path = prefix + "/Videos/" + id + "/AdditionalParts?UserId=1"
		code, raw = embyRequest(t, ts.URL, "GET", path, token, nil)
		var parts struct {
			Items            []any
			TotalRecordCount int
		}
		if code != 200 || json.Unmarshal(raw, &parts) != nil || parts.Items == nil || len(parts.Items) != 0 || parts.TotalRecordCount != 0 {
			t.Fatalf("parts: %d %s", code, raw)
		}
	}
	for _, path := range []string{"/Items/" + id + "/ThemeMedia", "/Videos/" + id + "/AdditionalParts"} {
		if code, _ := embyRequest(t, ts.URL, "GET", path, "", nil); code != 401 {
			t.Fatalf("auth: %d", code)
		}
		if code, _ := embyRequest(t, ts.URL, "GET", path+"?UserId=999", token, nil); code != 403 {
			t.Fatalf("cross-user: %d", code)
		}
	}
	if code, _ := embyRequest(t, ts.URL, "GET", "/Videos/999999/AdditionalParts", token, nil); code != 404 {
		t.Fatalf("missing item: %d", code)
	}
}
