package controllers

import (
	"io"
	"middleware-panel/models"
	"middleware-panel/services"
	"net/http"
	"strings"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

type VendorMasterController struct {
	svc services.VendorMasterService
}

func NewVendorMasterController(svc services.VendorMasterService) *VendorMasterController {
	return &VendorMasterController{svc: svc}
}

func (c *VendorMasterController) ShowVendorMaster(ctx *gin.Context) {
	session := sessions.Default(ctx)
	if session.Get("user_id") == nil {
		ctx.Redirect(http.StatusFound, "/login")
		return
	}

	username := "Admin"
	if u := session.Get("username"); u != nil {
		username = u.(string)
	}

	vendors, err := c.svc.GetVendors()
	if err != nil {
		vendors = []models.VendorMaster{}
	}

	cipCount := 0
	nonCipCount := 0
	bankCount := 0
	activeCount := 0

	for _, v := range vendors {
		if v.IsActive {
			activeCount++
		}
		if strings.EqualFold(v.CIPFlag, "CIP") {
			cipCount++
		} else {
			nonCipCount++
		}
		if strings.TrimSpace(v.BankAccount) != "" {
			bankCount++
		}
	}

	ctx.HTML(http.StatusOK, "vendor_master.html", gin.H{
		"Title":       "Vendor Master Data (Maximo & SAP S/4HANA)",
		"Username":    username,
		"Vendors":     vendors,
		"Total":       len(vendors),
		"ActiveCount": activeCount,
		"CIPCount":    cipCount,
		"NonCIPCount": nonCipCount,
		"BankCount":   bankCount,
		"ActiveMenu":  "vendor_master",
	})
}

func (c *VendorMasterController) HandleSave(ctx *gin.Context) {
	session := sessions.Default(ctx)
	if session.Get("user_id") == nil {
		ctx.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Unauthorized"})
		return
	}

	var req struct {
		VendorCode    string `json:"vendor_code" binding:"required"`
		CIPFlag       string `json:"cip_flag" binding:"required"`
		Email         string `json:"email"`
		BankKey       string `json:"bank_key"`
		BankAccount   string `json:"bank_account"`
		AccountHolder string `json:"account_holder"`
		Description   string `json:"description"`
		IsActive      bool   `json:"is_active"`
	}

	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Input tidak valid: " + err.Error()})
		return
	}

	cip := strings.ToUpper(strings.TrimSpace(req.CIPFlag))
	if cip != "CIP" && cip != "NON_CIP" {
		cip = "NON_CIP"
	}

	vm := models.VendorMaster{
		VendorCode:    strings.TrimSpace(req.VendorCode),
		CIPFlag:       cip,
		Email:         strings.TrimSpace(req.Email),
		BankKey:       strings.TrimSpace(req.BankKey),
		BankAccount:   strings.TrimSpace(req.BankAccount),
		AccountHolder: strings.TrimSpace(req.AccountHolder),
		Description:   strings.TrimSpace(req.Description),
		IsActive:      req.IsActive,
	}

	if err := c.svc.SaveVendor(&vm); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "Gagal menyimpan vendor: " + err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"status": "success", "message": "Vendor berhasil disimpan", "data": vm})
}

func (c *VendorMasterController) HandleDelete(ctx *gin.Context) {
	session := sessions.Default(ctx)
	if session.Get("user_id") == nil {
		ctx.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Unauthorized"})
		return
	}

	code := ctx.Param("code")
	if strings.TrimSpace(code) == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Vendor code wajib diisi"})
		return
	}

	if err := c.svc.DeleteVendor(code); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "Gagal menghapus: " + err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"status": "success", "message": "Vendor " + code + " berhasil dihapus"})
}

func (c *VendorMasterController) HandleToggleActive(ctx *gin.Context) {
	session := sessions.Default(ctx)
	if session.Get("user_id") == nil {
		ctx.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Unauthorized"})
		return
	}

	code := ctx.Param("code")
	if strings.TrimSpace(code) == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Vendor code wajib diisi"})
		return
	}

	if err := c.svc.ToggleActive(code); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "Gagal update status: " + err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"status": "success", "message": "Status vendor diperbarui"})
}

func (c *VendorMasterController) HandleSyncSAP(ctx *gin.Context) {
	session := sessions.Default(ctx)
	if session.Get("user_id") == nil {
		ctx.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Unauthorized"})
		return
	}

	targetURL := middlewareBaseURL() + "/finance/vendor-master/sync-sap"
	req, err := http.NewRequestWithContext(ctx.Request.Context(), http.MethodPost, targetURL, nil)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "Gagal membuat request sync: " + err.Error()})
		return
	}
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		ctx.JSON(http.StatusBadGateway, gin.H{"status": "error", "message": "Middleware Engine tidak dapat dihubungi: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	ctx.Data(resp.StatusCode, "application/json", body)
}
