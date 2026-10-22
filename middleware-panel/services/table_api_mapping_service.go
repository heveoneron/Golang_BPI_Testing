package services

import (
	"middleware-panel/models"
	"middleware-panel/repository"
)

type TableAPIMappingService interface {
	GetMappings() ([]models.TableAPIMapping, error)
	GetEndpoints() ([]models.SAPEndpoint, error)
	SaveMapping(m *models.TableAPIMapping) error
	ToggleActive(id uint) error
	DeleteMapping(id uint) error
	SeedDefaults() error
}

type tableAPIMappingService struct {
	mappingRepo repository.TableAPIMappingRepository
	endpointRepo repository.SAPEndpointRepository
}

func NewTableAPIMappingService(
	mRepo repository.TableAPIMappingRepository,
	eRepo repository.SAPEndpointRepository,
) TableAPIMappingService {
	return &tableAPIMappingService{
		mappingRepo:  mRepo,
		endpointRepo: eRepo,
	}
}

func (s *tableAPIMappingService) GetMappings() ([]models.TableAPIMapping, error) {
	return s.mappingRepo.GetAll()
}

func (s *tableAPIMappingService) GetEndpoints() ([]models.SAPEndpoint, error) {
	return s.endpointRepo.GetAll()
}

func (s *tableAPIMappingService) SaveMapping(m *models.TableAPIMapping) error {
	return s.mappingRepo.Save(m)
}

func (s *tableAPIMappingService) ToggleActive(id uint) error {
	return s.mappingRepo.ToggleActive(id)
}

func (s *tableAPIMappingService) DeleteMapping(id uint) error {
	return s.mappingRepo.Delete(id)
}

func (s *tableAPIMappingService) SeedDefaults() error {
	return s.mappingRepo.SeedDefaults()
}
