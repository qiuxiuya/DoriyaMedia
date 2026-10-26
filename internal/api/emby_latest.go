package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"doriyamedia/internal/models"
)

// embyLatest applies the shared item filters to playable leaves, not the
// containers returned by grouping. In particular, IncludeItemTypes=Episode and
// IsPlayed=false select episodes even when the response contains Series DTOs.
// https://dev.emby.media/doc/restapi/Latest-Items.html
func (s *Server) embyLatest(w http.ResponseWriter, r *http.Request, u *models.User, clause, order string, args []any) {
	rows, err := s.Store.DB.Query("SELECT i.id, i.library_id, i.type, i.parent_id FROM items i WHERE "+clause+" AND i.type IN ('episode','movie') ORDER BY "+order, args...)
	if err != nil {
		fail(w, 500, "Cannot load latest items")
		return
	}
	leaves := []models.Item{}
	for rows.Next() {
		var it models.Item
		if err := rows.Scan(&it.ID, &it.LibraryID, &it.Type, &it.ParentID); err != nil {
			rows.Close()
			fail(w, 500, "Cannot read latest items")
			return
		}
		leaves = append(leaves, it)
	}
	err = rows.Err()
	rows.Close() // Close before ItemByID: SQLite may use a single connection.
	if err != nil {
		fail(w, 500, "Cannot read latest items")
		return
	}

	group := !strings.EqualFold(embyQuery(r, "GroupItems"), "false")
	ids := []int64{}
	counts := map[int64]int{}
	parents := map[int64]*models.Item{}
	for i := range leaves {
		leaf := &leaves[i]
		id := leaf.ID
		if group && leaf.Type == "episode" {
			id, err = s.embyLatestSeriesID(leaf, parents)
			if err != nil {
				fail(w, 500, "Cannot load latest container")
				return
			}
		}
		// The first occurrence is the newest matching leaf of this group.
		// Keep its position; never sort by the show's own creation date.
		if counts[id] == 0 {
			ids = append(ids, id)
		}
		counts[id]++
	}
	start, limit := embyPageBounds(r)
	if start > len(ids) {
		start = len(ids)
	}
	end := start + limit
	if end > len(ids) {
		end = len(ids)
	}
	out := []map[string]any{}
	for _, id := range ids[start:end] {
		it, err := s.Store.ItemByID(id)
		if err != nil {
			fail(w, 500, "Cannot load latest item")
			return
		}
		dto := s.embyItem(it, u, r)
		if group && it.Type == "show" {
			dto["ChildCount"] = counts[id]
		}
		out = append(out, dto)
	}
	writeJSON(w, 200, out)
}

// Episodes can belong directly to a show or through a season. Never promote a
// malformed/cross-library hierarchy into another library's visible container.
func (s *Server) embyLatestSeriesID(leaf *models.Item, cache map[int64]*models.Item) (int64, error) {
	current := leaf
	seen := map[int64]bool{leaf.ID: true}
	for current.ParentID != nil {
		id := *current.ParentID
		if seen[id] {
			break
		}
		seen[id] = true
		parent, ok := cache[id]
		if !ok {
			var err error
			parent, err = s.Store.ItemByID(id)
			if err != nil {
				if !errors.Is(err, sql.ErrNoRows) {
					return 0, err
				}
				parent = nil
			}
			cache[id] = parent
		}
		if parent == nil || parent.LibraryID != leaf.LibraryID {
			break
		}
		if parent.Type == "show" {
			return parent.ID, nil
		}
		if parent.Type != "season" {
			break
		}
		current = parent
	}
	return leaf.ID, nil
}
