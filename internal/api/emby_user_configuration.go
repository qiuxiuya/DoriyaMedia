package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"doriyamedia/internal/models"
)

// embyUserConfigurationKeys is the documented UserConfiguration surface. The
// store is restricted to it so a client cannot grow the row with arbitrary JSON.
var embyUserConfigurationKeys = map[string]bool{
	"AudioLanguagePreference":    true,
	"PlayDefaultAudioTrack":      true,
	"SubtitleLanguagePreference": true,
	"SubtitleMode":               true,
	"DisplayMissingEpisodes":     true,
	"OrderedViews":               true,
	"LatestItemsExcludes":        true,
	"MyMediaExcludes":            true,
	"GroupedFolders":             true,
	"ExcludeFoldersFromGrouping": true,
	"HidePlayedInLatest":         true,
	"HidePlayedInMoreLikeThis":   true,
	"HidePlayedInSuggestions":    true,
	"RememberAudioSelections":    true,
	"RememberSubtitleSelections": true,
	"EnableNextEpisodeAutoPlay":  true,
	"ResumeRewindSeconds":        true,
	"IntroSkipMode":              true,
}

// embyUserConfiguration merges the stored preferences over protocol defaults.
// Array members are always present: the official clients call .includes() on
// them without a null check.
func (s *Server) embyUserConfiguration(u *models.User) map[string]any {
	cfg := map[string]any{
		"PlayDefaultAudioTrack":      true,
		"SubtitleMode":               "Default",
		"OrderedViews":               []any{},
		"MyMediaExcludes":            []any{},
		"LatestItemsExcludes":        []any{},
		"GroupedFolders":             []any{},
		"ExcludeFoldersFromGrouping": []any{},
		"HidePlayedInLatest":         true,
		"RememberAudioSelections":    true,
		"RememberSubtitleSelections": true,
		"EnableNextEpisodeAutoPlay":  true,
	}
	if raw, err := s.Store.EmbyUserConfiguration(u.ID); err == nil && raw != "" {
		var saved map[string]any
		if json.Unmarshal([]byte(raw), &saved) == nil {
			for k, v := range saved {
				if embyUserConfigurationKeys[k] {
					cfg[k] = v
				}
			}
		}
	}
	for _, k := range []string{"OrderedViews", "MyMediaExcludes", "LatestItemsExcludes", "GroupedFolders", "ExcludeFoldersFromGrouping"} {
		if _, ok := cfg[k].([]any); !ok {
			cfg[k] = []any{}
		}
	}
	return cfg
}

// embyUserConfigurationSave persists the settings the client posts after the
// user edits playback preferences. Unknown keys are dropped; malformed or
// non-object bodies are rejected rather than stored. The Partial variant merges
// over what is already saved, because clients use it to change one field.
func (s *Server) embyUserConfigurationSave(w http.ResponseWriter, r *http.Request, u *models.User) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var body map[string]any
	if err := readBody(r, &body); err != nil || body == nil {
		fail(w, 400, "Invalid user configuration")
		return
	}
	saved := map[string]any{}
	if strings.HasSuffix(strings.ToLower(r.URL.Path), "/partial") {
		if existing, err := s.Store.EmbyUserConfiguration(u.ID); err == nil && existing != "" {
			_ = json.Unmarshal([]byte(existing), &saved)
		}
	}
	for k, v := range body {
		if embyUserConfigurationKeys[k] {
			saved[k] = v
		}
	}
	raw, err := json.Marshal(saved)
	if err != nil {
		fail(w, 400, "Invalid user configuration")
		return
	}
	if err := s.Store.SaveEmbyUserConfiguration(u.ID, string(raw)); err != nil {
		fail(w, 500, "Cannot save user configuration")
		return
	}
	writeJSON(w, 200, s.embyUserConfiguration(u))
}
