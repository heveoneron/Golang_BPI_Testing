package models

import "gorm.io/gorm"

type TableAPIMapping struct {
	gorm.Model
	SourceTable       string `gorm:"type:varchar(100);uniqueIndex;not null" json:"source_table"` // e.g. ITEM, PO, COMPANIES, MATRECTRANS, PR, INVOICE
	SourceDesc        string `gorm:"type:varchar(255)" json:"source_desc"`                       // e.g. "Maximo Master Item & Material"
	TargetServiceCode string `gorm:"type:varchar(50);not null" json:"target_service_code"`      // FK/code to sap_endpoints: PRODUCT, PO, BP
	Direction         string `gorm:"type:varchar(30);default:'OUTBOUND'" json:"direction"`       // OUTBOUND (Maximo -> SAP), INBOUND (SAP -> Maximo)
	SyncMode          string `gorm:"type:varchar(30);default:'BATCH_SCHEDULE'" json:"sync_mode"` // BATCH_SCHEDULE, REALTIME, MANUAL
	IsActive          bool   `gorm:"default:true" json:"is_active"`
	Notes             string `gorm:"type:text" json:"notes"`
}

func (TableAPIMapping) TableName() string {
	return "table_api_mappings"
}
