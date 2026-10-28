package api

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"strconv"

	"doriyamedia/internal/config"
)

// A small built-in play mark keeps branding available without remote fetching.
var embyDefaultIcon = func() []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			c := color.NRGBA{R: 29, G: 35, B: 48, A: 255}
			if x >= 23 && x < 46 && y >= 17+(x-23)*15/23 && y <= 47-(x-23)*15/23 {
				c = color.NRGBA{R: 240, G: 169, B: 46, A: 255}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	var out bytes.Buffer
	_ = png.Encode(&out, img)
	return out.Bytes()
}()

func (s *Server) embyBranding(w http.ResponseWriter, r *http.Request) {
	logo := s.Cfg.Emby.Icon
	if logo == "" || config.ValidateEmbyIcon(logo) != nil {
		logo = "/emby/Branding/Logo"
	}
	writeJSON(w, http.StatusOK, map[string]any{"CustomLogoUrl": logo, "CustomIconUrl": logo})
}

func (s *Server) embyBrandingImage(w http.ResponseWriter, r *http.Request) {
	if icon := s.Cfg.Emby.Icon; icon != "" && config.ValidateEmbyIcon(icon) == nil {
		// Only the client follows the URL: never fetch administrator-provided
		// addresses on the server, including private network addresses.
		http.Redirect(w, r, icon, http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.Itoa(len(embyDefaultIcon)))
	if r.Method != http.MethodHead {
		_, _ = w.Write(embyDefaultIcon)
	}
}
