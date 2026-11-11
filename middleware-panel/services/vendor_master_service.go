package services

import (
	"middleware-panel/models"
	"middleware-panel/repository"
)

type VendorMasterService interface {
	GetVendors() ([]models.VendorMaster, error)
	SaveVendor(vm *models.VendorMaster) error
	DeleteVendor(code string) error
	ToggleActive(code string) error
}

type vendorMasterService struct {
	repo repository.VendorMasterRepository
}

func NewVendorMasterService(repo repository.VendorMasterRepository) VendorMasterService {
	return &vendorMasterService{repo: repo}
}

func (s *vendorMasterService) GetVendors() ([]models.VendorMaster, error) {
	return s.repo.GetAll()
}

func (s *vendorMasterService) SaveVendor(vm *models.VendorMaster) error {
	return s.repo.Save(vm)
}

func (s *vendorMasterService) DeleteVendor(code string) error {
	return s.repo.Delete(code)
}

func (s *vendorMasterService) ToggleActive(code string) error {
	return s.repo.ToggleActive(code)
}
