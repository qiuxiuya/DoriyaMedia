package api

import (
	"bytes"
	"doriyamedia/internal/models"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestEmbyEpisodePrimaryUsesOwnStill(t *testing.T) {
	s, ts := newTestServer(t)
	token := embyLoginTest(t, ts)
	fid := seedProgressFile(t, s.Store)
	f, err := s.Store.FileByID(fid)
	if err != nil {
		t.Fatal(err)
	}
	film, err := s.Store.ItemByID(f.ItemID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(s.Cfg.ImagesDir(), 0700); err != nil {
		t.Fatal(err)
	}
	data := map[string][]byte{}
	for n, name := range []string{"show.png", "ep1.png", "ep2.png"} {
		im := image.NewRGBA(image.Rect(0, 0, 4, 2))
		im.Set(0, 0, color.RGBA{R: uint8(50 + n), A: 255})
		var b bytes.Buffer
		if err := png.Encode(&b, im); err != nil {
			t.Fatal(err)
		}
		data[name] = b.Bytes()
		if err := os.WriteFile(filepath.Join(s.Cfg.ImagesDir(), name), b.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
	show := &models.Item{LibraryID: film.LibraryID, Type: "show", Name: "Anime", PosterPath: "show.png"}
	if err := s.Store.InsertItem(show); err != nil {
		t.Fatal(err)
	}
	var firstTag string
	for n, name := range []string{"ep1.png", "ep2.png"} {
		ep := &models.Item{LibraryID: film.LibraryID, ParentID: &show.ID, Type: "episode", Name: "Episode", Episode: n + 1, StillPath: name, PosterPath: "show.png"}
		if err := s.Store.InsertItem(ep); err != nil {
			t.Fatal(err)
		}
		code, raw := embyRequest(t, ts.URL, "GET", "/Users/1/Items/"+embyID(ep.ID), token, nil)
		if code != 200 {
			t.Fatalf("detail %d %s", code, raw)
		}
		dto := embyObject(t, raw)
		tags := dto["ImageTags"].(map[string]any)
		tag := tags["Primary"].(string)
		if _, ok := dto["PrimaryImageItemId"]; ok {
			t.Fatalf("own still replaced by inherited poster: %s", raw)
		}
		code, raw = embyRequest(t, ts.URL, "GET", "/emby/Items/"+embyID(ep.ID)+"/Images/Primary?tag="+tag, "", nil)
		if code != 200 || !bytes.Equal(raw, data[name]) {
			t.Fatalf("wrong episode image %d %q", code, raw)
		}
		if n == 0 {
			firstTag = tag
		} else {
			if firstTag == tag {
				t.Fatal("episodes share image identity")
			}
			if code, _ := embyRequest(t, ts.URL, "GET", "/emby/Items/"+embyID(ep.ID)+"/Images/Primary?tag="+firstTag, "", nil); code != 401 {
				t.Fatalf("cross episode tag accepted: %d", code)
			}
		}
	}
}
