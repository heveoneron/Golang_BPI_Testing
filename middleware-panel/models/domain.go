package models

import "time"

// --- EXISTING MODELS ---
type AdminUser struct {
	ID       uint   `gorm:"primaryKey"`
	Username string `gorm:"unique"`
	Password string
}

// 1. Struct Baru untuk tabel documents
type Document struct {
	DocType            string     `gorm:"primaryKey;not null;column:doc_type;type:text"`
	DocumentNo         string     `gorm:"primaryKey;not null;column:document_no;type:text"`
	FlowType           string     `gorm:"primaryKey;not null;column:flow_type;type:text"`
	ParentDocType      *string    `gorm:"column:parent_doc_type;type:text"` // Menggunakan pointer untuk mengizinkan nilai NULL
	ParentDocumentNo   *string    `gorm:"column:parent_document_no;type:text"`
	GroupID            string     `gorm:"column:group_id;type:text;not null"`
	Payload            string     `gorm:"column:raw_payload;type:jsonb;not null"`
	SAPPayload         *string    `gorm:"column:sap_payload;type:jsonb"`
	Status             string     `gorm:"column:status;type:text;not null"`
	ErrorMessage       *string    `gorm:"column:error_message;type:text"`
	CreatedAt          time.Time  `gorm:"column:created_at;type:timestamp;not null"`
	UpdatedAt          time.Time  `gorm:"column:updated_at;type:timestamp;not null"`
	DocumentDate       *time.Time `gorm:"column:document_date;type:timestamp"`
	BlockedReason      *string    `gorm:"column:blocked_reason;type:text"`
	RetryCount         int        `gorm:"column:retry_count;type:int"`
	LastAttemptAt      *time.Time `gorm:"column:last_attempt_at;type:timestamp"`
	WorkflowInstanceID *string    `gorm:"column:workflow_instance_id;type:text"`
	SAPDocumentNo      *string    `gorm:"column:sap_document_no;type:text"`

	// --- FIELD VIRTUAL UNTUK UI ---
	// Tidak ada di struktur tabel asli, diisi lewat Sub-Query / Select tambahan
	// pada query GORM. Kolom-kolom ini TIDAK pernah ditulis ke database (gorm:"->"
	// artinya read-only dari sisi GORM).

	// Apakah dokumen ini (kalau dia ROOT) punya child lain dalam group_id yang sama.
	// Untuk baris CHILD, nilai ini selalu false (diisi via CASE WHEN di repository).
	HasChildren bool `gorm:"->;column:has_children"`

	// Status milik parent document (versi pointer, nullable), diisi lewat
	// JOIN/subquery saat ambil children lewat GetChildDocuments. Dipakai oleh
	// JS (createChildRowHTML) lewat JSON response GetChildrenAPI.
	ParentStatus *string `gorm:"->;column:parent_status"`

	// Sama seperti ParentStatus, tapi NON-POINTER (selalu string, default ""
	// kalau NULL/tidak ada parent). Field ini KHUSUS dipakai oleh Go template
	// (dashboard.html) karena html/template tidak punya fungsi dereference
	// pointer bawaan -- membandingkan *string langsung di template ({{ if ne
	// .ParentStatus "success" }}) akan error karena .ParentStatus bertipe
	// *string, bukan string. Diisi lewat COALESCE(..., '') di repository,
	// supaya NULL otomatis jadi "" dan aman dibandingkan dengan ne/eq di template.
	ParentStatusStr string `gorm:"->;column:parent_status_str"`

	// Nomor dokumen parent, diisi HANYA kalau baris ini adalah CHILD yang ditampilkan
	// langsung sebagai baris utama di tabel (hasil filter search/status/docType yang
	// match ke child). Dipakai frontend untuk menampilkan info "Parent: XXX".
	// Bernilai NULL kalau baris ini ROOT.
	ParentDisplayNo *string `gorm:"->;column:parent_display_no"`
}

func (Document) TableName() string {
	return "documents"
}

type TransactionContext struct {
	TransactionType string      `json:"transactionType"`
	TraceId         string      `json:"traceId"`
	Data            interface{} `json:"data"`
}

type ContextList struct {
	List []TransactionContext `json:"list"`
}

type BotAPIRequest struct {
	DefinitionId string      `json:"definitionId"`
	Context      ContextList `json:"context"`
}

type SyncBotRequest struct {
	DocType    string      `json:"doc_type"`
	DocumentNo string      `json:"document_no"`
	EntryNo    string      `json:"entry_no"`
	Payload    interface{} `json:"payload"`
}

// BotAPIResponse merepresentasikan response dari Bot API/BTP.
// Contoh response sukses:
//
//	{
//	  "status": "success",
//	  "message": "Document sent to BPA successfully",
//	  "data": {
//	    "doc_trace_id": "DOC-MMMSupplierC-BP001",
//	    "workflow_instance_id": "c576457e-65a8-4904-9029-64dda669fba3"
//	  }
//	}
type BotAPIResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Data    struct {
		DocTraceID         string `json:"doc_trace_id"`
		WorkflowInstanceID string `json:"workflow_instance_id"`
	} `json:"data"`
}
