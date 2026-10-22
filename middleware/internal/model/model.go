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
	PONum        string         `json:"PONUM" example:"PO-BPI-2026-001"`
	Vendor       string         `json:"VENDOR" example:"VEND-PLTU-01"`
	OrderDate    string         `json:"ORDERDATE" example:"2026-10-12"`
	CurrencyCode string         `json:"CURRENCYCODE" example:"IDR"`
	Status       string         `json:"STATUS" example:"APPR"`
	RevisionNum  int            `json:"REVISIONNUM" example:"0"`
	Lines        []MaximoPOLine `json:"LINES"`
}

type MaximoPOLine struct {
	POLineNum    int     `json:"POLINENUM" example:"1"`
	PRNum        string  `json:"PRNUM,omitempty" example:"PR-2026-001"`
	PRLineNum    int     `json:"PRLINENUM,omitempty" example:"1"`
	LineType     string  `json:"LINETYPE,omitempty" example:"ITEM"`
	ItemNum      string  `json:"ITEMNUM" example:"BEARING-TURBINE-6204"`
	OrderUnit    string  `json:"ORDERUNIT" example:"PCS"`
	OrderQty     float64 `json:"QTY" example:"5"`
	UnitCost     float64 `json:"UNITCOST" example:"150000"`
	BudgetCode   string  `json:"BUDGETCODE" example:"10081"`
	CostCenter   string  `json:"DEPARTMENT" example:"3000"`
	MainAccount  string  `json:"MAINACCOUNT" example:"513043"`
	TaxCodePPN   string  `json:"TAXCODE1,omitempty" example:"V1"`
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
	PurchaseOrderType       string          `json:"PurchaseOrderType"`
	CompanyCode             string          `json:"CompanyCode"`
	PurchasingOrganization  string          `json:"PurchasingOrganization"`
	PurchasingGroup         string          `json:"PurchasingGroup"`
	Supplier                string          `json:"Supplier"`
	DocumentCurrency        string          `json:"DocumentCurrency"`
	Language                string          `json:"Language"`
	PurchaseOrderDate       string          `json:"PurchaseOrderDate"`
	PaymentTerms            string          `json:"PaymentTerms"`
	IncotermsClassification string          `json:"IncotermsClassification"`
	IncotermsLocation1      string          `json:"IncotermsLocation1"`
	PurchaseOrderItem       []SAPGrowPOItem `json:"_PurchaseOrderItem"`
}

type SAPGrowPOItem struct {
	PurchaseOrderItem         string `json:"PurchaseOrderItem"`
	PurchaseOrderItemText     string `json:"PurchaseOrderItemText"`
	Material                  string `json:"Material"`
	Plant                     string `json:"Plant"`
	StorageLocation           string `json:"StorageLocation"`
	OrderQuantity             string `json:"OrderQuantity"`
	PurchaseOrderQuantityUnit string `json:"PurchaseOrderQuantityUnit"`
	NetPriceAmount            string `json:"NetPriceAmount"`
	DocumentCurrency          string `json:"DocumentCurrency"`
	NetPriceQuantity          string `json:"NetPriceQuantity"`
	OrderPriceUnit            string `json:"OrderPriceUnit"`
}

// Payload Struktur Target SAP Grow OData v2 Business Partner
type SAPGrowBPPayload struct {
	Customer                string `json:"Customer,omitempty"`
	Supplier                string `json:"Supplier,omitempty"`
	BusinessPartnerCategory string `json:"BusinessPartnerCategory"`
	BusinessPartnerFullName string `json:"BusinessPartnerFullName"`
	BusinessPartnerName     string `json:"BusinessPartnerName"`
	LegalForm               string `json:"LegalForm,omitempty"`
	OrganizationBPName1     string `json:"OrganizationBPName1,omitempty"`
	SearchTerm1             string `json:"SearchTerm1,omitempty"`
}
