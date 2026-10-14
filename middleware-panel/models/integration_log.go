package models

import "time"

// IntegrationLog merepresentasikan jejak audit integrasi antar sistem (Maximo <-> Middleware <-> SAP)
type IntegrationLog struct {
	ID           uint      `gorm:"primaryKey;autoIncrement;column:id" json:"id"`
	TraceID      string    `gorm:"column:trace_id;type:varchar(100);index" json:"trace_id"`
	SourceSystem string    `gorm:"column:source_system;type:varchar(50)" json:"source_system"`
	TargetSystem string    `gorm:"column:target_system;type:varchar(50)" json:"target_system"`
	DocumentType string    `gorm:"column:document_type;type:varchar(50);index" json:"document_type"`
	DocumentNo   string    `gorm:"column:document_no;type:varchar(100);index" json:"document_no"`
	Status       string    `gorm:"column:status;type:varchar(30)" json:"status"` // SUCCESS, FAILED, SENT, RECEIVED
	ErrorMessage *string   `gorm:"column:error_message;type:text" json:"error_message"`
	Metadata     string    `gorm:"column:metadata;type:text" json:"metadata"`
	CreatedAt    time.Time `gorm:"column:created_at;type:timestamp" json:"created_at"`
}

func (IntegrationLog) TableName() string {
	return "integration_logs"
}
