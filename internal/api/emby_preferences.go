package api

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"doriyamedia/internal/models"
)

// Preferences are scoped by authenticated user, preference id and client.
// Official clients await this endpoint before resolving authenticateUserByName.
func (s *Server) embyDisplayPreferences(w http.ResponseWriter, r *http.Request, u *models.User) {
	id, client := r.PathValue("id"), embyQuery(r, "Client")
	if len(id) > 256 || len(client) > 256 {
		fail(w, 400, "Invalid display preferences key")
		return
	}
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		var data map[string]json.RawMessage
		if err := readBody(r, &data); err != nil || data == nil {
			fail(w, 400, "Invalid display preferences")
			return
		}
		// The route identifies the record; never accept a body-supplied user id.
		data["Id"], _ = json.Marshal(id)
		data["Client"], _ = json.Marshal(client)
		if v, ok := data["CustomPrefs"]; ok {
			var prefs map[string]string
			if json.Unmarshal(v, &prefs) != nil {
				fail(w, 400, "CustomPrefs must contain string values")
				return
			}
			if prefs == nil {
				data["CustomPrefs"] = json.RawMessage(`{}`)
			}
		} else {
			data["CustomPrefs"] = json.RawMessage(`{}`)
		}
		raw, err := json.Marshal(data)
		if err != nil {
			fail(w, 400, "Invalid display preferences")
			return
		}
		_, err = s.Store.DB.Exec(`INSERT INTO emby_display_preferences(user_id,preference_id,client,data) VALUES(?,?,?,?) ON CONFLICT(user_id,preference_id,client) DO UPDATE SET data=excluded.data`, u.ID, id, client, string(raw))
		if err != nil {
			fail(w, 500, "Cannot save display preferences")
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var raw string
	err := s.Store.DB.QueryRow(`SELECT data FROM emby_display_preferences WHERE user_id=? AND preference_id=? AND client=?`, u.ID, id, client).Scan(&raw)
	if err == sql.ErrNoRows {
		writeJSON(w, 200, map[string]any{"Id": id, "Client": client, "SortBy": "SortName", "SortOrder": "Ascending", "CustomPrefs": map[string]string{}})
		return
	}
	if err != nil || !json.Valid([]byte(raw)) {
		fail(w, 500, "Cannot load display preferences")
		return
	}
	writeJSON(w, 200, json.RawMessage(raw))
}
