package service

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang-bpi-testing/internal/client"
	"golang-bpi-testing/internal/model"
)

const (
	pullPageSize = 500
	pullMaxPages = 20

	syncIDGL      = "RICEF3_GL_LINE_ITEM"
	syncIDPayment = "RICEF5_PAYMENT_PROPOSAL"
	syncIDAsset   = "RICEF9_FIXED_ASSET"
)

// odataQueryEscape meng-escape nilai query OData; spasi menjadi %20 (bukan '+').
func odataQueryEscape(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

// odataLiteral meng-escape tanda kutip tunggal di dalam literal string OData.
func odataLiteral(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

var odataV2DateRe = regexp.MustCompile(`/Date\((-?\d+)`)

// parseODataDate mengubah tanggal OData V2 (/Date(ms)/) atau ISO (V4) menjadi YYYY-MM-DD.
func parseODataDate(v interface{}) *string {
	s, ok := v.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return nil
	}
	if m := odataV2DateRe.FindStringSubmatch(s); m != nil {
		ms, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil {
			return nil
		}
		d := time.UnixMilli(ms).UTC().Format("2006-01-02")
		return &d
	}
	if len(s) >= 10 {
		if _, err := time.Parse("2006-01-02", s[:10]); err == nil {
			d := s[:10]
			return &d
		}
	}
	return nil
}

// parseODataNumber membaca angka OData (V2 mengirim Edm.Decimal sebagai string).
func parseODataNumber(v interface{}) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return f
	case json.Number:
		f, _ := t.Float64()
		return f
	}
	return 0
}

func str(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			switch t := v.(type) {
			case string:
				if strings.TrimSpace(t) != "" {
					return strings.TrimSpace(t)
				}
			case float64:
				return strconv.FormatFloat(t, 'f', -1, 64)
			}
		}
	}
	return ""
}

func firstPresent(m map[string]interface{}, keys ...string) interface{} {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			return v
		}
	}
	return nil
}

// odataRecords mengambil daftar record dari response V2 (d.results / d) atau V4 (value).
func odataRecords(body []byte) ([]map[string]interface{}, error) {
	var root map[string]interface{}
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("response OData bukan JSON valid: %w", err)
	}
	toList := func(v interface{}) []map[string]interface{} {
		arr, _ := v.([]interface{})
		out := make([]map[string]interface{}, 0, len(arr))
		for _, it := range arr {
			if m, ok := it.(map[string]interface{}); ok {
				out = append(out, m)
			}
		}
		return out
	}
	if d, ok := root["d"].(map[string]interface{}); ok {
		if res, ok := d["results"]; ok {
			return toList(res), nil
		}
		return []map[string]interface{}{d}, nil
	}
	if v, ok := root["value"]; ok {
		return toList(v), nil
	}
	return nil, fmt.Errorf("format response OData tidak dikenali")
}

// rawRecord menyimpan record tanpa __metadata dan navigasi deferred.
func rawRecord(m map[string]interface{}) json.RawMessage {
	clean := make(map[string]interface{}, len(m))
	for k, v := range m {
		if k == "__metadata" {
			continue
		}
		if nested, ok := v.(map[string]interface{}); ok {
			if _, deferred := nested["__deferred"]; deferred {
				continue
			}
		}
		clean[k] = v
	}
	b, _ := json.Marshal(clean)
	return b
}

func metadataURI(m map[string]interface{}) string {
	if meta, ok := m["__metadata"].(map[string]interface{}); ok {
		if uri, ok := meta["uri"].(string); ok {
			return uri
		}
	}
	return ""
}

var glKeyRe = regexp.MustCompile(`GLAccountLineItem\('([^']*(?:''[^']*)*)'\)`)

// ParseGLLineItems memetakan response API_GLACCOUNTLINEITEM ke model.
func ParseGLLineItems(body []byte) ([]model.GLLineItem, error) {
	recs, err := odataRecords(body)
	if err != nil {
		return nil, err
	}
	out := make([]model.GLLineItem, 0, len(recs))
	for _, m := range recs {
		key := ""
		if mm := glKeyRe.FindStringSubmatch(metadataURI(m)); mm != nil {
			if k, err := url.PathUnescape(mm[1]); err == nil {
				key = strings.ReplaceAll(k, "''", "'")
			}
		}
		if key == "" {
			key = str(m, "GLAccountLineItem", "GLAccountLineItemUniqueID")
		}
		if key == "" {
			key = strings.Join([]string{str(m, "SourceLedger", "Ledger"), str(m, "CompanyCode"), str(m, "FiscalYear"),
				str(m, "AccountingDocument"), str(m, "LedgerGLLineItem")}, "~")
		}
		out = append(out, model.GLLineItem{
			Key:                         key,
			CompanyCode:                 str(m, "CompanyCode"),
			FiscalYear:                  str(m, "FiscalYear"),
			AccountingDocument:          str(m, "AccountingDocument"),
			LedgerGLLineItem:            str(m, "LedgerGLLineItem"),
			AccountingDocumentType:      str(m, "AccountingDocumentType"),
			PostingDate:                 parseODataDate(m["PostingDate"]),
			DocumentDate:                parseODataDate(m["DocumentDate"]),
			GLAccount:                   str(m, "GLAccount"),
			CostCenter:                  str(m, "CostCenter"),
			ProfitCenter:                str(m, "ProfitCenter"),
			Supplier:                    str(m, "Supplier"),
			DebitCreditCode:             str(m, "DebitCreditCode"),
			AmountInCompanyCodeCurrency: parseODataNumber(m["AmountInCompanyCodeCurrency"]),
			CompanyCodeCurrency:         str(m, "CompanyCodeCurrency"),
			AmountInTransactionCurrency: parseODataNumber(m["AmountInTransactionCurrency"]),
			TransactionCurrency:         str(m, "TransactionCurrency"),
			DocumentItemText:            str(m, "DocumentItemText"),
			Raw:                         rawRecord(m),
		})
	}
	return out, nil
}

// ParsePaymentProposals memetakan CDS custom YY1_PaymentProposal. Nama field belum final, sehingga
// kolom terstruktur diisi dari kandidat nama umum; record utuh tetap disimpan di raw.
func ParsePaymentProposals(body []byte) ([]model.PaymentProposal, error) {
	recs, err := odataRecords(body)
	if err != nil {
		return nil, err
	}
	out := make([]model.PaymentProposal, 0, len(recs))
	entitySet := paymentEntitySet()
	fields := map[string][]string{}
	f := func(name string) []string {
		if _, ok := fields[name]; !ok {
			fields[name] = sapFieldKeys("PAYMENT", name)
		}
		return fields[name]
	}
	for _, m := range recs {
		raw := rawRecord(m)
		key := metadataURI(m)
		if key == "" {
			sum := sha1.Sum(raw)
			key = hex.EncodeToString(sum[:])
		} else if i := strings.Index(key, entitySet); i >= 0 {
			key = key[i:]
		}
		if len(key) > 400 {
			sum := sha1.Sum([]byte(key))
			key = hex.EncodeToString(sum[:])
		}
		// Kandidat field: FIN_PAYMENT_FIELD_<NAMA> (finance_fieldmap.go).
		out = append(out, model.PaymentProposal{
			RecordKey:       key,
			PaymentRunID:    str(m, f("RUN_ID")...),
			PaymentRunDate:  parseODataDate(firstPresent(m, f("RUN_DATE")...)),
			CompanyCode:     str(m, f("COMPANY_CODE")...),
			Supplier:        str(m, f("SUPPLIER")...),
			PaymentDocument: str(m, f("PAYMENT_DOCUMENT")...),
			Amount:          parseODataNumber(firstPresent(m, f("AMOUNT")...)),
			Currency:        str(m, f("CURRENCY")...),
			Raw:             raw,
		})
	}
	return out, nil
}

// ParseFixedAssets memetakan response V4 api_fixedasset.
func ParseFixedAssets(body []byte) ([]model.FixedAsset, error) {
	recs, err := odataRecords(body)
	if err != nil {
		return nil, err
	}
	out := make([]model.FixedAsset, 0, len(recs))
	descKeys, addKeys, capKeys := sapFieldKeys("ASSET", "DESCRIPTION"), sapFieldKeys("ASSET", "ADDITIONAL_DESC"), sapFieldKeys("ASSET", "CAPITALIZATION_DATE")
	for _, m := range recs {
		a := model.FixedAsset{
			CompanyCode:        str(m, "CompanyCode"),
			MasterFixedAsset:   str(m, "MasterFixedAsset"),
			FixedAsset:         str(m, "FixedAsset"),
			Description:        str(m, descKeys...),
			AdditionalDesc:     str(m, addKeys...),
			AssetClass:         str(m, "AssetClass"),
			CapitalizationDate: parseODataDate(firstPresent(m, capKeys...)),
			Raw:                rawRecord(m),
		}
		if a.FixedAsset == "" {
			a.FixedAsset = "0"
		}
		if a.CompanyCode == "" || a.MasterFixedAsset == "" {
			continue
		}
		out = append(out, a)
	}
	return out, nil
}

// GLSyncOptions adalah parameter tarik GL line item.
type GLSyncOptions struct {
	CompanyCode string
	FromDate    string // YYYY-MM-DD, kosong = dari sinkron terakhir
	ToDate      string // YYYY-MM-DD, kosong = hari ini
	DocTypes    []string
}

// BuildGLFilter menyusun $filter tarikan GL line item.
func BuildGLFilter(o GLSyncOptions) string {
	parts := []string{
		fmt.Sprintf("CompanyCode eq '%s'", odataLiteral(o.CompanyCode)),
		"Ledger eq '0L'",
		fmt.Sprintf("PostingDate ge datetime'%sT00:00:00'", o.FromDate),
		fmt.Sprintf("PostingDate le datetime'%sT23:59:59'", o.ToDate),
	}
	if len(o.DocTypes) > 0 {
		var ors []string
		for _, t := range o.DocTypes {
			ors = append(ors, fmt.Sprintf("AccountingDocumentType eq '%s'", odataLiteral(t)))
		}
		parts = append(parts, "("+strings.Join(ors, " or ")+")")
	}
	return strings.Join(parts, " and ")
}

// DocTypesFor mengembalikan tipe dokumen akuntansi untuk kategori RICEF 3 (journal) / 4 (invoice).
func DocTypesFor(category string) []string {
	split := func(s string) []string {
		var out []string
		for _, p := range strings.Split(s, ",") {
			if t := strings.ToUpper(strings.TrimSpace(p)); t != "" {
				out = append(out, t)
			}
		}
		return out
	}
	switch strings.ToLower(category) {
	case "journal":
		return split(envOr("FIN_JE_DOC_TYPES", "SA"))
	case "invoice":
		return split(envOr("FIN_INVOICE_DOC_TYPES", "RE,KR"))
	}
	return []string{}
}

// pullPages menjalankan GET berhalaman dan memanggil handle untuk setiap halaman.
func (f *FinanceService) pullPages(ctx context.Context, serviceCode, filter string, handle func(body []byte) (int, int, error)) model.SyncResult {
	return f.pullPagesQ(ctx, serviceCode, filter, "", handle)
}

// pullPagesQ sama dengan pullPages dengan tambahan query (mis. "&$expand=...").
func (f *FinanceService) pullPagesQ(ctx context.Context, serviceCode, filter, extraQuery string, handle func(body []byte) (int, int, error)) model.SyncResult {
	res := model.SyncResult{Target: serviceCode}
	ep, err := f.PGRepo.GetSAPEndpointByCode(ctx, serviceCode)
	if err != nil || ep == nil {
		res.Error = fmt.Sprintf("endpoint SAP '%s' tidak terdaftar", serviceCode)
		return res
	}
	if !ep.IsActive {
		res.Error = fmt.Sprintf("endpoint SAP '%s' nonaktif", serviceCode)
		return res
	}
	for page := 0; page < pullMaxPages; page++ {
		q := fmt.Sprintf("?$top=%d&$skip=%d", pullPageSize, page*pullPageSize)
		if filter != "" {
			q += "&$filter=" + odataQueryEscape(filter)
		}
		q += extraQuery
		_, body, err := f.SAPClient.GetOData(ep, ep.EndpointPath+q)
		if err != nil {
			res.Error = err.Error()
			return res
		}
		fetched, upserted, err := handle(body)
		res.Pages++
		res.Fetched += fetched
		res.Upserted += upserted
		if err != nil {
			res.Error = err.Error()
			return res
		}
		if fetched < pullPageSize {
			break
		}
	}
	return res
}

// SyncGLLineItems menarik GL line item (RICEF 3 & 4) secara inkremental per tanggal posting.
func (f *FinanceService) SyncGLLineItems(ctx context.Context, o GLSyncOptions) model.SyncResult {
	if o.CompanyCode == "" {
		o.CompanyCode = DefaultCompanyCode()
	}
	if o.ToDate == "" {
		o.ToDate = time.Now().Format("2006-01-02")
	}
	if o.FromDate == "" {
		o.FromDate = f.PGRepo.SyncWindowStart(ctx, syncIDGL, 24*time.Hour, 30*24*time.Hour).Format("2006-01-02")
	}
	if len(o.DocTypes) == 0 {
		o.DocTypes = append(DocTypesFor("journal"), DocTypesFor("invoice")...)
	}
	res := f.pullPages(ctx, model.ServiceGLLineItems, BuildGLFilter(o), func(body []byte) (int, int, error) {
		items, err := ParseGLLineItems(body)
		if err != nil {
			return 0, 0, err
		}
		n, err := f.PGRepo.UpsertGLLineItems(ctx, items)
		return len(items), n, err
	})
	res.Target = "gl"
	if res.Error == "" {
		_ = f.PGRepo.UpdateLastSync(ctx, syncIDGL, time.Now())
	}
	return res
}

// SyncPaymentProposals menarik proposal pembayaran (RICEF 5). filter = $filter OData opsional.
func (f *FinanceService) SyncPaymentProposals(ctx context.Context, filter string) model.SyncResult {
	res := f.pullPages(ctx, model.ServicePaymentProposal, filter, func(body []byte) (int, int, error) {
		items, err := ParsePaymentProposals(body)
		if err != nil {
			return 0, 0, err
		}
		n, err := f.PGRepo.UpsertPaymentProposals(ctx, items)
		return len(items), n, err
	})
	res.Target = "payment"
	if res.Error == "" {
		_ = f.PGRepo.UpdateLastSync(ctx, syncIDPayment, time.Now())
	}
	return res
}

// SyncFixedAssets menarik master aset tetap (RICEF 9) untuk satu company code.
func (f *FinanceService) SyncFixedAssets(ctx context.Context, companyCode string) model.SyncResult {
	if companyCode == "" {
		companyCode = DefaultCompanyCode()
	}
	filter := fmt.Sprintf("CompanyCode eq '%s'", odataLiteral(companyCode))
	res := f.pullPages(ctx, model.ServiceFixedAsset, filter, func(body []byte) (int, int, error) {
		items, err := ParseFixedAssets(body)
		if err != nil {
			return 0, 0, err
		}
		n, err := f.PGRepo.UpsertFixedAssets(ctx, items)
		return len(items), n, err
	})
	res.Target = "asset"
	if res.Error == "" {
		_ = f.PGRepo.UpdateLastSync(ctx, syncIDAsset, time.Now())
	}
	return res
}

// SyncAll menjalankan seluruh tarikan finance; kegagalan satu target tidak menghentikan yang lain.
func (f *FinanceService) SyncAll(ctx context.Context) []model.SyncResult {
	results := []model.SyncResult{
		f.SyncGLLineItems(ctx, GLSyncOptions{}),
		f.SyncPaymentProposals(ctx, ""),
		f.SyncFixedAssets(ctx, ""),
	}
	for _, r := range results {
		if r.Error != "" {
			log.Printf("[WORKER-FINANCE] Tarik %s gagal: %s", r.Target, r.Error)
		} else if r.Upserted > 0 {
			log.Printf("[WORKER-FINANCE] Tarik %s: %d record disimpan (%d halaman)", r.Target, r.Upserted, r.Pages)
		}
	}
	return results
}

// GetGLLineItemDetail mengambil satu GL line item langsung dari SAP berdasarkan composite key.
func (f *FinanceService) GetGLLineItemDetail(ctx context.Context, key string) (json.RawMessage, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, newValidation("key wajib diisi")
	}
	ep, err := f.PGRepo.GetSAPEndpointByCode(ctx, model.ServiceGLLineItems)
	if err != nil || ep == nil {
		return nil, fmt.Errorf("endpoint SAP '%s' tidak terdaftar", model.ServiceGLLineItems)
	}
	path := fmt.Sprintf("%s('%s')", ep.EndpointPath, url.PathEscape(odataLiteral(key)))
	_, body, err := f.SAPClient.GetOData(ep, path)
	if err != nil {
		return nil, err
	}
	return body, nil
}

// GetFixedAssetDetail mengambil satu aset tetap langsung dari SAP.
func (f *FinanceService) GetFixedAssetDetail(ctx context.Context, companyCode, masterFixedAsset, fixedAsset string) (json.RawMessage, error) {
	if companyCode == "" {
		companyCode = DefaultCompanyCode()
	}
	if fixedAsset == "" {
		fixedAsset = "0"
	}
	if strings.TrimSpace(masterFixedAsset) == "" {
		return nil, newValidation("master_fixed_asset wajib diisi")
	}
	ep, err := f.PGRepo.GetSAPEndpointByCode(ctx, model.ServiceFixedAsset)
	if err != nil || ep == nil {
		return nil, fmt.Errorf("endpoint SAP '%s' tidak terdaftar", model.ServiceFixedAsset)
	}
	path := fmt.Sprintf("%s(CompanyCode='%s',MasterFixedAsset='%s',FixedAsset='%s')", ep.EndpointPath,
		url.PathEscape(odataLiteral(companyCode)), url.PathEscape(odataLiteral(masterFixedAsset)), url.PathEscape(odataLiteral(fixedAsset)))
	_, body, err := f.SAPClient.GetOData(ep, path)
	if err != nil {
		return nil, err
	}
	return body, nil
}

// SyncVendorMasterFromSAP menarik Master Data Vendor dari SAP OData API_BUSINESS_PARTNER
// sesuai spesifikasi Excel sheet "Outbound Master Data Vendor" (Address, Email, dan Bank).
func (f *FinanceService) SyncVendorMasterFromSAP(ctx context.Context) model.SyncResult {
	res := model.SyncResult{Target: "vendor_master"}
	ep, err := f.PGRepo.GetSAPEndpointByCode(ctx, "BP")
	if err != nil || ep == nil {
		res.Error = "endpoint SAP 'BP' (Business Partner) tidak terdaftar"
		return res
	}
	if !ep.IsActive {
		res.Error = "endpoint SAP 'BP' nonaktif"
		return res
	}

	// Jika mode simulasi (MOCK), gunakan sample data dari Excel sheet Outbound Master Data Vendor
	if client.IsMock(ep) {
		mockVendors := []model.EmployeeVendorMapping{
			{
				VendorCode:    "VND-0009",
				CIPFlag:       "CIP",
				Email:         "account.payable01@supplier.com",
				BankKey:       "Mandiri",
				BankAccount:   "140001928374",
				AccountHolder: "PT Dwijaya Perkasa",
				Description:   "Outbound SAP Vendor (Sample)",
				IsActive:      true,
			},
			{
				VendorCode:    "VND-0035",
				CIPFlag:       "NON_CIP",
				Email:         "billing@megatech.co.id",
				BankKey:       "BCA",
				BankAccount:   "8830192847",
				AccountHolder: "PT Megatech Solusindo",
				Description:   "Outbound SAP Vendor (Sample)",
				IsActive:      true,
			},
			{
				VendorCode:    "VND-0061",
				CIPFlag:       "CIP",
				Email:         "finance@andalankita.co.id",
				BankKey:       "BNI",
				BankAccount:   "0928374615",
				AccountHolder: "CV Andalan Mandiri",
				Description:   "Outbound SAP Vendor (Sample)",
				IsActive:      true,
			},
			{
				VendorCode:    "VND-0063",
				CIPFlag:       "NON_CIP",
				Email:         "contact@primasinergi.com",
				BankKey:       "BRI",
				BankAccount:   "034101002938",
				AccountHolder: "PT Prima Sinergi",
				Description:   "Outbound SAP Vendor (Sample)",
				IsActive:      true,
			},
		}
		n, err := f.PGRepo.UpsertEmployeeVendors(ctx, mockVendors)
		if err != nil {
			res.Error = err.Error()
			return res
		}
		res.Fetched = len(mockVendors)
		res.Upserted = n
		res.Pages = 1
		return res
	}

	// Live mode: tarik A_BusinessPartnerAddress dengan expand to_EmailAddress
	// Link 1 & 2 di Excel: <root endpoint_path BP>/A_BusinessPartnerAddress?$format=json
	addrPath, err := client.ServicePath(ep, "A_BusinessPartnerAddress?$expand=to_EmailAddress&$format=json&$top=500")
	if err != nil {
		res.Error = err.Error()
		return res
	}
	bankKeyFields, bankAccFields, holderFields := sapFieldKeys("VENDOR", "BANK_KEY"), sapFieldKeys("VENDOR", "BANK_ACCOUNT"), sapFieldKeys("VENDOR", "ACCOUNT_HOLDER")
	_, body, err := f.SAPClient.GetOData(ep, addrPath)
	if err != nil {
		res.Error = fmt.Sprintf("gagal mengambil A_BusinessPartnerAddress dari SAP: %v", err)
		return res
	}

	recs, err := odataRecords(body)
	if err != nil {
		res.Error = fmt.Sprintf("format data SAP tidak valid: %v", err)
		return res
	}

	var items []model.EmployeeVendorMapping
	for _, m := range recs {
		vendorCode := str(m, "BusinessPartner")
		if vendorCode == "" {
			continue
		}
		extID := strings.ToUpper(str(m, "AddressIDByExternalSystem"))
		cipFlag := "NON_CIP"
		if strings.Contains(extID, "CIP") && !strings.Contains(extID, "NON") {
			cipFlag = "CIP"
		}

		// Email dari to_EmailAddress
		email := ""
		if toEmail, ok := m["to_EmailAddress"].(map[string]interface{}); ok {
			if results, ok := toEmail["results"].([]interface{}); ok && len(results) > 0 {
				if em, ok := results[0].(map[string]interface{}); ok {
					email = str(em, "EmailAddress")
				}
			} else {
				email = str(toEmail, "EmailAddress")
			}
		}

		// Tarik data Bank dari A_BusinessPartner('{Vendor}')/to_BusinessPartnerBank?$format=json (Link 3 di Excel)
		bankKey := ""
		bankAccount := ""
		accHolder := ""
		bankPath, _ := client.ServicePath(ep, fmt.Sprintf("A_BusinessPartner('%s')/to_BusinessPartnerBank?$format=json", odataLiteral(vendorCode)))
		if _, bankBody, bErr := f.SAPClient.GetOData(ep, bankPath); bErr == nil {
			if bRecs, bParseErr := odataRecords(bankBody); bParseErr == nil && len(bRecs) > 0 {
				bm := bRecs[0]
				bankKey = str(bm, bankKeyFields...)
				bankAccount = str(bm, bankAccFields...)
				accHolder = str(bm, holderFields...)
			}
		}

		items = append(items, model.EmployeeVendorMapping{
			VendorCode:    vendorCode,
			CIPFlag:       cipFlag,
			Email:         email,
			BankKey:       bankKey,
			BankAccount:   bankAccount,
			AccountHolder: accHolder,
			Description:   "Tersinkronisasi dari SAP OData",
			IsActive:      true,
		})
	}

	if len(items) > 0 {
		n, err := f.PGRepo.UpsertEmployeeVendors(ctx, items)
		if err != nil {
			res.Error = err.Error()
			return res
		}
		res.Upserted = n
	}
	res.Fetched = len(items)
	res.Pages = 1
	return res
}
