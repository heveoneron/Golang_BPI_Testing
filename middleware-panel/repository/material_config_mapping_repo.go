package repository

import (
	"middleware-panel/models"
	"strings"

	"gorm.io/gorm"
)

type MaterialConfigMappingRepository interface {
	GetAll(configType string) ([]models.MaterialConfigMapping, error)
	GetByID(id uint) (*models.MaterialConfigMapping, error)
	Save(m *models.MaterialConfigMapping) error
	ToggleActive(id uint) error
	Delete(id uint) error
}

type materialConfigMappingRepo struct {
	db *gorm.DB
}

func NewMaterialConfigMappingRepository(db *gorm.DB) MaterialConfigMappingRepository {
	return &materialConfigMappingRepo{db: db}
}

func (r *materialConfigMappingRepo) GetAll(configType string) ([]models.MaterialConfigMapping, error) {
	var list []models.MaterialConfigMapping
	q := r.db.Order("config_type asc, maximo_value asc")
	if configType != "" {
		q = q.Where("config_type = ?", configType)
	}
	err := q.Find(&list).Error
	return list, err
}

func (r *materialConfigMappingRepo) GetByID(id uint) (*models.MaterialConfigMapping, error) {
	var item models.MaterialConfigMapping
	err := r.db.First(&item, id).Error
	return &item, err
}

func (r *materialConfigMappingRepo) Save(m *models.MaterialConfigMapping) error {
	m.ConfigType = strings.ToUpper(strings.TrimSpace(m.ConfigType))
	m.MaximoValue = strings.TrimSpace(m.MaximoValue)
	m.SAPValue = strings.TrimSpace(m.SAPValue)

	if m.ID > 0 {
		return r.db.Save(m).Error
	}

	var existing models.MaterialConfigMapping
	err := r.db.Where("config_type = ? AND UPPER(maximo_value) = UPPER(?)", m.ConfigType, m.MaximoValue).First(&existing).Error
	if err == nil {
		m.ID = existing.ID
		return r.db.Model(&existing).Updates(m).Error
	}

	return r.db.Create(m).Error
}

func (r *materialConfigMappingRepo) ToggleActive(id uint) error {
	var item models.MaterialConfigMapping
	if err := r.db.First(&item, id).Error; err != nil {
		return err
	}
	item.IsActive = !item.IsActive
	return r.db.Model(&item).Update("is_active", item.IsActive).Error
}

func (r *materialConfigMappingRepo) Delete(id uint) error {
	return r.db.Delete(&models.MaterialConfigMapping{}, id).Error
}

