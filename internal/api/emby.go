package api

// Emby compatibility is an adapter over the native store and playback engine.
// MeBox's public route layout and case-insensitive compatibility behavior were
// used as protocol references; no upstream implementation is vendored.
import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"doriyamedia/internal/auth"
	"doriyamedia/internal/models"
)

const embyTicksPerSecond = 10000000

var embyTokenParameter = regexp.MustCompile(`(?i)(?:^|[,\s])Token\s*=\s*"([^"]+)"`)

func embyID(id int64) string    { return strconv.FormatInt(id, 10) }
func embyNumber(v string) int64 { n, _ := strconv.ParseInt(v, 10, 64); return n }
func embyQuery(r *http.Request, name string) string {
	for k, v := range r.URL.Query() {
		if strings.EqualFold(k, name) && len(v) > 0 {
			return v[0]
		}
	}
	return ""
}
func embyToken(r *http.Request) string {
	for _, h := range []string{"X-Emby-Token", "X-MediaBrowser-Token"} {
		if v := r.Header.Get(h); v != "" {
			return v
		}
	}
	for _, h := range []string{"X-Emby-Authorization", "Authorization"} {
		v := r.Header.Get(h)
		if strings.HasPrefix(strings.ToLower(v), "bearer ") {
			return strings.TrimSpace(v[7:])
		}
		if m := embyTokenParameter.FindStringSubmatch(v); len(m) > 1 {
			return m[1]
		}
	}
	for _, k := range []string{"api_key", "X-Emby-Token"} {
		if v := embyQuery(r, k); v != "" {
			return v
		}
	}
	return sessionToken(r)
}
func (s *Server) embyServerID() string {
	h := sha256.Sum256([]byte("doriyamedia/emby/" + s.Cfg.Secret))
	return hex.EncodeToString(h[:16])
}
func (s *Server) embySystemInfo() map[string]any {
	return map[string]any{"Id": s.embyServerID(), "ServerName": "DoriyaMedia", "Version": "4.8.0.0", "OperatingSystem": "DoriyaMedia", "ProductName": "DoriyaMedia", "StartupWizardCompleted": true, "SupportsLibraryMonitor": false, "SupportsLocalPortConfiguration": false}
}
func (s *Server) embyUser(u *models.User) map[string]any {
	return map[string]any{"Id": embyID(u.ID), "Name": u.Name, "ServerId": s.embyServerID(), "HasPassword": true, "HasConfiguredPassword": true, "EnableAutoLogin": false, "Configuration": s.embyUserConfiguration(u), "Policy": map[string]any{"IsAdministrator": u.IsAdmin(), "IsDisabled": !u.CanLogin(), "EnableMediaPlayback": true, "EnableLiveTvAccess": false, "EnableLiveTvManagement": false, "EnableAudioPlaybackTranscoding": false, "EnableVideoPlaybackTranscoding": false, "EnableContentDeletion": false, "EnableAllFolders": true}}
}

func (s *Server) embyMiddleware(next http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /system/info/public", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, s.embySystemInfo()) })
	mux.HandleFunc("GET /users/public", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, []any{}) })
	mux.HandleFunc("POST /users/authenticatebyname", s.embyLogin)
	// This is loaded as a script, often before authentication. No customization
	// scripts exist here: return executable empty JS, never JSON or SPA HTML.
	mux.HandleFunc("GET /customcssjs/scripts", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/system/ping", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" && r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "Emby Server")
	})
	secured := func(pattern string, h authedHandler) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			token := embyToken(r)
			sess, err := s.Store.SessionByToken(token)
			if token == "" || err != nil || !time.Now().Before(sess.ExpiresAt) {
				fail(w, 401, "Invalid access token")
				return
			}
			u, err := s.Store.UserByID(sess.UserID)
			if err != nil || !u.CanLogin() || (s.Cfg.EmailVerificationRequired() && u.EmailPending()) {
				fail(w, 401, "Account unavailable")
				return
			}
			if id := r.PathValue("user"); id != "" && id != embyID(u.ID) {
				fail(w, 403, "Cannot access another user's data")
				return
			}
			if id := embyQuery(r, "UserId"); id != "" && id != embyID(u.ID) {
				fail(w, 403, "Cannot access another user's data")
				return
			}
			h(w, r, u)
		})
	}
	secured("GET /system/info", func(w http.ResponseWriter, r *http.Request, u *models.User) { writeJSON(w, 200, s.embySystemInfo()) })
	secured("GET /users/me", func(w http.ResponseWriter, r *http.Request, u *models.User) { writeJSON(w, 200, s.embyUser(u)) })
	secured("GET /users/{user}", func(w http.ResponseWriter, r *http.Request, u *models.User) { writeJSON(w, 200, s.embyUser(u)) })
	secured("GET /users", func(w http.ResponseWriter, r *http.Request, u *models.User) { writeJSON(w, 200, []any{s.embyUser(u)}) })
	secured("POST /sessions/logout", func(w http.ResponseWriter, r *http.Request, u *models.User) {
		if err := s.Store.DeleteSession(embyToken(r)); err != nil {
			fail(w, 500, "Logout failed")
			return
		}
		w.WriteHeader(204)
	})
	for _, p := range []string{"/sessions/capabilities", "/sessions/capabilities/full"} {
		secured("POST "+p, func(w http.ResponseWriter, r *http.Request, u *models.User) { w.WriteHeader(204) })
	}
	secured("GET /sessions", func(w http.ResponseWriter, r *http.Request, u *models.User) {
		writeJSON(w, 200, []any{s.embySession(r, u)})
	})
	secured("GET /branding/configuration", func(w http.ResponseWriter, r *http.Request, u *models.User) { writeJSON(w, 200, map[string]any{}) })
	secured("POST /users/{user}/configuration", s.embyUserConfigurationSave)
	secured("POST /users/{user}/configuration/partial", s.embyUserConfigurationSave)
	secured("GET /displaypreferences/{id}", s.embyDisplayPreferences)
	secured("POST /displaypreferences/{id}", s.embyDisplayPreferences)
	s.embyRegisterItems(secured)
	s.embyRegisterSupplemental(secured)
	s.embyRegisterImages(mux)
	s.embyRegisterPlayback(secured)
	s.embyRegisterUserState(secured)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimRight(strings.ToLower(r.URL.Path), "/")
		prefixed := false
		// Strip the base path at most twice: some clients prepend /emby to URLs
		// the server already built, yielding /emby/emby/Videos/... . Accepting
		// that keeps an older client working without weakening routing.
		for pass := 0; pass < 2; pass++ {
			stripped := false
			for _, prefix := range []string{"/emby", "/mediabrowser"} {
				if path == prefix || strings.HasPrefix(path, prefix+"/") {
					path = strings.TrimPrefix(path, prefix)
					prefixed = true
					stripped = true
					break
				}
			}
			if !stripped {
				break
			}
		}
		root := strings.Split(strings.TrimPrefix(path, "/"), "/")[0]
		known := false
		for _, v := range []string{"system", "users", "items", "shows", "videos", "sessions", "library", "branding", "displaypreferences", "genres", "persons", "customcssjs", "livetv", "localization", "scheduledtasks", "web"} {
			if root == v {
				known = true
				break
			}
		}
		// /library/{numeric ID} belongs to the existing Vue application.
		if !prefixed && root == "library" && path != "/library/mediafolders" {
			known = false
		}
		if !prefixed && !known {
			next.ServeHTTP(w, r)
			return
		}
		req := r.Clone(r.Context())
		u := *r.URL
		req.URL = &u
		req.URL.Path = path
		req.URL.RawPath = ""
		w.Header().Set("X-Emby-ServerId", s.embyServerID())
		w.Header().Set("Cache-Control", "no-store")
		// Hosted official web clients are cross-origin. Do not enable cookie
		// credentials or reflect arbitrary origins into authenticated responses.
		w.Header().Add("Vary", "Origin")
		if origin := r.Header.Get("Origin"); origin == "https://app.emby.media" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Expose-Headers", "X-Emby-ServerId")
			if r.Method == http.MethodOptions {
				w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, POST, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Emby-Authorization, X-Emby-Token, X-MediaBrowser-Token, Range")
				w.Header().Add("Vary", "Access-Control-Request-Method")
				w.Header().Add("Vary", "Access-Control-Request-Headers")
				embyServeLogged(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					r.Pattern = "OPTIONS Emby CORS preflight"
					w.WriteHeader(http.StatusNoContent)
				}), w, req)
				return
			}
		}
		embyServeLogged(mux, w, req)
	})
}
func (s *Server) embySession(r *http.Request, u *models.User) map[string]any {
	h := sha256.Sum256([]byte(embyToken(r)))
	return map[string]any{"Id": hex.EncodeToString(h[:16]), "UserId": embyID(u.ID), "UserName": u.Name, "ServerId": s.embyServerID(), "SupportsRemoteControl": false, "PlayableMediaTypes": []string{"Video"}, "AdditionalUsers": []any{}, "SupportedCommands": []string{}}
}
func (s *Server) embyLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string
		Pw       *string
		Password string // Legacy SHA1; never treat it as a plaintext password.
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	contentType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	var decodeErr error
	if contentType == "application/x-www-form-urlencoded" {
		defer r.Body.Close()
		decodeErr = r.ParseForm()
		if decodeErr == nil {
			// Use PostForm, not Form: credentials in the URL must not sign in.
			for key, values := range r.PostForm {
				if len(values) == 0 {
					continue
				}
				switch {
				case strings.EqualFold(key, "Username"):
					req.Username = values[0]
				case strings.EqualFold(key, "Pw"):
					pw := values[0]
					req.Pw = &pw
				case strings.EqualFold(key, "Password"):
					req.Password = values[0]
				}
			}
		}
	} else {
		decodeErr = readBody(r, &req)
	}
	if decodeErr != nil || strings.TrimSpace(req.Username) == "" {
		fail(w, 400, "Username and Pw are required (JSON or form body)")
		return
	}
	if req.Pw == nil {
		fail(w, 400, "Plaintext Pw is required; legacy SHA1 Password is not supported")
		return
	}
	key := clientIP(r)
	if !s.loginLimiter.Allow(key) {
		fail(w, 429, "Too many login attempts")
		return
	}
	// Do not create a CAPTCHA bypass through the compatibility endpoint.
	if s.Cfg.TurnstileEnabled() {
		fail(w, 403, "CAPTCHA is enabled; authenticate using the web interface")
		return
	}
	u, err := s.Store.UserByName(strings.TrimSpace(req.Username))
	if err != nil || !auth.Verify(*req.Pw, u.PassHash, u.Salt) {
		s.loginLimiter.Hit(key)
		fail(w, 401, "Invalid username or password; plaintext Pw is required")
		return
	}
	if !u.CanLogin() || (s.Cfg.EmailVerificationRequired() && u.EmailPending()) {
		fail(w, 403, "Account unavailable or email verification required")
		return
	}
	token := auth.NewToken()
	if err := s.Store.CreateSession(token, u.ID, time.Now().Add(sessionTTL)); err != nil {
		fail(w, 500, "Cannot create session")
		return
	}
	s.loginLimiter.Reset(key)
	_ = s.Store.TouchLastLogin(u.ID, time.Now())
	_ = s.Store.PruneSessions()
	reqURL := *r.URL
	reqURL.RawQuery = url.Values{"api_key": {token}}.Encode()
	r = r.Clone(r.Context())
	r.URL = &reqURL
	writeJSON(w, 200, map[string]any{"User": s.embyUser(u), "AccessToken": token, "ServerId": s.embyServerID(), "SessionInfo": s.embySession(r, u)})
}
