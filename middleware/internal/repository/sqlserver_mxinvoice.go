package repository

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	"golang-bpi-testing/internal/model"
)

// mxInvoiceColumns adalah urutan kolom dbo.mxinvoice (tanpa transid & transseq yang diisi terpisah).
var mxInvoiceColumns = []string{
	"currencycode", "invoicedesc", "invoicetype", "duedate", "glpostdate", "invoicedate", "invoicenum", "paid", "paiddate",
	"siteid", "status", "vendor", "budgetcode", "budgetnum", "bpichargecode", "bpichargevalue", "itemdesc", "bpigrnnumber",
	"invoicelinenum", "invoiceqty", "itemnum", "linecost", "linetype", "polinenum", "ponum", "positeid", "taxcode1",
	"taxcode2", "unitcost", "costlinenum", "gldebitacct", "transid", "transseq",
}

func nullTime(t *time.Time) interface{} {
	if t == nil {
		return sql.NullTime{}
	}
	return *t
}

func nullInt(v *int) interface{} {
	if v == nil {
		return sql.NullInt64{}
	}
	return int64(*v)
}

// mxInvoiceArgs mengembalikan nilai kolom sesuai urutan mxInvoiceColumns.
func mxInvoiceArgs(r model.MxInvoiceRow) []interface{} {
	return []interface{}{
		r.CurrencyCode, r.InvoiceDesc, r.InvoiceType, nullTime(r.DueDate), nullTime(r.GLPostDate), nullTime(r.InvoiceDate),
		r.InvoiceNum, r.Paid, nullTime(r.PaidDate), r.SiteID, r.Status, r.Vendor, r.BudgetCode, r.BudgetNum, r.BPIChargeCode,
		r.BPIChargeValue, r.ItemDesc, r.BPIGRNNumber, r.InvoiceLineNum, r.InvoiceQty, r.ItemNum, r.LineCost, r.LineType,
		nullInt(r.POLineNum), r.PONum, r.POSiteID, r.TaxCode1, r.TaxCode2, r.UnitCost, r.CostLineNum, r.GLDebitAcct,
		r.TransID, r.TransSeq,
	}
}

// MxInvoiceInsertSQL membangun INSERT dbo.mxinvoice dengan parameter @p1..@pN.
func MxInvoiceInsertSQL() string {
	ph := make([]string, len(mxInvoiceColumns))
	for i := range ph {
		ph[i] = fmt.Sprintf("@p%d", i+1)
	}
	return fmt.Sprintf("INSERT INTO dbo.mxinvoice (%s) VALUES (%s)", strings.Join(mxInvoiceColumns, ", "), strings.Join(ph, ", "))
}

func envDefault(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// WriteMxInvoiceMessage menulis seluruh baris satu dokumen ke dbo.mxinvoice beserta baris kontrol
// dbo.mxin_inter_trans dalam satu transaksi. Mengikuti pola data yang sudah ada di Maximo: setiap baris
// invoice mendapat transid sendiri dengan transseq 1. transid diambil dari IDENTITY mxin_inter_trans bila
// kolomnya IDENTITY, atau MAX(transid)+1 (dengan UPDLOCK) bila tidak.
func (r *SQLServerRepository) WriteMxInvoiceMessage(ctx context.Context, rows []model.MxInvoiceRow) ([]int64, error) {
	if r == nil || r.DB == nil {
		return nil, fmt.Errorf("SQL Server Maximo tidak dikonfigurasi (SQL_CONN_STRING kosong)")
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("tidak ada baris untuk ditulis")
	}
	iface := envDefault("FIN_MAXIMO_INVOICE_IFACE", "MXINVOICE")
	extSys := envDefault("FIN_MAXIMO_EXTSYSNAME", "SAP_GROW")

	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var identity sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT COLUMNPROPERTY(OBJECT_ID('dbo.mxin_inter_trans'), 'transid', 'IsIdentity')`).Scan(&identity); err != nil {
		return nil, fmt.Errorf("cek struktur mxin_inter_trans: %w", err)
	}

	insertSQL := MxInvoiceInsertSQL()
	transIDs := make([]int64, 0, len(rows))
	for _, row := range rows {
		var transID int64
		if identity.Valid && identity.Int64 == 1 {
			err = tx.QueryRowContext(ctx, `
				INSERT INTO dbo.mxin_inter_trans (ifacename, [action], extsysname, translanguage)
				OUTPUT INSERTED.transid VALUES (@p1, 'Add', @p2, 'EN')`, iface, extSys).Scan(&transID)
			if err != nil {
				return nil, fmt.Errorf("insert mxin_inter_trans: %w", err)
			}
		} else {
			err = tx.QueryRowContext(ctx, `
				SELECT CASE WHEN a.m > b.m THEN a.m ELSE b.m END + 1
				FROM (SELECT ISNULL(MAX(transid), 0) AS m FROM dbo.mxin_inter_trans WITH (UPDLOCK, HOLDLOCK)) a,
				     (SELECT ISNULL(MAX(transid), 0) AS m FROM dbo.mxinvoice WITH (UPDLOCK, HOLDLOCK)) b`).Scan(&transID)
			if err != nil {
				return nil, fmt.Errorf("ambil transid baru: %w", err)
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO dbo.mxin_inter_trans (ifacename, transid, [action], extsysname, translanguage)
				VALUES (@p1, @p2, 'Add', @p3, 'EN')`, iface, transID, extSys); err != nil {
				return nil, fmt.Errorf("insert mxin_inter_trans: %w", err)
			}
		}
		row.TransID = transID
		if row.TransSeq == 0 {
			row.TransSeq = 1
		}
		if _, err := tx.ExecContext(ctx, insertSQL, mxInvoiceArgs(row)...); err != nil {
			return nil, fmt.Errorf("insert mxinvoice line %d: %w", row.InvoiceLineNum, err)
		}
		transIDs = append(transIDs, transID)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return transIDs, nil
}
