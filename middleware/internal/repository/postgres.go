package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"golang-bpi-testing/internal/model"
	"strconv"

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

// 6. Audit Trail Logging (integration_logs)
func (r *PostgresRepository) InsertIntegrationLog(ctx context.Context, traceID, src, tgt, docType, docNo, status, errMsg, metadata string) error {
	if r.Pool == nil {
		return nil
	}
	_, err := r.Pool.Exec(ctx, `
		INSERT INTO integration_logs (
			trace_id, source_system, target_system, document_type, document_no, status, error_message, metadata, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, NOW()
		)
	`, traceID, src, tgt, docType, docNo, status, errMsg, metadata)
	return err
}

// 7. SAP Endpoints & Credential Registry Operations
func (r *PostgresRepository) EnsureSAPEndpointsTable(ctx context.Context) error {
	if r.Pool == nil {
		return nil
	}
	createTableSQL := `
		CREATE TABLE IF NOT EXISTS sap_endpoints (
			id SERIAL PRIMARY KEY,
			service_code VARCHAR(50) NOT NULL UNIQUE,
			service_name VARCHAR(100) NOT NULL,
			scenario_id VARCHAR(50) NOT NULL,
			odata_version VARCHAR(10) NOT NULL DEFAULT 'v4',
			base_url TEXT NOT NULL DEFAULT 'https://my445160-api.s4hana.cloud.sap',
			endpoint_path TEXT NOT NULL,
			csrf_path TEXT,
			username VARCHAR(100) NOT NULL,
			password TEXT NOT NULL,
			is_active BOOLEAN DEFAULT TRUE,
			last_status VARCHAR(30) DEFAULT 'UNTESTED',
			last_response TEXT,
			last_tested_at TIMESTAMP WITH TIME ZONE,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		);

		INSERT INTO sap_endpoints (service_code, service_name, scenario_id, odata_version, base_url, endpoint_path, username, password)
		VALUES 
		(
			'PO', 
			'Purchase Order Integration (OData v4)', 
			'SAP_COM_0053', 
			'v4', 
			'https://my445160-api.s4hana.cloud.sap',
			'/sap/opu/odata4/sap/api_purchaseorder_2/srvd_a2x/sap/purchaseorder/0001/PurchaseOrder',
			'CU_SAP_COM_0053',
			'PowerofOne@123456789'
		),
		(
			'BP', 
			'Business Partner Integration (OData v2)', 
			'SAP_COM_0008', 
			'v2', 
			'https://my445160-api.s4hana.cloud.sap',
			'/sap/opu/odata/sap/API_BUSINESS_PARTNER/A_BusinessPartner',
			'CU_SAP_COM_0008',
			'PowerofOne@123456789'
		),
		(
			'PRODUCT', 
			'Product / Material Master Integration (OData v2)', 
			'SAP_COM_0009', 
			'v2', 
			'https://my445160-api.s4hana.cloud.sap', 
			'/sap/opu/odata/sap/API_PRODUCT_SRV/A_Product', 
			'CU_SAP_COM_0009', 
			'PowerofOne@123456789'
		),
		(
			'PO_CHANGE_HEADER',
			'Purchase Order Change Header (PATCH OData v4)',
			'SAP_COM_0053',
			'v4',
			'https://my445160-api.s4hana.cloud.sap',
			'/sap/opu/odata4/sap/api_purchaseorder_2/srvd_a2x/sap/purchaseorder/0001/PurchaseOrder',
			'CU_SAP_COM_0053',
			'PowerofOne@123456789'
		),
		(
			'PO_CHANGE_ITEM',
			'Purchase Order Change Item (PATCH OData v4)',
			'SAP_COM_0053',
			'v4',
			'https://my445160-api.s4hana.cloud.sap',
			'/sap/opu/odata4/sap/api_purchaseorder_2/srvd_a2x/sap/purchaseorder/0001/PurchaseOrderItem',
			'CU_SAP_COM_0053',
			'PowerofOne@123456789'
		),
		(
			'GR_WITH_PO',
			'Goods Receipt / Material Document with PO (OData v2)',
			'SAP_COM_0108',
			'v2',
			'https://my445160-api.s4hana.cloud.sap',
			'/sap/opu/odata/sap/API_MATERIAL_DOCUMENT_SRV/A_MaterialDocumentHeader',
			'Y_BPI_CU_SAP_COM_0108',
			'PowerofOne@123456789'
		),
		(
			'GR_NON_PO',
			'Goods Receipt Non PO (OData v2)',
			'SAP_COM_0108',
			'v2',
			'https://my445160-api.s4hana.cloud.sap',
			'/sap/opu/odata/sap/API_MATERIAL_DOCUMENT_SRV/A_MaterialDocumentHeader',
			'Y_BPI_CU_SAP_COM_0108',
			'PowerofOne@123456789'
		),
		(
			'MATERIAL_DOC_DETAIL',
			'Material Document Header & Item Query (OData v2)',
			'SAP_COM_0108',
			'v2',
			'https://my445160-api.s4hana.cloud.sap',
			'/sap/opu/odata/sap/API_MATERIAL_DOCUMENT_SRV/A_MaterialDocumentHeader',
			'Y_BPI_CU_SAP_COM_0108',
			'PowerofOne@123456789'
		),
		(
			'GL_ACCOUNT_LINE_ITEMS',
			'G/L Account Line Items Query (OData v2)',
			'SAP_COM_0303',
			'v2',
			'https://my445160-api.s4hana.cloud.sap',
			'/sap/opu/odata/sap/API_GLACCOUNTLINEITEM/GLAccountLineItem',
			'Y_BPI_CU_SAP_COM_0303',
			'PowerofOne@123456789'
		)
		ON CONFLICT (service_code) DO UPDATE 
		SET username = EXCLUDED.username, 
		    password = EXCLUDED.password, 
		    base_url = EXCLUDED.base_url, 
		    endpoint_path = EXCLUDED.endpoint_path, 
		    odata_version = EXCLUDED.odata_version, 
		    updated_at = NOW();
	`
	_, err := r.Pool.Exec(ctx, createTableSQL)
	return err
}

func (r *PostgresRepository) GetSAPEndpoints(ctx context.Context) ([]model.SAPEndpoint, error) {
	if r.Pool == nil {
		return nil, nil
	}
	_ = r.EnsureSAPEndpointsTable(ctx)

	rows, err := r.Pool.Query(ctx, `
		SELECT id, service_code, service_name, scenario_id, odata_version, base_url, endpoint_path, COALESCE(csrf_path, ''),
		       username, password, is_active, COALESCE(last_status, 'UNTESTED'), COALESCE(last_response, ''), last_tested_at,
		       COALESCE(created_at, NOW()), COALESCE(updated_at, NOW())
		FROM sap_endpoints
		ORDER BY id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var endpoints []model.SAPEndpoint
	for rows.Next() {
		var ep model.SAPEndpoint
		if err := rows.Scan(
			&ep.ID, &ep.ServiceCode, &ep.ServiceName, &ep.ScenarioID, &ep.ODataVersion,
			&ep.BaseURL, &ep.EndpointPath, &ep.CSRFPath, &ep.Username, &ep.Password,
			&ep.IsActive, &ep.LastStatus, &ep.LastResponse, &ep.LastTestedAt, &ep.CreatedAt, &ep.UpdatedAt,
		); err == nil {
			endpoints = append(endpoints, ep)
		}
	}
	return endpoints, nil
}

func (r *PostgresRepository) GetSAPEndpointByCode(ctx context.Context, code string) (*model.SAPEndpoint, error) {
	if r.Pool == nil {
		return nil, nil
	}
	_ = r.EnsureSAPEndpointsTable(ctx)

	var ep model.SAPEndpoint
	err := r.Pool.QueryRow(ctx, `
		SELECT id, service_code, service_name, scenario_id, odata_version, base_url, endpoint_path, COALESCE(csrf_path, ''),
		       username, password, is_active, COALESCE(last_status, 'UNTESTED'), COALESCE(last_response, ''), last_tested_at,
		       COALESCE(created_at, NOW()), COALESCE(updated_at, NOW())
		FROM sap_endpoints
		WHERE UPPER(service_code) = UPPER($1)
	`, code).Scan(
		&ep.ID, &ep.ServiceCode, &ep.ServiceName, &ep.ScenarioID, &ep.ODataVersion,
		&ep.BaseURL, &ep.EndpointPath, &ep.CSRFPath, &ep.Username, &ep.Password,
		&ep.IsActive, &ep.LastStatus, &ep.LastResponse, &ep.LastTestedAt, &ep.CreatedAt, &ep.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &ep, nil
}

func (r *PostgresRepository) SaveSAPEndpoint(ctx context.Context, ep *model.SAPEndpoint) error {
	if r.Pool == nil {
		return nil
	}
	_ = r.EnsureSAPEndpointsTable(ctx)

	query := `
		INSERT INTO sap_endpoints (
			service_code, service_name, scenario_id, odata_version, base_url, endpoint_path, csrf_path, username, password, is_active, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW()
		)
		ON CONFLICT (service_code) DO UPDATE SET
			service_name = EXCLUDED.service_name,
			scenario_id = EXCLUDED.scenario_id,
			odata_version = EXCLUDED.odata_version,
			base_url = EXCLUDED.base_url,
			endpoint_path = EXCLUDED.endpoint_path,
			csrf_path = EXCLUDED.csrf_path,
			username = EXCLUDED.username,
			password = CASE WHEN EXCLUDED.password != '' THEN EXCLUDED.password ELSE sap_endpoints.password END,
			is_active = EXCLUDED.is_active,
			updated_at = NOW();
	`
	_, err := r.Pool.Exec(ctx, query,
		ep.ServiceCode, ep.ServiceName, ep.ScenarioID, ep.ODataVersion,
		ep.BaseURL, ep.EndpointPath, ep.CSRFPath, ep.Username, ep.Password, ep.IsActive,
	)
	return err
}

func (r *PostgresRepository) UpdateSAPEndpointStatus(ctx context.Context, code, status, response string) error {
	if r.Pool == nil {
		return nil
	}
	_, err := r.Pool.Exec(ctx, `
		UPDATE sap_endpoints
		SET last_status = $2, last_response = $3, last_tested_at = NOW(), updated_at = NOW()
		WHERE UPPER(service_code) = UPPER($1)
	`, code, status, response)
	return err
}

// 8. Scheduler Configuration & Auto-Pull Control
func (r *PostgresRepository) EnsureSchedulerConfigsTable(ctx context.Context) error {
	if r.Pool == nil {
		return nil
	}
	createTableSQL := `
		CREATE TABLE IF NOT EXISTS scheduler_configs (
			id INT PRIMARY KEY DEFAULT 1,
			pull_is_active BOOLEAN DEFAULT TRUE,
			pull_interval INT DEFAULT 3,
			push_is_active BOOLEAN DEFAULT TRUE,
			push_interval INT DEFAULT 1,
			is_active BOOLEAN DEFAULT TRUE,
			batch_limit INT DEFAULT 1,
			last_check_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		);

		DO $$ 
		BEGIN 
			IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='scheduler_configs' AND column_name='is_active') THEN
				ALTER TABLE scheduler_configs ADD COLUMN is_active BOOLEAN DEFAULT TRUE;
			END IF;
			IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='scheduler_configs' AND column_name='pull_is_active') THEN
				ALTER TABLE scheduler_configs ADD COLUMN pull_is_active BOOLEAN DEFAULT TRUE;
			END IF;
			IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='scheduler_configs' AND column_name='batch_limit') THEN
				ALTER TABLE scheduler_configs ADD COLUMN batch_limit INT DEFAULT 1;
			END IF;
			IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='scheduler_configs' AND column_name='last_check_at') THEN
				ALTER TABLE scheduler_configs ADD COLUMN last_check_at TIMESTAMP WITH TIME ZONE DEFAULT NOW();
			END IF;
		END $$;

		INSERT INTO scheduler_configs (id, pull_is_active, pull_interval, push_is_active, push_interval, is_active, batch_limit)
		VALUES (1, TRUE, 3, TRUE, 1, TRUE, 1)
		ON CONFLICT (id) DO NOTHING;
	`
	_, err := r.Pool.Exec(ctx, createTableSQL)
	return err
}

func (r *PostgresRepository) IsAutoPullActive(ctx context.Context) (bool, int) {
	// 1. Prioritas Utama: Environment Variable (.env)
	envPull := os.Getenv("AUTO_PULL_ENABLED")
	if envPull == "" {
		envPull = os.Getenv("ENABLE_AUTO_PULL")
	}
	if envPull != "" {
		if strings.EqualFold(envPull, "false") || envPull == "0" || strings.EqualFold(envPull, "off") || strings.EqualFold(envPull, "no") {
			return false, 0
		}
	}

	if r.Pool == nil {
		return true, 1
	}

	_ = r.EnsureSchedulerConfigsTable(ctx)

	// 2. Runtime Setting di Database PostgreSQL (Web Panel)
	var isActive bool = true
	var batchLimit int = 1
	err := r.Pool.QueryRow(ctx, `
		SELECT COALESCE(is_active, pull_is_active, true), COALESCE(batch_limit, 1)
		FROM scheduler_configs
		ORDER BY id ASC
		LIMIT 1
	`).Scan(&isActive, &batchLimit)
	if err != nil {
		return true, 1
	}

	return isActive, batchLimit
}

func (r *PostgresRepository) SetAutoPullActive(ctx context.Context, active bool) error {
	if r.Pool == nil {
		return nil
	}
	_ = r.EnsureSchedulerConfigsTable(ctx)

	tag, err := r.Pool.Exec(ctx, `
		UPDATE scheduler_configs
		SET is_active = $1, pull_is_active = $1, last_check_at = NOW()
		WHERE id = (SELECT id FROM scheduler_configs ORDER BY id ASC LIMIT 1)
	`, active)
	if err == nil && tag.RowsAffected() == 0 {
		_, err = r.Pool.Exec(ctx, `
			INSERT INTO scheduler_configs (id, pull_is_active, is_active, batch_limit, last_check_at)
			VALUES (1, $1, $1, 1, NOW())
			ON CONFLICT (id) DO UPDATE 
			SET is_active = EXCLUDED.is_active, pull_is_active = EXCLUDED.pull_is_active, last_check_at = NOW()
		`, active)
	}
	return err
}

// 9. Dynamic Table to SAP API Mappings
func (r *PostgresRepository) EnsureTableAPIMappingsTable(ctx context.Context) error {
	if r.Pool == nil {
		return nil
	}
	createTableSQL := `
		CREATE TABLE IF NOT EXISTS table_api_mappings (
			id SERIAL PRIMARY KEY,
			source_table VARCHAR(100) NOT NULL UNIQUE,
			source_desc VARCHAR(255),
			target_service_code VARCHAR(50) NOT NULL,
			direction VARCHAR(30) DEFAULT 'OUTBOUND',
			sync_mode VARCHAR(30) DEFAULT 'BATCH_SCHEDULE',
			is_active BOOLEAN DEFAULT TRUE,
			notes TEXT,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		);

		INSERT INTO table_api_mappings (source_table, source_desc, target_service_code, direction, sync_mode, is_active, notes)
		VALUES
			('ITEM', 'Master Item, Material & Spare Parts', 'PRODUCT', 'OUTBOUND', 'BATCH_SCHEDULE', true, 'Sinkronisasi master data item/material ke SAP Grow API_PRODUCT_SRV'),
			('PO', 'Purchase Order Header & Lines', 'PO', 'OUTBOUND', 'BATCH_SCHEDULE', true, 'Dokumen pemesanan barang ke SAP Grow api_purchaseorder_2'),
			('COMPANIES', 'Vendor, Supplier & Business Partner', 'BP', 'OUTBOUND', 'REALTIME', true, 'Sinkronisasi mitra bisnis & supplier ke SAP Grow API_BUSINESS_PARTNER'),
			('MATRECTRANS', 'Goods Receipt (GRN) & Material Movements', 'PO', 'OUTBOUND', 'MANUAL', true, 'Penerimaan barang fisik dari gudang Maximo terhadap PO SAP Grow'),
			('PR', 'Purchase Requisition (Permintaan Pembelian)', 'PO', 'OUTBOUND', 'MANUAL', true, 'Pengajuan PR sebelum dikonversi menjadi PO di SAP'),
			('AXUOM', 'Unit of Measure (UOM) Standar', 'PRODUCT', 'OUTBOUND', 'MANUAL', true, 'Katalog satuan ukuran Maximo ke SAP Product Master UOM')
		ON CONFLICT (source_table) DO NOTHING;
	`
	_, err := r.Pool.Exec(ctx, createTableSQL)
	return err
}

func (r *PostgresRepository) GetTableAPIMapping(ctx context.Context, sourceTableOrDocType string) (*model.TableAPIMapping, error) {
	if r.Pool == nil || strings.TrimSpace(sourceTableOrDocType) == "" {
		return nil, nil
	}
	_ = r.EnsureTableAPIMappingsTable(ctx)

	cleanCode := strings.ToUpper(strings.TrimSpace(sourceTableOrDocType))
	strippedCode := cleanCode
	if strings.HasPrefix(cleanCode, "AX") && len(cleanCode) > 2 {
		strippedCode = cleanCode[2:]
	}
	if strings.HasPrefix(strippedCode, "MX") && len(strippedCode) > 2 {
		strippedCode = strippedCode[2:]
	}

	var m model.TableAPIMapping
	err := r.Pool.QueryRow(ctx, `
		SELECT id, source_table, COALESCE(source_desc, ''), target_service_code,
		       COALESCE(direction, 'OUTBOUND'), COALESCE(sync_mode, 'BATCH_SCHEDULE'),
		       is_active, COALESCE(notes, ''), created_at, updated_at
		FROM table_api_mappings
		WHERE UPPER(source_table) = $1 
		   OR UPPER(source_table) = $2 
		   OR (UPPER(source_table) = 'PO' AND $1 IN ('PO', 'AXMXPO', 'MXPO'))
		   OR (UPPER(source_table) = 'ITEM' AND $1 IN ('ITEM', 'AXITEM'))
		   OR (UPPER(source_table) = 'COMPANIES' AND $1 IN ('COMPANIES', 'VENDOR', 'AXVENDOR', 'BP'))
		   OR (UPPER(source_table) = 'MATRECTRANS' AND $1 IN ('MATRECTRANS', 'RECEIPT', 'AXRECEIPT', 'GRN'))
		   OR (UPPER(source_table) = 'PR' AND $1 IN ('PR', 'AXPR'))
		   OR (UPPER(source_table) = 'AXUOM' AND $1 IN ('AXUOM', 'UOM'))
		LIMIT 1
	`, cleanCode, strippedCode).Scan(
		&m.ID, &m.SourceTable, &m.SourceDesc, &m.TargetServiceCode,
		&m.Direction, &m.SyncMode, &m.IsActive, &m.Notes, &m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// 10. Exchange Rate Operations
func (r *PostgresRepository) SaveExchangeRates(ctx context.Context, payload *model.MaximoExchangeRatePayload, rawJSON []byte) (int, error) {
	if r.Pool == nil {
		return 0, fmt.Errorf("postgres pool not available")
	}

	saved := 0
	for _, rate := range payload.Rates {
		ratePerUnitFloat, _ := strconv.ParseFloat(rate.RatePerUnit, 64)
		if ratePerUnitFloat == 0 && rate.Unit > 0 {
			ratePerUnitFloat = rate.Rate / float64(rate.Unit)
		}

		var validFrom interface{} = nil
		if payload.Period.ValidFrom != "" {
			validFrom = payload.Period.ValidFrom
		}
		var validTo interface{} = nil
		if payload.Period.ValidTo != "" {
			validTo = payload.Period.ValidTo
		}

		_, err := r.Pool.Exec(ctx, `
			INSERT INTO exchange_rates (
				event, source, kmk_number, valid_from, valid_to, document_url,
				currency_code, currency_name, rate, unit, rate_per_unit, change, raw_payload, created_at
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, NOW()
			)
		`,
			payload.Event,
			payload.Source,
			payload.Period.KMKNumber,
			validFrom,
			validTo,
			payload.Period.DocumentURL,
			rate.CurrencyCode,
			rate.CurrencyName,
			rate.Rate,
			rate.Unit,
			ratePerUnitFloat,
			rate.Change,
			rawJSON,
		)
		if err != nil {
			return saved, fmt.Errorf("failed inserting exchange rate %s: %w", rate.CurrencyCode, err)
		}
		saved++
	}

	// Insert audit trail log ke integration_logs
	meta := fmt.Sprintf(`{"kmk_number":"%s","rates_count":%d,"source":"%s"}`, payload.Period.KMKNumber, len(payload.Rates), payload.Source)
	_ = r.InsertIntegrationLog(ctx, uuid.New().String(), "MAXIMO", "POSTGRESQL", "EXCHANGE_RATE", payload.Period.KMKNumber, "SUCCESS", "", meta)

	return saved, nil
}

func (r *PostgresRepository) GetExchangeRates(ctx context.Context, limit int) ([]model.ExchangeRateRecord, error) {
	if r.Pool == nil {
		return nil, fmt.Errorf("postgres pool not available")
	}
	if limit <= 0 {
		limit = 100
	}

	rows, err := r.Pool.Query(ctx, `
		SELECT id, COALESCE(event, ''), COALESCE(source, ''), COALESCE(kmk_number, ''),
		       COALESCE(TO_CHAR(valid_from, 'YYYY-MM-DD'), ''),
		       COALESCE(TO_CHAR(valid_to, 'YYYY-MM-DD'), ''),
		       COALESCE(document_url, ''), currency_code, COALESCE(currency_name, ''),
		       rate, COALESCE(unit, 1), COALESCE(rate_per_unit, 0), change, created_at
		FROM exchange_rates
		ORDER BY id DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []model.ExchangeRateRecord
	for rows.Next() {
		var rec model.ExchangeRateRecord
		if err := rows.Scan(
			&rec.ID, &rec.Event, &rec.Source, &rec.KMKNumber,
			&rec.ValidFrom, &rec.ValidTo, &rec.DocumentURL,
			&rec.CurrencyCode, &rec.CurrencyName, &rec.Rate,
			&rec.Unit, &rec.RatePerUnit, &rec.Change, &rec.CreatedAt,
		); err == nil {
			records = append(records, rec)
		}
	}
	return records, nil
}

// 11. UOM Mapping Operations
func (r *PostgresRepository) GetUOMMappings(ctx context.Context) ([]model.UOMMapping, error) {
	if r.Pool == nil {
		return nil, fmt.Errorf("postgres pool not available")
	}

	rows, err := r.Pool.Query(ctx, `
		SELECT id, maximo_uom, sap_uom, COALESCE(description, ''), is_active, created_at, updated_at
		FROM uom_mappings
		ORDER BY maximo_uom ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []model.UOMMapping
	for rows.Next() {
		var u model.UOMMapping
		if err := rows.Scan(&u.ID, &u.MaximoUOM, &u.SAPUOM, &u.Description, &u.IsActive, &u.CreatedAt, &u.UpdatedAt); err == nil {
			list = append(list, u)
		}
	}
	return list, nil
}

func (r *PostgresRepository) GetSAPUOM(ctx context.Context, maximoUOM string) (string, error) {
	if r.Pool == nil {
		return maximoUOM, nil
	}

	var sapUOM string
	err := r.Pool.QueryRow(ctx, `
		SELECT sap_uom FROM uom_mappings
		WHERE UPPER(maximo_uom) = UPPER($1) AND is_active = true
		LIMIT 1
	`, maximoUOM).Scan(&sapUOM)
	if err != nil {
		return maximoUOM, nil // Fallback to original Maximo UOM if no mapping found
	}
	return sapUOM, nil
}


