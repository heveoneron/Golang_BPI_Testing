package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
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
			_ = s.PGRepo.InsertIntegrationLog(ctx, fmt.Sprintf("GRP-%s-%s", rec.DocType, rec.DocumentNo), "MAXIMO", "MIDDLEWARE", rec.DocType, rec.DocumentNo, "RECEIVED", "", fmt.Sprintf("Data pulled from SQL Server (TransID: %s, Table: %s)", rec.TransID, rec.IfaceTbName))
			if rec.TransID != "" {
				_ = s.SQLRepo.MarkProcessed(ctx, rec.TransID, rec.IfaceTbName)
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

		switch strings.ToUpper(stage.DocType) {
		case "PO", "AXMXPO":
			sapPayloadBytes, transformErr = s.transformPO(ctx, stage.RawPayload)
		case "AXVENDOR", "VENDOR", "COMPANIES", "BP":
			sapPayloadBytes, transformErr = s.transformVendor(ctx, stage.RawPayload)
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
			_ = s.PGRepo.InsertIntegrationLog(ctx, coreDoc.GroupID, "MIDDLEWARE", "MIDDLEWARE", coreDoc.DocType, coreDoc.DocumentNo, "TRANSFORMED", "", "Staging payload converted to SAP OData format")
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

		itemNum := line.ItemNum
		if itemNum == "" {
			itemNum = "PK0514"
		}

		sapItems = append(sapItems, model.SAPGrowPOItem{
			PurchaseOrderItem:         fmt.Sprintf("%d", (idx+1)*10),
			PurchaseOrderItemText:     itemNum,
			Material:                  itemNum,
			Plant:                     "5110",
			StorageLocation:           "511C",
			OrderQuantity:             fmt.Sprintf("%.0f", line.OrderQty),
			PurchaseOrderQuantityUnit: line.OrderUnit,
			NetPriceAmount:            fmt.Sprintf("%.0f", line.UnitCost),
			DocumentCurrency:          maxPO.CurrencyCode,
			NetPriceQuantity:          "1",
			OrderPriceUnit:            line.OrderUnit,
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

	docCurrency := maxPO.CurrencyCode
	if docCurrency == "" {
		docCurrency = "USD"
	}
	orderDate := maxPO.OrderDate
	if orderDate == "" {
		orderDate = time.Now().Format("2006-01-02")
	}

	// Bentuk struktur OData v4 SAP Grow
	sapPO := model.SAPGrowPOPayload{
		PurchaseOrderType:       "NB",
		CompanyCode:             "5110",
		PurchasingOrganization:  "5110",
		PurchasingGroup:         "001",
		Supplier:                maxPO.Vendor,
		DocumentCurrency:        docCurrency,
		Language:                "EN",
		PurchaseOrderDate:       orderDate,
		PaymentTerms:            "0001",
		IncotermsClassification: "EXW",
		IncotermsLocation1:      "Jakarta",
		PurchaseOrderItem:       sapItems,
	}

	return json.Marshal(sapPO)
}

// TransformVendor mengekspor transformasi vendor Maximo ke SAP Business Partner untuk testing / controller
func (s *SyncService) TransformVendor(ctx context.Context, raw json.RawMessage) ([]byte, error) {
	return s.transformVendor(ctx, raw)
}

// Transformasi data Vendor Maximo -> SAP Grow OData v2 Business Partner (A_BusinessPartner Deep Entity)
func (s *SyncService) transformVendor(ctx context.Context, raw json.RawMessage) ([]byte, error) {
	var company, bpiVendorGroup, currencyCode, name, orgID string

	// 1. Coba unmarshal sebagai MaximoVendorRecord langsung
	var vRec model.MaximoVendorRecord
	if err := json.Unmarshal(raw, &vRec); err == nil && vRec.Company != "" {
		company = vRec.Company
		bpiVendorGroup = vRec.BPIVendorGroup
		currencyCode = vRec.CurrencyCode
		name = vRec.Name
		orgID = vRec.OrgID
	} else {
		// 2. Coba parse map dinamis (misal dari extractGeneric atau format lain)
		var m map[string]interface{}
		if err := json.Unmarshal(raw, &m); err == nil {
			getString := func(keys ...string) string {
				for _, k := range keys {
					if v, ok := m[k].(string); ok && strings.TrimSpace(v) != "" {
						return strings.TrimSpace(v)
					}
				}
				return ""
			}
			company = getString("COMPANY", "company", "Company", "BusinessPartner")
			bpiVendorGroup = getString("BPIVENDORGROUP", "bpivendorgroup", "VendorGroup", "vendor_group")
			currencyCode = getString("CURRENCYCODE", "currencycode", "CurrencyCode", "currency")
			name = getString("NAME", "name", "Name", "OrganizationBPName1")
			orgID = getString("ORGID", "orgid", "OrgID")

			// Jika data terbungkus di dalam array "DATA"
			if company == "" {
				if dataArr, ok := m["DATA"].([]interface{}); ok && len(dataArr) > 0 {
					if firstRow, ok := dataArr[0].(map[string]interface{}); ok {
						for k, v := range firstRow {
							valStr := fmt.Sprintf("%v", v)
							switch strings.ToLower(k) {
							case "company":
								company = valStr
							case "bpivendorgroup":
								bpiVendorGroup = valStr
							case "currencycode":
								currencyCode = valStr
							case "name":
								name = valStr
							case "orgid":
								orgID = valStr
							}
						}
					}
				}
			}
		}
	}

	company = strings.TrimSpace(company)
	if company == "" {
		return nil, fmt.Errorf("field company / BusinessPartner wajib terisi")
	}

	// 3. Mapping BusinessPartnerGrouping
	bpGrouping := "Z001"
	if s.PGRepo != nil {
		if g, err := s.PGRepo.GetBPGrouping(ctx, bpiVendorGroup); err == nil && g != "" {
			bpGrouping = g
		}
	}

	// 4. Split name ke OrganizationBPName 1..4 (maksimal 40 karakter per field)
	name1, name2, name3, name4 := splitBPName(name)

	// 5. Mapping Reconciliation Account
	reconAccount := "211001"
	if s.PGRepo != nil {
		if acc, err := s.PGRepo.GetReconAccount(ctx, bpiVendorGroup, "5110"); err == nil && acc != "" {
			reconAccount = acc
		}
	}

	// 6. Currency
	docCurrency := strings.TrimSpace(currencyCode)
	if docCurrency == "" {
		docCurrency = "IDR"
	}

	_ = orgID // Bisa dipakai di masa depan jika ada mapping org

	// 7. Bentuk payload target SAP Grow OData v2 Business Partner
	sapBP := model.SAPGrowBPPayload{
		BusinessPartner:          company,
		BusinessPartnerCategory:  "2",
		BusinessPartnerGrouping:  bpGrouping,
		OrganizationBPName1:      name1,
		OrganizationBPName2:      name2,
		OrganizationBPName3:      name3,
		OrganizationBPName4:      name4,
		SearchTerm1:              "",
		FormOfAddress:            "0003",
		BusinessPartnerIsBlocked: false,
		ToBusinessPartnerRole: []model.SAPBPRole{
			{BusinessPartnerRole: "FLVN00"},
			{BusinessPartnerRole: "FLVN01"},
		},
		ToBusinessPartnerAddress: []model.SAPBPAddress{
			{Country: "ID", Language: "EN"},
		},
		ToSupplier: &model.SAPBPSupplier{
			ToSupplierCompany: []model.SAPSupplierCompany{
				{
					CompanyCode:           "5110",
					ReconciliationAccount: reconAccount,
				},
			},
			ToSupplierPurchasingOrg: []model.SAPSupplierPurchasingOrg{
				{
					PurchasingOrganization: "5110",
					PurchaseOrderCurrency:  docCurrency,
				},
			},
		},
	}

	return json.Marshal(sapBP)
}

// splitBPName memecah nama vendor menjadi 4 slot (max 40 karakter per slot) berbasis kata (word-wrap)
// agar kata tidak terpotong di tengah-tengah.
func splitBPName(name string) (string, string, string, string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", "", "", ""
	}

	words := strings.Fields(name)
	chunks := make([]string, 0, 4)
	var current strings.Builder

	pushChunk := func() {
		if current.Len() > 0 {
			chunks = append(chunks, current.String())
			current.Reset()
		}
	}

	for _, word := range words {
		if len(chunks) == 3 {
			// Sudah di slot terakhir (OrganizationBPName4)
			wordRunes := []rune(word)
			currLen := len([]rune(current.String()))
			if currLen == 0 {
				if len(wordRunes) > 40 {
					current.WriteString(string(wordRunes[:40]))
				} else {
					current.WriteString(word)
				}
			} else if currLen+1+len(wordRunes) <= 40 {
				current.WriteByte(' ')
				current.WriteString(word)
			}
			continue
		}

		wordRunes := []rune(word)
		// Jika ada kata tunggal tanpa spasi yang melebihi 40 karakter
		for len(wordRunes) > 40 {
			pushChunk()
			if len(chunks) >= 4 {
				break
			}
			chunks = append(chunks, string(wordRunes[:40]))
			wordRunes = wordRunes[40:]
		}
		if len(chunks) >= 4 {
			break
		}
		if len(wordRunes) == 0 {
			continue
		}
		word = string(wordRunes)

		currLen := len([]rune(current.String()))
		if currLen == 0 {
			current.WriteString(word)
		} else if currLen+1+len([]rune(word)) <= 40 {
			current.WriteByte(' ')
			current.WriteString(word)
		} else {
			pushChunk()
			if len(chunks) >= 4 {
				break
			}
			current.WriteString(word)
		}
	}
	pushChunk()

	var res [4]string
	for i := 0; i < len(chunks) && i < 4; i++ {
		res[i] = strings.TrimSpace(chunks[i])
	}
	return res[0], res[1], res[2], res[3]
}

// 3. PUSH CORE DOCUMENTS KE SAP GROW (Tier 2 Dispatcher)
func (s *SyncService) PushDocumentsToSAP(ctx context.Context) int {
	docs, err := s.PGRepo.FetchAndLockPendingDocuments(ctx, 20)
	if err != nil || len(docs) == 0 {
		return 0
	}

	pushedCount := 0
	for _, doc := range docs {
		// 1. Ambil pemetaan target service SAP secara dinamis dari tabel `table_api_mappings`
		targetService := doc.DocType
		mapping, mapErr := s.PGRepo.GetTableAPIMapping(ctx, doc.DocType)
		if mapErr == nil && mapping != nil {
			// Cek apakah relasi tabel sumber -> API SAP ini berstatus aktif di Web Panel
			if !mapping.IsActive {
				log.Printf("[SKIP] Dokumen %s (%s) dilewati: Status pemetaan tabel '%s' ke SAP sedang NONAKTIF di Table-to-API Mapping Matrix", doc.DocumentNo, doc.DocType, mapping.SourceTable)
				continue
			}
			if mapping.TargetServiceCode != "" {
				targetService = mapping.TargetServiceCode
			}
		}

		// 2. Ambil konfigurasi endpoint & kredensial spesifik berdasarkan targetService dari `sap_endpoints`
		ep, err := s.PGRepo.GetSAPEndpointByCode(ctx, targetService)
		if err != nil || ep == nil {
			log.Printf("[WARN] Tidak ditemukan endpoint SAP untuk target service '%s' (dokumen %s, tipe %s). Pastikan layanan terdaftar di SAP Integration Endpoints.", targetService, doc.DocumentNo, doc.DocType)
			_ = s.PGRepo.UpdateSAPError(ctx, doc.ID, "error_data", fmt.Sprintf("SAP Endpoint untuk service '%s' tidak ditemukan di registry database", targetService), nil)
			continue
		}

		if !ep.IsActive {
			log.Printf("[SKIP] Dokumen %s (%s) dilewati: Layanan SAP '%s' berstatus NONAKTIF di registry", doc.DocumentNo, doc.DocType, ep.ServiceCode)
			continue
		}

		// 2. Eksekusi request ke SAP Grow OData dengan auth & token handshake terisolasi
		statusCode, sapDocNo, respBody, sapErr := s.SAPClient.ExecuteOData(ep, "POST", doc.SAPPayload)
		targetURL := fmt.Sprintf("%s%s", strings.TrimRight(ep.BaseURL, "/"), ep.EndpointPath)

		if sapErr != nil {
			log.Printf("[WARN] Push to SAP Failed for %s (%s) [Target API: %s]: %v", doc.DocumentNo, doc.DocType, targetURL, sapErr)
			statusType := "error_data"
			if statusCode == 0 {
				statusType = "error_conn"
			}
			_ = s.PGRepo.UpdateSAPError(ctx, doc.ID, statusType, sapErr.Error(), json.RawMessage(respBody))
			errMsg := fmt.Sprintf("Failed sending to SAP Grow via %s [Target API: %s]: %s", ep.ServiceCode, targetURL, sapErr.Error())
			_ = s.PGRepo.InsertIntegrationLog(ctx, doc.GroupID, "MIDDLEWARE", "SAP GROW", doc.DocType, doc.DocumentNo, "FAILED", sapErr.Error(), errMsg)
		} else {
			_ = s.PGRepo.UpdateSAPSuccess(ctx, doc.ID, sapDocNo, json.RawMessage(respBody))
			successMsg := fmt.Sprintf("Successfully posted to SAP Grow via %s [Target API: %s] (SAP Doc: %s)", ep.ServiceCode, targetURL, sapDocNo)
			_ = s.PGRepo.InsertIntegrationLog(ctx, doc.GroupID, "MIDDLEWARE", "SAP GROW", doc.DocType, doc.DocumentNo, "SUCCESS", "", successMsg)
			pushedCount++
			log.Printf("[INFO] Push to SAP Success: %s -> %s [Target API: %s]", doc.DocumentNo, sapDocNo, targetURL)
		}
	}
	return pushedCount
}
