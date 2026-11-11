package repository

import (
	"errors"
	"middleware-panel/models"
	"strings"
	"time"

	"gorm.io/gorm"
)

type DefaultValueRepository interface {
	GetAll() ([]models.DefaultValue, error)
	UpdateValue(key, value, user string) error
	Reset(key, user string) error
}

type defaultValueRepo struct {
	db *gorm.DB
}

func NewDefaultValueRepository(db *gorm.DB) DefaultValueRepository {
	return &defaultValueRepo{db: db}
}

func (r *defaultValueRepo) GetAll() ([]models.DefaultValue, error) {
	var list []models.DefaultValue
	err := r.db.Order("category asc, key asc").Find(&list).Error
	return list, err
}

// UpdateValue hanya mengubah key yang sudah ada (key didaftarkan oleh Engine), agar salah ketik
// tidak membuat key yang tidak pernah dibaca.
func (r *defaultValueRepo) UpdateValue(key, value, user string) error {
	res := r.db.Model(&models.DefaultValue{}).Where("key = ?", strings.TrimSpace(key)).
		Updates(map[string]interface{}{"value": strings.TrimSpace(value), "updated_by": user, "updated_at": time.Now()})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.New("key tidak terdaftar: " + key)
	}
	return nil
}

func (r *defaultValueRepo) Reset(key, user string) error {
	res := r.db.Model(&models.DefaultValue{}).Where("key = ?", strings.TrimSpace(key)).
		Updates(map[string]interface{}{"value": gorm.Expr("default_value"), "updated_by": user, "updated_at": time.Now()})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.New("key tidak terdaftar: " + key)
	}
	return nil
}
