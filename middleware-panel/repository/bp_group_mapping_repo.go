package repository

import (
	"middleware-panel/models"
	"strings"

	"gorm.io/gorm"
)

type BPGroupMappingRepository interface {
	GetAll() ([]models.BPGroupMapping, error)
	GetByID(id uint) (*models.BPGroupMapping, error)
	Save(m *models.BPGroupMapping) error
	ToggleActive(id uint) error
	Delete(id uint) error
}

type bpGroupMappingRepo struct {
	db *gorm.DB
}

func NewBPGroupMappingRepository(db *gorm.DB) BPGroupMappingRepository {
	return &bpGroupMappingRepo{db: db}
}

func (r *bpGroupMappingRepo) GetAll() ([]models.BPGroupMapping, error) {
	var list []models.BPGroupMapping
	err := r.db.Order("vendor_group asc").Find(&list).Error
	return list, err
}

func (r *bpGroupMappingRepo) GetByID(id uint) (*models.BPGroupMapping, error) {
	var item models.BPGroupMapping
	err := r.db.First(&item, id).Error
	return &item, err
}

func (r *bpGroupMappingRepo) Save(m *models.BPGroupMapping) error {
	m.VendorGroup = strings.ToUpper(strings.TrimSpace(m.VendorGroup))
	m.BPGrouping = strings.ToUpper(strings.TrimSpace(m.BPGrouping))

	if m.ID > 0 {
		return r.db.Save(m).Error
	}

	var existing models.BPGroupMapping
	err := r.db.Where("UPPER(vendor_group) = UPPER(?)", m.VendorGroup).First(&existing).Error
	if err == nil {
		m.ID = existing.ID
		return r.db.Model(&existing).Updates(m).Error
	}

	return r.db.Create(m).Error
}

func (r *bpGroupMappingRepo) ToggleActive(id uint) error {
	var item models.BPGroupMapping
	if err := r.db.First(&item, id).Error; err != nil {
		return err
	}
	item.IsActive = !item.IsActive
	return r.db.Model(&item).Update("is_active", item.IsActive).Error
}

func (r *bpGroupMappingRepo) Delete(id uint) error {
	return r.db.Delete(&models.BPGroupMapping{}, id).Error
}
