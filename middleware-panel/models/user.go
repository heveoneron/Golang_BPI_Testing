package models

import (
	"time"

	"gorm.io/gorm"
)

type User struct {
	gorm.Model
	Username    string     `gorm:"type:varchar(50);uniqueIndex;not null" json:"username"`
	Password    string     `gorm:"type:varchar(255);not null" json:"-"`
	FullName    string     `gorm:"type:varchar(100)" json:"full_name"`
	Email       string     `gorm:"type:varchar(100)" json:"email"`
	Role        string     `gorm:"type:varchar(30);default:'operator'" json:"role"` // 'admin', 'operator', 'auditor'
	IsActive    bool       `gorm:"default:true" json:"is_active"`
	LastLoginAt *time.Time `json:"last_login_at"`
}

func (User) TableName() string {
	return "users"
}
