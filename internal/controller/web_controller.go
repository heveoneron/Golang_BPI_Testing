package controller

import (
	"encoding/json"
	"net/http"

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

// IngestManualPO: Simulasi webhook atau injection data langsung untuk testing lokal
func (c *WebController) IngestManualPO(ctx *gin.Context) {
	var maxPO model.MaximoPOHeader
	if err := ctx.ShouldBindJSON(&maxPO); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Langsung trigger transform
	transformed := c.SyncSvc.TransformStagingToCore(ctx.Request.Context())

	ctx.JSON(http.StatusOK, gin.H{
		"message":     "PO successfully ingested into staging_documents and transformed to core documents",
		"document_no": maxPO.PONum,
		"transformed": transformed,
	})
}

// TriggerPush: Force trigger push worker ke SAP Grow
func (c *WebController) TriggerPush(ctx *gin.Context) {
	count := c.SyncSvc.PushDocumentsToSAP(ctx.Request.Context())
	ctx.JSON(http.StatusOK, gin.H{
		"message":      "Push trigger executed",
		"pushed_count": count,
	})
}

// GetDocuments: Lihat isi tabel documents dan status SAP
func (c *WebController) GetDocuments(ctx *gin.Context) {
	rows, err := c.PGRepo.Pool.Query(ctx.Request.Context(), `
		SELECT id, doc_type, document_no, group_id, status, retry_count, sap_document_no, raw_payload, sap_payload, created_at
		FROM documents
		ORDER BY created_at DESC
		LIMIT 50
	`)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var list []map[string]interface{}
	for rows.Next() {
		var id uuid.UUID
		var docType, docNo, grpID, status string
		var retryCount int
		var sapDocNo *string
		var rawPayload, sapPayload []byte
		var createdAt interface{}

		if err := rows.Scan(&id, &docType, &docNo, &grpID, &status, &retryCount, &sapDocNo, &rawPayload, &sapPayload, &createdAt); err == nil {
			var rawMap, sapMap interface{}
			_ = json.Unmarshal(rawPayload, &rawMap)
			_ = json.Unmarshal(sapPayload, &sapMap)

			list = append(list, map[string]interface{}{
				"id":              id,
				"doc_type":        docType,
				"document_no":     docNo,
				"group_id":        grpID,
				"status":          status,
				"retry_count":     retryCount,
				"sap_document_no": sapDocNo,
				"raw_payload":     rawMap,
				"sap_payload":     sapMap,
				"created_at":      createdAt,
			})
		}
	}

	ctx.JSON(http.StatusOK, gin.H{"total": len(list), "data": list})
}

// HealthCheck
func (c *WebController) HealthCheck(ctx *gin.Context) {
	ctx.JSON(http.StatusOK, gin.H{
		"service": "BPI Maximo-SAP Grow Middleware",
		"status":  "running",
	})
}
