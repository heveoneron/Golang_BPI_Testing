package models

import "time"

type MaterialGLMapping struct {
	ID          uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	CtgItem     string    `gorm:"column:ctg_item;type:varchar(100)" json:"ctg_item"`
	GlAccount   string    `gorm:"column:gl_account;type:varchar(50)" json:"gl_account"`
	Description string    `gorm:"column:description;type:text" json:"description"`
	IsActive    bool      `gorm:"column:is_active;default:true" json:"is_active"`
	CreatedAt   time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

func (MaterialGLMapping) TableName() string {
	return "material_gl_mappings"
}
