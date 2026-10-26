package api

import (
	"crypto/hmac"
	"fmt"
	"net/http"
	"strings"
	"time"

	"doriyamedia/internal/auth"
	"doriyamedia/internal/models"
)

// Official image URLs contain the DTO tag, but no session token. Bind the tag
// to the image identity so it grants only that image, never a library or stream.
func (s *Server) embyImageAccessTag(id int64, kind, name string) string {
	return auth.Sign(s.Cfg.Secret, fmt.Sprintf("emby-image|%d|%s|%s", id, kind, name))[:32]
}

func embyImageName(it *models.Item, kind string) string {
	switch kind {
	case "primary":
		// Episode title cards/stills are Primary images in Emby, not only Thumb.
		// Otherwise clients fall back to a season/series poster for every episode.
		if it.Type == "episode" && it.StillPath != "" {
			return it.StillPath
		}
		return it.PosterPath
	case "thumb":
		return it.StillPath
	case "backdrop":
		return it.BackdropPath
	}
	return ""
}

func (s *Server) embyRegisterImages(mux *http.ServeMux) {
	for _, pattern := range []string{
		"GET /items/{id}/images/{kind}",
		"GET /items/{id}/images/{kind}/{index}",
		"GET /items/{id}/images/{kind}/{index}/{tag}/{format}/{width}/{height}/{played}/{unplayed}",
	} {
		mux.HandleFunc(pattern, s.embyImage)
	}
}

func (s *Server) embyImage(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if kind != "primary" && kind != "thumb" && kind != "backdrop" {
		fail(w, 404, "Image type not available")
		return
	}
	if index := r.PathValue("index"); index != "" && index != "0" {
		fail(w, 404, "Image index not available")
		return
	}
	tag := embyQuery(r, "Tag")
	if pathTag := r.PathValue("tag"); pathTag != "" {
		tag = pathTag
	}
	// Validate a session first. Old cached tags continue to work with a token.
	authenticated := false
	if token := embyToken(r); token != "" {
		if sess, err := s.Store.SessionByToken(token); err == nil && time.Now().Before(sess.ExpiresAt) {
			if u, err := s.Store.UserByID(sess.UserID); err == nil && u.CanLogin() && !(s.Cfg.EmailVerificationRequired() && u.EmailPending()) {
				authenticated = true
			}
		}
	}
	if !authenticated && tag == "" {
		fail(w, 401, "Access token or valid image tag required")
		return
	}
	it, err := s.Store.ItemByID(embyNumber(r.PathValue("id")))
	if err != nil {
		if authenticated {
			fail(w, 404, "Item not found")
		} else {
			fail(w, 401, "Invalid image tag")
		}
		return
	}
	name := embyImageName(it, kind)
	if !authenticated && (name == "" || !hmac.Equal([]byte(tag), []byte(s.embyImageAccessTag(it.ID, kind, name)))) {
		fail(w, 401, "Invalid image tag")
		return
	}
	if name == "" {
		fail(w, 404, "Image not available")
		return
	}
	switch strings.ToLower(kind) {
	case "primary":
		if it.Type == "episode" && it.StillPath != "" {
			r.SetPathValue("kind", "still")
		} else {
			r.SetPathValue("kind", "poster")
		}
	case "thumb":
		r.SetPathValue("kind", "still")
	}
	s.handleImage(w, r)
}
