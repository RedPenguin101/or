package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
)

// api.go is the route index. Handlers live in the *_api.go file beside the
// domain code they serve, and the dependency runs one way: *_api.go -> domain.
func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()
	a.registerProjectRoutes(mux)
	a.registerSchemaRoutes(mux)
	a.registerDatasetRoutes(mux)
	a.registerOutputRoutes(mux)
	a.registerRunRoutes(mux)

	// The built UI, when there is one. In development the Vite server proxies
	// /api to this process instead, so this path is not used.
	dist := "web/dist"
	if _, err := os.Stat(dist); err == nil {
		fs := http.FileServer(http.Dir(dist))
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				http.NotFound(w, r)
				return
			}
			if _, err := os.Stat(dist + r.URL.Path); err != nil && r.URL.Path != "/" {
				http.ServeFile(w, r, dist+"/index.html") // client-side routing
				return
			}
			fs.ServeHTTP(w, r)
		})
	}
	return logging(mux)
}

func logging(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL.Path)
		h.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		log.Printf("encoding response: %v", err)
	}
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func readJSON(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}
