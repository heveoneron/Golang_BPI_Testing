package services

import (
	"middleware-panel/models"
	"middleware-panel/repository"
)

type BPGroupMappingService interface {
	GetMappings() ([]models.BPGroupMapping, error)
	GetMappingByID(id uint) (*models.BPGroupMapping, error)
	SaveMapping(m *models.BPGroupMapping) error
	ToggleActive(id uint) error
	DeleteMapping(id uint) error
}

type bpGroupMappingService struct {
	repo repository.BPGroupMappingRepository
}

func NewBPGroupMappingService(repo repository.BPGroupMappingRepository) BPGroupMappingService {
	return &bpGroupMappingService{repo: repo}
}

func (s *bpGroupMappingService) GetMappings() ([]models.BPGroupMapping, error) {
	return s.repo.GetAll()
}

func (s *bpGroupMappingService) GetMappingByID(id uint) (*models.BPGroupMapping, error) {
	return s.repo.GetByID(id)
}

func (s *bpGroupMappingService) SaveMapping(m *models.BPGroupMapping) error {
	return s.repo.Save(m)
}

func (s *bpGroupMappingService) ToggleActive(id uint) error {
	return s.repo.ToggleActive(id)
}

func (s *bpGroupMappingService) DeleteMapping(id uint) error {
	return s.repo.Delete(id)
}
