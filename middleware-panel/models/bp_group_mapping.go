package models

import "time"

type BPGroupMapping struct {
	ID          uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	VendorGroup string    `gorm:"column:vendor_group;type:varchar(50);unique;not null" json:"vendor_group"`
	BPGrouping  string    `gorm:"column:bp_grouping;type:varchar(50);not null" json:"bp_grouping"`
	Description string    `gorm:"column:description;type:varchar(255)" json:"description"`
	IsActive    bool      `gorm:"column:is_active;default:true" json:"is_active"`
	CreatedAt   time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

func (BPGroupMapping) TableName() string {
	return "bp_group_mappings"
}
