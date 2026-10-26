package models

import "time"

type UOMMapping struct {
	ID          uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	MaximoUOM   string    `gorm:"column:maximo_uom;type:varchar(50);unique;not null" json:"maximo_uom"`
	SAPUOM      string    `gorm:"column:sap_uom;type:varchar(50);not null" json:"sap_uom"`
	Description string    `gorm:"column:description;type:varchar(255)" json:"description"`
	IsActive    bool      `gorm:"column:is_active;default:true" json:"is_active"`
	CreatedAt   time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

func (UOMMapping) TableName() string {
	return "uom_mappings"
}
