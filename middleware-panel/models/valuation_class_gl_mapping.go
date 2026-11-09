package models

import "time"

// ValuationClassGLMapping merepresentasikan model GORM untuk tabel valuation_class_gl_mappings
// sesuai Enhancement Specification DJ-MM-ENH-ZME003
type ValuationClassGLMapping struct {
	ID                 uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	ValuationClass     string    `gorm:"column:valuation_class;type:varchar(20);uniqueIndex" json:"valuation_class"`
	InventoryPostingGL string    `gorm:"column:inventory_posting_gl;type:varchar(30)" json:"inventory_posting_gl"`
	GoodsIssueGL       string    `gorm:"column:goods_issue_gl;type:varchar(30)" json:"goods_issue_gl"`
	StockAdjGL         string    `gorm:"column:stock_adj_gl;type:varchar(30)" json:"stock_adj_gl"`
	CostCenter         string    `gorm:"column:cost_center;type:varchar(30)" json:"cost_center"`
	Description        string    `gorm:"column:description;type:varchar(100)" json:"description"`
	IsActive           bool      `gorm:"column:is_active;default:true" json:"is_active"`
	CreatedAt          time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt          time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

func (ValuationClassGLMapping) TableName() string {
	return "valuation_class_gl_mappings"
}
