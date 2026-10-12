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
	PONum        string         `json:"PONUM"`
	Vendor       string         `json:"VENDOR"`
	OrderDate    string         `json:"ORDERDATE"`
	CurrencyCode string         `json:"CURRENCYCODE"`
	Status       string         `json:"STATUS"`
	RevisionNum  int            `json:"REVISIONNUM"`
	Lines        []MaximoPOLine `json:"LINES"`
}

type MaximoPOLine struct {
	POLineNum    int     `json:"POLINENUM"`
	PRNum        string  `json:"PRNUM"`
	PRLineNum    int     `json:"PRLINENUM"`
	LineType     string  `json:"LINETYPE"`
	ItemNum      string  `json:"ITEMNUM"`
	OrderUnit    string  `json:"ORDERUNIT"`
	OrderQty     float64 `json:"QTY"`
	UnitCost     float64 `json:"UNITCOST"`
	BudgetCode   string  `json:"BUDGETCODE"`
	CostCenter   string  `json:"DEPARTMENT"`
	MainAccount  string  `json:"MAINACCOUNT"`
	TaxCodePPN   string  `json:"TAXCODE1"`
}

// Payload Struktur Target SAP Grow OData
type SAPGrowPOPayload struct {
	CompanyCode             string             `json:"CompanyCode"`
	PurchasingOrganization  string             `json:"PurchasingOrganization"`
	PurchasingGroup         string             `json:"PurchasingGroup"`
	Supplier                string             `json:"Supplier"`
	PurchaseOrderDate       string             `json:"PurchaseOrderDate"`
	DocumentCurrency        string             `json:"DocumentCurrency"`
	CorrespncExternalRefDoc string             `json:"CorrespncExternalReferenceDoc"` // Maximo PO Num
	ToPurchaseOrderItem     []SAPGrowPOItem    `json:"to_PurchaseOrderItem"`
}

type SAPGrowPOItem struct {
	PurchaseOrderItem         string  `json:"PurchaseOrderItem"`
	Material                  string  `json:"Material"`
	OrderQuantity             string  `json:"OrderQuantity"`
	PurchaseOrderQuantityUnit string  `json:"PurchaseOrderQuantityUnit"`
	NetPriceAmount            string  `json:"NetPriceAmount"`
	AccountAssignmentCategory string  `json:"AccountAssignmentCategory"` // 'K' untuk Cost Center
	CostCenter                string  `json:"CostCenter"`
	GLAccount                 string  `json:"GLAccount"`
}
