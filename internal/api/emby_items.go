package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"doriyamedia/internal/metadata"
	"doriyamedia/internal/models"
)

func embyPage(items []map[string]any, total, start int) map[string]any {
	return map[string]any{"Items": items, "TotalRecordCount": total, "StartIndex": start}
}
func embyPageBounds(r *http.Request) (int, int) {
	start, _ := strconv.Atoi(embyQuery(r, "StartIndex"))
	limit := 100
	if v := embyQuery(r, "Limit"); v != "" {
		if n, e := strconv.Atoi(v); e == nil {
			limit = n
		}
	}
	if start < 0 {
		start = 0
	}
	if limit < 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	return start, limit
}

// Optional dates must be absent when unknown, not empty strings or zero dates.
// The SDK's generated Swift decoder accepts whole seconds or milliseconds.
func embyDTODate(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

func embyPremiereDate(value string) (string, bool) {
	for _, layout := range []string{"2006-01-02", time.RFC3339Nano} {
		if date, err := time.Parse(layout, value); err == nil && !date.IsZero() {
			return embyDTODate(date), true
		}
	}
	return "", false
}

func (s *Server) embyLibrary(lib models.Library) map[string]any {
	typ := "movies"
	if lib.Type == "series" {
		typ = "tvshows"
	}
	out := map[string]any{"Id": "lib-" + embyID(lib.ID), "Name": lib.Name, "Type": "CollectionFolder", "CollectionType": typ, "IsFolder": true, "ServerId": s.embyServerID(), "ImageTags": map[string]string{}, "BackdropImageTags": []string{}, "UserData": map[string]any{"Played": false, "IsFavorite": false, "PlaybackPositionTicks": 0, "Key": "lib-" + embyID(lib.ID)}}
	// A library has no artwork of its own, so its cover is the newest item's
	// poster in that library. The tag is scoped to the library id, which is what
	// the client will request the image for.
	if name, err := s.Store.LibraryArtwork(&lib, "primary"); err == nil && name != "" {
		out["ImageTags"] = map[string]string{"Primary": s.embyImageAccessTag(-lib.ID, "primary", name)}
	}
	if name, err := s.Store.LibraryArtwork(&lib, "backdrop"); err == nil && name != "" {
		out["BackdropImageTags"] = []string{s.embyImageAccessTag(-lib.ID, "backdrop", name)}
	}
	return out
}
func (s *Server) embyViews(w http.ResponseWriter, r *http.Request, u *models.User) {
	libs, err := s.Store.Libraries()
	if err != nil {
		fail(w, 500, "Cannot load libraries")
		return
	}
	out := []map[string]any{}
	for _, lib := range libs {
		if !strings.HasPrefix(lib.Name, "__browse__") && (lib.Type == "movie" || lib.Type == "series") {
			out = append(out, s.embyLibrary(lib))
		}
	}
	writeJSON(w, 200, embyPage(out, len(out), 0))
}
func (s *Server) embyItem(it *models.Item, u *models.User, r *http.Request) map[string]any {
	typ := map[string]string{"movie": "Movie", "show": "Series", "season": "Season", "episode": "Episode"}[it.Type]
	folder := it.Type == "show" || it.Type == "season"
	out := map[string]any{"Id": embyID(it.ID), "Name": it.Name, "OriginalTitle": it.OriginalTitle, "SortName": it.Name, "Type": typ, "IsFolder": folder, "ServerId": s.embyServerID(), "Overview": it.Overview, "ProductionYear": it.Year, "CommunityRating": it.Rating, "LocationType": "FileSystem", "ImageTags": map[string]string{}, "BackdropImageTags": []string{}, "Genres": []string{}, "People": []any{}, "ProviderIds": map[string]string{}, "UserData": map[string]any{"Key": embyID(it.ID), "Played": false, "PlayCount": 0, "IsFavorite": false, "PlaybackPositionTicks": int64(0)}}
	// These collections have no persisted metadata yet. Emit arrays, never null
	// or fabricated entries, for clients that render them without nil guards.
	out["Chapters"] = []any{}
	out["Studios"] = []any{}
	out["Taglines"] = []string{}
	out["ExternalUrls"] = []any{}
	out["MediaSources"] = []map[string]any{}
	out["MediaStreams"] = []map[string]any{}
	out["MediaSourceCount"] = 0
	if !it.AddedAt.IsZero() {
		out["DateCreated"] = embyDTODate(it.AddedAt)
	}
	if it.ParentID != nil {
		out["ParentId"] = embyID(*it.ParentID)
	} else {
		out["ParentId"] = "lib-" + embyID(it.LibraryID)
	}
	tags := out["ImageTags"].(map[string]string)
	primaryImage := embyImageName(it, "primary")
	if primaryImage != "" {
		tags["Primary"] = s.embyImageAccessTag(it.ID, "primary", primaryImage)
	}
	if it.StillPath != "" {
		tags["Thumb"] = s.embyImageAccessTag(it.ID, "thumb", it.StillPath)
	}
	if it.BackdropPath != "" {
		out["BackdropImageTags"] = []string{s.embyImageAccessTag(it.ID, "backdrop", it.BackdropPath)}
	}
	var genres []string
	if json.Unmarshal([]byte(it.Genres), &genres) == nil && genres != nil {
		out["Genres"] = genres
	}
	providers := out["ProviderIds"].(map[string]string)
	if it.TMDBID > 0 {
		providers["Tmdb"] = strconv.Itoa(it.TMDBID)
	}
	if it.BGMID > 0 {
		providers["Bangumi"] = strconv.Itoa(it.BGMID)
	}
	if date, ok := embyPremiereDate(it.AirDate); ok {
		out["PremiereDate"] = date
	}
	if it.Type == "season" {
		out["IndexNumber"] = it.Season
	}
	if it.Type == "episode" {
		out["IndexNumber"] = it.Episode
		out["ParentIndexNumber"] = it.Season
	}
	ancestor := it
	for depth := 0; depth < 3 && ancestor.ParentID != nil; depth++ {
		parent, err := s.Store.ItemByID(*ancestor.ParentID)
		if err != nil {
			break
		}
		// Inherited images belong to their actual owner. Do not advertise them
		// in this item's ImageTags or BackdropImageTags: those URLs would 404.
		if it.BackdropPath == "" && parent.BackdropPath != "" {
			if _, exists := out["ParentBackdropItemId"]; !exists {
				out["ParentBackdropItemId"] = embyID(parent.ID)
				out["ParentBackdropImageTags"] = []string{s.embyImageAccessTag(parent.ID, "backdrop", parent.BackdropPath)}
			}
		}
		// Seasons without their own artwork show the series poster. Episodes do
		// not: reusing the series poster there gave every episode in a show the
		// same cover, which is worse than no still at all. Clients fall back to
		// their own placeholder until a real still is cached.
		if it.Type == "season" && primaryImage == "" && parent.PosterPath != "" {
			if _, exists := out["PrimaryImageItemId"]; !exists {
				out["PrimaryImageItemId"] = embyID(parent.ID)
				out["PrimaryImageTag"] = s.embyImageAccessTag(parent.ID, "primary", parent.PosterPath)
			}
		}
		if parent.Type == "season" {
			out["SeasonId"] = embyID(parent.ID)
			out["SeasonName"] = parent.Name
		}
		if parent.Type == "show" {
			out["SeriesId"] = embyID(parent.ID)
			out["SeriesName"] = parent.Name
			if parent.PosterPath != "" {
				out["SeriesPrimaryImageTag"] = s.embyImageAccessTag(parent.ID, "primary", parent.PosterPath)
			}
		}
		ancestor = parent
	}
	if !folder {
		out["MediaType"] = "Video"
		files, err := s.Store.FilesByItem(it.ID)
		if err == nil {
			sources := []map[string]any{}
			for i := range files {
				sources = append(sources, s.embyMediaSource(&files[i], r))
				if p, e := s.Store.ProgressForFile(u.ID, files[i].ID); e == nil {
					played := p.Duration > 0 && p.Position/p.Duration >= 0.9
					count := 0
					if played {
						count = 1
					}
					state := map[string]any{"Key": embyID(it.ID), "Played": played, "PlayCount": count, "IsFavorite": false, "PlaybackPositionTicks": int64(p.Position * embyTicksPerSecond), "PlayedPercentage": embyPercent(p.Position, p.Duration)}
					if !p.UpdatedAt.IsZero() {
						state["LastPlayedDate"] = embyDTODate(p.UpdatedAt)
					}
					out["UserData"] = state
				}
			}
			out["MediaSources"] = sources
			out["MediaSourceCount"] = len(sources)
			if len(sources) > 0 {
				out["RunTimeTicks"] = sources[0]["RunTimeTicks"]
				out["Container"] = sources[0]["Container"]
				out["MediaStreams"] = sources[0]["MediaStreams"]
			}
		}
	}
	state := out["UserData"].(map[string]any)
	var favorite bool
	if err := s.Store.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM emby_favorites WHERE user_id=? AND item_id=?)", u.ID, it.ID).Scan(&favorite); err == nil {
		state["IsFavorite"] = favorite
	}
	state["ItemId"] = embyID(it.ID)
	state["ServerId"] = s.embyServerID()
	if _, ok := state["PlayedPercentage"]; !ok {
		state["PlayedPercentage"] = float64(0)
	}
	return out
}
func embyPercent(position, duration float64) float64 {
	if duration <= 0 {
		return 0
	}
	return position / duration * 100
}
func (s *Server) embyItemDetail(w http.ResponseWriter, r *http.Request, u *models.User) {
	id := r.PathValue("id")
	if strings.HasPrefix(id, "lib-") {
		lib, err := s.Store.LibraryByID(embyNumber(strings.TrimPrefix(id, "lib-")))
		if err != nil {
			fail(w, 404, "Library not found")
			return
		}
		embyWriteDetail(w, r, s.embyLibrary(*lib))
		return
	}
	it, err := s.Store.ItemByID(embyNumber(id))
	if err != nil {
		fail(w, 404, "Item not found")
		return
	}
	dto := s.embyItem(it, u, r)
	// A show or season is watched only when all of its episodes are. Computing
	// this per detail request keeps list pages cheap while still giving the
	// detail header the tick the official clients look for.
	if it.Type == "show" || it.Type == "season" {
		if files, err := s.embyDescendantFiles(it.ID, map[int64]bool{it.ID: true}, 0); err == nil && len(files) > 0 {
			ud := dto["UserData"].(map[string]any)
			ud["Played"] = s.embyFolderPlayed(u.ID, files)
			if ud["Played"] == true {
				ud["PlayedPercentage"] = 100.0
			}
		}
	}
	embyWriteDetail(w, r, dto)
}

// embyItemAncestors returns the nearest parent first, followed by the library.
// Clients request this independently of item details to build navigation.
func (s *Server) embyItemAncestors(w http.ResponseWriter, r *http.Request, u *models.User) {
	id := r.PathValue("id")
	out := []map[string]any{}
	if strings.HasPrefix(id, "lib-") {
		if _, err := s.Store.LibraryByID(embyNumber(strings.TrimPrefix(id, "lib-"))); err != nil {
			fail(w, 404, "Library not found")
			return
		}
		writeJSON(w, 200, out)
		return
	}
	it, err := s.Store.ItemByID(embyNumber(id))
	if err != nil {
		fail(w, 404, "Item not found")
		return
	}
	seen := map[int64]bool{it.ID: true}
	parents := []*models.Item{}
	for it.ParentID != nil {
		if seen[*it.ParentID] {
			fail(w, 500, "Invalid item hierarchy")
			return
		}
		parent, err := s.Store.ItemByID(*it.ParentID)
		if err != nil || parent.LibraryID != it.LibraryID {
			fail(w, 500, "Invalid item hierarchy")
			return
		}
		seen[parent.ID] = true
		parents = append(parents, parent)
		it = parent
	}
	lib, err := s.Store.LibraryByID(it.LibraryID)
	if err != nil {
		fail(w, 500, "Cannot load item library")
		return
	}
	for _, parent := range parents {
		out = append(out, s.embyItem(parent, u, r))
	}
	out = append(out, s.embyLibrary(*lib))
	writeJSON(w, 200, out)
}

// embyVisibleItems is the predicate every list, latest, resume and search query
// starts from: only film and series libraries, and never the hidden file-browser
// scratchpad libraries. Keeping it in one place stops a new endpoint from
// quietly exposing the scratchpad.
const embyVisibleItems = "i.library_id IN (SELECT id FROM libraries WHERE type IN ('movie','series') AND substr(name,1,10) != '__browse__')"

// Query values are always bound; only fixed, allow-listed SQL fragments are used.
func (s *Server) embyList(w http.ResponseWriter, r *http.Request, u *models.User) {
	where := []string{embyVisibleItems}
	args := []any{}
	parent := embyQuery(r, "ParentId")
	recursive := strings.EqualFold(embyQuery(r, "Recursive"), "true") || strings.HasSuffix(r.URL.Path, "/latest")
	if v := r.PathValue("id"); v != "" {
		parent = v
		recursive = strings.HasSuffix(r.URL.Path, "/episodes")
	}
	if parent != "" {
		if strings.HasPrefix(parent, "lib-") {
			where = append(where, "i.library_id=?")
			args = append(args, embyNumber(strings.TrimPrefix(parent, "lib-")))
			if !recursive {
				where = append(where, "i.parent_id IS NULL")
			}
		} else if recursive {
			where = append(where, "i.id IN (WITH RECURSIVE children(id) AS (SELECT id FROM items WHERE parent_id=? UNION SELECT items.id FROM items JOIN children ON items.parent_id=children.id) SELECT id FROM children)")
			args = append(args, embyNumber(parent))
		} else {
			where = append(where, "i.parent_id=?")
			args = append(args, embyNumber(parent))
		}
	} else if !recursive && !strings.Contains(r.URL.Path, "/resume") {
		where = append(where, "i.parent_id IS NULL")
	}
	types := embyQuery(r, "IncludeItemTypes")
	if strings.HasSuffix(r.URL.Path, "/seasons") {
		types = "Season"
	}
	if strings.HasSuffix(r.URL.Path, "/episodes") {
		types = "Episode"
	}
	if types != "" {
		marks := []string{}
		for _, v := range strings.Split(strings.ToLower(types), ",") {
			typ := map[string]string{"movie": "movie", "series": "show", "season": "season", "episode": "episode"}[strings.TrimSpace(v)]
			if typ != "" {
				marks = append(marks, "?")
				args = append(args, typ)
			}
		}
		if len(marks) == 0 {
			where = append(where, "0")
		} else {
			where = append(where, "i.type IN ("+strings.Join(marks, ",")+")")
		}
	}
	// Emby's home screen asks the same resume endpoint twice: "Continue
	// Watching" with MediaTypes=Video and "Continue Listening" with
	// MediaTypes=Audio. Ignoring the parameter put the same films and episodes
	// in both rows, so an audiobook row showed video progress.
	if v := embyQuery(r, "MediaTypes"); v != "" {
		video := false
		for _, part := range strings.Split(strings.ToLower(v), ",") {
			if strings.TrimSpace(part) == "video" {
				video = true
			}
		}
		// Every item this adapter exposes is a video (film, series, season or
		// episode); nothing here is audio, photo, book or game.
		if !video {
			where = append(where, "0")
		}
	}
	if v := embyQuery(r, "SeasonId"); v != "" {
		where = append(where, "i.parent_id=?")
		args = append(args, embyNumber(v))
	}
	if v := embyQuery(r, "Season"); v != "" {
		where = append(where, "i.season=?")
		args = append(args, embyNumber(v))
	}
	if v := strings.TrimSpace(embyQuery(r, "SearchTerm")); v != "" {
		// Match the same spellings the search box does, so a Traditional query
		// that finds a hint also finds it in the results grid.
		conds := []string{}
		for _, spelling := range metadata.SearchVariants(v) {
			conds = append(conds, "(i.name LIKE ? OR i.original_title LIKE ?)")
			args = append(args, "%"+spelling+"%", "%"+spelling+"%")
		}
		where = append(where, "("+strings.Join(conds, " OR ")+")")
	}
	if v := embyQuery(r, "Ids"); v != "" {
		marks := []string{}
		for _, id := range strings.Split(v, ",") {
			marks = append(marks, "?")
			args = append(args, embyNumber(id))
		}
		where = append(where, "i.id IN ("+strings.Join(marks, ",")+")")
	}
	if strings.HasSuffix(r.URL.Path, "/resume") || strings.Contains(strings.ToLower(embyQuery(r, "Filters")), "isresumable") {
		where = append(where, "EXISTS (SELECT 1 FROM progress p WHERE p.item_id=i.id AND p.user_id=? AND p.position>0 AND p.duration>0 AND p.position<p.duration*0.9)")
		args = append(args, u.ID)
		// HideFromResume removes a card from Continue Watching without touching
		// the progress row, so the played state and position stay intact.
		where = append(where, "NOT EXISTS (SELECT 1 FROM resume_hidden h WHERE h.item_id=i.id AND h.user_id=?)")
		args = append(args, u.ID)
	}
	if v := embyQuery(r, "IsPlayed"); strings.EqualFold(v, "true") || strings.EqualFold(v, "false") {
		prefix := ""
		if strings.EqualFold(v, "false") {
			prefix = "NOT "
		}
		where = append(where, prefix+"EXISTS (SELECT 1 FROM progress p WHERE p.item_id=i.id AND p.user_id=? AND p.duration>0 AND p.position>=p.duration*0.9)")
		args = append(args, u.ID)
	}
	favoriteFilter := strings.ToLower(embyQuery(r, "IsFavorite"))
	for _, filter := range strings.Split(embyQuery(r, "Filters"), ",") {
		if strings.EqualFold(strings.TrimSpace(filter), "IsFavorite") || strings.EqualFold(strings.TrimSpace(filter), "IsFavoriteOrLikes") {
			favoriteFilter = "true"
		}
	}
	if favoriteFilter == "true" || favoriteFilter == "false" {
		prefix := ""
		if favoriteFilter == "false" {
			prefix = "NOT "
		}
		where = append(where, prefix+"EXISTS (SELECT 1 FROM emby_favorites fav WHERE fav.item_id=i.id AND fav.user_id=?)")
		args = append(args, u.ID)
	}
	order := "i.added_at DESC, i.id DESC"
	switch strings.ToLower(strings.Split(embyQuery(r, "SortBy"), ",")[0]) {
	case "sortname", "name":
		order = "i.name COLLATE NOCASE"
	case "productionyear":
		order = "i.year"
	case "communityrating":
		order = "i.rating"
	case "indexnumber":
		order = "i.season, i.episode"
	}
	if strings.EqualFold(embyQuery(r, "SortOrder"), "Descending") && !strings.Contains(order, "DESC") {
		order += " DESC"
	}
	if strings.HasSuffix(r.URL.Path, "/episodes") || strings.HasSuffix(r.URL.Path, "/seasons") {
		order = "i.season, i.episode, i.id"
	}
	if strings.HasSuffix(r.URL.Path, "/resume") {
		order = fmt.Sprintf("(SELECT MAX(p.updated_at) FROM progress p WHERE p.item_id=i.id AND p.user_id=%d) DESC, i.id", u.ID)
	}
	clause := strings.Join(where, " AND ")
	if strings.HasSuffix(r.URL.Path, "/latest") {
		s.embyLatest(w, r, u, clause, order, args)
		return
	}
	var total int
	if err := s.Store.DB.QueryRow("SELECT COUNT(*) FROM items i WHERE "+clause, args...).Scan(&total); err != nil {
		fail(w, 500, "Cannot count items")
		return
	}
	start, limit := embyPageBounds(r)
	queryArgs := append(append([]any{}, args...), limit, start)
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
	out := []map[string]any{}
	for _, id := range ids {
		it, err := s.Store.ItemByID(id)
		if err != nil {
			fail(w, 500, "Cannot load item")
			return
		}
		out = append(out, s.embyItem(it, u, r))
	}
	if strings.HasSuffix(r.URL.Path, "/latest") {
		writeJSON(w, 200, out)
	} else {
		writeJSON(w, 200, embyPage(out, total, start))
	}
}
func (s *Server) embyRegisterItems(secured func(string, authedHandler)) {
	for _, p := range []string{"/users/{user}/views", "/users/{user}/groupingoptions", "/library/mediafolders"} {
		secured("GET "+p, s.embyViews)
	}
	for _, p := range []string{"/items", "/users/{user}/items", "/users/{user}/items/latest", "/users/{user}/items/resume", "/shows/{id}/seasons", "/shows/{id}/episodes"} {
		secured("GET "+p, s.embyList)
	}
	secured("GET /items/{id}", s.embyItemDetail)
	secured("GET /users/{user}/items/{id}", s.embyItemDetail)
	secured("GET /items/{id}/ancestors", s.embyItemAncestors)
	secured("GET /items/counts", func(w http.ResponseWriter, r *http.Request, u *models.User) {
		out := map[string]int64{}
		for typ, key := range map[string]string{"movie": "MovieCount", "show": "SeriesCount", "episode": "EpisodeCount"} {
			n, e := s.Store.CountItems(typ)
			if e != nil {
				fail(w, 500, "Cannot count items")
				return
			}
			out[key] = n
		}
		writeJSON(w, 200, out)
	})
	// Detail clients fetch these optional collections independently. No themes
	// or critic reviews are stored, so return typed empty results, not a 404.
	for _, p := range []string{"/items/{id}/intros", "/items/{id}/similar", "/items/{id}/localtrailers", "/items/{id}/specialfeatures", "/items/{id}/themesongs", "/items/{id}/themevideos", "/items/{id}/criticreviews", "/users/{user}/items/{id}/localtrailers", "/users/{user}/items/{id}/specialfeatures", "/users/{user}/items/{id}/intros"} {
		secured("GET "+p, func(w http.ResponseWriter, r *http.Request, u *models.User) {
			if strings.HasSuffix(r.URL.Path, "/localtrailers") || strings.HasSuffix(r.URL.Path, "/specialfeatures") {
				writeJSON(w, 200, []any{})
			} else {
				writeJSON(w, 200, embyPage([]map[string]any{}, 0, 0))
			}
		})
	}
}
