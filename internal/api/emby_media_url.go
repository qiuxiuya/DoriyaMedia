package api

import (
	"net/http"
	"net/url"
	"strings"
)

// MediaSource.Path with Protocol=Http is a playable HTTP resource, not the
// server-relative API route used by DirectStreamUrl. Do not expose local disk
// paths or upstream cookies; retain the existing authenticated stream endpoint.
func (s *Server) embyAbsoluteMediaURL(r *http.Request, relative string) string {
	ref, err := url.Parse(relative)
	if err != nil {
		return relative
	}
	if ref.IsAbs() {
		return relative
	}
	base, err := url.Parse(strings.TrimSpace(s.Cfg.Users.BaseURL))
	if err != nil || base.Host == "" || (base.Scheme != "http" && base.Scheme != "https") || base.User != nil {
		scheme := "http"
		if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			scheme = "https"
		}
		base = &url.URL{Scheme: scheme, Host: r.Host}
	}
	return base.ResolveReference(ref).String()
}
