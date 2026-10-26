package api

import (
	"net/http"
	"strings"

	"doriyamedia/internal/models"
)

func (s *Server) embyRegisterUserState(secured func(string, authedHandler)) {
	secured("GET /shows/nextup", s.embyNextUp)
	secured("POST /users/{user}/favoriteitems/{id}", s.embyFavorite)
	secured("DELETE /users/{user}/favoriteitems/{id}", s.embyFavorite)
	secured("POST /users/{user}/favoriteitems/{id}/delete", func(w http.ResponseWriter, r *http.Request, u *models.User) {
		req := r.Clone(r.Context())
		req.Method = http.MethodDelete
		s.embyFavorite(w, req, u)
	})
	for _, method := range []string{"POST", "DELETE"} {
		secured(method+" /users/{user}/playeditems/{id}", s.embyPlayed)
	}
	// Some clients tunnel DELETE through POST to a /Delete suffix. Preserve
	// the existing unwatch logic; a plain POST here would mark the item played.
	secured("POST /users/{user}/playeditems/{id}/delete", func(w http.ResponseWriter, r *http.Request, u *models.User) {
		req := r.Clone(r.Context())
		req.Method = http.MethodDelete
		s.embyPlayed(w, req, u)
	})
	secured("GET /items/{id}/thememedia", func(w http.ResponseWriter, r *http.Request, u *models.User) {
		writeJSON(w, 200, map[string]any{"ThemeSongsResult": embyPage([]map[string]any{}, 0, 0), "ThemeVideosResult": embyPage([]map[string]any{}, 0, 0)})
	})
}

// NextUp picks the first uncompleted episode after the last completed episode
// of a series the viewer has actually started. Unwatched series are not added.
func (s *Server) embyNextUp(w http.ResponseWriter, r *http.Request, u *models.User) {
	libs, err := s.Store.Libraries()
	if err != nil {
		fail(w, 500, "Cannot load libraries")
		return
	}
	out := []map[string]any{}
	for _, lib := range libs {
		if lib.Type != "series" || strings.HasPrefix(lib.Name, "__browse__") {
			continue
		}
		if parent := embyQuery(r, "ParentId"); parent != "" && parent != "lib-"+embyID(lib.ID) {
			continue
		}
		shows, _, err := s.Store.ItemsByLibrary(lib.ID, "show", "", "name", true, 0, -1)
		if err != nil {
			fail(w, 500, "Cannot load series")
			return
		}
		for _, show := range shows {
			if id := embyQuery(r, "SeriesId"); id != "" && id != embyID(show.ID) {
				continue
			}
			rows, err := s.Store.DB.Query(`WITH RECURSIVE descendants(id) AS (
    SELECT id FROM items WHERE parent_id=? UNION SELECT i.id FROM items i JOIN descendants d ON i.parent_id=d.id
   ) SELECT i.id,COALESCE(MAX(CASE WHEN p.duration>0 AND p.position>=p.duration*0.9 THEN 1 ELSE 0 END),0),COALESCE(MAX(p.position),0)
    FROM items i LEFT JOIN progress p ON p.item_id=i.id AND p.user_id=?
    WHERE i.id IN (SELECT id FROM descendants) AND i.type='episode'
    GROUP BY i.id ORDER BY i.season,i.episode,i.id`, show.ID, u.ID)
			if err != nil {
				fail(w, 500, "Cannot load next episode")
				return
			}
			type episode struct {
				id       int64
				played   bool
				position float64
			}
			episodes := []episode{}
			started := false
			lastPlayed := -1
			for rows.Next() {
				var ep episode
				var played int
				if err := rows.Scan(&ep.id, &played, &ep.position); err != nil {
					rows.Close()
					fail(w, 500, "Cannot read episodes")
					return
				}
				ep.played = played != 0
				if ep.position > 0 || ep.played {
					started = true
				}
				if ep.played {
					lastPlayed = len(episodes)
				}
				episodes = append(episodes, ep)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				fail(w, 500, "Cannot read episodes")
				return
			}
			if !started {
				continue
			}
			for i := lastPlayed + 1; i < len(episodes); i++ {
				if !episodes[i].played {
					it, e := s.Store.ItemByID(episodes[i].id)
					if e != nil {
						fail(w, 500, "Cannot load episode")
						return
					}
					out = append(out, s.embyItem(it, u, r))
					break
				}
			}
		}
	}
	total := len(out)
	start, limit := embyPageBounds(r)
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	writeJSON(w, 200, embyPage(out[start:end], total, start))
}

func (s *Server) embyPlayed(w http.ResponseWriter, r *http.Request, u *models.User) {
	itemID := embyNumber(r.PathValue("id"))
	it, err := s.Store.ItemByID(itemID)
	if err != nil {
		fail(w, 404, "Item not found")
		return
	}
	files, err := s.Store.FilesByItem(itemID)
	if err != nil {
		fail(w, 500, "Cannot load media files")
		return
	}
	if len(files) == 0 {
		fail(w, 400, "Only playable items can be marked watched")
		return
	}
	played := r.Method == "POST"
	for _, f := range files {
		duration := embyProbe(&f).Duration()
		if duration <= 0 {
			if p, e := s.Store.ProgressForFile(u.ID, f.ID); e == nil {
				duration = p.Duration
			}
		}
		if played && duration <= 0 {
			fail(w, 409, "Play or probe the item before marking watched")
			return
		}
	}
	for _, f := range files {
		if !played {
			if _, err := s.Store.DB.Exec("DELETE FROM progress WHERE user_id=? AND file_id=?", u.ID, f.ID); err != nil {
				fail(w, 500, "Cannot clear watched state")
				return
			}
			continue
		}
		duration := embyProbe(&f).Duration()
		if duration <= 0 {
			p, _ := s.Store.ProgressForFile(u.ID, f.ID)
			duration = p.Duration
		}
		if err := s.Store.SaveProgress(&models.Progress{UserID: u.ID, FileID: f.ID, ItemID: itemID, Position: duration, Duration: duration}); err != nil {
			fail(w, 500, "Cannot save watched state")
			return
		}
		if s.BangumiSync != nil {
			_ = s.BangumiSync.EnqueueWatched(u.ID, f.ID)
		}
	}
	writeJSON(w, 200, s.embyItem(it, u, r)["UserData"])
}
