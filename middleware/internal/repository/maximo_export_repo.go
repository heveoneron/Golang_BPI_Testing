package repository

import (
	"context"
	"fmt"

	"golang-bpi-testing/internal/model"
)

// Jenis sumber ekspor ke dbo.mxinvoice (kolom maximo_invoice_exports.source_type).
const (
	MaximoSourceInvoice = "INVOICE" // RICEF 4: dokumen RE/KR dengan baris supplier (AP)
	MaximoSourceJournal = "JOURNAL" // RICEF 3: dokumen SA dengan baris cost center
)

// ListGLDocumentsForExport mengelompokkan GL line item per dokumen akuntansi yang belum ditulis ke Maximo.
// INVOICE hanya mengambil dokumen yang punya baris supplier; JOURNAL hanya yang punya baris cost center.
// Hasil: urutan kunci dokumen dan map kunci -> baris line item (urut nomor item).
func (r *PostgresRepository) ListGLDocumentsForExport(ctx context.Context, sourceType string, docTypes []string, limit int) ([]string, map[string][]model.GLLineItem, error) {
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
			HAVING CASE WHEN $3 = 'INVOICE' THEN bool_or(COALESCE(g.supplier, '') <> '')
			            ELSE bool_or(COALESCE(g.cost_center, '') <> '') END
		)
		SELECT d.k, g.gl_line_item_key, COALESCE(g.company_code, ''), COALESCE(g.fiscal_year, ''), COALESCE(g.accounting_document, ''),
		       COALESCE(g.ledger_gl_line_item, ''), COALESCE(g.accounting_document_type, ''),
		       TO_CHAR(g.posting_date, 'YYYY-MM-DD'), TO_CHAR(g.document_date, 'YYYY-MM-DD'),
		       COALESCE(g.gl_account, ''), COALESCE(g.cost_center, ''), COALESCE(g.profit_center, ''), COALESCE(g.supplier, ''),
		       COALESCE(g.debit_credit_code, ''), COALESCE(g.amount_in_company_code_currency, 0)::float8, COALESCE(g.company_code_currency, ''),
		       COALESCE(g.amount_in_transaction_currency, 0)::float8, COALESCE(g.transaction_currency, ''), COALESCE(g.document_item_text, ''), g.raw
		FROM (
			SELECT docs.* FROM docs
			LEFT JOIN maximo_invoice_exports e ON e.source_type = $3 AND e.source_key = docs.k
			WHERE e.status IS NULL OR e.status <> 'EXPORTED'
			ORDER BY docs.pd NULLS LAST, docs.k
			LIMIT $2
		) d
		JOIN sap_gl_line_items g ON g.company_code = d.company_code AND g.fiscal_year = d.fiscal_year AND g.accounting_document = d.accounting_document
		ORDER BY d.pd NULLS LAST, d.k, g.ledger_gl_line_item`, docTypes, limit, sourceType)
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
