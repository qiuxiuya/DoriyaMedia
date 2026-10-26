package api

import (
	"bytes"
	"encoding/json"
	"log"
	"math"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEmbyDetailEncodingAndSafeSchema(t *testing.T) {
	var logs bytes.Buffer
	old := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(old)
	item := map[string]any{"Id": "42", "Name": "private-title", "Overview": "private-plot", "Type": "Movie", "ImageTags": map[string]string{"Primary": "private-tag"}, "MediaSources": []map[string]any{{"Id": "7", "Path": "/stream?api_key=private-token", "MediaStreams": []any{}}}}
	req := httptest.NewRequest("GET", "/items/42", nil)
	w := httptest.NewRecorder()
	embyWriteDetail(w, req, item)
	if w.Code != 200 || !json.Valid(w.Body.Bytes()) {
		t.Fatalf("invalid detail %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "private-token") {
		t.Fatal("response content was changed by log sanitization")
	}
	output := logs.String()
	for _, secret := range []string{"private-title", "private-plot", "private-tag", "private-token", "api_key"} {
		if strings.Contains(output, secret) {
			t.Fatalf("logged %s: %s", secret, output)
		}
	}
	for _, field := range []string{"response_bytes=", "MediaSources", "MediaStreams", "length", "item_id"} {
		if !strings.Contains(output, field) {
			t.Fatalf("missing %s: %s", field, output)
		}
	}
	logs.Reset()
	w = httptest.NewRecorder()
	embyWriteDetail(w, req, map[string]any{"CommunityRating": math.NaN()})
	if w.Code != 500 || !json.Valid(w.Body.Bytes()) || !strings.Contains(logs.String(), "encode_error=true") {
		t.Fatalf("false success: %d %s %s", w.Code, w.Body.String(), logs.String())
	}
}
