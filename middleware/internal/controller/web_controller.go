package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"golang-bpi-testing/internal/model"
	"golang-bpi-testing/internal/repository"
	"golang-bpi-testing/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type WebController struct {
	PGRepo  *repository.PostgresRepository
	SyncSvc *service.SyncService
}

func NewWebController(pgRepo *repository.PostgresRepository, syncSvc *service.SyncService) *WebController {
	return &WebController{
		PGRepo:  pgRepo,
		SyncSvc: syncSvc,
	}
}

// DTO Responses untuk Swagger Documentation
type HealthResponse struct {
	Service string `json:"service" example:"BPI Maximo-SAP Grow Middleware"`
	Status  string `json:"status" example:"running"`
}

type IngestManualPOResponse struct {
	Message     string `json:"message" example:"PO successfully ingested into staging_documents and transformed to core documents"`
	DocumentNo  string `json:"document_no" example:"PO-BPI-2026-001"`
	Transformed int    `json:"transformed" example:"1"`
}

type TriggerPushResponse struct {
	Message     string `json:"message" example:"Push trigger executed"`
	PushedCount int    `json:"pushed_count" example:"1"`
}

type TriggerPullResponse struct {
	Message     string `json:"message" example:"Pull trigger executed successfully"`
	PulledCount int    `json:"pulled_count" example:"1"`
	Transformed int    `json:"transformed" example:"1"`
	Limit       int    `json:"limit" example:"1"`
}

type DocumentItemDTO struct {
	ID            uuid.UUID   `json:"id" example:"a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11"`
	DocType       string      `json:"doc_type" example:"PO"`
	DocumentNo    string      `json:"document_no" example:"PO-BPI-2026-001"`
	GroupID       string      `json:"group_id" example:"GRP-PO-PO-BPI-2026-001"`
	Status        string      `json:"status" example:"pending"`
	RetryCount    int         `json:"retry_count" example:"0"`
	SAPDocumentNo *string     `json:"sap_document_no,omitempty" example:"SAP-PO-1730000000"`
	RawPayload    interface{} `json:"raw_payload" swaggertype:"object"`
	SAPPayload    interface{} `json:"sap_payload" swaggertype:"object"`
	CreatedAt     interface{} `json:"created_at" swaggertype:"string" example:"2026-10-12T14:00:00Z"`
}

type GetDocumentsResponse struct {
	Total int               `json:"total" example:"1"`
	Data  []DocumentItemDTO `json:"data"`
}

type ErrorResponse struct {
	Error string `json:"error" example:"invalid request body"`
}

// IngestManualPO godoc
// @Summary      Ingest Manual PO Maximo
// @Description  Simulasi webhook atau inject payload PO Maximo secara langsung ke staging_documents dan mentransformasikannya ke documents
// @Tags         Ingestion
// @Accept       json
// @Produce      json
// @Param        payload  body      model.MaximoPOHeader  true  "Maximo PO Header JSON Payload"
// @Success      200      {object}  IngestManualPOResponse
// @Failure      400      {object}  ErrorResponse
// @Failure      500      {object}  ErrorResponse
// @Failure      503      {object}  ErrorResponse
// @Router       /ingest/po [post]
func (c *WebController) IngestManualPO(ctx *gin.Context) {
	if c.PGRepo == nil || c.PGRepo.Pool == nil {
		ctx.JSON(http.StatusServiceUnavailable, ErrorResponse{Error: "PostgreSQL database pool is not connected"})
		return
	}

	var maxPO model.MaximoPOHeader
	if err := ctx.ShouldBindJSON(&maxPO); err != nil {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	payloadBytes, _ := json.Marshal(maxPO)
	stgDoc := model.StagingDocument{
		ID:         uuid.New(),
		DocType:    "PO",
		DocumentNo: maxPO.PONum,
		FlowType:   "forward",
		RawPayload: payloadBytes,
		Status:     "PENDING",
	}

	if err := c.PGRepo.UpsertStagingDocument(ctx.Request.Context(), stgDoc); err != nil {
		ctx.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	// Langsung trigger transform
	transformed := 0
	if c.SyncSvc != nil {
		transformed = c.SyncSvc.TransformStagingToCore(ctx.Request.Context())
	}

	ctx.JSON(http.StatusOK, IngestManualPOResponse{
		Message:     "PO successfully ingested into staging_documents and transformed to core documents",
		DocumentNo:  maxPO.PONum,
		Transformed: transformed,
	})
}

// IngestManualVendor godoc
// @Summary      Ingest Manual Vendor / Business Partner Maximo
// @Description  Simulasi webhook atau inject payload Vendor Maximo (atau transid) ke staging_documents dan mentransformasikannya ke documents siap kirim ke SAP Business Partner
// @Tags         Ingestion
// @Accept       json
// @Produce      json
// @Param        payload  body      model.MaximoVendorRecord  true  "Maximo Vendor JSON Payload"
// @Success      200      {object}  IngestManualPOResponse
// @Failure      400      {object}  ErrorResponse
// @Failure      500      {object}  ErrorResponse
// @Failure      503      {object}  ErrorResponse
// @Router       /ingest/vendor [post]
func (c *WebController) IngestManualVendor(ctx *gin.Context) {
	if c.PGRepo == nil || c.PGRepo.Pool == nil {
		ctx.JSON(http.StatusServiceUnavailable, ErrorResponse{Error: "PostgreSQL database pool is not connected"})
		return
	}

	var req map[string]interface{}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	var docNo string
	var payloadBytes []byte

	// Jika diberikan transid dan database SQL Server tersedia, coba tarik dari axvendor
	if tidVal, ok := req["transid"]; ok && c.SyncSvc != nil && c.SyncSvc.SQLRepo != nil {
		var tid int64
		switch t := tidVal.(type) {
		case float64:
			tid = int64(t)
		case string:
			tid, _ = strconv.ParseInt(t, 10, 64)
		}
		if tid > 0 {
			rec, err := c.SyncSvc.SQLRepo.ExtractVendorByTransID(ctx.Request.Context(), tid)
			if err == nil && rec != nil {
				docNo = rec.DocumentNo
				payloadBytes = rec.PayloadJSON
			}
		}
	}

	if len(payloadBytes) == 0 {
		// Ambil langsung dari request body
		company := ""
		for _, k := range []string{"COMPANY", "company", "Company", "BusinessPartner"} {
			if v, ok := req[k].(string); ok && strings.TrimSpace(v) != "" {
				company = strings.TrimSpace(v)
				break
			}
		}
		if company == "" {
			ctx.JSON(http.StatusBadRequest, ErrorResponse{Error: "Field COMPANY / company / BusinessPartner wajib diisi"})
			return
		}
		docNo = company
		payloadBytes, _ = json.Marshal(req)
	}

	stgDoc := model.StagingDocument{
		ID:         uuid.New(),
		DocType:    "AXVENDOR",
		DocumentNo: docNo,
		FlowType:   "forward",
		RawPayload: payloadBytes,
		Status:     "PENDING",
	}

	if err := c.PGRepo.UpsertStagingDocument(ctx.Request.Context(), stgDoc); err != nil {
		ctx.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	transformed := 0
	if c.SyncSvc != nil {
		transformed = c.SyncSvc.TransformStagingToCore(ctx.Request.Context())
	}

	ctx.JSON(http.StatusOK, IngestManualPOResponse{
		Message:     "Vendor successfully ingested into staging_documents and transformed to core documents",
		DocumentNo:  docNo,
		Transformed: transformed,
	})
}

// TriggerPush godoc
// @Summary      Trigger Push Dispatcher ke SAP Grow
// @Description  Memicu eksekusi manual worker push untuk mengirim dokumen pending ke SAP Grow OData
// @Tags         Dispatching
// @Produce      json
// @Success      200  {object}  TriggerPushResponse
// @Failure      503  {object}  ErrorResponse
// @Router       /trigger/push [post]
func (c *WebController) TriggerPush(ctx *gin.Context) {
	if c.SyncSvc == nil {
		ctx.JSON(http.StatusServiceUnavailable, ErrorResponse{Error: "Sync service is not initialized"})
		return
	}

	count := c.SyncSvc.PushDocumentsToSAP(ctx.Request.Context())
	ctx.JSON(http.StatusOK, TriggerPushResponse{
		Message:     "Push trigger executed",
		PushedCount: count,
	})
}

// TriggerPull godoc
// @Summary      Trigger Pull Worker dari SQL Server Maximo
// @Description  Memicu eksekusi manual worker pull untuk menarik data antrean mxout_inter_trans dari SQL Server Maximo ke PostgreSQL staging_documents
// @Tags         Ingestion
// @Produce      json
// @Param        limit    query     int   false  "Jumlah data yang mau ditarik (Default: 1 untuk testing)"
// @Success      200      {object}  TriggerPullResponse
// @Failure      503      {object}  ErrorResponse
// @Router       /trigger/pull [post]
func (c *WebController) TriggerPull(ctx *gin.Context) {
	if c.SyncSvc == nil {
		ctx.JSON(http.StatusServiceUnavailable, ErrorResponse{Error: "Sync service is not initialized"})
		return
	}

	limit := 1
	if lStr := ctx.Query("limit"); lStr != "" {
		if l, err := strconv.Atoi(lStr); err == nil && l > 0 {
			limit = l
		}
	}

	pulled, transformed := c.SyncSvc.PullDataFromSQLServer(ctx.Request.Context(), limit)
	ctx.JSON(http.StatusOK, TriggerPullResponse{
		Message:     "Pull trigger executed successfully",
		PulledCount: pulled,
		Transformed: transformed,
		Limit:       limit,
	})
}

// GetDocuments godoc
// @Summary      Lihat Daftar Core Documents
// @Description  Mengambil hingga 50 data dokumen antrean & status integrasi SAP dari tabel documents
// @Tags         Monitoring
// @Produce      json
// @Success      200  {object}  GetDocumentsResponse
// @Failure      500  {object}  ErrorResponse
// @Failure      503  {object}  ErrorResponse
// @Router       /documents [get]
func (c *WebController) GetDocuments(ctx *gin.Context) {
	if c.PGRepo == nil || c.PGRepo.Pool == nil {
		ctx.JSON(http.StatusOK, GetDocumentsResponse{
			Total: 0,
			Data:  []DocumentItemDTO{},
		})
		return
	}

	rows, err := c.PGRepo.Pool.Query(ctx.Request.Context(), `
		SELECT id, doc_type, document_no, group_id, status, retry_count, sap_document_no, raw_payload, sap_payload, created_at
		FROM documents
		ORDER BY created_at DESC
		LIMIT 50
	`)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	defer rows.Close()

	var list []DocumentItemDTO
	for rows.Next() {
		var id uuid.UUID
		var docType, docNo, grpID, status string
		var retryCount int
		var sapDocNo *string
		var rawPayload, sapPayload []byte
		var createdAt interface{}

		if err := rows.Scan(&id, &docType, &docNo, &grpID, &status, &retryCount, &sapDocNo, &rawPayload, &sapPayload, &createdAt); err == nil {
			var rawMap, sapMap map[string]interface{}
			_ = json.Unmarshal(rawPayload, &rawMap)
			_ = json.Unmarshal(sapPayload, &sapMap)

			list = append(list, DocumentItemDTO{
				ID:            id,
				DocType:       docType,
				DocumentNo:    docNo,
				GroupID:       grpID,
				Status:        status,
				RetryCount:    retryCount,
				SAPDocumentNo: sapDocNo,
				RawPayload:    rawMap,
				SAPPayload:    sapMap,
				CreatedAt:     createdAt,
			})
		}
	}

	if list == nil {
		list = []DocumentItemDTO{}
	}

	ctx.JSON(http.StatusOK, GetDocumentsResponse{Total: len(list), Data: list})
}

// GetIntegrationLogs godoc
// @Summary      Get integration audit trail logs
// @Description  Mendapatkan riwayat audit trail pertukaran data Maximo <-> Middleware <-> SAP
// @Tags         Logs
// @Produce      json
// @Param        document_type query string false "Filter Document Type"
// @Param        document_no   query string false "Filter Document No"
// @Success      200  {object}  map[string]interface{}
// @Router       /integration-logs [get]
func (c *WebController) GetIntegrationLogs(ctx *gin.Context) {
	docType := ctx.Query("document_type")
	if docType == "" {
		docType = ctx.Query("doc_type")
	}
	docNo := ctx.Query("document_no")
	if docNo == "" {
		docNo = ctx.Query("doc_no")
	}

	if c.PGRepo == nil || c.PGRepo.Pool == nil {
		ctx.JSON(http.StatusOK, gin.H{
			"status": "success",
			"data":   []interface{}{},
		})
		return
	}

	query := `
		SELECT id, trace_id, source_system, target_system, document_type, document_no, status, error_message, metadata, created_at
		FROM integration_logs
		WHERE 1=1
	`
	args := []interface{}{}
	argIdx := 1
	if docType != "" {
		query += fmt.Sprintf(" AND document_type = $%d", argIdx)
		args = append(args, docType)
		argIdx++
	}
	if docNo != "" {
		query += fmt.Sprintf(" AND document_no = $%d", argIdx)
		args = append(args, docNo)
		argIdx++
	}
	query += " ORDER BY created_at ASC"

	rows, err := c.PGRepo.Pool.Query(ctx.Request.Context(), query, args...)
	if err != nil {
		// Jika tabel belum di-create atau error, buatkan log sintetis dari documents
		logs := c.getSyntheticLogsFromDocuments(ctx.Request.Context(), docType, docNo)
		ctx.JSON(http.StatusOK, gin.H{
			"status": "success",
			"data":   logs,
		})
		return
	}
	defer rows.Close()

	var logs []map[string]interface{}
	for rows.Next() {
		var id int64
		var traceID, src, tgt, dType, dNo, status string
		var errMsg, meta *string
		var createdAt time.Time
		if err := rows.Scan(&id, &traceID, &src, &tgt, &dType, &dNo, &status, &errMsg, &meta, &createdAt); err == nil {
			errStr := ""
			if errMsg != nil {
				errStr = *errMsg
			}
			metaStr := ""
			if meta != nil {
				metaStr = *meta
			}

			// Pastikan jika log tujuan SAP GROW dan belum memiliki [Target API: ...], kita tambahkan URL API targetnya
			if tgt == "SAP GROW" && !strings.Contains(metaStr, "[Target API:") {
				targetService := dType
				if targetService == "" && docType != "" {
					targetService = docType
				}
				if targetService == "" {
					targetService = "PO"
				}
				targetURL := "https://my445160-api.s4hana.cloud.sap/sap/opu/odata4/sap/api_purchaseorder_2/srvd_a2x/sap/purchaseorder/0001/PurchaseOrder"
				if mapping, _ := c.PGRepo.GetTableAPIMapping(ctx.Request.Context(), targetService); mapping != nil && mapping.TargetServiceCode != "" {
					targetService = mapping.TargetServiceCode
				}
				if ep, _ := c.PGRepo.GetSAPEndpointByCode(ctx.Request.Context(), targetService); ep != nil {
					targetURL = fmt.Sprintf("%s%s", strings.TrimRight(ep.BaseURL, "/"), ep.EndpointPath)
				}
				if strings.Contains(metaStr, "Successfully posted to SAP Grow") {
					metaStr = strings.Replace(metaStr, "Successfully posted to SAP Grow", fmt.Sprintf("Successfully posted to SAP Grow via %s [Target API: %s]", targetService, targetURL), 1)
				} else if metaStr != "" {
					metaStr = fmt.Sprintf("%s [Target API: %s]", metaStr, targetURL)
				}
			}

			var sapRespStr string
			if tgt == "SAP GROW" && dNo != "" {
				_ = c.PGRepo.Pool.QueryRow(ctx.Request.Context(), "SELECT COALESCE(sap_response::text, '') FROM documents WHERE document_no = $1", dNo).Scan(&sapRespStr)
			}

			logs = append(logs, map[string]interface{}{
				"id":               id,
				"trace_id":         traceID,
				"source_system":    src,
				"target_system":    tgt,
				"document_type":    dType,
				"document_no":      dNo,
				"status":           status,
				"error_message":    errStr,
				"metadata":         metaStr,
				"response_payload": sapRespStr,
				"created_at":       createdAt,
			})
		}
	}

	if len(logs) == 0 && docNo != "" {
		logs = c.getSyntheticLogsFromDocuments(ctx.Request.Context(), docType, docNo)
	}

	ctx.JSON(http.StatusOK, gin.H{
		"status": "success",
		"data":   logs,
	})
}

func (c *WebController) getSyntheticLogsFromDocuments(ctx context.Context, docType, docNo string) []map[string]interface{} {
	var logs []map[string]interface{}
	if c.PGRepo == nil || c.PGRepo.Pool == nil || docNo == "" {
		return logs
	}

	var dType, dNo, groupID, status string
	var sapDocNo, errMsg, sapResp *string
	var createdAt, updatedAt time.Time
	var query string
	var args []interface{}
	if docType != "" {
		query = `
			SELECT doc_type, document_no, group_id, status, sap_document_no, error_message, sap_response::text, created_at, updated_at
			FROM documents
			WHERE document_no = $1 AND doc_type = $2
			LIMIT 1
		`
		args = []interface{}{docNo, docType}
	} else {
		query = `
			SELECT doc_type, document_no, group_id, status, sap_document_no, error_message, sap_response::text, created_at, updated_at
			FROM documents
			WHERE document_no = $1
			LIMIT 1
		`
		args = []interface{}{docNo}
	}
	err := c.PGRepo.Pool.QueryRow(ctx, query, args...).Scan(&dType, &dNo, &groupID, &status, &sapDocNo, &errMsg, &sapResp, &createdAt, &updatedAt)
	if err != nil {
		return logs
	}

	// 1. Log Received Maximo
	logs = append(logs, map[string]interface{}{
		"source_system": "MAXIMO",
		"target_system": "MIDDLEWARE",
		"document_type": dType,
		"document_no":   dNo,
		"status":        "RECEIVED",
		"metadata":      fmt.Sprintf("Payload received from Maximo ERP (Group: %s)", groupID),
		"created_at":    createdAt,
	})

	// 2. Log Sent / Processing to SAP
	sapStatus := "SENT"
	metaMsg := "Waiting for confirmation from SAP"

	// Resolusi Target API URL secara dinamis dari table mapping & sap endpoints
	targetService := dType
	targetURL := "https://my445160-api.s4hana.cloud.sap/sap/opu/odata4/sap/api_purchaseorder_2/srvd_a2x/sap/purchaseorder/0001/PurchaseOrder"
	if mapping, _ := c.PGRepo.GetTableAPIMapping(ctx, dType); mapping != nil && mapping.TargetServiceCode != "" {
		targetService = mapping.TargetServiceCode
	}
	if ep, _ := c.PGRepo.GetSAPEndpointByCode(ctx, targetService); ep != nil {
		targetURL = fmt.Sprintf("%s%s", strings.TrimRight(ep.BaseURL, "/"), ep.EndpointPath)
	}

	switch status {
	case "success":
		sapStatus = "SUCCESS"
		metaMsg = fmt.Sprintf("Successfully posted to SAP Grow via %s [Target API: %s]", targetService, targetURL)
		if sapDocNo != nil && *sapDocNo != "" {
			metaMsg = fmt.Sprintf("Successfully posted to SAP Grow via %s [Target API: %s] (SAP Doc: %s)", targetService, targetURL, *sapDocNo)
		}
	case "error", "error_data", "error_conn":
		sapStatus = "FAILED"
		if errMsg != nil {
			metaMsg = fmt.Sprintf("[Target API: %s] %s", targetURL, *errMsg)
		} else {
			metaMsg = fmt.Sprintf("Failed processing document to SAP Grow [Target API: %s]", targetURL)
		}
	case "pending", "requeued":
		sapStatus = "PENDING"
		metaMsg = fmt.Sprintf("Queued in middleware buffer [Target API: %s]", targetURL)
	}

	upTime := updatedAt
	if upTime.Equal(createdAt) {
		upTime = createdAt.Add(1 * time.Second)
	}

	logs = append(logs, map[string]interface{}{
		"source_system":    "MIDDLEWARE",
		"target_system":    "SAP GROW",
		"document_type":    dType,
		"document_no":      dNo,
		"status":           sapStatus,
		"error_message":    errMsg,
		"metadata":         metaMsg,
		"response_payload": sapResp,
		"created_at":       upTime,
	})

	return logs
}

// HealthCheck godoc
// @Summary      Health Check Service
// @Description  Memeriksa status service BPI Maximo-SAP Grow Middleware
// @Tags         System
// @Produce      json
// @Success      200  {object}  HealthResponse
// @Router       /health [get]
func (c *WebController) HealthCheck(ctx *gin.Context) {
	ctx.JSON(http.StatusOK, HealthResponse{
		Service: "BPI Maximo-SAP Grow Middleware",
		Status:  "running",
	})
}

// ==========================================
// SAP ENDPOINTS & MULTI-CREDENTIAL HANDLERS
// ==========================================

// GetSAPEndpoints godoc
// @Summary      Mengambil Daftar Semua Endpoint SAP Terdaftar
// @Description  Menampilkan semua konfigurasi endpoint, skenario komunikasi, dan kredensial SAP S/4HANA Cloud
// @Tags         SAP Endpoints
// @Produce      json
// @Success      200  {object}  map[string]interface{}
// @Failure      500  {object}  ErrorResponse
// @Router       /sap/endpoints [get]
func (c *WebController) GetSAPEndpoints(ctx *gin.Context) {
	if c.PGRepo == nil {
		ctx.JSON(http.StatusServiceUnavailable, ErrorResponse{Error: "Database repository is not available"})
		return
	}

	endpoints, err := c.PGRepo.GetSAPEndpoints(ctx.Request.Context())
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"status": "success",
		"data":   endpoints,
	})
}

// TestSAPEndpoint godoc
// @Summary      Uji Koneksi & Handshake CSRF Token ke SAP Grow
// @Description  Melakukan uji coba konektivitas HTTP dan handshake pengambilan x-csrf-token dari server SAP S/4HANA Cloud
// @Tags         SAP Endpoints
// @Accept       json
// @Produce      json
// @Param        service_code  query     string  false  "Kode Layanan SAP (PO, BP, PRODUCT, dll)"
// @Success      200           {object}  map[string]interface{}
// @Failure      404           {object}  ErrorResponse
// @Failure      502           {object}  map[string]interface{}
// @Router       /sap/endpoints/test [post]
func (c *WebController) TestSAPEndpoint(ctx *gin.Context) {
	if c.PGRepo == nil || c.SyncSvc == nil || c.SyncSvc.SAPClient == nil {
		ctx.JSON(http.StatusServiceUnavailable, ErrorResponse{Error: "SAP client service is not initialized"})
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

	ep, err := c.PGRepo.GetSAPEndpointByCode(ctx.Request.Context(), req.ServiceCode)
	if err != nil || ep == nil {
		ctx.JSON(http.StatusNotFound, ErrorResponse{Error: fmt.Sprintf("Service code '%s' tidak ditemukan", req.ServiceCode)})
		return
	}

	csrfToken, latency, connErr := c.SyncSvc.SAPClient.TestConnection(ep)
	status := "SUCCESS"
	respMsg := fmt.Sprintf("Handshake Berhasil! CSRF Token didapatkan (Latency: %dms)", latency)
	if connErr != nil {
		status = "FAILED"
		respMsg = fmt.Sprintf("Handshake Gagal: %v (Latency: %dms)", connErr, latency)
	}

	_ = c.PGRepo.UpdateSAPEndpointStatus(ctx.Request.Context(), ep.ServiceCode, status, respMsg)

	if connErr != nil {
		ctx.JSON(http.StatusBadGateway, gin.H{
			"status":       "error",
			"service_code": ep.ServiceCode,
			"scenario_id":  ep.ScenarioID,
			"username":     ep.Username,
			"latency_ms":   latency,
			"message":      respMsg,
		})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"status":       "success",
		"service_code": ep.ServiceCode,
		"scenario_id":  ep.ScenarioID,
		"username":     ep.Username,
		"csrf_token":   csrfToken,
		"latency_ms":   latency,
		"message":      respMsg,
	})
}

// SaveSAPEndpoint godoc
// @Summary      Menyimpan atau Memperbarui Konfigurasi Endpoint SAP
// @Description  Menyimpan atau mengupdate URL, OData Version, dan kredensial Communication User untuk integrasi SAP
// @Tags         SAP Endpoints
// @Accept       json
// @Produce      json
// @Param        endpoint  body      model.SAPEndpoint  true  "Data Konfigurasi Endpoint SAP"
// @Success      200       {object}  map[string]interface{}
// @Failure      400       {object}  ErrorResponse
// @Failure      500       {object}  ErrorResponse
// @Router       /sap/endpoints [post]
func (c *WebController) SaveSAPEndpoint(ctx *gin.Context) {
	if c.PGRepo == nil {
		ctx.JSON(http.StatusServiceUnavailable, ErrorResponse{Error: "Database repository is not available"})
		return
	}

	var ep model.SAPEndpoint
	if err := ctx.ShouldBindJSON(&ep); err != nil {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{Error: "Format JSON tidak valid: " + err.Error()})
		return
	}

	if ep.ServiceCode == "" || ep.Username == "" {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{Error: "ServiceCode dan Username wajib diisi"})
		return
	}

	if err := c.PGRepo.SaveSAPEndpoint(ctx.Request.Context(), &ep); err != nil {
		ctx.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": fmt.Sprintf("Endpoint %s (%s) berhasil disimpan", ep.ServiceCode, ep.Username),
	})
}

// ExecuteSAPTest godoc
// @Summary      Eksekusi Payload Test ke SAP Grow (Sandbox Tester)
// @Description  Mengirimkan payload HTTP POST / PATCH / GET langsung ke server SAP Grow S/4HANA Cloud dengan auto CSRF handshake & Basic Auth
// @Tags         SAP Sandbox
// @Accept       json
// @Produce      json
// @Success      200  {object}  map[string]interface{}
// @Failure      400  {object}  ErrorResponse
// @Failure      404  {object}  ErrorResponse
// @Router       /sap/execute-test [post]
func (c *WebController) ExecuteSAPTest(ctx *gin.Context) {
	if c.PGRepo == nil || c.SyncSvc == nil || c.SyncSvc.SAPClient == nil {
		ctx.JSON(http.StatusServiceUnavailable, ErrorResponse{Error: "SAP client service is not initialized"})
		return
	}

	var req struct {
		ServiceCode string      `json:"service_code"`
		Method      string      `json:"method"`
		CustomPath  string      `json:"custom_path"`
		Payload     interface{} `json:"payload"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{Error: "Format request tidak valid: " + err.Error()})
		return
	}

	if req.ServiceCode == "" {
		req.ServiceCode = "PO"
	}
	if req.Method == "" {
		req.Method = "POST"
	}

	ep, err := c.PGRepo.GetSAPEndpointByCode(ctx.Request.Context(), req.ServiceCode)
	if err != nil || ep == nil {
		ctx.JSON(http.StatusNotFound, ErrorResponse{Error: fmt.Sprintf("Service '%s' tidak ditemukan di database", req.ServiceCode)})
		return
	}

	if custom := strings.TrimSpace(req.CustomPath); custom != "" {
		epCopy := *ep
		if strings.HasPrefix(custom, "/") || strings.HasPrefix(custom, "(") || strings.HasPrefix(custom, "?") {
			epCopy.EndpointPath = epCopy.EndpointPath + custom
		} else {
			epCopy.EndpointPath = epCopy.EndpointPath + "/" + custom
		}
		ep = &epCopy
	}

	var payloadBytes []byte
	if req.Payload != nil {
		payloadBytes, _ = json.Marshal(req.Payload)
	}

	statusCode, sapDocNo, respBody, sapErr := c.SyncSvc.SAPClient.ExecuteOData(ep, req.Method, payloadBytes)

	// Catat ke integration logs untuk audit trail
	logStatus := "SUCCESS"
	errMsg := ""
	if sapErr != nil {
		logStatus = "FAILED"
		errMsg = sapErr.Error()
	}
	_ = c.PGRepo.InsertIntegrationLog(
		ctx.Request.Context(),
		fmt.Sprintf("SANDBOX-%s-%d", ep.ServiceCode, time.Now().Unix()),
		"SANDBOX_PANEL",
		"SAP GROW",
		ep.ServiceCode,
		sapDocNo,
		logStatus,
		errMsg,
		fmt.Sprintf("Sandbox %s test execution via %s (%s)", req.Method, ep.ScenarioID, ep.Username),
	)

	ctx.JSON(http.StatusOK, gin.H{
		"status":         "executed",
		"http_code":      statusCode,
		"service_code":   ep.ServiceCode,
		"scenario_id":    ep.ScenarioID,
		"username":       ep.Username,
		"sap_document_no": sapDocNo,
		"response":       respBody,
		"error":          errMsg,
	})
}

// SchedulerStatusResponse merepresentasikan respons status scheduler
type SchedulerStatusResponse struct {
	Status          string `json:"status" example:"success"`
	AutoPullEnabled bool   `json:"auto_pull_enabled" example:"true"`
	EnvSetting      string `json:"env_setting" example:"true"`
	BatchLimit      int    `json:"batch_limit" example:"1"`
	Message         string `json:"message,omitempty" example:"Auto-pull status di database berhasil diatur"`
}

// GetSchedulerStatus godoc
// @Summary      Cek Status Pengaturan Auto-Pull
// @Description  Memeriksa status aktif/nonaktif fitur penarikan data otomatis dari Maximo (berdasarkan .env dan DB)
// @Tags         Scheduler
// @Produce      json
// @Success      200  {object}  SchedulerStatusResponse
// @Router       /scheduler/status [get]
func (c *WebController) GetSchedulerStatus(ctx *gin.Context) {
	if c.PGRepo == nil {
		ctx.JSON(http.StatusServiceUnavailable, ErrorResponse{Error: "Database repository is not available"})
		return
	}

	isActive, batchLimit := c.PGRepo.IsAutoPullActive(ctx.Request.Context())
	envPull := os.Getenv("AUTO_PULL_ENABLED")
	if envPull == "" {
		envPull = os.Getenv("ENABLE_AUTO_PULL")
	}

	ctx.JSON(http.StatusOK, SchedulerStatusResponse{
		Status:          "success",
		AutoPullEnabled: isActive,
		EnvSetting:      envPull,
		BatchLimit:      batchLimit,
	})
}

// ToggleAutoPull godoc
// @Summary      Ubah Status On/Off Auto-Pull di Database
// @Description  Mengubah status aktif/nonaktif auto get data from Maximo pada database runtime
// @Tags         Scheduler
// @Accept       json
// @Produce      json
// @Param        active  query     bool  false  "Status aktif (true/false)"
// @Success      200     {object}  SchedulerStatusResponse
// @Router       /scheduler/toggle [post]
func (c *WebController) ToggleAutoPull(ctx *gin.Context) {
	if c.PGRepo == nil {
		ctx.JSON(http.StatusServiceUnavailable, ErrorResponse{Error: "Database repository is not available"})
		return
	}

	var targetActive bool = true
	hasParam := false

	var body struct {
		Active *bool `json:"active"`
	}
	if err := ctx.ShouldBindJSON(&body); err == nil && body.Active != nil {
		targetActive = *body.Active
		hasParam = true
	}

	if !hasParam {
		q := ctx.DefaultQuery("active", "")
		if q != "" {
			targetActive = (q == "true" || q == "1" || q == "on")
			hasParam = true
		}
	}

	if !hasParam {
		currentActive, _ := c.PGRepo.IsAutoPullActive(ctx.Request.Context())
		targetActive = !currentActive
	}

	if err := c.PGRepo.SetAutoPullActive(ctx.Request.Context(), targetActive); err != nil {
		ctx.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	effectiveActive, limit := c.PGRepo.IsAutoPullActive(ctx.Request.Context())
	envPull := os.Getenv("AUTO_PULL_ENABLED")
	if envPull == "" {
		envPull = os.Getenv("ENABLE_AUTO_PULL")
	}

	ctx.JSON(http.StatusOK, SchedulerStatusResponse{
		Status:          "success",
		AutoPullEnabled: effectiveActive,
		EnvSetting:      envPull,
		BatchLimit:      limit,
		Message:         fmt.Sprintf("Auto-pull status di database berhasil diatur ke: %v", targetActive),
	})
}

// ReceiveExchangeRate godoc
// @Summary      Menerima Update Kurs Pajak / Exchange Rate dari Maximo
// @Description  Menerima payload kurs.updated dari Maximo/Kemenkeu, menyimpan ke exchange_rates, dan mencatat audit trail di integration_logs
// @Tags         Exchange Rate
// @Accept       json
// @Produce      json
// @Param        payload body      model.MaximoExchangeRatePayload  true  "Payload Kurs Pajak dari Maximo / Kemenkeu"
// @Success      200     {object}  map[string]interface{}
// @Failure      400     {object}  ErrorResponse
// @Failure      500     {object}  ErrorResponse
// @Router       /exchange-rate [post]
func (c *WebController) ReceiveExchangeRate(ctx *gin.Context) {
	if c.PGRepo == nil || c.PGRepo.Pool == nil {
		ctx.JSON(http.StatusServiceUnavailable, ErrorResponse{Error: "PostgreSQL database pool is not connected"})
		return
	}

	rawBytes, err := ctx.GetRawData()
	if err != nil {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{Error: "Failed reading request payload: " + err.Error()})
		return
	}

	var payload model.MaximoExchangeRatePayload
	if err := json.Unmarshal(rawBytes, &payload); err != nil {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid JSON format: " + err.Error()})
		return
	}

	if len(payload.Rates) == 0 {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{Error: "Rates array cannot be empty"})
		return
	}

	saved, err := c.PGRepo.SaveExchangeRates(ctx.Request.Context(), &payload, rawBytes)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"status":      "success",
		"message":     fmt.Sprintf("Successfully processed %d exchange rates for KMK %s", saved, payload.Period.KMKNumber),
		"event":       payload.Event,
		"kmk_number":  payload.Period.KMKNumber,
		"valid_from":  payload.Period.ValidFrom,
		"valid_to":    payload.Period.ValidTo,
		"rates_saved": saved,
	})
}

// GetExchangeRates godoc
// @Summary      Mengambil Daftar History Exchange Rate
// @Tags         Exchange Rate
// @Produce      json
// @Router       /exchange-rates [get]
func (c *WebController) GetExchangeRates(ctx *gin.Context) {
	if c.PGRepo == nil || c.PGRepo.Pool == nil {
		ctx.JSON(http.StatusServiceUnavailable, ErrorResponse{Error: "Database repository is not available"})
		return
	}

	limit := 100
	if l := ctx.Query("limit"); l != "" {
		if val, err := strconv.Atoi(l); err == nil && val > 0 {
			limit = val
		}
	}

	rates, err := c.PGRepo.GetExchangeRates(ctx.Request.Context(), limit)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"status": "success",
		"total":  len(rates),
		"data":   rates,
	})
}

// GetUOMMappings godoc
// @Summary      Mengambil Daftar Master Data Pemetaan UOM Maximo ke SAP
// @Tags         UOM Mapping
// @Produce      json
// @Router       /uom-mappings [get]
func (c *WebController) GetUOMMappings(ctx *gin.Context) {
	if c.PGRepo == nil || c.PGRepo.Pool == nil {
		ctx.JSON(http.StatusServiceUnavailable, ErrorResponse{Error: "Database repository is not available"})
		return
	}

	list, err := c.PGRepo.GetUOMMappings(ctx.Request.Context())
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"status": "success",
		"total":  len(list),
		"data":   list,
	})
}

// GetSAPMaterialDocumentDetail godoc
// @Summary      Mengecek Live Detail Material Document / Goods Receipt dari SAP Grow
// @Tags         SAP Inspection
// @Produce      json
// @Param        doc_no   path      string  true  "Material Document No (e.g. 202500000001 or 5000000000)"
// @Param        year     query     string  false "Material Document Year (default: 2025)"
// @Router       /sap/material-document/{doc_no} [get]
func (c *WebController) GetSAPMaterialDocumentDetail(ctx *gin.Context) {
	docNo := ctx.Param("doc_no")
	if docNo == "" {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{Error: "Parameter doc_no is required"})
		return
	}

	year := ctx.DefaultQuery("year", "2025")

	// Ambil endpoint konfigurasi untuk Material Document
	ep, err := c.PGRepo.GetSAPEndpointByCode(ctx.Request.Context(), "MATERIAL_DOC_DETAIL")
	if err != nil || ep == nil {
		ep, err = c.PGRepo.GetSAPEndpointByCode(ctx.Request.Context(), "GR_WITH_PO")
	}

	if ep == nil {
		ep = &model.SAPEndpoint{
			ServiceCode:  "MATERIAL_DOC_DETAIL",
			ServiceName:  "Material Document Detail OData v2",
			ScenarioID:   "SAP_COM_0108",
			ODataVersion: "v2",
			BaseURL:      "https://my445160-api.s4hana.cloud.sap",
			EndpointPath: "/sap/opu/odata/sap/API_MATERIAL_DOCUMENT_SRV/A_MaterialDocumentHeader",
			Username:     "Y_BPI_CU_SAP_COM_0108",
			Password:     "PowerofOne@123456789",
		}
	}

	customEP := *ep
	if strings.Contains(docNo, "(") {
		customEP.EndpointPath = fmt.Sprintf("/sap/opu/odata/sap/API_MATERIAL_DOCUMENT_SRV/A_MaterialDocumentHeader%s", docNo)
	} else if len(docNo) == 12 && (strings.HasPrefix(docNo, "2024") || strings.HasPrefix(docNo, "2025") || strings.HasPrefix(docNo, "2026")) {
		// Contoh nomor 12 digit: 202500000001 (Tahun 2025, Dokumen 00000001)
		customEP.EndpointPath = fmt.Sprintf("/sap/opu/odata/sap/API_MATERIAL_DOCUMENT_SRV/A_MaterialDocumentHeader(MaterialDocumentYear='%s',MaterialDocument='%s')?$expand=to_MaterialDocumentItem", docNo[:4], docNo[4:])
	} else {
		// Standar composite key SAP OData v2: MaterialDocumentYear & MaterialDocument
		customEP.EndpointPath = fmt.Sprintf("/sap/opu/odata/sap/API_MATERIAL_DOCUMENT_SRV/A_MaterialDocumentHeader(MaterialDocumentYear='%s',MaterialDocument='%s')?$expand=to_MaterialDocumentItem", year, docNo)
	}

	statusCode, sapDocNo, respBody, sapErr := c.SyncSvc.SAPClient.ExecuteOData(&customEP, "GET", nil)

	errMsg := ""
	if sapErr != nil {
		errMsg = sapErr.Error()
	}

	var parsedBody interface{}
	if err := json.Unmarshal([]byte(respBody), &parsedBody); err == nil {
		ctx.JSON(http.StatusOK, gin.H{
			"status":          "success",
			"http_code":       statusCode,
			"material_doc_no": docNo,
			"year":            year,
			"endpoint_path":   customEP.EndpointPath,
			"sap_document_no": sapDocNo,
			"data":            parsedBody,
			"error":           errMsg,
		})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"status":          "success",
		"http_code":       statusCode,
		"material_doc_no": docNo,
		"year":            year,
		"endpoint_path":   customEP.EndpointPath,
		"sap_document_no": sapDocNo,
		"raw_response":    respBody,
		"error":           errMsg,
	})
}

