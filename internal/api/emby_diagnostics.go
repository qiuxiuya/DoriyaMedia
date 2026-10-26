package api

import (
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

type embyStatusWriter struct {
	http.ResponseWriter
	status int
}

func (w *embyStatusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *embyStatusWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *embyStatusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

// Unknown endpoints must remain diagnosable: a whitelist of known endpoints
// hides precisely the routes we need to discover. Log short ASCII route words,
// not IDs, encoded segments or credential-bearing tails. Never pass RawQuery.
func embyDiagnosticPath(path string) string {
	parts := strings.Split(strings.Trim(strings.ToLower(path), "/"), "/")
	out := make([]string, len(parts))
	redactFrom := len(parts)
	for i, part := range parts {
		dynamic := false
		if i > 0 {
			switch out[i-1] {
			case "items", "users", "videos", "shows", "persons", "displaypreferences":
				dynamic = true
			}
		}
		// These are documented static resources, not record identifiers.
		if dynamic {
			switch part {
			case "root", "me", "public", "authenticatebyname", "counts", "latest", "resume", "nextup", "activeencodings":
				dynamic = false
			}
		}
		if part == "images" && i+2 < redactFrom {
			redactFrom = i + 2 // Keep image kind; hide index/tag/format path tail.
		}
		switch part {
		case "token", "tokens", "api_key", "apikey", "signature", "sig", "password", "pw", "access_token", "accesstoken", "auth_tokens":
			if i+1 < redactFrom {
				redactFrom = i + 1
			}
		}
		word := len(part) > 0 && len(part) <= 40
		for _, c := range part {
			if c < 'a' || c > 'z' {
				word = false
				break
			}
		}
		if dynamic || i >= redactFrom || !word {
			out[i] = "{redacted}"
		} else {
			out[i] = part
		}
	}
	return "/" + strings.Join(out, "/")
}

var embyRequestSequence atomic.Uint64

// Only protocol options are logged. Unknown keys, credentials, search text,
// nested URLs and signatures are omitted, rather than trusting their names.
func embyDiagnosticQuery(r *http.Request) string {
	allowed := "|userid|itemid|parentid|mediasourceid|isplayback|startindex|limit|recursive|includeitemtypes|fields|sortby|sortorder|seasonid|season|seriesid|isplayed|enableimages|enableuserdata|imageopacity|maxwidth|maxheight|width|height|quality|format|reqformat|"
	clean := url.Values{}
	for key, values := range r.URL.Query() {
		key = strings.ToLower(key)
		if !strings.Contains(allowed, "|"+key+"|") {
			continue
		}
		for i, value := range values {
			if i >= 8 {
				break
			}
			if len(value) > 256 {
				value = "{redacted}"
			}
			clean.Add(key, value)
		}
	}
	return clean.Encode()
}

func embyServeLogged(h http.Handler, w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	id := embyRequestSequence.Add(1)
	path, query := embyDiagnosticPath(r.URL.Path), embyDiagnosticQuery(r)
	// Emit before dispatch as well: a request still blocked in a driver must
	// be visible, not only requests whose handlers have already completed.
	log.Printf("Emby %s request=%d phase=start path=%q query=%q", r.Method, id, path, query)
	out := &embyStatusWriter{ResponseWriter: w}
	h.ServeHTTP(out, r)
	status := out.status
	if status == 0 {
		status = http.StatusOK
	}
	pattern := r.Pattern
	if pattern == "" {
		pattern = "unmatched " + path
	}
	log.Printf("Emby %s request=%d phase=end path=%q query=%q route=%q status=%d duration=%s", r.Method, id, path, query, pattern, status, time.Since(start).Round(time.Millisecond))
}
