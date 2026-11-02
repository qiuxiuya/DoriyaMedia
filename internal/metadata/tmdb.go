package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TMDB mounts its API under /3 and its images under /t/p. Both are fixed by the
// protocol, not by the deployment, so the settings page does not need to ask for
// them: a bare host gets the prefix appended here.
const (
	tmdbAPIVersion     = "/3"
	tmdbImgPrefix      = "/t/p"
	defaultTMDBAPIBase = "https://api.themoviedb.org" + tmdbAPIVersion
	defaultTMDBImgBase = "https://image.tmdb.org" + tmdbImgPrefix
)

// tmdbBaseCache remembers which form of a bare host actually answered (with the
// prefix or at the root), so a mirror only pays for the 404 discovery once.
var tmdbBaseCache sync.Map // "<prefix>|<base>" -> resolved base

type TMDBClient struct {
	APIKey   string
	Language string
	APIBase  string
	ImgBase  string
	client   *http.Client

	// altAPIBase / altImgBase carry the unversioned base to retry when the
	// configured one answers 404: a reverse proxy may serve the API at its root
	// instead of under /3. The alt is dropped as soon as one form wins, and the
	// package cache keeps later clients from repeating the attempt.
	altAPIBase string
	altImgBase string
	apiCache   string
	imgCache   string
	mu         sync.Mutex
}

func NewTMDBClient(apiKey, language, apiBase, imgBase string, client *http.Client) *TMDBClient {
	if language == "" {
		language = "zh-CN"
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	apiBase, altAPI, apiCache := tmdbBaseCandidates(apiBase, defaultTMDBAPIBase, tmdbAPIVersion)
	imgBase, altImg, imgCache := tmdbBaseCandidates(imgBase, defaultTMDBImgBase, tmdbImgPrefix)
	return &TMDBClient{
		APIKey:     apiKey,
		Language:   language,
		APIBase:    apiBase,
		ImgBase:    imgBase,
		altAPIBase: altAPI,
		altImgBase: altImg,
		apiCache:   apiCache,
		imgCache:   imgCache,
		client:     client,
	}
}

// tmdbBaseCandidates resolves a user-supplied base. A value that already names
// a path — the documented "https://api.themoviedb.org/3", "…/t/p", or any
// mirror — is used exactly as entered. A bare host gets prefix appended and
// also reports the unversioned form as the alternative to try if that 404s.
func tmdbBaseCandidates(raw, fallback, prefix string) (base, alt, cacheKey string) {
	b := strings.TrimRight(strings.TrimSpace(raw), "/")
	if b == "" {
		return fallback, "", ""
	}
	u, err := url.Parse(b)
	if err != nil || u.Host == "" || strings.Trim(u.Path, "/") != "" {
		// An explicit path (a mirror's own mount point, or the documented /3):
		// leave the user's value alone rather than guessing around it.
		return b, "", ""
	}
	key := prefix + "|" + b
	if v, ok := tmdbBaseCache.Load(key); ok {
		return v.(string), "", ""
	}
	return b + prefix, b, key
}

func (t *TMDBClient) Enabled() bool { return t != nil && t.APIKey != "" }

func (t *TMDBClient) apiBases() (base, alt string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.APIBase, t.altAPIBase
}

func (t *TMDBClient) imgBases() (base, alt string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.ImgBase, t.altImgBase
}

func (t *TMDBClient) adoptAPIBase(base string) {
	t.mu.Lock()
	if t.altAPIBase == base {
		t.APIBase, t.altAPIBase = base, ""
	}
	t.mu.Unlock()
	if t.apiCache != "" {
		tmdbBaseCache.Store(t.apiCache, base)
	}
}

func (t *TMDBClient) adoptImgBase(base string) {
	t.mu.Lock()
	if t.altImgBase == base {
		t.ImgBase, t.altImgBase = base, ""
	}
	t.mu.Unlock()
	if t.imgCache != "" {
		tmdbBaseCache.Store(t.imgCache, base)
	}
}

// confirmAPIBase / confirmImgBase record that the prefixed base works, so the
// fallback is not kept alive: a genuine 404 (a title that does not exist) must
// not cost a second request for the rest of the process's life.
func (t *TMDBClient) confirmAPIBase() {
	t.mu.Lock()
	base := t.APIBase
	t.altAPIBase = ""
	t.mu.Unlock()
	if t.apiCache != "" {
		tmdbBaseCache.Store(t.apiCache, base)
	}
}

func (t *TMDBClient) confirmImgBase() {
	t.mu.Lock()
	base := t.ImgBase
	t.altImgBase = ""
	t.mu.Unlock()
	if t.imgCache != "" {
		tmdbBaseCache.Store(t.imgCache, base)
	}
}

// tmdbHTTPError keeps the status so a 404 can be retried against the other base.
type tmdbHTTPError struct {
	what   string
	status int
	body   string
}

func (e *tmdbHTTPError) Error() string {
	if body := strings.TrimSpace(e.body); body != "" {
		return fmt.Sprintf("%s: HTTP %d %s", e.what, e.status, body)
	}
	return fmt.Sprintf("%s: HTTP %d", e.what, e.status)
}

func isTMDBStatus(err error, status int) bool {
	var he *tmdbHTTPError
	return errors.As(err, &he) && he.status == status
}

// authMode: v3 keys are 32-char hex (api_key query param); v4 read access
// tokens are long JWTs (Authorization: Bearer header). Accept both.
func (t *TMDBClient) authMode() string {
	k := strings.TrimSpace(t.APIKey)
	if strings.HasPrefix(k, "eyJ") || len(k) > 64 {
		return "bearer"
	}
	return "query"
}

func (t *TMDBClient) get(ctx context.Context, path string, params map[string]string, out any) error {
	base, alt := t.apiBases()
	err := t.getFrom(ctx, base, path, params, out)
	if err == nil {
		if alt != "" {
			t.confirmAPIBase()
		}
		return nil
	}
	if alt == "" || !isTMDBStatus(err, http.StatusNotFound) {
		return err
	}
	// The configured base serves the API at its root rather than under the
	// version prefix: remember the root so the rest of the run skips the 404.
	if altErr := t.getFrom(ctx, alt, path, params, out); altErr == nil {
		t.adoptAPIBase(alt)
		return nil
	}
	return err
}

// getFrom performs one request against an explicit base. out is filled on
// success, so a caller must only inspect it when the returned error is nil.
func (t *TMDBClient) getFrom(ctx context.Context, base, path string, params map[string]string, out any) error {
	u, err := url.Parse(base + path)
	if err != nil {
		return err
	}
	q := u.Query()
	q.Set("language", t.Language)
	for k, v := range params {
		if v != "" {
			q.Set(k, v)
		}
	}
	if t.authMode() != "bearer" {
		q.Set("api_key", strings.TrimSpace(t.APIKey))
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if t.authMode() == "bearer" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(t.APIKey))
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return &tmdbHTTPError{what: "tmdb " + path, status: resp.StatusCode, body: string(b)}
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

type TMDBSearchItem struct {
	ID            int     `json:"id"`
	Title         string  `json:"title"`
	Name          string  `json:"name"`
	OriginalTitle string  `json:"original_title"`
	OriginalName  string  `json:"original_name"`
	Overview      string  `json:"overview"`
	PosterPath    string  `json:"poster_path"`
	BackdropPath  string  `json:"backdrop_path"`
	ReleaseDate   string  `json:"release_date"`
	FirstAirDate  string  `json:"first_air_date"`
	VoteAverage   float64 `json:"vote_average"`
}

type TMDBSearchResp struct {
	Results []TMDBSearchItem `json:"results"`
}

func (t *TMDBClient) SearchMovie(ctx context.Context, query string, year int) ([]TMDBSearchItem, error) {
	params := map[string]string{"query": query, "include_adult": "false"}
	if year > 0 {
		params["year"] = strconv.Itoa(year)
	}
	var r TMDBSearchResp
	if err := t.get(ctx, "/search/movie", params, &r); err != nil {
		return nil, err
	}
	return r.Results, nil
}

func (t *TMDBClient) SearchTV(ctx context.Context, query string, year int) ([]TMDBSearchItem, error) {
	params := map[string]string{"query": query, "include_adult": "false"}
	if year > 0 {
		params["first_air_date_year"] = strconv.Itoa(year)
	}
	var r TMDBSearchResp
	if err := t.get(ctx, "/search/tv", params, &r); err != nil {
		return nil, err
	}
	return r.Results, nil
}

type TMDBGenres struct {
	Genres []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"genres"`
}

type TMDBMovieDetail struct {
	ID            int     `json:"id"`
	Title         string  `json:"title"`
	OriginalTitle string  `json:"original_title"`
	Overview      string  `json:"overview"`
	PosterPath    string  `json:"poster_path"`
	BackdropPath  string  `json:"backdrop_path"`
	ReleaseDate   string  `json:"release_date"`
	VoteAverage   float64 `json:"vote_average"`
	Runtime       int     `json:"runtime"`
	TMDBGenres
}

type TMDBTVDetail struct {
	ID              int     `json:"id"`
	Name            string  `json:"name"`
	OriginalName    string  `json:"original_name"`
	Overview        string  `json:"overview"`
	PosterPath      string  `json:"poster_path"`
	BackdropPath    string  `json:"backdrop_path"`
	FirstAirDate    string  `json:"first_air_date"`
	VoteAverage     float64 `json:"vote_average"`
	NumberOfSeasons int     `json:"number_of_seasons"`
	TMDBGenres
}

type TMDBSeasonDetail struct {
	SeasonNumber int `json:"season_number"`
	Episodes     []struct {
		EpisodeNumber int     `json:"episode_number"`
		Name          string  `json:"name"`
		Overview      string  `json:"overview"`
		StillPath     string  `json:"still_path"`
		AirDate       string  `json:"air_date"`
		VoteAverage   float64 `json:"vote_average"`
	} `json:"episodes"`
}

func (t *TMDBClient) MovieDetail(ctx context.Context, id int) (*TMDBMovieDetail, error) {
	var d TMDBMovieDetail
	if err := t.get(ctx, fmt.Sprintf("/movie/%d", id), nil, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

func (t *TMDBClient) TVDetail(ctx context.Context, id int) (*TMDBTVDetail, error) {
	var d TMDBTVDetail
	if err := t.get(ctx, fmt.Sprintf("/tv/%d", id), nil, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

func (t *TMDBClient) TVSeason(ctx context.Context, tvID, season int) (*TMDBSeasonDetail, error) {
	var d TMDBSeasonDetail
	if err := t.get(ctx, fmt.Sprintf("/tv/%d/season/%d", tvID, season), nil, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// DownloadImage fetches a TMDB image path at the given size, returns raw bytes.
func (t *TMDBClient) DownloadImage(ctx context.Context, imagePath, size string) ([]byte, error) {
	if strings.TrimSpace(imagePath) == "" {
		return nil, fmt.Errorf("no image")
	}
	base, alt := t.imgBases()
	if base == "" {
		return nil, fmt.Errorf("no image")
	}
	data, status, err := t.downloadImageFrom(ctx, base, imagePath, size)
	if err != nil {
		return nil, err
	}
	if status == 200 {
		if alt != "" {
			t.confirmImgBase()
		}
		return data, nil
	}
	// A bare image host that keeps posters at its root rather than under /t/p.
	if alt != "" && status == http.StatusNotFound {
		if altData, altStatus, altErr := t.downloadImageFrom(ctx, alt, imagePath, size); altErr == nil && altStatus == 200 {
			t.adoptImgBase(alt)
			return altData, nil
		}
	}
	return nil, fmt.Errorf("image HTTP %d", status)
}

// downloadImageFrom returns the status alongside the body so the caller can tell
// a wrong base (404) from a real failure.
func (t *TMDBClient) downloadImageFrom(ctx context.Context, base, imagePath, size string) ([]byte, int, error) {
	u := strings.TrimRight(base, "/") + "/" + size + "/" + strings.TrimPrefix(imagePath, "/")
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, resp.StatusCode, nil
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return data, resp.StatusCode, nil
}
