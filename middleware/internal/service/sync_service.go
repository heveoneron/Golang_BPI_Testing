package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"golang-bpi-testing/internal/client"
	"golang-bpi-testing/internal/model"
	"golang-bpi-testing/internal/repository"

	"github.com/google/uuid"
)

type SyncService struct {
	SQLRepo   *repository.SQLServerRepository
	PGRepo    *repository.PostgresRepository
	SAPClient *client.SAPClient
}

func NewSyncService(sqlRepo *repository.SQLServerRepository, pgRepo *repository.PostgresRepository, sapClient *client.SAPClient) *SyncService {
	return &SyncService{
		SQLRepo:   sqlRepo,
		PGRepo:    pgRepo,
		SAPClient: sapClient,
	}
}

// 1. EXTRACT & LOAD DARI SQL SERVER KE STAGING_DOCUMENTS (Tier 1)
func (s *SyncService) PullDataFromSQLServer(ctx context.Context, limit int) (int, int) {
	if limit <= 0 {
		limit = 1
	}

	records, err := s.SQLRepo.ExtractData(ctx, limit)
	if err != nil {
		log.Printf("[WARN] Pull SQL Server error: %v", err)
		return 0, 0
	}

	insertedCount := 0
	for _, rec := range records {
		doc := model.StagingDocument{
			ID:         uuid.New(),
			DocType:    rec.DocType,
			DocumentNo: rec.DocumentNo,
			FlowType:   "forward",
			RawPayload: rec.PayloadJSON,
			Status:     "PENDING",
		}
		if err := s.PGRepo.UpsertStagingDocument(ctx, doc); err == nil {
			insertedCount++
			if rec.TransID != "" {
				_ = s.SQLRepo.MarkProcessed(ctx, rec.TransID)
			}
		} else {
			log.Printf("[WARN] Upsert staging gagal untuk %s %s: %v", rec.DocType, rec.DocumentNo, err)
		}
	}

	transformedCount := 0
	if insertedCount > 0 {
		transformedCount = s.TransformStagingToCore(ctx)
	}

	return insertedCount, transformedCount
}

// 2. TRANSFORM DARI STAGING_DOCUMENTS KE DOCUMENTS (Tier 2 - ShoeTown Pattern)
func (s *SyncService) TransformStagingToCore(ctx context.Context) int {
	stagedDocs, err := s.PGRepo.GetPendingStagingDocuments(ctx, 50)
	if err != nil || len(stagedDocs) == 0 {
		return 0
	}

	transformedCount := 0
	for _, stage := range stagedDocs {
		var sapPayloadBytes []byte
		var transformErr error
		docNo := stage.DocumentNo

		switch stage.DocType {
		case "PO", "axmxpo":
			sapPayloadBytes, transformErr = s.transformPO(ctx, stage.RawPayload)
		default:
			// Generic pass-through
			sapPayloadBytes = stage.RawPayload
		}

		if transformErr != nil {
			log.Printf("[ERROR] Transform gagal untuk %s %s: %v", stage.DocType, stage.DocumentNo, transformErr)
			_ = s.PGRepo.MarkStagingProcessed(ctx, stage.ID, "FAILED", transformErr.Error())
			continue
		}

		coreDoc := model.Document{
			ID:             uuid.New(),
			DocType:        stage.DocType,
			DocumentNo:     docNo,
			FlowType:       stage.FlowType,
			GroupID:        fmt.Sprintf("GRP-%s-%s", stage.DocType, docNo),
			RawPayload:     stage.RawPayload,
			SAPPayload:     json.RawMessage(sapPayloadBytes),
			Status:         "pending",
			SyncBackStatus: "pending",
		}

		if err := s.PGRepo.UpsertCoreDocument(ctx, coreDoc); err == nil {
			_ = s.PGRepo.MarkStagingProcessed(ctx, stage.ID, "PROCESSED", "-")
			transformedCount++
		}
	}
	return transformedCount
}

// Transformasi khusus PO Maximo -> SAP Grow OData + Budget Check
func (s *SyncService) transformPO(ctx context.Context, raw json.RawMessage) ([]byte, error) {
	var maxPO model.MaximoPOHeader
	if err := json.Unmarshal(raw, &maxPO); err != nil {
		return nil, fmt.Errorf("invalid json maximo po: %w", err)
	}

	// Hitung total nilai PO untuk verifikasi Budget Lock
	var totalPOAmount float64
	var budgetCode, costCenter string
	var sapItems []model.SAPGrowPOItem

	for idx, line := range maxPO.Lines {
		totalPOAmount += line.OrderQty * line.UnitCost
		if budgetCode == "" {
			budgetCode = line.BudgetCode
			costCenter = line.CostCenter
		}

		sapItems = append(sapItems, model.SAPGrowPOItem{
			PurchaseOrderItem:         fmt.Sprintf("%d", (idx+1)*10),
			Material:                  line.ItemNum,
			OrderQuantity:             fmt.Sprintf("%.2f", line.OrderQty),
			PurchaseOrderQuantityUnit: line.OrderUnit,
			NetPriceAmount:            fmt.Sprintf("%.2f", line.UnitCost),
			AccountAssignmentCategory: "K",
			CostCenter:                line.CostCenter,
			GLAccount:                 line.MainAccount,
		})
	}

	// Validasi & Lock Budget
	if budgetCode != "" {
		combo := fmt.Sprintf("%s-%s", costCenter, budgetCode)
		year := time.Now().Year()
		ok, err := s.PGRepo.CheckAndLockBudget(ctx, combo, year, "PO", maxPO.PONum, totalPOAmount)
		if !ok || err != nil {
			return nil, fmt.Errorf("transaction lock budget rejected: %v", err)
		}
	}

	// Bentuk struktur OData SAP Grow
	sapPO := model.SAPGrowPOPayload{
		CompanyCode:             "1000",
		PurchasingOrganization:  "1010",
		PurchasingGroup:         "001",
		Supplier:                maxPO.Vendor,
		PurchaseOrderDate:       maxPO.OrderDate,
		DocumentCurrency:        maxPO.CurrencyCode,
		CorrespncExternalRefDoc: maxPO.PONum,
		ToPurchaseOrderItem:     sapItems,
	}

	return json.Marshal(sapPO)
}

// 3. PUSH CORE DOCUMENTS KE SAP GROW (Tier 2 Dispatcher)
func (s *SyncService) PushDocumentsToSAP(ctx context.Context) int {
	docs, err := s.PGRepo.FetchAndLockPendingDocuments(ctx, 20)
	if err != nil || len(docs) == 0 {
		return 0
	}

	pushedCount := 0
	for _, doc := range docs {
		endpoint := "/sap/opu/odata/sap/API_PURCHASEORDER_PROCESS_SRV/A_PurchaseOrder"
		if doc.DocType == "GRN" {
			endpoint = "/sap/opu/odata/sap/API_MATERIAL_DOCUMENT_SRV/A_MaterialDocumentHeader"
		}

		respBody, sapErr := s.SAPClient.PostODataDocument(endpoint, doc.SAPPayload)
		if sapErr != nil {
			log.Printf("[WARN] Push to SAP Failed for %s: %v", doc.DocumentNo, sapErr)
			_ = s.PGRepo.UpdateSAPError(ctx, doc.ID, "error_conn", sapErr.Error(), json.RawMessage(respBody))
		} else {
			// Ekstrak Document No dari response OData
			var respMap map[string]interface{}
			_ = json.Unmarshal([]byte(respBody), &respMap)
			sapDocNo := fmt.Sprintf("SAP-%s-%d", doc.DocType, time.Now().Unix())
			if d, ok := respMap["d"].(map[string]interface{}); ok {
				if po, exists := d["PurchaseOrder"].(string); exists {
					sapDocNo = po
				} else if mat, exists := d["MaterialDocument"].(string); exists {
					sapDocNo = mat
				}
			}

			_ = s.PGRepo.UpdateSAPSuccess(ctx, doc.ID, sapDocNo, json.RawMessage(respBody))
			pushedCount++
			log.Printf("[INFO] Push to SAP Success: %s -> %s", doc.DocumentNo, sapDocNo)
		}
	}
	return pushedCount
}
