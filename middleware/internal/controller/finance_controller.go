package controller

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"golang-bpi-testing/internal/model"
	"golang-bpi-testing/internal/repository"
	"golang-bpi-testing/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// maxUploadBytes membatasi ukuran file upload Excel/CSV (10 MB).
const maxUploadBytes = 10 << 20

// FinanceController menangani endpoint RICEF 1-9 di bawah /api/v1/finance.
type FinanceController struct {
	Svc *service.FinanceService
}

func NewFinanceController(svc *service.FinanceService) *FinanceController {
	return &FinanceController{Svc: svc}
}

// ValidationErrorResponse dipakai untuk error validasi (HTTP 422).
type ValidationErrorResponse struct {
	Error   string   `json:"error" example:"validasi gagal"`
	Details []string `json:"details"`
}

func (c *FinanceController) ready(ctx *gin.Context) bool {
	if c.Svc == nil || c.Svc.PGRepo == nil || c.Svc.PGRepo.Pool == nil {
		ctx.JSON(http.StatusServiceUnavailable, ErrorResponse{Error: "PostgreSQL database pool is not connected"})
		return false
	}
	return true
}

func respondFinanceError(ctx *gin.Context, err error) {
	var vErr *service.ValidationError
	switch {
	case errors.As(err, &vErr):
		ctx.JSON(http.StatusUnprocessableEntity, ValidationErrorResponse{Error: "validasi gagal", Details: vErr.Messages})
	case errors.Is(err, repository.ErrAlreadyPosted):
		ctx.JSON(http.StatusConflict, ErrorResponse{Error: err.Error()})
	case errors.Is(err, repository.ErrNotFound):
		ctx.JSON(http.StatusNotFound, ErrorResponse{Error: err.Error()})
	default:
		ctx.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
	}
}

func queryInt(ctx *gin.Context, key string, def int) int {
	if v, err := strconv.Atoi(ctx.Query(key)); err == nil && v > 0 {
		return v
	}
	return def
}

// ===================== RICEF 1 =====================

// ListBudgetMasters godoc
// @Summary      [RICEF 1] Daftar pagu budget
// @Description  Pagu per kombinasi CostCenter-BudgetCode beserta sisa (approved - lock RESERVED/COMMITTED)
// @Tags         Finance - Budget
// @Produce      json
// @Param        year  query  int  false  "Tahun budget (kosong = semua)"
// @Success      200  {object}  map[string]interface{}
// @Router       /finance/budget/masters [get]
func (c *FinanceController) ListBudgetMasters(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	year, _ := strconv.Atoi(ctx.Query("year"))
	masters, err := c.Svc.PGRepo.ListBudgetMasters(ctx.Request.Context(), year)
	if err != nil {
		respondFinanceError(ctx, err)
		return
	}
	adjustments, _ := c.Svc.PGRepo.ListBudgetAdjustments(ctx.Request.Context(), 100)
	ctx.JSON(http.StatusOK, gin.H{"status": "success", "data": masters, "adjustments": adjustments})
}

type budgetMasterRequest struct {
	MainAccount                 string   `json:"main_account" example:"513043"`
	BudgetCode                  string   `json:"budget_code" example:"28011"`
	Year                        int      `json:"year" example:"2026"`
	ApprovedBudget              float64  `json:"approved_budget" example:"500000000"`
	Type                        string   `json:"type,omitempty" example:"OPX"`
	RemainingBudgetPaymentBased *float64 `json:"remaining_budget_payment_based,omitempty"`
	Status                      string   `json:"status,omitempty" example:"APPR"`
}

// SaveBudgetMaster godoc
// @Summary      [RICEF 1] Simpan pagu budget
// @Tags         Finance - Budget
// @Accept       json
// @Produce      json
// @Param        payload  body  budgetMasterRequest  true  "Pagu"
// @Success      200  {object}  map[string]interface{}
// @Failure      422  {object}  ValidationErrorResponse
// @Router       /finance/budget/masters [post]
func (c *FinanceController) SaveBudgetMaster(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	var req budgetMasterRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	var errs []string
	if strings.TrimSpace(req.MainAccount) == "" || strings.TrimSpace(req.BudgetCode) == "" || req.Year <= 0 || req.ApprovedBudget < 0 {
		errs = append(errs, "main_account, budget_code, year wajib diisi dan approved_budget tidak boleh negatif")
	}
	req.Type = strings.ToUpper(strings.TrimSpace(req.Type))
	if req.Type != "" && req.Type != "OPX" && req.Type != "CPX" {
		errs = append(errs, "type harus OPX atau CPX")
	}
	if len(errs) > 0 {
		respondFinanceError(ctx, &service.ValidationError{Messages: errs})
		return
	}
	combo := service.BudgetCombination(req.MainAccount, req.BudgetCode)
	if err := c.Svc.PGRepo.UpsertBudgetMaster(ctx.Request.Context(), model.BudgetMaster{
		BudgetCombination: combo, BudgetYear: req.Year, BudgetCode: strings.TrimSpace(req.BudgetCode), Type: req.Type,
		ApprovedBudget: req.ApprovedBudget, RemainingBudgetPaymentBased: req.RemainingBudgetPaymentBased,
		Status: strings.ToUpper(strings.TrimSpace(req.Status)),
	}); err != nil {
		respondFinanceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"status": "success", "budget_combination": combo})
}

// SupplementBudget godoc
// @Summary      [RICEF 1] Supplement budget cost center
// @Tags         Finance - Budget
// @Accept       json
// @Produce      json
// @Param        payload  body  model.BudgetSupplementRequest  true  "Supplement"
// @Success      202  {object}  model.BudgetAdjustment
// @Failure      422  {object}  ValidationErrorResponse
// @Router       /finance/budget/supplement [post]
func (c *FinanceController) SupplementBudget(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	var req model.BudgetSupplementRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	adj, err := c.Svc.SupplementBudget(ctx.Request.Context(), req)
	if err != nil {
		respondFinanceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusAccepted, adj)
}

// TransferBudget godoc
// @Summary      [RICEF 1] Transfer budget antar cost center
// @Tags         Finance - Budget
// @Accept       json
// @Produce      json
// @Param        payload  body  model.BudgetTransferRequest  true  "Transfer"
// @Success      202  {object}  model.BudgetAdjustment
// @Failure      422  {object}  ValidationErrorResponse
// @Router       /finance/budget/transfer [post]
func (c *FinanceController) TransferBudget(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	var req model.BudgetTransferRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	adj, err := c.Svc.TransferBudget(ctx.Request.Context(), req)
	if err != nil {
		respondFinanceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusAccepted, adj)
}

// ListBudgetLocks godoc
// @Summary      [RICEF 1] Daftar budget lock
// @Tags         Finance - Budget
// @Produce      json
// @Param        status  query  string  false  "RESERVED / COMMITTED / RELEASED"
// @Success      200  {object}  map[string]interface{}
// @Router       /finance/budget/locks [get]
func (c *FinanceController) ListBudgetLocks(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	locks, err := c.Svc.PGRepo.ListBudgetLocks(ctx.Request.Context(), ctx.Query("status"), queryInt(ctx, "limit", 200))
	if err != nil {
		respondFinanceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"status": "success", "data": locks})
}

type releaseLockRequest struct {
	ReferenceDocType string `json:"reference_doc_type" example:"PO"`
	ReferenceDocNo   string `json:"reference_doc_no" example:"4500016528"`
}

// ReleaseBudgetLock godoc
// @Summary      [RICEF 1] Lepas budget lock manual
// @Tags         Finance - Budget
// @Accept       json
// @Produce      json
// @Param        payload  body  releaseLockRequest  true  "Referensi dokumen"
// @Success      200  {object}  map[string]interface{}
// @Router       /finance/budget/locks/release [post]
func (c *FinanceController) ReleaseBudgetLock(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	var req releaseLockRequest
	if err := ctx.ShouldBindJSON(&req); err != nil || req.ReferenceDocType == "" || req.ReferenceDocNo == "" {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{Error: "reference_doc_type dan reference_doc_no wajib diisi"})
		return
	}
	n, err := c.Svc.PGRepo.ReleaseBudgetLocksManual(ctx.Request.Context(), strings.ToUpper(req.ReferenceDocType), req.ReferenceDocNo)
	if err != nil {
		respondFinanceError(ctx, err)
		return
	}
	if n == 0 {
		ctx.JSON(http.StatusNotFound, ErrorResponse{Error: "tidak ada lock aktif untuk referensi tersebut"})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"status": "success", "released": n})
}

// ===================== RICEF 2 =====================

// CreateJournalEntry godoc
// @Summary      [RICEF 2] Buat Journal Entry GI (dikirim via SOAP)
// @Description  Validasi keseimbangan debit/kredit, tahan pagu per cost center + budget code, lalu antre kirim SOAP
// @Tags         Finance - Journal
// @Accept       json
// @Produce      json
// @Param        payload  body  model.JournalEntryRequest  true  "Jurnal"
// @Success      202  {object}  map[string]interface{}
// @Failure      409  {object}  ErrorResponse
// @Failure      422  {object}  ValidationErrorResponse
// @Router       /finance/journal-entries [post]
func (c *FinanceController) CreateJournalEntry(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	var req model.JournalEntryRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	ref, err := c.Svc.CreateJournalEntry(ctx.Request.Context(), req)
	if err != nil {
		respondFinanceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusAccepted, gin.H{"status": "queued", "doc_type": model.DocTypeJournalGI, "document_no": ref})
}

// ListJournalEntries godoc
// @Summary      [RICEF 2] Daftar Journal Entry GI
// @Tags         Finance - Journal
// @Produce      json
// @Success      200  {object}  map[string]interface{}
// @Router       /finance/journal-entries [get]
func (c *FinanceController) ListJournalEntries(ctx *gin.Context) {
	c.listDocuments(ctx, model.DocTypeJournalGI)
}

func (c *FinanceController) listDocuments(ctx *gin.Context, docType string) {
	if !c.ready(ctx) {
		return
	}
	docs, err := c.Svc.PGRepo.ListDocumentsByType(ctx.Request.Context(), docType, queryInt(ctx, "limit", 200))
	if err != nil {
		respondFinanceError(ctx, err)
		return
	}
	out := make([]gin.H, 0, len(docs))
	for _, d := range docs {
		out = append(out, gin.H{
			"id": d.ID, "document_no": d.DocumentNo, "status": d.Status, "retry_count": d.RetryCount,
			"sap_document_no": d.SAPDocumentNo, "error_message": d.ErrorMessage, "raw_payload": d.RawPayload,
			"created_at": d.CreatedAt, "updated_at": d.UpdatedAt,
		})
	}
	ctx.JSON(http.StatusOK, gin.H{"status": "success", "data": out})
}

// ===================== RICEF 3/4, 5, 9 =====================

// ListGLLineItems godoc
// @Summary      [RICEF 3/4] Daftar GL line item tersimpan
// @Tags         Finance - SAP Pull
// @Produce      json
// @Param        category      query  string  false  "journal (RICEF 3) / invoice (RICEF 4)"
// @Param        company_code  query  string  false  "Company code"
// @Param        from          query  string  false  "Posting date dari (YYYY-MM-DD)"
// @Param        to            query  string  false  "Posting date sampai (YYYY-MM-DD)"
// @Param        q             query  string  false  "Cari nomor dokumen / GL / teks"
// @Success      200  {object}  map[string]interface{}
// @Router       /finance/gl-line-items [get]
func (c *FinanceController) ListGLLineItems(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	items, err := c.Svc.PGRepo.ListGLLineItems(ctx.Request.Context(), repository.GLLineItemFilter{
		CompanyCode: ctx.Query("company_code"),
		FromDate:    ctx.Query("from"),
		ToDate:      ctx.Query("to"),
		DocTypes:    service.DocTypesFor(ctx.Query("category")),
		Search:      ctx.Query("q"),
		Limit:       queryInt(ctx, "limit", 500),
	})
	if err != nil {
		respondFinanceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"status": "success", "data": items})
}

// GetGLLineItemDetail godoc
// @Summary      [RICEF 3/4] Detail GL line item langsung dari SAP
// @Tags         Finance - SAP Pull
// @Produce      json
// @Param        key  query  string  true  "Composite key GLAccountLineItem"
// @Success      200  {object}  map[string]interface{}
// @Router       /finance/gl-line-items/detail [get]
func (c *FinanceController) GetGLLineItemDetail(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	body, err := c.Svc.GetGLLineItemDetail(ctx.Request.Context(), ctx.Query("key"))
	if err != nil {
		respondFinanceError(ctx, err)
		return
	}
	ctx.Data(http.StatusOK, "application/json", body)
}

// ListPaymentProposals godoc
// @Summary      [RICEF 5] Daftar payment proposal tersimpan
// @Tags         Finance - SAP Pull
// @Produce      json
// @Param        q  query  string  false  "Cari payment run / supplier / dokumen"
// @Success      200  {object}  map[string]interface{}
// @Router       /finance/payment-proposals [get]
func (c *FinanceController) ListPaymentProposals(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	items, err := c.Svc.PGRepo.ListPaymentProposals(ctx.Request.Context(), ctx.Query("q"), queryInt(ctx, "limit", 500))
	if err != nil {
		respondFinanceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"status": "success", "data": items})
}

// ListFixedAssets godoc
// @Summary      [RICEF 9] Daftar fixed asset tersimpan
// @Tags         Finance - SAP Pull
// @Produce      json
// @Param        company_code  query  string  false  "Company code"
// @Param        q             query  string  false  "Cari nomor / deskripsi aset"
// @Success      200  {object}  map[string]interface{}
// @Router       /finance/fixed-assets [get]
func (c *FinanceController) ListFixedAssets(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	items, err := c.Svc.PGRepo.ListFixedAssets(ctx.Request.Context(), ctx.Query("company_code"), ctx.Query("q"), queryInt(ctx, "limit", 500))
	if err != nil {
		respondFinanceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"status": "success", "data": items})
}

// GetFixedAssetDetail godoc
// @Summary      [RICEF 9] Detail fixed asset langsung dari SAP
// @Tags         Finance - SAP Pull
// @Produce      json
// @Param        company_code        query  string  false  "Company code (default 5110)"
// @Param        master_fixed_asset  query  string  true   "Master fixed asset"
// @Param        fixed_asset         query  string  false  "Sub-number (default 0)"
// @Success      200  {object}  map[string]interface{}
// @Router       /finance/fixed-assets/detail [get]
func (c *FinanceController) GetFixedAssetDetail(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	body, err := c.Svc.GetFixedAssetDetail(ctx.Request.Context(), ctx.Query("company_code"), ctx.Query("master_fixed_asset"), ctx.Query("fixed_asset"))
	if err != nil {
		respondFinanceError(ctx, err)
		return
	}
	ctx.Data(http.StatusOK, "application/json", body)
}

// TriggerSync godoc
// @Summary      [RICEF 3/4, 5, 9] Tarik data dari SAP sekarang
// @Tags         Finance - SAP Pull
// @Produce      json
// @Param        target        path   string  true   "gl / payment / asset / all"
// @Param        company_code  query  string  false  "Company code (gl, asset)"
// @Param        from          query  string  false  "gl: posting date dari (YYYY-MM-DD)"
// @Param        to            query  string  false  "gl: posting date sampai (YYYY-MM-DD)"
// @Param        filter        query  string  false  "payment: \$filter OData opsional"
// @Success      200  {object}  map[string]interface{}
// @Router       /finance/sync/{target} [post]
func (c *FinanceController) TriggerSync(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	reqCtx := ctx.Request.Context()
	var results []model.SyncResult
	switch strings.ToLower(ctx.Param("target")) {
	case "gl":
		from, to := ctx.Query("from"), ctx.Query("to")
		for _, d := range []string{from, to} {
			if d != "" && !isISODate(d) {
				respondFinanceError(ctx, &service.ValidationError{Messages: []string{"from/to wajib format YYYY-MM-DD"}})
				return
			}
		}
		results = append(results, c.Svc.SyncGLLineItems(reqCtx, service.GLSyncOptions{
			CompanyCode: ctx.Query("company_code"), FromDate: from, ToDate: to,
			DocTypes: service.DocTypesFor(ctx.Query("category")),
		}))
	case "payment":
		results = append(results, c.Svc.SyncPaymentProposals(reqCtx, ctx.Query("filter")))
	case "invoice":
		from, to := ctx.Query("from"), ctx.Query("to")
		for _, d := range []string{from, to} {
			if d != "" && !isISODate(d) {
				respondFinanceError(ctx, &service.ValidationError{Messages: []string{"from/to wajib format YYYY-MM-DD"}})
				return
			}
		}
		results = append(results, c.Svc.SyncSupplierInvoices(reqCtx, service.InvoicePullOptions{
			CompanyCode: ctx.Query("company_code"), FromDate: from, ToDate: to,
		}))
	case "asset":
		results = append(results, c.Svc.SyncFixedAssets(reqCtx, ctx.Query("company_code")))
	case "all":
		results = c.Svc.SyncAll(reqCtx)
	default:
		ctx.JSON(http.StatusBadRequest, ErrorResponse{Error: "target harus gl, payment, asset, invoice, atau all"})
		return
	}
	status := http.StatusOK
	for _, r := range results {
		if r.Error != "" {
			status = http.StatusBadGateway
		}
	}
	ctx.JSON(status, gin.H{"status": map[bool]string{true: "success", false: "error"}[status == http.StatusOK], "results": results})
}

func isISODate(s string) bool {
	return len(s) == 10 && s[4] == '-' && s[7] == '-'
}

// ===================== RICEF 6 =====================

type queueExchangeRateRequest struct {
	KMKNumber string `json:"kmk_number" example:"KMK 52/KM.10/2026"`
}

// QueueExchangeRates godoc
// @Summary      [RICEF 6] Antre kirim kurs KMK ke SAP
// @Tags         Finance - Exchange Rate
// @Accept       json
// @Produce      json
// @Param        payload  body  queueExchangeRateRequest  false  "Nomor KMK (kosong = terbaru)"
// @Success      202  {object}  service.ExchangeRateQueueResult
// @Router       /finance/exchange-rates/queue [post]
func (c *FinanceController) QueueExchangeRates(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	var req queueExchangeRateRequest
	_ = ctx.ShouldBindJSON(&req)
	if req.KMKNumber == "" {
		req.KMKNumber = ctx.Query("kmk_number")
	}
	res, err := c.Svc.QueueExchangeRates(ctx.Request.Context(), req.KMKNumber)
	if err != nil {
		respondFinanceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusAccepted, res)
}

// ListExchangeRateDocuments godoc
// @Summary      [RICEF 6] Status pengiriman kurs ke SAP
// @Tags         Finance - Exchange Rate
// @Produce      json
// @Success      200  {object}  map[string]interface{}
// @Router       /finance/exchange-rates/documents [get]
func (c *FinanceController) ListExchangeRateDocuments(ctx *gin.Context) {
	c.listDocuments(ctx, model.DocTypeExchangeRate)
}

// ===================== RICEF 7 =====================

// UploadSupplierInvoices godoc
// @Summary      [RICEF 7] Upload Excel/CSV reimbursement & business trip
// @Description  Seluruh file ditolak bila ada baris tidak valid (HTTP 422 dengan daftar error per baris)
// @Tags         Finance - Supplier Invoice
// @Accept       multipart/form-data
// @Produce      json
// @Param        file         formData  file    true   "File .xlsx / .csv"
// @Param        uploaded_by  formData  string  false  "Pengunggah"
// @Success      202  {object}  service.UploadResult
// @Failure      422  {object}  service.UploadResult
// @Router       /finance/supplier-invoices/upload [post]
func (c *FinanceController) UploadSupplierInvoices(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, maxUploadBytes+1<<20)
	fh, err := ctx.FormFile("file")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{Error: "field 'file' wajib berisi file .xlsx / .csv"})
		return
	}
	if fh.Size > maxUploadBytes {
		ctx.JSON(http.StatusRequestEntityTooLarge, ErrorResponse{Error: "ukuran file maksimal 10 MB"})
		return
	}
	f, err := fh.Open()
	if err != nil {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxUploadBytes))
	if err != nil {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	// Query uploaded_by (diisi Panel dari sesi login) diutamakan di atas field form.
	uploadedBy := ctx.Query("uploaded_by")
	if uploadedBy == "" {
		uploadedBy = ctx.PostForm("uploaded_by")
	}
	res, err := c.Svc.UploadSupplierInvoices(ctx.Request.Context(), fh.Filename, uploadedBy, data)
	if err != nil {
		respondFinanceError(ctx, err)
		return
	}
	if res.Batch.Status == "REJECTED" {
		ctx.JSON(http.StatusUnprocessableEntity, res)
		return
	}
	ctx.JSON(http.StatusAccepted, res)
}

// ListSupplierInvoices godoc
// @Summary      [RICEF 7] Monitoring supplier invoice hasil upload
// @Tags         Finance - Supplier Invoice
// @Produce      json
// @Success      200  {object}  map[string]interface{}
// @Router       /finance/supplier-invoices [get]
func (c *FinanceController) ListSupplierInvoices(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	items, err := c.Svc.ListSupplierInvoices(ctx.Request.Context(), queryInt(ctx, "limit", 200))
	if err != nil {
		respondFinanceError(ctx, err)
		return
	}
	batches, _ := c.Svc.PGRepo.ListUploadBatches(ctx.Request.Context(), 20)
	ctx.JSON(http.StatusOK, gin.H{"status": "success", "data": items, "batches": batches})
}

// ListReimbursementMappings godoc
// @Summary      [RICEF 7] Mapping kebijakan reimbursement -> GL & budget code
// @Tags         Finance - Supplier Invoice
// @Produce      json
// @Success      200  {object}  map[string]interface{}
// @Router       /finance/reimbursement-mappings [get]
func (c *FinanceController) ListReimbursementMappings(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	items, err := c.Svc.PGRepo.ListReimbursementMappings(ctx.Request.Context())
	if err != nil {
		respondFinanceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"status": "success", "data": items})
}

// SaveReimbursementMapping godoc
// @Summary      [RICEF 7] Simpan mapping kebijakan reimbursement
// @Description  Insert / update berdasarkan nama kebijakan + CIP flag
// @Tags         Finance - Supplier Invoice
// @Accept       json
// @Produce      json
// @Param        payload  body  model.ReimbursementGLMapping  true  "Mapping"
// @Success      200  {object}  map[string]interface{}
// @Failure      422  {object}  ValidationErrorResponse
// @Router       /finance/reimbursement-mappings [post]
func (c *FinanceController) SaveReimbursementMapping(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	m := model.ReimbursementGLMapping{IsActive: true}
	if err := ctx.ShouldBindJSON(&m); err != nil {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	flag, ok := model.NormalizeCIPFlag(m.CIPFlag)
	m.PolicyName, m.GLAccount, m.BudgetCode = strings.TrimSpace(m.PolicyName), strings.TrimSpace(m.GLAccount), strings.TrimSpace(m.BudgetCode)
	if !ok || m.PolicyName == "" || m.GLAccount == "" || m.BudgetCode == "" {
		respondFinanceError(ctx, &service.ValidationError{Messages: []string{"policy_name, cip_flag (CIP / Non CIP), gl_account, budget_code wajib diisi"}})
		return
	}
	m.CIPFlag = flag
	if err := c.Svc.PGRepo.SaveReimbursementMapping(ctx.Request.Context(), m); err != nil {
		respondFinanceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"status": "success"})
}

// DeleteReimbursementMapping godoc
// @Summary      [RICEF 7] Hapus mapping kebijakan reimbursement
// @Tags         Finance - Supplier Invoice
// @Param        id  path  int  true  "ID mapping"
// @Success      200  {object}  map[string]interface{}
// @Router       /finance/reimbursement-mappings/{id} [delete]
func (c *FinanceController) DeleteReimbursementMapping(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	id, err := strconv.Atoi(ctx.Param("id"))
	if err != nil {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{Error: "id tidak valid"})
		return
	}
	if err := c.Svc.PGRepo.DeleteReimbursementMapping(ctx.Request.Context(), id); err != nil {
		respondFinanceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"status": "success"})
}

// ListEmployeeVendors godoc
// @Summary      [RICEF 7] Daftar vendor karyawan (CIP / Non CIP)
// @Tags         Finance - Supplier Invoice
// @Produce      json
// @Param        q  query  string  false  "Cari kode / deskripsi"
// @Success      200  {object}  map[string]interface{}
// @Router       /finance/employee-vendors [get]
func (c *FinanceController) ListEmployeeVendors(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	items, err := c.Svc.PGRepo.ListEmployeeVendors(ctx.Request.Context(), ctx.Query("q"), queryInt(ctx, "limit", 1000))
	if err != nil {
		respondFinanceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"status": "success", "data": items})
}

// SaveEmployeeVendor godoc
// @Summary      [RICEF 7] Simpan satu vendor karyawan
// @Tags         Finance - Supplier Invoice
// @Accept       json
// @Produce      json
// @Param        payload  body  model.EmployeeVendorMapping  true  "Vendor karyawan"
// @Success      200  {object}  map[string]interface{}
// @Router       /finance/employee-vendors [post]
func (c *FinanceController) SaveEmployeeVendor(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	m := model.EmployeeVendorMapping{IsActive: true}
	if err := ctx.ShouldBindJSON(&m); err != nil {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	flag, ok := model.NormalizeCIPFlag(m.CIPFlag)
	m.VendorCode = strings.TrimSpace(m.VendorCode)
	if !ok || m.VendorCode == "" {
		respondFinanceError(ctx, &service.ValidationError{Messages: []string{"vendor_code dan cip_flag (CIP / Non CIP) wajib diisi"}})
		return
	}
	m.CIPFlag = flag
	if _, err := c.Svc.PGRepo.UpsertEmployeeVendors(ctx.Request.Context(), []model.EmployeeVendorMapping{m}); err != nil {
		respondFinanceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"status": "success"})
}

// ImportEmployeeVendors godoc
// @Summary      [RICEF 7] Import vendor karyawan dari .xlsx / .csv
// @Description  Kolom: Vendor Code, CIP atau "Comment (CIP/Non CIP)", opsional Description. Satu baris salah = tidak ada yang disimpan.
// @Tags         Finance - Supplier Invoice
// @Accept       multipart/form-data
// @Produce      json
// @Param        file  formData  file  true  "File .xlsx / .csv"
// @Success      200  {object}  map[string]interface{}
// @Failure      422  {object}  map[string]interface{}
// @Router       /finance/employee-vendors/import [post]
func (c *FinanceController) ImportEmployeeVendors(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, maxUploadBytes+1<<20)
	fh, err := ctx.FormFile("file")
	if err != nil || fh.Size > maxUploadBytes {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{Error: "field 'file' wajib berisi file .xlsx / .csv (maks 10 MB)"})
		return
	}
	f, err := fh.Open()
	if err != nil {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxUploadBytes))
	if err != nil {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	n, rowErrs, err := c.Svc.ImportEmployeeVendors(ctx.Request.Context(), fh.Filename, data)
	if err != nil {
		respondFinanceError(ctx, err)
		return
	}
	if len(rowErrs) > 0 {
		ctx.JSON(http.StatusUnprocessableEntity, gin.H{"status": "rejected", "errors": rowErrs})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"status": "success", "imported": n})
}

// DeleteEmployeeVendor godoc
// @Summary      [RICEF 7] Hapus vendor karyawan
// @Tags         Finance - Supplier Invoice
// @Param        code  path  string  true  "Vendor code"
// @Success      200  {object}  map[string]interface{}
// @Router       /finance/employee-vendors/{code} [delete]
func (c *FinanceController) DeleteEmployeeVendor(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	if err := c.Svc.PGRepo.DeleteEmployeeVendor(ctx.Request.Context(), ctx.Param("code")); err != nil {
		respondFinanceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"status": "success"})
}

// ===================== SAP -> Maximo (dbo.mxinvoice) =====================

type maximoExportRequest struct {
	DryRun bool     `json:"dry_run" example:"true"`
	Limit  int      `json:"limit" example:"50"`
	Types  []string `json:"types" example:"INVOICE,JOURNAL"`
}

// ExportToMaximo godoc
// @Summary      Tulis invoice & jurnal SAP ke Maximo (dbo.mxinvoice)
// @Description  dry_run=true hanya menampilkan baris yang akan ditulis. Tanpa dry_run, setiap baris ditulis ke dbo.mxinvoice + dbo.mxin_inter_trans.
// @Tags         Finance - SAP to Maximo
// @Accept       json
// @Produce      json
// @Param        payload  body  maximoExportRequest  true  "Opsi"
// @Success      200  {object}  service.MaximoExportResult
// @Router       /finance/maximo/export [post]
func (c *FinanceController) ExportToMaximo(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	var req maximoExportRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	res, err := c.Svc.ExportToMaximo(ctx.Request.Context(), service.MaximoExportOptions{DryRun: req.DryRun, Limit: req.Limit, Types: req.Types})
	if err != nil {
		respondFinanceError(ctx, err)
		return
	}
	status := http.StatusOK
	if res.Failed > 0 {
		status = http.StatusMultiStatus
	}
	ctx.JSON(status, res)
}

// ListMaximoExports godoc
// @Summary      Riwayat tulis ke Maximo
// @Tags         Finance - SAP to Maximo
// @Produce      json
// @Success      200  {object}  map[string]interface{}
// @Router       /finance/maximo/exports [get]
func (c *FinanceController) ListMaximoExports(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	items, err := c.Svc.PGRepo.ListMaximoExports(ctx.Request.Context(), queryInt(ctx, "limit", 200))
	if err != nil {
		respondFinanceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"status": "success", "data": items, "writer_configured": c.Svc.MaximoWriter != nil})
}

// ===================== RICEF 8 =====================

// ListAssetAcquisitions godoc
// @Summary      [RICEF 8] Daftar parking jurnal aset
// @Tags         Finance - Asset
// @Produce      json
// @Param        status  query  string  false  "PARKED / APPROVED / POSTED / FAILED / REJECTED"
// @Success      200  {object}  map[string]interface{}
// @Router       /finance/assets/acquisitions [get]
func (c *FinanceController) ListAssetAcquisitions(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	items, err := c.Svc.PGRepo.ListAssetAcquisitions(ctx.Request.Context(), ctx.Query("status"), queryInt(ctx, "limit", 200))
	if err != nil {
		respondFinanceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"status": "success", "data": items})
}

// GetAssetAcquisition godoc
// @Summary      [RICEF 8] Detail parking jurnal aset
// @Tags         Finance - Asset
// @Produce      json
// @Param        id  path  string  true  "ID parking"
// @Success      200  {object}  model.AssetAcquisition
// @Router       /finance/assets/acquisitions/{id} [get]
func (c *FinanceController) GetAssetAcquisition(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	id, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{Error: "id tidak valid"})
		return
	}
	a, err := c.Svc.PGRepo.GetAssetAcquisition(ctx.Request.Context(), id)
	if err != nil {
		respondFinanceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, a)
}

// ParkAssetAcquisition godoc
// @Summary      [RICEF 8] Parking jurnal pengakuan aset
// @Tags         Finance - Asset
// @Accept       json
// @Produce      json
// @Param        payload  body  model.AssetAcquisitionRequest  true  "Parking aset"
// @Success      201  {object}  model.AssetAcquisition
// @Failure      422  {object}  ValidationErrorResponse
// @Router       /finance/assets/acquisitions [post]
func (c *FinanceController) ParkAssetAcquisition(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	var req model.AssetAcquisitionRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	a, err := c.Svc.ParkAssetAcquisition(ctx.Request.Context(), req)
	if err != nil {
		respondFinanceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusCreated, a)
}

// ApproveAssetAcquisition godoc
// @Summary      [RICEF 8] Approve parking aset dan antre posting ke SAP
// @Description  Approver wajib berbeda dari pembuat parking (maker-checker)
// @Tags         Finance - Asset
// @Accept       json
// @Produce      json
// @Param        id       path  string                 true  "ID parking"
// @Param        payload  body  model.ApprovalRequest  true  "Approver"
// @Success      200  {object}  model.AssetAcquisition
// @Failure      422  {object}  ValidationErrorResponse
// @Router       /finance/assets/acquisitions/{id}/approve [post]
func (c *FinanceController) ApproveAssetAcquisition(ctx *gin.Context) {
	c.assetTransition(ctx, true)
}

// RejectAssetAcquisition godoc
// @Summary      [RICEF 8] Tolak parking aset
// @Tags         Finance - Asset
// @Accept       json
// @Produce      json
// @Param        id       path  string                 true  "ID parking"
// @Param        payload  body  model.ApprovalRequest  true  "Penolak dan alasan"
// @Success      200  {object}  model.AssetAcquisition
// @Failure      422  {object}  ValidationErrorResponse
// @Router       /finance/assets/acquisitions/{id}/reject [post]
func (c *FinanceController) RejectAssetAcquisition(ctx *gin.Context) {
	c.assetTransition(ctx, false)
}

func (c *FinanceController) assetTransition(ctx *gin.Context, approve bool) {
	if !c.ready(ctx) {
		return
	}
	id, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{Error: "id tidak valid"})
		return
	}
	var req model.ApprovalRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	var a *model.AssetAcquisition
	if approve {
		a, err = c.Svc.ApproveAssetAcquisition(ctx.Request.Context(), id, req.By)
	} else {
		a, err = c.Svc.RejectAssetAcquisition(ctx.Request.Context(), id, req.By, req.Reason)
	}
	if err != nil {
		respondFinanceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, a)
}
