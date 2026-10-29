package controllers

import (
	"middleware-panel/models"
	"middleware-panel/services"
	"net/http"
	"strconv"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

type BPGroupMappingController struct {
	svc services.BPGroupMappingService
}

func NewBPGroupMappingController(svc services.BPGroupMappingService) *BPGroupMappingController {
	return &BPGroupMappingController{svc: svc}
}

func (c *BPGroupMappingController) ShowBPGroupMappings(ctx *gin.Context) {
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
		mappings = []models.BPGroupMapping{}
	}

	activeCount := 0
	for _, m := range mappings {
		if m.IsActive {
			activeCount++
		}
	}

	ctx.HTML(http.StatusOK, "bp_group_mapping.html", gin.H{
		"Title":       "Master Data BP Group Mapping (Maximo to SAP)",
		"Username":    username,
		"Mappings":    mappings,
		"Total":       len(mappings),
		"ActiveCount": activeCount,
		"ActiveMenu":  "bp_group_mapping",
	})
}

func (c *BPGroupMappingController) HandleSave(ctx *gin.Context) {
	session := sessions.Default(ctx)
	if session.Get("user_id") == nil {
		ctx.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Unauthorized"})
		return
	}

	var m models.BPGroupMapping
	if err := ctx.ShouldBindJSON(&m); err != nil {
		m.VendorGroup = ctx.PostForm("vendor_group")
		m.BPGrouping = ctx.PostForm("bp_grouping")
		m.Description = ctx.PostForm("description")
		m.IsActive = (ctx.PostForm("is_active") == "true" || ctx.PostForm("is_active") == "on" || ctx.PostForm("is_active") == "1")
		if idStr := ctx.PostForm("id"); idStr != "" {
			if idVal, err := strconv.Atoi(idStr); err == nil && idVal > 0 {
				m.ID = uint(idVal)
			}
		}
	}

	if m.VendorGroup == "" || m.BPGrouping == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Maximo Vendor Group dan SAP BP Grouping wajib diisi"})
		return
	}

	if err := c.svc.SaveMapping(&m); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Data BP Group Mapping berhasil disimpan",
		"data":    m,
	})
}

func (c *BPGroupMappingController) HandleToggleActive(ctx *gin.Context) {
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

	ctx.JSON(http.StatusOK, gin.H{"status": "success", "message": "Status BP Group mapping berhasil diubah"})
}

func (c *BPGroupMappingController) HandleDelete(ctx *gin.Context) {
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

	ctx.JSON(http.StatusOK, gin.H{"status": "success", "message": "BP Group mapping berhasil dihapus"})
}
