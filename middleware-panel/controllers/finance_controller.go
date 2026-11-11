package controllers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// FinanceController menampilkan halaman Finance Integration (RICEF 1-9) dan meneruskan
// panggilan API ke Middleware Engine (/api/v1/finance) sehingga browser tidak perlu
// mengakses port Engine secara langsung.
type FinanceController struct {
	client *http.Client
}

func NewFinanceController() *FinanceController {
	return &FinanceController{client: &http.Client{Timeout: 120 * time.Second}}
}

func middlewareBaseURL() string {
	u := os.Getenv("MIDDLEWARE_API_URL")
	if u == "" {
		u = "http://localhost:8085/api/v1"
	}
	return strings.TrimRight(u, "/")
}

func sessionUsername(ctx *gin.Context) string {
	if u, ok := sessions.Default(ctx).Get("username").(string); ok && u != "" {
		return u
	}
	return "unknown"
}

func (c *FinanceController) ShowPage(ctx *gin.Context) {
	ctx.HTML(http.StatusOK, "finance.html", gin.H{
		"Title":      "Finance Integration (RICEF 1-9)",
		"Username":   sessionUsername(ctx),
		"ActiveMenu": "finance",
	})
}

// identityFields mengembalikan field identitas yang wajib diisi dari sesi login untuk path tertentu.
func identityFields(path string) []string {
	switch {
	case strings.HasSuffix(path, "/approve") || strings.HasSuffix(path, "/reject") || strings.HasSuffix(path, "/print"):
		return []string{"by"}
	case path == "/assets/acquisitions":
		return []string{"parked_by"}
	case strings.HasPrefix(path, "/assets/acquisitions/"):
		return []string{"by"}
	case path == "/budget/supplement" || path == "/budget/transfer":
		return []string{"requested_by"}
	}
	return nil
}

// Proxy meneruskan request ke Engine. Identitas pengguna (parked_by, by, requested_by,
// uploaded_by) selalu ditimpa dengan username sesi agar tidak bisa dipalsukan dari browser.
func (c *FinanceController) Proxy(ctx *gin.Context) {
	path := "/" + strings.TrimLeft(ctx.Param("path"), "/")
	if strings.Contains(path, "..") {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "path tidak valid"})
		return
	}
	user := sessionUsername(ctx)

	query := ctx.Request.URL.Query()
	if path == "/supplier-invoices/upload" {
		query.Set("uploaded_by", user)
	}
	target := fmt.Sprintf("%s/finance%s", middlewareBaseURL(), path)
	if enc := query.Encode(); enc != "" {
		target += "?" + enc
	}

	var body io.Reader
	contentType := ctx.GetHeader("Content-Type")
	if ctx.Request.Method != http.MethodGet && ctx.Request.Body != nil {
		raw, err := io.ReadAll(io.LimitReader(ctx.Request.Body, 12<<20))
		if err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "gagal membaca request"})
			return
		}
		// Selalu ditimpa apa pun Content-Type-nya: Engine membaca body sebagai JSON tanpa melihat header.
		if fields := identityFields(path); len(fields) > 0 {
			var obj map[string]interface{}
			if len(bytes.TrimSpace(raw)) == 0 {
				obj = map[string]interface{}{}
			} else if err := json.Unmarshal(raw, &obj); err != nil {
				ctx.JSON(http.StatusBadRequest, gin.H{"error": "JSON tidak valid"})
				return
			}
			for _, f := range fields {
				obj[f] = user
			}
			raw, _ = json.Marshal(obj)
			contentType = "application/json"
		}
		body = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx.Request.Context(), ctx.Request.Method, target, body)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		ctx.JSON(http.StatusBadGateway, gin.H{"error": "Middleware Engine tidak dapat dihubungi: " + err.Error()})
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/json"
	}
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		ctx.Header("Content-Disposition", cd)
	}
	ctx.Data(resp.StatusCode, ct, respBody)
}
