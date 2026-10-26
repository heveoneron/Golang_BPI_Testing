package repository

import (
	"middleware-panel/models"
	"strings"

	"gorm.io/gorm"
)

type UOMMappingRepository interface {
	GetAll() ([]models.UOMMapping, error)
	GetByID(id uint) (*models.UOMMapping, error)
	Save(m *models.UOMMapping) error
	ToggleActive(id uint) error
	Delete(id uint) error
}

type uomMappingRepo struct {
	db *gorm.DB
}

func NewUOMMappingRepository(db *gorm.DB) UOMMappingRepository {
	return &uomMappingRepo{db: db}
}

func (r *uomMappingRepo) GetAll() ([]models.UOMMapping, error) {
	var list []models.UOMMapping
	err := r.db.Order("maximo_uom asc").Find(&list).Error
	return list, err
}

func (r *uomMappingRepo) GetByID(id uint) (*models.UOMMapping, error) {
	var item models.UOMMapping
	err := r.db.First(&item, id).Error
	return &item, err
}

func (r *uomMappingRepo) Save(m *models.UOMMapping) error {
	m.MaximoUOM = strings.ToUpper(strings.TrimSpace(m.MaximoUOM))
	m.SAPUOM = strings.ToUpper(strings.TrimSpace(m.SAPUOM))

	if m.ID > 0 {
		return r.db.Save(m).Error
	}

	var existing models.UOMMapping
	err := r.db.Where("UPPER(maximo_uom) = UPPER(?)", m.MaximoUOM).First(&existing).Error
	if err == nil {
		m.ID = existing.ID
		return r.db.Model(&existing).Updates(m).Error
	}

	return r.db.Create(m).Error
}

func (r *uomMappingRepo) ToggleActive(id uint) error {
	var item models.UOMMapping
	if err := r.db.First(&item, id).Error; err != nil {
		return err
	}
	item.IsActive = !item.IsActive
	return r.db.Model(&item).Update("is_active", item.IsActive).Error
}

func (r *uomMappingRepo) Delete(id uint) error {
	return r.db.Delete(&models.UOMMapping{}, id).Error
}
