package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"golang-bpi-testing/internal/client"
	"golang-bpi-testing/internal/model"
)

// toJSONResponse memastikan body respons SAP bisa disimpan ke kolom JSONB sap_response.
// Body non-JSON (XML SOAP, HTML error gateway) disimpan sebagai string JSON agar UPDATE tidak gagal.
func toJSONResponse(body string) json.RawMessage {
	if strings.TrimSpace(body) == "" {
		return nil
	}
	if json.Valid([]byte(body)) {
		return json.RawMessage(body)
	}
	b, _ := json.Marshal(body)
	return b
}

// executeSOAPDocument mengirim dokumen yang sap_payload-nya berisi envelope XML (string JSON).
func (s *SyncService) executeSOAPDocument(ep *model.SAPEndpoint, payload json.RawMessage) (int, string, string, error) {
	var envelope string
	if err := json.Unmarshal(payload, &envelope); err != nil || strings.TrimSpace(envelope) == "" {
		return -1, "", "", fmt.Errorf("sap_payload SOAP tidak valid: harus berupa string XML")
	}
	statusCode, body, err := s.SAPClient.ExecuteSOAP(ep, envelope)
	if err != nil {
		return statusCode, "", body, err
	}
	conf, perr := client.ParseJournalEntryConfirmation(body)
	if perr != nil {
		return statusCode, "", body, perr
	}
	if !conf.Posted() {
		msg := "SAP tidak mengembalikan nomor AccountingDocument"
		if len(conf.Notes) > 0 {
			msg += ": " + strings.Join(conf.Notes, "; ")
		}
		return statusCode, "", body, fmt.Errorf("%s", msg)
	}
	docNo := conf.AccountingDocument
	if conf.FiscalYear != "" {
		docNo += "/" + conf.FiscalYear
	}
	return statusCode, docNo, body, nil
}

// budgetRefType memetakan doc_type dokumen ke reference_doc_type yang dipakai saat lock budget.
func budgetRefType(docType string) string {
	switch strings.ToUpper(docType) {
	case "PO", "AXMXPO", "MXPO":
		return "PO"
	default:
		return strings.ToUpper(docType)
	}
}

// afterPushSuccess dijalankan setelah SAP menerima dokumen.
func (s *SyncService) afterPushSuccess(ctx context.Context, doc model.Document, sapDocNo string) {
	if s.PGRepo == nil || s.PGRepo.Pool == nil {
		return
	}
	if err := s.PGRepo.CommitBudgetLocks(ctx, budgetRefType(doc.DocType), doc.DocumentNo); err != nil {
		log.Printf("[WARN] Commit budget lock %s %s gagal: %v", doc.DocType, doc.DocumentNo, err)
	}
	switch strings.ToUpper(doc.DocType) {
	case model.DocTypeBudget:
		if err := s.PGRepo.ApplyBudgetAdjustment(ctx, doc.DocumentNo); err != nil {
			log.Printf("[WARN] Penerapan budget adjustment %s gagal: %v", doc.DocumentNo, err)
		}
	case model.DocTypeAssetAcq:
		if err := s.PGRepo.UpdateAssetAcquisitionResult(ctx, doc.DocumentNo, "POSTED", sapDocNo, ""); err != nil {
			log.Printf("[WARN] Update status aset %s gagal: %v", doc.DocumentNo, err)
		}
	}
}

// afterPushRejected dijalankan saat SAP menolak dokumen (error_data): pagu yang ditahan dilepas.
func (s *SyncService) afterPushRejected(ctx context.Context, doc model.Document, reason string) {
	if s.PGRepo == nil || s.PGRepo.Pool == nil {
		return
	}
	if err := s.PGRepo.ReleaseBudgetLocks(ctx, budgetRefType(doc.DocType), doc.DocumentNo); err != nil {
		log.Printf("[WARN] Release budget lock %s %s gagal: %v", doc.DocType, doc.DocumentNo, err)
	}
	switch strings.ToUpper(doc.DocType) {
	case model.DocTypeBudget:
		_ = s.PGRepo.FailBudgetAdjustment(ctx, doc.DocumentNo, reason)
	case model.DocTypeAssetAcq:
		_ = s.PGRepo.UpdateAssetAcquisitionResult(ctx, doc.DocumentNo, "FAILED", "", reason)
	}
}
