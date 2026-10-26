package repository

import (
	"errors"
	"middleware-panel/models"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type UserRepository interface {
	GetAll() ([]models.User, error)
	GetByID(id uint) (*models.User, error)
	GetByUsername(username string) (*models.User, error)
	Create(u *models.User) error
	Update(u *models.User, updatePassword bool) error
	ToggleActive(id uint) error
	Delete(id uint) error
	UpdateLastLogin(id uint) error
	VerifyPassword(user *models.User, password string) bool
	SeedAdminUser() error
}

type userRepo struct {
	db *gorm.DB
}

func NewUserRepository(db *gorm.DB) UserRepository {
	return &userRepo{db: db}
}

func (r *userRepo) GetAll() ([]models.User, error) {
	var users []models.User
	err := r.db.Order("id asc").Find(&users).Error
	return users, err
}

func (r *userRepo) GetByID(id uint) (*models.User, error) {
	var user models.User
	err := r.db.First(&user, id).Error
	return &user, err
}

func (r *userRepo) GetByUsername(username string) (*models.User, error) {
	var user models.User
	err := r.db.Where("LOWER(username) = LOWER(?)", strings.TrimSpace(username)).First(&user).Error
	return &user, err
}

func (r *userRepo) Create(u *models.User) error {
	u.Username = strings.TrimSpace(u.Username)
	var existing models.User
	if err := r.db.Where("LOWER(username) = LOWER(?)", u.Username).First(&existing).Error; err == nil {
		return errors.New("Username sudah digunakan")
	}

	if u.AllowedMenus == "" {
		switch u.Role {
		case "admin":
			u.AllowedMenus = "*"
		case "operator":
			u.AllowedMenus = "dashboard,swagger,exchange_rates,uom_mapping,scheduler"
		case "auditor":
			u.AllowedMenus = "dashboard,exchange_rates,uom_mapping"
		default:
			u.AllowedMenus = "dashboard"
		}
	}

	// Hash password menggunakan bcrypt
	if u.Password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(u.Password), bcrypt.DefaultCost)
		if err == nil {
			u.Password = string(hash)
		}
	}

	return r.db.Create(u).Error
}

func (r *userRepo) Update(u *models.User, updatePassword bool) error {
	updates := map[string]interface{}{
		"full_name":     u.FullName,
		"email":         u.Email,
		"role":          u.Role,
		"allowed_menus": u.AllowedMenus,
		"is_active":     u.IsActive,
		"updated_at":    time.Now(),
	}

	if updatePassword && u.Password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(u.Password), bcrypt.DefaultCost)
		if err == nil {
			updates["password"] = string(hash)
		} else {
			updates["password"] = u.Password
		}
	}

	return r.db.Model(&models.User{}).Where("id = ?", u.ID).Updates(updates).Error
}

func (r *userRepo) ToggleActive(id uint) error {
	var user models.User
	if err := r.db.First(&user, id).Error; err != nil {
		return err
	}
	return r.db.Model(&user).Update("is_active", !user.IsActive).Error
}

func (r *userRepo) Delete(id uint) error {
	return r.db.Delete(&models.User{}, id).Error
}

func (r *userRepo) UpdateLastLogin(id uint) error {
	now := time.Now()
	return r.db.Model(&models.User{}).Where("id = ?", id).Update("last_login_at", &now).Error
}

func (r *userRepo) VerifyPassword(user *models.User, password string) bool {
	if user == nil || user.Password == "" {
		return false
	}

	// 1. Coba bcrypt compare
	err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password))
	if err == nil {
		return true
	}

	// 2. Fallback plain text match (misal seed awal belum ter-hash)
	if user.Password == password {
		// Otomatis upgrade ke bcrypt hash
		if hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost); err == nil {
			_ = r.db.Model(user).Update("password", string(hash))
		}
		return true
	}

	return false
}

func (r *userRepo) SeedAdminUser() error {
	var count int64
	r.db.Model(&models.User{}).Count(&count)
	if count > 0 {
		return nil
	}

	adminUser := os.Getenv("PANEL_ADMIN_USER")
	if adminUser == "" {
		adminUser = "admin"
	}
	adminPass := os.Getenv("PANEL_ADMIN_PASS")
	if adminPass == "" {
		adminPass = "admin"
	}

	hash, _ := bcrypt.GenerateFromPassword([]byte(adminPass), bcrypt.DefaultCost)
	defaultAdmin := models.User{
		Username:     adminUser,
		Password:     string(hash),
		FullName:     "System Administrator",
		Email:        "admin@middleware.local",
		Role:         "admin",
		AllowedMenus: "*",
		IsActive:     true,
	}

	return r.db.Create(&defaultAdmin).Error
}
