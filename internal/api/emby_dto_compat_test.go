package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"doriyamedia/internal/models"
)

// Field types follow MediaBrowser/Emby.SDK's Swift5 generated BaseItemDto,
// UserItemDataDto and MediaSourceInfo, rather than an invented mandatory schema:
// https://github.com/MediaBrowser/Emby.SDK/tree/master/SampleCode/RestApi/Swift5/EmbyClient/Classes/Swaggers/Models
// Optional SDK fields may be omitted, but a supplied value must have the right
// JSON type. In particular media source enums and numeric fields are not strings.
type embyCompatSource struct {
	ID                         string `json:"Id"`
	ItemID                     string `json:"ItemId"`
	Protocol                   string
	Type                       string
	Size                       int64
	RunTimeTicks               int64
	Bitrate                    int
	SupportsDirectPlay         bool
	SupportsDirectStream       bool
	SupportsTranscoding        bool
	DefaultAudioStreamIndex    int
	DefaultSubtitleStreamIndex int
	RequiredHttpHeaders        map[string]string
	MediaStreams               []struct {
		Index      int
		Type       string
		Codec      string
		BitRate    int
		IsExternal bool
	}
}

type embyCompatItem struct {
	ID           string `json:"Id"`
	DateCreated  *time.Time
	PremiereDate *time.Time
	Chapters     []struct {
		StartPositionTicks int64
		Name               string
	}
	Studios []struct {
		Name string
		ID   int64 `json:"Id"`
	}
	Taglines     []string
	ExternalUrls []struct {
		Name string
		URL  string `json:"Url"`
	}
	MediaSources     []embyCompatSource
	MediaSourceCount int
	UserData         struct {
		ItemID                string `json:"ItemId"`
		ServerID              string `json:"ServerId"`
		Key                   string
		Played                bool
		PlayCount             int
		IsFavorite            bool
		PlaybackPositionTicks int64
		PlayedPercentage      float64
		LastPlayedDate        *time.Time
	}
}

func TestEmbyDTOEmptyCollectionsAndUnknownDates(t *testing.T) {
	s, _ := newTestServer(t)
	r := httptest.NewRequest(http.MethodGet, "/emby/Items/42", nil)
	for _, typ := range []string{"movie", "episode", "show", "season"} {
		t.Run(typ, func(t *testing.T) {
			dto := s.embyItem(&models.Item{ID: 42, Type: typ}, &models.User{ID: 1}, r)
			for _, key := range []string{"DateCreated", "PremiereDate"} {
				if v, ok := dto[key]; ok {
					t.Fatalf("unknown %s emitted as %v", key, v)
				}
			}
			raw, err := json.Marshal(dto)
			if err != nil {
				t.Fatal(err)
			}
			var decoded embyCompatItem
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatalf("typed SDK decoding: %v: %s", err, raw)
			}
			if decoded.Chapters == nil || decoded.Studios == nil || decoded.Taglines == nil || decoded.ExternalUrls == nil || decoded.MediaSources == nil {
				t.Fatalf("null or missing detail collection: %s", raw)
			}
			if decoded.UserData.Key != "42" || decoded.UserData.ItemID != "42" || decoded.UserData.ServerID == "" || decoded.UserData.LastPlayedDate != nil {
				t.Fatalf("unknown user data defaults: %s", raw)
			}
		})
	}
}

func TestEmbyDTOPremiereDateValidation(t *testing.T) {
	s, _ := newTestServer(t)
	r := httptest.NewRequest(http.MethodGet, "/emby/Items/42", nil)
	for _, tc := range []struct{ input, want string }{
		{"", ""}, {"unknown", ""}, {"2026-02-30", ""},
		{"0001-01-01", ""}, {"2026-10-25", "2026-10-25T00:00:00.000Z"},
		{"2026-10-25T20:30:00+08:00", "2026-10-25T12:30:00.000Z"},
		{"2026-10-25T20:30:00.123456789Z", "2026-10-25T20:30:00.123Z"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			dto := s.embyItem(&models.Item{ID: 42, Type: "show", AirDate: tc.input}, &models.User{ID: 1}, r)
			date, ok := dto["PremiereDate"]
			if tc.want == "" {
				if ok {
					t.Fatalf("invalid date emitted: %v", date)
				}
				return
			}
			if date != tc.want {
				t.Fatalf("PremiereDate = %v, want %s", date, tc.want)
			}
		})
	}
}

func TestEmbyDTODetailAndPlaybackSourceJSONTypes(t *testing.T) {
	s, ts := newTestServer(t)
	token := embyLoginTest(t, ts)
	fid := seedProgressFile(t, s.Store)
	f, err := s.Store.FileByID(fid)
	if err != nil {
		t.Fatal(err)
	}
	probe := `{"format":{"duration":"100","bit_rate":"8000000","format_name":"matroska"},"streams":[{"index":0,"codec_type":"video","codec_name":"h264","width":1920,"height":1080,"bit_rate":"7500000"},{"index":1,"codec_type":"audio","codec_name":"aac","channels":2,"bit_rate":"128000","disposition":{"default":1}}]}`
	if err := s.Store.UpdateFileProbe(fid, probe); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.SaveProgress(&models.Progress{UserID: 1, FileID: fid, ItemID: f.ItemID, Position: 25, Duration: 100}); err != nil {
		t.Fatal(err)
	}
	for _, prefix := range []string{"/emby/Items/", "/emby/Users/1/Items/"} {
		code, raw := embyDetailGet(t, ts.URL, prefix+embyID(f.ItemID), token)
		if code != 200 {
			t.Fatalf("detail %d: %s", code, raw)
		}
		var dto embyCompatItem
		if err := json.Unmarshal(raw, &dto); err != nil {
			t.Fatalf("typed detail: %v: %s", err, raw)
		}
		if len(dto.MediaSources) != 1 || dto.MediaSourceCount != 1 {
			t.Fatalf("detail sources: %s", raw)
		}
		if dto.UserData.PlaybackPositionTicks != 25*embyTicksPerSecond || dto.UserData.PlayedPercentage != 25 || dto.UserData.LastPlayedDate == nil || dto.DateCreated == nil {
			t.Fatalf("detail dates/progress: %s", raw)
		}
		var values map[string]any
		if err := json.Unmarshal(raw, &values); err != nil {
			t.Fatal(err)
		}
		date := values["DateCreated"].(string)
		if _, err := time.Parse("2006-01-02T15:04:05.000Z", date); err != nil {
			t.Fatalf("SDK millisecond date: %v", err)
		}
		ms := dto.MediaSources[0]
		if ms.ID != embyID(fid) || ms.ItemID != dto.ID || ms.Type != "Default" || ms.Protocol != "Http" || ms.RunTimeTicks != 100*embyTicksPerSecond || ms.Bitrate != 8000000 || len(ms.MediaStreams) != 2 || ms.RequiredHttpHeaders == nil {
			t.Fatalf("source types/values: %+v", ms)
		}
		code, playbackRaw := embyDetailGet(t, ts.URL, "/emby/Items/"+dto.ID+"/PlaybackInfo?IsPlayback=false", token)
		if code != 200 {
			t.Fatalf("preflight %d: %s", code, playbackRaw)
		}
		var playback struct{ MediaSources []embyCompatSource }
		if err := json.Unmarshal(playbackRaw, &playback); err != nil {
			t.Fatalf("typed preflight: %v: %s", err, playbackRaw)
		}
		if len(playback.MediaSources) != 1 || playback.MediaSources[0].ID != ms.ID || playback.MediaSources[0].RunTimeTicks != ms.RunTimeTicks {
			t.Fatalf("preflight/detail mismatch: %s", playbackRaw)
		}
	}
}
