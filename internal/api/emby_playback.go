package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"doriyamedia/internal/auth"
	"doriyamedia/internal/media"
	"doriyamedia/internal/models"
	"doriyamedia/internal/source"
)

func embyImageTag(name string) string {
	h := sha256.Sum256([]byte(name))
	return hex.EncodeToString(h[:8])
}
func embyProbe(f *models.MediaFile) *media.Probe {
	var p media.Probe
	_ = json.Unmarshal([]byte(f.Probe), &p)
	return &p
}
func embySubtitleIndex(sub models.Subtitle) int {
	if sub.Embedded {
		return sub.StreamIdx
	}
	return 1000000 + int(sub.ID)
}
func (s *Server) embyMediaSource(f *models.MediaFile, r *http.Request) map[string]any {
	p := embyProbe(f)
	container := strings.TrimPrefix(strings.ToLower(path.Ext(f.Path)), ".")
	if container == "" {
		container = "mp4"
	}
	if strings.Contains(p.Format.FormatName, "matroska") {
		container = "mkv"
	} else if strings.Contains(p.Format.FormatName, "mp4") {
		container = "mp4"
	}
	q := url.Values{"api_key": {embyToken(r)}, "MediaSourceId": {embyID(f.ID)}, "Static": {"true"}}
	streamURL := "/emby/Videos/" + embyID(f.ItemID) + "/stream." + container + "?" + q.Encode()
	streams := []map[string]any{}
	audioIndex := -1
	for _, st := range p.Streams {
		if st.CodecType != "video" && st.CodecType != "audio" && st.CodecType != "subtitle" {
			continue
		}
		if st.IsCoverArt() {
			continue
		}
		typ := map[string]string{"video": "Video", "audio": "Audio", "subtitle": "Subtitle"}[st.CodecType]
		var disp struct {
			Default int
			Forced  int
		}
		_ = json.Unmarshal(st.Disposition, &disp)
		bitrate, _ := st.BitRate.Int64()
		entry := map[string]any{"Index": st.Index, "Type": typ, "Codec": st.CodecName, "Language": st.Tags["language"], "Title": st.Tags["title"], "DisplayTitle": st.Tags["title"], "IsExternal": false, "IsDefault": disp.Default == 1, "IsForced": disp.Forced == 1, "Width": st.Width, "Height": st.Height, "Channels": st.Channels, "BitRate": bitrate}
		if st.CodecType == "audio" && (audioIndex < 0 || disp.Default == 1) {
			audioIndex = st.Index
		}
		if st.CodecType == "subtitle" {
			entry["IsTextSubtitleStream"] = st.CodecName != "hdmv_pgs_subtitle" && st.CodecName != "dvd_subtitle"
			entry["SupportsExternalStream"] = false
		}
		streams = append(streams, entry)
	}
	subs, _ := s.Store.SubtitlesByFile(f.ID)
	for _, sub := range subs {
		if !sub.Convertible {
			continue
		}
		index := embySubtitleIndex(sub)
		// Replace cached embedded tracks with the native engine's convertible rendition.
		for i := 0; i < len(streams); i++ {
			if streams[i]["Index"] == index && streams[i]["Type"] == "Subtitle" {
				streams = append(streams[:i], streams[i+1:]...)
				break
			}
		}
		// Server-root relative, like the official server. Clients prepend their
		// configured base path, so including /emby here produced /emby/emby/...
		delivery := fmt.Sprintf("/Videos/%d/%d/Subtitles/%d/Stream.vtt?api_key=%s", f.ItemID, f.ID, index, url.QueryEscape(embyToken(r)))
		streams = append(streams, map[string]any{"Index": index, "Type": "Subtitle", "Codec": "webvtt", "Language": sub.Lang, "Title": sub.Title, "DisplayTitle": sub.Title, "IsExternal": true, "IsTextSubtitleStream": true, "SupportsExternalStream": true, "DeliveryMethod": "External", "DeliveryUrl": delivery, "IsDefault": false, "IsForced": false})
	}
	bitrate, _ := p.Format.BitRate.Int64()
	return map[string]any{"Id": embyID(f.ID), "ItemId": embyID(f.ItemID), "Name": path.Base(f.Path), "Path": s.embyAbsoluteMediaURL(r, streamURL), "Protocol": "Http", "Type": "Default", "Container": container, "Size": f.Size, "RunTimeTicks": int64(p.Duration() * embyTicksPerSecond), "Bitrate": bitrate, "SupportsDirectPlay": true, "SupportsDirectStream": true, "SupportsTranscoding": false, "IsRemote": false, "ReadAtNativeFramerate": false, "RequiresOpening": false, "RequiresClosing": false, "RequiredHttpHeaders": map[string]string{}, "MediaStreams": streams, "DefaultAudioStreamIndex": audioIndex, "DefaultSubtitleStreamIndex": -1, "DirectStreamUrl": streamURL}
}
func (s *Server) embyFile(itemID int64, sourceID string) (*models.MediaFile, error) {
	if sourceID != "" {
		f, err := s.Store.FileByID(embyNumber(sourceID))
		if err != nil || f.ItemID != itemID {
			return nil, fmt.Errorf("media source does not belong to item")
		}
		return f, nil
	}
	files, err := s.Store.FilesByItem(itemID)
	if err != nil || len(files) == 0 {
		return nil, fmt.Errorf("no media files")
	}
	return &files[0], nil
}
func (s *Server) embyPlaybackInfo(w http.ResponseWriter, r *http.Request, u *models.User) {
	itemID := embyNumber(r.PathValue("id"))
	sourceID := embyQuery(r, "MediaSourceId")
	// The official client sends playback options in the query and only the
	// DeviceProfile in the POST body. Keep body options for existing clients,
	// and preserve the historical playback behavior when IsPlayback is absent.
	isPlayback := true
	if value := embyQuery(r, "IsPlayback"); value != "" {
		var err error
		isPlayback, err = strconv.ParseBool(value)
		if err != nil {
			fail(w, 400, "Invalid IsPlayback value")
			return
		}
	}
	if r.Method == "POST" && r.Body != nil {
		var body struct {
			MediaSourceId string
			UserId        string
			IsPlayback    *bool
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err != io.EOF {
			fail(w, 400, "Invalid playback request")
			return
		}
		if body.UserId != "" && body.UserId != embyID(u.ID) {
			fail(w, 403, "Cannot access another user's data")
			return
		}
		if body.MediaSourceId != "" {
			sourceID = body.MediaSourceId
		}
		if body.IsPlayback != nil {
			isPlayback = *body.IsPlayback
		}
	}
	files, err := s.Store.FilesByItem(itemID)
	if err != nil || len(files) == 0 {
		fail(w, 404, "No playable media")
		return
	}
	sources := []map[string]any{}
	for i := range files {
		if sourceID != "" && sourceID != embyID(files[i].ID) {
			continue
		}
		var multi bool
		// Detail-page track selection is metadata-only. Even DriverFor may do
		// remote work, so use only the stored probe/subtitles for preflights.
		if isPlayback && s.Engine != nil {
			drv, e := s.Engine.DriverFor(&files[i])
			if e != nil {
				fail(w, 502, "Cannot resolve media source")
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
			probe, e := s.Engine.EnsureProbe(ctx, &files[i], drv)
			if e != nil {
				cancel()
				fail(w, 502, "Cannot probe media source")
				return
			}
			if probe != nil {
				raw, _ := json.Marshal(probe)
				files[i].Probe = string(raw)
				if err := s.Store.UpdateFileProbe(files[i].ID, string(raw)); err != nil {
					cancel()
					fail(w, 500, "Cannot cache media probe")
					return
				}
				s.Engine.RegisterEmbeddedSubs(&files[i], probe)
			}
			s.discoverSubtitles(ctx, drv, &files[i])
			cancel()
			_, multi = drv.(source.MultiPart)
		}
		ms := s.embyMediaSource(&files[i], r)
		if multi {
			// DASH sources have separate video/audio inputs; never advertise
			// the raw video-only URL as a directly playable movie.
			capture := &embyDecisionWriter{header: make(http.Header)}
			native := r.Clone(r.Context())
			native.URL = new(url.URL)
			*native.URL = *r.URL
			q := native.URL.Query()
			q.Set("force", "remux")
			q.Set("client", "safari")
			q.Set("at", "0")
			native.URL.RawQuery = q.Encode()
			// The client controls seeking. Native automatic resume must not
			// shift this HLS timeline behind the client's back.
			native.Header.Del("Cookie")
			native.Header.Del("Authorization")
			s.playFile(capture, native, &files[i])
			var decision struct {
				Mode   string
				URL    string
				Reason string
			}
			if capture.status != 200 || json.Unmarshal(capture.body.Bytes(), &decision) != nil || decision.URL == "" || decision.Mode != "remux" {
				fail(w, 502, "Source cannot be remuxed for this client")
				return
			}
			ms["SupportsDirectPlay"] = false
			ms["Container"] = "m3u8"
			ms["Path"] = s.embyAbsoluteMediaURL(r, decision.URL)
			ms["DirectStreamUrl"] = decision.URL
			ms["TranscodingUrl"] = decision.URL
			ms["TranscodingContainer"] = "ts"
			if s.Cfg.Playback.HLSSessionPackager {
				ms["TranscodingContainer"] = "mp4"
			}
			ms["TranscodingSubProtocol"] = "hls"
		}
		sources = append(sources, ms)
	}
	if len(sources) == 0 {
		fail(w, 404, "Media source not found")
		return
	}
	writeJSON(w, 200, map[string]any{"MediaSources": sources, "PlaySessionId": auth.NewToken()})
}

// Buffer only the native JSON playback decision, never media bytes.
type embyDecisionWriter struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func (w *embyDecisionWriter) Header() http.Header    { return w.header }
func (w *embyDecisionWriter) WriteHeader(status int) { w.status = status }
func (w *embyDecisionWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	return w.body.Write(b)
}

func (s *Server) embyStream(w http.ResponseWriter, r *http.Request, u *models.User) {
	f, err := s.embyFile(embyNumber(r.PathValue("id")), embyQuery(r, "MediaSourceId"))
	if err != nil {
		fail(w, 404, "Media source not found")
		return
	}
	if s.Engine == nil {
		fail(w, 503, "Playback engine unavailable")
		return
	}
	r.SetPathValue("id", embyID(f.ID))
	s.handleStream(w, r)
}
func (s *Server) embySubtitle(w http.ResponseWriter, r *http.Request, u *models.User) {
	if !strings.HasSuffix(r.PathValue("format"), ".vtt") {
		fail(w, 400, "Only WebVTT subtitles are supported")
		return
	}
	f, err := s.embyFile(embyNumber(r.PathValue("id")), r.PathValue("source"))
	if err != nil {
		fail(w, 404, "Media source not found")
		return
	}
	index, err := strconv.Atoi(r.PathValue("index"))
	if err != nil {
		fail(w, 400, "Invalid subtitle index")
		return
	}
	subs, err := s.Store.SubtitlesByFile(f.ID)
	if err != nil {
		fail(w, 500, "Cannot load subtitles")
		return
	}
	for _, sub := range subs {
		if sub.Convertible && embySubtitleIndex(sub) == index {
			if s.Engine == nil {
				fail(w, 503, "Playback engine unavailable")
				return
			}
			r.SetPathValue("id", embyID(f.ID))
			r.SetPathValue("sub", embyID(sub.ID)+".vtt")
			s.handleSubtitle(w, r)
			return
		}
	}
	fail(w, 404, "Subtitle not found")
}

type embyProgressRequest struct {
	ItemId        string
	MediaSourceId string
	PositionTicks *int64
	RunTimeTicks  int64
}

func (s *Server) embyPlaying(w http.ResponseWriter, r *http.Request, u *models.User) {
	var req embyProgressRequest
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := readBody(r, &req); err != nil {
		fail(w, 400, "Invalid progress request")
		return
	}
	f, err := s.embyFile(embyNumber(req.ItemId), req.MediaSourceId)
	if err != nil {
		fail(w, 404, "Media source not found")
		return
	}
	if req.PositionTicks == nil {
		w.WriteHeader(204)
		return
	}
	if *req.PositionTicks < 0 || req.RunTimeTicks < 0 {
		fail(w, 400, "Invalid playback position")
		return
	}
	duration := embyProbe(f).Duration()
	if duration <= 0 && req.RunTimeTicks > 0 {
		duration = float64(req.RunTimeTicks) / embyTicksPerSecond
	}
	if duration <= 0 {
		if p, e := s.Store.ProgressForFile(u.ID, f.ID); e == nil {
			duration = p.Duration
		}
	}
	// A start event without a measured duration must not destroy resume state.
	if duration <= 0 {
		w.WriteHeader(204)
		return
	}
	position := float64(*req.PositionTicks) / embyTicksPerSecond
	if math.IsInf(duration, 0) || math.IsNaN(duration) || position > duration+1 {
		fail(w, 400, "Invalid playback position")
		return
	}
	if position > duration {
		position = duration
	}
	raw, _ := json.Marshal(map[string]any{"file_id": f.ID, "position": position, "duration": duration})
	native := r.Clone(r.Context())
	native.Body = io.NopCloser(bytes.NewReader(raw))
	adapter := &embyProgressWriter{ResponseWriter: w}
	s.handleSaveProgress(adapter, native, u)
}

// Native progress enqueues Bangumi sync as well. Preserve errors but translate
// its successful JSON response into Emby's no-content acknowledgement.
type embyProgressWriter struct {
	http.ResponseWriter
	status int
}

func (w *embyProgressWriter) WriteHeader(status int) {
	w.status = status
	if status == 200 {
		w.Header().Del("Content-Type")
		w.ResponseWriter.WriteHeader(204)
	} else {
		w.ResponseWriter.WriteHeader(status)
	}
}
func (w *embyProgressWriter) Write(b []byte) (int, error) {
	if w.status == 200 {
		return len(b), nil
	}
	return w.ResponseWriter.Write(b)
}

// The official API client fetches System/Endpoint before PlaybackInfo and
// derives its own NetworkType from IsInNetwork. Do not trust forwarded headers:
// without a configured trusted proxy, only the immediate peer is known.
func (s *Server) embyEndpoint(w http.ResponseWriter, r *http.Request, u *models.User) {
	ip := net.ParseIP(clientIP(r))
	isLocal := ip != nil && ip.IsLoopback()
	isInNetwork := isLocal || (ip != nil && (ip.IsPrivate() || ip.IsLinkLocalUnicast()))
	writeJSON(w, 200, map[string]bool{"IsLocal": isLocal, "IsInNetwork": isInNetwork})
}

func (s *Server) embyRegisterPlayback(secured func(string, authedHandler)) {
	secured("GET /system/endpoint", s.embyEndpoint)
	for _, method := range []string{"GET", "POST"} {
		secured(method+" /items/{id}/playbackinfo", s.embyPlaybackInfo)
	}
	// ServeMux variables cannot match part of a segment (stream.{extension}).
	secured("GET /videos/{id}/{resource}", func(w http.ResponseWriter, r *http.Request, u *models.User) {
		resource := r.PathValue("resource")
		if resource != "stream" && !strings.HasPrefix(resource, "stream.") {
			fail(w, 404, "Unsupported playback resource")
			return
		}
		s.embyStream(w, r, u)
	})
	secured("GET /videos/{id}/{source}/subtitles/{index}/{format}", s.embySubtitle)
	for _, p := range []string{"/sessions/playing", "/sessions/playing/progress", "/sessions/playing/stopped"} {
		secured("POST "+p, s.embyPlaying)
	}
	secured("DELETE /videos/activeencodings", func(w http.ResponseWriter, r *http.Request, u *models.User) { w.WriteHeader(204) })
}
