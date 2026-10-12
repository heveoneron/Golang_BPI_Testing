package controllers

import (
	"middleware-panel/models"
	"middleware-panel/services"
	"net/http"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

type AppConfigController struct {
	service services.AppConfigService
}

func NewAppConfigController(s services.AppConfigService) *AppConfigController {
	return &AppConfigController{service: s}
}

// Page Config
func (c *AppConfigController) ShowConfig(ctx *gin.Context) {
	session := sessions.Default(ctx)
	if session.Get("user_id") == nil {
		ctx.Redirect(http.StatusFound, "/login")
		return
	}

	configs, _ := c.service.GetAllConfigs()

	ctx.HTML(http.StatusOK, "config.html", gin.H{
		"Configs":    configs,
		"Username":   session.Get("username"),
		"ActiveMenu": "config",
	})
}

// API Create / Update Config
func (c *AppConfigController) SaveConfig(ctx *gin.Context) {
	var req models.Config
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
		return
	}

	//Agar password tidak tertimpa kosong jika form dikosongkan
	if (req.Key == "sap_password" || req.Key == "admin_password") && req.Value == "" {
		ctx.JSON(http.StatusOK, gin.H{"status": "success", "message": "Password dipertahankan (tidak diubah)"})
		return
	}

	if err := c.service.SaveConfig(req); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"status": "success", "message": "Konfigurasi berhasil disimpan"})
}

// API Delete Config
func (c *AppConfigController) DeleteConfig(ctx *gin.Context) {
	key := ctx.Param("key")

	if key == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Key tidak valid"})
		return
	}

	if err := c.service.DeleteConfig(key); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"status": "success", "message": "Konfigurasi berhasil dihapus"})
}
