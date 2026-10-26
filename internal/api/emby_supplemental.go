package api

import (
	"doriyamedia/internal/models"
	"net/http"
)

func (s *Server) embyRegisterSupplemental(secured func(string, authedHandler)) {
	secured("GET /livetv/info", func(w http.ResponseWriter, r *http.Request, u *models.User) {
		writeJSON(w, 200, map[string]any{"IsEnabled": false, "EnabledUsers": []string{}, "Services": []any{}})
	})
	// No tuner/channel backend is implemented. Clients may still query this
	// collection during home/detail navigation despite the disabled policy.
	secured("GET /livetv/channels", func(w http.ResponseWriter, r *http.Request, u *models.User) {
		start, _ := embyPageBounds(r)
		writeJSON(w, 200, embyPage([]map[string]any{}, 0, start))
	})
	secured("GET /videos/{id}/additionalparts", func(w http.ResponseWriter, r *http.Request, u *models.User) {
		if _, err := s.Store.ItemByID(embyNumber(r.PathValue("id"))); err != nil {
			fail(w, 404, "Item not found")
			return
		}
		// FilesByItem describes alternative versions, not ordered multipart videos.
		writeJSON(w, 200, embyPage([]map[string]any{}, 0, 0))
	})
}
