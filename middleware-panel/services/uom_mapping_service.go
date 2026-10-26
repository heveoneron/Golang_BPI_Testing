package services

import (
	"middleware-panel/models"
	"middleware-panel/repository"
)

type UOMMappingService interface {
	GetMappings() ([]models.UOMMapping, error)
	GetByID(id uint) (*models.UOMMapping, error)
	SaveMapping(m *models.UOMMapping) error
	ToggleActive(id uint) error
	DeleteMapping(id uint) error
}

type uomMappingService struct {
	repo repository.UOMMappingRepository
}

func NewUOMMappingService(repo repository.UOMMappingRepository) UOMMappingService {
	return &uomMappingService{repo: repo}
}

func (s *uomMappingService) GetMappings() ([]models.UOMMapping, error) {
	return s.repo.GetAll()
}

func (s *uomMappingService) GetByID(id uint) (*models.UOMMapping, error) {
	return s.repo.GetByID(id)
}

func (s *uomMappingService) SaveMapping(m *models.UOMMapping) error {
	return s.repo.Save(m)
}

func (s *uomMappingService) ToggleActive(id uint) error {
	return s.repo.ToggleActive(id)
}

func (s *uomMappingService) DeleteMapping(id uint) error {
	return s.repo.Delete(id)
}
