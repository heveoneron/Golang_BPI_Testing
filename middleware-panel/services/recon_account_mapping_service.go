package services

import (
	"middleware-panel/models"
	"middleware-panel/repository"
)

type ReconAccountMappingService interface {
	GetMappings() ([]models.ReconAccountMapping, error)
	GetMappingByID(id uint) (*models.ReconAccountMapping, error)
	SaveMapping(m *models.ReconAccountMapping) error
	ToggleActive(id uint) error
	DeleteMapping(id uint) error
}

type reconAccountMappingService struct {
	repo repository.ReconAccountMappingRepository
}

func NewReconAccountMappingService(repo repository.ReconAccountMappingRepository) ReconAccountMappingService {
	return &reconAccountMappingService{repo: repo}
}

func (s *reconAccountMappingService) GetMappings() ([]models.ReconAccountMapping, error) {
	return s.repo.GetAll()
}

func (s *reconAccountMappingService) GetMappingByID(id uint) (*models.ReconAccountMapping, error) {
	return s.repo.GetByID(id)
}

func (s *reconAccountMappingService) SaveMapping(m *models.ReconAccountMapping) error {
	return s.repo.Save(m)
}

func (s *reconAccountMappingService) ToggleActive(id uint) error {
	return s.repo.ToggleActive(id)
}

func (s *reconAccountMappingService) DeleteMapping(id uint) error {
	return s.repo.Delete(id)
}
