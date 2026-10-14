package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
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
			logs = append(logs, map[string]interface{}{
				"id":            id,
				"trace_id":      traceID,
				"source_system": src,
				"target_system": tgt,
				"document_type": dType,
				"document_no":   dNo,
				"status":        status,
				"error_message": errStr,
				"metadata":      metaStr,
				"created_at":    createdAt,
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
	var sapDocNo, errMsg *string
	var createdAt, updatedAt time.Time
	err := c.PGRepo.Pool.QueryRow(ctx, `
		SELECT doc_type, document_no, group_id, status, sap_document_no, error_message, created_at, updated_at
		FROM documents
		WHERE document_no = $1
		LIMIT 1
	`, docNo).Scan(&dType, &dNo, &groupID, &status, &sapDocNo, &errMsg, &createdAt, &updatedAt)
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
	if status == "success" {
		sapStatus = "SUCCESS"
		metaMsg = "Document successfully posted to SAP Grow"
		if sapDocNo != nil && *sapDocNo != "" {
			metaMsg = fmt.Sprintf("Document successfully posted to SAP Grow (SAP Doc: %s)", *sapDocNo)
		}
	} else if status == "error" || status == "error_data" || status == "error_conn" {
		sapStatus = "FAILED"
		if errMsg != nil {
			metaMsg = *errMsg
		}
	} else if status == "pending" || status == "requeued" {
		sapStatus = "PENDING"
		metaMsg = "Queued in middleware buffer"
	}

	upTime := updatedAt
	if upTime.Equal(createdAt) {
		upTime = createdAt.Add(1 * time.Second)
	}

	logs = append(logs, map[string]interface{}{
		"source_system": "MIDDLEWARE",
		"target_system": "SAP GROW",
		"document_type": dType,
		"document_no":   dNo,
		"status":        sapStatus,
		"error_message": errMsg,
		"metadata":      metaMsg,
		"created_at":    upTime,
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
