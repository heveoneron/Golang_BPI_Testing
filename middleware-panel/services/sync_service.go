package services

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"middleware-panel/models"
	"middleware-panel/repository"
	"net/http"
	"os"
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

// SendToSAPServiceLayer mengirim dokumen ke Bot API.
//
// PENTING: parameter docType WAJIB disertakan -- document_no SAJA TIDAK CUKUP
// untuk mengidentifikasi dokumen secara unik, karena document_no yang sama
// bisa dipakai di beberapa doc_type berbeda (mis. "HV5180-010" sebagai
// MMMMaterialC DAN sebagai PPMBillOfMaterialC). Memanggil fungsi ini hanya
// dengan documentNo (tanpa docType yang benar) berisiko mengirim payload dari
// dokumen yang SALAH ke Bot API.
func (s *syncService) SendToSAPServiceLayer(docType string, documentNo string, token string) error {
	fmt.Println("--> Memproses Document (Kirim ke Bot API):", docType, "/", documentNo)

	// 1. Ambil dokumen menggunakan kombinasi doc_type + document_no
	doc, err := s.repo.GetDocumentByTypeAndNo(docType, documentNo)
	if err != nil {
		return errors.New("data dokumen tidak ditemukan")
	}

	// 2. Validasi backend -- kalau dokumen ini punya parent, parent harus sudah
	// berstatus "success" sebelum child boleh dikirim.
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

	apiURL := os.Getenv("API_URL")
	if apiURL == "" {
		apiURL = os.Getenv("MIDDLEWARE_API_URL")
	}
	if apiURL == "" {
		doc.Status = "error_data"
		errMsg := "Konfigurasi API_URL atau MIDDLEWARE_API_URL di file .env tidak ditemukan"
		doc.ErrorMessage = &errMsg
		s.repo.UpdateDocument(&doc)
		return errors.New("missing api configuration")
	}

	if token == "" {
		doc.Status = "error_data"
		errMsg := "Akses ditolak: Token API tidak ditemukan. Silakan login ulang."
		doc.ErrorMessage = &errMsg
		s.repo.UpdateDocument(&doc)
		return errors.New("missing api token")
	}

	// 3. Ubah Payload Mentah DB jadi Map JSON
	var rawPayloadMap map[string]interface{}
	if err := json.Unmarshal([]byte(doc.Payload), &rawPayloadMap); err != nil {
		doc.Status = "error_data"
		errMsg := "Format JSON Payload di DB tidak valid: " + err.Error()
		doc.ErrorMessage = &errMsg
		s.repo.UpdateDocument(&doc)
		return errors.New("invalid json payload")
	}

	// 4. Mapping data request, gunakan doc.DocType dan doc.DocumentNo dari hasil lookup
	botRequest := models.SyncBotRequest{
		DocType:    doc.DocType,
		DocumentNo: doc.DocumentNo,
		EntryNo:    "1",
		Payload:    rawPayloadMap,
	}

	jsonData, err := json.Marshal(botRequest)
	if err != nil {
		doc.Status = "error_data"
		errMsg := "Gagal membungkus JSON: " + err.Error()
		doc.ErrorMessage = &errMsg
		s.repo.UpdateDocument(&doc)
		return err
	}

	fmt.Println("--> Request Payload ke API:", string(jsonData))

	// 5. PENGIRIMAN DATA DENGAN TOKEN
	respBody, connErr := postToBotAPI(apiURL, token, jsonData)

	switch {
	case connErr != nil:
		doc.Status = "error_conn"
		errMsg := "Gagal terhubung ke Bot API: " + connErr.Error()
		doc.ErrorMessage = &errMsg

	default:
		var botResp models.BotAPIResponse
		if parseErr := json.Unmarshal(respBody, &botResp); parseErr != nil {
			doc.Status = "error_data"
			errMsg := fmt.Sprintf("Response Bot API tidak valid: %s | Raw: %s", parseErr.Error(), string(respBody))
			doc.ErrorMessage = &errMsg
		} else if botResp.Status == "pending" {
			return nil
		} else if botResp.Status != "success" {
			doc.Status = "error_data"
			errMsg := botResp.Message
			if errMsg == "" {
				errMsg = "Bot API menolak request tanpa pesan detail"
			}
			doc.ErrorMessage = &errMsg

		} else {
			// --- PENANGANAN STATUS MENGGUNAKAN TAGGED SWITCH ---
			switch doc.DocType {
			case "PPMSendEmailNotifSCEC":
				doc.Status = "success" // Bypass otomatis hijau
			case "PPMBillOfMaterialC", "PPMBillOfMaterialItemC", "PPMBillOfMaterialItemU", "PPMBillOfMaterialItemD":
				// Modul BOM bersifat sinkron, langsung sukses jika Bot API merespons success
				doc.Status = "success"
			default:
				doc.Status = "sent" // Async, menunggu BPA webhook
			}

			msg := botResp.Message
			doc.ErrorMessage = &msg

			if botResp.Data.WorkflowInstanceID != "" {
				wfID := botResp.Data.WorkflowInstanceID
				doc.WorkflowInstanceID = &wfID
			}
		}
	}

	// 6. Update data dokumen
	// Selalu update database untuk mencatat status final (termasuk untuk BOM yang sinkron)
	return s.repo.UpdateDocument(&doc)
}

// postToBotAPI mengirim request ke Bot API.
//
// Return value: (responseBody []byte, connErr error)
//   - connErr diisi HANYA untuk kegagalan koneksi murni (request gagal dibuat,
//     timeout, DNS gagal, connection refused, dll).
//   - Untuk HTTP non-2xx, connErr TETAP nil dan body response dikembalikan apa
//     adanya, supaya caller bisa membaca pesan error dari Bot API.
func postToBotAPI(url string, token string, payload []byte) ([]byte, error) {
	tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	client := &http.Client{Transport: tr, Timeout: 15 * time.Second}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(payload))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("koneksi timeout saat menghubungi endpoint API Bot")
	}
	defer resp.Body.Close()

	bodyBytes, readErr := ioutil.ReadAll(resp.Body)
	if readErr != nil {
		return nil, errors.New("gagal membaca response dari Bot API: " + readErr.Error())
	}

	return bodyBytes, nil
}
