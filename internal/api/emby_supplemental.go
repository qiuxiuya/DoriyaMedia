package api

import (
	"net/http"

	"doriyamedia/internal/models"
)

// embyGetListPage is the shared shape for the official dashboard list endpoints
// that this server deliberately leaves empty. They must answer with the standard
// QueryResult envelope: several clients call .Items.map(...) without a guard and
// break on 404 or on a bare JSON array.
func embyEmptyPage(r *http.Request) map[string]any {
	start, _ := embyPageBounds(r)
	return embyPage([]map[string]any{}, 0, start)
}

func (s *Server) embyRegisterSupplemental(secured func(string, authedHandler)) {
	secured("GET /livetv/info", func(w http.ResponseWriter, r *http.Request, u *models.User) {
		writeJSON(w, 200, map[string]any{"IsEnabled": false, "EnabledUsers": []string{}, "Services": []any{}})
	})
	// No tuner/channel backend is implemented. Clients may still query these
	// collections during home/detail navigation despite the disabled policy, and
	// the official clients expect a QueryResult rather than a 404.
	for _, p := range []string{"/livetv/channels", "/livetv/programs", "/livetv/recordings", "/livetv/timers", "/livetv/series"} {
		secured("GET "+p, func(w http.ResponseWriter, r *http.Request, u *models.User) {
			writeJSON(w, 200, embyEmptyPage(r))
		})
	}
	secured("GET /livetv/programs/recommended", func(w http.ResponseWriter, r *http.Request, u *models.User) {
		writeJSON(w, 200, []map[string]any{})
	})
	secured("GET /videos/{id}/additionalparts", func(w http.ResponseWriter, r *http.Request, u *models.User) {
		if _, err := s.Store.ItemByID(embyNumber(r.PathValue("id"))); err != nil {
			fail(w, 404, "Item not found")
			return
		}
		// FilesByItem describes alternative versions, not ordered multipart videos.
		writeJSON(w, 200, embyPage([]map[string]any{}, 0, 0))
	})
	// Trickplay images are never generated, so the set is empty. The official
	// clients poll this during playback; a 404 makes them retry every second.
	secured("GET /items/{id}/thumbnailset", func(w http.ResponseWriter, r *http.Request, u *models.User) {
		if _, err := s.Store.ItemByID(embyNumber(r.PathValue("id"))); err != nil {
			fail(w, 404, "Item not found")
			return
		}
		writeJSON(w, 200, map[string]any{"Thumbnails": []any{}})
	})
	s.embyRegisterAdmin(secured)
}

// embyRegisterAdmin answers the server-management endpoints the official web
// client calls when an administrator opens the dashboard. Nothing here is
// configurable through Emby, so every collection is empty instead of 404.
func (s *Server) embyRegisterAdmin(secured func(string, authedHandler)) {
	secured("GET /scheduledtasks", func(w http.ResponseWriter, r *http.Request, u *models.User) {
		writeJSON(w, 200, []map[string]any{})
	})
	secured("GET /system/activitylog/entries", func(w http.ResponseWriter, r *http.Request, u *models.User) {
		writeJSON(w, 200, embyEmptyPage(r))
	})
	secured("GET /web/configurationpages", func(w http.ResponseWriter, r *http.Request, u *models.User) {
		writeJSON(w, 200, []map[string]any{})
	})
	secured("GET /localization/cultures", func(w http.ResponseWriter, r *http.Request, u *models.User) {
		writeJSON(w, 200, embyCultures())
	})
	secured("GET /localization/countries", func(w http.ResponseWriter, r *http.Request, u *models.User) {
		writeJSON(w, 200, []map[string]any{})
	})
	secured("GET /localization/parentalratings", func(w http.ResponseWriter, r *http.Request, u *models.User) {
		writeJSON(w, 200, []map[string]any{})
	})
}

// embyCultures lists the display languages the official clients offer. Only the
// two the interface is actually written in are advertised.
func embyCultures() []map[string]any {
	return []map[string]any{
		{"Name": "zh-CN", "DisplayName": "Chinese (Simplified)", "TwoLetterISOLanguageName": "zh", "ThreeLetterISOLanguageName": "zho", "ThreeLetterISOLanguageNames": []string{"zho", "chi"}, "TwoLetterISOLanguageNames": []string{"zh"}},
		{"Name": "en-US", "DisplayName": "English", "TwoLetterISOLanguageName": "en", "ThreeLetterISOLanguageName": "eng", "ThreeLetterISOLanguageNames": []string{"eng"}, "TwoLetterISOLanguageNames": []string{"en"}},
	}
}
