package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// StagingDocument menampung data raw dari SQL Server Maximo (Tier 1)
type StagingDocument struct {
	ID               uuid.UUID       `json:"id"`
	DocType          string          `json:"doc_type"`
	DocumentNo       string          `json:"document_no"`
	FlowType         string          `json:"flow_type"`
	RawPayload       json.RawMessage `json:"raw_payload"`
	Status           string          `json:"status"` // PENDING, PROCESSED, FAILED
	ErrorMsg         string          `json:"error_msg"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

// Document adalah State Machine Core (Tier 2, ala ShoeTown)
type Document struct {
	ID                 uuid.UUID       `json:"id"`
	DocType            string          `json:"doc_type"`
	DocumentNo         string          `json:"document_no"`
	FlowType           string          `json:"flow_type"`
	GroupID            string          `json:"group_id"`
	ParentDocType      *string         `json:"parent_doc_type"`
	ParentDocumentNo   *string         `json:"parent_document_no"`
	RawPayload         json.RawMessage `json:"raw_payload"`
	SAPPayload         json.RawMessage `json:"sap_payload"`
	Status             string          `json:"status"` // pending, processing, success, error_data, error_conn
	RetryCount         int             `json:"retry_count"`
	LastAttemptAt      *time.Time      `json:"last_attempt_at"`
	SAPDocumentNo      *string         `json:"sap_document_no"`
	SAPResponse        json.RawMessage `json:"sap_response"`
	ErrorMessage       *string         `json:"error_message"`
	SyncBackStatus     string          `json:"sync_back_status"`
	DocumentDate       *time.Time      `json:"document_date"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
}

// BudgetMaster menampung pagu anggaran
type BudgetMaster struct {
	ID                int64     `json:"id"`
	BudgetCombination string    `json:"budget_combination"`
	BudgetYear        int       `json:"budget_year"`
	ApprovedBudget    float64   `json:"approved_budget"`
	RemainingBudget   float64   `json:"remaining_budget"`
	Status            string    `json:"status"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// BudgetTransactionLock menyimpan hold budget reservation
type BudgetTransactionLock struct {
	ID                int64     `json:"id"`
	BudgetCombination string    `json:"budget_combination"`
	BudgetYear        int       `json:"budget_year"`
	ReferenceDocType  string    `json:"reference_doc_type"`
	ReferenceDocNo    string    `json:"reference_doc_no"`
	LockAmount        float64   `json:"lock_amount"`
	LockStatus        string    `json:"lock_status"` // RESERVED, COMMITTED, RELEASED
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// SchedulerConfig menampung runtime settings poller
type SchedulerConfig struct {
	ID           int       `json:"id"`
	PullIsActive bool      `json:"pull_is_active"`
	PullInterval int       `json:"pull_interval"` // menit
	PushIsActive bool      `json:"push_is_active"`
	PushInterval int       `json:"push_interval"` // menit
	LastCheckAt  time.Time `json:"last_check_at"`
}

// Payload Struktur Maximo (Contoh PO)
type MaximoPOHeader struct {
	PONum                       string         `json:"PONUM" example:"PO-BPI-2026-001"`
	Vendor                      string         `json:"VENDOR" example:"VEND-PLTU-01"`
	OrderDate                   string         `json:"ORDERDATE" example:"2026-10-12"`
	CurrencyCode                string         `json:"CURRENCYCODE" example:"IDR"`
	Status                      string         `json:"STATUS" example:"APPR"`
	RevisionNum                 int            `json:"REVISIONNUM" example:"0"`
	SiteID                      string         `json:"SITEID,omitempty"`
	DownPaymentType             string         `json:"DOWNPAYMENTTYPE,omitempty"`
	DownPaymentPercentageOfTotAmt float64      `json:"DOWNPAYMENTPERCENTAGE,omitempty"`
	DownPaymentDueDate          string         `json:"DOWNPAYMENTDUEDATE,omitempty"`
	Lines                       []MaximoPOLine `json:"LINES"`
}

type MaximoPOLine struct {
	POLineNum    int     `json:"POLINENUM" example:"1"`
	PRNum        string  `json:"PRNUM,omitempty" example:"PR-2026-001"`
	PRLineNum    int     `json:"PRLINENUM,omitempty" example:"1"`
	LineType     string  `json:"LINETYPE,omitempty" example:"ITEM"`
	ItemNum      string  `json:"ITEMNUM" example:"BEARING-TURBINE-6204"`
	Description  string  `json:"DESCRIPTION,omitempty"`
	OrderUnit    string  `json:"ORDERUNIT" example:"PCS"`
	OrderQty     float64 `json:"QTY" example:"5"`
	UnitCost     float64 `json:"UNITCOST" example:"150000"`
	BudgetCode   string  `json:"BUDGETCODE" example:"10081"`
	CostCenter   string  `json:"DEPARTMENT" example:"3000"`
	MainAccount  string  `json:"MAINACCOUNT" example:"513043"`
	TaxCodePPN   string  `json:"TAXCODE1,omitempty" example:"V1"`
	SiteID       string  `json:"SITEID,omitempty"`
}

// SAPEndpoint merepresentasikan registry service endpoint & kredensial SAP
type SAPEndpoint struct {
	ID           int        `json:"id"`
	ServiceCode  string     `json:"service_code"`
	ServiceName  string     `json:"service_name"`
	ScenarioID   string     `json:"scenario_id"`
	ODataVersion string     `json:"odata_version"`
	BaseURL      string     `json:"base_url"`
	EndpointPath string     `json:"endpoint_path"`
	CSRFPath     string     `json:"csrf_path"`
	Username     string     `json:"username"`
	Password     string     `json:"password,omitempty"`
	IsActive     bool       `json:"is_active"`
	LastStatus   string     `json:"last_status"`
	LastResponse string     `json:"last_response"`
	LastTestedAt *time.Time `json:"last_tested_at"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// Payload Struktur Target SAP Grow OData v4 Purchase Order
type SAPGrowPOPayload struct {
	PurchaseOrder                string          `json:"PurchaseOrder,omitempty"`
	PurchaseOrderType            string          `json:"PurchaseOrderType"`
	PurchaseOrderDate            string          `json:"PurchaseOrderDate"`
	CompanyCode                  string          `json:"CompanyCode"`
	PurchasingOrganization       string          `json:"PurchasingOrganization"`
	PurchasingGroup              string          `json:"PurchasingGroup"`
	Supplier                     string          `json:"Supplier"`
	SupplierRespSalesPersonName  string          `json:"SupplierRespSalesPersonName,omitempty"`
	DocumentCurrency             string          `json:"DocumentCurrency"`
	Language                     string          `json:"Language,omitempty"`
	PaymentTerms                 string          `json:"PaymentTerms,omitempty"`
	IncotermsClassification      string          `json:"IncotermsClassification,omitempty"`
	IncotermsLocation1           string          `json:"IncotermsLocation1,omitempty"`
	DownPaymentType              string          `json:"DownPaymentType,omitempty"`
	DownPaymentPercentageOfTotAmt *float64       `json:"DownPaymentPercentageOfTotAmt,omitempty"`
	DownPaymentDueDate           string          `json:"DownPaymentDueDate,omitempty"`
	PurchaseOrderItem            []SAPGrowPOItem `json:"_PurchaseOrderItem"`
}

type SAPGrowPOItem struct {
	PurchaseOrderItem         string                           `json:"PurchaseOrderItem"`
	Material                  string                           `json:"Material"`
	Plant                     string                           `json:"Plant"`
	AccountAssignmentCategory string                           `json:"AccountAssignmentCategory,omitempty"`
	PurchaseOrderItemText     string                           `json:"PurchaseOrderItemText"`
	PurchaseOrderQuantityUnit string                           `json:"PurchaseOrderQuantityUnit"`
	OrderPriceUnit            string                           `json:"OrderPriceUnit"`
	NetPriceAmount            float64                          `json:"NetPriceAmount"`
	DocumentCurrency          string                           `json:"DocumentCurrency"`
	OrderQuantity             float64                          `json:"OrderQuantity"`
	NetPriceQuantity          int                              `json:"NetPriceQuantity,omitempty"`
	TaxCode                   string                           `json:"TaxCode,omitempty"`
	InvoiceIsGoodsReceiptBased bool                            `json:"InvoiceIsGoodsReceiptBased"`
	StorageLocation           string                           `json:"StorageLocation,omitempty"`
	PurOrdAccountAssignment   []SAPGrowPurOrdAccountAssignment `json:"_PurOrdAccountAssignment,omitempty"`
	PurchaseOrderItemNote     []SAPGrowPurchaseOrderItemNote   `json:"_PurchaseOrderItemNote,omitempty"`
	PurOrdPricingElement      []SAPGrowPurOrdPricingElement    `json:"_PurOrdPricingElement,omitempty"`
}

type SAPGrowPurOrdAccountAssignment struct {
	PurchaseOrder           string `json:"PurchaseOrder,omitempty"`
	PurchaseOrderItem       string `json:"PurchaseOrderItem,omitempty"`
	AccountAssignmentNumber string `json:"AccountAssignmentNumber"`
	CostCenter              string `json:"CostCenter"`
}

type SAPGrowPurchaseOrderItemNote struct {
	PurchaseOrder     string `json:"PurchaseOrder,omitempty"`
	PurchaseOrderItem string `json:"PurchaseOrderItem,omitempty"`
	TextObjectType    string `json:"TextObjectType"`
	Language          string `json:"Language"`
	PlainLongText     string `json:"PlainLongText"`
}

type SAPGrowPurOrdPricingElement struct {
	PurchaseOrderItem      string  `json:"PurchaseOrderItem"`
	ConditionType          string  `json:"ConditionType"`
	ConditionRateAmount    float64 `json:"ConditionRateAmount"`
	ConditionRateRatioUnit string  `json:"ConditionRateRatioUnit,omitempty"`
}

type SAPMatDocItemSummary struct {
	MaterialDocument     string  `json:"MaterialDocument"`
	MaterialDocumentYear string  `json:"MaterialDocumentYear"`
	GoodsMovementType    string  `json:"GoodsMovementType"`
	QuantityInEntryUnit  float64 `json:"QuantityInEntryUnit"`
}

// MaximoVendorRecord merepresentasikan baris vendor dari tabel axvendor di SQL Server Maximo
type MaximoVendorRecord struct {
	BPIVendorGroup string `json:"BPIVENDORGROUP" example:"01"`
	Company        string `json:"COMPANY" example:"VND-UATF"`
	CurrencyCode   string `json:"CURRENCYCODE" example:"USD"`
	Name           string `json:"NAME" example:"Vendor UAT Final Testing (2)"`
	OrgID          string `json:"ORGID" example:"BPI"`
	TransID        string `json:"TRANSID,omitempty" example:"1020951"`
	TransSeq       int    `json:"TRANSSEQ,omitempty" example:"1"`
	Action         string `json:"ACTION,omitempty" example:"Add"`
}

// Payload Struktur Target SAP Grow OData v2 Business Partner (A_BusinessPartner Deep Insert)
type SAPGrowBPPayload struct {
	BusinessPartner          string                   `json:"BusinessPartner,omitempty"`
	BusinessPartnerCategory  string                   `json:"BusinessPartnerCategory"`
	BusinessPartnerGrouping  string                   `json:"BusinessPartnerGrouping"`
	OrganizationBPName1      string                   `json:"OrganizationBPName1"`
	OrganizationBPName2      string                   `json:"OrganizationBPName2"`
	OrganizationBPName3      string                   `json:"OrganizationBPName3"`
	OrganizationBPName4      string                   `json:"OrganizationBPName4"`
	SearchTerm1              string                   `json:"SearchTerm1"`
	FormOfAddress            string                   `json:"FormOfAddress"`
	BusinessPartnerIsBlocked bool                     `json:"BusinessPartnerIsBlocked"`
	ToBusinessPartnerRole    []SAPBPRole              `json:"to_BusinessPartnerRole,omitempty"`
	ToBusinessPartnerAddress []SAPBPAddress           `json:"to_BusinessPartnerAddress,omitempty"`
	ToSupplier               *SAPBPSupplier           `json:"to_Supplier,omitempty"`
}

type SAPBPRole struct {
	BusinessPartnerRole string `json:"BusinessPartnerRole"`
}

type SAPBPAddress struct {
	Country  string `json:"Country"`
	Language string `json:"Language"`
}

type SAPBPSupplier struct {
	ToSupplierCompany       []SAPSupplierCompany       `json:"to_SupplierCompany,omitempty"`
	ToSupplierPurchasingOrg []SAPSupplierPurchasingOrg `json:"to_SupplierPurchasingOrg,omitempty"`
}

type SAPSupplierCompany struct {
	CompanyCode           string `json:"CompanyCode"`
	ReconciliationAccount string `json:"ReconciliationAccount"`
}

type SAPSupplierPurchasingOrg struct {
	PurchasingOrganization string `json:"PurchasingOrganization"`
	PurchaseOrderCurrency  string `json:"PurchaseOrderCurrency"`
}

// MaximoReceiptHeader merepresentasikan header penerimaan barang (GRN / Return / Void) dari Maximo
type MaximoReceiptHeader struct {
	BPIGRNNumber    string              `json:"BPIGRNUMBER" example:"2026/GRN-03581"`
	PackingSlipNum  string              `json:"PACKINGSLIPNUM" example:"3215/TSL/09/26"`
	IssueType       string              `json:"ISSUETYPE" example:"RECEIPT"` // RECEIPT, RETURN, VOIDRETURN
	PONum           string              `json:"PONUM" example:"4500016528"`
	ActualDate      string              `json:"ACTUALDATE" example:"2026-10-29"`
	TransDate       string              `json:"TRANSDATE,omitempty"`
	Status          string              `json:"STATUS" example:"COMP"`
	Action          string              `json:"ACTION,omitempty"`
	ReceiptRef      string              `json:"RECEIPTREF,omitempty"`
	RefBPIGRNNumber string              `json:"REF_BPIGRNUMBER,omitempty"`
	Lines           []MaximoReceiptLine `json:"LINES"`
}

// MaximoReceiptLine merepresentasikan baris item penerimaan barang dari Maximo
type MaximoReceiptLine struct {
	POLineNum     int     `json:"POLINENUM" example:"1"`
	ItemNum       string  `json:"ITEMNUM" example:"91-01193"`
	Qty           float64 `json:"QTY" example:"1.00"`
	ReceiptRef    string  `json:"RECEIPTREF,omitempty" example:"48045"`
	MatrecTransID int64   `json:"MATRECTRANSID,omitempty" example:"49303"`
	ConditionCode string  `json:"CONDITIONCODE,omitempty"`
	TransID       int64   `json:"TRANSID,omitempty"`
	TransSeq      int     `json:"TRANSSEQ,omitempty"`
	PONum         string  `json:"PONUM,omitempty"`
}

// SAPGrowMaterialDocumentPayload merepresentasikan struktur OData v2 A_MaterialDocumentHeader Deep Insert
type SAPGrowMaterialDocumentPayload struct {
	MaterialDocumentHeaderText string                    `json:"MaterialDocumentHeaderText"`
	ReferenceDocument          string                    `json:"ReferenceDocument,omitempty"`
	GoodsMovementCode          string                    `json:"GoodsMovementCode"`
	VersionForPrintingSlip     string                    `json:"VersionForPrintingSlip"`
	ToMaterialDocumentItem     []SAPMaterialDocumentItem `json:"to_MaterialDocumentItem"`
}

// SAPMaterialDocumentItem merepresentasikan baris item pergerakan barang di SAP Grow
type SAPMaterialDocumentItem struct {
	Material                    string `json:"Material"`
	Plant                       string `json:"Plant"`
	GoodsMovementType           string `json:"GoodsMovementType"`
	PurchaseOrder               string `json:"PurchaseOrder"`
	PurchaseOrderItem           string `json:"PurchaseOrderItem"`
	GoodsMovementRefDocType     string `json:"GoodsMovementRefDocType"`
	QuantityInEntryUnit         string `json:"QuantityInEntryUnit"`
	GoodsMovementReasonCode     string `json:"GoodsMovementReasonCode,omitempty"`
	InvtryMgmtReferenceDocument string `json:"InvtryMgmtReferenceDocument,omitempty"`
	MaterialDocumentItemText    string `json:"MaterialDocumentItemText,omitempty"`
}

// SAPCancelMaterialDocPayload merepresentasikan parameter function import Cancel
type SAPCancelMaterialDocPayload struct {
	MaterialDocumentYear string `json:"MaterialDocumentYear"`
	MaterialDocument     string `json:"MaterialDocument"`
}

// BPGroupMapping mewakili pemetaan vendor group Maximo ke BusinessPartnerGrouping SAP Grow
type BPGroupMapping struct {
	ID          int       `json:"id"`
	VendorGroup string    `json:"vendor_group"`
	BPGrouping  string    `json:"bp_grouping"`
	Description string    `json:"description"`
	IsActive    bool      `json:"is_active"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ReconAccountMapping mewakili pemetaan vendor group Maximo ke Reconciliation Account & BP Grouping SAP Grow
type ReconAccountMapping struct {
	ID           int       `json:"id"`
	VendorGroup  string    `json:"vendor_group"`
	BPGrouping   string    `json:"bp_grouping"`
	CompanyCode  string    `json:"company_code"`
	ReconAccount string    `json:"recon_account"`
	Description  string    `json:"description"`
	IsActive     bool      `json:"is_active"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// MaterialConfigMapping mewakili pemetaan konfigurasi material/item (Item Type, Item Group, Stock Type)
type MaterialConfigMapping struct {
	ID          int       `json:"id"`
	ConfigType  string    `json:"config_type"`
	MaximoValue string    `json:"maximo_value"`
	SAPValue    string    `json:"sap_value"`
	Description string    `json:"description"`
	IsActive    bool      `json:"is_active"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// TableAPIMapping mewakili pemetaan dinamis dari tabel Maximo ke service API SAP Grow
type TableAPIMapping struct {
	ID                uint      `json:"id"`
	SourceTable       string    `json:"source_table"`
	SourceDesc        string    `json:"source_desc"`
	TargetServiceCode string    `json:"target_service_code"`
	Direction         string    `json:"direction"`
	SyncMode          string    `json:"sync_mode"`
	IsActive          bool      `json:"is_active"`
	Notes             string    `json:"notes"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// Model & Payload untuk Exchange Rate (Kurs Pajak Kemenkeu / Maximo)
type ExchangeRatePeriod struct {
	KMKNumber   string `json:"kmk_number"`
	ValidFrom   string `json:"valid_from"`
	ValidTo     string `json:"valid_to"`
	DocumentURL string `json:"document_url"`
}

type ExchangeRateItem struct {
	CurrencyCode string   `json:"currency_code"`
	CurrencyName string   `json:"currency_name"`
	Rate         float64  `json:"rate"`
	Unit         int      `json:"unit"`
	RatePerUnit  string   `json:"rate_per_unit"`
	Change       *float64 `json:"change"`
}

type MaximoExchangeRatePayload struct {
	Event  string             `json:"event"`
	SentAt string             `json:"sent_at"`
	Source string             `json:"source"`
	Period ExchangeRatePeriod `json:"period"`
	Rates  []ExchangeRateItem `json:"rates"`
}

type ExchangeRateRecord struct {
	ID           int64     `json:"id"`
	Event        string    `json:"event"`
	Source       string    `json:"source"`
	KMKNumber    string    `json:"kmk_number"`
	ValidFrom    string    `json:"valid_from"`
	ValidTo      string    `json:"valid_to"`
	DocumentURL  string    `json:"document_url"`
	CurrencyCode string    `json:"currency_code"`
	CurrencyName string    `json:"currency_name"`
	Rate         float64   `json:"rate"`
	Unit         int       `json:"unit"`
	RatePerUnit  float64   `json:"rate_per_unit"`
	Change       *float64  `json:"change"`
	CreatedAt    time.Time `json:"created_at"`
}

// UOMMapping mewakili pemetaan satuan ukuran dari Maximo ke SAP Grow
type UOMMapping struct {
	ID          int       `json:"id"`
	MaximoUOM   string    `json:"maximo_uom"`
	SAPUOM      string    `json:"sap_uom"`
	Description string    `json:"description"`
	IsActive    bool      `json:"is_active"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// MaximoItemPayload mewakili baris item mentah dari IBM Maximo (tabel axitem)
type MaximoItemPayload struct {
	ItemNum     string `json:"ITEMNUM"`
	ItemGroupID string `json:"ITEMGROUPID"`
	ItemType    string `json:"ITEMTYPE"`
	StockType   string `json:"STOCKTYPE"`
	IssueUnit   string `json:"ISSUEUNIT"`
	OrderUnit   string `json:"ORDERUNIT"`
	Description string `json:"DESCRIPTION"`
	Status      string `json:"STATUS,omitempty"`
	SiteID      string `json:"SITEID,omitempty"`
	Location    string `json:"LOCATION,omitempty"`
	TransID     int64  `json:"TRANSID,omitempty"`
	Action      string `json:"ACTION,omitempty"`
}

// SAPProductPayload mewakili payload OData v2 A_Product untuk Deep Insert ke SAP Grow
type SAPProductPayload struct {
	Product                   string                        `json:"Product"`
	ProductType               string                        `json:"ProductType"`
	CrossPlantStatus          string                        `json:"CrossPlantStatus"`
	CrossPlantStatusValidity  *string                       `json:"CrossPlantStatusValidityDate"`
	IsMarkedForDeletion       bool                          `json:"IsMarkedForDeletion"`
	PurchaseOrderQuantityUnit string                        `json:"PurchaseOrderQuantityUnit,omitempty"`
	ProductGroup              string                        `json:"ProductGroup"`
	BaseUnit                  string                        `json:"BaseUnit"`
	ToDescription             SAPProductDescriptionWrapper  `json:"to_Description"`
	ToPlant                   SAPProductPlantWrapper        `json:"to_Plant"`
	ToProductUnitsOfMeasure   *SAPProductUOMWrapper         `json:"to_ProductUnitsOfMeasure,omitempty"`
	ToValuation               SAPProductValuationWrapper    `json:"to_Valuation"`
}

type SAPProductDescriptionWrapper struct {
	Results []SAPProductDescription `json:"results"`
}

type SAPProductDescription struct {
	Product            string `json:"Product"`
	Language           string `json:"Language"`
	ProductDescription string `json:"ProductDescription"`
}

type SAPProductPlantWrapper struct {
	Results []SAPProductPlant `json:"results"`
}

type SAPProductPlant struct {
	Product string `json:"Product"`
	Plant   string `json:"Plant"`
}

type SAPProductUOMWrapper struct {
	Results []SAPProductUOM `json:"results"`
}

type SAPProductUOM struct {
	Product             string `json:"Product"`
	AlternativeUnit     string `json:"AlternativeUnit"`
	BaseUnit            string `json:"BaseUnit"`
	QuantityNumerator   string `json:"QuantityNumerator"`
	QuantityDenominator string `json:"QuantityDenominator"`
}

type SAPProductValuationWrapper struct {
	Results []SAPProductValuation `json:"results"`
}

type SAPProductValuation struct {
	Product                     string `json:"Product"`
	ValuationArea               string `json:"ValuationArea"`
	ValuationClass              string `json:"ValuationClass"`
	InventoryValuationProcedure string `json:"InventoryValuationProcedure"`
}
