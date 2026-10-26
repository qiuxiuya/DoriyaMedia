package api

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestEmbyOfficialImageURLs(t *testing.T) {
	s, ts := newTestServer(t)
	token := embyLoginTest(t, ts)
	fid := seedProgressFile(t, s.Store)
	f, err := s.Store.FileByID(fid)
	if err != nil {
		t.Fatal(err)
	}
	it, err := s.Store.ItemByID(f.ItemID)
	if err != nil {
		t.Fatal(err)
	}
	var pngBytes bytes.Buffer
	if err := png.Encode(&pngBytes, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(s.Cfg.ImagesDir(), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"poster.png", "backdrop.png", "thumb.png"} {
		if err := os.WriteFile(filepath.Join(s.Cfg.ImagesDir(), name), pngBytes.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
	it.PosterPath, it.BackdropPath, it.StillPath = "poster.png", "backdrop.png", "thumb.png"
	if err := s.Store.UpdateItemMeta(it); err != nil {
		t.Fatal(err)
	}
	id := embyID(it.ID)
	code, raw := embyRequest(t, ts.URL, "GET", "/emby/Users/1/Items/"+id, token, nil)
	if code != 200 {
		t.Fatalf("detail %d %s", code, raw)
	}
	obj := embyObject(t, raw)
	tags := obj["ImageTags"].(map[string]any)
	cases := []struct{ kind, tag string }{
		{"Primary", tags["Primary"].(string)},
		{"Thumb", tags["Thumb"].(string)},
		{"Backdrop", obj["BackdropImageTags"].([]any)[0].(string)},
	}
	for _, tc := range cases {
		for _, prefix := range []string{"", "/EmBy", "/MediaBrowser"} {
			base := prefix + "/Items/" + id + "/Images/" + tc.kind
			for _, path := range []string{base + "?tag=" + tc.tag, base + "/0?Tag=" + tc.tag, base + "/0/" + tc.tag + "/original/500/500/0/0"} {
				code, raw := embyRequest(t, ts.URL, "GET", path, "", nil)
				if code != 200 || !bytes.Equal(raw, pngBytes.Bytes()) {
					t.Fatalf("tokenless %s: %d %q", path, code, raw)
				}
			}
		}
	}
	tag := cases[0].tag
	for _, tc := range []struct {
		path   string
		status int
	}{
		{"/Items/" + id + "/Images/Primary", 401},
		{"/Items/" + id + "/Images/Primary?tag=forged", 401},
		{"/Items/" + id + "/Images/Backdrop?tag=" + tag, 401},
		{"/Items/99999/Images/Primary?tag=" + tag, 401},
		{"/Items/" + id + "?tag=" + tag, 401},
		{"/Videos/" + id + "/stream?tag=" + tag, 401},
	} {
		code, raw := embyRequest(t, ts.URL, "GET", tc.path, "", nil)
		if code != tc.status {
			t.Fatalf("scope %s: %d %s", tc.path, code, raw)
		}
	}
	code, raw = embyRequest(t, ts.URL, "GET", "/Items/"+id+"/Images/Primary?tag=old-cache-tag", token, nil)
	if code != 200 || !bytes.Equal(raw, pngBytes.Bytes()) {
		t.Fatalf("authenticated legacy tag: %d %s", code, raw)
	}
}
