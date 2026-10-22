package controllers

import (
	"middleware-panel/models"
	"middleware-panel/services"
	"net/http"
	"strconv"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

type TableAPIMappingController struct {
	svc services.TableAPIMappingService
}

func NewTableAPIMappingController(svc services.TableAPIMappingService) *TableAPIMappingController {
	return &TableAPIMappingController{svc: svc}
}

func (c *TableAPIMappingController) ShowMappings(ctx *gin.Context) {
	session := sessions.Default(ctx)
	if session.Get("user_id") == nil {
		ctx.Redirect(http.StatusFound, "/login")
		return
	}

	mappings, err := c.svc.GetMappings()
	if err != nil {
		mappings = []models.TableAPIMapping{}
	}

	endpoints, err := c.svc.GetEndpoints()
	if err != nil {
		endpoints = []models.SAPEndpoint{}
	}

	username := "Admin"
	if u := session.Get("username"); u != nil {
		username = u.(string)
	}

	// Hitung statistik
	activeCount := 0
	outboundCount := 0
	for _, m := range mappings {
		if m.IsActive {
			activeCount++
		}
		if m.Direction == "OUTBOUND" {
			outboundCount++
		}
	}

	ctx.HTML(http.StatusOK, "table_api_mapping.html", gin.H{
		"Title":         "Table to SAP API Mapping Matrix",
		"Username":      username,
		"Mappings":      mappings,
		"Endpoints":     endpoints,
		"TotalMappings": len(mappings),
		"ActiveCount":   activeCount,
		"OutboundCount": outboundCount,
		"ActiveMenu":    "table_mapping",
	})
}

func (c *TableAPIMappingController) HandleSaveMapping(ctx *gin.Context) {
	session := sessions.Default(ctx)
	if session.Get("user_id") == nil {
		ctx.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Unauthorized"})
		return
	}

	var m models.TableAPIMapping
	if err := ctx.ShouldBindJSON(&m); err != nil {
		// Fallback parse form jika bukan JSON
		m.SourceTable = ctx.PostForm("source_table")
		m.SourceDesc = ctx.PostForm("source_desc")
		m.TargetServiceCode = ctx.PostForm("target_service_code")
		m.Direction = ctx.PostForm("direction")
		m.SyncMode = ctx.PostForm("sync_mode")
		m.Notes = ctx.PostForm("notes")
		m.IsActive = (ctx.PostForm("is_active") == "true" || ctx.PostForm("is_active") == "on" || ctx.PostForm("is_active") == "1")
		if idStr := ctx.PostForm("id"); idStr != "" {
			if idVal, err := strconv.Atoi(idStr); err == nil && idVal > 0 {
				m.ID = uint(idVal)
			}
		}
	}

	if m.SourceTable == "" || m.TargetServiceCode == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": "Nama tabel sumber (Maximo) dan kode service SAP wajib diisi.",
		})
		return
	}

	if m.Direction == "" {
		m.Direction = "OUTBOUND"
	}
	if m.SyncMode == "" {
		m.SyncMode = "BATCH_SCHEDULE"
	}

	if err := c.svc.SaveMapping(&m); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": "Gagal menyimpan mapping: " + err.Error(),
		})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Mapping tabel " + m.SourceTable + " ke API SAP [" + m.TargetServiceCode + "] berhasil disimpan!",
		"data":    m,
	})
}

func (c *TableAPIMappingController) HandleToggleActive(ctx *gin.Context) {
	session := sessions.Default(ctx)
	if session.Get("user_id") == nil {
		ctx.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Unauthorized"})
		return
	}

	idStr := ctx.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		ctx.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "ID tidak valid"})
		return
	}

	if err := c.svc.ToggleActive(uint(id)); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Status mapping berhasil diubah",
	})
}

func (c *TableAPIMappingController) HandleDeleteMapping(ctx *gin.Context) {
	session := sessions.Default(ctx)
	if session.Get("user_id") == nil {
		ctx.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Unauthorized"})
		return
	}

	idStr := ctx.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		ctx.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "ID tidak valid"})
		return
	}

	if err := c.svc.DeleteMapping(uint(id)); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Mapping berhasil dihapus",
	})
}
