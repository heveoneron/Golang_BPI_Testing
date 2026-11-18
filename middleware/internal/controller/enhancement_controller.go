package controller

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"golang-bpi-testing/internal/service"

	"github.com/gin-gonic/gin"
)

// panelUser: nama user Panel yang diteruskan proxy Panel (header X-Panel-User).
func panelUser(ctx *gin.Context) string {
	if u := strings.TrimSpace(ctx.GetHeader("X-Panel-User")); u != "" {
		return u
	}
	return "api"
}

func respondEnhancementError(ctx *gin.Context, err error) {
	var ve *service.ValidationError
	if errors.As(err, &ve) {
		ctx.JSON(http.StatusUnprocessableEntity, gin.H{"status": "error", "message": strings.Join(ve.Messages, "; "), "errors": ve.Messages})
		return
	}
	ctx.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
}

// PreviewTransform godoc
// @Summary      Preview mapping staging -> SAP OData (inline edit Tab 1 Panel)
// @Description  Menjalankan ulang mapping engine untuk raw payload hasil edit tanpa menyimpan dan tanpa lock budget.
// @Tags         Documents
// @Accept       json
// @Produce      json
// @Param        body  body  object  true  "{doc_type, raw_payload}"
// @Router       /documents/transform-preview [post]
func (c *WebController) PreviewTransform(ctx *gin.Context) {
	if c.SyncSvc == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "message": "Sync service is not initialized"})
		return
	}
	var req struct {
		DocType    string          `json:"doc_type"`
		RawPayload json.RawMessage `json:"raw_payload"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.DocType) == "" || len(req.RawPayload) == 0 {
		ctx.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "doc_type dan raw_payload (objek JSON) wajib diisi"})
		return
	}
	out, err := c.SyncSvc.PreviewTransform(ctx.Request.Context(), req.DocType, req.RawPayload)
	if err != nil {
		ctx.JSON(http.StatusUnprocessableEntity, gin.H{"status": "error", "message": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"status": "success", "sap_payload": json.RawMessage(out)})
}

// POOutstandingTemplate godoc
// @Summary      Download template CSV PO Outstanding < 2025
// @Tags         PO Outstanding
// @Produce      text/csv
// @Router       /po-outstanding/template [get]
func (c *WebController) POOutstandingTemplate(ctx *gin.Context) {
	sample := []string{"IDR", "2024-01-08", "20", "4500010135", "BTNG", "VND-2721", "16011", "Data Communication Subscription Jan-Mar 2024", "92-00016", "3", "MONTH", "1", "11PPN", "3410000"}
	body := "\xEF\xBB\xBF" + strings.Join(service.POOutstandingTemplateHeaders, ";") + "\r\n" + strings.Join(sample, ";") + "\r\n"
	ctx.Header("Content-Disposition", `attachment; filename="Template_PO_Outstanding_before_2025.csv"`)
	ctx.Data(http.StatusOK, "text/csv; charset=utf-8", []byte(body))
}

// UploadPOOutstanding godoc
// @Summary      Upload template PO Outstanding < 2025 (.xlsx / .csv) ke staging
// @Tags         PO Outstanding
// @Accept       multipart/form-data
// @Produce      json
// @Param        file           formData  file    true  "Template PO Outstanding and AX (.xlsx / .csv)"
// @Param        source_system  formData  string  true  "MAXIMO / AX"
// @Router       /po-outstanding/upload [post]
func (c *WebController) UploadPOOutstanding(ctx *gin.Context) {
	fh, err := ctx.FormFile("file")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "file wajib diunggah"})
		return
	}
	if fh.Size > 20<<20 {
		ctx.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "ukuran file maksimal 20 MB"})
		return
	}
	f, err := fh.Open()
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 20<<20))
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
		return
	}
	res, err := c.SyncSvc.UploadPOOutstanding(ctx.Request.Context(), fh.Filename, data, ctx.PostForm("source_system"), panelUser(ctx))
	if err != nil {
		respondEnhancementError(ctx, err)
		return
	}
	if len(res.Errors) > 0 {
		ctx.JSON(http.StatusUnprocessableEntity, gin.H{"status": "error", "message": "file ditolak, perbaiki baris yang error lalu upload ulang", "data": res})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"status": "success", "message": "upload tersimpan", "data": res})
}

// ListPOOutstanding godoc
// @Summary      Daftar staging PO Outstanding < 2025
// @Tags         PO Outstanding
// @Produce      json
// @Param        status  query  string  false  "PENDING / PROCESSING / SYNCED / FAILED"
// @Param        q       query  string  false  "Cari ponum / vendor / itemnum"
// @Router       /po-outstanding [get]
func (c *WebController) ListPOOutstanding(ctx *gin.Context) {
	items, err := c.PGRepo.ListPOOutstanding(ctx.Request.Context(), ctx.Query("status"), ctx.Query("q"), 5000)
	if err != nil {
		respondEnhancementError(ctx, err)
		return
	}
	stats, _ := c.PGRepo.POOutstandingStats(ctx.Request.Context())
	ctx.JSON(http.StatusOK, gin.H{"status": "success", "data": items, "stats": stats})
}

// SyncPOOutstanding godoc
// @Summary      Kirim PO Outstanding < 2025 ke SAP (Sync Selected / Sync All Pending)
// @Tags         PO Outstanding
// @Accept       json
// @Produce      json
// @Param        body  body  object  true  "{ids: [id baris], all_pending: bool}"
// @Router       /po-outstanding/sync [post]
func (c *WebController) SyncPOOutstanding(ctx *gin.Context) {
	var req struct {
		IDs        []int64 `json:"ids"`
		AllPending bool    `json:"all_pending"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "format tidak valid"})
		return
	}
	res, err := c.SyncSvc.SyncPOOutstanding(ctx.Request.Context(), req.IDs, req.AllPending)
	if err != nil {
		respondEnhancementError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"status": "success", "data": res})
}
