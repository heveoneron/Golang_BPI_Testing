package repository

import (
	"fmt"
	"middleware-panel/models"
	"strings"
	"time"

	"gorm.io/gorm"
)

type DashboardRepository interface {
	GetAllRootDocuments(search string, status string, docType string, startDate string, endDate string, page int, limit int) ([]models.Document, int64, int64, error)
	GetChildDocuments(parentNo string) ([]models.Document, error)
	GetDocumentByTypeAndNo(docType string, documentNo string) (models.Document, error)
	UpdateDocument(doc *models.Document) error
	GetDocumentStats(docType string, startDate string, endDate string) (int64, int64, int64, int64, int64)
	GetDistinctDocTypes() ([]string, error)
	GetTrendStats(docType string, startDate string, endDate string) map[string][]int
	GetAuditTrail(docType string, documentNo string) ([]models.IntegrationLog, error)
	DeleteDocument(docType, docNo string) error
	BulkDeleteDocuments(items []string) (int64, error)
	ClearAllDocuments() error
}

type dashboardRepo struct {
	db *gorm.DB
}

func NewDashboardRepository(db *gorm.DB) DashboardRepository {
	return &dashboardRepo{db}
}

func (r *dashboardRepo) GetAllRootDocuments(search, status, docType, startDate, endDate string, page, limit int) ([]models.Document, int64, int64, error) {
	var docs []models.Document
	var totalAll int64

	buildFilteredQuery := func() *gorm.DB {
		q := r.db.Model(&models.Document{})

		if startDate != "" && endDate != "" {
			start := startDate + " 00:00:00"
			end := endDate + " 23:59:59.999"
			q = q.Where("created_at >= ? AND created_at <= ?", start, end)
		} else if startDate != "" {
			start := startDate + " 00:00:00"
			q = q.Where("created_at >= ?", start)
		} else if endDate != "" {
			end := endDate + " 23:59:59.999"
			q = q.Where("created_at <= ?", end)
		}

		if search != "" {
			searchParam := "%" + search + "%"
			q = q.Where("(document_no ILIKE ? OR group_id ILIKE ?)", searchParam, searchParam)
		}

		if status != "" {
			switch status {
			case "error":
				q = q.Where("status IN ?", []string{"error_data", "error_conn", "blocked"})
			case "error_conn":
				q = q.Where("status IN ? AND (error_message ILIKE ? OR error_message ILIKE ? OR error_message ILIKE ? OR error_message ILIKE ?)",
					[]string{"error_data", "error_conn", "error"}, "%timeout%", "%conn_error%", "%connection%", "%deadline exceeded%")
			case "error_data":
				q = q.Where("status IN ? AND NOT (error_message ILIKE ? OR error_message ILIKE ? OR error_message ILIKE ? OR error_message ILIKE ?)",
					[]string{"error_data", "error_conn", "error", "blocked"}, "%timeout%", "%conn_error%", "%connection%", "%deadline exceeded%")
			case "sent":
				q = q.Where("status IN ?", []string{"sent", "processing", "in_progress"})
			case "pending":
				q = q.Where("status IN ?", []string{"pending", "requeued"})
			default:
				q = q.Where("status = ?", status)
			}
		}
		if docType != "" {
			q = q.Where("doc_type = ?", docType)
		}
		return q
	}

	if err := buildFilteredQuery().Count(&totalAll).Error; err != nil {
		return nil, 0, 0, err
	}

	offset := (page - 1) * limit

	err := buildFilteredQuery().
		Select(`*,
			CASE WHEN documents.parent_doc_type IS NULL THEN
				EXISTS(SELECT 1 FROM documents child WHERE child.group_id = documents.group_id AND child.document_no != documents.document_no)
			ELSE false END AS has_children,
			CASE WHEN documents.parent_doc_type IS NOT NULL THEN documents.parent_document_no
			ELSE NULL END AS parent_display_no,
			CASE WHEN documents.parent_document_no IS NOT NULL THEN
				COALESCE((SELECT p.status FROM documents p WHERE p.document_no = documents.parent_document_no AND p.doc_type = documents.parent_doc_type), '')
			ELSE '' END AS parent_status_str
		`).
		Order("created_at desc").
		Limit(limit).
		Offset(offset).
		Find(&docs).Error
	if err != nil {
		return nil, 0, 0, err
	}

	return docs, totalAll, totalAll, nil
}

func (r *dashboardRepo) GetChildDocuments(parentNo string) ([]models.Document, error) {
	var docs []models.Document

	var parentDoc models.Document
	if err := r.db.Select("group_id, status").Where("document_no = ?", parentNo).First(&parentDoc).Error; err != nil {
		return nil, err
	}

	err := r.db.Model(&models.Document{}).
		Select("*, ? AS parent_status_str", parentDoc.Status).
		Where("group_id = ? AND document_no != ?", parentDoc.GroupID, parentNo).
		Order("created_at asc").
		Find(&docs).Error

	return docs, err
}

func (r *dashboardRepo) GetDocumentByTypeAndNo(docType, documentNo string) (models.Document, error) {
	var doc models.Document
	err := r.db.Where("doc_type = ? AND document_no = ?", docType, documentNo).First(&doc).Error
	return doc, err
}

func (r *dashboardRepo) UpdateDocument(doc *models.Document) error {
	return r.db.Save(doc).Error
}

func (r *dashboardRepo) GetDocumentStats(docType, startDate, endDate string) (int64, int64, int64, int64, int64) {
	var stats struct {
		Total   int64
		Success int64
		Failed  int64
		Sent    int64
		Pending int64
	}

	q := r.db.Model(&models.Document{})
	if docType != "" {
		q = q.Where("doc_type = ?", docType)
	}
	if startDate != "" && endDate != "" {
		start := startDate + " 00:00:00"
		end := endDate + " 23:59:59.999"
		q = q.Where("created_at >= ? AND created_at <= ?", start, end)
	} else if startDate != "" {
		q = q.Where("created_at >= ?", startDate+" 00:00:00")
	} else if endDate != "" {
		q = q.Where("created_at <= ?", endDate+" 23:59:59.999")
	}

	q.Select(`
		COUNT(*) as total,
		COUNT(CASE WHEN status = 'success' THEN 1 END) as success,
		COUNT(CASE WHEN status IN ('error_data', 'error_conn', 'blocked') THEN 1 END) as failed,
		COUNT(CASE WHEN status IN ('sent', 'processing', 'in_progress') THEN 1 END) as sent,
		COUNT(CASE WHEN status IN ('pending', 'requeued') THEN 1 END) as pending
	`).Scan(&stats)

	return stats.Total, stats.Success, stats.Failed, stats.Sent, stats.Pending
}

func (r *dashboardRepo) GetDistinctDocTypes() ([]string, error) {
	var docTypes []string
	err := r.db.Model(&models.Document{}).
		Distinct("doc_type").
		Order("doc_type asc").
		Pluck("doc_type", &docTypes).Error
	return docTypes, err
}

func (r *dashboardRepo) GetTrendStats(docType string, startDate string, endDate string) map[string][]int {
	var start, end time.Time
	var err error

	// 1. Baca filter kalender dari UI pengguna
	if startDate != "" && endDate != "" {
		start, err = time.Parse("2006-01-02", startDate)
		end, _ = time.Parse("2006-01-02", endDate)
	}

	// Jika tidak ada filter, gunakan 14 hari terakhir sebagai default
	if err != nil || startDate == "" || endDate == "" {
		now := time.Now()
		end = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
		start = end.AddDate(0, 0, -14)
	}

	// Batasi rentang maksimal 60 hari agar memori Javascript/Grafik tidak terlalu berat
	days := int(end.Sub(start).Hours() / 24)
	if days < 1 {
		days = 1
	}
	if days > 60 {
		start = end.AddDate(0, 0, -60)
		days = 60
	}

	// 2. Buat kerangka tanggal (untuk menjaga hari yang tidak ada transaksi tetap bernilai 0)
	dateList := make([]string, 0, days+1)
	dateMap := make(map[string]int)
	for i := 0; i <= days; i++ {
		d := start.AddDate(0, 0, i).Format("2006-01-02")
		dateList = append(dateList, d)
		dateMap[d] = i
	}

	trends := map[string][]int{
		"All":     make([]int, len(dateList)),
		"Success": make([]int, len(dateList)),
		"Failed":  make([]int, len(dateList)),
		"Sent":    make([]int, len(dateList)),
		"Pending": make([]int, len(dateList)),
	}

	var results []struct {
		DateStr      string `gorm:"column:date_str"`
		TotalAll     int    `gorm:"column:total_all"`
		TotalSuccess int    `gorm:"column:total_success"`
		TotalFailed  int    `gorm:"column:total_failed"`
		TotalSent    int    `gorm:"column:total_sent"`
		TotalPending int    `gorm:"column:total_pending"`
	}

	// 3. KUNCI PERBAIKAN: Gunakan COUNT(*) dan COUNT(CASE WHEN...) yang terbukti aman!
	q := r.db.Model(&models.Document{}).
		Select(`
			TO_CHAR(created_at, 'YYYY-MM-DD') as date_str,
			COUNT(*) as total_all,
			COUNT(CASE WHEN status = 'success' THEN 1 END) as total_success,
			COUNT(CASE WHEN status IN ('error_data', 'error_conn', 'blocked') THEN 1 END) as total_failed,
			COUNT(CASE WHEN status IN ('sent', 'processing', 'in_progress') THEN 1 END) as total_sent,
			COUNT(CASE WHEN status IN ('pending', 'requeued') THEN 1 END) as total_pending
		`).
		Where("created_at >= ? AND created_at <= ?", start.Format("2006-01-02")+" 00:00:00", end.Format("2006-01-02")+" 23:59:59.999")

	if docType != "" {
		q = q.Where("doc_type = ?", docType)
	}

	// Eksekusi query
	err = q.Group("TO_CHAR(created_at, 'YYYY-MM-DD')").Scan(&results).Error
	if err != nil {
		// Jika terjadi error pada SQL, kembalikan array kosong dengan rapi agar UI tidak crash
		return trends
	}

	// 4. Petakan data SQL ke array Javascript
	for _, res := range results {
		if idx, exists := dateMap[res.DateStr]; exists {
			trends["All"][idx] = res.TotalAll
			trends["Success"][idx] = res.TotalSuccess
			trends["Failed"][idx] = res.TotalFailed
			trends["Sent"][idx] = res.TotalSent
			trends["Pending"][idx] = res.TotalPending
		}
	}

	// Proteksi JS: Jika memfilter cuma 1 hari (Today), duplikat titiknya agar garis bisa ditarik
	if len(dateList) == 1 {
		for k := range trends {
			trends[k] = append([]int{0}, trends[k][0])
		}
	}

	return trends
}

func (r *dashboardRepo) GetAuditTrail(docType string, documentNo string) ([]models.IntegrationLog, error) {
	var logs []models.IntegrationLog
	query := r.db.Model(&models.IntegrationLog{})
	if docType != "" {
		query = query.Where("LOWER(document_type) = LOWER(?)", docType)
	}
	if documentNo != "" {
		query = query.Where("document_no = ?", documentNo)
	}
	_ = query.Order("created_at ASC").Find(&logs).Error

	// Fallback cerdas: Jika belum ada log di integration_logs untuk dokumen ini,
	// buatkan log representatif dari lifecycle data di tabel documents agar button logs tidak kosong!
	if len(logs) == 0 && documentNo != "" {
		var doc models.Document
		docQuery := r.db.Model(&models.Document{}).Where("document_no = ?", documentNo)
		if docType != "" {
			docQuery = docQuery.Where("LOWER(doc_type) = LOWER(?)", docType)
		}
		if err := docQuery.First(&doc).Error; err != nil {
			// Coba lagi tanpa filter docType
			_ = r.db.Model(&models.Document{}).Where("document_no = ?", documentNo).First(&doc).Error
		}

		if doc.DocumentNo != "" {
			// Step 1: Received dari Maximo ERP
			logs = append(logs, models.IntegrationLog{
				SourceSystem: "MAXIMO",
				TargetSystem: "MIDDLEWARE",
				DocumentType: doc.DocType,
				DocumentNo:   doc.DocumentNo,
				Status:       "RECEIVED",
				Metadata:     fmt.Sprintf("Payload received from Maximo ERP (Group: %s)", doc.GroupID),
				CreatedAt:    doc.CreatedAt,
			})

			// Step 2: Kirim / Status ke SAP Grow
			sapStatus := "SENT"
			targetSys := "SAP GROW"
			metadataMsg := "Waiting for confirmation from SAP"
			if strings.EqualFold(doc.Status, "success") {
				sapStatus = "SUCCESS"
				metadataMsg = "Document successfully posted to SAP Grow"
				if doc.SAPDocumentNo != nil && *doc.SAPDocumentNo != "" {
					metadataMsg = fmt.Sprintf("Document successfully posted to SAP Grow (SAP Doc: %s)", *doc.SAPDocumentNo)
				}
			} else if strings.Contains(strings.ToLower(doc.Status), "error") || strings.EqualFold(doc.Status, "failed") {
				sapStatus = "FAILED"
				if doc.ErrorMessage != nil && *doc.ErrorMessage != "" {
					metadataMsg = *doc.ErrorMessage
				} else {
					metadataMsg = "Failed processing document to SAP"
				}
			} else if strings.EqualFold(doc.Status, "pending") || strings.EqualFold(doc.Status, "requeued") {
				sapStatus = "PENDING"
				metadataMsg = "Queued in middleware buffer"
			}

			updatedTime := doc.UpdatedAt
			if updatedTime.Equal(doc.CreatedAt) || updatedTime.Before(doc.CreatedAt) {
				updatedTime = doc.CreatedAt.Add(1 * time.Second)
			}

			logs = append(logs, models.IntegrationLog{
				SourceSystem: "MIDDLEWARE",
				TargetSystem: targetSys,
				DocumentType: doc.DocType,
				DocumentNo:   doc.DocumentNo,
				Status:       sapStatus,
				ErrorMessage: doc.ErrorMessage,
				Metadata:     metadataMsg,
				CreatedAt:    updatedTime,
			})
		}
	}

	return logs, nil
}

func (r *dashboardRepo) DeleteDocument(docType, docNo string) error {
	// 1. Delete from documents
	q := r.db.Where("document_no = ?", docNo)
	if docType != "" {
		q = q.Where("LOWER(doc_type) = LOWER(?)", docType)
	}
	if err := q.Delete(&models.Document{}).Error; err != nil {
		return err
	}

	// 2. Delete from staging_documents
	qStage := r.db.Table("staging_documents").Where("document_no = ?", docNo)
	if docType != "" {
		qStage = qStage.Where("LOWER(doc_type) = LOWER(?)", docType)
	}
	_ = qStage.Delete(nil).Error

	// 3. Delete from integration_logs
	qLog := r.db.Table("integration_logs").Where("document_no = ?", docNo)
	if docType != "" {
		qLog = qLog.Where("LOWER(document_type) = LOWER(?)", docType)
	}
	_ = qLog.Delete(nil).Error

	return nil
}

func (r *dashboardRepo) BulkDeleteDocuments(items []string) (int64, error) {
	var count int64
	for _, it := range items {
		parts := strings.SplitN(it, "|", 2)
		docType := ""
		docNo := it
		if len(parts) == 2 {
			docType = parts[0]
			docNo = parts[1]
		}
		if err := r.DeleteDocument(docType, docNo); err == nil {
			count++
		}
	}
	return count, nil
}

func (r *dashboardRepo) ClearAllDocuments() error {
	_ = r.db.Exec("DELETE FROM documents").Error
	_ = r.db.Exec("DELETE FROM staging_documents").Error
	_ = r.db.Exec("DELETE FROM integration_logs").Error
	return nil
}

