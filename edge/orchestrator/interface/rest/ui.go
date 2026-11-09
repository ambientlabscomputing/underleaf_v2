package rest

import (
	"net/http"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
)

// One origin for the UI and the API (RFD 5, step 3).
//
// The Orchestrator serves the built UI from the same listener as the API, so
// the browser only ever talks to one origin: no CORS, a single port to
// publish or tunnel, and the Host/Origin/Content-Type guards apply uniformly
// to assets, API calls and WebSocket upgrades.

// securityHeaders adds response headers that keep the UI from being framed by
// another site (clickjacking a no-login UI that can deploy workloads) or
// content-sniffed.
func securityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", "frame-ancestors 'none'")
		c.Next()
	}
}

// registerUI serves the SPA in uiDir for any GET/HEAD that is not an API
// route: an existing file is served as is, anything else gets index.html so
// client-side routes survive a reload. Unknown API paths get a JSON 404, not
// the SPA. With an empty uiDir the router has no UI and unknown paths 404.
func registerUI(r *gin.Engine, basePath, uiDir string) {
	apiPrefix := path.Clean("/" + basePath)
	fsys := http.Dir(uiDir)

	r.NoRoute(func(c *gin.Context) {
		p := c.Request.URL.Path
		if p == apiPrefix || strings.HasPrefix(p, apiPrefix+"/") || uiDir == "" {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			c.JSON(http.StatusMethodNotAllowed, gin.H{"error": "method not allowed"})
			return
		}

		name := path.Clean("/" + p)
		// Never serve dotfiles (e.g. a stray .env in the build directory).
		for _, seg := range strings.Split(name, "/") {
			if strings.HasPrefix(seg, ".") && seg != "" {
				c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
				return
			}
		}
		if serveUIFile(c, fsys, name) {
			return
		}
		// SPA fallback.
		if !serveUIFile(c, fsys, "/index.html") {
			c.JSON(http.StatusNotFound, gin.H{"error": "ui not found"})
		}
	})
}

// serveUIFile serves name if it is a regular file. http.Dir confines lookups
// to its root, whatever the request path says.
func serveUIFile(c *gin.Context, fsys http.FileSystem, name string) bool {
	f, err := fsys.Open(name)
	if err != nil {
		return false
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || fi.IsDir() {
		return false
	}
	switch {
	case name == "/index.html":
		c.Header("Cache-Control", "no-cache")
	case strings.HasPrefix(name, "/assets/"):
		// Vite fingerprints everything under assets/.
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
	}
	http.ServeContent(c.Writer, c.Request, fi.Name(), fi.ModTime(), f)
	return true
}
