package models

import "time"

// DefaultValue adalah satu nilai default transform Maximo -> SAP. Tabel dibuat & di-seed oleh Engine
// (middleware/internal/repository/default_values.go); Panel hanya mengubah kolom value.
type DefaultValue struct {
	Key          string    `gorm:"column:key;primaryKey" json:"key"`
	Value        string    `gorm:"column:value" json:"value"`
	DefaultValue string    `gorm:"column:default_value" json:"default_value"`
	Category     string    `gorm:"column:category" json:"category"`
	Description  string    `gorm:"column:description" json:"description"`
	UpdatedBy    string    `gorm:"column:updated_by" json:"updated_by"`
	UpdatedAt    time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (DefaultValue) TableName() string {
	return "sap_default_values"
}
