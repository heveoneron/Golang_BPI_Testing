package controllers

import (
	"middleware-panel/models"
	"middleware-panel/services"
	"net/http"
	"strconv"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

type ReconAccountMappingController struct {
	svc services.ReconAccountMappingService
}

func NewReconAccountMappingController(svc services.ReconAccountMappingService) *ReconAccountMappingController {
	return &ReconAccountMappingController{svc: svc}
}

func (c *ReconAccountMappingController) ShowReconAccountMappings(ctx *gin.Context) {
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
		mappings = []models.ReconAccountMapping{}
	}

	activeCount := 0
	for _, m := range mappings {
		if m.IsActive {
			activeCount++
		}
	}

	ctx.HTML(http.StatusOK, "recon_account_mapping.html", gin.H{
		"Title":       "Master Data Reconciliation Account Mapping",
		"Username":    username,
		"Mappings":    mappings,
		"Total":       len(mappings),
		"ActiveCount": activeCount,
		"ActiveMenu":  "recon_account_mapping",
	})
}

func (c *ReconAccountMappingController) HandleSave(ctx *gin.Context) {
	session := sessions.Default(ctx)
	if session.Get("user_id") == nil {
		ctx.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Unauthorized"})
		return
	}

	var m models.ReconAccountMapping
	if err := ctx.ShouldBindJSON(&m); err != nil {
		m.VendorGroup = ctx.PostForm("vendor_group")
		m.BPGrouping = ctx.PostForm("bp_grouping")
		m.CompanyCode = ctx.PostForm("company_code")
		m.ReconAccount = ctx.PostForm("recon_account")
		m.Description = ctx.PostForm("description")
		m.IsActive = (ctx.PostForm("is_active") == "true" || ctx.PostForm("is_active") == "on" || ctx.PostForm("is_active") == "1")
		if idStr := ctx.PostForm("id"); idStr != "" {
			if idVal, err := strconv.Atoi(idStr); err == nil && idVal > 0 {
				m.ID = uint(idVal)
			}
		}
	}

	if m.CompanyCode == "" {
		m.CompanyCode = "5110"
	}
	if m.BPGrouping == "" {
		m.BPGrouping = "Z001"
	}

	if m.VendorGroup == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Maximo Vendor Type wajib diisi"})
		return
	}

	if err := c.svc.SaveMapping(&m); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Data Reconciliation Account Mapping berhasil disimpan",
		"data":    m,
	})
}

func (c *ReconAccountMappingController) HandleToggleActive(ctx *gin.Context) {
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

	ctx.JSON(http.StatusOK, gin.H{"status": "success", "message": "Status Recon Account mapping berhasil diubah"})
}

func (c *ReconAccountMappingController) HandleDelete(ctx *gin.Context) {
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

	ctx.JSON(http.StatusOK, gin.H{"status": "success", "message": "Recon Account mapping berhasil dihapus"})
}
