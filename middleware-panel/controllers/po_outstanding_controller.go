package controllers

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// engineBaseURL = MIDDLEWARE_API_URL (default http://localhost:8085/api/v1). Header X-API-Key ditambahkan
// otomatis oleh config.InstallEngineAuth.
func engineBaseURL() string {
	u := strings.TrimRight(os.Getenv("MIDDLEWARE_API_URL"), "/")
	if u == "" {
		u = "http://localhost:8085/api/v1"
	}
	return u
}

// engineProxy meneruskan request Panel ke Engine apa adanya (method, query, body, content-type) dan
// mengembalikan respons Engine. Sync PO bisa lama, jadi timeout 10 menit.
func engineProxy(ctx *gin.Context, enginePath string) {
	username, ok := sessionUser(ctx)
	if !ok {
		ctx.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Unauthorized. Silakan login kembali."})
		return
	}
	target := engineBaseURL() + enginePath
	if q := ctx.Request.URL.RawQuery; q != "" {
		target += "?" + q
	}
	req, err := http.NewRequestWithContext(ctx.Request.Context(), ctx.Request.Method, target, ctx.Request.Body)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
		return
	}
	if ct := ctx.GetHeader("Content-Type"); ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	req.Header.Set("X-Panel-User", username)
	resp, err := (&http.Client{Timeout: 10 * time.Minute}).Do(req)
	if err != nil {
		ctx.JSON(http.StatusBadGateway, gin.H{"status": "error", "message": fmt.Sprintf("Gagal menghubungi Middleware Engine: %v", err)})
		return
	}
	defer resp.Body.Close()
	for _, h := range []string{"Content-Type", "Content-Disposition"} {
		if v := resp.Header.Get(h); v != "" {
			ctx.Header(h, v)
		}
	}
	ctx.Status(resp.StatusCode)
	_, _ = io.Copy(ctx.Writer, io.LimitReader(resp.Body, 64<<20))
}

// POOutstandingController melayani menu PO Outstanding < 2025 (PRD Fitur 3); data & proses ada di Engine.
type POOutstandingController struct{}

func NewPOOutstandingController() *POOutstandingController { return &POOutstandingController{} }

func (c *POOutstandingController) ShowPage(ctx *gin.Context) {
	username, ok := sessionUser(ctx)
	if !ok {
		ctx.Redirect(http.StatusFound, "/login")
		return
	}
	ctx.HTML(http.StatusOK, "po_outstanding.html", gin.H{
		"Title":      "PO Outstanding < 2025",
		"Username":   username,
		"ActiveMenu": "po_outstanding",
	})
}

// Proxy: /api/po-outstanding/<path> -> Engine /po-outstanding/<path> (list, template, upload, sync).
func (c *POOutstandingController) Proxy(ctx *gin.Context) {
	p := strings.Trim(ctx.Param("path"), "/")
	switch p {
	case "list":
		engineProxy(ctx, "/po-outstanding")
	case "template", "upload", "sync":
		engineProxy(ctx, "/po-outstanding/"+p)
	default:
		ctx.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "endpoint tidak dikenal"})
	}
}

// HandleTransformPreview: inline edit Tab 1 -> Engine /documents/transform-preview (regenerasi JSON Tab 2).
func HandleTransformPreview(ctx *gin.Context) {
	engineProxy(ctx, "/documents/transform-preview")
}
