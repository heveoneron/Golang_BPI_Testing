package controllers

import (
	"encoding/json"
	"fmt"
	"io"
	"middleware-panel/models"
	"middleware-panel/services"
	"net/http"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

type SAPEndpointController struct {
	svc services.SAPEndpointService
}

func NewSAPEndpointController(svc services.SAPEndpointService) *SAPEndpointController {
	return &SAPEndpointController{svc: svc}
}

func (c *SAPEndpointController) ShowEndpoints(ctx *gin.Context) {
	session := sessions.Default(ctx)
	if session.Get("user_id") == nil {
		ctx.Redirect(http.StatusFound, "/login")
		return
	}

	endpoints, err := c.svc.GetEndpoints()
	if err != nil {
		endpoints = []models.SAPEndpoint{}
	}

	username := "Admin"
	if u := session.Get("user_id"); u != nil {
		username = u.(string)
	}

	ctx.HTML(http.StatusOK, "sap_endpoints.html", gin.H{
		"Title":     "SAP Grow Integration Endpoints",
		"Username":  username,
		"Endpoints": endpoints,
		"ActiveTab": "sap-endpoints",
	})
}

func (c *SAPEndpointController) HandleSaveEndpoint(ctx *gin.Context) {
	session := sessions.Default(ctx)
	if session.Get("user_id") == nil {
		ctx.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Unauthorized"})
		return
	}

	var ep models.SAPEndpoint
	if err := ctx.ShouldBindJSON(&ep); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Format data tidak valid: " + err.Error()})
		return
	}

	if ep.ServiceCode == "" || ep.Username == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Service Code dan Username wajib diisi"})
		return
	}

	if err := c.svc.SaveEndpoint(&ep); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "Gagal menyimpan endpoint: " + err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Konfigurasi endpoint SAP berhasil disimpan",
	})
}

func (c *SAPEndpointController) HandleTestHandshake(ctx *gin.Context) {
	session := sessions.Default(ctx)
	if session.Get("user_id") == nil {
		ctx.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Unauthorized"})
		return
	}

	var req struct {
		ServiceCode string `json:"service_code"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil || req.ServiceCode == "" {
		req.ServiceCode = ctx.Query("service_code")
	}

	if req.ServiceCode == "" {
		req.ServiceCode = "PO"
	}

	result, err := c.svc.TestHandshake(req.ServiceCode)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, result)
}

func (c *SAPEndpointController) HandleExecuteSandbox(ctx *gin.Context) {
	session := sessions.Default(ctx)
	if session.Get("user_id") == nil {
		ctx.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Unauthorized"})
		return
	}

	bodyBytes, err := io.ReadAll(ctx.Request.Body)
	if err != nil || len(bodyBytes) == 0 {
		ctx.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Request body tidak boleh kosong"})
		return
	}

	var req struct {
		ServiceCode string      `json:"service_code"`
		Method      string      `json:"method"`
		CustomPath  string      `json:"custom_path"`
		Payload     interface{} `json:"payload"`
	}
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Format JSON tidak valid: " + err.Error()})
		return
	}

	if req.ServiceCode == "" {
		req.ServiceCode = "PO"
	}
	if req.Method == "" {
		req.Method = "POST"
	}

	result, err := c.svc.ExecuteSandbox(req.ServiceCode, req.Method, req.CustomPath, req.Payload)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, result)
}

func (c *SAPEndpointController) HandleDeleteEndpoint(ctx *gin.Context) {
	session := sessions.Default(ctx)
	if session.Get("user_id") == nil {
		ctx.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Unauthorized"})
		return
	}

	code := ctx.Param("code")
	if code == "" {
		code = ctx.Query("code")
	}

	if code == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Service code tidak boleh kosong"})
		return
	}

	if err := c.svc.DeleteEndpoint(code); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "Gagal menghapus endpoint: " + err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": fmt.Sprintf("Layanan SAP '%s' berhasil dihapus", code),
	})
}

func (c *SAPEndpointController) HandleGetMaterialDocument(ctx *gin.Context) {
	session := sessions.Default(ctx)
	if session.Get("user_id") == nil {
		ctx.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Unauthorized"})
		return
	}

	docNo := ctx.Param("doc_no")
	if docNo == "" {
		docNo = ctx.Query("doc_no")
	}
	year := ctx.DefaultQuery("year", "2025")

	if docNo == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Parameter doc_no wajib diisi"})
		return
	}

	result, err := c.svc.GetMaterialDocumentDetail(docNo, year)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, result)
}
