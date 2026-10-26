package api

import (
	"net/http"
	"testing"
	"time"

	"doriyamedia/internal/auth"
	"doriyamedia/internal/models"
)

func TestEmbyOfficialLoginInitialization(t *testing.T) {
	s, ts := newTestServer(t)
	token := embyLoginTest(t, ts)
	// Official authenticateUserByName awaits onAuthenticated -> setUserInfo ->
	// GET DisplayPreferences/usersettings; an HTTP 200 login alone is insufficient.
	path := "/emby/DisplayPreferences/usersettings?userId=1&client=emby"
	code, raw := embyRequest(t, ts.URL, "GET", path, token, nil)
	if code != 200 {
		t.Fatalf("login initialization preferences: %d %s", code, raw)
	}
	obj := embyObject(t, raw)
	if _, ok := obj["CustomPrefs"].(map[string]any); !ok {
		t.Fatalf("CustomPrefs must be an object: %s", raw)
	}
	prefs := map[string]any{"Id": "ignored", "CustomPrefs": map[string]string{"homesection0": "smalllibrarytiles", "language": "zh-CN"}}
	code, raw = embyRequest(t, ts.URL, "POST", path, token, prefs)
	if code != 204 {
		t.Fatalf("save preferences: %d %s", code, raw)
	}
	code, raw = embyRequest(t, ts.URL, "GET", path, token, nil)
	if code != 200 {
		t.Fatalf("reload: %d %s", code, raw)
	}
	obj = embyObject(t, raw)
	if obj["Id"] != "usersettings" || obj["Client"] != "emby" || obj["CustomPrefs"].(map[string]any)["language"] != "zh-CN" {
		t.Fatalf("persisted preferences: %s", raw)
	}
	var persisted string
	if err := s.Store.DB.QueryRow("SELECT data FROM emby_display_preferences WHERE user_id=1 AND preference_id='usersettings' AND client='emby'").Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	hash, salt := auth.HashPassword("viewer-pass")
	u := &models.User{Name: "viewer", PassHash: hash, Salt: salt, Status: models.StatusActive}
	if err := s.Store.CreateUser(u); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.CreateSession("viewer-prefs", u.ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	otherPath := "/DisplayPreferences/usersettings?userId=" + embyID(u.ID) + "&client=emby"
	code, raw = embyRequest(t, ts.URL, "GET", otherPath, "viewer-prefs", nil)
	if code != 200 || len(embyObject(t, raw)["CustomPrefs"].(map[string]any)) != 0 {
		t.Fatalf("user isolation: %d %s", code, raw)
	}
	if code, _ := embyRequest(t, ts.URL, "GET", path, "viewer-prefs", nil); code != 403 {
		t.Fatalf("cross-user preferences: %d", code)
	}
	if code, _ := embyRequest(t, ts.URL, "GET", path, "", nil); code != 401 {
		t.Fatalf("anonymous preferences: %d", code)
	}
	code, raw = embyRequest(t, ts.URL, "GET", "/DisplayPreferences/usersettings?client=other", token, nil)
	if code != 200 || len(embyObject(t, raw)["CustomPrefs"].(map[string]any)) != 0 {
		t.Fatalf("client isolation: %d %s", code, raw)
	}
	code, raw = embyRequest(t, ts.URL, "GET", "/Users/Me", token, nil)
	if code != 200 {
		t.Fatalf("user: %d %s", code, raw)
	}
	cfg := embyObject(t, raw)["Configuration"].(map[string]any)
	for _, key := range []string{"LatestItemsExcludes", "MyMediaExcludes", "OrderedViews", "GroupedFolders"} {
		if _, ok := cfg[key].([]any); !ok {
			t.Fatalf("official client calls %s.includes: missing array: %s", key, raw)
		}
	}
}

func TestEmbyOfficialHostedPreflight(t *testing.T) {
	_, ts := newTestServer(t)
	for _, origin := range []string{"https://app.emby.media", "https://untrusted.example"} {
		req, err := http.NewRequest("OPTIONS", ts.URL+"/emby/Users/AuthenticateByName", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", "POST")
		req.Header.Set("Access-Control-Request-Headers", "x-emby-authorization,content-type")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if origin == "https://app.emby.media" {
			if resp.StatusCode != 204 || resp.Header.Get("Access-Control-Allow-Origin") != origin {
				t.Fatalf("official preflight: %d %v", resp.StatusCode, resp.Header)
			}
		} else if resp.Header.Get("Access-Control-Allow-Origin") != "" {
			t.Fatal("reflected untrusted origin")
		}
		if resp.Header.Get("Access-Control-Allow-Credentials") != "" {
			t.Fatal("cross-origin cookies enabled")
		}
	}
}
