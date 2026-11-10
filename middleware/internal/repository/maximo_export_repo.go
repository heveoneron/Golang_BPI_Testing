package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"golang-bpi-testing/internal/model"

	"github.com/jackc/pgx/v5"
)

// SAPSupplierInvoiceRecord adalah supplier invoice SAP yang disimpan sebelum ditulis ke Maximo.
type SAPSupplierInvoiceRecord struct {
	SupplierInvoice string
	FiscalYear      string
	CompanyCode     string
	PostingDate     *string
	Raw             json.RawMessage
}

func (r *PostgresRepository) UpsertSAPSupplierInvoices(ctx context.Context, items []SAPSupplierInvoiceRecord) (int, error) {
	if r.Pool == nil || len(items) == 0 {
		return 0, nil
	}
	batch := &pgx.Batch{}
	for _, it := range items {
		var pd interface{}
		if it.PostingDate != nil {
			pd = nullIfEmpty(*it.PostingDate)
		}
		batch.Queue(`
			INSERT INTO sap_supplier_invoices (supplier_invoice, fiscal_year, company_code, posting_date, raw, synced_at)
			VALUES ($1, $2, $3, $4, $5, NOW())
			ON CONFLICT (supplier_invoice, fiscal_year) DO UPDATE SET
				company_code = EXCLUDED.company_code, posting_date = EXCLUDED.posting_date, raw = EXCLUDED.raw, synced_at = NOW()
		`, it.SupplierInvoice, it.FiscalYear, it.CompanyCode, pd, it.Raw)
	}
	return execBatch(ctx, r, batch, len(items))
}

// ListSupplierInvoicesForExport mengembalikan invoice SAP yang belum pernah sukses ditulis ke Maximo.
func (r *PostgresRepository) ListSupplierInvoicesForExport(ctx context.Context, limit int) ([]SAPSupplierInvoiceRecord, error) {
	if r.Pool == nil {
		return nil, fmt.Errorf("postgres pool not available")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.Pool.Query(ctx, `
		SELECT s.supplier_invoice, s.fiscal_year, COALESCE(s.company_code, ''), TO_CHAR(s.posting_date, 'YYYY-MM-DD'), s.raw
		FROM sap_supplier_invoices s
		LEFT JOIN maximo_invoice_exports e
		  ON e.source_type = 'INVOICE' AND e.source_key = s.supplier_invoice || '/' || s.fiscal_year
		WHERE e.status IS NULL OR e.status <> 'EXPORTED'
		ORDER BY s.posting_date NULLS LAST, s.supplier_invoice
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SAPSupplierInvoiceRecord
	for rows.Next() {
		var s SAPSupplierInvoiceRecord
		if err := rows.Scan(&s.SupplierInvoice, &s.FiscalYear, &s.CompanyCode, &s.PostingDate, &s.Raw); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListJournalsForExport mengelompokkan GL line item jurnal (RICEF 3) per dokumen akuntansi yang belum
// ditulis ke Maximo. Hasil: map kunci dokumen -> baris line item (urut nomor item).
func (r *PostgresRepository) ListJournalsForExport(ctx context.Context, docTypes []string, limit int) ([]string, map[string][]model.GLLineItem, error) {
	if r.Pool == nil {
		return nil, nil, fmt.Errorf("postgres pool not available")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if docTypes == nil {
		docTypes = []string{}
	}
	rows, err := r.Pool.Query(ctx, `
		WITH docs AS (
			SELECT g.company_code, g.fiscal_year, g.accounting_document,
			       g.company_code || '/' || g.fiscal_year || '/' || g.accounting_document AS k,
			       MIN(g.posting_date) AS pd
			FROM sap_gl_line_items g
			WHERE COALESCE(cardinality($1::text[]), 0) = 0 OR g.accounting_document_type = ANY($1)
			GROUP BY g.company_code, g.fiscal_year, g.accounting_document
			HAVING bool_or(COALESCE(g.cost_center, '') <> '')
		)
		SELECT d.k, g.gl_line_item_key, COALESCE(g.company_code, ''), COALESCE(g.fiscal_year, ''), COALESCE(g.accounting_document, ''),
		       COALESCE(g.ledger_gl_line_item, ''), COALESCE(g.accounting_document_type, ''),
		       TO_CHAR(g.posting_date, 'YYYY-MM-DD'), TO_CHAR(g.document_date, 'YYYY-MM-DD'),
		       COALESCE(g.gl_account, ''), COALESCE(g.cost_center, ''), COALESCE(g.profit_center, ''), COALESCE(g.supplier, ''),
		       COALESCE(g.debit_credit_code, ''), COALESCE(g.amount_in_company_code_currency, 0)::float8, COALESCE(g.company_code_currency, ''),
		       COALESCE(g.amount_in_transaction_currency, 0)::float8, COALESCE(g.transaction_currency, ''), COALESCE(g.document_item_text, ''), g.raw
		FROM (
			SELECT docs.* FROM docs
			LEFT JOIN maximo_invoice_exports e ON e.source_type = 'JOURNAL' AND e.source_key = docs.k
			WHERE e.status IS NULL OR e.status <> 'EXPORTED'
			ORDER BY docs.pd NULLS LAST, docs.k
			LIMIT $2
		) d
		JOIN sap_gl_line_items g ON g.company_code = d.company_code AND g.fiscal_year = d.fiscal_year AND g.accounting_document = d.accounting_document
		ORDER BY d.pd NULLS LAST, d.k, g.ledger_gl_line_item`, docTypes, limit)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var order []string
	out := map[string][]model.GLLineItem{}
	for rows.Next() {
		var k string
		var it model.GLLineItem
		if err := rows.Scan(&k, &it.Key, &it.CompanyCode, &it.FiscalYear, &it.AccountingDocument, &it.LedgerGLLineItem,
			&it.AccountingDocumentType, &it.PostingDate, &it.DocumentDate, &it.GLAccount, &it.CostCenter, &it.ProfitCenter,
			&it.Supplier, &it.DebitCreditCode, &it.AmountInCompanyCodeCurrency, &it.CompanyCodeCurrency,
			&it.AmountInTransactionCurrency, &it.TransactionCurrency, &it.DocumentItemText, &it.Raw); err != nil {
			return nil, nil, err
		}
		if _, ok := out[k]; !ok {
			order = append(order, k)
		}
		out[k] = append(out[k], it)
	}
	return order, out, rows.Err()
}

// RecordMaximoExport mencatat hasil penulisan satu dokumen ke Maximo.
func (r *PostgresRepository) RecordMaximoExport(ctx context.Context, e model.MaximoExport) error {
	if r.Pool == nil {
		return nil
	}
	_, err := r.Pool.Exec(ctx, `
		INSERT INTO maximo_invoice_exports (source_type, source_key, status, line_count, trans_ids, error_message, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), NOW(), NOW())
		ON CONFLICT (source_type, source_key) DO UPDATE SET
			status = EXCLUDED.status, line_count = EXCLUDED.line_count, trans_ids = EXCLUDED.trans_ids,
			error_message = EXCLUDED.error_message, updated_at = NOW()
	`, e.SourceType, e.SourceKey, e.Status, e.LineCount, e.TransIDs, e.Error)
	return err
}

func (r *PostgresRepository) ListMaximoExports(ctx context.Context, limit int) ([]model.MaximoExport, error) {
	if r.Pool == nil {
		return nil, fmt.Errorf("postgres pool not available")
	}
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := r.Pool.Query(ctx, `
		SELECT source_type, source_key, status, line_count, COALESCE(trans_ids, ''), COALESCE(error_message, ''), updated_at
		FROM maximo_invoice_exports ORDER BY updated_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.MaximoExport
	for rows.Next() {
		var e model.MaximoExport
		if err := rows.Scan(&e.SourceType, &e.SourceKey, &e.Status, &e.LineCount, &e.TransIDs, &e.Error, &e.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// POLineInfo adalah informasi baris PO Maximo (dari raw_payload dokumen PO yang pernah dikirim).
type POLineInfo struct {
	ItemNum     string
	BudgetCode  string
	MainAccount string
}

// LookupPOLine mencari ITEMNUM / BUDGETCODE / MAINACCOUNT baris PO dari dokumen PO yang sudah diproses.
func (r *PostgresRepository) LookupPOLine(ctx context.Context, poNum string, lineNum int) (*POLineInfo, error) {
	if r.Pool == nil {
		return nil, nil
	}
	var info POLineInfo
	err := r.Pool.QueryRow(ctx, `
		SELECT COALESCE(l->>'ITEMNUM', ''), COALESCE(l->>'BUDGETCODE', ''), COALESCE(l->>'MAINACCOUNT', '')
		FROM documents d, jsonb_array_elements(COALESCE(d.raw_payload->'LINES', '[]'::jsonb)) l
		WHERE d.doc_type IN ('PO', 'AXMXPO') AND d.document_no = $1 AND l->>'POLINENUM' = $2::text
		ORDER BY d.updated_at DESC LIMIT 1`, poNum, fmt.Sprintf("%d", lineNum)).Scan(&info.ItemNum, &info.BudgetCode, &info.MainAccount)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &info, nil
}

// LookupGRNByMaterialDocument mencari nomor GRN Maximo (BPIGRNUMBER) dari nomor material document SAP.
func (r *PostgresRepository) LookupGRNByMaterialDocument(ctx context.Context, matDoc string) (string, error) {
	if r.Pool == nil || matDoc == "" {
		return "", nil
	}
	var grn string
	err := r.Pool.QueryRow(ctx, `
		SELECT document_no FROM documents
		WHERE doc_type = 'GRN' AND status = 'success' AND split_part(COALESCE(sap_document_no, ''), '/', 1) = $1
		ORDER BY updated_at DESC LIMIT 1`, matDoc).Scan(&grn)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return grn, err
}
