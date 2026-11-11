package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strconv"
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

// dv membaca nilai dari menu Default Value (tabel sap_default_values, lihat repository.DefaultValueSpecs).
func (s *SyncService) dv(ctx context.Context, key string) string {
	return s.PGRepo.Default(ctx, key)
}

func splitList(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
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
			if rec.TransID != "" && s.SQLRepo != nil {
				_ = s.SQLRepo.MarkProcessed(ctx, rec.TransID, rec.IfaceTbName)
				for _, aid := range rec.AssociatedTransIDs {
					_ = s.SQLRepo.MarkProcessed(ctx, fmt.Sprintf("%d", aid), rec.IfaceTbName)
				}
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

		// Kiriman ulang Maximo untuk dokumen yang sudah sukses di SAP: jangan transform (budget lock) / post ulang.
		if posted, err := s.PGRepo.IsAlreadyPosted(ctx, stage.DocType, docNo, stage.FlowType, stage.RawPayload); err == nil && posted {
			log.Printf("[SKIP] %s %s sudah sukses di SAP dengan data identik; kiriman ulang Maximo diabaikan", stage.DocType, docNo)
			_ = s.PGRepo.MarkStagingProcessed(ctx, stage.ID, "DUPLICATE", "Sudah sukses di SAP dengan data identik")
			_ = s.PGRepo.InsertIntegrationLog(ctx, fmt.Sprintf("GRP-%s-%s", stage.DocType, docNo), "MIDDLEWARE", "MIDDLEWARE", stage.DocType, docNo, "DUPLICATE", "", "Kiriman ulang Maximo diabaikan: dokumen sudah sukses di SAP")
			continue
		}

		switch strings.ToUpper(stage.DocType) {
		case "PO", "AXMXPO":
			sapPayloadBytes, transformErr = s.transformPO(ctx, stage.RawPayload)
		case "AXVENDOR", "VENDOR", "COMPANIES", "BP":
			sapPayloadBytes, transformErr = s.transformVendor(ctx, stage.RawPayload)
		case "GRN", "AXRECEIPT", "MATRECTRANS", "RECEIPT":
			sapPayloadBytes, transformErr = s.transformGRN(ctx, stage.RawPayload)
		case "ITEM", "AXITEM", "PRODUCT":
			sapPayloadBytes, transformErr = s.transformItem(ctx, stage.RawPayload)
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

// TransformPO mengekspor fungsi transformasi PO untuk testing dan controller
func (s *SyncService) TransformPO(ctx context.Context, raw json.RawMessage) ([]byte, error) {
	return s.transformPO(ctx, raw)
}

// Transformasi khusus PO Maximo -> SAP Grow OData v4 Purchase Order (Deep Insert)
func (s *SyncService) transformPO(ctx context.Context, raw json.RawMessage) ([]byte, error) {
	var maxPO model.MaximoPOHeader
	if err := json.Unmarshal(raw, &maxPO); err != nil {
		return nil, fmt.Errorf("invalid json maximo po: %w", err)
	}

	// Fallback jika LINES kosong, coba periksa apakah format generic map
	if len(maxPO.Lines) == 0 {
		var genericMap map[string]interface{}
		if gErr := json.Unmarshal(raw, &genericMap); gErr == nil {
			if po, ok := genericMap["PONUM"].(string); ok && po != "" {
				maxPO.PONum = po
			}
			if v, ok := genericMap["VENDOR"].(string); ok && v != "" {
				maxPO.Vendor = v
			}
			if d, ok := genericMap["ORDERDATE"].(string); ok && d != "" {
				maxPO.OrderDate = d
			}
			if c, ok := genericMap["CURRENCYCODE"].(string); ok && c != "" {
				maxPO.CurrencyCode = c
			}
			if dpType, ok := genericMap["DOWNPAYMENTTYPE"].(string); ok && dpType != "" {
				maxPO.DownPaymentType = dpType
			}
			if dpDue, ok := genericMap["DOWNPAYMENTDUEDATE"].(string); ok && dpDue != "" {
				maxPO.DownPaymentDueDate = dpDue
			}
			if dpPct, ok := genericMap["DOWNPAYMENTPERCENTAGE"].(float64); ok {
				maxPO.DownPaymentPercentageOfTotAmt = dpPct
			}
		}
	}

	// 1. Tentukan PurchaseOrderType berdasarkan awalan PONUM
	// "If Prefix PO 45 = ZPRC, Else If Prefix PO 47 = ZCTR, Else fallback ZBPI atau NB"
	cleanPO := strings.TrimSpace(maxPO.PONum)
	sapPONum := ConvertMaximoPOToSAP(cleanPO)
	poType := s.dv(ctx, "PO_TYPE_DEFAULT")
	if strings.HasPrefix(cleanPO, "45") {
		poType = s.dv(ctx, "PO_TYPE_PREFIX_45")
	} else if strings.HasPrefix(cleanPO, "47") {
		poType = s.dv(ctx, "PO_TYPE_PREFIX_47")
	}

	// 2. Nomor PO asli Maximo selalu ditaruh di field SupplierRespSalesPersonName

	salesPerson := cleanPO



	docCurrency := strings.TrimSpace(maxPO.CurrencyCode)
	if docCurrency == "" {
		docCurrency = s.dv(ctx, "PO_DEFAULT_CURRENCY")
	}
	defMaterial, defUOM, defPlant, defCostCenter := s.dv(ctx, "PO_DEFAULT_MATERIAL"), s.dv(ctx, "PO_DEFAULT_UOM"), s.dv(ctx, "PO_DEFAULT_PLANT"), s.dv(ctx, "PO_DEFAULT_COST_CENTER")
	acctCat, noTaxCode, taxCond, priceCond := s.dv(ctx, "PO_ACCOUNT_ASSIGNMENT_CATEGORY"), s.dv(ctx, "PO_NO_TAX_CODE"), s.dv(ctx, "PO_TAX_CONDITION_TYPE"), s.dv(ctx, "PO_PRICE_CONDITION_TYPE")
	noteType, noteLang := s.dv(ctx, "PO_ITEM_NOTE_TEXT_TYPE"), s.dv(ctx, "PO_LANGUAGE")
	defTaxRate, _ := strconv.ParseFloat(s.dv(ctx, "PO_DEFAULT_TAX_RATE"), 64)

	orderDate := strings.TrimSpace(maxPO.OrderDate)
	if orderDate == "" {
		orderDate = time.Now().Format("2006-01-02")
	}

	// 3. Down Payment (DP) fields di Header (Tipe 3 & 4)
	var dpType string
	var dpPct *float64
	var dpDueDate string

	if maxPO.DownPaymentPercentageOfTotAmt > 0 || maxPO.DownPaymentDueDate != "" || strings.EqualFold(maxPO.DownPaymentType, "V") {
		dpType = "V"
		val := maxPO.DownPaymentPercentageOfTotAmt
		dpPct = &val
		dpDueDate = maxPO.DownPaymentDueDate
	}

	// 4. Hitung total nilai PO untuk verifikasi Budget Lock & bangun items
	// Pagu ditahan per kombinasi [MAINACCOUNT]-[BUDGETCODE] dan tahun budget baris PO.
	type budgetKey struct {
		combo string
		year  int
	}
	budgetLines := map[budgetKey]float64{}
	var sapItems []model.SAPGrowPOItem

	for idx, line := range maxPO.Lines {
		if strings.TrimSpace(line.MainAccount) != "" && strings.TrimSpace(line.BudgetCode) != "" {
			year := time.Now().Year()
			if y, err := strconv.Atoi(strings.TrimSpace(line.BudgetYear)); err == nil && y > 2000 {
				year = y
			}
			budgetLines[budgetKey{BudgetCombination(line.MainAccount, line.BudgetCode), year}] += line.OrderQty * line.UnitCost
		}

		itemNum := strings.TrimSpace(line.ItemNum)
		if itemNum == "" {
			itemNum = defMaterial
		}
		if itemNum == "" {
			return nil, fmt.Errorf("baris PO %d: ITEMNUM kosong dan PO_DEFAULT_MATERIAL tidak diisi", idx+1)
		}

		// Line Number: gunakan POLINENUM jika ada, jika tidak urutkan idx+1
		lineNumStr := fmt.Sprintf("%d", line.POLineNum)
		if line.POLineNum <= 0 {
			lineNumStr = fmt.Sprintf("%d", idx+1)
		}

		// Deskripsi Item:
		// PurchaseOrderItemText maksimal 40 karakter
		desc := strings.TrimSpace(line.Description)
		if desc == "" {
			desc = itemNum
		}
		itemText := truncateRunes(desc, 40)

		// UOM conversion
		orderUnit := strings.TrimSpace(line.OrderUnit)
		if orderUnit == "" {
			orderUnit = defUOM
		}
		if s.PGRepo != nil {
			if u, err := s.PGRepo.GetSAPUOM(ctx, orderUnit); err == nil && u != "" {
				orderUnit = u
			}
		}

		// Plant
		plant := strings.TrimSpace(line.SiteID)
		if plant == "" {
			plant = strings.TrimSpace(maxPO.SiteID)
		}
		if plant == "" {
			plant = defPlant
		}

		// Cost center untuk AccountAssignment
		lineCostCenter := strings.TrimSpace(line.CostCenter)
		if lineCostCenter == "" {
			lineCostCenter = defCostCenter
		}
		if lineCostCenter == "" {
			return nil, fmt.Errorf("baris PO %s: COSTCENTER kosong dan PO_DEFAULT_COST_CENTER tidak diisi", lineNumStr)
		}

		itemObj := model.SAPGrowPOItem{
			PurchaseOrderItem:         lineNumStr,
			Material:                  itemNum,
			Plant:                     plant,
			AccountAssignmentCategory: acctCat,
			PurchaseOrderItemText:     itemText,
			PurchaseOrderQuantityUnit: orderUnit,
			OrderPriceUnit:            orderUnit,
			NetPriceAmount:            line.UnitCost,
			DocumentCurrency:          docCurrency,
			OrderQuantity:             line.OrderQty,
			NetPriceQuantity:          1,
			InvoiceIsGoodsReceiptBased: true,
			PurOrdAccountAssignment: []model.SAPGrowPurOrdAccountAssignment{
				{
					PurchaseOrder:           sapPONum,
					PurchaseOrderItem:       lineNumStr,
					AccountAssignmentNumber: "1",
					CostCenter:              lineCostCenter,
				},
			},
			PurchaseOrderItemNote: []model.SAPGrowPurchaseOrderItemNote{
				{
					PurchaseOrder:     sapPONum,
					PurchaseOrderItem: lineNumStr,
					TextObjectType:    noteType,
					Language:          noteLang,
					PlainLongText:     desc,
				},
			},
		}

		// 5. Logika Pajak (_PurOrdPricingElement vs TaxCode V0)
		// Ketentuan:
		// Jika TaxCode1 terisi maka ConditionType dan ConditionRatioUnit terisi hardcode,
		// Lalu ConditionRateAmount diisi dari UnitCost (PMP0) dan tax rate (DRG1), NetPriceAmount diisi dari UnitCost.
		// Jika tidak ada Tax maka _PurOrdPricingElement tidak usah diisi (hanya TaxCode: "V0" dan NetPriceAmount: UnitCost).
		tax1 := strings.TrimSpace(line.TaxCodePPN)
		if tax1 != "" {
			taxRate := parseTaxRateOr(tax1, defTaxRate)

			itemObj.PurOrdPricingElement = []model.SAPGrowPurOrdPricingElement{
				{
					PurchaseOrderItem:      lineNumStr,
					ConditionType:          taxCond,
					ConditionRateAmount:    taxRate,
					ConditionRateRatioUnit: "%",
				},
				{
					PurchaseOrderItem:   lineNumStr,
					ConditionType:       priceCond,
					ConditionRateAmount: line.UnitCost,
				},
			}
		} else {
			itemObj.TaxCode = noTaxCode
		}

		sapItems = append(sapItems, itemObj)
	}

	// Validasi & Lock Budget: lock lama PO ini dilepas dulu agar revisi baris tidak meninggalkan lock basi.
	if len(budgetLines) > 0 && s.PGRepo != nil && s.PGRepo.Pool != nil {
		_ = s.PGRepo.ReleaseBudgetLocks(ctx, "PO", cleanPO)
		for k, amount := range budgetLines {
			ok, err := s.PGRepo.CheckAndLockBudget(ctx, k.combo, k.year, "PO", cleanPO+"|"+k.combo, math.Round(amount*100)/100)
			if !ok || err != nil {
				_ = s.PGRepo.ReleaseBudgetLocks(ctx, "PO", cleanPO)
				return nil, fmt.Errorf("transaction lock budget rejected (%s): %v", k.combo, err)
			}
		}
	}

	// Bentuk struktur OData v4 SAP Grow
	sapPO := model.SAPGrowPOPayload{
		PurchaseOrder:                 sapPONum,
		PurchaseOrderType:             poType,
		PurchaseOrderDate:             orderDate,
		CompanyCode:                   s.dv(ctx, "PO_COMPANY_CODE"),
		PurchasingOrganization:        s.dv(ctx, "PO_PURCHASING_ORG"),
		PurchasingGroup:               s.dv(ctx, "PO_PURCHASING_GROUP"),
		Supplier:                      strings.TrimSpace(maxPO.Vendor),
		SupplierRespSalesPersonName:   salesPerson,
		DocumentCurrency:              docCurrency,
		DownPaymentType:               dpType,
		DownPaymentPercentageOfTotAmt: dpPct,
		DownPaymentDueDate:            dpDueDate,
		PurchaseOrderItem:             sapItems,
	}

	return json.Marshal(sapPO)
}

func parseTaxRate(taxCode string) float64 {
	return parseTaxRateOr(taxCode, 11.0)
}

// parseTaxRateOr membaca tarif dari kode pajak Maximo (PPN11, V11, 11%); def bila tidak berisi angka.
func parseTaxRateOr(taxCode string, def float64) float64 {
	clean := strings.ToUpper(strings.TrimSpace(taxCode))
	clean = strings.TrimPrefix(clean, "PPN")
	clean = strings.TrimPrefix(clean, "V")
	clean = strings.TrimSuffix(clean, "%")
	if val, err := strconv.ParseFloat(clean, 64); err == nil && val > 0 {
		return val
	}
	return def
}

// ConvertMaximoPOToSAP mengonversi nomor PO Maximo ke format PurchaseOrder SAP Grow sesuai aturan:
// - Prefix 45: Hapus 1 digit angka 0 setelah "45" (cth: 4500005678 -> 450005678)
// - Prefix 47: Hapus 4 digit angka 0 setelah "47", ubah "-" jadi "0", jika digit setelah "-" hanya 1 digit, tambahkan 0 di depannya (cth: 4700000012-23 -> 470012023, 4700000123-1 -> 470123001)
// - Fallback prefix lain: gunakan nomor asli yang di-trim
func ConvertMaximoPOToSAP(maximoPONum string) string {
	cleanPO := strings.TrimSpace(maximoPONum)
	if cleanPO == "" {
		return ""
	}

	// 1. Prefix 45: Hapus 1 digit '0' setelah "45"
	if strings.HasPrefix(cleanPO, "45") {
		rest := cleanPO[2:]
		if strings.HasPrefix(rest, "0") {
			return "45" + rest[1:]
		}
		return cleanPO
	}

	// 2. Prefix 47: Hapus 4 digit '0' setelah "47", '-' ubah jadi '0', padding digit setelah '-'
	if strings.HasPrefix(cleanPO, "47") {
		parts := strings.SplitN(cleanPO, "-", 2)
		mainPart := parts[0]
		subPart := ""
		if len(parts) > 1 {
			subPart = strings.TrimSpace(parts[1])
		}

		// Hapus maksimal 4 angka '0' setelah "47"
		rest := mainPart[2:]
		zerosRemoved := 0
		for zerosRemoved < 4 && strings.HasPrefix(rest, "0") {
			rest = rest[1:]
			zerosRemoved++
		}
		convertedMain := "47" + rest

		if subPart != "" {
			if len(subPart) == 1 {
				subPart = "0" + subPart
			}
			return convertedMain + "0" + subPart
		}
		return convertedMain
	}

	return cleanPO
}

// defaultPlantMap adalah pemetaan lama SITEID -> Plant (dipakai bila menu Default Value tidak tersedia).
const defaultPlantMap = "JKT=1000,1000=1000,BTNG=2000,2000=2000"

func convertSiteIDToPlant(siteID string, defaultPlant string) string {
	return convertSiteIDToPlantMap(siteID, defaultPlant, defaultPlantMap)
}

// convertSiteIDToPlantMap memetakan SITEID Maximo ke Plant SAP sesuai GRN_PLANT_MAP ("SITE=PLANT,...").
// SITEID yang tidak terdaftar dikirim apa adanya; SITEID kosong memakai defaultPlant.
func convertSiteIDToPlantMap(siteID, defaultPlant, plantMap string) string {
	clean := strings.ToUpper(strings.TrimSpace(siteID))
	if clean == "" {
		return defaultPlant
	}
	for _, pair := range splitList(plantMap) {
		if k, v, ok := strings.Cut(pair, "="); ok && strings.EqualFold(strings.TrimSpace(k), clean) {
			return strings.TrimSpace(v)
		}
	}
	return clean
}

func formatODataV2Date(dateStr string, preventBackdate bool) (string, string) {
	now := time.Now()
	docDate := now
	postingDate := now

	if trimmed := strings.TrimSpace(dateStr); trimmed != "" {
		formats := []string{
			"2006-01-02 15:04:05",
			"2006-01-02",
			time.RFC3339,
			"2006/01/02",
		}
		for _, f := range formats {
			if t, err := time.Parse(f, trimmed); err == nil {
				docDate = t
				postingDate = t
				break
			}
		}
	}

	// Larangan backdate: jika tanggal masa lalu, postingDate diset ke hari ini
	if preventBackdate {
		todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		if postingDate.Before(todayStart) {
			postingDate = now
		}
	}

	docDateMs := docDate.UnixNano() / int64(time.Millisecond)
	postingDateMs := postingDate.UnixNano() / int64(time.Millisecond)

	return fmt.Sprintf("/Date(%d)/", docDateMs), fmt.Sprintf("/Date(%d)/", postingDateMs)
}

// ValidatePOChange memeriksa apakah PO dapat diubah/direvisi berdasarkan dokumen material SAP.
// Aturan:
// - if no matdoc found = PO bisa di change (true)
// - if matdoc found = hitung 101 - 102 - 122
// - If Hasil > 0 = PO do not change (false)
// - if hasil <= 0 = PO monggo di change (true)
func (s *SyncService) ValidatePOChange(ctx context.Context, poNum string) (bool, string, error) {
	if s.SAPClient == nil {
		return true, "", nil
	}

	ep, err := s.PGRepo.GetSAPEndpointByCode(ctx, "GR_WITH_PO")
	if err != nil || ep == nil {
		ep, err = s.PGRepo.GetSAPEndpointByCode(ctx, "GRN")
	}
	if err != nil || ep == nil {
		return true, "", nil
	}

	items, err := s.SAPClient.GetMaterialDocumentItemsByPO(ep, poNum)
	if err != nil {
		return false, "", fmt.Errorf("gagal query material document untuk PO %s: %w", poNum, err)
	}

	if len(items) == 0 {
		return true, "No material document found, PO can be changed", nil
	}

	var sum101, sum102, sum122 float64
	for _, it := range items {
		switch strings.TrimSpace(it.GoodsMovementType) {
		case "101":
			sum101 += it.QuantityInEntryUnit
		case "102":
			sum102 += it.QuantityInEntryUnit
		case "122":
			sum122 += it.QuantityInEntryUnit
		}
	}

	hasil := sum101 - sum102 - sum122
	if hasil > 0 {
		return false, fmt.Sprintf("PO tidak dapat diubah: terdapat dokumen penerimaan aktif (101: %.2f - 102: %.2f - 122: %.2f = %.2f > 0)",
			sum101, sum102, sum122, hasil), nil
	}

	return true, fmt.Sprintf("PO dapat diubah: hasil kalkulasi material document net qty adalah %.2f", hasil), nil
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
	bpGrouping := s.dv(ctx, "BP_DEFAULT_GROUPING")
	bpCompany := s.dv(ctx, "BP_COMPANY_CODE")
	if s.PGRepo != nil {
		if g, err := s.PGRepo.GetBPGrouping(ctx, bpiVendorGroup); err == nil && g != "" {
			bpGrouping = g
		}
	}

	// 4. Split name ke OrganizationBPName 1..4 (maksimal 40 karakter per field)
	name1, name2, name3, name4 := splitBPName(name)

	// 5. Mapping Reconciliation Account
	reconAccount := s.dv(ctx, "BP_DEFAULT_RECON_ACCOUNT")
	if s.PGRepo != nil {
		if acc, err := s.PGRepo.GetReconAccount(ctx, bpiVendorGroup, bpCompany); err == nil && acc != "" {
			reconAccount = acc
		}
	}

	// 6. Currency
	docCurrency := strings.TrimSpace(currencyCode)
	if docCurrency == "" {
		docCurrency = s.dv(ctx, "BP_DEFAULT_CURRENCY")
	}
	var roles []model.SAPBPRole
	for _, role := range splitList(s.dv(ctx, "BP_ROLES")) {
		roles = append(roles, model.SAPBPRole{BusinessPartnerRole: role})
	}

	_ = orgID // Bisa dipakai di masa depan jika ada mapping org

	// 7. Bentuk payload target SAP Grow OData v2 Business Partner
	sapBP := model.SAPGrowBPPayload{
		BusinessPartner:          company,
		BusinessPartnerCategory:  s.dv(ctx, "BP_CATEGORY"),
		BusinessPartnerGrouping:  bpGrouping,
		OrganizationBPName1:      name1,
		OrganizationBPName2:      name2,
		OrganizationBPName3:      name3,
		OrganizationBPName4:      name4,
		SearchTerm1:              "",
		FormOfAddress:            s.dv(ctx, "BP_FORM_OF_ADDRESS"),
		BusinessPartnerIsBlocked: false,
		ToBusinessPartnerRole:    roles,
		ToBusinessPartnerAddress: []model.SAPBPAddress{
			{Country: s.dv(ctx, "BP_COUNTRY"), Language: s.dv(ctx, "BP_LANGUAGE")},
		},
		ToSupplier: &model.SAPBPSupplier{
			ToSupplierCompany: []model.SAPSupplierCompany{
				{
					CompanyCode:           bpCompany,
					ReconciliationAccount: reconAccount,
				},
			},
			ToSupplierPurchasingOrg: []model.SAPSupplierPurchasingOrg{
				{
					PurchasingOrganization: s.dv(ctx, "BP_PURCHASING_ORG"),
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

// TransformItem mengekspor transformasi Item/Material Maximo ke SAP Product OData v2 untuk testing / controller
func (s *SyncService) TransformItem(ctx context.Context, raw json.RawMessage) ([]byte, error) {
	return s.transformItem(ctx, raw)
}

// transformItem mentransformasi data Item Maximo ke skema OData v2 A_Product Deep Insert
func (s *SyncService) transformItem(ctx context.Context, raw json.RawMessage) ([]byte, error) {
	var itemNum, itemGroupID, itemType, stockType, issueUnit, orderUnit, description string

	// 1. Coba unmarshal sebagai model.MaximoItemPayload
	var p model.MaximoItemPayload
	if err := json.Unmarshal(raw, &p); err == nil && p.ItemNum != "" {
		itemNum = p.ItemNum
		itemGroupID = p.ItemGroupID
		itemType = p.ItemType
		stockType = p.StockType
		issueUnit = p.IssueUnit
		orderUnit = p.OrderUnit
		description = p.Description
	} else {
		// 2. Fallback parsing generic map
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
			itemNum = getString("ITEMNUM", "itemnum", "ItemNum", "Product", "product")
			itemGroupID = getString("ITEMGROUPID", "itemgroupid", "bpiitemgroupid", "BPIITEMGROUPID", "ProductGroup", "ItemGroupID")
			itemType = getString("ITEMTYPE", "itemtype", "ItemType", "ProductType")
			stockType = getString("STOCKTYPE", "stocktype", "Stocktype", "StockType")
			issueUnit = getString("ISSUEUNIT", "issueunit", "BaseUnit", "baseunit", "IssueUnit")
			orderUnit = getString("ORDERUNIT", "orderunit", "PurchaseOrderQuantityUnit", "OrderUnit")
			description = getString("DESCRIPTION", "description", "Description", "ProductDescription")

			// Cek jika terbungkus di dalam array DATA (ekstraksi generic)
			if itemNum == "" {
				if dataArr, ok := m["DATA"].([]interface{}); ok && len(dataArr) > 0 {
					if firstRow, ok := dataArr[0].(map[string]interface{}); ok {
						for k, v := range firstRow {
							valStr := fmt.Sprintf("%v", v)
							switch strings.ToLower(k) {
							case "itemnum", "product":
								itemNum = valStr
							case "itemgroupid", "bpiitemgroupid", "productgroup":
								itemGroupID = valStr
							case "itemtype", "producttype":
								itemType = valStr
							case "stocktype":
								stockType = valStr
							case "issueunit", "baseunit":
								issueUnit = valStr
							case "orderunit", "purchaseorderquantityunit":
								orderUnit = valStr
							case "description", "productdescription":
								description = valStr
							}
						}
					}
				}
			}
		}
	}

	itemNum = strings.ToUpper(strings.TrimSpace(itemNum))
	itemNum = strings.ReplaceAll(itemNum, "&", "N")
	if itemNum == "" {
		return nil, fmt.Errorf("field ITEMNUM / Product wajib terisi")
	}

	// 2. Mapping ProductType (Maximo ITEMTYPE -> SAP ProductType)
	// Default: ZITM
	productType := s.dv(ctx, "PRODUCT_DEFAULT_TYPE")
	serviceType := s.dv(ctx, "PRODUCT_SERVICE_TYPE")
	if s.PGRepo != nil && itemType != "" {
		if pt, err := s.PGRepo.GetMaterialConfigSAPValue(ctx, "ITEM_TYPE", itemType); err == nil && pt != "" {
			productType = pt
		} else if strings.EqualFold(itemType, "SERVICE") {
			productType = serviceType
		} else if strings.EqualFold(itemType, "ROH") {
			productType = "ROH"
		}
	} else if strings.EqualFold(itemType, "SERVICE") {
		productType = serviceType
	}

	// 3. Mapping ProductGroup (Kini dari Maximo StockType -> SAP ProductGroup / Material Group)
	// Default: PRODUCT_DEFAULT_GROUP atau fallback ke nilai stockType
	productGroup := s.dv(ctx, "PRODUCT_DEFAULT_GROUP")
	if s.PGRepo != nil && stockType != "" {
		if pg, err := s.PGRepo.GetMaterialConfigSAPValue(ctx, "STOCK_TYPE", stockType); err == nil && pg != "" {
			productGroup = pg
		} else {
			productGroup = strings.ToUpper(strings.TrimSpace(stockType))
		}
	} else if stockType != "" {
		productGroup = strings.ToUpper(strings.TrimSpace(stockType))
	}

	// 4. Mapping BaseUnit & PurchaseOrderQuantityUnit via uom_mappings
	baseUnit := s.dv(ctx, "PRODUCT_DEFAULT_BASE_UNIT")
	if s.PGRepo != nil && issueUnit != "" {
		if u, err := s.PGRepo.GetSAPUOM(ctx, issueUnit); err == nil && u != "" {
			baseUnit = u
		} else {
			baseUnit = strings.ToUpper(strings.TrimSpace(issueUnit))
		}
	} else if issueUnit != "" {
		baseUnit = strings.ToUpper(strings.TrimSpace(issueUnit))
	}

	poUnit := baseUnit
	if s.PGRepo != nil && orderUnit != "" {
		if u, err := s.PGRepo.GetSAPUOM(ctx, orderUnit); err == nil && u != "" {
			poUnit = u
		} else {
			poUnit = strings.ToUpper(strings.TrimSpace(orderUnit))
		}
	} else if orderUnit != "" {
		poUnit = strings.ToUpper(strings.TrimSpace(orderUnit))
	}

	// 5. Mapping ValuationClass (Kini dari Maximo ITEMGROUPID -> SAP ValuationClass)
	// Default: "3000" (Standard SAP Material Valuation Class)
	valuationClass := s.dv(ctx, "PRODUCT_DEFAULT_VALUATION_CLASS")
	if s.PGRepo != nil && itemGroupID != "" {
		if vc, err := s.PGRepo.GetMaterialConfigSAPValue(ctx, "ITEM_GROUP", itemGroupID); err == nil && vc != "" {
			valuationClass = vc
		}
	}

	// 6. Truncate Description to 40 chars (sesuai spesifikasi skema OData v2 A_ProductDescription MaxLength: 40)
	desc := truncateRunes(description, 40)

	// 7. Plant & Valuation Areas: menu Default Value PRODUCT_PLANTS (seed awal dari SAP_PRODUCT_PLANTS bila diisi)
	plants := splitList(s.dv(ctx, "PRODUCT_PLANTS"))
	if len(plants) == 0 {
		return nil, fmt.Errorf("PRODUCT_PLANTS di menu Default Value kosong")
	}
	valProc := s.dv(ctx, "PRODUCT_VALUATION_PROCEDURE")

	var plantItems []model.SAPProductPlant
	var valuationItems []model.SAPProductValuation
	for _, pl := range plants {
		plantItems = append(plantItems, model.SAPProductPlant{
			Product: itemNum,
			Plant:   pl,
		})
		valuationItems = append(valuationItems, model.SAPProductValuation{
			Product:                     itemNum,
			ValuationArea:               pl,
			ValuationClass:              valuationClass,
			InventoryValuationProcedure: valProc,
		})
	}

	// 8. Aturan SAP OData v2 M3/332: Alternative Unit dan PurchaseOrderQuantityUnit hanya dikirim jika BERBEDA dari BaseUnit.
	// Jika sama, SAP mengembalikan error 'The alternative unit of measure is the same as the base unit of measure'.
	var uomWrapper *model.SAPProductUOMWrapper
	poUnitToSend := ""
	if poUnit != "" && !strings.EqualFold(poUnit, baseUnit) {
		poUnitToSend = poUnit
		uomWrapper = &model.SAPProductUOMWrapper{
			Results: []model.SAPProductUOM{
				{
					Product:             itemNum,
					AlternativeUnit:     poUnit,
					BaseUnit:            baseUnit,
					QuantityNumerator:   "1",
					QuantityDenominator: "1",
				},
			},
		}
	}

	// 9. Bangun struktur Deep Insert OData v2 A_Product
	sapProduct := model.SAPProductPayload{
		Product:                   itemNum,
		ProductType:               productType,
		CrossPlantStatus:          "",
		CrossPlantStatusValidity:  nil,
		IsMarkedForDeletion:       false,
		PurchaseOrderQuantityUnit: poUnitToSend,
		ProductGroup:              productGroup,
		BaseUnit:                  baseUnit,
		ToDescription: model.SAPProductDescriptionWrapper{
			Results: []model.SAPProductDescription{
				{
					Product:            itemNum,
					Language:           s.dv(ctx, "PRODUCT_LANGUAGE"),
					ProductDescription: desc,
				},
			},
		},
		ToPlant: model.SAPProductPlantWrapper{
			Results: plantItems,
		},
		ToProductUnitsOfMeasure: uomWrapper,
		ToValuation: model.SAPProductValuationWrapper{
			Results: valuationItems,
		},
	}

	return json.Marshal(sapProduct)
}

// isSAPMatDoc: nomor Material Document SAP = 10 digit angka. Nomor palsu (SAP-<code>-...) tidak lolos.
func isSAPMatDoc(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) != 10 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isYear(s string) bool {
	y, err := strconv.Atoi(strings.TrimSpace(s))
	return err == nil && y >= 1900 && y <= 9999
}

// matDocYearFromResponse membaca MaterialDocumentYear dari respons SAP GR asli (OData v2 "d" atau root).
func matDocYearFromResponse(resp json.RawMessage) string {
	var m map[string]interface{}
	if len(resp) == 0 || json.Unmarshal(resp, &m) != nil {
		return ""
	}
	if d, ok := m["d"].(map[string]interface{}); ok {
		m = d
	}
	if y, ok := m["MaterialDocumentYear"].(string); ok && len(y) == 4 {
		return y
	}
	return ""
}

func formatQuantity(qty float64) string {
	return strconv.FormatFloat(qty, 'f', -1, 64)
}

// Transformasi khusus Penerimaan Barang Maximo (RECEIPT / RETURN / VOIDRETURN) -> SAP Grow OData Material Document
// Transformasi khusus Penerimaan Barang Maximo (RECEIPT / RETURN / VOIDRETURN) -> SAP Grow OData Material Document
func (s *SyncService) transformGRN(ctx context.Context, raw json.RawMessage) ([]byte, error) {
	var maxReceipt model.MaximoReceiptHeader
	if err := json.Unmarshal(raw, &maxReceipt); err != nil {
		return nil, fmt.Errorf("invalid json maximo receipt: %w", err)
	}

	issueType := strings.ToUpper(strings.TrimSpace(maxReceipt.IssueType))
	if issueType == "" {
		issueType = "RECEIPT"
	}

	// 1. Kasus VOIDRETURN / VOIDRECEIPT (Cancel GR)
	if issueType == "VOIDRETURN" || issueType == "VOIDRECEIPT" {
		lookupDocNo := strings.TrimSpace(maxReceipt.RefBPIGRNNumber)
		if lookupDocNo == "" || lookupDocNo == "-" {
			lookupDocNo = strings.TrimSpace(maxReceipt.BPIGRNNumber)
		}
		if lookupDocNo == "" || lookupDocNo == "-" {
			lookupDocNo = strings.TrimSpace(maxReceipt.ReceiptRef)
		}

		targetMatDoc := ""
		targetDocYear := ""

		// Coba cari di PostgreSQL core_documents terlebih dahulu (tahun diambil dari respons SAP GR asli)
		if lookupDocNo != "" && lookupDocNo != "-" && s.PGRepo != nil {
			if existingDoc, err := s.PGRepo.GetDocumentByTypeAndNo(ctx, "GRN", lookupDocNo); err == nil && existingDoc != nil && existingDoc.SAPDocumentNo != nil && isSAPMatDoc(*existingDoc.SAPDocumentNo) {
				targetMatDoc = *existingDoc.SAPDocumentNo
				targetDocYear = matDocYearFromResponse(existingDoc.SAPResponse)
			}
		}

		// Jika belum ketemu di local DB, lakukan lookup API ke SAP berdasarkan no doc maximo
		if targetMatDoc == "" && lookupDocNo != "" && lookupDocNo != "-" && s.SAPClient != nil && s.PGRepo != nil {
			ep, epErr := s.PGRepo.GetSAPEndpointByCode(ctx, "GRN")
			if epErr == nil && ep != nil {
				if matDoc, docYear, err := s.SAPClient.GetMaterialDocumentByMaximoDocNo(ep, lookupDocNo); err == nil && matDoc != "" {
					targetMatDoc = matDoc
					targetDocYear = docYear
				}
			}
		}

		// Fallback: Jika pengguna sementara menaruh nomor SAP Material Document di field PONum.
		// Nomor PO biasa TIDAK dipakai sebagai nomor Material Document (cancel akan salah sasaran);
		// bila tetap kosong, Push menandai dokumen error_data.
		if targetMatDoc == "" {
			trimmedPO := strings.TrimSpace(maxReceipt.PONum)
			if isSAPMatDoc(trimmedPO) && strings.HasPrefix(trimmedPO, "500") {
				targetMatDoc = trimmedPO
			} else if trimmedPO != "" && trimmedPO != "-" && s.PGRepo != nil {
				if existingDoc, err := s.PGRepo.GetDocumentByTypeAndNo(ctx, "GRN", trimmedPO); err == nil && existingDoc != nil && existingDoc.SAPDocumentNo != nil && isSAPMatDoc(*existingDoc.SAPDocumentNo) {
					targetMatDoc = *existingDoc.SAPDocumentNo
					targetDocYear = matDocYearFromResponse(existingDoc.SAPResponse)
				}
			}
		}

		// Fallback tahun jika tidak diketahui dari SAP: tahun ActualDate atau tahun sekarang
		if targetDocYear == "" {
			if len(maxReceipt.ActualDate) >= 4 {
				targetDocYear = maxReceipt.ActualDate[:4]
			} else {
				targetDocYear = fmt.Sprintf("%d", time.Now().Year())
			}
		}

		cancelPayload := model.SAPCancelMaterialDocPayload{
			MaterialDocumentYear: targetDocYear,
			MaterialDocument:     targetMatDoc,
		}
		return json.Marshal(cancelPayload)
	}

	headerSiteID := strings.TrimSpace(maxReceipt.SiteID)
	docDateStr, postingDateStr := formatODataV2Date(maxReceipt.ActualDate, true)
	grnPlant, grnPlantMap := s.dv(ctx, "GRN_DEFAULT_PLANT"), s.dv(ctx, "GRN_PLANT_MAP")
	grnMoveCode, grnPrintVer := s.dv(ctx, "GRN_GOODS_MOVEMENT_CODE"), s.dv(ctx, "GRN_PRINT_VERSION")

	// 2. Kasus RETURN (Return to Vendor - Movement 122)
	if issueType == "RETURN" {
		lookupDocNo := strings.TrimSpace(maxReceipt.RefBPIGRNNumber)
		if lookupDocNo == "" || lookupDocNo == "-" {
			lookupDocNo = strings.TrimSpace(maxReceipt.BPIGRNNumber)
		}
		if lookupDocNo == "" || lookupDocNo == "-" {
			lookupDocNo = strings.TrimSpace(maxReceipt.ReceiptRef)
		}

		refMatDoc := ""
		// Coba cari di PostgreSQL core_documents
		if lookupDocNo != "" && lookupDocNo != "-" && s.PGRepo != nil {
			if existingDoc, err := s.PGRepo.GetDocumentByTypeAndNo(ctx, "GRN", lookupDocNo); err == nil && existingDoc != nil && existingDoc.SAPDocumentNo != nil && *existingDoc.SAPDocumentNo != "" {
				refMatDoc = *existingDoc.SAPDocumentNo
			}
		}

		// Jika belum ada di local DB, lookup langsung ke SAP via API berdasarkan no doc maximo
		if refMatDoc == "" && lookupDocNo != "" && lookupDocNo != "-" && s.SAPClient != nil && s.PGRepo != nil {
			ep, epErr := s.PGRepo.GetSAPEndpointByCode(ctx, "GRN")
			if epErr == nil && ep != nil {
				if matDoc, _, err := s.SAPClient.GetMaterialDocumentByMaximoDocNo(ep, lookupDocNo); err == nil && matDoc != "" {
					refMatDoc = matDoc
				}
			}
		}

		// Fallback: Jika lookupDocNo sudah merupakan nomor dokumen SAP (misal 5000000097)
		if refMatDoc == "" && len(lookupDocNo) == 10 && strings.HasPrefix(lookupDocNo, "500") {
			refMatDoc = lookupDocNo
		}

		var items []model.SAPMaterialDocumentItem
		for _, line := range maxReceipt.Lines {
			poNum := line.PONum
			if poNum == "" {
				poNum = maxReceipt.PONum
			}
			lineSite := line.SiteID
			if lineSite == "" {
				lineSite = headerSiteID
			}
			plant := convertSiteIDToPlantMap(lineSite, grnPlant, grnPlantMap)
			sapPONum := ConvertMaximoPOToSAP(poNum)

			// Kuantitas return di Maximo negatif (-1.00), harus positif di SAP
			qtyAbs := math.Abs(line.Qty)
			qtyStr := formatQuantity(qtyAbs)

			items = append(items, model.SAPMaterialDocumentItem{
				Material:                    line.ItemNum,
				Plant:                       plant,
				GoodsMovementType:           s.dv(ctx, "GRN_MOVEMENT_RETURN"),
				PurchaseOrder:               sapPONum,
				PurchaseOrderItem:           fmt.Sprintf("%d", line.POLineNum),
				GoodsMovementReasonCode:     s.dv(ctx, "GRN_RETURN_REASON_CODE"),
				GoodsMovementRefDocType:     "B",
				QuantityInEntryUnit:         qtyStr,
				InvtryMgmtReferenceDocument: refMatDoc,
				MaterialDocumentItemText:    s.dv(ctx, "GRN_RETURN_ITEM_TEXT"),
			})
		}

		sapPayload := model.SAPGrowMaterialDocumentPayload{
			MaterialDocumentHeaderText: maxReceipt.BPIGRNNumber,
			ReferenceDocument:          maxReceipt.PackingSlipNum,
			GoodsMovementCode:          grnMoveCode,
			VersionForPrintingSlip:     grnPrintVer,
			DocumentDate:               docDateStr,
			PostingDate:                postingDateStr,
			ToMaterialDocumentItem:     items,
		}
		return json.Marshal(sapPayload)
	}

	// 3. Kasus Standard RECEIPT (GRN - Movement 101)
	var items []model.SAPMaterialDocumentItem
	for _, line := range maxReceipt.Lines {
		poNum := line.PONum
		if poNum == "" {
			poNum = maxReceipt.PONum
		}
		lineSite := line.SiteID
		if lineSite == "" {
			lineSite = headerSiteID
		}
		plant := convertSiteIDToPlantMap(lineSite, grnPlant, grnPlantMap)
		sapPONum := ConvertMaximoPOToSAP(poNum)

		qtyAbs := math.Abs(line.Qty)
		qtyStr := formatQuantity(qtyAbs)

		items = append(items, model.SAPMaterialDocumentItem{
			Material:                line.ItemNum,
			Plant:                   plant,
			GoodsMovementType:       s.dv(ctx, "GRN_MOVEMENT_RECEIPT"),
			PurchaseOrder:           sapPONum,
			PurchaseOrderItem:       fmt.Sprintf("%d", line.POLineNum),
			GoodsMovementRefDocType: "B",
			QuantityInEntryUnit:     qtyStr,
		})
	}

	sapPayload := model.SAPGrowMaterialDocumentPayload{
		MaterialDocumentHeaderText: maxReceipt.BPIGRNNumber,
		ReferenceDocument:          maxReceipt.PackingSlipNum,
		GoodsMovementCode:          grnMoveCode,
		VersionForPrintingSlip:     grnPrintVer,
		DocumentDate:               docDateStr,
		PostingDate:                postingDateStr,
		ToMaterialDocumentItem:     items,
	}
	return json.Marshal(sapPayload)
}

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
				reason := fmt.Sprintf("Pemetaan tabel '%s' ke SAP sedang NONAKTIF di Table-to-API Mapping Matrix", mapping.SourceTable)
				log.Printf("[SKIP] Dokumen %s (%s) dilewati: %s", doc.DocumentNo, doc.DocType, reason)
				_ = s.PGRepo.MarkDocumentSkipped(ctx, doc.ID, reason)
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
			reason := fmt.Sprintf("Layanan SAP '%s' berstatus NONAKTIF di registry", ep.ServiceCode)
			log.Printf("[SKIP] Dokumen %s (%s) dilewati: %s", doc.DocumentNo, doc.DocType, reason)
			_ = s.PGRepo.MarkDocumentSkipped(ctx, doc.ID, reason)
			continue
		}

		// 2. Eksekusi request ke SAP Grow OData dengan auth & token handshake terisolasi
		epToSend := ep
		payloadToSend := doc.SAPPayload

		// Perlakuan khusus jika GRN adalah VOIDRETURN / VOIDRECEIPT (Cancel function import)
		if strings.EqualFold(doc.DocType, "GRN") {
			isCancelDoc := false
			var rawMap map[string]interface{}
			if rErr := json.Unmarshal(doc.RawPayload, &rawMap); rErr == nil {
				it, _ := rawMap["ISSUETYPE"].(string)
				if it == "" {
					it, _ = rawMap["issuetype"].(string)
				}
				if strings.EqualFold(it, "VOIDRECEIPT") || strings.EqualFold(it, "VOIDRETURN") {
					isCancelDoc = true
				}
			}

			// Juga cek apakah payloadnya adalah SAPCancelMaterialDocPayload (memiliki MaterialDocument dan tanpa to_MaterialDocumentItem)
			if !isCancelDoc && bytes.Contains(doc.SAPPayload, []byte(`"MaterialDocument"`)) && !bytes.Contains(doc.SAPPayload, []byte(`"to_MaterialDocumentItem"`)) {
				isCancelDoc = true
			}

			if isCancelDoc {
				var cancelParam model.SAPCancelMaterialDocPayload
				_ = json.Unmarshal(doc.SAPPayload, &cancelParam)
				targetMatDoc := strings.TrimSpace(cancelParam.MaterialDocument)
				targetDocYear := strings.TrimSpace(cancelParam.MaterialDocumentYear)

				// Fallback jika MaterialDocument di SAPPayload masih kosong, coba ambil dari raw_payload (REF_BPIGRNUMBER atau PONUM 500xxx)
				// (hanya bila nilainya memang berbentuk nomor Material Document SAP).
				if targetMatDoc == "" && rawMap != nil {
					if r, ok := rawMap["REF_BPIGRNUMBER"].(string); ok && isSAPMatDoc(r) {
						targetMatDoc = strings.TrimSpace(r)
					} else if p, ok := rawMap["PONUM"].(string); ok && isSAPMatDoc(p) && strings.HasPrefix(strings.TrimSpace(p), "500") {
						targetMatDoc = strings.TrimSpace(p)
					}
				}
				if targetDocYear == "" {
					targetDocYear = fmt.Sprintf("%d", time.Now().Year())
				}

				// Tanpa nomor Material Document SAP yang valid, Cancel tidak dikirim (sebelumnya payload cancel
				// ter-POST ke A_MaterialDocumentHeader dan pasti ditolak SAP).
				if !isSAPMatDoc(targetMatDoc) || !isYear(targetDocYear) {
					reason := fmt.Sprintf("Cancel GRN %s: nomor Material Document SAP tidak ditemukan/valid (doc=%q, tahun=%q)", doc.DocumentNo, targetMatDoc, targetDocYear)
					log.Printf("[WARN] %s", reason)
					_ = s.PGRepo.UpdateSAPError(ctx, doc.ID, "error_data", reason, nil)
					_ = s.PGRepo.InsertIntegrationLog(ctx, doc.GroupID, "MIDDLEWARE", "SAP GROW", doc.DocType, doc.DocumentNo, "FAILED", reason, reason)
					continue
				}
				{
					// Root layanan Material Document diambil dari endpoint_path endpoint GRN.
					cancelEP := *ep
					cancelPath, pErr := client.ServicePath(ep, fmt.Sprintf("Cancel?MaterialDocumentYear='%s'&MaterialDocument='%s'", targetDocYear, targetMatDoc))
					csrfPath, cErr := client.ServicePath(ep, "A_MaterialDocumentHeader")
					if pErr != nil || cErr != nil {
						_ = s.PGRepo.UpdateSAPError(ctx, doc.ID, "error_data", fmt.Sprintf("Path Cancel GRN tidak bisa disusun: %v", firstErr(pErr, cErr)), nil)
						continue
					}
					cancelEP.EndpointPath = cancelPath
					cancelEP.CSRFPath = csrfPath
					epToSend = &cancelEP
					payloadToSend = nil
				}
			}
		}

		// Validasi perubahan PO (PO Change Validation) jika target service atau tipe dokumen adalah perubahan PO
		if strings.HasPrefix(strings.ToUpper(targetService), "PO_CHANGE") || strings.HasPrefix(strings.ToUpper(doc.DocType), "PO_CHANGE") {
			canChange, reason, vErr := s.ValidatePOChange(ctx, doc.DocumentNo)
			if vErr != nil {
				log.Printf("[WARN] Validasi PO Change gagal untuk %s: %v", doc.DocumentNo, vErr)
				_ = s.PGRepo.UpdateSAPError(ctx, doc.ID, "error_conn", fmt.Sprintf("Gagal validasi status Material Document di SAP: %v", vErr), nil)
				continue
			}
			if !canChange {
				log.Printf("[BLOCK] Perubahan PO %s dibatalkan: %s", doc.DocumentNo, reason)
				_ = s.PGRepo.UpdateSAPError(ctx, doc.ID, "error_data", fmt.Sprintf("PO Change Ditolak: %s", reason), nil)
				errMsg := fmt.Sprintf("Perubahan PO ditolak oleh middleware: %s", reason)
				_ = s.PGRepo.InsertIntegrationLog(ctx, doc.GroupID, "MIDDLEWARE", "SAP GROW", doc.DocType, doc.DocumentNo, "BLOCKED", reason, errMsg)
				continue
			}
		}

		httpMethod := "POST"
		if strings.Contains(strings.ToUpper(epToSend.ServiceName), "PATCH") || strings.Contains(strings.ToUpper(epToSend.ServiceCode), "CHANGE") {
			httpMethod = "PATCH"
			// PATCH OData v4 harus ke entity ber-key, bukan ke koleksi (.../PurchaseOrder -> 405/400).
			suffix, kErr := client.PatchKeySuffix(epToSend.EndpointPath, payloadToSend)
			if kErr != nil {
				_ = s.PGRepo.UpdateSAPError(ctx, doc.ID, "error_data", kErr.Error(), nil)
				_ = s.PGRepo.InsertIntegrationLog(ctx, doc.GroupID, "MIDDLEWARE", "SAP GROW", doc.DocType, doc.DocumentNo, "FAILED", kErr.Error(), kErr.Error())
				continue
			}
			if suffix != "" {
				patchEP := *epToSend
				patchEP.EndpointPath = strings.TrimRight(patchEP.EndpointPath, "/") + suffix
				if patchEP.CSRFPath == "" {
					patchEP.CSRFPath = epToSend.EndpointPath
				}
				epToSend = &patchEP
			}
		}

		var statusCode int
		var sapDocNo, respBody string
		var sapErr error
		if strings.EqualFold(epToSend.ODataVersion, "soap") {
			statusCode, sapDocNo, respBody, sapErr = s.executeSOAPDocument(epToSend, payloadToSend)
		} else {
			statusCode, sapDocNo, respBody, sapErr = s.SAPClient.ExecuteOData(epToSend, httpMethod, payloadToSend)
		}
		targetURL := fmt.Sprintf("%s%s", strings.TrimRight(epToSend.BaseURL, "/"), epToSend.EndpointPath)

		if sapErr != nil {
			log.Printf("[WARN] Push to SAP Failed for %s (%s) [Target API: %s]: %v", doc.DocumentNo, doc.DocType, targetURL, sapErr)
			statusType := "error_data"
			if statusCode == 0 {
				statusType = "error_conn"
			}
			_ = s.PGRepo.UpdateSAPError(ctx, doc.ID, statusType, sapErr.Error(), toJSONResponse(respBody))
			errMsg := fmt.Sprintf("Failed sending to SAP Grow via %s [Target API: %s]: %s", ep.ServiceCode, targetURL, sapErr.Error())
			_ = s.PGRepo.InsertIntegrationLog(ctx, doc.GroupID, "MIDDLEWARE", "SAP GROW", doc.DocType, doc.DocumentNo, "FAILED", sapErr.Error(), errMsg)
			if statusType == "error_data" {
				s.afterPushRejected(ctx, doc, sapErr.Error())
			}
		} else {
			_ = s.PGRepo.UpdateSAPSuccess(ctx, doc.ID, sapDocNo, toJSONResponse(respBody))
			s.afterPushSuccess(ctx, doc, sapDocNo)
			successMsg := fmt.Sprintf("Successfully posted to SAP Grow via %s [Target API: %s] (SAP Doc: %s)", ep.ServiceCode, targetURL, sapDocNo)
			_ = s.PGRepo.InsertIntegrationLog(ctx, doc.GroupID, "MIDDLEWARE", "SAP GROW", doc.DocType, doc.DocumentNo, "SUCCESS", "", successMsg)
			pushedCount++
			log.Printf("[INFO] Push to SAP Success: %s -> %s [Target API: %s]", doc.DocumentNo, sapDocNo, targetURL)

			// Second Post: Khusus Item Master (A_Product), trigger A_ProductProcurement jika ada PurchaseOrderQuantityUnit
			if strings.EqualFold(targetService, "PRODUCT") || strings.EqualFold(doc.DocType, "ITEM") || strings.EqualFold(doc.DocType, "PRODUCT") {
				var prodMap map[string]interface{}
				if err := json.Unmarshal(doc.SAPPayload, &prodMap); err == nil {
					poUnit, _ := prodMap["PurchaseOrderQuantityUnit"].(string)
					prodID, _ := prodMap["Product"].(string)
					if prodID == "" {
						prodID = sapDocNo
					}
					procPath, pErr := client.ServicePath(ep, "A_ProductProcurement")
					csrfPath, cErr := client.ServicePath(ep, "A_Product")
					if pErr != nil || cErr != nil {
						log.Printf("[WARN] Sub-request A_ProductProcurement dilewati untuk Product %s: %v", prodID, firstErr(pErr, cErr))
					} else if poUnit != "" && prodID != "" {
						// Root layanan Product diambil dari endpoint_path endpoint PRODUCT.
						procEP := *ep
						procEP.EndpointPath = procPath
						procEP.CSRFPath = csrfPath
						procPayload := model.SAPProductProcurementPayload{
							Product:                   prodID,
							PurchaseOrderQuantityUnit: poUnit,
							VarblPurOrdUnitStatus:     "1",
						}
						if procBytes, procJsonErr := json.Marshal(procPayload); procJsonErr == nil {
							_, _, procRespBody, procErr := s.SAPClient.ExecuteOData(&procEP, "POST", procBytes)
							if procErr != nil {
								log.Printf("[WARN] Sub-request A_ProductProcurement gagal untuk Product %s: %v (Response: %s)", prodID, procErr, procRespBody)
							} else {
								log.Printf("[INFO] Variable PO unit successfully configured for Product %s: Unit %s", prodID, poUnit)
								_ = s.PGRepo.InsertIntegrationLog(ctx, doc.GroupID, "MIDDLEWARE", "SAP GROW", "PRODUCT_PROCUREMENT", doc.DocumentNo, "SUCCESS", "", fmt.Sprintf("Variable PO Unit '%s' activated via A_ProductProcurement", poUnit))
							}
						}
					}
				}
			}
		}
	}
	return pushedCount
}
