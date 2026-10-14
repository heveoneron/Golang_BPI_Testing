package services

import (
	"middleware-panel/models"
	"middleware-panel/repository"
)

type DashboardService interface {
	GetDashboardData(search, status, docType, startDate, endDate string, page, limit int) ([]models.Document, int64, int64, map[string]int64, map[string][]int, error)
	GetChildDocuments(parentNo string) ([]models.Document, error)
	GetDocTypeOptions() ([]string, error)
	GetAuditTrail(docType string, documentNo string) ([]models.IntegrationLog, error)
}

type dashboardService struct {
	repo repository.DashboardRepository
}

func NewDashboardService(repo repository.DashboardRepository) DashboardService {
	return &dashboardService{repo}
}

func (s *dashboardService) GetDashboardData(search, status, docType, startDate, endDate string, page, limit int) ([]models.Document, int64, int64, map[string]int64, map[string][]int, error) {

	docs, totalRoot, totalAll, err := s.repo.GetAllRootDocuments(search, status, docType, startDate, endDate, page, limit)

	tAll, tSucc, tFail, tSent, tPend := s.repo.GetDocumentStats(docType, startDate, endDate)

	// PENYEBAB ERROR KEDUA: Parameter sudah disamakan menjadi 3 argumen pemanggil
	trends := s.repo.GetTrendStats(docType, startDate, endDate)

	stats := map[string]int64{
		"TotalAll":     tAll,
		"TotalSuccess": tSucc,
		"TotalFailed":  tFail,
		"TotalSent":    tSent,
		"TotalPending": tPend,
	}

	return docs, totalRoot, totalAll, stats, trends, err
}

func (s *dashboardService) GetChildDocuments(parentNo string) ([]models.Document, error) {
	return s.repo.GetChildDocuments(parentNo)
}

func (s *dashboardService) GetDocTypeOptions() ([]string, error) {
	return s.repo.GetDistinctDocTypes()
}

func (s *dashboardService) GetAuditTrail(docType string, documentNo string) ([]models.IntegrationLog, error) {
	return s.repo.GetAuditTrail(docType, documentNo)
}
