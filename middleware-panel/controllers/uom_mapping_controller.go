package controllers

import (
	"middleware-panel/models"
	"middleware-panel/services"
	"net/http"
	"strconv"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

type UOMMappingController struct {
	svc services.UOMMappingService
}

func NewUOMMappingController(svc services.UOMMappingService) *UOMMappingController {
	return &UOMMappingController{svc: svc}
}

func (c *UOMMappingController) ShowUOMMappings(ctx *gin.Context) {
	session := sessions.Default(ctx)
	if session.Get("user_id") == nil {
		ctx.Redirect(http.StatusFound, "/login")
		return
	}

	username := "Admin"
	if u := session.Get("username"); u != nil {
		username = u.(string)
	}

	mappings, err := c.svc.GetMappings()
	if err != nil {
		mappings = []models.UOMMapping{}
	}

	activeCount := 0
	for _, m := range mappings {
		if m.IsActive {
			activeCount++
		}
	}

	ctx.HTML(http.StatusOK, "uom_mapping.html", gin.H{
		"Title":       "Master Data UOM Mapping (Maximo to SAP)",
		"Username":    username,
		"Mappings":    mappings,
		"Total":       len(mappings),
		"ActiveCount": activeCount,
		"ActiveMenu":  "uom_mapping",
	})
}

func (c *UOMMappingController) HandleSave(ctx *gin.Context) {
	session := sessions.Default(ctx)
	if session.Get("user_id") == nil {
		ctx.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Unauthorized"})
		return
	}

	var m models.UOMMapping
	if err := ctx.ShouldBindJSON(&m); err != nil {
		m.MaximoUOM = ctx.PostForm("maximo_uom")
		m.SAPUOM = ctx.PostForm("sap_uom")
		m.Description = ctx.PostForm("description")
		m.IsActive = (ctx.PostForm("is_active") == "true" || ctx.PostForm("is_active") == "on" || ctx.PostForm("is_active") == "1")
		if idStr := ctx.PostForm("id"); idStr != "" {
			if idVal, err := strconv.Atoi(idStr); err == nil && idVal > 0 {
				m.ID = uint(idVal)
			}
		}
	}

	if m.MaximoUOM == "" || m.SAPUOM == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Maximo UOM dan SAP UOM wajib diisi"})
		return
	}

	if err := c.svc.SaveMapping(&m); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Data UOM Mapping berhasil disimpan",
		"data":    m,
	})
}

func (c *UOMMappingController) HandleToggleActive(ctx *gin.Context) {
	session := sessions.Default(ctx)
	if session.Get("user_id") == nil {
		ctx.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Unauthorized"})
		return
	}

	idStr := ctx.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "ID tidak valid"})
		return
	}

	if err := c.svc.ToggleActive(uint(id)); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"status": "success", "message": "Status UOM mapping berhasil diubah"})
}

func (c *UOMMappingController) HandleDelete(ctx *gin.Context) {
	session := sessions.Default(ctx)
	if session.Get("user_id") == nil {
		ctx.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Unauthorized"})
		return
	}

	idStr := ctx.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "ID tidak valid"})
		return
	}

	if err := c.svc.DeleteMapping(uint(id)); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"status": "success", "message": "UOM mapping berhasil dihapus"})
}
