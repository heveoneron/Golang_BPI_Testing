package model

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Doc types (dan service_code SAP) untuk RICEF finance yang dikirim Middleware -> SAP.
const (
	DocTypeBudget          = "BUDGET"           // RICEF 1
	DocTypeJournalGI       = "JOURNAL_GI"       // RICEF 2 (SOAP)
	DocTypeJournalStockAdj = "JOURNAL_ADJ"      // RICEF 2 variant (SOAP Stock Adjustment)
	DocTypeExchangeRate    = "EXCHANGE_RATE"    // RICEF 6
	DocTypeSupplierInvoice = "SUPPLIER_INVOICE" // RICEF 7
	DocTypeAssetAcq        = "ASSET_ACQ"        // RICEF 8
)

// Service code SAP untuk RICEF finance yang ditarik SAP -> Middleware.
const (
	ServiceGLLineItems     = "GL_ACCOUNT_LINE_ITEMS" // RICEF 3 & 4
	ServicePaymentProposal = "PAYMENT_PROPOSAL"      // RICEF 5
	ServiceFixedAsset      = "FIXED_ASSET"           // RICEF 9
)

// ===================== RICEF 1: Budget =====================

// BudgetSupplementRequest menambah pagu satu kombinasi [MainAccount]-[BudgetCode].
type BudgetSupplementRequest struct {
	MainAccount string  `json:"main_account" example:"513043"`
	BudgetCode  string  `json:"budget_code" example:"10081"`
	CostCenter  string  `json:"cost_center,omitempty" example:"51101101"` // opsional, diteruskan ke SAP
	Year        int     `json:"year" example:"2026"`
	Amount      float64 `json:"amount" example:"50000000"`
	Reason      string  `json:"reason" example:"Tambahan pagu pemeliharaan turbin"`
	RequestedBy string  `json:"requested_by" example:"finance01"`
}

// BudgetTransferRequest memindahkan pagu dari satu kombinasi ke kombinasi lain.
type BudgetTransferRequest struct {
	FromMainAccount string  `json:"from_main_account" example:"513043"`
	FromBudgetCode  string  `json:"from_budget_code" example:"10081"`
	ToMainAccount   string  `json:"to_main_account" example:"513044"`
	ToBudgetCode    string  `json:"to_budget_code" example:"10082"`
	Year            int     `json:"year" example:"2026"`
	Amount          float64 `json:"amount" example:"10000000"`
	Reason          string  `json:"reason" example:"Realokasi pagu"`
	RequestedBy     string  `json:"requested_by" example:"finance01"`
}

// BudgetAdjustment adalah catatan supplement / transfer budget (tabel budget_adjustments).
type BudgetAdjustment struct {
	ID              int64     `json:"id"`
	DocumentNo      string    `json:"document_no"`
	AdjustmentType  string    `json:"adjustment_type"` // SUPPLEMENT, TRANSFER
	BudgetYear      int       `json:"budget_year"`
	FromCombination string    `json:"from_combination,omitempty"`
	ToCombination   string    `json:"to_combination"`
	Amount          float64   `json:"amount"`
	Reason          string    `json:"reason"`
	RequestedBy     string    `json:"requested_by"`
	Status          string    `json:"status"` // PENDING, APPLIED, FAILED
	ErrorMessage    string    `json:"error_message,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// SAPBudgetPayload adalah payload sementara RICEF 1 (endpoint SAP masih TBD).
type SAPBudgetPayload struct {
	AdjustmentType    string  `json:"AdjustmentType"`
	FiscalYear        string  `json:"FiscalYear"`
	BudgetCombination string  `json:"BudgetCombination"`
	MainAccount       string  `json:"MainAccount"`
	BudgetCode        string  `json:"BudgetCode"`
	CostCenter        string  `json:"CostCenter,omitempty"`
	SenderMainAccount string  `json:"SenderMainAccount,omitempty"`
	SenderBudgetCode  string  `json:"SenderBudgetCode,omitempty"`
	Amount            float64 `json:"Amount"`
	Reason            string  `json:"Reason,omitempty"`
	ExternalReference string  `json:"ExternalReference"`
}

// ===================== RICEF 2: Journal Entry GI =====================

type JournalEntryRequest struct {
	Reference    string             `json:"reference" example:"JE-BPI-2026-0001"`
	CompanyCode  string             `json:"company_code" example:"5110"`
	DocumentType string             `json:"document_type" example:"SA"`
	DocumentDate string             `json:"document_date" example:"2026-11-05"`
	PostingDate  string             `json:"posting_date" example:"2026-11-05"`
	Currency     string             `json:"currency" example:"IDR"`
	HeaderText   string             `json:"header_text" example:"Jurnal GI pemakaian material"`
	Reference1   string             `json:"reference1,omitempty" example:"GI-2026-0001"` // Reference1InDocumentHeader (maks 20), default = reference
	Reference2   string             `json:"reference2,omitempty" example:"WO12345"`      // Reference2InDocumentHeader (maks 10)
	Items        []JournalEntryItem `json:"items"`
}

type JournalEntryItem struct {
	GLAccount   string  `json:"gl_account" example:"513043"`
	Amount      float64 `json:"amount" example:"1500000"`
	DebitCredit string  `json:"debit_credit" example:"S"` // S = debit, H = kredit
	CostCenter  string  `json:"cost_center,omitempty" example:"51101101"`
	BudgetCode  string  `json:"budget_code,omitempty" example:"10081"`
	ItemText    string  `json:"item_text,omitempty" example:"Pemakaian bearing"`
}

// ValuationClassGLMapping merepresentasikan matriks pemetaan Valuation Class ke Akun GL & Cost Center
// sesuai Enhancement Specification DJ-MM-ENH-ZME003
type ValuationClassGLMapping struct {
	ID                 int64     `json:"id"`
	ValuationClass     string    `json:"valuation_class"`
	InventoryPostingGL string    `json:"inventory_posting_gl"`
	GoodsIssueGL       string    `json:"goods_issue_gl"`
	StockAdjGL         string    `json:"stock_adj_gl"`
	CostCenter         string    `json:"cost_center"`
	Description        string    `json:"description,omitempty"`
	IsActive           bool      `json:"is_active"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// GIOrAdjustmentInput merepresentasikan payload input transaksi Goods Issue / Stock Adjustment dari Maximo
type GIOrAdjustmentInput struct {
	Reference       string  `json:"reference"`       // Contoh: GI-2026-0001 atau ADJ-2026-0001
	TransactionType string  `json:"transaction_type"` // "GOODS_ISSUE" / "GI" atau "STOCK_ADJUSTMENT" / "ADJ"
	ItemNum         string  `json:"item_num"`
	ValuationClass  string  `json:"valuation_class,omitempty"` // Opsional, jika kosong di-fetch via OData SAP
	Quantity        float64 `json:"quantity"`
	UnitCost        float64 `json:"unit_cost,omitempty"`       // Biaya per unit
	TotalAmount     float64 `json:"total_amount,omitempty"`    // Total nominal (jika 0, dihitung dari Quantity * UnitCost)
	Currency        string  `json:"currency,omitempty"`        // Default IDR / USD
	TransDate       string  `json:"trans_date"`                // YYYY-MM-DD
	DocumentDate    string  `json:"document_date,omitempty"`   // YYYY-MM-DD
	PostingDate     string  `json:"posting_date,omitempty"`    // YYYY-MM-DD
	CompanyCode     string  `json:"company_code,omitempty"`    // Default 1000
	Reference1      string  `json:"reference1,omitempty"`
	Reference2      string  `json:"reference2,omitempty"`
	ItemText        string  `json:"item_text,omitempty"`
}

// ===================== RICEF 3/4, 5, 9: data dari SAP =====================

type GLLineItem struct {
	Key                         string          `json:"gl_line_item_key"`
	CompanyCode                 string          `json:"company_code"`
	FiscalYear                  string          `json:"fiscal_year"`
	AccountingDocument          string          `json:"accounting_document"`
	LedgerGLLineItem            string          `json:"ledger_gl_line_item"`
	AccountingDocumentType      string          `json:"accounting_document_type"`
	PostingDate                 *string         `json:"posting_date"`
	DocumentDate                *string         `json:"document_date"`
	GLAccount                   string          `json:"gl_account"`
	CostCenter                  string          `json:"cost_center"`
	ProfitCenter                string          `json:"profit_center"`
	Supplier                    string          `json:"supplier"`
	DebitCreditCode             string          `json:"debit_credit_code"`
	AmountInCompanyCodeCurrency float64         `json:"amount_in_company_code_currency"`
	CompanyCodeCurrency         string          `json:"company_code_currency"`
	AmountInTransactionCurrency float64         `json:"amount_in_transaction_currency"`
	TransactionCurrency         string          `json:"transaction_currency"`
	DocumentItemText            string          `json:"document_item_text"`
	Raw                         json.RawMessage `json:"raw,omitempty" swaggertype:"object"`
	SyncedAt                    time.Time       `json:"synced_at"`
}

type PaymentProposal struct {
	RecordKey       string          `json:"record_key"`
	PaymentRunID    string          `json:"payment_run_id"`
	PaymentRunDate  *string         `json:"payment_run_date"`
	CompanyCode     string          `json:"company_code"`
	Supplier        string          `json:"supplier"`
	PaymentDocument string          `json:"payment_document"`
	Amount          float64         `json:"amount"`
	Currency        string          `json:"currency"`
	Raw             json.RawMessage `json:"raw,omitempty" swaggertype:"object"`
	SyncedAt        time.Time       `json:"synced_at"`
}

type FixedAsset struct {
	CompanyCode        string          `json:"company_code"`
	MasterFixedAsset   string          `json:"master_fixed_asset"`
	FixedAsset         string          `json:"fixed_asset"`
	Description        string          `json:"description"`
	AdditionalDesc     string          `json:"additional_description"`
	AssetClass         string          `json:"asset_class"`
	CapitalizationDate *string         `json:"capitalization_date"`
	Raw                json.RawMessage `json:"raw,omitempty" swaggertype:"object"`
	SyncedAt           time.Time       `json:"synced_at"`
}

// SyncResult adalah ringkasan satu siklus penarikan data dari SAP.
type SyncResult struct {
	Target   string `json:"target"`
	Fetched  int    `json:"fetched"`
	Upserted int    `json:"upserted"`
	Pages    int    `json:"pages"`
	Error    string `json:"error,omitempty"`
}

// ===================== RICEF 6: Exchange Rate =====================

type SAPExchangeRatePayload struct {
	ExchangeRateType            string  `json:"ExchangeRateType"`
	SourceCurrency              string  `json:"SourceCurrency"`
	TargetCurrency              string  `json:"TargetCurrency"`
	ValidityStartDate           string  `json:"ValidityStartDate"`
	ExchangeRate                float64 `json:"ExchangeRate"`
	NumberOfSourceCurrencyUnits int     `json:"NumberOfSourceCurrencyUnits"`
	NumberOfTargetCurrencyUnits int     `json:"NumberOfTargetCurrencyUnits"`
}

// ===================== RICEF 7: Supplier Invoice (Excel) =====================

// SupplierInvoiceRow adalah satu baris Excel/CSV upload reimbursement & business trip.
type SupplierInvoiceRow struct {
	RowNumber      int     `json:"row"`
	InvoiceRef     string  `json:"InvoiceRef"`
	CompanyCode    string  `json:"CompanyCode"`
	DocumentDate   string  `json:"DocumentDate"`
	PostingDate    string  `json:"PostingDate"`
	InvoicingParty string  `json:"InvoicingParty"`
	Currency       string  `json:"Currency"`
	GrossAmount    float64 `json:"GrossAmount"`
	HeaderText     string  `json:"HeaderText,omitempty"`
	DocType        string  `json:"DocType"`
	GLAccount      string  `json:"GLAccount"`
	CostCenter     string  `json:"CostCenter,omitempty"`
	Amount         float64 `json:"Amount"`
	TaxCode        string  `json:"TaxCode,omitempty"`
	ItemText       string  `json:"ItemText,omitempty"`
	AssignmentRef  string  `json:"AssignmentRef,omitempty"`
	PaymentTerms   string  `json:"PaymentTerms,omitempty"`
	PolicyName     string  `json:"PolicyName,omitempty"` // nama kebijakan reimbursement (penentu GL)
	CIPFlag        string  `json:"CIP,omitempty"`        // override CIP / NON_CIP; kosong = dari mapping vendor karyawan
	BudgetCode     string  `json:"BudgetCode,omitempty"` // hasil mapping kebijakan
}

// SAPSupplierInvoicePayload mengikuti template "BPI - Payload API BT & Reimbursement.txt"
// (A_SupplierInvoice deep insert, item GL tanpa referensi PO).
type SAPSupplierInvoicePayload struct {
	CompanyCode                   string                         `json:"CompanyCode"`
	DocumentDate                  string                         `json:"DocumentDate"`
	PostingDate                   string                         `json:"PostingDate"`
	TaxDeterminationDate          string                         `json:"TaxDeterminationDate"`
	SupplierInvoiceIDByInvcgParty string                         `json:"SupplierInvoiceIDByInvcgParty"`
	InvoicingParty                string                         `json:"InvoicingParty"`
	DocumentCurrency              string                         `json:"DocumentCurrency"`
	InvoiceGrossAmount            string                         `json:"InvoiceGrossAmount"`
	DocumentHeaderText            string                         `json:"DocumentHeaderText,omitempty"`
	SupplierInvoiceStatus         string                         `json:"SupplierInvoiceStatus,omitempty"`
	AccountingDocumentType        string                         `json:"AccountingDocumentType,omitempty"`
	TaxIsCalculatedAutomatically  bool                           `json:"TaxIsCalculatedAutomatically"`
	PaymentTerms                  string                         `json:"PaymentTerms,omitempty"`
	DueCalculationBaseDate        string                         `json:"DueCalculationBaseDate"`
	ToSupplierInvoiceItemGLAcct   []SAPSupplierInvoiceGLAcctItem `json:"to_SupplierInvoiceItemGLAcct"`
}

type SAPSupplierInvoiceGLAcctItem struct {
	SupplierInvoiceItem       string `json:"SupplierInvoiceItem"`
	DocumentCurrency          string `json:"DocumentCurrency"`
	SupplierInvoiceItemAmount string `json:"SupplierInvoiceItemAmount"`
	DebitCreditCode           string `json:"DebitCreditCode"`
	GLAccount                 string `json:"GLAccount"`
	CostCenter                string `json:"CostCenter,omitempty"`
	TaxCode                   string `json:"TaxCode,omitempty"`
	SupplierInvoiceItemText   string `json:"SupplierInvoiceItemText,omitempty"`
	AssignmentReference       string `json:"AssignmentReference,omitempty"`
}

// UploadRowError menjelaskan kesalahan validasi satu baris upload.
type UploadRowError struct {
	Row     int    `json:"row"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

type ExcelUploadBatch struct {
	ID            int64            `json:"id"`
	FileName      string           `json:"file_name"`
	UploadedBy    string           `json:"uploaded_by"`
	TotalRows     int              `json:"total_rows"`
	TotalInvoices int              `json:"total_invoices"`
	Status        string           `json:"status"` // QUEUED, REJECTED
	Errors        []UploadRowError `json:"errors,omitempty"`
	CreatedAt     time.Time        `json:"created_at"`
}

// ReimbursementGLMapping memetakan kebijakan reimbursement + status CIP ke GL account & budget code.
type ReimbursementGLMapping struct {
	ID         int       `json:"id"`
	PolicyName string    `json:"policy_name" example:"Medical Claim - Outpatient"`
	CIPFlag    string    `json:"cip_flag" example:"CIP"` // CIP / NON_CIP
	GLAccount  string    `json:"gl_account" example:"511006"`
	BudgetCode string    `json:"budget_code" example:"10081"`
	IsActive   bool      `json:"is_active"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// EmployeeVendorMapping menyatakan vendor karyawan termasuk CIP atau Non CIP.
type EmployeeVendorMapping struct {
	VendorCode    string    `json:"vendor_code" example:"VND-5072"`
	CIPFlag       string    `json:"cip_flag" example:"NON_CIP"`
	Email         string    `json:"email,omitempty" example:"vendor@company.com"`
	BankKey       string    `json:"bank_key,omitempty" example:"Mandiri"`
	BankAccount   string    `json:"bank_account,omitempty" example:"1010101"`
	AccountHolder string    `json:"account_holder,omitempty" example:"PT Vendor"`
	Description   string    `json:"description,omitempty"`
	IsActive      bool      `json:"is_active"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// NormalizePolicyName menyamakan penulisan nama kebijakan: huruf kecil, semua jenis tanda pisah
// (-, –, —, karakter rusak) menjadi "-", dan spasi berlebih dirapikan.
func NormalizePolicyName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch r {
		case '–', '—', '‒', '−', '�':
			b.WriteRune('-')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	out = strings.ReplaceAll(out, " - ", "-")
	out = strings.ReplaceAll(out, "- ", "-")
	return strings.ReplaceAll(out, " -", "-")
}

// NormalizeCIPFlag menerima "CIP", "Non CIP", "NON-CIP", "non_cip", dst. ok=false bila tidak dikenali.
func NormalizeCIPFlag(s string) (string, bool) {
	k := strings.ToUpper(strings.NewReplacer(" ", "", "-", "", "_", "").Replace(strings.TrimSpace(s)))
	switch k {
	case "CIP":
		return "CIP", true
	case "NONCIP":
		return "NON_CIP", true
	}
	return "", false
}

// ===================== SAP -> Maximo (tabel dbo.mxinvoice) =====================

// MxInvoiceRow adalah satu baris tabel interface Maximo dbo.mxinvoice (satu baris = satu line invoice).
// Dokumen mapping: "BPI_MAPPING_API_MAXIMO_to_SAP (FinanceModule)", sheet Invoice & GJ.
type MxInvoiceRow struct {
	CurrencyCode   string     `json:"currencycode"`
	InvoiceDesc    string     `json:"invoicedesc"`
	InvoiceType    string     `json:"invoicetype"` // INVOICE / JOURNAL
	DueDate        *time.Time `json:"duedate"`
	GLPostDate     *time.Time `json:"glpostdate"`
	InvoiceDate    *time.Time `json:"invoicedate"`
	InvoiceNum     string     `json:"invoicenum"`
	Paid           float64    `json:"paid"`
	PaidDate       *time.Time `json:"paiddate"`
	SiteID         string     `json:"siteid"`
	Status         string     `json:"status"`
	Vendor         string     `json:"vendor"`
	BudgetCode     string     `json:"budgetcode"`
	BudgetNum      string     `json:"budgetnum"`
	BPIChargeCode  string     `json:"bpichargecode"`
	BPIChargeValue float64    `json:"bpichargevalue"`
	ItemDesc       string     `json:"itemdesc"`
	BPIGRNNumber   string     `json:"bpigrnnumber"`
	InvoiceLineNum int        `json:"invoicelinenum"`
	InvoiceQty     float64    `json:"invoiceqty"`
	ItemNum        string     `json:"itemnum"`
	LineCost       float64    `json:"linecost"`
	LineType       string     `json:"linetype"`
	POLineNum      *int       `json:"polinenum"`
	PONum          string     `json:"ponum"`
	POSiteID       string     `json:"positeid"`
	TaxCode1       string     `json:"taxcode1"`
	TaxCode2       string     `json:"taxcode2"`
	UnitCost       float64    `json:"unitcost"`
	CostLineNum    int        `json:"costlinenum"`
	GLDebitAcct    string     `json:"gldebitacct"`
	TransID        int64      `json:"transid"`
	TransSeq       int        `json:"transseq"`
}

// MaximoExportMessage adalah satu dokumen SAP (invoice / jurnal) yang akan ditulis ke mxinvoice.
type MaximoExportMessage struct {
	SourceType string         `json:"source_type"` // INVOICE / JOURNAL
	SourceKey  string         `json:"source_key"`  // SupplierInvoice/FiscalYear atau CompanyCode/FiscalYear/AccountingDocument
	Rows       []MxInvoiceRow `json:"rows"`
	Warnings   []string       `json:"warnings,omitempty"`
}

// MaximoExport adalah riwayat penulisan satu dokumen ke Maximo.
type MaximoExport struct {
	SourceType string    `json:"source_type"`
	SourceKey  string    `json:"source_key"`
	Status     string    `json:"status"` // EXPORTED / FAILED
	LineCount  int       `json:"line_count"`
	TransIDs   string    `json:"trans_ids"`
	Error      string    `json:"error,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// ===================== RICEF 8: Asset Acquisition =====================

type AssetAcquisitionRequest struct {
	Reference         string  `json:"reference" example:"AQ-2026-0001"`
	CompanyCode       string  `json:"company_code" example:"5110"`
	MasterFixedAsset  string  `json:"master_fixed_asset" example:"50000000"`
	FixedAsset        string  `json:"fixed_asset" example:"0"`
	DocumentDate      string  `json:"document_date" example:"2026-11-05"`
	PostingDate       string  `json:"posting_date" example:"2026-11-05"`
	AssetValueDate    string  `json:"asset_value_date" example:"2026-11-05"`
	Amount            float64 `json:"amount" example:"250000000"`
	Currency          string  `json:"currency" example:"IDR"`
	OffsettingAccount string  `json:"offsetting_account" example:"199999"`
	ItemText          string  `json:"item_text" example:"Pengakuan aset pompa"`
	ParkedBy          string  `json:"parked_by" example:"finance01"`
}

type AssetAcquisition struct {
	ID                uuid.UUID  `json:"id"`
	Reference         string     `json:"reference"`
	CompanyCode       string     `json:"company_code"`
	MasterFixedAsset  string     `json:"master_fixed_asset"`
	FixedAsset        string     `json:"fixed_asset"`
	DocumentDate      string     `json:"document_date"`
	PostingDate       string     `json:"posting_date"`
	AssetValueDate    string     `json:"asset_value_date"`
	Amount            float64    `json:"amount"`
	Currency          string     `json:"currency"`
	OffsettingAccount string     `json:"offsetting_account"`
	ItemText          string     `json:"item_text"`
	Status            string     `json:"status"` // PARKED, APPROVED, POSTED, FAILED, REJECTED
	ParkedBy          string     `json:"parked_by"`
	ApprovedBy        string     `json:"approved_by,omitempty"`
	ApprovedAt        *time.Time `json:"approved_at,omitempty"`
	RejectedBy        string     `json:"rejected_by,omitempty"`
	RejectReason      string     `json:"reject_reason,omitempty"`
	SAPDocumentNo     string     `json:"sap_document_no,omitempty"`
	ErrorMessage      string     `json:"error_message,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	PrintedAt         *time.Time `json:"printed_at,omitempty"` // terakhir Print Form dicetak; syarat Approve/Reject
}

// SAPAssetAcquisitionPayload adalah parameter action FixedAssetAcquisition/SAP__self.Post (OData V4).
// Nama field perlu diverifikasi terhadap $metadata tenant.
type SAPAssetAcquisitionPayload struct {
	CompanyCode                  string  `json:"CompanyCode"`
	MasterFixedAsset             string  `json:"MasterFixedAsset"`
	FixedAsset                   string  `json:"FixedAsset"`
	DocumentDate                 string  `json:"DocumentDate"`
	PostingDate                  string  `json:"PostingDate"`
	AssetValueDate               string  `json:"AssetValueDate"`
	AcqnAmtInTransactionCurrency float64 `json:"AcqnAmtInTransactionCurrency"`
	TransactionCurrency          string  `json:"TransactionCurrency"`
	OffsettingAccount            string  `json:"OffsettingAccount"`
	DocumentReferenceID          string  `json:"DocumentReferenceID"`
	DocumentItemText             string  `json:"DocumentItemText,omitempty"`
}

// ApprovalRequest dipakai untuk approve / reject parking aset.
type ApprovalRequest struct {
	By     string `json:"by" example:"manager01"`
	Reason string `json:"reason,omitempty" example:"Nilai tidak sesuai BAST"`
}
