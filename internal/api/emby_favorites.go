package api

import (
	"doriyamedia/internal/models"
	"net/http"
	"strconv"
)

func (s *Server) embyFavorite(w http.ResponseWriter, r *http.Request, u *models.User) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		fail(w, 404, "Item not found")
		return
	}
	item, err := s.Store.ItemByID(id)
	if err != nil {
		fail(w, 404, "Item not found")
		return
	}
	if r.Method == http.MethodDelete {
		_, err = s.Store.DB.Exec("DELETE FROM emby_favorites WHERE user_id=? AND item_id=?", u.ID, id)
	} else {
		_, err = s.Store.DB.Exec("INSERT OR IGNORE INTO emby_favorites (user_id,item_id) VALUES (?,?)", u.ID, id)
	}
	if err != nil {
		fail(w, 500, "Cannot update favorite state")
		return
	}
	writeJSON(w, 200, s.embyItem(item, u, r)["UserData"])
}
