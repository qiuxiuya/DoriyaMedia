package api

import (
	"doriyamedia/internal/models"
	"encoding/json"
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
	// The web UI's 收藏 button and the Emby clients write the same row, so a
	// favorite made in either place shows up in the other. Unstarring a series
	// also drops any episode rows a client wrote, or the show would stay on the
	// 我的收藏 shelf with no visible star.
	if r.Method == http.MethodDelete {
		err = s.Store.ClearFavoriteTree(u.ID, id)
	} else {
		err = s.Store.SetFavorite(u.ID, id, true)
	}
	if err != nil {
		fail(w, 500, "Cannot update favorite state")
		return
	}
	writeJSON(w, 200, s.embyItem(item, u, r)["UserData"])
}

// ---------- /api/favorites ----------

// favoriteTitle resolves the row a favorite is really about. A season or an
// episode is favorited through its series: the 我的收藏 page lists films and
// series only, and starring one episode should not hide the show from the
// shelf. The walk is depth-limited so a corrupt parent cycle cannot spin here.
func (s *Server) favoriteTitle(it *models.Item) *models.Item {
	target := it
	for depth := 0; depth < 3 && target.ParentID != nil; depth++ {
		parent, err := s.Store.ItemByID(*target.ParentID)
		if err != nil {
			break
		}
		target = parent
	}
	return target
}

// handleFavorites lists the viewer's favorites for the 我的收藏 page, split the
// way the page is: films and series. An Emby client may star one episode or a
// season; that row collapses onto its title, so both clients show the same
// shelf and one title never appears twice. kind=movie|show narrows one tab.
func (s *Server) handleFavorites(w http.ResponseWriter, r *http.Request, u *models.User) {
	kind := r.URL.Query().Get("kind")
	if kind != "movie" && kind != "show" {
		kind = ""
	}
	// Newest first; the first row that resolves to a title fixes its position,
	// and later rows for the same title are duplicates.
	rows, err := s.Store.DB.Query(`SELECT i.id FROM emby_favorites fav JOIN items i ON i.id=fav.item_id
		WHERE fav.user_id=? AND i.library_id IN (SELECT id FROM libraries WHERE name NOT LIKE '__browse__%')
		ORDER BY fav.created_at DESC, fav.item_id DESC`, u.ID)
	if err != nil {
		fail(w, 500, "Cannot load favorites")
		return
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			fail(w, 500, "Cannot read favorites")
			return
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		fail(w, 500, "Cannot read favorites")
		return
	}
	items := make([]models.Item, 0, len(ids))
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		it, err := s.Store.ItemByID(id)
		if err != nil {
			continue // the row outlived its item; skip rather than fail the page
		}
		title := s.favoriteTitle(it)
		if title.Type != "movie" && title.Type != "show" {
			continue
		}
		if kind != "" && title.Type != kind {
			continue
		}
		if seen[title.ID] {
			continue
		}
		seen[title.ID] = true
		items = append(items, *title)
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": len(items)})
}

// handleFavoriteItem toggles one favorite from the web UI. GET reports the
// current state; PUT with {"favorite":bool} writes it. Both answer with the
// state so a button can settle on the server's truth.
func (s *Server) handleFavoriteItem(w http.ResponseWriter, r *http.Request, u *models.User) {
	id, ok := pathID(r)
	if !ok || id <= 0 {
		fail(w, 404, "条目不存在")
		return
	}
	item, err := s.Store.ItemByID(id)
	if err != nil {
		fail(w, 404, "条目不存在")
		return
	}
	target := s.favoriteTitle(item)
	if r.Method == http.MethodGet {
		// A row an Emby client wrote on one episode still lights the title's
		// star, because that episode is what put the show on the shelf.
		on, err := s.Store.IsFavoriteTree(u.ID, target.ID)
		if err != nil {
			fail(w, 500, "Cannot read favorite state")
			return
		}
		writeJSON(w, 200, map[string]any{"item_id": target.ID, "favorite": on})
		return
	}
	var body struct {
		Favorite *bool `json:"favorite"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Favorite == nil {
		fail(w, 400, "缺少 favorite 参数")
		return
	}
	if *body.Favorite {
		if err := s.Store.SetFavorite(u.ID, target.ID, true); err != nil {
			fail(w, 500, "Cannot update favorite state")
			return
		}
	} else if err := s.Store.ClearFavoriteTree(u.ID, target.ID); err != nil {
		fail(w, 500, "Cannot update favorite state")
		return
	}
	writeJSON(w, 200, map[string]any{"item_id": target.ID, "favorite": *body.Favorite})
}
