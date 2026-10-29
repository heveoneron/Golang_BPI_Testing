package models

import "time"

type ReconAccountMapping struct {
	ID           uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	VendorGroup  string    `gorm:"column:vendor_group;type:varchar(50);unique;not null" json:"vendor_group"`
	BPGrouping   string    `gorm:"column:bp_grouping;type:varchar(50);default:'Z001'" json:"bp_grouping"`
	CompanyCode  string    `gorm:"column:company_code;type:varchar(20);default:'5110'" json:"company_code"`
	ReconAccount string    `gorm:"column:recon_account;type:varchar(50);not null" json:"recon_account"`
	Description  string    `gorm:"column:description;type:varchar(255)" json:"description"`
	IsActive     bool      `gorm:"column:is_active;default:true" json:"is_active"`
	CreatedAt    time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt    time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

func (ReconAccountMapping) TableName() string {
	return "recon_account_mappings"
}
