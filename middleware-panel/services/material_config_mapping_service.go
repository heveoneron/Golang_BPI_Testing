package services

import (
	"middleware-panel/models"
	"middleware-panel/repository"
)

type MaterialConfigMappingService interface {
	GetMappings(configType string) ([]models.MaterialConfigMapping, error)
	GetByID(id uint) (*models.MaterialConfigMapping, error)
	SaveMapping(m *models.MaterialConfigMapping) error
	ToggleActive(id uint) error
	DeleteMapping(id uint) error
}

type materialConfigMappingService struct {
	repo repository.MaterialConfigMappingRepository
}

func NewMaterialConfigMappingService(repo repository.MaterialConfigMappingRepository) MaterialConfigMappingService {
	return &materialConfigMappingService{repo: repo}
}

func (s *materialConfigMappingService) GetMappings(configType string) ([]models.MaterialConfigMapping, error) {
	return s.repo.GetAll(configType)
}

func (s *materialConfigMappingService) GetByID(id uint) (*models.MaterialConfigMapping, error) {
	return s.repo.GetByID(id)
}

func (s *materialConfigMappingService) SaveMapping(m *models.MaterialConfigMapping) error {
	return s.repo.Save(m)
}

func (s *materialConfigMappingService) ToggleActive(id uint) error {
	return s.repo.ToggleActive(id)
}

func (s *materialConfigMappingService) DeleteMapping(id uint) error {
	return s.repo.Delete(id)
}

