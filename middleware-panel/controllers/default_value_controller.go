package controllers

import (
	"middleware-panel/models"
	"middleware-panel/repository"
	"net/http"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// DefaultValueController melayani menu Configuration > Default Value (tabel sap_default_values).
type DefaultValueController struct {
	repo repository.DefaultValueRepository
}

func NewDefaultValueController(repo repository.DefaultValueRepository) *DefaultValueController {
	return &DefaultValueController{repo: repo}
}

func sessionUser(ctx *gin.Context) (string, bool) {
	session := sessions.Default(ctx)
	if session.Get("user_id") == nil {
		return "", false
	}
	if u, ok := session.Get("username").(string); ok && u != "" {
		return u, true
	}
	return "Admin", true
}

func (c *DefaultValueController) ShowDefaultValues(ctx *gin.Context) {
	username, ok := sessionUser(ctx)
	if !ok {
		ctx.Redirect(http.StatusFound, "/login")
		return
	}
	values, err := c.repo.GetAll()
	loadErr := ""
	if err != nil {
		values = []models.DefaultValue{}
		loadErr = "Tabel sap_default_values belum tersedia. Jalankan Engine sekali agar tabel dibuat dan di-seed."
	}
	changed := 0
	categories := []string{}
	seen := map[string]bool{}
	for _, v := range values {
		if v.Value != v.DefaultValue {
			changed++
		}
		if !seen[v.Category] {
			seen[v.Category] = true
			categories = append(categories, v.Category)
		}
	}
	ctx.HTML(http.StatusOK, "default_values.html", gin.H{
		"Title":        "Default Value",
		"Username":     username,
		"Values":       values,
		"Total":        len(values),
		"ChangedCount": changed,
		"Categories":   categories,
		"LoadError":    loadErr,
		"ActiveMenu":   "default_values",
	})
}

func (c *DefaultValueController) HandleSave(ctx *gin.Context) {
	username, ok := sessionUser(ctx)
	if !ok {
		ctx.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Unauthorized"})
		return
	}
	var req struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil || req.Key == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "key wajib diisi"})
		return
	}
	if err := c.repo.UpdateValue(req.Key, req.Value, username); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"status": "success", "message": "Default value " + req.Key + " disimpan. Engine memakai nilai baru paling lambat 1 menit."})
}

func (c *DefaultValueController) HandleReset(ctx *gin.Context) {
	username, ok := sessionUser(ctx)
	if !ok {
		ctx.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Unauthorized"})
		return
	}
	key := ctx.Param("key")
	if err := c.repo.Reset(key, username); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"status": "success", "message": "Default value " + key + " dikembalikan ke nilai awal."})
}
