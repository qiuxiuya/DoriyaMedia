package api

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestEmbyFormLogin(t *testing.T) {
	s, ts := newTestServer(t)
	setupAdmin(t, ts.URL)
	for _, tc := range []struct {
		name   string
		values url.Values
		status int
	}{
		{"official form", url.Values{"Username": {"admin"}, "Pw": {"admin-pass"}}, 200},
		{"case insensitive", url.Values{"username": {" admin "}, "pw": {"admin-pass"}}, 200},
		{"wrong password", url.Values{"Username": {"admin"}, "Pw": {"wrong"}}, 401},
		{"hash is not plaintext", url.Values{"Username": {"admin"}, "Password": {"admin-pass"}}, 400},
		{"do not trim password", url.Values{"Username": {"admin"}, "Pw": {" admin-pass "}}, 401},
		{"missing username", url.Values{"Pw": {"admin-pass"}}, 400},
		{"oversized body", url.Values{"Username": {"admin"}, "Pw": {"admin-pass"}, "extra": {strings.Repeat("x", 65<<10)}}, 400},
		{"missing password", url.Values{"Username": {"admin"}}, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest("POST", ts.URL+"/emby/Users/AuthenticateByName", strings.NewReader(tc.values.Encode()))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
			req.Header.Set("X-Emby-Authorization", `Emby Client="Emby", Device="Test", DeviceId="test", Version="1"`)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			raw, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != tc.status {
				t.Fatalf("status %d, want %d: %s", resp.StatusCode, tc.status, raw)
			}
			if tc.status == 200 {
				token, _ := embyObject(t, raw)["AccessToken"].(string)
				if _, err := s.Store.SessionByToken(token); token == "" || err != nil {
					t.Fatalf("missing persisted session: %v", err)
				}
			}
		})
	}
}

func TestEmbyLoginDoesNotAcceptQueryCredentials(t *testing.T) {
	_, ts := newTestServer(t)
	setupAdmin(t, ts.URL)
	req, err := http.NewRequest("POST", ts.URL+"/Users/AuthenticateByName?Username=admin&Pw=admin-pass", strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("query credentials accepted: %d", resp.StatusCode)
	}
}
