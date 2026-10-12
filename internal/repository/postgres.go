package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"golang-bpi-testing/internal/model"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	Pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{Pool: pool}
}

// 1. Watermark Tracker
func (r *PostgresRepository) GetLastSync(ctx context.Context, interfaceID string) (time.Time, error) {
	var lastSync time.Time
	err := r.Pool.QueryRow(ctx, `SELECT last_sync_timestamp FROM sap_sync_tracker WHERE interface_id=$1`, interfaceID).Scan(&lastSync)
	if err != nil {
		return time.Now().Add(-24 * time.Hour), err
	}
	return lastSync, nil
}

func (r *PostgresRepository) UpdateLastSync(ctx context.Context, interfaceID string, syncTime time.Time) error {
	_, err := r.Pool.Exec(ctx, `
		INSERT INTO sap_sync_tracker (interface_id, last_sync_timestamp, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (interface_id) DO UPDATE SET last_sync_timestamp = EXCLUDED.last_sync_timestamp, updated_at = NOW()
	`, interfaceID, syncTime)
	return err
}

// 2. Ingestion Staging (Tier 1)
func (r *PostgresRepository) UpsertStagingDocument(ctx context.Context, doc model.StagingDocument) error {
	query := `
		INSERT INTO staging_documents (
			id, doc_type, document_no, flow_type, raw_payload, status, error_msg, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, NOW(), NOW()
		)
		ON CONFLICT (doc_type, document_no, flow_type)
		DO UPDATE SET 
			raw_payload = EXCLUDED.raw_payload,
			status = CASE 
				WHEN staging_documents.raw_payload IS DISTINCT FROM EXCLUDED.raw_payload THEN 'PENDING'
				ELSE staging_documents.status 
			END,
			error_msg = CASE 
				WHEN staging_documents.raw_payload IS DISTINCT FROM EXCLUDED.raw_payload THEN '-'
				ELSE staging_documents.error_msg 
			END,
			updated_at = NOW();
	`
	if doc.ID == uuid.Nil {
		doc.ID = uuid.New()
	}
	_, err := r.Pool.Exec(ctx, query,
		doc.ID, doc.DocType, doc.DocumentNo, doc.FlowType, doc.RawPayload, doc.Status, doc.ErrorMsg,
	)
	return err
}

// Ambil antrean dokumen dari staging_documents untuk ditransformasi ke documents
func (r *PostgresRepository) GetPendingStagingDocuments(ctx context.Context, limit int) ([]model.StagingDocument, error) {
	query := `SELECT id, doc_type, document_no, flow_type, raw_payload, status, error_msg, created_at, updated_at
	          FROM staging_documents 
	          WHERE status = 'PENDING' 
	          ORDER BY created_at ASC 
	          LIMIT $1`
	rows, err := r.Pool.Query(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var docs []model.StagingDocument
	for rows.Next() {
		var d model.StagingDocument
		if err := rows.Scan(&d.ID, &d.DocType, &d.DocumentNo, &d.FlowType, &d.RawPayload, &d.Status, &d.ErrorMsg, &d.CreatedAt, &d.UpdatedAt); err != nil {
			continue
		}
		docs = append(docs, d)
	}
	return docs, nil
}

func (r *PostgresRepository) MarkStagingProcessed(ctx context.Context, id uuid.UUID, status, errMsg string) error {
	_, err := r.Pool.Exec(ctx, `UPDATE staging_documents SET status=$1, error_msg=$2, updated_at=NOW() WHERE id=$3`, status, errMsg, id)
	return err
}

// 3. Core Documents (Tier 2, Ala ShoeTown)
func (r *PostgresRepository) UpsertCoreDocument(ctx context.Context, doc model.Document) error {
	query := `
		INSERT INTO documents (
			id, doc_type, document_no, flow_type, group_id, parent_doc_type, parent_document_no,
			raw_payload, sap_payload, status, retry_count, sync_back_status, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NOW(), NOW()
		)
		ON CONFLICT (doc_type, document_no, flow_type)
		DO UPDATE SET 
			raw_payload = EXCLUDED.raw_payload,
			sap_payload = EXCLUDED.sap_payload,
			status = CASE 
				WHEN documents.sap_payload IS DISTINCT FROM EXCLUDED.sap_payload THEN 'pending'
				ELSE documents.status 
			END,
			retry_count = CASE 
				WHEN documents.sap_payload IS DISTINCT FROM EXCLUDED.sap_payload THEN 0
				ELSE documents.retry_count 
			END,
			error_message = NULL,
			updated_at = NOW();
	`
	if doc.ID == uuid.Nil {
		doc.ID = uuid.New()
	}
	_, err := r.Pool.Exec(ctx, query,
		doc.ID, doc.DocType, doc.DocumentNo, doc.FlowType, doc.GroupID,
		doc.ParentDocType, doc.ParentDocumentNo, doc.RawPayload, doc.SAPPayload,
		doc.Status, doc.RetryCount, doc.SyncBackStatus,
	)
	return err
}

// Atomic lock untuk dispatching worker ke SAP Grow
func (r *PostgresRepository) FetchAndLockPendingDocuments(ctx context.Context, limit int) ([]model.Document, error) {
	query := `
		UPDATE documents
		SET status = 'processing', last_attempt_at = NOW()
		WHERE id IN (
			SELECT id FROM documents
			WHERE status IN ('pending', 'error_conn') AND retry_count < 5
			ORDER BY created_at ASC
			FOR UPDATE SKIP LOCKED
			LIMIT $1
		)
		RETURNING id, doc_type, document_no, flow_type, group_id, raw_payload, sap_payload, status, retry_count
	`
	rows, err := r.Pool.Query(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var docs []model.Document
	for rows.Next() {
		var d model.Document
		if err := rows.Scan(&d.ID, &d.DocType, &d.DocumentNo, &d.FlowType, &d.GroupID, &d.RawPayload, &d.SAPPayload, &d.Status, &d.RetryCount); err != nil {
			continue
		}
		docs = append(docs, d)
	}
	return docs, nil
}

func (r *PostgresRepository) UpdateSAPSuccess(ctx context.Context, id uuid.UUID, sapDocNo string, sapResponse json.RawMessage) error {
	_, err := r.Pool.Exec(ctx, `
		UPDATE documents 
		SET status = 'success', sap_document_no = $1, sap_response = $2, error_message = NULL, updated_at = NOW()
		WHERE id = $3
	`, sapDocNo, sapResponse, id)
	return err
}

func (r *PostgresRepository) UpdateSAPError(ctx context.Context, id uuid.UUID, status, errMsg string, sapResponse json.RawMessage) error {
	_, err := r.Pool.Exec(ctx, `
		UPDATE documents 
		SET status = $1, error_message = $2, sap_response = $3, retry_count = retry_count + 1, updated_at = NOW()
		WHERE id = $4
	`, status, errMsg, sapResponse, id)
	return err
}

// 4. Budget Transaction Lock (BPI Specific)
func (r *PostgresRepository) CheckAndLockBudget(ctx context.Context, budgetCombo string, year int, docType, docNo string, amount float64) (bool, error) {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	// Hitung total lock aktif
	var totalLocked float64
	err = tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(lock_amount), 0) 
		FROM budget_transaction_locks 
		WHERE budget_combination = $1 AND budget_year = $2 AND lock_status IN ('RESERVED', 'COMMITTED')
		  AND NOT (reference_doc_type = $3 AND reference_doc_no = $4)
	`, budgetCombo, year, docType, docNo).Scan(&totalLocked)
	if err != nil {
		return false, err
	}

	// Ambil pagu anggaran
	var approvedBudget, remainingBudget float64
	err = tx.QueryRow(ctx, `
		SELECT approved_budget, remaining_budget 
		FROM budget_masters 
		WHERE budget_combination = $1 AND budget_year = $2
	`, budgetCombo, year).Scan(&approvedBudget, &remainingBudget)

	if err != nil {
		// Jika budget master belum terdaftar, default izinkan dengan log peringatan
		return true, nil
	}

	currentRemaining := approvedBudget - totalLocked
	if currentRemaining < amount {
		return false, fmt.Errorf("budget deficit: required %.2f but available %.2f", amount, currentRemaining)
	}

	// Simpan lock entry
	_, err = tx.Exec(ctx, `
		INSERT INTO budget_transaction_locks (
			budget_combination, budget_year, reference_doc_type, reference_doc_no, lock_amount, lock_status, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, 'RESERVED', NOW(), NOW()
		)
		ON CONFLICT (reference_doc_type, reference_doc_no)
		DO UPDATE SET lock_amount = EXCLUDED.lock_amount, lock_status = 'RESERVED', updated_at = NOW()
	`, budgetCombo, year, docType, docNo, amount)
	if err != nil {
		return false, err
	}

	return true, tx.Commit(ctx)
}
