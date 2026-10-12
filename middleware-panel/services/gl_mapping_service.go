package services

import (
	"middleware-panel/models"
	"middleware-panel/repository"
)

type GLMappingService interface {
	GetAllMappings() ([]models.MaterialGLMapping, error)
	SaveMapping(req models.MaterialGLMapping) error
	DeleteMapping(id uint) error
}

type glMappingService struct {
	repo repository.GLMappingRepository
}

func NewGLMappingService(repo repository.GLMappingRepository) GLMappingService {
	return &glMappingService{repo}
}

func (s *glMappingService) GetAllMappings() ([]models.MaterialGLMapping, error) {
	return s.repo.GetAll()
}

func (s *glMappingService) SaveMapping(req models.MaterialGLMapping) error {
	if req.ID == 0 {
		return s.repo.Create(&req)
	}

	existing, err := s.repo.GetByID(req.ID)
	if err != nil {
		return err
	}

	existing.CtgItem = req.CtgItem
	existing.GlAccount = req.GlAccount
	existing.Description = req.Description
	existing.IsActive = req.IsActive

	return s.repo.Update(&existing)
}

func (s *glMappingService) DeleteMapping(id uint) error {
	return s.repo.Delete(id)
}
