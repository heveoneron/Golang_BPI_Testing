package repository

import (
	"middleware-panel/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type VendorMasterRepository interface {
	GetAll() ([]models.VendorMaster, error)
	GetByCode(code string) (*models.VendorMaster, error)
	Save(vm *models.VendorMaster) error
	Delete(code string) error
	ToggleActive(code string) error
}

type vendorMasterRepository struct {
	db *gorm.DB
}

func NewVendorMasterRepository(db *gorm.DB) VendorMasterRepository {
	return &vendorMasterRepository{db: db}
}

func (r *vendorMasterRepository) GetAll() ([]models.VendorMaster, error) {
	var list []models.VendorMaster
	err := r.db.Order("vendor_code ASC").Find(&list).Error
	return list, err
}

func (r *vendorMasterRepository) GetByCode(code string) (*models.VendorMaster, error) {
	var vm models.VendorMaster
	err := r.db.Where("vendor_code = ?", code).First(&vm).Error
	if err != nil {
		return nil, err
	}
	return &vm, nil
}

func (r *vendorMasterRepository) Save(vm *models.VendorMaster) error {
	return r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "vendor_code"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"cip_flag", "email", "bank_key", "bank_account", "account_holder", "description", "is_active", "updated_at",
		}),
	}).Create(vm).Error
}

func (r *vendorMasterRepository) Delete(code string) error {
	return r.db.Where("vendor_code = ?", code).Delete(&models.VendorMaster{}).Error
}

func (r *vendorMasterRepository) ToggleActive(code string) error {
	vm, err := r.GetByCode(code)
	if err != nil {
		return err
	}
	return r.db.Model(&models.VendorMaster{}).Where("vendor_code = ?", code).Update("is_active", !vm.IsActive).Error
}
