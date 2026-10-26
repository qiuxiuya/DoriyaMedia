package api

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"doriyamedia/internal/models"
)

// These are documented BaseItemDto fields, not invented ParentPrimaryImage*
// or SeasonPrimaryImageTag aliases. Inherited tags must be scoped to the owner.
func TestEmbyInheritedImagesResolveToActualOwner(t *testing.T) {
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
	images := map[string][]byte{}
	for i, name := range []string{"series-poster.png", "series-backdrop.png", "season-poster.png", "season-backdrop.png", "episode-poster.png", "episode-backdrop.png"} {
		im := image.NewRGBA(image.Rect(0, 0, 2, 3))
		im.Set(0, 0, color.RGBA{R: uint8(i + 1), A: 255})
		var data bytes.Buffer
		if err := png.Encode(&data, im); err != nil {
			t.Fatal(err)
		}
		images[name] = data.Bytes()
		if err := os.WriteFile(filepath.Join(s.Cfg.ImagesDir(), name), data.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
	show := &models.Item{LibraryID: film.LibraryID, Type: "show", Name: "Series", PosterPath: "series-poster.png", BackdropPath: "series-backdrop.png"}
	if err := s.Store.InsertItem(show); err != nil {
		t.Fatal(err)
	}
	season := &models.Item{LibraryID: film.LibraryID, ParentID: &show.ID, Type: "season", Name: "Season 1", Season: 1}
	if err := s.Store.InsertItem(season); err != nil {
		t.Fatal(err)
	}
	ep := &models.Item{LibraryID: film.LibraryID, ParentID: &season.ID, Type: "episode", Name: "Episode 1", Season: 1, Episode: 1}
	if err := s.Store.InsertItem(ep); err != nil {
		t.Fatal(err)
	}
	getDetail := func(item *models.Item) map[string]any {
		t.Helper()
		code, raw := embyDetailGet(t, ts.URL, "/emby/Users/1/Items/"+embyID(item.ID), token)
		if code != 200 {
			t.Fatalf("detail %d: %s", code, raw)
		}
		return embyObject(t, raw)
	}
	getImage := func(id, kind, tag, name string) {
		t.Helper()
		code, raw := embyRequest(t, ts.URL, "GET", "/emby/Items/"+id+"/Images/"+kind+"?tag="+tag, "", nil)
		if code != 200 || !bytes.Equal(raw, images[name]) {
			t.Fatalf("tokenless inherited image %s/%s = %d: %q", id, kind, code, raw)
		}
	}
	checkInherited := func(item, owner *models.Item) {
		t.Helper()
		dto := getDetail(item)
		if len(dto["ImageTags"].(map[string]any)) != 0 || len(dto["BackdropImageTags"].([]any)) != 0 {
			t.Fatalf("inherited images advertised as self-owned: %v", dto)
		}
		if dto["PrimaryImageItemId"] != embyID(owner.ID) || dto["ParentBackdropItemId"] != embyID(owner.ID) {
			t.Fatalf("incorrect nearest owner: %v", dto)
		}
		getImage(dto["PrimaryImageItemId"].(string), "Primary", dto["PrimaryImageTag"].(string), owner.PosterPath)
		getImage(dto["ParentBackdropItemId"].(string), "Backdrop", dto["ParentBackdropImageTags"].([]any)[0].(string), owner.BackdropPath)
		if dto["SeriesId"] != embyID(show.ID) {
			t.Fatalf("missing series owner: %v", dto)
		}
		getImage(dto["SeriesId"].(string), "Primary", dto["SeriesPrimaryImageTag"].(string), show.PosterPath)
		for _, invented := range []string{"ParentPrimaryImageTag", "ParentPrimaryImageItemId", "SeasonPrimaryImageTag"} {
			if _, ok := dto[invented]; ok {
				t.Fatalf("undocumented image field %s", invented)
			}
		}
	}
	// Season without artwork and episode through that season both use series.
	checkInherited(season, show)
	checkInherited(ep, show)
	// Once the immediate parent has artwork it wins, not the more distant series.
	season.PosterPath, season.BackdropPath = "season-poster.png", "season-backdrop.png"
	if err := s.Store.UpdateItemMeta(season); err != nil {
		t.Fatal(err)
	}
	checkInherited(ep, season)
	// Own artwork is never replaced by inherited references.
	ep.PosterPath, ep.BackdropPath = "episode-poster.png", "episode-backdrop.png"
	if err := s.Store.UpdateItemMeta(ep); err != nil {
		t.Fatal(err)
	}
	dto := getDetail(ep)
	for _, key := range []string{"PrimaryImageItemId", "PrimaryImageTag", "ParentBackdropItemId", "ParentBackdropImageTags"} {
		if _, ok := dto[key]; ok {
			t.Fatalf("own artwork also inherited %s: %v", key, dto)
		}
	}
	getImage(embyID(ep.ID), "Primary", dto["ImageTags"].(map[string]any)["Primary"].(string), ep.PosterPath)
	getImage(embyID(ep.ID), "Backdrop", dto["BackdropImageTags"].([]any)[0].(string), ep.BackdropPath)
	// Nothing is fabricated if neither item nor ancestors have artwork.
	show.PosterPath, show.BackdropPath = "", ""
	season.PosterPath, season.BackdropPath = "", ""
	ep.PosterPath, ep.BackdropPath = "", ""
	for _, item := range []*models.Item{show, season, ep} {
		if err := s.Store.UpdateItemMeta(item); err != nil {
			t.Fatal(err)
		}
	}
	dto = getDetail(ep)
	for _, key := range []string{"PrimaryImageItemId", "PrimaryImageTag", "ParentBackdropItemId", "ParentBackdropImageTags", "SeriesPrimaryImageTag"} {
		if _, ok := dto[key]; ok {
			t.Fatalf("missing ancestor image fabricated %s: %v", key, dto)
		}
	}
}
