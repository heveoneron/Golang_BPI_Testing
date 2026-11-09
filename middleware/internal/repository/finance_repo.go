package repository

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"golang-bpi-testing/internal/model"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

//go:embed migrations/finance.sql
var financeMigrationSQL string

// ErrAlreadyPosted dikembalikan saat dokumen dengan kunci yang sama sudah sukses diposting ke SAP.
var ErrAlreadyPosted = errors.New("dokumen dengan nomor yang sama sudah sukses diposting ke SAP")

// ErrNotFound dikembalikan saat data yang dicari tidak ada.
var ErrNotFound = errors.New("data tidak ditemukan")

// nullIfEmpty mengubah string kosong menjadi NULL untuk kolom DATE / opsional.
func nullIfEmpty(s string) interface{} {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

// MigrateFinance membuat tabel RICEF 1-9 dan mendaftarkan endpoint SAP baru.
// Endpoint di-seed dengan ON CONFLICT DO NOTHING agar perubahan dari Panel tidak tertimpa,
// dan tanpa password (Engine memakai SAP_PASS dari .env bila password kosong).
func (r *PostgresRepository) MigrateFinance(ctx context.Context) error {
	if r.Pool == nil {
		return nil
	}
	if err := r.EnsureSAPEndpointsTable(ctx); err != nil {
		return fmt.Errorf("ensure sap_endpoints: %w", err)
	}
	if _, err := r.Pool.Exec(ctx, financeMigrationSQL); err != nil {
		return fmt.Errorf("finance migration: %w", err)
	}

	baseURL := strings.TrimSpace(os.Getenv("SAP_BASE_URL"))
	if baseURL == "" {
		baseURL = "MOCK"
	}

	type seed struct {
		code, name, scenario, version, path, csrf, user string
		active                                          bool
	}
	seeds := []seed{
		{model.DocTypeBudget, "Cost Center Budget Supplement & Transfer (endpoint TBD)", "TBD", "v4", "/TBD/CostCenterBudget", "", "", false},
		{model.DocTypeJournalGI, "Journal Entry GI (SOAP XML)", "SAP_COM_0002", "soap", "/sap/bc/srt/scs_ext/sap/journalentrycreaterequestconfi", "", "Y_BPI_CU_SAP_COM_0002", true},
		{model.ServiceGLLineItems, "G/L Account Line Items Query (OData v2)", "SAP_COM_0303", "v2", "/sap/opu/odata/sap/API_GLACCOUNTLINEITEM/GLAccountLineItem", "", "Y_BPI_CU_SAP_COM_0303", true},
		{model.ServicePaymentProposal, "Payment Proposal CDS (OData v2)", "YY1_PAYMENTPROPOSAL", "v2", "/sap/opu/odata/sap/YY1_PAYMENTPROPOSAL_CDS/YY1_PaymentProposal", "", "Y_BPI_CU_PAYMENTPROPOSAL", true},
		{model.DocTypeExchangeRate, "Currency Exchange Rate KMK (OData v4)", "Z_ZFI_COM_0001", "v4", "/sap/opu/odata4/sap/z_fi_sb_currencyexchangerate/srvd_a2x/sap/z_fi_sd_currencyexchangerate/0001/CurrencyExchangeRate", "/sap/opu/odata4/sap/z_fi_sb_currencyexchangerate/srvd_a2x/sap/z_fi_sd_currencyexchangerate/0001/", "Z_BPI_CU_ZFI_COM_0001", true},
		{model.DocTypeSupplierInvoice, "Supplier Invoice Reimbursement & Business Trip (OData v2)", "SAP_COM_0057", "v2", "/sap/opu/odata/sap/API_SUPPLIERINVOICE_PROCESS_SRV/A_SupplierInvoice", "/sap/opu/odata/sap/API_SUPPLIERINVOICE_PROCESS_SRV/", "Y_BPI_CU_SAP_COM_0057", true},
		{model.ServiceFixedAsset, "Fixed Asset Master Data (OData v4)", "SAP_COM_0A93", "v4", "/sap/opu/odata4/sap/api_fixedasset/srvd_a2x/sap/fixedasset/0001/FixedAsset", "", "Y_BPI_CU_SAP_COM_0A93", true},
		{model.DocTypeAssetAcq, "Fixed Asset Acquisition Post (OData v4)", "SAP_COM_0A95", "v4", "/sap/opu/odata4/sap/api_fixedassetacquisition/srvd_a2x/sap/fixedassetacquisition/0001/FixedAssetAcquisition/SAP__self.Post", "/sap/opu/odata4/sap/api_fixedassetacquisition/srvd_a2x/sap/fixedassetacquisition/0001/FixedAssetAcquisition", "Y_BPI_CU_SAP_COM_0A95", true},
	}
	for _, s := range seeds {
		_, err := r.Pool.Exec(ctx, `
			INSERT INTO sap_endpoints (service_code, service_name, scenario_id, odata_version, base_url, endpoint_path, csrf_path, username, password, is_active)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, '', $9)
			ON CONFLICT (service_code) DO NOTHING
		`, s.code, s.name, s.scenario, s.version, baseURL, s.path, nullIfEmpty(s.csrf), s.user, s.active)
		if err != nil {
			return fmt.Errorf("seed endpoint %s: %w", s.code, err)
		}
	}
	if err := r.seedValuationClassGLMappings(ctx); err != nil {
		return fmt.Errorf("seed valuation class gl mapping: %w", err)
	}
	if err := r.seedReimbursementMappings(ctx); err != nil {
		return fmt.Errorf("seed reimbursement mapping: %w", err)
	}
	return nil
}

// ===================== Dokumen generik =====================

// QueueDocument menyimpan dokumen baru berstatus pending. Dokumen dengan kunci sama yang sudah
// sukses tidak ditimpa (ErrAlreadyPosted).
func (r *PostgresRepository) QueueDocument(ctx context.Context, doc model.Document) error {
	if r.Pool == nil {
		return fmt.Errorf("postgres pool not available")
	}
	if doc.ID == uuid.Nil {
		doc.ID = uuid.New()
	}
	if doc.FlowType == "" {
		doc.FlowType = "forward"
	}
	if doc.GroupID == "" {
		doc.GroupID = fmt.Sprintf("GRP-%s-%s", doc.DocType, doc.DocumentNo)
	}
	tag, err := r.Pool.Exec(ctx, `
		INSERT INTO documents (
			id, doc_type, document_no, flow_type, group_id, raw_payload, sap_payload,
			status, retry_count, sync_back_status, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, 'pending', 0, 'pending', NOW(), NOW())
		ON CONFLICT (doc_type, document_no, flow_type) DO UPDATE SET
			raw_payload = EXCLUDED.raw_payload,
			sap_payload = EXCLUDED.sap_payload,
			status = 'pending',
			retry_count = 0,
			error_message = NULL,
			updated_at = NOW()
		WHERE documents.status <> 'success'
	`, doc.ID, doc.DocType, doc.DocumentNo, doc.FlowType, doc.GroupID, doc.RawPayload, doc.SAPPayload)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrAlreadyPosted
	}
	return nil
}

// GetDocumentStatus mengembalikan status dokumen (found=false bila belum ada).
func (r *PostgresRepository) GetDocumentStatus(ctx context.Context, docType, docNo string) (string, bool, error) {
	if r.Pool == nil {
		return "", false, nil
	}
	var status string
	err := r.Pool.QueryRow(ctx, `SELECT status FROM documents WHERE doc_type = $1 AND document_no = $2 AND flow_type = 'forward'`, docType, docNo).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return status, true, nil
}

// MarkDocumentSkipped menandai dokumen yang tidak dikirim karena mapping / endpoint nonaktif.
// Dokumen skipped diambil ulang otomatis oleh FetchAndLockPendingDocuments setelah jeda.
func (r *PostgresRepository) MarkDocumentSkipped(ctx context.Context, id uuid.UUID, reason string) error {
	if r.Pool == nil {
		return nil
	}
	_, err := r.Pool.Exec(ctx, `UPDATE documents SET status = 'skipped', error_message = $1, updated_at = NOW() WHERE id = $2`, reason, id)
	return err
}

// ListDocumentsByType menampilkan dokumen satu doc_type, terbaru dulu.
func (r *PostgresRepository) ListDocumentsByType(ctx context.Context, docType string, limit int) ([]model.Document, error) {
	if r.Pool == nil {
		return nil, fmt.Errorf("postgres pool not available")
	}
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := r.Pool.Query(ctx, `
		SELECT id, doc_type, document_no, flow_type, group_id, raw_payload, sap_payload, status, retry_count,
		       last_attempt_at, sap_document_no, error_message, created_at, updated_at
		FROM documents WHERE doc_type = $1
		ORDER BY created_at DESC LIMIT $2
	`, docType, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Document
	for rows.Next() {
		var d model.Document
		if err := rows.Scan(&d.ID, &d.DocType, &d.DocumentNo, &d.FlowType, &d.GroupID, &d.RawPayload, &d.SAPPayload,
			&d.Status, &d.RetryCount, &d.LastAttemptAt, &d.SAPDocumentNo, &d.ErrorMessage, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ===================== RICEF 1: Budget =====================

func (r *PostgresRepository) ListBudgetMasters(ctx context.Context, year int) ([]model.BudgetMaster, error) {
	if r.Pool == nil {
		return nil, fmt.Errorf("postgres pool not available")
	}
	rows, err := r.Pool.Query(ctx, `
		SELECT m.id, m.budget_combination, m.budget_year, COALESCE(m.budget_code, ''), COALESCE(m.budget_type, ''),
		       m.approved_budget::float8,
		       (m.approved_budget - COALESCE((SELECT SUM(l.lock_amount) FROM budget_transaction_locks l
		            WHERE l.budget_combination = m.budget_combination AND l.budget_year = m.budget_year
		              AND l.lock_status IN ('RESERVED', 'COMMITTED')), 0))::float8 AS available,
		       m.remaining_budget_payment_based::float8, COALESCE(m.status, ''), m.updated_at
		FROM budget_masters m
		WHERE ($1 = 0 OR m.budget_year = $1)
		ORDER BY m.budget_year DESC, m.budget_combination ASC
	`, year)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.BudgetMaster
	for rows.Next() {
		var m model.BudgetMaster
		if err := rows.Scan(&m.ID, &m.BudgetCombination, &m.BudgetYear, &m.BudgetCode, &m.Type, &m.ApprovedBudget,
			&m.RemainingBudget, &m.RemainingBudgetPaymentBased, &m.Status, &m.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// UpsertBudgetMaster menyimpan pagu. Field Type, RemainingBudgetPaymentBased, Status opsional
// (dokumen mapping Budget SAP Grow -> Maximo). Status kosong = APPR.
func (r *PostgresRepository) UpsertBudgetMaster(ctx context.Context, m model.BudgetMaster) error {
	if r.Pool == nil {
		return fmt.Errorf("postgres pool not available")
	}
	if m.Status == "" {
		m.Status = "APPR"
	}
	_, err := r.Pool.Exec(ctx, `
		INSERT INTO budget_masters (budget_combination, budget_year, budget_code, budget_type, approved_budget, remaining_budget,
			remaining_budget_payment_based, status, updated_at)
		VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), $5, $5, $6, $7, NOW())
		ON CONFLICT (budget_combination, budget_year) DO UPDATE
		SET approved_budget = EXCLUDED.approved_budget, remaining_budget = EXCLUDED.remaining_budget,
		    budget_code = EXCLUDED.budget_code, budget_type = EXCLUDED.budget_type,
		    remaining_budget_payment_based = EXCLUDED.remaining_budget_payment_based,
		    status = EXCLUDED.status, updated_at = NOW()
	`, m.BudgetCombination, m.BudgetYear, m.BudgetCode, m.Type, m.ApprovedBudget, m.RemainingBudgetPaymentBased, m.Status)
	return err
}

// availableBudgetTx mengunci baris budget_masters (FOR UPDATE) lalu menghitung sisa pagu.
// found=false bila kombinasi belum terdaftar.
func availableBudgetTx(ctx context.Context, tx pgx.Tx, combo string, year int, excludeType, excludeNo string) (float64, bool, error) {
	var approved float64
	err := tx.QueryRow(ctx, `
		SELECT approved_budget::float8 FROM budget_masters
		WHERE budget_combination = $1 AND budget_year = $2
		FOR UPDATE
	`, combo, year).Scan(&approved)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	var locked float64
	err = tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(lock_amount), 0)::float8 FROM budget_transaction_locks
		WHERE budget_combination = $1 AND budget_year = $2 AND lock_status IN ('RESERVED', 'COMMITTED')
		  AND NOT (reference_doc_type = $3 AND reference_doc_no = $4)
	`, combo, year, excludeType, excludeNo).Scan(&locked)
	if err != nil {
		return 0, true, err
	}
	return approved - locked, true, nil
}

// CreateBudgetAdjustment menyimpan supplement / transfer. Untuk transfer, pagu asal dicek dan
// ditahan (lock BUDGET_TRANSFER) dalam satu transaksi agar tidak terpakai dua kali.
func (r *PostgresRepository) CreateBudgetAdjustment(ctx context.Context, adj *model.BudgetAdjustment) error {
	if r.Pool == nil {
		return fmt.Errorf("postgres pool not available")
	}
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if adj.AdjustmentType == "TRANSFER" {
		avail, found, err := availableBudgetTx(ctx, tx, adj.FromCombination, adj.BudgetYear, "BUDGET_TRANSFER", adj.DocumentNo)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("pagu asal %s tahun %d belum terdaftar di budget master", adj.FromCombination, adj.BudgetYear)
		}
		if avail < adj.Amount {
			return fmt.Errorf("sisa pagu asal %s tidak cukup: tersedia %.2f, diminta %.2f", adj.FromCombination, avail, adj.Amount)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO budget_transaction_locks (budget_combination, budget_year, reference_doc_type, reference_doc_no, lock_amount, lock_status, created_at, updated_at)
			VALUES ($1, $2, 'BUDGET_TRANSFER', $3, $4, 'RESERVED', NOW(), NOW())
			ON CONFLICT (reference_doc_type, reference_doc_no) DO UPDATE
			SET lock_amount = EXCLUDED.lock_amount, lock_status = 'RESERVED', updated_at = NOW()
		`, adj.FromCombination, adj.BudgetYear, adj.DocumentNo, adj.Amount); err != nil {
			return err
		}
	}

	err = tx.QueryRow(ctx, `
		INSERT INTO budget_adjustments (document_no, adjustment_type, budget_year, from_combination, to_combination, amount, reason, requested_by, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'PENDING', NOW(), NOW())
		RETURNING id, created_at, updated_at
	`, adj.DocumentNo, adj.AdjustmentType, adj.BudgetYear, nullIfEmpty(adj.FromCombination), adj.ToCombination,
		adj.Amount, adj.Reason, adj.RequestedBy).Scan(&adj.ID, &adj.CreatedAt, &adj.UpdatedAt)
	if err != nil {
		return err
	}
	adj.Status = "PENDING"
	return tx.Commit(ctx)
}

// ApplyBudgetAdjustment menerapkan supplement / transfer ke budget_masters setelah SAP sukses.
func (r *PostgresRepository) ApplyBudgetAdjustment(ctx context.Context, docNo string) error {
	if r.Pool == nil {
		return nil
	}
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var adjType, fromCombo, toCombo, status string
	var year int
	var amount float64
	err = tx.QueryRow(ctx, `
		SELECT adjustment_type, COALESCE(from_combination, ''), to_combination, budget_year, amount::float8, status
		FROM budget_adjustments WHERE document_no = $1 FOR UPDATE
	`, docNo).Scan(&adjType, &fromCombo, &toCombo, &year, &amount, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if status == "APPLIED" {
		return nil
	}

	addTo := func(combo string, delta float64) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO budget_masters (budget_combination, budget_year, budget_code, approved_budget, remaining_budget, status, updated_at)
			VALUES ($1::text, $2, NULLIF(substring($1::text from position('-' in $1::text) + 1), ''), $3, $3, 'APPR', NOW())
			ON CONFLICT (budget_combination, budget_year) DO UPDATE
			SET approved_budget = budget_masters.approved_budget + $3,
			    remaining_budget = budget_masters.remaining_budget + $3,
			    updated_at = NOW()
		`, combo, year, delta)
		return err
	}

	if adjType == "TRANSFER" {
		if err := addTo(fromCombo, -amount); err != nil {
			return err
		}
		// Pagu asal sudah dikurangi langsung, sehingga lock penahan transfer dilepas.
		if _, err := tx.Exec(ctx, `
			UPDATE budget_transaction_locks SET lock_status = 'RELEASED', updated_at = NOW()
			WHERE reference_doc_type = 'BUDGET_TRANSFER' AND reference_doc_no = $1
		`, docNo); err != nil {
			return err
		}
	}
	if err := addTo(toCombo, amount); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE budget_adjustments SET status = 'APPLIED', error_message = NULL, updated_at = NOW() WHERE document_no = $1`, docNo); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// FailBudgetAdjustment menandai penyesuaian gagal dan melepas lock transfernya.
func (r *PostgresRepository) FailBudgetAdjustment(ctx context.Context, docNo, msg string) error {
	if r.Pool == nil {
		return nil
	}
	if _, err := r.Pool.Exec(ctx, `UPDATE budget_adjustments SET status = 'FAILED', error_message = $2, updated_at = NOW() WHERE document_no = $1 AND status <> 'APPLIED'`, docNo, msg); err != nil {
		return err
	}
	return r.ReleaseBudgetLocks(ctx, "BUDGET_TRANSFER", docNo)
}

func (r *PostgresRepository) ListBudgetAdjustments(ctx context.Context, limit int) ([]model.BudgetAdjustment, error) {
	if r.Pool == nil {
		return nil, fmt.Errorf("postgres pool not available")
	}
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := r.Pool.Query(ctx, `
		SELECT id, document_no, adjustment_type, budget_year, COALESCE(from_combination, ''), to_combination,
		       amount::float8, COALESCE(reason, ''), COALESCE(requested_by, ''), status, COALESCE(error_message, ''),
		       created_at, updated_at
		FROM budget_adjustments ORDER BY id DESC LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.BudgetAdjustment
	for rows.Next() {
		var a model.BudgetAdjustment
		if err := rows.Scan(&a.ID, &a.DocumentNo, &a.AdjustmentType, &a.BudgetYear, &a.FromCombination, &a.ToCombination,
			&a.Amount, &a.Reason, &a.RequestedBy, &a.Status, &a.ErrorMessage, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) ListBudgetLocks(ctx context.Context, status string, limit int) ([]model.BudgetTransactionLock, error) {
	if r.Pool == nil {
		return nil, fmt.Errorf("postgres pool not available")
	}
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := r.Pool.Query(ctx, `
		SELECT id, budget_combination, budget_year, reference_doc_type, reference_doc_no, lock_amount::float8, lock_status, created_at, updated_at
		FROM budget_transaction_locks
		WHERE ($1 = '' OR lock_status = $1)
		ORDER BY id DESC LIMIT $2
	`, strings.ToUpper(status), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.BudgetTransactionLock
	for rows.Next() {
		var l model.BudgetTransactionLock
		if err := rows.Scan(&l.ID, &l.BudgetCombination, &l.BudgetYear, &l.ReferenceDocType, &l.ReferenceDocNo, &l.LockAmount, &l.LockStatus, &l.CreatedAt, &l.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// setBudgetLockStatus mengubah status lock milik satu dokumen. Lock per kombinasi (misal jurnal
// dengan beberapa cost center) memakai reference_doc_no "<docNo>|<kombinasi>" dan ikut berubah.
func (r *PostgresRepository) setBudgetLockStatus(ctx context.Context, refType, refNo, newStatus string, fromStatuses []string) (int64, error) {
	if r.Pool == nil {
		return 0, nil
	}
	tag, err := r.Pool.Exec(ctx, `
		UPDATE budget_transaction_locks SET lock_status = $3, updated_at = NOW()
		WHERE reference_doc_type = $1
		  AND (reference_doc_no = $2 OR left(reference_doc_no, length($2) + 1) = $2 || '|')
		  AND lock_status = ANY($4)
	`, refType, refNo, newStatus, fromStatuses)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// CommitBudgetLocks: SAP sukses -> RESERVED menjadi COMMITTED.
func (r *PostgresRepository) CommitBudgetLocks(ctx context.Context, refType, refNo string) error {
	_, err := r.setBudgetLockStatus(ctx, refType, refNo, "COMMITTED", []string{"RESERVED"})
	return err
}

// ReleaseBudgetLocks: SAP menolak / dibatalkan -> RESERVED menjadi RELEASED.
func (r *PostgresRepository) ReleaseBudgetLocks(ctx context.Context, refType, refNo string) error {
	_, err := r.setBudgetLockStatus(ctx, refType, refNo, "RELEASED", []string{"RESERVED"})
	return err
}

// ReleaseBudgetLocksManual melepas lock RESERVED maupun COMMITTED (dipakai dari Panel).
func (r *PostgresRepository) ReleaseBudgetLocksManual(ctx context.Context, refType, refNo string) (int64, error) {
	return r.setBudgetLockStatus(ctx, refType, refNo, "RELEASED", []string{"RESERVED", "COMMITTED"})
}

// ===================== RICEF 3/4: GL Line Items =====================

func (r *PostgresRepository) UpsertGLLineItems(ctx context.Context, items []model.GLLineItem) (int, error) {
	if r.Pool == nil || len(items) == 0 {
		return 0, nil
	}
	batch := &pgx.Batch{}
	for _, it := range items {
		var postingDate, documentDate interface{}
		if it.PostingDate != nil {
			postingDate = nullIfEmpty(*it.PostingDate)
		}
		if it.DocumentDate != nil {
			documentDate = nullIfEmpty(*it.DocumentDate)
		}
		batch.Queue(`
			INSERT INTO sap_gl_line_items (
				gl_line_item_key, company_code, fiscal_year, accounting_document, ledger_gl_line_item,
				accounting_document_type, posting_date, document_date, gl_account, cost_center, profit_center,
				supplier, debit_credit_code, amount_in_company_code_currency, company_code_currency,
				amount_in_transaction_currency, transaction_currency, document_item_text, raw, synced_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,NOW())
			ON CONFLICT (gl_line_item_key) DO UPDATE SET
				company_code = EXCLUDED.company_code, fiscal_year = EXCLUDED.fiscal_year,
				accounting_document = EXCLUDED.accounting_document, ledger_gl_line_item = EXCLUDED.ledger_gl_line_item,
				accounting_document_type = EXCLUDED.accounting_document_type, posting_date = EXCLUDED.posting_date,
				document_date = EXCLUDED.document_date, gl_account = EXCLUDED.gl_account, cost_center = EXCLUDED.cost_center,
				profit_center = EXCLUDED.profit_center, supplier = EXCLUDED.supplier, debit_credit_code = EXCLUDED.debit_credit_code,
				amount_in_company_code_currency = EXCLUDED.amount_in_company_code_currency,
				company_code_currency = EXCLUDED.company_code_currency,
				amount_in_transaction_currency = EXCLUDED.amount_in_transaction_currency,
				transaction_currency = EXCLUDED.transaction_currency, document_item_text = EXCLUDED.document_item_text,
				raw = EXCLUDED.raw, synced_at = NOW()
		`, it.Key, it.CompanyCode, it.FiscalYear, it.AccountingDocument, it.LedgerGLLineItem, it.AccountingDocumentType,
			postingDate, documentDate, it.GLAccount, it.CostCenter, it.ProfitCenter, it.Supplier, it.DebitCreditCode,
			it.AmountInCompanyCodeCurrency, it.CompanyCodeCurrency, it.AmountInTransactionCurrency, it.TransactionCurrency,
			it.DocumentItemText, it.Raw)
	}
	return execBatch(ctx, r, batch, len(items))
}

func execBatch(ctx context.Context, r *PostgresRepository, batch *pgx.Batch, n int) (int, error) {
	br := r.Pool.SendBatch(ctx, batch)
	defer br.Close()
	done := 0
	for i := 0; i < n; i++ {
		if _, err := br.Exec(); err != nil {
			return done, err
		}
		done++
	}
	return done, nil
}

// GLLineItemFilter adalah filter daftar GL line item tersimpan.
type GLLineItemFilter struct {
	CompanyCode string
	FromDate    string
	ToDate      string
	DocTypes    []string
	Search      string
	Limit       int
}

func (r *PostgresRepository) ListGLLineItems(ctx context.Context, f GLLineItemFilter) ([]model.GLLineItem, error) {
	if r.Pool == nil {
		return nil, fmt.Errorf("postgres pool not available")
	}
	if f.Limit <= 0 || f.Limit > 2000 {
		f.Limit = 500
	}
	rows, err := r.Pool.Query(ctx, `
		SELECT gl_line_item_key, COALESCE(company_code, ''), COALESCE(fiscal_year, ''), COALESCE(accounting_document, ''),
		       COALESCE(ledger_gl_line_item, ''), COALESCE(accounting_document_type, ''),
		       TO_CHAR(posting_date, 'YYYY-MM-DD'), TO_CHAR(document_date, 'YYYY-MM-DD'),
		       COALESCE(gl_account, ''), COALESCE(cost_center, ''), COALESCE(profit_center, ''), COALESCE(supplier, ''),
		       COALESCE(debit_credit_code, ''), COALESCE(amount_in_company_code_currency, 0)::float8, COALESCE(company_code_currency, ''),
		       COALESCE(amount_in_transaction_currency, 0)::float8, COALESCE(transaction_currency, ''),
		       COALESCE(document_item_text, ''), synced_at
		FROM sap_gl_line_items
		WHERE ($1 = '' OR company_code = $1)
		  AND ($2 = '' OR posting_date >= NULLIF($2, '')::date)
		  AND ($3 = '' OR posting_date <= NULLIF($3, '')::date)
		  AND (COALESCE(cardinality($4::text[]), 0) = 0 OR accounting_document_type = ANY($4))
		  AND ($5 = '' OR accounting_document ILIKE '%' || $5 || '%' OR gl_account ILIKE '%' || $5 || '%' OR document_item_text ILIKE '%' || $5 || '%')
		ORDER BY posting_date DESC NULLS LAST, accounting_document DESC, ledger_gl_line_item ASC
		LIMIT $6
	`, f.CompanyCode, f.FromDate, f.ToDate, f.DocTypes, f.Search, f.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.GLLineItem
	for rows.Next() {
		var it model.GLLineItem
		if err := rows.Scan(&it.Key, &it.CompanyCode, &it.FiscalYear, &it.AccountingDocument, &it.LedgerGLLineItem,
			&it.AccountingDocumentType, &it.PostingDate, &it.DocumentDate, &it.GLAccount, &it.CostCenter, &it.ProfitCenter,
			&it.Supplier, &it.DebitCreditCode, &it.AmountInCompanyCodeCurrency, &it.CompanyCodeCurrency,
			&it.AmountInTransactionCurrency, &it.TransactionCurrency, &it.DocumentItemText, &it.SyncedAt); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// ===================== RICEF 5: Payment Proposal =====================

func (r *PostgresRepository) UpsertPaymentProposals(ctx context.Context, items []model.PaymentProposal) (int, error) {
	if r.Pool == nil || len(items) == 0 {
		return 0, nil
	}
	batch := &pgx.Batch{}
	for _, p := range items {
		var runDate interface{}
		if p.PaymentRunDate != nil {
			runDate = nullIfEmpty(*p.PaymentRunDate)
		}
		batch.Queue(`
			INSERT INTO sap_payment_proposals (record_key, payment_run_id, payment_run_date, company_code, supplier, payment_document, amount, currency, raw, synced_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,NOW())
			ON CONFLICT (record_key) DO UPDATE SET
				payment_run_id = EXCLUDED.payment_run_id, payment_run_date = EXCLUDED.payment_run_date,
				company_code = EXCLUDED.company_code, supplier = EXCLUDED.supplier,
				payment_document = EXCLUDED.payment_document, amount = EXCLUDED.amount,
				currency = EXCLUDED.currency, raw = EXCLUDED.raw, synced_at = NOW()
		`, p.RecordKey, p.PaymentRunID, runDate, p.CompanyCode, p.Supplier, p.PaymentDocument, p.Amount, p.Currency, p.Raw)
	}
	return execBatch(ctx, r, batch, len(items))
}

func (r *PostgresRepository) ListPaymentProposals(ctx context.Context, search string, limit int) ([]model.PaymentProposal, error) {
	if r.Pool == nil {
		return nil, fmt.Errorf("postgres pool not available")
	}
	if limit <= 0 || limit > 2000 {
		limit = 500
	}
	rows, err := r.Pool.Query(ctx, `
		SELECT record_key, COALESCE(payment_run_id, ''), TO_CHAR(payment_run_date, 'YYYY-MM-DD'), COALESCE(company_code, ''),
		       COALESCE(supplier, ''), COALESCE(payment_document, ''), COALESCE(amount, 0)::float8, COALESCE(currency, ''), raw, synced_at
		FROM sap_payment_proposals
		WHERE ($1 = '' OR payment_run_id ILIKE '%' || $1 || '%' OR supplier ILIKE '%' || $1 || '%' OR payment_document ILIKE '%' || $1 || '%')
		ORDER BY payment_run_date DESC NULLS LAST, payment_run_id DESC
		LIMIT $2
	`, search, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.PaymentProposal
	for rows.Next() {
		var p model.PaymentProposal
		if err := rows.Scan(&p.RecordKey, &p.PaymentRunID, &p.PaymentRunDate, &p.CompanyCode, &p.Supplier, &p.PaymentDocument,
			&p.Amount, &p.Currency, &p.Raw, &p.SyncedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ===================== RICEF 9: Fixed Asset =====================

func (r *PostgresRepository) UpsertFixedAssets(ctx context.Context, items []model.FixedAsset) (int, error) {
	if r.Pool == nil || len(items) == 0 {
		return 0, nil
	}
	batch := &pgx.Batch{}
	for _, a := range items {
		var capDate interface{}
		if a.CapitalizationDate != nil {
			capDate = nullIfEmpty(*a.CapitalizationDate)
		}
		batch.Queue(`
			INSERT INTO sap_fixed_assets (company_code, master_fixed_asset, fixed_asset, description, asset_class, capitalization_date, raw, synced_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,NOW())
			ON CONFLICT (company_code, master_fixed_asset, fixed_asset) DO UPDATE SET
				description = EXCLUDED.description, asset_class = EXCLUDED.asset_class,
				capitalization_date = EXCLUDED.capitalization_date, raw = EXCLUDED.raw, synced_at = NOW()
		`, a.CompanyCode, a.MasterFixedAsset, a.FixedAsset, a.Description, a.AssetClass, capDate, a.Raw)
	}
	return execBatch(ctx, r, batch, len(items))
}

func (r *PostgresRepository) ListFixedAssets(ctx context.Context, companyCode, search string, limit int) ([]model.FixedAsset, error) {
	if r.Pool == nil {
		return nil, fmt.Errorf("postgres pool not available")
	}
	if limit <= 0 || limit > 2000 {
		limit = 500
	}
	rows, err := r.Pool.Query(ctx, `
		SELECT company_code, master_fixed_asset, fixed_asset, COALESCE(description, ''), COALESCE(asset_class, ''),
		       TO_CHAR(capitalization_date, 'YYYY-MM-DD'), synced_at
		FROM sap_fixed_assets
		WHERE ($1 = '' OR company_code = $1)
		  AND ($2 = '' OR master_fixed_asset ILIKE '%' || $2 || '%' OR description ILIKE '%' || $2 || '%')
		ORDER BY company_code, master_fixed_asset, fixed_asset
		LIMIT $3
	`, companyCode, search, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.FixedAsset
	for rows.Next() {
		var a model.FixedAsset
		if err := rows.Scan(&a.CompanyCode, &a.MasterFixedAsset, &a.FixedAsset, &a.Description, &a.AssetClass, &a.CapitalizationDate, &a.SyncedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) FixedAssetExists(ctx context.Context, companyCode, masterFixedAsset, fixedAsset string) (bool, error) {
	if r.Pool == nil {
		return false, fmt.Errorf("postgres pool not available")
	}
	var n int
	err := r.Pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM sap_fixed_assets WHERE company_code = $1 AND master_fixed_asset = $2 AND fixed_asset = $3
	`, companyCode, masterFixedAsset, fixedAsset).Scan(&n)
	return n > 0, err
}

// ===================== RICEF 8: Asset Acquisition =====================

const assetAcqColumns = `
	id, reference_no, company_code, master_fixed_asset, fixed_asset,
	TO_CHAR(document_date, 'YYYY-MM-DD'), TO_CHAR(posting_date, 'YYYY-MM-DD'), TO_CHAR(asset_value_date, 'YYYY-MM-DD'),
	amount::float8, currency, offsetting_account, COALESCE(item_text, ''), status, parked_by,
	COALESCE(approved_by, ''), approved_at, COALESCE(rejected_by, ''), COALESCE(reject_reason, ''),
	COALESCE(sap_document_no, ''), COALESCE(error_message, ''), created_at, updated_at`

func scanAssetAcq(row pgx.Row) (*model.AssetAcquisition, error) {
	var a model.AssetAcquisition
	err := row.Scan(&a.ID, &a.Reference, &a.CompanyCode, &a.MasterFixedAsset, &a.FixedAsset,
		&a.DocumentDate, &a.PostingDate, &a.AssetValueDate, &a.Amount, &a.Currency, &a.OffsettingAccount, &a.ItemText,
		&a.Status, &a.ParkedBy, &a.ApprovedBy, &a.ApprovedAt, &a.RejectedBy, &a.RejectReason, &a.SAPDocumentNo,
		&a.ErrorMessage, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *PostgresRepository) CreateAssetAcquisition(ctx context.Context, req model.AssetAcquisitionRequest) (*model.AssetAcquisition, error) {
	if r.Pool == nil {
		return nil, fmt.Errorf("postgres pool not available")
	}
	row := r.Pool.QueryRow(ctx, `
		INSERT INTO asset_acquisitions (reference_no, company_code, master_fixed_asset, fixed_asset, document_date, posting_date,
			asset_value_date, amount, currency, offsetting_account, item_text, status, parked_by, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'PARKED',$12,NOW(),NOW())
		RETURNING `+assetAcqColumns,
		req.Reference, req.CompanyCode, req.MasterFixedAsset, req.FixedAsset, req.DocumentDate, req.PostingDate,
		req.AssetValueDate, req.Amount, req.Currency, req.OffsettingAccount, req.ItemText, req.ParkedBy)
	return scanAssetAcq(row)
}

func (r *PostgresRepository) GetAssetAcquisition(ctx context.Context, id uuid.UUID) (*model.AssetAcquisition, error) {
	if r.Pool == nil {
		return nil, fmt.Errorf("postgres pool not available")
	}
	return scanAssetAcq(r.Pool.QueryRow(ctx, `SELECT `+assetAcqColumns+` FROM asset_acquisitions WHERE id = $1`, id))
}

func (r *PostgresRepository) ListAssetAcquisitions(ctx context.Context, status string, limit int) ([]model.AssetAcquisition, error) {
	if r.Pool == nil {
		return nil, fmt.Errorf("postgres pool not available")
	}
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := r.Pool.Query(ctx, `SELECT `+assetAcqColumns+` FROM asset_acquisitions
		WHERE ($1 = '' OR status = $1) ORDER BY created_at DESC LIMIT $2`, strings.ToUpper(status), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.AssetAcquisition
	for rows.Next() {
		a, err := scanAssetAcq(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// TransitionAssetAcquisition mengunci baris (FOR UPDATE), memanggil check untuk validasi aturan
// bisnis, lalu menyimpan status baru. Seluruhnya dalam satu transaksi.
func (r *PostgresRepository) TransitionAssetAcquisition(ctx context.Context, id uuid.UUID,
	check func(a *model.AssetAcquisition) error, apply func(ctx context.Context, tx pgx.Tx, a *model.AssetAcquisition) error) (*model.AssetAcquisition, error) {
	if r.Pool == nil {
		return nil, fmt.Errorf("postgres pool not available")
	}
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	a, err := scanAssetAcq(tx.QueryRow(ctx, `SELECT `+assetAcqColumns+` FROM asset_acquisitions WHERE id = $1 FOR UPDATE`, id))
	if err != nil {
		return nil, err
	}
	if err := check(a); err != nil {
		return nil, err
	}
	if err := apply(ctx, tx, a); err != nil {
		return nil, err
	}
	updated, err := scanAssetAcq(tx.QueryRow(ctx, `SELECT `+assetAcqColumns+` FROM asset_acquisitions WHERE id = $1`, id))
	if err != nil {
		return nil, err
	}
	return updated, tx.Commit(ctx)
}

// QueueDocumentTx sama dengan QueueDocument tetapi di dalam transaksi yang sedang berjalan.
func QueueDocumentTx(ctx context.Context, tx pgx.Tx, doc model.Document) error {
	if doc.ID == uuid.Nil {
		doc.ID = uuid.New()
	}
	if doc.FlowType == "" {
		doc.FlowType = "forward"
	}
	if doc.GroupID == "" {
		doc.GroupID = fmt.Sprintf("GRP-%s-%s", doc.DocType, doc.DocumentNo)
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO documents (id, doc_type, document_no, flow_type, group_id, raw_payload, sap_payload,
			status, retry_count, sync_back_status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'pending', 0, 'pending', NOW(), NOW())
		ON CONFLICT (doc_type, document_no, flow_type) DO UPDATE SET
			raw_payload = EXCLUDED.raw_payload, sap_payload = EXCLUDED.sap_payload, status = 'pending',
			retry_count = 0, error_message = NULL, updated_at = NOW()
		WHERE documents.status <> 'success'
	`, doc.ID, doc.DocType, doc.DocumentNo, doc.FlowType, doc.GroupID, doc.RawPayload, doc.SAPPayload)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrAlreadyPosted
	}
	return nil
}

// UpdateAssetAcquisitionResult dipanggil hook pasca-kirim: POSTED (sukses) atau FAILED (ditolak SAP).
func (r *PostgresRepository) UpdateAssetAcquisitionResult(ctx context.Context, reference, status, sapDocNo, errMsg string) error {
	if r.Pool == nil {
		return nil
	}
	_, err := r.Pool.Exec(ctx, `
		UPDATE asset_acquisitions
		SET status = $2, sap_document_no = NULLIF($3, ''), error_message = NULLIF($4, ''), updated_at = NOW()
		WHERE reference_no = $1 AND status IN ('APPROVED', 'FAILED')
	`, reference, status, sapDocNo, errMsg)
	return err
}

// ===================== RICEF 7: Upload batch =====================

func (r *PostgresRepository) InsertUploadBatch(ctx context.Context, b *model.ExcelUploadBatch) error {
	if r.Pool == nil {
		return fmt.Errorf("postgres pool not available")
	}
	var errJSON []byte
	if len(b.Errors) > 0 {
		errJSON, _ = json.Marshal(b.Errors)
	}
	return r.Pool.QueryRow(ctx, `
		INSERT INTO excel_upload_batches (file_name, uploaded_by, total_rows, total_invoices, status, errors, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW()) RETURNING id, created_at
	`, b.FileName, b.UploadedBy, b.TotalRows, b.TotalInvoices, b.Status, errJSON).Scan(&b.ID, &b.CreatedAt)
}

func (r *PostgresRepository) ListUploadBatches(ctx context.Context, limit int) ([]model.ExcelUploadBatch, error) {
	if r.Pool == nil {
		return nil, fmt.Errorf("postgres pool not available")
	}
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows, err := r.Pool.Query(ctx, `
		SELECT id, COALESCE(file_name, ''), COALESCE(uploaded_by, ''), total_rows, total_invoices, status, errors, created_at
		FROM excel_upload_batches ORDER BY id DESC LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ExcelUploadBatch
	for rows.Next() {
		var b model.ExcelUploadBatch
		var errJSON []byte
		if err := rows.Scan(&b.ID, &b.FileName, &b.UploadedBy, &b.TotalRows, &b.TotalInvoices, &b.Status, &errJSON, &b.CreatedAt); err != nil {
			return nil, err
		}
		if len(errJSON) > 0 {
			_ = json.Unmarshal(errJSON, &b.Errors)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ===================== RICEF 6: Exchange Rate =====================

// GetExchangeRatesForKMK mengambil baris kurs satu nomor KMK (kosong = KMK terbaru yang diterima).
func (r *PostgresRepository) GetExchangeRatesForKMK(ctx context.Context, kmk string) ([]model.ExchangeRateRecord, error) {
	if r.Pool == nil {
		return nil, fmt.Errorf("postgres pool not available")
	}
	if strings.TrimSpace(kmk) == "" {
		err := r.Pool.QueryRow(ctx, `SELECT COALESCE(kmk_number, '') FROM exchange_rates ORDER BY id DESC LIMIT 1`).Scan(&kmk)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
	}
	rows, err := r.Pool.Query(ctx, `
		SELECT DISTINCT ON (currency_code)
		       id, COALESCE(event, ''), COALESCE(source, ''), COALESCE(kmk_number, ''),
		       COALESCE(TO_CHAR(valid_from, 'YYYY-MM-DD'), ''), COALESCE(TO_CHAR(valid_to, 'YYYY-MM-DD'), ''),
		       COALESCE(document_url, ''), currency_code, COALESCE(currency_name, ''),
		       rate::float8, COALESCE(unit, 1), COALESCE(rate_per_unit, 0)::float8, change::float8, created_at
		FROM exchange_rates WHERE COALESCE(kmk_number, '') = $1
		ORDER BY currency_code, id DESC
	`, kmk)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ExchangeRateRecord
	for rows.Next() {
		var rec model.ExchangeRateRecord
		if err := rows.Scan(&rec.ID, &rec.Event, &rec.Source, &rec.KMKNumber, &rec.ValidFrom, &rec.ValidTo, &rec.DocumentURL,
			&rec.CurrencyCode, &rec.CurrencyName, &rec.Rate, &rec.Unit, &rec.RatePerUnit, &rec.Change, &rec.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	if len(out) == 0 {
		return nil, ErrNotFound
	}
	return out, rows.Err()
}

// LogFinanceWarning mencatat peringatan ke integration_logs (dan log aplikasi).
func (r *PostgresRepository) LogFinanceWarning(ctx context.Context, docType, docNo, msg string) {
	log.Printf("[WARN] %s %s: %s", docType, docNo, msg)
	_ = r.InsertIntegrationLog(ctx, fmt.Sprintf("GRP-%s-%s", docType, docNo), "MIDDLEWARE", "MIDDLEWARE", docType, docNo, "WARNING", msg, msg)
}

// SyncWindowStart mengembalikan tanggal awal tarikan inkremental: sinkron terakhir dikurangi overlap.
func (r *PostgresRepository) SyncWindowStart(ctx context.Context, interfaceID string, overlap time.Duration, fallback time.Duration) time.Time {
	if r.Pool == nil {
		return time.Now().Add(-fallback)
	}
	var last time.Time
	err := r.Pool.QueryRow(ctx, `SELECT last_sync_timestamp FROM sap_sync_tracker WHERE interface_id = $1`, interfaceID).Scan(&last)
	if err != nil {
		return time.Now().Add(-fallback)
	}
	return last.Add(-overlap)
}
