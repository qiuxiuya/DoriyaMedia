package api

import (
	"strings"

	"doriyamedia/internal/models"
)

// Official clients draw the "remaining episodes" badge on a series or season
// poster from UserData.UnplayedItemCount, and read RecursiveItemCount for the
// total. A folder DTO that omits both is rendered as a plain cover with no
// badge at all, which is why the list pages looked empty of progress even
// though the episodes were there. One recursive query answers every folder on
// the page, so a 100-item grid stays one extra statement.
//
// An episode counts as watched when every one of its files is marked played —
// the same rule embyFolderPlayed uses, so the grid and the detail header can
// never disagree. Episodes that carry no file yet (an announced B站 episode, an
// unscanned entry) still count as remaining: they exist and nobody watched them.
func (s *Server) embyFolderCounts(userID int64, folderIDs []int64) (total, unplayed map[int64]int) {
	total, unplayed = map[int64]int{}, map[int64]int{}
	if len(folderIDs) == 0 {
		return total, unplayed
	}
	marks := make([]string, 0, len(folderIDs))
	args := make([]any, 0, len(folderIDs)+1)
	for _, id := range folderIDs {
		marks = append(marks, "?")
		args = append(args, id)
	}
	args = append(args, userID)
	rows, err := s.Store.DB.Query(`WITH RECURSIVE descendants(root_id, id) AS (
    SELECT id, id FROM items WHERE id IN (`+strings.Join(marks, ",")+`)
    UNION
    SELECT d.root_id, i.id FROM items i JOIN descendants d ON i.parent_id=d.id
  ) SELECT d.root_id,
      COALESCE(SUM(CASE WHEN i.type='episode' THEN 1 ELSE 0 END), 0),
      COALESCE(SUM(CASE WHEN i.type='episode'
          AND EXISTS (SELECT 1 FROM files f WHERE f.item_id=i.id)
          AND NOT EXISTS (SELECT 1 FROM files f WHERE f.item_id=i.id AND NOT EXISTS (
                SELECT 1 FROM progress p WHERE p.file_id=f.id AND p.user_id=? AND p.played=1))
        THEN 1 ELSE 0 END), 0)
    FROM descendants d JOIN items i ON i.id=d.id
    GROUP BY d.root_id`, args...)
	if err != nil {
		return total, unplayed
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var t, p int
		if rows.Scan(&id, &t, &p) != nil {
			return total, unplayed
		}
		total[id] = t
		unplayed[id] = t - p
	}
	return total, unplayed
}

// embyApplyFolderCounts fills the badge fields on the folder DTOs of a page.
// items and dtos are parallel; entries that are not a series or season are
// skipped. A folder with no episode beneath it gets neither a count nor a
// watched tick: "0 unplayed" would read as finished.
func (s *Server) embyApplyFolderCounts(items []*models.Item, dtos []map[string]any, u *models.User) {
	ids := []int64{}
	index := map[int64]int{}
	for i, it := range items {
		if it == nil || (it.Type != "show" && it.Type != "season") {
			continue
		}
		ids = append(ids, it.ID)
		index[it.ID] = i
	}
	if len(ids) == 0 {
		return
	}
	total, unplayed := s.embyFolderCounts(u.ID, ids)
	for id, i := range index {
		n := total[id]
		if n == 0 {
			continue
		}
		dto := dtos[i]
		dto["RecursiveItemCount"] = n
		ud, ok := dto["UserData"].(map[string]any)
		if !ok {
			continue
		}
		ud["UnplayedItemCount"] = unplayed[id]
		if unplayed[id] == 0 {
			ud["Played"] = true
			ud["PlayedPercentage"] = 100.0
		}
	}
}
