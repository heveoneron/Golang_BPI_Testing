package controllers

import (
	"middleware-panel/models"
	"middleware-panel/services"
	"net/http"
	"strconv"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

type MaterialConfigMappingController struct {
	svc services.MaterialConfigMappingService
}

func NewMaterialConfigMappingController(svc services.MaterialConfigMappingService) *MaterialConfigMappingController {
	return &MaterialConfigMappingController{svc: svc}
}

func (c *MaterialConfigMappingController) ShowMaterialMappings(ctx *gin.Context) {
	session := sessions.Default(ctx)
	if session.Get("user_id") == nil {
		ctx.Redirect(http.StatusFound, "/login")
		return
	}

	username := "Admin"
	if u := session.Get("username"); u != nil {
		username = u.(string)
	}

	allMappings, err := c.svc.GetMappings("")
	if err != nil {
		allMappings = []models.MaterialConfigMapping{}
	}

	var itemGroupList []models.MaterialConfigMapping
	var itemTypeList []models.MaterialConfigMapping
	var stockTypeList []models.MaterialConfigMapping

	activeCount := 0
	for _, m := range allMappings {
		if m.IsActive {
			activeCount++
		}
		switch m.ConfigType {
		case "ITEM_GROUP":
			itemGroupList = append(itemGroupList, m)
		case "ITEM_TYPE":
			itemTypeList = append(itemTypeList, m)
		case "STOCK_TYPE":
			stockTypeList = append(stockTypeList, m)
		}
	}

	ctx.HTML(http.StatusOK, "material_mapping.html", gin.H{
		"Title":          "Master Data Material & Item Config (Maximo to SAP)",
		"Username":       username,
		"Total":          len(allMappings),
		"ActiveCount":    activeCount,
		"ItemGroupList":  itemGroupList,
		"ItemTypeList":   itemTypeList,
		"StockTypeList":  stockTypeList,
		"ItemGroupCount": len(itemGroupList),
		"ItemTypeCount":  len(itemTypeList),
		"StockTypeCount": len(stockTypeList),
		"ActiveMenu":     "material_mapping",
	})
}

func (c *MaterialConfigMappingController) HandleSave(ctx *gin.Context) {
	session := sessions.Default(ctx)
	if session.Get("user_id") == nil {
		ctx.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Unauthorized"})
		return
	}

	var req struct {
		ID                 uint   `json:"id"`
		ConfigType         string `json:"config_type" binding:"required"`
		MaximoValue        string `json:"maximo_value" binding:"required"`
		SAPValue           string `json:"sap_value" binding:"required"`
		InventoryPostingGL string `json:"inventory_posting_gl"`
		GoodsIssueGL       string `json:"goods_issue_gl"`
		StockAdjGL         string `json:"stock_adj_gl"`
		CostCenter         string `json:"cost_center"`
		Description        string `json:"description"`
		IsActive           bool   `json:"is_active"`
	}

	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Input tidak valid: " + err.Error()})
		return
	}

	mapping := models.MaterialConfigMapping{
		ID:                 req.ID,
		ConfigType:         req.ConfigType,
		MaximoValue:        req.MaximoValue,
		SAPValue:           req.SAPValue,
		InventoryPostingGL: req.InventoryPostingGL,
		GoodsIssueGL:       req.GoodsIssueGL,
		StockAdjGL:         req.StockAdjGL,
		CostCenter:         req.CostCenter,
		Description:        req.Description,
		IsActive:           req.IsActive,
	}

	if err := c.svc.SaveMapping(&mapping); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "Gagal menyimpan: " + err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"status": "success", "message": "Pemetaan berhasil disimpan", "data": mapping})
}

func (c *MaterialConfigMappingController) HandleToggleActive(ctx *gin.Context) {
	session := sessions.Default(ctx)
	if session.Get("user_id") == nil {
		ctx.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Unauthorized"})
		return
	}

	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "ID tidak valid"})
		return
	}

	if err := c.svc.ToggleActive(uint(id)); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "Gagal mengubah status: " + err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"status": "success", "message": "Status pemetaan berhasil diubah"})
}

func (c *MaterialConfigMappingController) HandleDelete(ctx *gin.Context) {
	session := sessions.Default(ctx)
	if session.Get("user_id") == nil {
		ctx.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Unauthorized"})
		return
	}

	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "ID tidak valid"})
		return
	}

	if err := c.svc.DeleteMapping(uint(id)); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "Gagal menghapus: " + err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"status": "success", "message": "Pemetaan berhasil dihapus"})
}

