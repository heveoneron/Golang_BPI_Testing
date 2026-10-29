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
	DeleteDocument(docType string, documentNo string) error
	BulkDeleteDocuments(items []string) (int64, error)
	ClearAllDocuments() error
	UpdatePayload(docType, documentNo, sapPayload string) error
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

func (s *dashboardService) DeleteDocument(docType string, documentNo string) error {
	return s.repo.DeleteDocument(docType, documentNo)
}

func (s *dashboardService) BulkDeleteDocuments(items []string) (int64, error) {
	return s.repo.BulkDeleteDocuments(items)
}

func (s *dashboardService) ClearAllDocuments() error {
	return s.repo.ClearAllDocuments()
}

func (s *dashboardService) UpdatePayload(docType, documentNo, sapPayload string) error {
	doc, err := s.repo.GetDocumentByTypeAndNo(docType, documentNo)
	if err != nil {
		return err
	}
	clean := sapPayload
	doc.SAPPayload = &clean
	doc.Status = "error_data"
	doc.RetryCount = 0
	return s.repo.UpdateDocument(&doc)
}
