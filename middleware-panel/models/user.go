package models

import (
	"strings"
	"time"

	"gorm.io/gorm"
)

type User struct {
	gorm.Model
	Username     string     `gorm:"type:varchar(50);uniqueIndex;not null" json:"username"`
	Password     string     `gorm:"type:varchar(255);not null" json:"-"`
	FullName     string     `gorm:"type:varchar(100)" json:"full_name"`
	Email        string     `gorm:"type:varchar(100)" json:"email"`
	Role         string     `gorm:"type:varchar(30);default:'operator'" json:"role"` // 'admin', 'operator', 'auditor', 'custom'
	AllowedMenus string     `gorm:"type:text;default:'*'" json:"allowed_menus"`      // '*' or comma-separated list
	IsActive     bool       `gorm:"default:true" json:"is_active"`
	LastLoginAt  *time.Time `json:"last_login_at"`
}

func (User) TableName() string {
	return "users"
}

// CanAccessMenu mengecek apakah user memiliki hak akses ke menu tertentu
func (u User) CanAccessMenu(menu string) bool {
	if u.Role == "admin" || u.AllowedMenus == "*" || u.AllowedMenus == "" {
		return true
	}
	menus := strings.Split(u.AllowedMenus, ",")
	for _, m := range menus {
		if strings.TrimSpace(m) == strings.TrimSpace(menu) {
			return true
		}
	}
	return false
}

// GetMenuCount menghitung jumlah menu yang dapat diakses
func (u User) GetMenuCount() int {
	if u.Role == "admin" || u.AllowedMenus == "*" || u.AllowedMenus == "" {
		return 10 // Total semua menu yang ada
	}
	menus := strings.Split(u.AllowedMenus, ",")
	count := 0
	for _, m := range menus {
		if strings.TrimSpace(m) != "" {
			count++
		}
	}
	return count
}
