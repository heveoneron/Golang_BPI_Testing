package controllers

import (
	"middleware-panel/models"
	"middleware-panel/services"
	"net/http"
	"strconv"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

type GLMappingController struct {
	service services.GLMappingService
}

func NewGLMappingController(s services.GLMappingService) *GLMappingController {
	return &GLMappingController{service: s}
}

// Menampilkan Halaman HTML
func (c *GLMappingController) ShowPage(ctx *gin.Context) {
	mappings, _ := c.service.GetAllMappings()

	session := sessions.Default(ctx)

	username := session.Get("username")

	if username == nil {
		username = session.Get("admin_username")
	}

	if username == nil {
		username = "admin"
	}

	ctx.HTML(http.StatusOK, "gl_mapping.html", gin.H{
		"Title":      "Material GL Mapping",
		"Mappings":   mappings,
		"Username":   username,
		"ActiveMenu": "gl_mapping",
	})
}

// API: Create / Update (Save)
func (c *GLMappingController) Save(ctx *gin.Context) {
	var req models.MaterialGLMapping
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
		return
	}

	if err := c.service.SaveMapping(req); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"status": "success", "message": "Data berhasil disimpan"})
}

// API: Delete
func (c *GLMappingController) Delete(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "ID tidak valid"})
		return
	}

	if err := c.service.DeleteMapping(uint(id)); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"status": "success", "message": "Data berhasil dihapus"})
}
