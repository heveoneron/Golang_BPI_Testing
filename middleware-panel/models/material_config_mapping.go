package models

import "time"

type MaterialConfigMapping struct {
	ID                 uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	ConfigType         string    `gorm:"column:config_type;type:varchar(50);not null" json:"config_type"` // ITEM_GROUP, ITEM_TYPE, STOCK_TYPE
	MaximoValue        string    `gorm:"column:maximo_value;type:varchar(50);not null" json:"maximo_value"`
	SAPValue           string    `gorm:"column:sap_value;type:varchar(50);not null" json:"sap_value"`
	InventoryPostingGL string    `gorm:"column:inventory_posting_gl;type:varchar(50)" json:"inventory_posting_gl"`
	GoodsIssueGL       string    `gorm:"column:goods_issue_gl;type:varchar(50)" json:"goods_issue_gl"`
	StockAdjGL         string    `gorm:"column:stock_adj_gl;type:varchar(50)" json:"stock_adj_gl"`
	CostCenter         string    `gorm:"column:cost_center;type:varchar(50)" json:"cost_center"`
	Description        string    `gorm:"column:description;type:varchar(255)" json:"description"`
	IsActive           bool      `gorm:"column:is_active;default:true" json:"is_active"`
	CreatedAt          time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt          time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

func (MaterialConfigMapping) TableName() string {
	return "material_config_mappings"
}

