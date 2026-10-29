package repository

import (
	"middleware-panel/models"
	"strings"

	"gorm.io/gorm"
)

type ReconAccountMappingRepository interface {
	GetAll() ([]models.ReconAccountMapping, error)
	GetByID(id uint) (*models.ReconAccountMapping, error)
	Save(m *models.ReconAccountMapping) error
	ToggleActive(id uint) error
	Delete(id uint) error
}

type reconAccountMappingRepo struct {
	db *gorm.DB
}

func NewReconAccountMappingRepository(db *gorm.DB) ReconAccountMappingRepository {
	return &reconAccountMappingRepo{db: db}
}

func (r *reconAccountMappingRepo) GetAll() ([]models.ReconAccountMapping, error) {
	var list []models.ReconAccountMapping
	err := r.db.Order("vendor_group asc").Find(&list).Error
	return list, err
}

func (r *reconAccountMappingRepo) GetByID(id uint) (*models.ReconAccountMapping, error) {
	var item models.ReconAccountMapping
	err := r.db.First(&item, id).Error
	return &item, err
}

func (r *reconAccountMappingRepo) Save(m *models.ReconAccountMapping) error {
	m.VendorGroup = strings.ToUpper(strings.TrimSpace(m.VendorGroup))
	m.BPGrouping = strings.ToUpper(strings.TrimSpace(m.BPGrouping))
	if m.BPGrouping == "" {
		m.BPGrouping = "Z001"
	}
	m.ReconAccount = strings.TrimSpace(m.ReconAccount)
	if m.CompanyCode == "" {
		m.CompanyCode = "5110"
	} else {
		m.CompanyCode = strings.TrimSpace(m.CompanyCode)
	}

	if m.ID > 0 {
		return r.db.Save(m).Error
	}

	var existing models.ReconAccountMapping
	err := r.db.Where("UPPER(vendor_group) = UPPER(?) AND company_code = ?", m.VendorGroup, m.CompanyCode).First(&existing).Error
	if err == nil {
		m.ID = existing.ID
		return r.db.Model(&existing).Updates(m).Error
	}

	return r.db.Create(m).Error
}

func (r *reconAccountMappingRepo) ToggleActive(id uint) error {
	var item models.ReconAccountMapping
	if err := r.db.First(&item, id).Error; err != nil {
		return err
	}
	item.IsActive = !item.IsActive
	return r.db.Model(&item).Update("is_active", item.IsActive).Error
}

func (r *reconAccountMappingRepo) Delete(id uint) error {
	return r.db.Delete(&models.ReconAccountMapping{}, id).Error
}
