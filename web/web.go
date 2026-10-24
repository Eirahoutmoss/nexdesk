// Package web serves the NexDesk browser technician app (a PWA that runs on
// iPhone, Android and desktop browsers) embedded in the signaling binary.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed static
var files embed.FS

// Handler serves the app with strict security headers. wsOrigin is the
// wss:// origin the page may connect to (its own host).
func Handler() http.Handler {
	sub, _ := fs.Sub(files, "static")
	fsrv := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; connect-src 'self' wss: ws:; img-src 'self' blob: data:; style-src 'self'; script-src 'self'; worker-src 'self'; manifest-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if r.TLS != nil {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		p := path.Clean("/" + r.URL.Path)
		switch {
		case p == "/" || p == "/index.html" || p == "/sw.js" || strings.HasSuffix(p, ".webmanifest"):
			h.Set("Cache-Control", "no-cache") // always revalidate: updates arrive at once
		default:
			h.Set("Cache-Control", "public, max-age=300")
		}
		if strings.HasSuffix(p, ".webmanifest") {
			h.Set("Content-Type", "application/manifest+json")
		}
		fsrv.ServeHTTP(w, r)
	})
}
