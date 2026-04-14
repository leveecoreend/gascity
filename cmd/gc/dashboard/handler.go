package dashboard

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed static
var staticFiles embed.FS

// NewDashboardMux creates an HTTP handler that serves the static dashboard.
// All API operations go from the browser directly to the supervisor via WebSocket.
func NewDashboardMux(apiURL, initialCityScope string) (http.Handler, error) {

	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return nil, err
	}
	staticHandler := http.FileServer(http.FS(staticFS))

	mux := http.NewServeMux()
	mux.Handle("/static/", http.StripPrefix("/static/", staticHandler))
	// Serve index.html for all non-static paths.
	dashAPIURL := apiURL // capture for closure
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		indexData, err := fs.ReadFile(staticFS, "index.html")
		if err != nil {
			http.Error(w, "dashboard not found", http.StatusInternalServerError)
			return
		}
		// Inject API URL and selected city into the static HTML.
		html := string(indexData)
		city := r.URL.Query().Get("city")
		if city == "" {
			city = initialCityScope
		}
		inject := ""
		if dashAPIURL != "" {
			inject += `<meta name="api-url" content="` + dashAPIURL + `">`
		}
		if city != "" {
			inject += `<meta name="selected-city" content="` + city + `">`
		}
		if inject != "" {
			html = strings.Replace(html, "</head>", inject+"\n</head>", 1)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(html)) //nolint:errcheck
	})

	return mux, nil
}
