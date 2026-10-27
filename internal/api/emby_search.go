package api

import (
	"net/http"
	"strings"

	"doriyamedia/internal/metadata"
	"doriyamedia/internal/models"
)

// emby_search.go: GET /Search/Hints.
//
// Emby clients do not run their search box through /Items?SearchTerm=. The web
// app, the mobile apps and the TV apps ask /Search/Hints first and render
// whatever comes back; /Items?SearchTerm= is only used afterwards to open the
// full results grid. Without this route the search tab stays empty while the
// same query returns items from /Items, which reads as "search is broken".
//
// The reply is a SearchHintResult: ItemId/Id, Name, MatchedTerm, Type,
// MediaType, IsFolder, PrimaryImageTag, ProductionYear, IndexNumber,
// ParentIndexNumber, SeriesName and RunTimeTicks. Clients use PrimaryImageTag
// with the regular /Items/{id}/Images/Primary endpoint, so the tag must be the
// one that endpoint accepts — never a fabricated one.

// embySearchHintLimit is Emby's own default for the search box. The grid that
// follows a hint click pages through /Items, so the hint list stays short.
const embySearchHintLimit = 30

// embySearchHintTypes maps the IncludeItemTypes values a client may send onto
// stored item types. Absent, the search box looks for films, series and
// episodes; seasons are navigational and would only add rows that duplicate
// their series.
func embySearchHintTypes(r *http.Request) []string {
	v := embyQuery(r, "IncludeItemTypes")
	if strings.TrimSpace(v) == "" {
		return []string{"movie", "show", "episode"}
	}
	names := map[string]string{"movie": "movie", "series": "show", "season": "season", "episode": "episode"}
	out := []string{}
	seen := map[string]bool{}
	for _, part := range strings.Split(strings.ToLower(v), ",") {
		if typ := names[strings.TrimSpace(part)]; typ != "" && !seen[typ] {
			seen[typ] = true
			out = append(out, typ)
		}
	}
	return out
}

// embySearchHint builds one hint. It is deliberately leaner than embyItem: a
// hint row only needs the identity, the type, the year, one image and a
// runtime, and building full DTOs for 30 rows would resolve media sources,
// subtitle URLs and favourites the client never reads here.
func (s *Server) embySearchHint(it *models.Item, term string) map[string]any {
	folder := it.Type == "show" || it.Type == "season"
	hint := map[string]any{
		"ItemId":      embyID(it.ID),
		"Id":          embyID(it.ID),
		"Name":        it.Name,
		"MatchedTerm": term,
		"Type":        map[string]string{"movie": "Movie", "show": "Series", "season": "Season", "episode": "Episode"}[it.Type],
		"IsFolder":    folder,
		"ServerId":    s.embyServerID(),
	}
	if it.Year > 0 {
		hint["ProductionYear"] = it.Year
	}
	if !folder {
		hint["MediaType"] = "Video"
	}
	if it.Type == "season" {
		hint["IndexNumber"] = it.Season
	}
	if it.Type == "episode" {
		hint["IndexNumber"] = it.Episode
		hint["ParentIndexNumber"] = it.Season
	}
	// One image per hint, and it must be the item's own: clients build the image
	// URL from the hint's Id, so a tag signed for a parent's poster would be
	// rejected by /Items/{id}/Images/Primary. A season or episode without
	// artwork therefore carries no tag and the client shows its placeholder.
	if name := embyImageName(it, "primary"); name != "" {
		hint["PrimaryImageTag"] = s.embyImageAccessTag(it.ID, "primary", name)
	}
	// Episodes and seasons are recognisable only by their series, so the hint
	// carries the series name that client rows display next to the title.
	ancestor := it
	for depth := 0; depth < 3 && ancestor.ParentID != nil; depth++ {
		parent, err := s.Store.ItemByID(*ancestor.ParentID)
		if err != nil {
			break
		}
		if parent.Type == "show" {
			hint["SeriesName"] = parent.Name
			hint["SeriesId"] = embyID(parent.ID)
			break
		}
		ancestor = parent
	}
	if !folder {
		if files, err := s.Store.FilesByItem(it.ID); err == nil && len(files) > 0 {
			if d := embyProbe(&files[0]).Duration(); d > 0 {
				hint["RunTimeTicks"] = int64(d * embyTicksPerSecond)
			}
		}
	}
	return hint
}

// embyRegisterSearch wires the search box. It is the only route under /Search
// this adapter serves; /Search/Hints alone is enough for the official clients,
// which then page the results grid through /Items?SearchTerm=.
func (s *Server) embyRegisterSearch(secured func(string, authedHandler)) {
	secured("GET /search/hints", s.embySearchHints)
}

func (s *Server) embySearchHints(w http.ResponseWriter, r *http.Request, u *models.User) {
	term := strings.TrimSpace(embyQuery(r, "SearchTerm"))
	start, limit := embyPageBounds(r)
	// Unlike /Items, this list has its own small default: Limit=0 (some clients
	// send it while the user is still typing) means "use the default", not
	// "return nothing".
	if embyQuery(r, "Limit") == "" || limit <= 0 {
		limit = embySearchHintLimit
	}
	if limit > 100 {
		limit = 100
	}
	empty := func() {
		writeJSON(w, 200, map[string]any{"SearchHints": []map[string]any{}, "TotalRecordCount": 0, "StartIndex": start})
	}
	// An empty term must not fall through to "everything": clients call this
	// endpoint as the user types, including after deleting the last character.
	if term == "" {
		empty()
		return
	}
	types := embySearchHintTypes(r)
	if len(types) == 0 {
		empty()
		return
	}
	where := []string{embyVisibleItems}
	args := []any{}
	marks := []string{}
	for _, typ := range types {
		marks = append(marks, "?")
		args = append(args, typ)
	}
	where = append(where, "i.type IN ("+strings.Join(marks, ",")+")")
	if parent := embyQuery(r, "ParentId"); parent != "" {
		if strings.HasPrefix(parent, "lib-") {
			where = append(where, "i.library_id=?")
			args = append(args, embyNumber(strings.TrimPrefix(parent, "lib-")))
		} else {
			where = append(where, "i.id IN (WITH RECURSIVE children(id) AS (SELECT id FROM items WHERE parent_id=? UNION SELECT items.id FROM items JOIN children ON items.parent_id=children.id) SELECT id FROM children)")
			args = append(args, embyNumber(parent))
		}
	}
	// Query values are always bound; only fixed, allow-listed SQL fragments are
	// concatenated. Matching a Traditional query against Simplified titles is
	// done by expanding the term, never by rewriting the stored title.
	conds := []string{}
	for _, v := range metadata.SearchVariants(term) {
		conds = append(conds, "(i.name LIKE ? OR i.original_title LIKE ?)")
		args = append(args, "%"+v+"%", "%"+v+"%")
	}
	where = append(where, "("+strings.Join(conds, " OR ")+")")
	clause := strings.Join(where, " AND ")
	var total int
	if err := s.Store.DB.QueryRow("SELECT COUNT(*) FROM items i WHERE "+clause, args...).Scan(&total); err != nil {
		fail(w, 500, "Cannot count items")
		return
	}
	// Exact names first, then prefix matches, then the rest: a search for
	// "流浪地球" should offer the film before "流浪地球2制作特辑".
	order := "CASE WHEN i.name = ? THEN 0 WHEN i.name LIKE ? THEN 1 ELSE 2 END, i.name COLLATE NOCASE, i.id"
	queryArgs := append(append([]any{}, args...), term, term+"%", limit, start)
	rows, err := s.Store.DB.Query("SELECT i.id FROM items i WHERE "+clause+" ORDER BY "+order+" LIMIT ? OFFSET ?", queryArgs...)
	if err != nil {
		fail(w, 500, "Cannot load items")
		return
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			fail(w, 500, "Cannot read items")
			return
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		fail(w, 500, "Cannot read items")
		return
	}
	hints := []map[string]any{}
	for _, id := range ids {
		it, err := s.Store.ItemByID(id)
		if err != nil {
			continue
		}
		hints = append(hints, s.embySearchHint(it, term))
	}
	writeJSON(w, 200, map[string]any{"SearchHints": hints, "TotalRecordCount": total, "StartIndex": start})
}
