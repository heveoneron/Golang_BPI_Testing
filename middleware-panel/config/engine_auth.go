package config

import (
	"net/http"
	"net/url"
	"os"
	"strings"
)

// engineAuthTransport menambahkan header X-API-Key ke setiap request menuju Engine (host
// MIDDLEWARE_API_URL / API_URL / BACKUP_API_URL) bila ENGINE_API_KEY diisi. Semua pemanggilan Engine
// di Panel memakai http.DefaultTransport, sehingga cukup dipasang sekali saat start.
type engineAuthTransport struct {
	base  http.RoundTripper
	key   string
	hosts map[string]bool
}

func (t *engineAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.hosts[strings.ToLower(req.URL.Host)] && req.Header.Get("X-API-Key") == "" {
		req = req.Clone(req.Context())
		req.Header.Set("X-API-Key", t.key)
	}
	return t.base.RoundTrip(req)
}

// InstallEngineAuth memasang transport di atas http.DefaultTransport. Tanpa ENGINE_API_KEY tidak ada perubahan.
func InstallEngineAuth() {
	key := strings.TrimSpace(os.Getenv("ENGINE_API_KEY"))
	if key == "" {
		return
	}
	hosts := map[string]bool{"localhost:8085": true}
	for _, env := range []string{"MIDDLEWARE_API_URL", "API_URL", "BACKUP_API_URL"} {
		if u, err := url.Parse(strings.TrimSpace(os.Getenv(env))); err == nil && u.Host != "" {
			hosts[strings.ToLower(u.Host)] = true
		}
	}
	http.DefaultTransport = &engineAuthTransport{base: http.DefaultTransport, key: key, hosts: hosts}
}
