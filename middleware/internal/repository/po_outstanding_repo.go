package repository

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// POOutstandingLine = satu baris "Template PO Outstanding and AX.xlsx" di tabel stg_po_outstanding_legacy.
type POOutstandingLine struct {
	ID            int64      `json:"id"`
	BatchID       string     `json:"batch_id"`
	SourceSystem  string     `json:"source_system"`
	RowNumber     int        `json:"row_number"`
	CurrencyCode  string     `json:"currencycode"`
	OrderDate     string     `json:"orderdate"` // YYYY-MM-DD
	DPPercentage  float64    `json:"dp_percentage"`
	PONum         string     `json:"ponum"`
	SiteID        string     `json:"siteid"`
	Vendor        string     `json:"vendor"`
	BPIBudgetCode string     `json:"bpibudgetcode"`
	POLDescLD     string     `json:"pol_desc_ld"`
	ItemNum       string     `json:"itemnum"`
	OrderQty      float64    `json:"orderqty"`
	OrderUnit     string     `json:"orderunit"`
	POLineNum     int        `json:"polinenum"`
	TaxCode1      string     `json:"taxcode1"`
	UnitCost      float64    `json:"unitcost"`
	SyncStatus    string     `json:"sync_status"`
	SAPPONumber   string     `json:"sap_po_number"`
	ErrorMessage  string     `json:"error_message"`
	IsSent        bool       `json:"is_sent"`
	IsProcessing  bool       `json:"is_processing"`
	SentAt        *time.Time `json:"sent_at"`
	RetryCount    int        `json:"retry_count"`
	UploadedBy    string     `json:"uploaded_by"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// POOutstandingKey mengidentifikasi satu PO (semua baris dengan source & ponum sama dikirim sebagai satu PO).
type POOutstandingKey struct {
	SourceSystem string
	PONum        string
}

const poOutstandingCols = `id, batch_id, source_system, COALESCE(row_number, 0), COALESCE(currencycode, ''),
	to_char(orderdate, 'YYYY-MM-DD'), COALESCE(dp_percentage, 0)::float8, ponum, COALESCE(siteid, ''), COALESCE(vendor, ''),
	COALESCE(bpibudgetcode, ''), COALESCE(pol_desc_ld, ''), COALESCE(itemnum, ''), COALESCE(orderqty, 0)::float8,
	COALESCE(orderunit, ''), polinenum, COALESCE(taxcode1, ''), COALESCE(unitcost, 0)::float8, sync_status,
	COALESCE(sap_po_number, ''), COALESCE(error_message, ''), is_sent, is_processing, sent_at, retry_count,
	COALESCE(uploaded_by, ''), created_at, updated_at`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanPOOutstanding(s rowScanner) (POOutstandingLine, error) {
	var l POOutstandingLine
	err := s.Scan(&l.ID, &l.BatchID, &l.SourceSystem, &l.RowNumber, &l.CurrencyCode, &l.OrderDate, &l.DPPercentage,
		&l.PONum, &l.SiteID, &l.Vendor, &l.BPIBudgetCode, &l.POLDescLD, &l.ItemNum, &l.OrderQty, &l.OrderUnit,
		&l.POLineNum, &l.TaxCode1, &l.UnitCost, &l.SyncStatus, &l.SAPPONumber, &l.ErrorMessage, &l.IsSent,
		&l.IsProcessing, &l.SentAt, &l.RetryCount, &l.UploadedBy, &l.CreatedAt, &l.UpdatedAt)
	return l, err
}

// SyncedPONumbers mengembalikan PO (source|ponum) yang sudah punya baris terkirim; baris baru untuk PO ini
// ditolak saat upload karena PO SAP-nya sudah terbentuk.
func (r *PostgresRepository) SyncedPONumbers(ctx context.Context, keys []POOutstandingKey) (map[POOutstandingKey]string, error) {
	out := map[POOutstandingKey]string{}
	if len(keys) == 0 {
		return out, nil
	}
	sources, ponums := make([]string, len(keys)), make([]string, len(keys))
	for i, k := range keys {
		sources[i], ponums[i] = k.SourceSystem, k.PONum
	}
	rows, err := r.Pool.Query(ctx, `
		SELECT DISTINCT s.source_system, s.ponum, COALESCE(s.sap_po_number, '')
		FROM stg_po_outstanding_legacy s
		JOIN unnest($1::text[], $2::text[]) AS k(src, po) ON k.src = s.source_system AND k.po = s.ponum
		WHERE s.is_sent OR s.is_processing`, sources, ponums)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k POOutstandingKey
		var sapPO string
		if err := rows.Scan(&k.SourceSystem, &k.PONum, &sapPO); err != nil {
			return nil, err
		}
		out[k] = sapPO
	}
	return out, rows.Err()
}

// UpsertPOOutstanding menyimpan baris hasil upload. Baris yang sudah terkirim / sedang diproses tidak ditimpa.
// Mengembalikan jumlah baris tersimpan.
func (r *PostgresRepository) UpsertPOOutstanding(ctx context.Context, batchID, uploadedBy string, lines []POOutstandingLine) (int, error) {
	if r.Pool == nil {
		return 0, fmt.Errorf("postgres pool not available")
	}
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	saved := 0
	for _, l := range lines {
		tag, err := tx.Exec(ctx, `
			INSERT INTO stg_po_outstanding_legacy (batch_id, source_system, row_number, currencycode, orderdate, dp_percentage,
				ponum, siteid, vendor, bpibudgetcode, pol_desc_ld, itemnum, orderqty, orderunit, polinenum, taxcode1,
				unitcost, sync_status, uploaded_by, updated_at)
			VALUES ($1, $2, $3, $4, $5::date, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, 'PENDING', $18, NOW())
			ON CONFLICT (source_system, ponum, polinenum) DO UPDATE SET
				batch_id = EXCLUDED.batch_id, row_number = EXCLUDED.row_number, currencycode = EXCLUDED.currencycode,
				orderdate = EXCLUDED.orderdate, dp_percentage = EXCLUDED.dp_percentage, siteid = EXCLUDED.siteid,
				vendor = EXCLUDED.vendor, bpibudgetcode = EXCLUDED.bpibudgetcode, pol_desc_ld = EXCLUDED.pol_desc_ld,
				itemnum = EXCLUDED.itemnum, orderqty = EXCLUDED.orderqty, orderunit = EXCLUDED.orderunit,
				taxcode1 = EXCLUDED.taxcode1, unitcost = EXCLUDED.unitcost, sync_status = 'PENDING',
				error_message = NULL, uploaded_by = EXCLUDED.uploaded_by, updated_at = NOW()
			WHERE NOT stg_po_outstanding_legacy.is_sent AND NOT stg_po_outstanding_legacy.is_processing`,
			batchID, l.SourceSystem, l.RowNumber, l.CurrencyCode, l.OrderDate, l.DPPercentage, l.PONum, l.SiteID,
			l.Vendor, l.BPIBudgetCode, l.POLDescLD, l.ItemNum, l.OrderQty, l.OrderUnit, l.POLineNum, l.TaxCode1,
			l.UnitCost, uploadedBy)
		if err != nil {
			return 0, fmt.Errorf("PO %s baris %d: %w", l.PONum, l.POLineNum, err)
		}
		saved += int(tag.RowsAffected())
	}
	return saved, tx.Commit(ctx)
}

// ListPOOutstanding menampilkan baris staging (filter status: PENDING / PROCESSING / SYNCED / FAILED; kosong = semua).
func (r *PostgresRepository) ListPOOutstanding(ctx context.Context, status, search string, limit int) ([]POOutstandingLine, error) {
	if r.Pool == nil {
		return nil, fmt.Errorf("postgres pool not available")
	}
	if limit <= 0 || limit > 5000 {
		limit = 1000
	}
	rows, err := r.Pool.Query(ctx, `SELECT `+poOutstandingCols+` FROM stg_po_outstanding_legacy
		WHERE ($1 = '' OR sync_status = $1)
		  AND ($2 = '' OR ponum ILIKE '%' || $2 || '%' OR vendor ILIKE '%' || $2 || '%' OR itemnum ILIKE '%' || $2 || '%')
		ORDER BY ponum, polinenum LIMIT $3`, strings.ToUpper(strings.TrimSpace(status)), strings.TrimSpace(search), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []POOutstandingLine
	for rows.Next() {
		l, err := scanPOOutstanding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// POOutstandingStats menghitung jumlah baris per sync_status.
func (r *PostgresRepository) POOutstandingStats(ctx context.Context) (map[string]int, error) {
	rows, err := r.Pool.Query(ctx, `SELECT sync_status, COUNT(*) FROM stg_po_outstanding_legacy GROUP BY sync_status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var s string
		var n int
		if err := rows.Scan(&s, &n); err != nil {
			return nil, err
		}
		out[s] = n
	}
	return out, rows.Err()
}

// POOutstandingKeys menerjemahkan pilihan baris (id) atau "semua pending" menjadi daftar PO yang akan dikirim.
func (r *PostgresRepository) POOutstandingKeys(ctx context.Context, ids []int64, allPending bool) ([]POOutstandingKey, error) {
	q := `SELECT DISTINCT source_system, ponum FROM stg_po_outstanding_legacy WHERE id = ANY($1) ORDER BY ponum`
	args := []any{ids}
	if allPending {
		q = `SELECT DISTINCT source_system, ponum FROM stg_po_outstanding_legacy
			WHERE sync_status = 'PENDING' AND NOT is_sent AND NOT is_processing ORDER BY ponum`
		args = nil
	}
	rows, err := r.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []POOutstandingKey
	for rows.Next() {
		var k POOutstandingKey
		if err := rows.Scan(&k.SourceSystem, &k.PONum); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// ClaimPOOutstanding mengunci atomik semua baris PO yang belum terkirim (PRD Idempotency Pre-Send Check).
// Kosong = PO sedang diproses worker lain atau sudah terkirim -> pengiriman dibatalkan.
func (r *PostgresRepository) ClaimPOOutstanding(ctx context.Context, key POOutstandingKey) ([]POOutstandingLine, error) {
	rows, err := r.Pool.Query(ctx, `
		UPDATE stg_po_outstanding_legacy SET is_processing = TRUE, sync_status = 'PROCESSING', updated_at = NOW()
		WHERE source_system = $1 AND ponum = $2 AND NOT is_sent AND NOT is_processing
		  AND NOT EXISTS (SELECT 1 FROM stg_po_outstanding_legacy o
		                  WHERE o.source_system = $1 AND o.ponum = $2 AND (o.is_sent OR o.is_processing))
		RETURNING `+poOutstandingCols, key.SourceSystem, key.PONum)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []POOutstandingLine
	for rows.Next() {
		l, err := scanPOOutstanding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// FinishPOOutstanding mencatat hasil pengiriman satu PO (Post-Send Update).
func (r *PostgresRepository) FinishPOOutstanding(ctx context.Context, ids []int64, sapPONumber, errMsg string) error {
	if errMsg == "" {
		_, err := r.Pool.Exec(ctx, `
			UPDATE stg_po_outstanding_legacy SET is_sent = TRUE, is_processing = FALSE, sync_status = 'SYNCED',
				sap_po_number = $2, sent_at = NOW(), error_message = NULL, updated_at = NOW()
			WHERE id = ANY($1)`, ids, sapPONumber)
		return err
	}
	_, err := r.Pool.Exec(ctx, `
		UPDATE stg_po_outstanding_legacy SET is_processing = FALSE, sync_status = 'FAILED', error_message = $2,
			retry_count = retry_count + 1, updated_at = NOW()
		WHERE id = ANY($1)`, ids, errMsg)
	return err
}
