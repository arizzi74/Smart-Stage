package web

import (
	"embed"
	"net/http"
)

//go:embed assets/*
var assets embed.FS

func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "Method not allowed", 405)
			return
		}
		name := ""
		contentType := ""
		switch r.URL.Path {
		case "/":
			http.Redirect(w, r, "/admin", http.StatusSeeOther)
			return
		case "/admin", "/command":
			name = "index.html"
			contentType = "text/html; charset=utf-8"
		case "/assets/app.js":
			name = "app.js"
			contentType = "text/javascript; charset=utf-8"
		case "/assets/wake-lock.js":
			name = "wake-lock.js"
			contentType = "text/javascript; charset=utf-8"
		case "/assets/style.css":
			name = "style.css"
			contentType = "text/css; charset=utf-8"
		default:
			http.NotFound(w, r)
			return
		}
		data, err := assets.ReadFile("assets/" + name)
		if err != nil {
			http.Error(w, "Embedded asset unavailable", 500)
			return
		}
		w.Header().Set("Content-Type", contentType)
		if r.Method != http.MethodHead {
			_, _ = w.Write(data)
		}
	})
}
