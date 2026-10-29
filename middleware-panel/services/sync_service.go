package services

import (
	"errors"
	"fmt"
	"io/ioutil"
	"middleware-panel/repository"
	"net/http"
	"os"
	"strings"
	"time"
)

type SyncService interface {
	SendToSAPServiceLayer(docType string, documentNo string, token string) error
}

type syncService struct {
	repo repository.DashboardRepository
}

func NewSyncService(repo repository.DashboardRepository) SyncService {
	return &syncService{repo}
}

// SendToSAPServiceLayer mengirim dokumen langsung ke SAP Grow OData via Middleware Engine.
func (s *syncService) SendToSAPServiceLayer(docType string, documentNo string, token string) error {
	fmt.Println("--> Memproses Document (Kirim langsung ke SAP Grow via Middleware Engine):", docType, "/", documentNo)

	// 1. Ambil dokumen menggunakan kombinasi doc_type + document_no
	doc, err := s.repo.GetDocumentByTypeAndNo(docType, documentNo)
	if err != nil {
		return errors.New("data dokumen tidak ditemukan")
	}

	// 2. Validasi parent jika ada
	if doc.ParentDocumentNo != nil && *doc.ParentDocumentNo != "" {
		parentDocType := ""
		if doc.ParentDocType != nil {
			parentDocType = *doc.ParentDocType
		}
		parentDoc, perr := s.repo.GetDocumentByTypeAndNo(parentDocType, *doc.ParentDocumentNo)
		if perr != nil {
			doc.Status = "blocked"
			errMsg := "Parent document tidak ditemukan: " + *doc.ParentDocumentNo
			doc.ErrorMessage = &errMsg
			blockReason := "Parent document missing"
			doc.BlockedReason = &blockReason
			s.repo.UpdateDocument(&doc)
			return errors.New("parent document tidak ditemukan")
		}
		if parentDoc.Status != "success" {
			doc.Status = "blocked"
			errMsg := fmt.Sprintf("Tidak bisa dikirim, parent document %s masih berstatus '%s'", *doc.ParentDocumentNo, parentDoc.Status)
			doc.ErrorMessage = &errMsg
			blockReason := "Waiting for parent document to succeed"
			doc.BlockedReason = &blockReason
			s.repo.UpdateDocument(&doc)
			return errors.New("parent document belum success")
		}
	}

	// 3. Reset status dokumen ke 'pending' agar diproses oleh SAP Push Dispatcher
	doc.Status = "pending"
	doc.RetryCount = 0
	doc.ErrorMessage = nil
	if err := s.repo.UpdateDocument(&doc); err != nil {
		return fmt.Errorf("gagal update status dokumen: %w", err)
	}

	// 4. Picu Middleware Engine untuk mengeksekusi push ke SAP Grow OData langsung
	middlewareURL := os.Getenv("MIDDLEWARE_API_URL")
	if middlewareURL == "" {
		middlewareURL = "http://localhost:8085/api/v1"
	}
	endpoint := fmt.Sprintf("%s/trigger/push", strings.TrimRight(middlewareURL, "/"))

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Post(endpoint, "application/json", nil)
	if err != nil {
		errMsg := fmt.Sprintf("Gagal menghubungi Middleware Engine di %s: %v", endpoint, err)
		doc.Status = "error_conn"
		doc.ErrorMessage = &errMsg
		_ = s.repo.UpdateDocument(&doc)
		return errors.New(errMsg)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBytes, _ := ioutil.ReadAll(resp.Body)
		errMsg := fmt.Sprintf("Middleware Engine mengembalikan status %d: %s", resp.StatusCode, string(respBytes))
		doc.Status = "error_conn"
		doc.ErrorMessage = &errMsg
		_ = s.repo.UpdateDocument(&doc)
		return errors.New(errMsg)
	}

	return nil
}
