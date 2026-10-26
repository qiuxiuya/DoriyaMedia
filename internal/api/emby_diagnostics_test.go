package api

import (
	"bytes"
	"log"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEmbyUnknownRouteNamesRemainVisible(t *testing.T) {
	for _, tc := range []struct{ path, want string }{
		{"/NewService/NewOperation", "/newservice/newoperation"},
		{"/Items/secretitem/NewOperation", "/items/{redacted}/newoperation"},
		{"/Items/items/NewOperation", "/items/{redacted}/newoperation"},
		{"/Items/123/Images/Primary/0/secret/format", "/items/{redacted}/images/primary/{redacted}/{redacted}/{redacted}"},
		{"/Service/Token/secret/anothersecret", "/service/token/{redacted}/{redacted}"},
		{"/NewService/7ca123dd", "/newservice/{redacted}"},
		{"/NewService/secret%20value", "/newservice/{redacted}"},
	} {
		if got := embyDiagnosticPath(tc.path); got != tc.want {
			t.Errorf("%s: got %q want %q", tc.path, got, tc.want)
		}
	}
	s, _ := newTestServer(t)
	var logs bytes.Buffer
	old := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(old)
	req := httptest.NewRequest("GET", "/emby/NewService/NewOperation?api_key=secretquery", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	output := logs.String()
	if w.Code != 404 || !strings.Contains(output, "unmatched /newservice/newoperation") {
		t.Fatalf("diagnostic: %d %s", w.Code, output)
	}
	if strings.Contains(output, "secretquery") || strings.Contains(output, "api_key") {
		t.Fatalf("query leaked: %s", output)
	}
}
