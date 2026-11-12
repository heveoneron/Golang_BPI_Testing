package repository

import (
	"context"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

// DefaultValueSpec adalah satu nilai default (dulu hardcode di transform) yang bisa diubah dari
// Panel > Configuration > Default Value. Value di sini hanya nilai awal saat seed.
type DefaultValueSpec struct {
	Key         string
	Value       string
	Category    string
	Description string
	// SeedEnv: env lama yang, bila diisi, dipakai sebagai nilai seed (kompatibel dengan .env lama).
	SeedEnv string
}

// DefaultValueSpecs adalah daftar nilai default beserta nilai awalnya (= perilaku lama).
var DefaultValueSpecs = []DefaultValueSpec{
	// Purchase Order (API_PURCHASEORDER_2)
	{Key: "PO_COMPANY_CODE", Value: "5110", Category: "PO", Description: "CompanyCode header PO"},
	{Key: "PO_PURCHASING_ORG", Value: "5110", Category: "PO", Description: "PurchasingOrganization header PO"},
	{Key: "PO_PURCHASING_GROUP", Value: "001", Category: "PO", Description: "PurchasingGroup header PO"},
	{Key: "PO_TYPE_DEFAULT", Value: "ZBPI", Category: "PO", Description: "PurchaseOrderType bila nomor PO bukan prefix 45/47"},
	{Key: "PO_TYPE_PREFIX_45", Value: "ZPRC", Category: "PO", Description: "PurchaseOrderType untuk PO Maximo prefix 45"},
	{Key: "PO_TYPE_PREFIX_47", Value: "ZCTR", Category: "PO", Description: "PurchaseOrderType untuk PO Maximo prefix 47"},
	{Key: "PO_DEFAULT_CURRENCY", Value: "IDR", Category: "PO", Description: "DocumentCurrency bila CURRENCYCODE Maximo kosong"},
	{Key: "PO_DEFAULT_PLANT", Value: "5110", Category: "PO", Description: "Plant bila SITEID baris & header kosong"},
	{Key: "PO_DEFAULT_COST_CENTER", Value: "51101101", Category: "PO", Description: "CostCenter account assignment bila kosong. Kosongkan = dokumen ditolak (error data)"},
	{Key: "PO_DEFAULT_MATERIAL", Value: "SP006", Category: "PO", Description: "Material bila ITEMNUM baris kosong. Kosongkan = dokumen ditolak (error data)"},
	{Key: "PO_DEFAULT_UOM", Value: "PC", Category: "PO", Description: "Satuan order bila ORDERUNIT kosong"},
	{Key: "PO_ACCOUNT_ASSIGNMENT_CATEGORY", Value: "K", Category: "PO", Description: "AccountAssignmentCategory item (K = cost center)"},
	{Key: "PO_NO_TAX_CODE", Value: "V0", Category: "PO", Description: "TaxCode bila baris tanpa pajak (PPN 0)"},
	{Key: "PO_TAX_CONDITION_TYPE", Value: "DRG1", Category: "PO", Description: "ConditionType pajak (persen)"},
	{Key: "PO_PRICE_CONDITION_TYPE", Value: "PMP0", Category: "PO", Description: "ConditionType harga"},
	{Key: "PO_DEFAULT_TAX_RATE", Value: "11", Category: "PO", Description: "Tarif pajak (%) bila kode pajak Maximo tidak berisi angka"},
	{Key: "PO_ITEM_NOTE_TEXT_TYPE", Value: "F01", Category: "PO", Description: "TextObjectType catatan item"},
	{Key: "PO_LANGUAGE", Value: "EN", Category: "PO", Description: "Bahasa teks catatan item"},
	// Business Partner (API_BUSINESS_PARTNER)
	{Key: "BP_COMPANY_CODE", Value: "5110", Category: "BP", Description: "CompanyCode supplier & lookup recon account"},
	{Key: "BP_PURCHASING_ORG", Value: "5110", Category: "BP", Description: "PurchasingOrganization supplier"},
	{Key: "BP_DEFAULT_GROUPING", Value: "Z001", Category: "BP", Description: "BusinessPartnerGrouping bila vendor group tidak ada di mapping"},
	{Key: "BP_DEFAULT_RECON_ACCOUNT", Value: "211001", Category: "BP", Description: "ReconciliationAccount bila vendor group tidak ada di mapping"},
	{Key: "BP_CATEGORY", Value: "2", Category: "BP", Description: "BusinessPartnerCategory (2 = organisasi)"},
	{Key: "BP_FORM_OF_ADDRESS", Value: "0003", Category: "BP", Description: "FormOfAddress"},
	{Key: "BP_ROLES", Value: "FLVN00,FLVN01", Category: "BP", Description: "Role BP yang dibuat (dipisah koma)"},
	{Key: "BP_COUNTRY", Value: "ID", Category: "BP", Description: "Country alamat BP"},
	{Key: "BP_LANGUAGE", Value: "EN", Category: "BP", Description: "Language alamat BP"},
	{Key: "BP_DEFAULT_CURRENCY", Value: "IDR", Category: "BP", Description: "PurchaseOrderCurrency bila CURRENCYCODE kosong"},
	// Product (API_PRODUCT_SRV)
	{Key: "PRODUCT_DEFAULT_TYPE", Value: "ZITM", Category: "PRODUCT", Description: "ProductType bila ITEMTYPE tidak ada di Material Config"},
	{Key: "PRODUCT_SERVICE_TYPE", Value: "ZSVC", Category: "PRODUCT", Description: "ProductType untuk ITEMTYPE=SERVICE"},
	{Key: "PRODUCT_DEFAULT_GROUP", Value: "ZSTK", Category: "PRODUCT", Description: "ProductGroup bila STOCKTYPE kosong"},
	{Key: "PRODUCT_DEFAULT_BASE_UNIT", Value: "PC", Category: "PRODUCT", Description: "BaseUnit bila ISSUEUNIT kosong"},
	{Key: "PRODUCT_DEFAULT_VALUATION_CLASS", Value: "3000", Category: "PRODUCT", Description: "ValuationClass bila ITEMGROUPID tidak ada di Material Config"},
	{Key: "PRODUCT_VALUATION_PROCEDURE", Value: "V", Category: "PRODUCT", Description: "InventoryValuationProcedure (V = moving average, S = standard)"},
	{Key: "PRODUCT_PLANTS", Value: "5110,BTNG", Category: "PRODUCT", Description: "Plant/valuation area tempat material dibuat (dipisah koma)", SeedEnv: "SAP_PRODUCT_PLANTS"},
	{Key: "PRODUCT_LANGUAGE", Value: "EN", Category: "PRODUCT", Description: "Bahasa deskripsi produk"},
	// Goods Receipt (API_MATERIAL_DOCUMENT_SRV)
	{Key: "GRN_DEFAULT_PLANT", Value: "5110", Category: "GRN", Description: "Plant bila SITEID kosong"},
	{Key: "GRN_PLANT_MAP", Value: "JKT=1000,1000=1000,BTNG=2000,2000=2000", Category: "GRN", Description: "Pemetaan SITEID Maximo ke Plant SAP (SITE=PLANT, dipisah koma); SITEID lain dikirim apa adanya"},
	{Key: "GRN_MOVEMENT_RECEIPT", Value: "101", Category: "GRN", Description: "GoodsMovementType penerimaan"},
	{Key: "GRN_MOVEMENT_RETURN", Value: "122", Category: "GRN", Description: "GoodsMovementType retur ke vendor"},
	{Key: "GRN_GOODS_MOVEMENT_CODE", Value: "01", Category: "GRN", Description: "GoodsMovementCode header"},
	{Key: "GRN_PRINT_VERSION", Value: "3", Category: "GRN", Description: "VersionForPrintingSlip"},
	{Key: "GRN_RETURN_REASON_CODE", Value: "1", Category: "GRN", Description: "GoodsMovementReasonCode retur"},
	{Key: "GRN_RETURN_ITEM_TEXT", Value: "Goods Receipt from Maximo", Category: "GRN", Description: "MaterialDocumentItemText baris retur"},
	// Finance
	{Key: "FIN_COMPANY_CODE", Value: "1000", Category: "FINANCE", Description: "Company code modul finance: jurnal GI, tarik GL/aset, invoice, Talenta (my445160 = 1000, my446174 = 5110)", SeedEnv: "FIN_COMPANY_CODE"},
	{Key: "JE_DEFAULT_DOC_TYPE", Value: "SA", Category: "FINANCE", Description: "AccountingDocumentType jurnal bila tidak diisi"},
	{Key: "JE_DEFAULT_CURRENCY", Value: "IDR", Category: "FINANCE", Description: "Mata uang jurnal bila tidak diisi"},
	{Key: "ASSET_DEFAULT_CURRENCY", Value: "IDR", Category: "FINANCE", Description: "Mata uang akuisisi aset bila tidak diisi"},
	// File bank MUFG/BTMU (RICEF 5, "BPI - Mapping file bank MUFG")
	{Key: "FIN_BTMU_SEPARATOR", Value: ";", Category: "BANK_MUFG", Description: "Pemisah kolom file MUFG", SeedEnv: "FIN_BTMU_SEPARATOR"},
	{Key: "FIN_BTMU_BRANCH", Value: "3665", Category: "BANK_MUFG", Description: "Kode cabang MUFG kolom 1 (template hanya domestik; kolom 1 = cabang + mata uang + tipe + rekening)"},
	{Key: "FIN_BTMU_CURRENCY", Value: "IDR", Category: "BANK_MUFG", Description: "Mata uang transfer domestik; baris dengan mata uang lain ditolak"},
	{Key: "FIN_BTMU_ACCOUNT_TYPE", Value: "CUA", Category: "BANK_MUFG", Description: "Tipe rekening debit di kolom 1"},
	{Key: "FIN_BTMU_ACCOUNT", Value: "5100015546", Category: "BANK_MUFG", Description: "Nomor rekening debit BPI di MUFG. Kosong = file ditolak"},
	{Key: "FIN_BTMU_NAME_MAX_LEN", Value: "0", Category: "BANK_MUFG", Description: "Panjang maksimum nama vendor di kolom 3 (0 = tanpa batas)"},
	{Key: "FIN_BTMU_TRANSFER_TYPE", Value: "Domestic", Category: "BANK_MUFG", Description: "Kolom 4 (jenis transfer)", SeedEnv: "FIN_BTMU_TRANSFER_TYPE"},
	{Key: "FIN_BTMU_CLEARING_CODE", Value: "03", Category: "BANK_MUFG", Description: "Kolom 16 (kliring)", SeedEnv: "FIN_BTMU_CLEARING_CODE"},
	{Key: "FIN_BTMU_INSTRUCTION", Value: "RRCR0142+", Category: "BANK_MUFG", Description: "Kolom 17 (kode instruksi)", SeedEnv: "FIN_BTMU_INSTRUCTION"},
	{Key: "FIN_BTMU_CHARGE_BEARER", Value: "OUR", Category: "BANK_MUFG", Description: "Kolom 18 (charge bearer)", SeedEnv: "FIN_BTMU_CHARGE_BEARER"},
	// File bank BRI (RICEF 5, "BPI - Mapping file bank BRI")
	{Key: "FIN_BRI_SEPARATOR", Value: "|", Category: "BANK_BRI", Description: "Pemisah kolom file BRI", SeedEnv: "FIN_BRI_SEPARATOR"},
	{Key: "FIN_BRI_REF_PREFIX", Value: "BRI", Category: "BANK_BRI", Description: "Awalan kolom 2 (awalan + nomor urut kolom 1, contoh BRI0000000001)"},
	{Key: "FIN_BRI_REF_DIGITS", Value: "10", Category: "BANK_BRI", Description: "Jumlah digit nomor urut di kolom 2 (diisi nol di depan)"},
	{Key: "FIN_BRI_TRANSFER_SAME", Value: "IFT", Category: "BANK_BRI", Description: "Kolom 3 bila rekening vendor juga di BRI"},
	{Key: "FIN_BRI_TRANSFER_OTHER", Value: "LLG", Category: "BANK_BRI", Description: "Kolom 3 bila rekening vendor di bank lain"},
	{Key: "FIN_BRI_SAME_BANK_SWIFT", Value: "BRINIDJA", Category: "BANK_BRI", Description: "SWIFT BRI; PayeeSwiftCode dengan awalan ini (atau kode bank FIN_BRI_SAME_BANK_CODE) = IFT"},
	{Key: "FIN_BRI_SAME_BANK_CODE", Value: "002", Category: "BANK_BRI", Description: "Kode bank BRI (alternatif pengecekan sesama BRI)", SeedEnv: "FIN_BRI_SAME_BANK_CODE"},
	{Key: "FIN_BRI_DEBIT_ACCOUNT", Value: "", Category: "BANK_BRI", Description: "Kolom 5 rekening BPI bila field BankAccount payload kosong", SeedEnv: "FIN_BRI_DEBIT_ACCOUNT"},
	{Key: "FIN_BRI_REMITTER_NAME", Value: "PT BHIMASENA POWER INDONESIA", Category: "BANK_BRI", Description: "Kolom 6 nama pemilik rekening BPI", SeedEnv: "FIN_BRI_REMITTER_NAME"},
	{Key: "FIN_BRI_CITY", Value: "BATANG", Category: "BANK_BRI", Description: "Kolom 10"},
	{Key: "FIN_BRI_CHARGE_BEARER", Value: "OUR", Category: "BANK_BRI", Description: "Kolom 15"},
	{Key: "FIN_BRI_COL16", Value: "ID", Category: "BANK_BRI", Description: "Kolom 16"},
	{Key: "FIN_BRI_COL17", Value: "", Category: "BANK_BRI", Description: "Kolom 17"},
	{Key: "FIN_BRI_REMITTER_ADDRESS", Value: "UJUNGNEGORO BATANG INDONESIA", Category: "BANK_BRI", Description: "Kolom 18 alamat BPI"},
	{Key: "FIN_BRI_REMITTER_PHONE", Value: "81234567890", Category: "BANK_BRI", Description: "Kolom 19 nomor telepon BPI"},
	{Key: "FIN_BRI_COL20", Value: "ID", Category: "BANK_BRI", Description: "Kolom 20"},
	{Key: "FIN_BRI_COL21", Value: "ID", Category: "BANK_BRI", Description: "Kolom 21"},
}

var defaultSpecByKey = func() map[string]DefaultValueSpec {
	m := make(map[string]DefaultValueSpec, len(DefaultValueSpecs))
	for _, s := range DefaultValueSpecs {
		m[s.Key] = s
	}
	return m
}()

// defaultValueTTL: perubahan dari Panel berlaku paling lambat selama ini tanpa restart Engine.
const defaultValueTTL = 60 * time.Second

type defaultValueCache struct {
	mu       sync.Mutex
	values   map[string]string
	loadedAt time.Time
}

const defaultValuesDDL = `
	CREATE TABLE IF NOT EXISTS sap_default_values (
		key VARCHAR(100) PRIMARY KEY,
		value TEXT NOT NULL DEFAULT '',
		default_value TEXT NOT NULL DEFAULT '',
		category VARCHAR(30) NOT NULL DEFAULT 'GENERAL',
		description TEXT,
		updated_by VARCHAR(100),
		updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
	)`

// EnsureDefaultValues membuat tabel sap_default_values dan menambahkan key baru. Nilai yang sudah
// diubah dari Panel tidak ditimpa; default_value & deskripsi selalu diperbarui sebagai referensi.
func (r *PostgresRepository) EnsureDefaultValues(ctx context.Context) error {
	if r == nil || r.Pool == nil {
		return nil
	}
	if _, err := r.Pool.Exec(ctx, defaultValuesDDL); err != nil {
		return err
	}
	for _, s := range DefaultValueSpecs {
		seed := s.seedValue()
		if _, err := r.Pool.Exec(ctx, `
			INSERT INTO sap_default_values (key, value, default_value, category, description, updated_by)
			VALUES ($1, $2, $3, $4, $5, 'seed')
			ON CONFLICT (key) DO UPDATE SET default_value = EXCLUDED.default_value,
				category = EXCLUDED.category, description = EXCLUDED.description`,
			s.Key, seed, s.Value, s.Category, s.Description); err != nil {
			return err
		}
	}
	r.InvalidateDefaultValues()
	return nil
}

// InvalidateDefaultValues memaksa pembacaan ulang dari database pada pemanggilan berikutnya.
func (r *PostgresRepository) InvalidateDefaultValues() {
	if r == nil {
		return
	}
	r.defaults.mu.Lock()
	r.defaults.loadedAt = time.Time{}
	r.defaults.mu.Unlock()
}

// Default mengembalikan nilai default untuk key: isi tabel sap_default_values (Panel), atau nilai
// awal di DefaultValueSpecs bila database tidak tersedia / key belum ada. Nilai kosong yang sengaja
// disimpan dari Panel dikembalikan apa adanya (dipakai untuk "wajib diisi").
func (r *PostgresRepository) Default(ctx context.Context, key string) string {
	spec, known := defaultSpecByKey[key]
	if !known {
		log.Printf("[WARN] default value %q tidak terdaftar di DefaultValueSpecs", key)
	}
	if r == nil || r.Pool == nil {
		return spec.seedValue()
	}
	r.defaults.mu.Lock()
	defer r.defaults.mu.Unlock()
	if r.defaults.values == nil || time.Since(r.defaults.loadedAt) > defaultValueTTL {
		if vals, err := r.loadDefaultValues(ctx); err == nil {
			r.defaults.values, r.defaults.loadedAt = vals, time.Now()
		} else if r.defaults.values == nil {
			return spec.seedValue()
		}
	}
	if v, ok := r.defaults.values[key]; ok {
		return strings.TrimSpace(v)
	}
	return spec.seedValue()
}

// seedValue: env lama (SeedEnv) bila diisi, selain itu nilai awal di kode.
func (s DefaultValueSpec) seedValue() string {
	if s.SeedEnv != "" {
		if v := strings.TrimSpace(os.Getenv(s.SeedEnv)); v != "" {
			return v
		}
	}
	return s.Value
}

func (r *PostgresRepository) loadDefaultValues(ctx context.Context) (map[string]string, error) {
	rows, err := r.Pool.Query(ctx, `SELECT key, value FROM sap_default_values`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}
