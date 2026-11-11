package routes

import (
	"crypto/subtle"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

// APIKeyHeader adalah header yang wajib dikirim pemanggil Engine (Panel, Maximo, sistem kurs) bila
// ENGINE_API_KEY diisi. "Authorization: Bearer <key>" juga diterima.
const APIKeyHeader = "X-API-Key"

// apiKeyAuth melindungi /api/v1 dan /admin. Bila ENGINE_API_KEY kosong, autentikasi nonaktif
// (perilaku lama, untuk lokal) dan peringatan dicetak saat start. /api/v1/health selalu terbuka.
func apiKeyAuth() gin.HandlerFunc {
	key := strings.TrimSpace(os.Getenv("ENGINE_API_KEY"))
	if key == "" {
		log.Println("[WARN] ENGINE_API_KEY kosong: API Engine TANPA autentikasi. Isi ENGINE_API_KEY di luar lingkungan lokal.")
		return func(c *gin.Context) { c.Next() }
	}
	want := []byte(key)
	return func(c *gin.Context) {
		p := c.Request.URL.Path
		protected := strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/admin")
		if !protected || c.Request.Method == http.MethodOptions || p == "/api/v1/health" {
			c.Next()
			return
		}
		got := strings.TrimSpace(c.GetHeader(APIKeyHeader))
		if got == "" {
			if auth := c.GetHeader("Authorization"); len(auth) > 7 && strings.EqualFold(auth[:7], "Bearer ") {
				got = strings.TrimSpace(auth[7:])
			}
		}
		if got == "" || subtle.ConstantTimeCompare([]byte(got), want) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "API key tidak valid atau tidak dikirim (header " + APIKeyHeader + ")"})
			return
		}
		c.Next()
	}
}

// corsOrigin membaca ENGINE_CORS_ORIGINS (dipisah koma). Kosong = "*" (perilaku lama).
func corsOrigin(reqOrigin string) string {
	raw := strings.TrimSpace(os.Getenv("ENGINE_CORS_ORIGINS"))
	if raw == "" || raw == "*" {
		return "*"
	}
	for _, o := range strings.Split(raw, ",") {
		if strings.EqualFold(strings.TrimSpace(o), reqOrigin) {
			return reqOrigin
		}
	}
	return ""
}
